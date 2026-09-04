package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReplayCreatesOneSourceEventAndOneJob(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stamp := time.Now().UnixNano()
	var householdID, userID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Replay %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	email := fmt.Sprintf("replay-%d@example.test", stamp)
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Replay Test','not-used') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	telegramID := stamp % 9_000_000_000
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, telegramID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	input := CaptureInput{UpdateID: stamp, TelegramUserID: telegramID, RawPayload: []byte(fmt.Sprintf(`{"update_id":%d,"message":{"text":"makan 10rb"}}`, stamp))}
	store := NewPostgreSQLStore(pool)
	created, err := store.Capture(ctx, input)
	if err != nil || !created {
		t.Fatalf("first capture: created=%v err=%v", created, err)
	}
	created, err = store.Capture(ctx, input)
	if err != nil || created {
		t.Fatalf("replay capture: created=%v err=%v", created, err)
	}
	var events, jobs int
	externalID := fmt.Sprintf("telegram:update:%d", stamp)
	if err := pool.QueryRow(ctx, `SELECT count(*),(SELECT count(*) FROM job j JOIN source_event s ON (j.payload_json->>'source_event_id')::uuid=s.id WHERE s.household_id=$1 AND s.external_id=$2) FROM source_event WHERE household_id=$1 AND external_id=$2`, householdID, externalID).Scan(&events, &jobs); err != nil {
		t.Fatal(err)
	}
	if events != 1 || jobs != 1 {
		t.Fatalf("replay produced events=%d jobs=%d", events, jobs)
	}
}

func TestImageReplayCreatesOneTelegramImageJob(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stamp := time.Now().UnixNano()
	var householdID, userID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Image replay %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Image Test','unused') RETURNING id`, fmt.Sprintf("image-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	telegramID := stamp % 9_000_000_000
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, telegramID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	input := ImageInput{CaptureInput: CaptureInput{UpdateID: stamp, TelegramUserID: telegramID, RawPayload: []byte(fmt.Sprintf(`{"update_id":%d}`, stamp))}, FileID: "telegram-file-id", FileName: "receipt.jpg", MIMEType: "image/jpeg", Caption: "struk"}
	store := NewPostgreSQLStore(pool)
	created, err := store.CaptureImage(ctx, input)
	if err != nil || !created {
		t.Fatalf("first capture created=%v err=%v", created, err)
	}
	created, err = store.CaptureImage(ctx, input)
	if err != nil || created {
		t.Fatalf("replay created=%v err=%v", created, err)
	}
	var sourceType, jobType string
	var jobs int
	if err = pool.QueryRow(ctx, `SELECT s.source_type,j.type,count(*) OVER() FROM source_event s JOIN job j ON (j.payload_json->>'source_event_id')::uuid=s.id WHERE s.household_id=$1 AND s.external_id=$2`, householdID, fmt.Sprintf("telegram:update:%d", stamp)).Scan(&sourceType, &jobType, &jobs); err != nil {
		t.Fatal(err)
	}
	if sourceType != "TELEGRAM_IMAGE" || jobType != "FETCH_TELEGRAM_IMAGE" || jobs != 1 {
		t.Fatalf("source=%s job=%s count=%d", sourceType, jobType, jobs)
	}
}
