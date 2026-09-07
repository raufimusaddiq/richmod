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

type integrationGateway struct{ raw string }

func (g integrationGateway) NativeToolCall(_ context.Context, _ string, _ string, _ any, _ []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
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
	p := NewProcessor(pool, integrationGateway{raw: raw})
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
	if err := NewProcessor(pool, integrationGateway{raw: raw}).Process(ctx, Payload{SourceEventID: source}); err != nil {
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
