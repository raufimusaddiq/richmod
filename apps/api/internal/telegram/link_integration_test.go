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

func TestTelegramInviteLifecycle(t *testing.T) {
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
	var householdID, ownerID, memberID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Invite %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		name string
		id   *string
	}{{"owner", &ownerID}, {"member", &memberID}} {
		if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,$2,'unused') RETURNING id`, fmt.Sprintf("%s-%d@example.test", entry.name, stamp), entry.name).Scan(entry.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER'),($1,$3,'MEMBER')`, householdID, ownerID, memberID); err != nil {
		t.Fatal(err)
	}
	store := NewPostgreSQLStore(pool)

	createInvite := func(token, status string, expiry time.Time) {
		digest := sha256.Sum256([]byte(token))
		if _, e := pool.Exec(ctx, `INSERT INTO telegram_link_invite(household_id,user_id,token_hash,status,expires_at,created_by_user_id) VALUES($1,$2,$3,$4,$5,$6)`, householdID, memberID, digest[:], status, expiry, ownerID); e != nil {
			t.Fatal(e)
		}
	}
	validToken := "valid-telegram-link-token-123456"
	createInvite(validToken, "PENDING", time.Now().Add(time.Hour))
	telegramID := int64(stamp%8_000_000_000 + 1_000_000_000)
	linked, err := store.Link(ctx, CaptureInput{UpdateID: stamp, TelegramUserID: telegramID}, validToken)
	if err != nil || !linked {
		t.Fatalf("linked=%v err=%v", linked, err)
	}
	var linkedUser, status string
	if err = pool.QueryRow(ctx, `SELECT ti.user_id,tli.status FROM telegram_identity ti JOIN telegram_link_invite tli ON tli.user_id=ti.user_id WHERE ti.telegram_user_id=$1`, telegramID).Scan(&linkedUser, &status); err != nil || linkedUser != memberID || status != "CONSUMED" {
		t.Fatalf("user=%s status=%s err=%v", linkedUser, status, err)
	}
	if _, err = store.Link(ctx, CaptureInput{UpdateID: stamp + 1, TelegramUserID: telegramID + 1}, validToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("reused err=%v", err)
	}

	revoked := "revoked-telegram-link-token-123"
	createInvite(revoked, "REVOKED", time.Now().Add(time.Hour))
	if _, err = store.Link(ctx, CaptureInput{UpdateID: stamp + 2, TelegramUserID: telegramID + 2}, revoked); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked err=%v", err)
	}
	expired := "expired-telegram-link-token-123"
	createInvite(expired, "PENDING", time.Now().Add(-time.Minute))
	if _, err = store.Link(ctx, CaptureInput{UpdateID: stamp + 3, TelegramUserID: telegramID + 3}, expired); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired err=%v", err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM telegram_link_invite WHERE token_hash=$1`, sha256Bytes(expired)).Scan(&status); err != nil || status != "EXPIRED" {
		t.Fatalf("expired status=%s err=%v", status, err)
	}
}

func sha256Bytes(value string) []byte { digest := sha256.Sum256([]byte(value)); return digest[:] }
