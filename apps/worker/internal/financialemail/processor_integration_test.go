package financialemail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type integrationGateway struct {
	raw   string
	calls int
}

func (g *integrationGateway) NativeToolCall(_ context.Context, _ string, _ string, _ any, _ []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	g.calls++
	return gateway.ToolCall{Name: "emit_financial_email_observations", Arguments: json.RawMessage(g.raw)}, gateway.Metadata{Model: "test"}, nil
}

func TestFinancialEmailProviderReferenceIsIdempotent(t *testing.T) {
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
	h, _, account, wealth, source := seedFinancialEmail(t, ctx, pool, time.Now().UnixNano(), "ACTIVE")
	_ = h
	raw := `{"observations":[{"kind":"CASH_MOVEMENT","movement_type":"CONTRIBUTION","amount_idr":"3000000","occurred_at":"2026-09-07T10:00:00+07:00","funding_account_hint":"Jago Autodebit","provider_account_hint":"","provider_reference":"reference-1","account_hint":null,"value_idr":null,"observed_date":null,"confidence":0.99}]}`
	p := NewProcessor(pool, &integrationGateway{raw: raw})
	if err := p.Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var count int
	var typ, purpose, status, observedStatus, gotAccount, gotWealth string
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, h).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("transactions=%d", count)
	}
	if err := pool.QueryRow(ctx, `SELECT type,purpose,status,account_id::text,related_wealth_account_id::text FROM transaction WHERE household_id=$1`, h).Scan(&typ, &purpose, &status, &gotAccount, &gotWealth); err != nil {
		t.Fatal(err)
	}
	if typ != "TRANSFER" || purpose != "INVESTMENT_CONTRIBUTION" || status != "CONFIRMED" || gotAccount != account || gotWealth != wealth {
		t.Fatalf("transaction=%s/%s/%s account=%s wealth=%s", typ, purpose, status, gotAccount, gotWealth)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM financial_email_observation WHERE source_event_id=$1`, source).Scan(&observedStatus); err != nil {
		t.Fatal(err)
	}
	if observedStatus != "APPLIED" {
		t.Fatalf("observation=%s", observedStatus)
	}

	var financialSource string
	if err := pool.QueryRow(ctx, `SELECT financial_source_id::text FROM financial_email_event WHERE source_event_id=$1`, source).Scan(&financialSource); err != nil {
		t.Fatal(err)
	}
	retry := seedFinancialEmailFor(t, ctx, pool, h, financialSource)
	if err := p.Process(ctx, Payload{SourceEventID: retry}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, h).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reference retry transactions=%d", count)
	}
}

func TestFinancialEmailMultipleObservationsKeepIndependentReviewBindings(t *testing.T) {
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
	h, _, account, _, source := seedFinancialEmail(t, ctx, pool, time.Now().UnixNano(), "ACTIVE")
	for _, amount := range []string{"100", "200"} {
		if _, err := pool.Exec(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,transaction_at,purpose) VALUES($1,$2,'UNCLASSIFIED','NEEDS_REVIEW',$3,'2026-09-07T10:00:00+07:00','GENERAL')`, h, account, amount); err != nil {
			t.Fatal(err)
		}
	}
	raw := `{"observations":[{"kind":"CASH_MOVEMENT","movement_type":"CONTRIBUTION","amount_idr":"100","occurred_at":"2026-09-07T10:00:00+07:00","funding_account_hint":"Jago Autodebit","provider_account_hint":"","provider_reference":null,"account_hint":null,"value_idr":null,"observed_date":null,"confidence":0.99},{"kind":"CASH_MOVEMENT","movement_type":"CONTRIBUTION","amount_idr":"200","occurred_at":"2026-09-07T10:00:00+07:00","funding_account_hint":"Jago Autodebit","provider_account_hint":"","provider_reference":null,"account_hint":null,"value_idr":null,"observed_date":null,"confidence":0.99}]}`
	if err := NewProcessor(pool, &integrationGateway{raw: raw}).Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var observations, cases, reviews, missingBinding int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_email_observation WHERE source_event_id=$1`, source).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transfer_reconciliation_case WHERE financial_email_observation_id IN (SELECT id FROM financial_email_observation WHERE source_event_id=$1)`, source).Scan(&cases); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE financial_email_observation_id IS NULL) FROM review_item WHERE financial_email_observation_id IN (SELECT id FROM financial_email_observation WHERE source_event_id=$1)`, source).Scan(&reviews, &missingBinding); err != nil {
		t.Fatal(err)
	}
	if observations != 2 || cases != 2 || reviews != 2 || missingBinding != 0 {
		t.Fatalf("observations=%d cases=%d reviews=%d missing=%d", observations, cases, reviews, missingBinding)
	}
}

func TestFinancialEmailZeroObservationExtractionIsIdempotent(t *testing.T) {
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
	h, _, _, _, source := seedFinancialEmail(t, ctx, pool, time.Now().UnixNano(), "ACTIVE")
	g := &integrationGateway{raw: `{"observations":[]}`}
	p := NewProcessor(pool, g)
	if err := p.Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	if err := p.Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var observations, transactions, wealth, reviews, count int
	var extractionStatus string
	if err := pool.QueryRow(ctx, `SELECT extraction_status,observation_count FROM financial_email_event WHERE source_event_id=$1`, source).Scan(&extractionStatus, &count); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_email_observation WHERE source_event_id=$1`, source).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, h).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wealth_observation WHERE household_id=$1`, h).Scan(&wealth); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1`, h).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if g.calls != 1 || extractionStatus != "SUCCEEDED" || count != 0 || observations != 0 || transactions != 0 || wealth != 0 || reviews != 0 {
		t.Fatalf("calls=%d extraction=%s/%d observations=%d transactions=%d wealth=%d reviews=%d", g.calls, extractionStatus, count, observations, transactions, wealth, reviews)
	}
}

