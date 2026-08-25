package gmail

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/jago"
)

func TestJagoTransferPersistencePolicy(t *testing.T) {
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
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Jago policy %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("jago-policy-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, stamp%8_000_000_000+1_000_000_000, householdID, userID); err != nil {
		t.Fatal(err)
	}
	processor := &Processor{pool: pool}
	sequence := 0
	createSource := func() string {
		sequence++
		var id string
		hash := []byte(fmt.Sprintf("source-%d-%d", stamp, sequence))
		if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("message-%d-%d", stamp, sequence), hash).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	base := jago.Event{Family: jago.FamilyOutgoingTransfer, FinancialEffect: jago.EffectNeedsReview, Amount: "1000000", Currency: "IDR", TransactionAt: time.Now(), TransactionChannel: "TRANSFER", Reference: fmt.Sprintf("ref-%d", stamp)}

	unknownSource := createSource()
	unknown := base
	unknown.ToName = "BCA ****9999"
	if err = processor.persistEvent(ctx, householdID, unknownSource, unknown); err != nil {
		t.Fatal(err)
	}
	assertJagoState(t, pool, unknownSource, "UNCLASSIFIED", "NEEDS_REVIEW", "UNCLASSIFIED", "NEEDS_REVIEW", "NEEDS_REVIEW", 1)

	for _, known := range []struct {
		hint, relationship, wantType, wantStatus, wantProposal, wantProposalStatus, wantSource string
		reviews                                                                                int
	}{{"1234", "OWN_ACCOUNT", "TRANSFER", "CONFIRMED", "TRANSFER", "ACCEPTED", "PROCESSED", 0}, {"5678", "HOUSEHOLD_ACCOUNT", "TRANSFER", "CONFIRMED", "TRANSFER", "ACCEPTED", "PROCESSED", 0}, {"2468", "INVESTMENT_ACCOUNT", "UNCLASSIFIED", "VOIDED", "UNCLASSIFIED", "REJECTED", "IGNORED", 0}} {
		if _, err = pool.Exec(ctx, `INSERT INTO known_account(household_id,institution,display_name,match_hint,relationship) VALUES($1,'BCA',$2,$3,$4)`, householdID, "BCA ****"+known.hint, known.hint, known.relationship); err != nil {
			t.Fatal(err)
		}
		source := createSource()
		event := base
		event.ToName = "BCA ****" + known.hint
		if err = processor.persistEvent(ctx, householdID, source, event); err != nil {
			t.Fatal(err)
		}
		assertJagoState(t, pool, source, known.wantType, known.wantStatus, known.wantProposal, known.wantProposalStatus, known.wantSource, known.reviews)
	}

	expenseSource := createSource()
	expense := jago.Event{Family: jago.FamilyMerchantPayment, FinancialEffect: jago.EffectExpenseCandidate, Amount: "75000", Currency: "IDR", TransactionAt: time.Now(), Merchant: "TOKO UJI", TransactionChannel: "MERCHANT"}
	if err = processor.persistEvent(ctx, householdID, expenseSource, expense); err != nil {
		t.Fatal(err)
	}
	assertJagoState(t, pool, expenseSource, "EXPENSE", "NEEDS_REVIEW", "EXPENSE", "NEEDS_REVIEW", "NEEDS_REVIEW", 1)
	var confirmedExpense string
	if err = pool.QueryRow(ctx, `SELECT COALESCE(sum(amount) FILTER(WHERE type='EXPENSE' AND status='CONFIRMED'),0)::text FROM transaction WHERE household_id=$1`, householdID).Scan(&confirmedExpense); err != nil || confirmedExpense != "0" {
		t.Fatalf("confirmed expense=%s err=%v", confirmedExpense, err)
	}
}

func assertJagoState(t *testing.T, pool *pgxpool.Pool, sourceID, wantType, wantStatus, wantProposal, wantProposalStatus, wantSource string, wantReviews int) {
	t.Helper()
	var transactionType, status, proposalType, proposalStatus, sourceStatus string
	var reviews int
	err := pool.QueryRow(context.Background(), `SELECT t.type,t.status,p.proposed_type,p.proposal_status,s.processing_status,(SELECT count(*) FROM review_request r WHERE r.transaction_id=t.id) FROM source_event s JOIN transaction_proposal p ON p.source_event_id=s.id JOIN transaction_evidence e ON (e.metadata_json->>'proposal_id')::uuid=p.id JOIN transaction t ON t.id=e.transaction_id WHERE s.id=$1`, sourceID).Scan(&transactionType, &status, &proposalType, &proposalStatus, &sourceStatus, &reviews)
	if err != nil {
		t.Fatal(err)
	}
	if transactionType != wantType || status != wantStatus || proposalType != wantProposal || proposalStatus != wantProposalStatus || sourceStatus != wantSource || reviews != wantReviews {
		t.Fatalf("got %s/%s %s/%s source=%s reviews=%d", transactionType, status, proposalType, proposalStatus, sourceStatus, reviews)
	}
}
