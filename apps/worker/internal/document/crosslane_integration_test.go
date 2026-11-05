package document

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// One purchase seen by several lanes stays one canonical transaction with many
// evidence rows (AGENTS.md). These pin the document side of every lane order:
// the earlier lane's transaction may still be NEEDS_REVIEW.

// seedLaneTransaction inserts the transaction another lane already wrote for
// the event, with that lane's evidence row.
func seedLaneTransaction(t *testing.T, fixture receiptFixture, status, evidenceType, merchant string, at time.Time) (string, string) {
	t.Helper()
	ctx := context.Background()
	var merchantID *string
	if merchant != "" {
		var id string
		if err := fixture.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id::text`, fixture.householdID, merchant).Scan(&id); err != nil {
			t.Fatal(err)
		}
		merchantID = &id
	}
	var source, transaction string
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',gen_random_uuid()::text,now(),decode(md5(random()::text),'hex'),$2) RETURNING id::text`, fixture.householdID, map[string]string{"CONFIRMED": "PROCESSED", "NEEDS_REVIEW": "NEEDS_REVIEW"}[status]).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,confirmed_at) VALUES($1,'EXPENSE',$2,57500,'IDR',$3,$4,CASE WHEN $2='CONFIRMED' THEN now() END) RETURNING id::text`, fixture.householdID, status, at, merchantID).Scan(&transaction); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,$3,.95)`, transaction, source, evidenceType); err != nil {
		t.Fatal(err)
	}
	if status == "NEEDS_REVIEW" {
		if _, err := fixture.pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","knownFacts":{},"missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb)`, fixture.householdID, transaction); err != nil {
			t.Fatal(err)
		}
	}
	return transaction, source
}

func TestReceiptAfterAnotherLaneLinksInsteadOfDuplicating(t *testing.T) {
	for _, tt := range []struct {
		name, status, evidence, merchant string
		wantSource                       string
	}{
		{"bank first, still in review", "NEEDS_REVIEW", "BANK_EMAIL", "QRIS ALFAMART SUDIRMAN", "NEEDS_REVIEW"},
		{"bank first, confirmed", "CONFIRMED", "BANK_EMAIL", "QRIS ALFAMART SUDIRMAN", "PROCESSED"},
		{"screenshot first, still in review", "NEEDS_REVIEW", "TRANSACTION_SCREENSHOT", "Alfamart", "NEEDS_REVIEW"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := seedReceiptFixture(t, "Cross-lane receipt")
			ctx := context.Background()
			existing, _ := seedLaneTransaction(t, fixture, tt.status, tt.evidence, tt.merchant, receiptTime().Add(-20*time.Minute))
			slug := fixture.categorySlug
			value := receiptExtraction{Merchant: "Alfamart", Total: "57500", Currency: "IDR", CategorySlug: &slug, CategoryConfidence: .95, Confidence: .95, Items: []receiptItem{{Name: "Roti", Amount: "57500"}}}
			if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", receiptValidation{TransactionAt: receiptTime(), DateKnown: true}, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
				t.Fatal(err)
			}
			var transactions, evidence int
			var sourceStatus string
			if err := fixture.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1),(SELECT count(*) FROM transaction_evidence WHERE transaction_id=$2),(SELECT processing_status FROM source_event WHERE id=$3)`, fixture.householdID, existing, fixture.sourceID).Scan(&transactions, &evidence, &sourceStatus); err != nil {
				t.Fatal(err)
			}
			if transactions != 1 || evidence != 2 {
				t.Fatalf("one event must stay one transaction with two evidence rows: transactions=%d evidence=%d", transactions, evidence)
			}
			if sourceStatus != tt.wantSource {
				t.Fatalf("receipt source=%s, want %s (the matched transaction decides)", sourceStatus, tt.wantSource)
			}
		})
	}
}

func TestReceiptAfterAmbiguousLaneTransactionAsksAndNamesTheCandidate(t *testing.T) {
	fixture := seedReceiptFixture(t, "Cross-lane ambiguous receipt")
	ctx := context.Background()
	// No merchant on the bank row and 30 minutes apart: plausible, not provable.
	existing, _ := seedLaneTransaction(t, fixture, "NEEDS_REVIEW", "BANK_EMAIL", "", receiptTime().Add(-30*time.Minute))
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Alfamart", Total: "57500", Currency: "IDR", CategorySlug: &slug, CategoryConfidence: .95, Confidence: .95}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", receiptValidation{TransactionAt: receiptTime(), DateKnown: true}, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var reviewType string
	var named bool
	if err := fixture.pool.QueryRow(ctx, `SELECT ri.review_type,ri.decision->'knownFacts'->'prior_match_transaction_ids' ? $2 FROM review_item ri JOIN transaction t ON t.id=ri.transaction_id WHERE ri.household_id=$1 AND ri.status='OPEN' AND t.id<>$2::uuid`, fixture.householdID, existing).Scan(&reviewType, &named); err != nil {
		t.Fatal(err)
	}
	if reviewType != "POSSIBLE_DUPLICATE" || !named {
		t.Fatalf("ambiguous cross-lane match must ask and name the pending candidate: type=%s named=%v", reviewType, named)
	}
}

