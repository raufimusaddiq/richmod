package document

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

type receiptCategoryVerifier struct {
	calls   int
	answer  judgment.Answer
	request judgment.Request
}

func (v *receiptCategoryVerifier) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	v.calls++
	v.request = request
	return judgment.Result{Model: "stub-jev", Answers: map[string]judgment.Answer{"category": v.answer}}, nil
}

func receiptCategoryAnswer(choice string, top float64) judgment.Answer {
	distribution := map[string]float64{}
	for _, key := range []string{"food-and-drink", "groceries", "OTHER_OR_UNCLEAR"} {
		if key == choice {
			distribution[key] = top
		} else {
			distribution[key] = (1 - top) / 2
		}
	}
	return judgment.Answer{Type: "choice", Choice: choice, Distribution: distribution, Confidence: 0.9, HasConfidence: true}
}

type receiptFixture struct {
	pool                     *pgxpool.Pool
	householdID, sourceID    string
	documentID               string
	categoryID, categorySlug string
}

// Hermes review on PR #127: a same-amount transaction within the window whose
// merchant text differs scores below the old 0.70 filter. It must still block the
// auto-confirm, otherwise the receipt becomes a second ledger entry for the same
// real event (PRD §17: no unresolved duplicate ambiguity).
func TestReceiptWithWeakSameAmountCandidateStaysInReview(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt weak duplicate")
	ctx := context.Background()
	// 12 hours earlier, same amount, different merchant text: 0.45+0.20 = 0.65.
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,$2,'Belanja lain',now())`, fixture.householdID, receiptTime().Add(-12*time.Hour)); err != nil {
		t.Fatal(err)
	}
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Subtotal: ptr("50000"), Tax: ptr("7500"), Currency: "IDR", CategorySlug: &slug, CategoryConfidence: 0.95, Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true, ArithmeticAvailable: true, ArithmeticOK: true}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var confirmed, needsReview int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='CONFIRMED'),count(*) FILTER (WHERE status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&confirmed, &needsReview); err != nil {
		t.Fatal(err)
	}
	if confirmed != 1 || needsReview != 1 {
		t.Fatalf("a plausible duplicate must not be auto-confirmed: confirmed=%d needs_review=%d", confirmed, needsReview)
	}
	var reviewType string
	if err := fixture.pool.QueryRow(ctx, `SELECT review_type FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, fixture.householdID).Scan(&reviewType); err != nil {
		t.Fatal(err)
	}
	if reviewType != "POSSIBLE_DUPLICATE" {
		t.Fatalf("the review must say why: got %s", reviewType)
	}
	var amount, transactionAt, transactionType, action string
	if err := fixture.pool.QueryRow(ctx, `SELECT decision->'knownFacts'->>'amount_idr',decision->'knownFacts'->>'transaction_at',decision->'knownFacts'->>'type',decision->'allowedActions'->>0 FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, fixture.householdID).Scan(&amount, &transactionAt, &transactionType, &action); err != nil {
		t.Fatal(err)
	}
	if amount != "57500" || transactionAt != receiptTime().Format(time.RFC3339) || transactionType != "EXPENSE" || action != "MERGE_EXISTING" {
		t.Fatalf("receipt duplicate decision lost known facts or bounded candidate action: amount=%q time=%q type=%q action=%q", amount, transactionAt, transactionType, action)
	}
}

func TestReceiptReviewPersistsDecisionWithoutTelegramRecipient(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt review without Telegram")
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, `DELETE FROM telegram_identity WHERE household_id=$1`, fixture.householdID); err != nil {
		t.Fatal(err)
	}
	value := receiptExtraction{Merchant: "Warung Bu Tini", Total: "25000", Currency: "IDR", Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var reason, amount, transactionAt, transactionType string
	if err := fixture.pool.QueryRow(ctx, `SELECT review_type,decision->'knownFacts'->>'amount_idr',decision->'knownFacts'->>'transaction_at',decision->'knownFacts'->>'type' FROM review_item WHERE household_id=$1 AND status='OPEN'`, fixture.householdID).Scan(&reason, &amount, &transactionAt, &transactionType); err != nil {
		t.Fatal(err)
	}
	if reason != "AMBIGUOUS_CATEGORY" || amount != "25000" || transactionType != "EXPENSE" || transactionAt != receiptTime().Format(time.RFC3339) {
		t.Fatalf("receipt ReviewDecision incomplete without Telegram: reason=%q amount=%q time=%q type=%q", reason, amount, transactionAt, transactionType)
	}
	var requests int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM review_request r JOIN transaction t ON t.id=r.transaction_id WHERE t.household_id=$1`, fixture.householdID).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatalf("no Telegram recipient should create no Telegram review request, got %d", requests)
	}
}

