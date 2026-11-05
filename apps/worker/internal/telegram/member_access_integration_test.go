package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func memberAccessPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type accessFixture struct {
	household, otherHousehold, user string
	telegramID                      int64
}

func newAccessFixture(t *testing.T, pool *pgxpool.Pool) accessFixture {
	t.Helper()
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var f accessFixture
	for _, target := range []*string{&f.household, &f.otherHousehold} {
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Member access %d", stamp)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Member','x') RETURNING id`, fmt.Sprintf("member-access-%d@example.test", stamp)).Scan(&f.user); err != nil {
		t.Fatal(err)
	}
	f.telegramID = stamp%8_000_000_000 + 11
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, f.household, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, f.telegramID, f.household, f.user); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTelegramMemberAuthorized(t *testing.T) {
	pool := memberAccessPool(t)
	ctx := context.Background()
	f := newAccessFixture(t, pool)
	check := func(label, household string, want bool) {
		t.Helper()
		got, err := telegramMemberAuthorized(ctx, pool, f.telegramID, household)
		if err != nil || got != want {
			t.Fatalf("%s: authorized=%v err=%v, want %v", label, got, err, want)
		}
	}
	check("active member, own household", f.household, true)
	check("active member, any household", "", true)
	check("active member, another household", f.otherHousehold, false)
	if _, err := pool.Exec(ctx, `UPDATE "user" SET active=false WHERE id=$1`, f.user); err != nil {
		t.Fatal(err)
	}
	check("disabled user", f.household, false)
	if _, err := pool.Exec(ctx, `UPDATE "user" SET active=true WHERE id=$1`, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE household_member SET active=false,deactivated_at=now() WHERE user_id=$1`, f.user); err != nil {
		t.Fatal(err)
	}
	check("removed member", f.household, false)
}

// TestQueuedTurnFromRemovedMemberIsIgnoredSilently: a message captured while
// the sender was a member, but processed after removal, gets no answer.
func TestQueuedTurnFromRemovedMemberIsIgnoredSilently(t *testing.T) {
	pool := memberAccessPool(t)
	ctx := context.Background()
	f := newAccessFixture(t, pool)
	var sourceEvent string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_chat_id) VALUES($1,'TELEGRAM_TEXT',$2,now(),'\x01','RECEIVED',$3) RETURNING id`, f.household, fmt.Sprintf("telegram:update:access-%d", f.telegramID), f.telegramID).Scan(&sourceEvent); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"update_id":1,"message":{"message_id":5,"text":"berapa pengeluaran bulan ini","from":{"id":%d},"chat":{"id":%d,"type":"private"}}}`, f.telegramID, f.telegramID)
	if _, err := pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceEvent, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE household_member SET active=false,deactivated_at=now() WHERE user_id=$1`, f.user); err != nil {
		t.Fatal(err)
	}
	if err := NewProcessor(pool, nil).ProcessAgent(ctx, sourceEvent); err != nil {
		t.Fatal(err)
	}
	var status string
	var replies int
	if err := pool.QueryRow(ctx, `SELECT processing_status,(SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$2::bigint::text) FROM source_event WHERE id=$1`, sourceEvent, f.telegramID).Scan(&status, &replies); err != nil {
		t.Fatal(err)
	}
	if status != "IGNORED" || replies != 0 {
		t.Fatalf("status=%s replies=%d, want IGNORED and no reply", status, replies)
	}
}