func TestFinancialEmailMixedObservationReprocessingIsIdempotent(t *testing.T) {
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
	h, _, account, wealth, source := seedFinancialEmail(t, ctx, pool, time.Now().UnixNano(), "ACTIVE")
	raw := `{"observations":[{"kind":"CASH_MOVEMENT","movement_type":"CONTRIBUTION","amount_idr":"3000000","occurred_at":"2026-09-08T10:00:00+07:00","funding_account_hint":"Unresolved funding","provider_account_hint":"","provider_reference":"mixed-1","account_hint":null,"value_idr":null,"observed_date":null,"confidence":0.99},{"kind":"WEALTH_VALUE","movement_type":null,"amount_idr":null,"occurred_at":null,"funding_account_hint":null,"provider_account_hint":null,"provider_reference":null,"account_hint":"","value_idr":"42700000","observed_date":"2026-09-08","confidence":0.99}]}`
	p := NewProcessor(pool, &integrationGateway{raw: raw})
	if err := p.Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var cashObservation string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM financial_email_observation WHERE source_event_id=$1 AND ordinal=0 AND status='REVIEW'`, source).Scan(&cashObservation); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE financial_email_observation SET resolved_account_id=$2,resolved_wealth_account_id=$3,status='PENDING' WHERE id=$1`, cashObservation, account, wealth); err != nil {
		t.Fatal(err)
	}
	if err := p.Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var wealthChildren, wealthReviews, transactions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wealth_observation WHERE financial_email_observation_id=(SELECT id FROM financial_email_observation WHERE source_event_id=$1 AND ordinal=1)`, source).Scan(&wealthChildren); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE financial_email_observation_id=(SELECT id FROM financial_email_observation WHERE source_event_id=$1 AND ordinal=1) AND review_type='WEALTH_OBSERVATION_CONFIRMATION'`, source).Scan(&wealthReviews); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, h).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if wealthChildren != 1 || wealthReviews != 1 || transactions != 1 {
		t.Fatalf("wealth children=%d reviews=%d transactions=%d", wealthChildren, wealthReviews, transactions)
	}
}

