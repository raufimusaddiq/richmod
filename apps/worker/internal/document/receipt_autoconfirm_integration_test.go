package document

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	var missingFacts []string
	var allowedActions []string
	if err := fixture.pool.QueryRow(ctx, `SELECT ARRAY(SELECT jsonb_array_elements_text(decision->'missingFacts')),ARRAY(SELECT jsonb_array_elements_text(decision->'allowedActions')) FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, fixture.householdID).Scan(&missingFacts, &allowedActions); err != nil {
		t.Fatal(err)
	}
	if len(missingFacts) != 1 || missingFacts[0] != "duplicate_relationship" || len(allowedActions) != 1 || allowedActions[0] != "IGNORE" {
		t.Fatalf("receipt duplicate review must expose only actions it can resolve: missing=%v actions=%v", missingFacts, allowedActions)
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
