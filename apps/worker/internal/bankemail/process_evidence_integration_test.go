package bankemail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type countingGateway struct {
	raw   string
	calls int
}

func (g *countingGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	g.calls++
	return gateway.ToolCall{Name: "emit_bank_transaction", Arguments: json.RawMessage(g.raw)}, gateway.Metadata{Model: "test"}, nil
}

func seedBankEmailProcess(t *testing.T, ctx context.Context, pool *pgxpool.Pool, stamp int64, receivedAt time.Time, body string) (household, listener, source string) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var user, account string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("bank email %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','x') RETURNING id`, fmt.Sprintf("bank-%d@test.invalid", stamp)).Scan(&user))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening','BANK','FULL_LEDGER') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,account_id,created_by_user_id) VALUES($1,'Synthetic Bank',$2,$3,$4) RETURNING id`, household, fmt.Sprintf("bank-%d@test.invalid", stamp), account, user).Scan(&listener))
	var knownAccount string
	must(pool.QueryRow(ctx, `INSERT INTO known_account(household_id,user_id,institution,display_name,match_hint,relationship) VALUES($1,$2,'Synthetic Bank','Tabungan','1234','OWN_ACCOUNT') RETURNING id`, household, user).Scan(&knownAccount))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,$3,$4,'RECEIVED') RETURNING id`, household, fmt.Sprintf("bank-%d", stamp), receivedAt, []byte(fmt.Sprintf("bank-%d", stamp))).Scan(&source))
	_, err := pool.Exec(ctx, `INSERT INTO bank_email_event(source_event_id,listener_id,observed_sender,message_id,subject,body) VALUES($1,$2,'notifikasi@bank.test',$3,'Notifikasi',$4)`, source, listener, fmt.Sprintf("message-%d", stamp), body)
	must(err)
	return
}

func TestBankEmailAmountMismatchRequiresReview(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wib := time.FixedZone("WIB", 7*60*60)
	household, _, source := seedBankEmailProcess(t, ctx, pool, testsupport.UniqueStamp(), time.Date(2026, 9, 7, 10, 1, 0, 0, wib), "Transfer Rp1.250.000,00 ke 1234")
	raw := `{"kind":"TRANSACTION","direction":"OUTGOING","channel":"TRANSFER","amount_idr":"125000000","transaction_at":"2026-09-07T10:00:00+07:00","merchant":null,"counterparty":"Tujuan 1234","reference":null,"description":null,"missing_fields":[],"confidence":0.9}`
	if err = NewProcessor(pool, NewExtractor(&countingGateway{raw: raw})).Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var status string
	var reviews int
	if err = pool.QueryRow(ctx, `SELECT status FROM transaction WHERE household_id=$1`, household).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE source_event_id=$1`, source).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" || reviews != 1 {
		t.Fatalf("transaction=%s reviews=%d", status, reviews)
	}
}

func TestBankEmailShiftedTimestampRequiresReview(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wib := time.FixedZone("WIB", 7*60*60)
	household, _, source := seedBankEmailProcess(t, ctx, pool, testsupport.UniqueStamp(), time.Date(2026, 9, 7, 23, 35, 0, 0, wib), "Transfer Rp1.250.000,00 ke 1234 23:30 WIB")
	raw := `{"kind":"TRANSACTION","direction":"OUTGOING","channel":"TRANSFER","amount_idr":"1250000","transaction_at":"2026-09-07T23:30:00Z","merchant":null,"counterparty":"Tujuan 1234","reference":null,"description":null,"missing_fields":[],"confidence":0.9}`
	if err = NewProcessor(pool, NewExtractor(&countingGateway{raw: raw})).Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM transaction WHERE household_id=$1`, household).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" {
		t.Fatalf("transaction=%s", status)
	}
}

func TestBankEmailSupportedFactsAutoConfirm(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wib := time.FixedZone("WIB", 7*60*60)
	household, _, source := seedBankEmailProcess(t, ctx, pool, testsupport.UniqueStamp(), time.Date(2026, 9, 7, 10, 1, 0, 0, wib), "Transfer Rp1.250.000,00 ke 1234")
	raw := `{"kind":"TRANSACTION","direction":"OUTGOING","channel":"TRANSFER","amount_idr":"1250000","transaction_at":"2026-09-07T10:00:00+07:00","merchant":null,"counterparty":"Tujuan 1234","reference":null,"description":null,"missing_fields":[],"confidence":0.9}`
	if err = NewProcessor(pool, NewExtractor(&countingGateway{raw: raw})).Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var status string
	var reviews int
	if err = pool.QueryRow(ctx, `SELECT status FROM transaction WHERE household_id=$1`, household).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE source_event_id=$1`, source).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || reviews != 0 {
		t.Fatalf("transaction=%s reviews=%d", status, reviews)
	}
}