func TestReceiptResidualReviewTypesSatisfyDatabaseConstraints(t *testing.T) {
	for _, test := range []struct {
		name          string
		categoryKnown bool
		dateKnown     bool
		wantType      string
		wantFacts     []string
	}{
		{name: "category only", categoryKnown: false, dateKnown: true, wantType: "AMBIGUOUS_CATEGORY", wantFacts: []string{"category"}},
		{name: "category and date", categoryKnown: false, dateKnown: false, wantType: "TRANSACTION_FACTS_MISSING", wantFacts: []string{"category", "transaction_at"}},
		{name: "date only", categoryKnown: true, dateKnown: false, wantType: "MISSING_TRANSACTION_DATE", wantFacts: []string{"transaction_at"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := seedReceiptFixture(t, "Receipt residual type "+test.name)
			ctx := context.Background()
			value := receiptExtraction{Merchant: "Warung Bu Tini", Total: "25000", Currency: "IDR", Confidence: 0.95}
			validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: test.dateKnown}
			categories := []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}
			if test.categoryKnown {
				slug := fixture.categorySlug
				value.CategorySlug, value.CategoryConfidence = &slug, 0.95
			}
			if err := (&Processor{pool: fixture.pool, receiptAutoConfirmOff: true}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, categories); err != nil {
				t.Fatal(err)
			}
			var reviewType string
			var missingFacts []string
			if err := fixture.pool.QueryRow(ctx, `SELECT review_type,ARRAY(SELECT jsonb_array_elements_text(decision->'missingFacts')) FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, fixture.householdID).Scan(&reviewType, &missingFacts); err != nil {
				t.Fatal(err)
			}
			if reviewType != test.wantType || fmt.Sprint(missingFacts) != fmt.Sprint(test.wantFacts) {
				t.Fatalf("review type/facts=%s/%v, want %s/%v", reviewType, missingFacts, test.wantType, test.wantFacts)
			}
			var knownDate, timeSource string
			if err := fixture.pool.QueryRow(ctx, `SELECT COALESCE(decision->'knownFacts'->>'transaction_at',''),COALESCE(decision->'knownFacts'->>'transaction_time_source','') FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, fixture.householdID).Scan(&knownDate, &timeSource); err != nil {
				t.Fatal(err)
			}
			if test.dateKnown && knownDate == "" || !test.dateKnown && (knownDate != "" || timeSource != "RECEIVED_AT_FALLBACK") {
				t.Fatalf("known date/source=%q/%q; fallback time must remain provenance", knownDate, timeSource)
			}
		})
	}
}

func seedReceiptFixture(t *testing.T, label string) receiptFixture {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	stamp := time.Now().UnixNano()
	fixture := receiptFixture{pool: pool}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("%s %d", label, stamp)).Scan(&fixture.householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Belanja Rumah','groceries') RETURNING id`, fixture.householdID).Scan(&fixture.categoryID); err != nil {
		t.Fatal(err)
	}
	fixture.categorySlug = "groceries"
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSING') RETURNING id`, fixture.householdID, fmt.Sprintf("receipt-autoconfirm-%d", stamp), []byte(fmt.Sprintf("receipt-autoconfirm-%d", stamp))).Scan(&fixture.sourceID); err != nil {
		t.Fatal(err)
	}
	var attachmentID string
	if err := pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',3,1,1,$3) RETURNING id`, fixture.householdID, []byte(fmt.Sprintf("receipt-hash-%d", stamp)), fmt.Sprintf("test/receipt-%d.jpg", stamp)).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status,document_type) VALUES($1,$2,$3,'RECEIVED','RECEIPT') RETURNING id`, fixture.householdID, fixture.sourceID, attachmentID).Scan(&fixture.documentID); err != nil {
		t.Fatal(err)
	}
	// Receipt reviews are delivered through the Telegram review request, so the
	// household needs a bound identity for the review path to be exercised.
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Recipient','test-only-hash') RETURNING id`, fmt.Sprintf("receipt-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, stamp%1000000000, fixture.householdID, userID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func receiptTime() time.Time {
	return time.Date(2026, 9, 23, 13, 45, 0, 0, time.FixedZone("WIB", 7*3600))
}

