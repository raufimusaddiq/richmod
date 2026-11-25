package telegram

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

type mergeTargetFixture struct {
	t                                      *testing.T
	ctx                                    context.Context
	pool                                   *pgxpool.Pool
	household, user, target, item, request string
	replySource                            string
	chat                                   int64
	update                                 telegramUpdate
}

func newMergeTargetFixture(t *testing.T, kind, reviewType string) *mergeTargetFixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := &mergeTargetFixture{t: t, ctx: ctx, pool: pool}
	stamp := testsupport.UniqueStamp()
	f.chat = stamp
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, sql, args...)
		must(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("merge target %d", stamp)).Scan(&f.household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("merge-target-%d@example.test", stamp)).Scan(&f.user))
	exec(`INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, f.household, f.user)
	exec(`INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, f.chat, f.household, f.user)
	var bank string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, f.household, fmt.Sprintf("bank-%d", stamp), []byte(fmt.Sprintf("bank-%d", stamp))).Scan(&bank))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,counterparty_name) VALUES($1,$2,'NEEDS_REVIEW',75000,now(),'Toko') RETURNING id`, f.household, kind).Scan(&f.target))
	exec(`INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'BANK_EMAIL')`, f.target, bank)
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,$3,'OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, f.household, f.target, reviewType).Scan(&f.item))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,$4,'OPEN',$5) RETURNING id`, f.household, f.item, f.target, reviewType, f.chat).Scan(&f.request))
	var dup string
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,counterparty_name,voided_at) VALUES($1,$2,'VOIDED',75000,now(),'Toko',now()) RETURNING id`, f.household, kind).Scan(&dup))
	exec(`INSERT INTO reconciliation_merge(household_id,source_transaction_id,target_transaction_id,status,created_by_user_id) VALUES($1,$2,$3,'ACTIVE',$4)`, f.household, dup, f.target, f.user)
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, f.household, fmt.Sprintf("reply-%d", stamp), []byte(fmt.Sprintf("reply-%d", stamp))).Scan(&f.replySource))
	f.update.Message.From.ID, f.update.Message.Chat.ID, f.update.Message.MessageID, f.update.Message.Text = f.chat, f.chat, 7, "Abaikan"
	return f
}

func (f *mergeTargetFixture) str(sql string, args ...any) string {
	f.t.Helper()
	var v string
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&v); err != nil {
		f.t.Fatal(err)
	}
	return v
}

func (f *mergeTargetFixture) untouched() {
	f.t.Helper()
	if got := f.str(`SELECT status FROM transaction WHERE id=$1`, f.target); got != "NEEDS_REVIEW" {
		f.t.Fatalf("target status = %s", got)
	}
	if got := f.str(`SELECT status FROM review_item WHERE id=$1`, f.item); got != "OPEN" {
		f.t.Fatalf("review item = %s", got)
	}
	if got := f.str(`SELECT status FROM review_request WHERE id=$1`, f.request); got != "OPEN" {
		f.t.Fatalf("review request = %s", got)
	}
	if got := f.str(`SELECT count(*)::text FROM audit_log WHERE entity_id=$1`, f.target); got != "0" {
		f.t.Fatalf("audit rows = %s", got)
	}
	if got := f.str(`SELECT count(*)::text FROM reconciliation_merge WHERE target_transaction_id=$1 AND status='ACTIVE'`, f.target); got != "1" {
		f.t.Fatalf("active merges = %s", got)
	}
}

func (f *mergeTargetFixture) refusedReply() {
	f.t.Helper()
	text := f.str(`SELECT COALESCE(string_agg(payload_json->>'text',' | '),'') FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1`, f.chat)
	if !strings.Contains(text, "tujuan penggabungan yang masih aktif") || !strings.Contains(text, "Pisahkan penggabungannya") {
		f.t.Fatalf("reply text = %q", text)
	}
	if got := f.str(`SELECT processing_status FROM source_event WHERE id=$1`, f.replySource); got != "IGNORED" {
		f.t.Fatalf("reply source event = %s", got)
	}
}
