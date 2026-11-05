package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestCaptureRejectsDisabledUserAndRemovedMember: an active Telegram identity
// alone is not enough; the user must be active and still a member.
func TestCaptureRejectsDisabledUserAndRemovedMember(t *testing.T) {
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
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Access %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Access Test','x') RETURNING id`, fmt.Sprintf("access-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	telegramID := stamp%9_000_000_000 + 7
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, telegramID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	store := NewPostgreSQLStore(pool)
	capture := func(offset int64) error {
		_, err := store.Capture(ctx, CaptureInput{UpdateID: stamp + offset, TelegramUserID: telegramID, RawPayload: []byte(fmt.Sprintf(`{"update_id":%d,"message":{"text":"makan 10rb"}}`, stamp+offset))})
		return err
	}
	if err := capture(1); err != nil {
		t.Fatalf("active member: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE "user" SET active=false WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if err := capture(2); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("disabled user: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE "user" SET active=true WHERE id=$1; `, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE household_member SET active=false,deactivated_at=now() WHERE user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if err := capture(3); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("removed member: %v", err)
	}
}