// PRD §26 R2: when exactly one existing transaction is a strong match, the
// receipt links to it as evidence instead of creating a second ledger row. The
// second upload gets its own source event/document, because reprocessing one
// document is already guarded by the persisted document status.
func TestReceiptR2StrongMatchLinksEvidenceWithoutDuplicate(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt strong match")
	ctx := context.Background()
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Subtotal: ptr("50000"), Tax: ptr("7500"), Currency: "IDR", CategorySlug: &slug, CategoryConfidence: 0.95, Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true, ArithmeticAvailable: true, ArithmeticOK: true}
	category := []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}
	processor := &Processor{pool: fixture.pool}
	if err := processor.persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, category); err != nil {
		t.Fatal(err)
	}
	// Same receipt evidence arrives again as a new document: the transaction the
	// first upload wrote is the single strong match, so the second upload links to
	// it rather than adding a ledger row for the same real event.
	stamp := time.Now().UnixNano()
	var secondSource, attachmentID, secondDocument string
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSING') RETURNING id`, fixture.householdID, fmt.Sprintf("receipt-repeat-%d", stamp), []byte(fmt.Sprintf("receipt-repeat-%d", stamp))).Scan(&secondSource); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',3,1,1,$3) RETURNING id`, fixture.householdID, []byte(fmt.Sprintf("receipt-repeat-hash-%d", stamp)), fmt.Sprintf("test/receipt-repeat-%d.jpg", stamp)).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status,document_type) VALUES($1,$2,$3,'RECEIVED','RECEIPT') RETURNING id`, fixture.householdID, secondSource, attachmentID).Scan(&secondDocument); err != nil {
		t.Fatal(err)
	}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, secondDocument, fixture.householdID, secondSource, value, "test-model", validation, category); err != nil {
		t.Fatal(err)
	}
	var transactions, evidence int
	if err := fixture.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1),(SELECT count(*) FROM transaction_evidence e JOIN transaction t ON t.id=e.transaction_id WHERE t.household_id=$1 AND e.evidence_type='RECEIPT_IMAGE')`, fixture.householdID).Scan(&transactions, &evidence); err != nil {
		t.Fatal(err)
	}
	if transactions != 1 {
		t.Fatalf("a strong match must not create a second transaction, got %d", transactions)
	}
	if evidence != 2 {
		t.Fatalf("both receipts must link as evidence on the matched transaction, got %d", evidence)
	}
}

// R1: a clear new receipt with valid arithmetic, a known date, and a resolved
// category must reach canonical state without a review (PRD §10, example D).
func TestClearNewReceiptAutoConfirmsWithoutReview(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt auto confirm")
	ctx := context.Background()
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Subtotal: ptr("50000"), Tax: ptr("7500"), Currency: "IDR", CategorySlug: &slug, CategoryConfidence: 0.95, Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true, ArithmeticAvailable: true, ArithmeticOK: true}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var status, purpose string
	var amount string
	if err := fixture.pool.QueryRow(ctx, `SELECT status,amount::text,type FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&status, &amount, &purpose); err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || amount != "57500" || purpose != "EXPENSE" {
		t.Fatalf("clear receipt must auto-confirm: status=%s amount=%s type=%s", status, amount, purpose)
	}
	var reviews int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1`, fixture.householdID).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if reviews != 0 {
		t.Fatalf("a clear receipt must not create a review, got %d", reviews)
	}
}

// R4/R5: a receipt that cannot resolve the category must still go to review and
// must not be confirmed on a guess.
func TestReceiptWithUnresolvedCategoryStaysInReview(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt review")
	ctx := context.Background()
	value := receiptExtraction{Merchant: "Warung Bu Tini", Total: "25000", Currency: "IDR", Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := fixture.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" {
		t.Fatalf("an undecided category must not auto-confirm, status=%s", status)
	}
	var reviews int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, fixture.householdID).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if reviews != 1 {
		t.Fatalf("an undecided category must open one review, got %d", reviews)
	}
}

// IR-05 R-category-rescue: with a printed date, no duplicate candidate, and the
// category as the only bounded residual, exactly one Jev category rescue makes a
// decisive receipt confirm directly. Vision stays the only generative call.
func TestReceiptCategoryRescueConfirmsOnDecisiveAnswer(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt category rescue")
	ctx := context.Background()
	var foodCategoryID string
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food & Drink','food-and-drink') RETURNING id`, fixture.householdID).Scan(&foodCategoryID); err != nil {
		t.Fatal(err)
	}
	verifier := &receiptCategoryVerifier{answer: receiptCategoryAnswer(fixture.categorySlug, 0.95)}
	value := receiptExtraction{Merchant: "Warung Bu Tini", Total: "25000", Currency: "IDR", Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true}
	processor := &Processor{pool: fixture.pool, verifier: verifier}
	if err := processor.persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}, {ID: foodCategoryID, Slug: "food-and-drink"}}); err != nil {
		t.Fatal(err)
	}
	if verifier.calls != 1 {
		t.Fatalf("one residual category must cost one bounded request, got %d", verifier.calls)
	}
	if len(verifier.request.Questions) != 1 {
		t.Fatalf("rescue must ask exactly the category question, got %v", verifier.request.Questions)
	}
	var status, categoryID string
	var reviews int
	if err := fixture.pool.QueryRow(ctx, `SELECT status,COALESCE(category_id::text,''),(SELECT count(*) FROM review_item WHERE household_id=$1) FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&status, &categoryID, &reviews); err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || categoryID != fixture.categoryID || reviews != 0 {
		t.Fatalf("decisive rescue must confirm: status=%s category=%s reviews=%d", status, categoryID, reviews)
	}
	var rescued bool
	if err := fixture.pool.QueryRow(ctx, `SELECT (metadata_json->>'category_rescued')::boolean FROM transaction_proposal WHERE household_id=$1`, fixture.householdID).Scan(&rescued); err != nil {
		t.Fatal(err)
	}
	if !rescued {
		t.Fatal("the confirmed proposal must record that a bounded rescue supplied the category")
	}
	var decisions int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM judgment_decision WHERE source_event_id=$1 AND task='RECEIPT_CATEGORY' AND outcome='AUTO_CONFIRM'`, fixture.sourceID).Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if decisions != 1 {
		t.Fatalf("one bounded provenance row required, got %d", decisions)
	}
}