func TestFinancialEmailProviderReferenceSemanticConflictsRequireReview(t *testing.T) {
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
	h, _, _, wealthA, source := seedFinancialEmail(t, ctx, pool, time.Now().UnixNano(), "ACTIVE")
	var financialSource string
	if err := pool.QueryRow(ctx, `SELECT financial_source_id::text FROM financial_email_event WHERE source_event_id=$1`, source).Scan(&financialSource); err != nil {
		t.Fatal(err)
	}
	contribution := func(ref string) string {
		return `{"observations":[{"kind":"CASH_MOVEMENT","movement_type":"CONTRIBUTION","amount_idr":"3000000","occurred_at":"2026-09-08T10:00:00+07:00","funding_account_hint":"Jago Autodebit","provider_account_hint":"","provider_reference":"` + ref + `","account_hint":null,"value_idr":null,"observed_date":null,"confidence":0.99}]}`
	}
	if err := NewProcessor(pool, &integrationGateway{raw: contribution("semantic-purpose")}).Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	purposeConflict := seedFinancialEmailFor(t, ctx, pool, h, financialSource)
	assetPurchase := `{"observations":[{"kind":"CASH_MOVEMENT","movement_type":"ASSET_PURCHASE","amount_idr":"3000000","occurred_at":"2026-09-08T10:00:00+07:00","funding_account_hint":"Jago Autodebit","provider_account_hint":"","provider_reference":"semantic-purpose","account_hint":null,"value_idr":null,"observed_date":null,"confidence":0.99}]}`
	if err := NewProcessor(pool, &integrationGateway{raw: assetPurchase}).Process(ctx, Payload{SourceEventID: purposeConflict}); err != nil {
		t.Fatal(err)
	}
	var wealthB string
	if err := pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'Investasi kedua','Provider','ASSET','BROKERAGE','INVESTMENT') RETURNING id`, h).Scan(&wealthB); err != nil {
		t.Fatal(err)
	}
	wealthSource := seedFinancialEmailFor(t, ctx, pool, h, financialSource)
	if err := NewProcessor(pool, &integrationGateway{raw: contribution("semantic-wealth")}).Process(ctx, Payload{SourceEventID: wealthSource}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE financial_email_source SET default_wealth_account_id=$2 WHERE id=$1`, financialSource, wealthB); err != nil {
		t.Fatal(err)
	}
	wealthConflict := seedFinancialEmailFor(t, ctx, pool, h, financialSource)
	if err := NewProcessor(pool, &integrationGateway{raw: contribution("semantic-wealth")}).Process(ctx, Payload{SourceEventID: wealthConflict}); err != nil {
		t.Fatal(err)
	}
	var transactions, conflicts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, h).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE review_type='CONFLICTING_EVIDENCE' AND financial_email_observation_id IN (SELECT id FROM financial_email_observation WHERE source_event_id IN ($1,$2))`, purposeConflict, wealthConflict).Scan(&conflicts); err != nil {
		t.Fatal(err)
	}
	if transactions != 1 || conflicts != 2 || wealthA == wealthB {
		t.Fatalf("transactions=%d conflicts=%d wealthA=%s wealthB=%s", transactions, conflicts, wealthA, wealthB)
	}
}

func TestFinancialEmailReusesCompatibleBankTransaction(t *testing.T) {
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
	h, _, account, wealth, source := seedFinancialEmail(t, ctx, pool, time.Now().UnixNano(), "ACTIVE")
	var transaction string
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,transaction_at,purpose,related_wealth_account_id,confirmed_at) VALUES($1,$2,'TRANSFER','CONFIRMED',3000000,'2026-09-08T10:00:00+07:00','INVESTMENT_CONTRIBUTION',$3,now()) RETURNING id`, h, account, wealth).Scan(&transaction); err != nil {
		t.Fatal(err)
	}
	raw := `{"observations":[{"kind":"CASH_MOVEMENT","movement_type":"CONTRIBUTION","amount_idr":"3000000","occurred_at":"2026-09-08T10:00:00+07:00","funding_account_hint":"Jago Autodebit","provider_account_hint":"","provider_reference":"bank-match-1","account_hint":null,"value_idr":null,"observed_date":null,"confidence":0.99}]}`
	if err := NewProcessor(pool, &integrationGateway{raw: raw}).Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var transactions, evidence int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, h).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND source_event_id=$2 AND evidence_type='FINANCIAL_EMAIL'`, transaction, source).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if transactions != 1 || evidence != 1 {
		t.Fatalf("transactions=%d provider evidence=%d", transactions, evidence)
	}
}

func TestFinancialEmailDisabledQueuedSourceStopsWithoutRetry(t *testing.T) {
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
	h, _, _, _, source := seedFinancialEmail(t, ctx, pool, time.Now().UnixNano(), "ACTIVE")
	if _, err := pool.Exec(ctx, `UPDATE financial_email_source SET status='DISABLED' WHERE id=(SELECT financial_source_id FROM financial_email_event WHERE source_event_id=$1)`, source); err != nil {
		t.Fatal(err)
	}
	g := &integrationGateway{raw: `{"observations":[]}`}
	if err := NewProcessor(pool, g).Process(ctx, Payload{SourceEventID: source}); err != nil {
		t.Fatal(err)
	}
	var status string
	var observations, transactions int
	if err := pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, source).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_email_observation WHERE source_event_id=$1`, source).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, h).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if status != "IGNORED" || g.calls != 0 || observations != 0 || transactions != 0 {
		t.Fatalf("status=%s calls=%d observations=%d transactions=%d", status, g.calls, observations, transactions)
	}
}

func seedFinancialEmail(t *testing.T, ctx context.Context, pool *pgxpool.Pool, stamp int64, status string) (household, user, account, wealth, source string) {
	t.Helper()
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("financial email %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','x') RETURNING id`, fmt.Sprintf("financial-%d@test.invalid", stamp)).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank Jago','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'Investasi','Provider','ASSET','BROKERAGE','INVESTMENT') RETURNING id`, household).Scan(&wealth); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,account_id,alias,normalized_alias,source) VALUES($1,'ACCOUNT',$2,'Jago','jago','SYSTEM')`, household, account)
	if err != nil {
		t.Fatal(err)
	}
	var financialSource string
	if err = pool.QueryRow(ctx, `INSERT INTO financial_email_source(household_id,provider_name,sender_address,capabilities,default_wealth_account_id,status,created_by_user_id) VALUES($1,'Example Provider',$2,ARRAY['CASH_MOVEMENT','WEALTH_VALUE'],$3,$4,$5) RETURNING id`, household, fmt.Sprintf("provider-%d@test.invalid", stamp), wealth, status, user).Scan(&financialSource); err != nil {
		t.Fatal(err)
	}
	source = seedFinancialEmailFor(t, ctx, pool, household, financialSource)
	return
}

func seedFinancialEmailFor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, household, financialSource string) string {
	t.Helper()
	var source string
	id := fmt.Sprintf("financial-event-%d", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'FINANCIAL_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, id, []byte(id)).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO financial_email_event(source_event_id,financial_source_id,observed_sender,message_id,subject,body) VALUES($1,$2,'provider@test.invalid',$3,'test','body')`, source, financialSource, id); err != nil {
		t.Fatal(err)
	}
	return source
}
