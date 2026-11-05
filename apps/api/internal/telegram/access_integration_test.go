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

// TestLinkNeverRebindsABoundTelegramID: Telegram ID and user are bound 1:1.
// An ID still bound to a removed member (inactive) or to an active member is
// refused through any invite, and the binding is left untouched.
func TestLinkNeverRebindsABoundTelegramID(t *testing.T) {
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
	// A fresh Telegram ID still links normally.
	if err := link(1, stamp%9_000_000_000+37); err != nil {
		t.Fatalf("fresh Telegram ID: %v", err)
	}
}