// IR-05 R-category-undecided: an undecided or failure answer must not confirm
// and must keep a category-only review, never a guessed category.
func TestReceiptCategoryUndecidedStaysInCategoryReview(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt category undecided")
	ctx := context.Background()
	var foodCategoryID string
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food & Drink','food-and-drink') RETURNING id`, fixture.householdID).Scan(&foodCategoryID); err != nil {
		t.Fatal(err)
	}
	verifier := &receiptCategoryVerifier{answer: receiptCategoryAnswer("OTHER_OR_UNCLEAR", 0.40)}
	value := receiptExtraction{Merchant: "Warung Bu Tini", Total: "25000", Currency: "IDR", Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true}
	processor := &Processor{pool: fixture.pool, verifier: verifier}
	if err := processor.persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}, {ID: foodCategoryID, Slug: "food-and-drink"}}); err != nil {
		t.Fatal(err)
	}
	var status, reviewType, categoryID string
	if err := fixture.pool.QueryRow(ctx, `SELECT t.status,COALESCE(t.category_id::text,''),r.review_type FROM transaction t JOIN review_item r ON r.transaction_id=t.id WHERE t.household_id=$1`, fixture.householdID).Scan(&status, &categoryID, &reviewType); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" || reviewType != "AMBIGUOUS_CATEGORY" || categoryID != "" {
		t.Fatalf("undecided rescue must keep a category-only review: status=%s review=%s category=%s", status, reviewType, categoryID)
	}
	var decisions int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM judgment_decision WHERE source_event_id=$1 AND task='RECEIPT_CATEGORY' AND outcome='REVIEW'`, fixture.sourceID).Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if decisions != 1 {
		t.Fatalf("undecided rescue provenance row count=%d", decisions)
	}
}

func TestReceiptCategoryProviderFailureKeepsReview(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt category provider failure")
	ctx := context.Background()
	verifier := &stubReceiptFailureVerifier{}
	value := receiptExtraction{Merchant: "Warung Bu Tini", Total: "25000", Currency: "IDR", Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true}
	processor := &Processor{pool: fixture.pool, verifier: verifier}
	if err := processor.persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}, {ID: "second-category", Slug: "food-and-drink"}}); err != nil {
		t.Fatal(err)
	}
	var status, outcome string
	if err := fixture.pool.QueryRow(ctx, `SELECT t.status,d.outcome FROM transaction t JOIN judgment_decision d ON d.source_event_id=$1 AND d.task='RECEIPT_CATEGORY' WHERE t.household_id=$2`, fixture.sourceID, fixture.householdID).Scan(&status, &outcome); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" || outcome != "PROVIDER_FAILURE" {
		t.Fatalf("provider failure must preserve review: status=%s outcome=%s", status, outcome)
	}
}

type stubReceiptFailureVerifier struct{}

func (*stubReceiptFailureVerifier) Evaluate(context.Context, string, judgment.Request) (judgment.Result, error) {
	return judgment.Result{}, fmt.Errorf("test judgment provider failure")
}

// IR-05 R-date-missing: a date that is genuinely absent must never be rescued by
// Jev. The rescue must not even run, and the review names only the date.
func TestReceiptMissingDateIsNeverRescued(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt dateless rescue")
	ctx := context.Background()
	var foodCategoryID string
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food & Drink','food-and-drink') RETURNING id`, fixture.householdID).Scan(&foodCategoryID); err != nil {
		t.Fatal(err)
	}
	verifier := &receiptCategoryVerifier{answer: receiptCategoryAnswer(fixture.categorySlug, 0.95)}
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Warung Bu Tini", Total: "25000", Currency: "IDR", CategorySlug: &slug, CategoryConfidence: 0.95, Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime()}
	processor := &Processor{pool: fixture.pool, verifier: verifier}
	if err := processor.persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}, {ID: foodCategoryID, Slug: "food-and-drink"}}); err != nil {
		t.Fatal(err)
	}
	if verifier.calls != 0 {
		t.Fatalf("a missing date must not trigger a category rescue, calls=%d", verifier.calls)
	}
	var status, reviewType string
	if err := fixture.pool.QueryRow(ctx, `SELECT t.status,r.review_type FROM transaction t JOIN review_item r ON r.transaction_id=t.id WHERE t.household_id=$1`, fixture.householdID).Scan(&status, &reviewType); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" || reviewType != "MISSING_TRANSACTION_DATE" {
		t.Fatalf("missing date review: status=%s review=%s", status, reviewType)
	}
}

