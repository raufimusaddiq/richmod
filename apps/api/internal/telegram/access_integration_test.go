package telegram

import (
	"context"
	"crypto/sha256"
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

// TestLinkRebindsRetiredIdentityButNeverMovesAnActiveOne: a Telegram account
// whose earlier membership was removed can be linked again through an invite;
// an account still linked to an active member is refused.
func TestLinkRebindsRetiredIdentityButNeverMovesAnActiveOne(t *testing.T) {
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
	var oldHousehold, newHousehold, formerUser, newMember, owner string
	for _, target := range []*string{&oldHousehold, &newHousehold} {
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Relink %d", stamp)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	for i, target := range []*string{&formerUser, &newMember, &owner} {
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Relink','x') RETURNING id`, fmt.Sprintf("relink-%d-%d@example.test", stamp, i)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role,active,deactivated_at) VALUES($1,$2,'MEMBER',false,now())`, oldHousehold, formerUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER'),($1,$3,'MEMBER')`, newHousehold, owner, newMember); err != nil {
		t.Fatal(err)
	}
	telegramID := stamp%9_000_000_000 + 29
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id,active) VALUES($1,$2,$3,false)`, telegramID, oldHousehold, formerUser); err != nil {
		t.Fatal(err)
	}
	invite := func(token string) {
		t.Helper()
		digest := sha256.Sum256([]byte(token))
		if _, err := pool.Exec(ctx, `UPDATE telegram_link_invite SET status='REVOKED' WHERE household_id=$1 AND user_id=$2 AND status='PENDING'`, newHousehold, newMember); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO telegram_link_invite(household_id,user_id,token_hash,expires_at,created_by_user_id) VALUES($1,$2,$3,now()+interval '15 minutes',$4)`, newHousehold, newMember, digest[:], owner); err != nil {
			t.Fatal(err)
		}
	}
	// The removed member left an open prompt in the old household.
	var oldSource string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),'\x01','PROCESSED') RETURNING id`, oldHousehold, fmt.Sprintf("relink-%d", stamp)).Scan(&oldSource); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_pending_batch(household_id,telegram_user_id,telegram_chat_id,source_event_id,items_json,status,expires_at) VALUES($1,$2,$2,$3,'[]','PENDING',now()+interval '1 hour')`, oldHousehold, telegramID, oldSource); err != nil {
		t.Fatalf("seed pending batch: %v", err)
	}
	store := NewPostgreSQLStore(pool)
	token := fmt.Sprintf("relink-token-%d", stamp)
	invite(token)
	if _, err := store.Link(ctx, CaptureInput{UpdateID: stamp, TelegramUserID: telegramID, RawPayload: []byte(`{}`)}, token); err != nil {
		t.Fatalf("relink a retired identity: %v", err)
	}
	var household, user string
	var active bool
	if err := pool.QueryRow(ctx, `SELECT household_id::text,user_id::text,active FROM telegram_identity WHERE telegram_user_id=$1`, telegramID).Scan(&household, &user, &active); err != nil {
		t.Fatal(err)
	}
	if household != newHousehold || user != newMember || !active {
		t.Fatalf("identity = household %s user %s active %v", household, user, active)
	}
	var openPrompts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM telegram_pending_batch WHERE telegram_user_id=$1 AND status='PENDING'`, telegramID).Scan(&openPrompts); err != nil || openPrompts != 0 {
		t.Fatalf("old household prompts still open: %d (%v)", openPrompts, err)
	}
	// Now active: another invite (for the owner) cannot take it over.
	ownerToken := fmt.Sprintf("relink-owner-token-%d", stamp)
	digest := sha256.Sum256([]byte(ownerToken))
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_link_invite(household_id,user_id,token_hash,expires_at,created_by_user_id) VALUES($1,$2,$3,now()+interval '15 minutes',$2)`, newHousehold, owner, digest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Link(ctx, CaptureInput{UpdateID: stamp + 1, TelegramUserID: telegramID, RawPayload: []byte(`{}`)}, ownerToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("active identity moved: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT user_id::text FROM telegram_identity WHERE telegram_user_id=$1`, telegramID).Scan(&user); err != nil || user != newMember {
		t.Fatalf("active identity owner changed to %s (%v)", user, err)
	}
}