func TestScreenshotMatcherSeesTransactionsStillInReview(t *testing.T) {
	fixture := seedReceiptFixture(t, "Cross-lane screenshot")
	ctx := context.Background()
	// Receipt (or bank) first: its transaction is still awaiting review.
	existing, _ := seedLaneTransaction(t, fixture, "NEEDS_REVIEW", "RECEIPT_IMAGE", "Alfamart", receiptTime().Add(-10*time.Minute))
	row := validatedScreenshotRow{Value: screenshotRow{Amount: ptr("57500"), Merchant: "Alfamart"}, Type: "EXPENSE", TransactionAt: receiptTime(), DateKnown: true}
	matches, err := (&Processor{pool: fixture.pool}).findScreenshotMatches(ctx, fixture.householdID, row)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(matches, func(candidate matchCandidate) bool {
		return candidate.ID == existing && candidate.Status == "NEEDS_REVIEW"
	}) {
		t.Fatalf("a screenshot must see the pending receipt transaction as a candidate: %+v", matches)
	}
	if ids := pendingCandidateIDs(matches); !slices.Contains(ids, existing) {
		t.Fatalf("pending candidate must be named for merge: %v", ids)
	}
}

// A bank email writing the same purchase while the receipt is being decided
// must not produce two transactions: the receipt write re-checks under the
// shared ledger lock and re-decides, linking to the bank transaction.
func TestConcurrentBankAndReceiptStayOneTransaction(t *testing.T) {
	fixture := seedReceiptFixture(t, "Cross-lane race")
	ctx := context.Background()
	var bankSource, merchantID string
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',gen_random_uuid()::text,now(),decode(md5(random()::text),'hex'),'PROCESSED') RETURNING id::text`, fixture.householdID).Scan(&bankSource); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'QRIS ALFAMART') RETURNING id::text`, fixture.householdID).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	// The bank lane holds the ledger lock and has written, but not committed.
	bank, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bank.Rollback(ctx)
	if err := reviewdomain.LockLedgerEvent(ctx, bank, fixture.householdID, "57500"); err != nil {
		t.Fatal(err)
	}
	var bankTransaction string
	if err := bank.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,'IDR',$2,$3,now()) RETURNING id::text`, fixture.householdID, receiptTime().Add(-5*time.Minute), merchantID).Scan(&bankTransaction); err != nil {
		t.Fatal(err)
	}
	if _, err := bank.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,'BANK_EMAIL',.95)`, bankTransaction, bankSource); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		slug := fixture.categorySlug
		value := receiptExtraction{Merchant: "Alfamart", Total: "57500", Currency: "IDR", CategorySlug: &slug, CategoryConfidence: .95, Confidence: .95}
		done <- (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", receiptValidation{TransactionAt: receiptTime(), DateKnown: true}, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}})
	}()
	time.Sleep(300 * time.Millisecond) // the receipt has decided "new" and waits on the lock
	if err := bank.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var transactions, evidence int
	if err := fixture.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1),(SELECT count(*) FROM transaction_evidence WHERE transaction_id=$2)`, fixture.householdID, bankTransaction).Scan(&transactions, &evidence); err != nil {
		t.Fatal(err)
	}
	if transactions != 1 || evidence != 2 {
		t.Fatalf("concurrent lanes duplicated the event: transactions=%d evidence=%d", transactions, evidence)
	}
}

// The locked re-check runs inside the writing transaction: a screenshot write
// must complete on a single-connection pool instead of waiting forever for a
// second connection.
func TestScreenshotLedgerRecheckNeedsNoSecondConnection(t *testing.T) {
	fixture := seedReceiptFixture(t, "Cross-lane single connection")
	config, err := pgxpool.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	row := validatedScreenshotRow{Value: screenshotRow{Amount: ptr("12500"), Merchant: "Warung", Confidence: .4}, Type: "EXPENSE", TransactionAt: receiptTime(), DateKnown: true}
	if err := (&Processor{pool: pool, rowAutoConfirmOff: true}).persistScreenshot(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, "TRANSACTION_HISTORY_SCREENSHOT", screenshotExtraction{Confidence: .95}, "test-model", rowChoiceProvenance{}, []validatedScreenshotRow{row}); err != nil {
		t.Fatalf("single-connection screenshot write: %v", err)
	}
}