func TestBankEmailReplayCallsModelOnce(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wib := time.FixedZone("WIB", 7*60*60)
	_, _, source := seedBankEmailProcess(t, ctx, pool, testsupport.UniqueStamp(), time.Date(2026, 9, 7, 10, 1, 0, 0, wib), "Bayar Rp54.000 di Warung")
	raw := `{"kind":"TRANSACTION","direction":"OUTGOING","channel":"QR","amount_idr":"54000","transaction_at":"2026-09-07T10:00:00+07:00","merchant":"Warung","counterparty":null,"reference":null,"description":null,"missing_fields":[],"confidence":0.9}`
	g := &countingGateway{raw: raw}
	processor := NewProcessor(pool, NewExtractor(g))
	if err = processor.Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var status string
	var reviews int
	if err = pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, source).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE source_event_id=$1`, source).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if err = processor.Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var replayStatus string
	var replayReviews, proposals int
	if err = pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, source).Scan(&replayStatus); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE source_event_id=$1`, source).Scan(&replayReviews); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM transaction_proposal WHERE source_event_id=$1`, source).Scan(&proposals); err != nil {
		t.Fatal(err)
	}
	if g.calls != 1 || status != replayStatus || reviews != replayReviews || proposals != 1 {
		t.Fatalf("calls=%d status=%s/%s reviews=%d/%d proposals=%d", g.calls, status, replayStatus, reviews, replayReviews, proposals)
	}
}

func TestBankEmailInactiveListenerIsIgnored(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wib := time.FixedZone("WIB", 7*60*60)
	_, listener, source := seedBankEmailProcess(t, ctx, pool, testsupport.UniqueStamp(), time.Date(2026, 9, 7, 10, 1, 0, 0, wib), "Transfer Rp1.250.000,00")
	if _, err = pool.Exec(ctx, `UPDATE bank_email_listener SET active=false WHERE id=$1`, listener); err != nil {
		t.Fatal(err)
	}
	g := &countingGateway{}
	if err = NewProcessor(pool, NewExtractor(g)).Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var status string
	var extractions int
	if err = pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, source).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM bank_email_extraction WHERE source_event_id=$1`, source).Scan(&extractions); err != nil {
		t.Fatal(err)
	}
	if g.calls != 0 || status != "IGNORED" || extractions != 0 {
		t.Fatalf("calls=%d status=%s extractions=%d", g.calls, status, extractions)
	}
}

func TestBankPersistStopsWhenCompletionReviewClosed(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wib := time.FixedZone("WIB", 7*60*60)
	household, listener, source := seedBankEmailProcess(t, ctx, pool, testsupport.UniqueStamp(), time.Date(2026, 9, 7, 10, 1, 0, 0, wib), "Transfer Rp54.000,00")
	if _, err = pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','test',$3,'{}'::jsonb,'VALID')`, source, listener, ToolSchemaVersion); err != nil {
		t.Fatal(err)
	}
	var reviewID string
	if err = pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','CANCELLED','{}'::jsonb) RETURNING id`, household, source).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}
	var account, householdFromListener string
	if err = pool.QueryRow(ctx, `SELECT COALESCE(account_id::text,''),household_id::text FROM bank_email_listener WHERE id=$1`, listener).Scan(&account, &householdFromListener); err != nil {
		t.Fatal(err)
	}
	amount := "54000"
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, wib)
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, TransactionAt: &at}
	result := PolicyResult{Type: "TRANSFER", Status: "CONFIRMED", AutoConfirm: true}
	if err = (&Processor{pool: pool}).persist(ctx, Listener{ID: listener, HouseholdID: household, AccountID: account}, source, extraction, result, reviewID); err != nil {
		t.Fatal(err)
	}
	var transactions int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, householdFromListener).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if transactions != 0 {
		t.Fatalf("a closed completion review must not record a transaction: %d", transactions)
	}
}