// Hermes review on PR #127, PRD §18.4: upload time is not the receipt's
// transaction time, so a receipt without a printed date keeps asking for it
// instead of confirming against the moment it arrived.
func TestReceiptWithoutPrintedDateStaysInReview(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt dateless")
	ctx := context.Background()
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Subtotal: ptr("50000"), Tax: ptr("7500"), Currency: "IDR", CategorySlug: &slug, CategoryConfidence: 0.95, Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), ArithmeticAvailable: true, ArithmeticOK: true}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := fixture.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" {
		t.Fatalf("a receipt with no printed date must not auto-confirm, status=%s", status)
	}
}

// PRD §33: the receipt auto-confirm must be independently disable-able, so a
// bad rollout can roll back this source without touching bank or screenshot
// auto-confirm. With the switch off, the same clear receipt parks a review.
func TestReceiptAutoConfirmKillSwitchGatesConfirmation(t *testing.T) {
	fixture := seedReceiptFixture(t, "Receipt kill switch")
	ctx := context.Background()
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Currency: "IDR", CategorySlug: &slug, CategoryConfidence: 0.95, Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true}
	processor := &Processor{pool: fixture.pool}
	processor.SetReceiptAutoConfirm(false)
	if err := processor.persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := fixture.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" {
		t.Fatalf("the receipt switch off must park a review, status=%s", status)
	}
}
