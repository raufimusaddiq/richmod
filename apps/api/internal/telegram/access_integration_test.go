package telegram

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

func TestCaptureRejectsDisabledUserAndRemovedMember(t *testing.T) {
	t.Parallel()
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
	stamp := testsupport.UniqueStamp()
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

func TestLinkNeverRebindsABoundTelegramID(t *testing.T) {
	t.Parallel()
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
	stamp := testsupport.UniqueStamp()
	var household, formerUser, newMember, owner string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Relink %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	for i, target := range []*string{&formerUser, &newMember, &owner} {
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Relink','x') RETURNING id`, fmt.Sprintf("relink-%d-%d@example.test", stamp, i)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role,active,deactivated_at) VALUES($1,$2,'MEMBER',false,now())`, household, formerUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER'),($1,$3,'MEMBER')`, household, owner, newMember); err != nil {
		t.Fatal(err)
	}
	retiredID, ownerID := stamp%9_000_000_000+29, stamp%9_000_000_000+31
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id,active) VALUES($1,$2,$3,false),($4,$2,$5,true)`, retiredID, household, formerUser, ownerID, owner); err != nil {
		t.Fatal(err)
	}
	store := NewPostgreSQLStore(pool)
	link := func(offset int64, telegramID int64) error {
		t.Helper()
		token := fmt.Sprintf("relink-token-%d-%d", stamp, offset)
		digest := sha256.Sum256([]byte(token))
		if _, err := pool.Exec(ctx, `UPDATE telegram_link_invite SET status='REVOKED' WHERE household_id=$1 AND user_id=$2 AND status='PENDING'`, household, newMember); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO telegram_link_invite(household_id,user_id,token_hash,expires_at,created_by_user_id) VALUES($1,$2,$3,now()+interval '15 minutes',$4)`, household, newMember, digest[:], owner); err != nil {
			t.Fatal(err)
		}
		_, err := store.Link(ctx, CaptureInput{UpdateID: stamp + offset, TelegramUserID: telegramID, RawPayload: []byte(`{}`)}, token)
		return err
	}
	for name, telegramID := range map[string]int64{"removed member's ID": retiredID, "active member's ID": ownerID} {
		if err := link(telegramID, telegramID); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("%s was rebound: %v", name, err)
		}
	}
	var bound int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM telegram_identity WHERE (telegram_user_id=$1 AND user_id=$2 AND NOT active) OR (telegram_user_id=$3 AND user_id=$4 AND active)`, retiredID, formerUser, ownerID, owner).Scan(&bound); err != nil || bound != 2 {
		t.Fatalf("bindings changed: %d intact (%v)", bound, err)
	}
	if err := link(1, stamp%9_000_000_000+37); err != nil {
		t.Fatalf("fresh Telegram ID: %v", err)
	}
}

func TestCaptureRoutesRepliesToLiveReviewCardsToTheCardAnswerJob(t *testing.T) {
	t.Parallel()
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
	stamp := testsupport.UniqueStamp()
	telegramID := stamp%9_000_000_000 + 11
	var householdID, userID, transactionID, itemID, requestID string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Card answer %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Card Answer','x') RETURNING id`, fmt.Sprintf("card-answer-%d@example.test", stamp)).Scan(&userID))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, householdID, userID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, telegramID, householdID, userID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',25000,now()) RETURNING id`, householdID).Scan(&transactionID))
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_MERCHANT','OPEN','{"version":1,"reasonCode":"UNKNOWN_MERCHANT","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, householdID, transactionID).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'UNKNOWN_MERCHANT','OPEN',$4) RETURNING id`, householdID, itemID, transactionID, telegramID).Scan(&requestID))
	_, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,4242)`, requestID, telegramID)
	must(err)

	store := NewPostgreSQLStore(pool)
	jobFor := func(offset int64, replyTo int64) string {
		t.Helper()
		reply := ""
		if replyTo != 0 {
			reply = fmt.Sprintf(`,"reply_to_message":{"message_id":%d}`, replyTo)
		}
		payload := fmt.Sprintf(`{"update_id":%d,"message":{"message_id":%d,"text":"Warung Contoh","chat":{"id":%d}%s}}`, stamp+offset, 9000+offset, telegramID, reply)
		_, err := store.Capture(ctx, CaptureInput{UpdateID: stamp + offset, TelegramUserID: telegramID, RawPayload: []byte(payload)})
		must(err)
		var jobType string
		must(pool.QueryRow(ctx, `SELECT j.type FROM job j JOIN source_event s ON s.id::text=j.payload_json->>'source_event_id' WHERE s.external_id=$1`, fmt.Sprintf("telegram:update:%d", stamp+offset)).Scan(&jobType))
		return jobType
	}
	if got := jobFor(1, 4242); got != "PROCESS_TELEGRAM_CARD_ANSWER" {
		t.Fatalf("a reply to a live card queued %s", got)
	}
	if got := jobFor(2, 777); got != "PROCESS_TELEGRAM_TEXT" {
		t.Fatalf("a reply to an unrelated message queued %s", got)
	}
	if got := jobFor(3, 0); got != "PROCESS_TELEGRAM_TEXT" {
		t.Fatalf("a plain message queued %s", got)
	}
	_, err = pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now() WHERE id=$1`, itemID)
	must(err)
	if got := jobFor(4, 4242); got != "PROCESS_TELEGRAM_TEXT" {
		t.Fatalf("a reply to a finished card queued %s", got)
	}
}
