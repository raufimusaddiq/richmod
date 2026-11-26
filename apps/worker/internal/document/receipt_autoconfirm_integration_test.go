package document

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

type receiptFixture struct {
	pool                     *pgxpool.Pool
	householdID, sourceID    string
	documentID               string
	categoryID, categorySlug string
}

func TestReceiptWithWeakSameAmountCandidateDoesNotBlock(t *testing.T) {
	t.Parallel()
	fixture := seedReceiptFixture(t, "Receipt weak duplicate")
	ctx := context.Background()
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
	if confirmed != 2 || needsReview != 0 {
		t.Fatalf("a weak query hit must not create duplicate review: confirmed=%d needs_review=%d", confirmed, needsReview)
	}
}

func TestReceiptNearSameAmountCandidateStillBlocks(t *testing.T) {
	t.Parallel()
	fixture := seedReceiptFixture(t, "Receipt near duplicate")
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,$2,'Another merchant',now())`, fixture.householdID, receiptTime().Add(-15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Currency: "IDR", CategorySlug: &slug, CategoryConfidence: .95, Confidence: .35}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var reason string
	if err := fixture.pool.QueryRow(ctx, `SELECT review_type FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, fixture.householdID).Scan(&reason); err != nil || reason != "POSSIBLE_DUPLICATE" {
		t.Fatalf("near same-amount candidate must remain in review: reason=%q err=%v", reason, err)
	}
}

func TestReceiptPlausibleCandidateBeyondTenWeakQueryHitsStillBlocks(t *testing.T) {
	t.Parallel()
	fixture := seedReceiptFixture(t, "Receipt candidate after weak hits")
	ctx := context.Background()
	for hour := 2; hour <= 11; hour++ {
		if _, err := fixture.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,$2,'Other merchant',now())`, fixture.householdID, receiptTime().Add(-time.Duration(hour)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	var merchantID string
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Indomaret') RETURNING id`, fixture.householdID).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,$2,$3,now())`, fixture.householdID, receiptTime().Add(-60*time.Hour), merchantID); err != nil {
		t.Fatal(err)
	}
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Currency: "IDR", CategorySlug: &slug, CategoryConfidence: .95, Confidence: .35}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", receiptValidation{TransactionAt: receiptTime(), DateKnown: true}, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var reviewType string
	if err := fixture.pool.QueryRow(ctx, `SELECT review_type FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, fixture.householdID).Scan(&reviewType); err != nil || reviewType != "POSSIBLE_DUPLICATE" {
		t.Fatalf("plausible duplicate beyond weak query hits must block: review=%q err=%v", reviewType, err)
	}
}

func TestReceiptMissingCategoryAmbiguousMerchantKeepsMerchantGap(t *testing.T) {
	t.Parallel()
	fixture := seedReceiptFixture(t, "Receipt ambiguous merchant category")
	ctx := context.Background()
	long := "1128 5635 31"
	aliasRaw := "JOHN DOE BANK MANDIRI 1128 5635 31"
	var shortMerchant, longMerchant string
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE SHORT') RETURNING id`, fixture.householdID).Scan(&shortMerchant); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE LONG') RETURNING id`, fixture.householdID).Scan(&longMerchant); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id) VALUES($1,'JOHN DOE',$2),($1,$3,$4)`, fixture.householdID, shortMerchant, aliasRaw, longMerchant); err != nil {
		t.Fatal(err)
	}
	value := receiptExtraction{Merchant: long, Total: "57500", Currency: "IDR", Confidence: .95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true, ArithmeticAvailable: true, ArithmeticOK: true}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var reviewType string
	var categoryGap, merchantGap, knownMerchant bool
	if err := fixture.pool.QueryRow(ctx, `SELECT review_type,decision->'missingFacts' ? 'category',decision->'missingFacts' ? 'merchant',decision->'knownFacts' ? 'merchant' FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, fixture.householdID).Scan(&reviewType, &categoryGap, &merchantGap, &knownMerchant); err != nil {
		t.Fatal(err)
	}
	if reviewType != "AMBIGUOUS_CATEGORY" || !categoryGap || !merchantGap || knownMerchant {
		t.Fatalf("review type=%s categoryGap=%t merchantGap=%t knownMerchant=%t", reviewType, categoryGap, merchantGap, knownMerchant)
	}
}

func TestReceiptDuplicateAmbiguousMerchantKeepsMerchantGap(t *testing.T) {
	t.Parallel()
	fixture := seedReceiptFixture(t, "Receipt ambiguous merchant duplicate")
	ctx := context.Background()
	long := "1128 5635 31"
	aliasRaw := "JOHN DOE BANK MANDIRI 1128 5635 31"
	var shortMerchant, longMerchant string
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE SHORT') RETURNING id`, fixture.householdID).Scan(&shortMerchant); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE LONG') RETURNING id`, fixture.householdID).Scan(&longMerchant); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id) VALUES($1,'JOHN DOE',$2),($1,$3,$4)`, fixture.householdID, shortMerchant, aliasRaw, longMerchant); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,$2,'Another merchant',now())`, fixture.householdID, receiptTime().Add(-15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: long, Total: "57500", Currency: "IDR", CategorySlug: &slug, CategoryConfidence: .95, Confidence: .35}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var reviewType string
	var duplicateGap, merchantGap, knownMerchant bool
	if err := fixture.pool.QueryRow(ctx, `SELECT review_type,decision->'missingFacts' ? 'duplicate_relationship',decision->'missingFacts' ? 'merchant',decision->'knownFacts' ? 'merchant' FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, fixture.householdID).Scan(&reviewType, &duplicateGap, &merchantGap, &knownMerchant); err != nil {
		t.Fatal(err)
	}
	if reviewType != "POSSIBLE_DUPLICATE" || !duplicateGap || !merchantGap || knownMerchant {
		t.Fatalf("review type=%s duplicateGap=%t merchantGap=%t knownMerchant=%t", reviewType, duplicateGap, merchantGap, knownMerchant)
	}
}

func TestReceiptReviewPersistsDecisionWithoutTelegramRecipient(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	stamp := testsupport.UniqueStamp()
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
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Recipient','test-only-hash') RETURNING id`, fmt.Sprintf("receipt-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role,active,deactivated_at) VALUES($1,$2,'MEMBER',false,now())`, fixture.householdID, userID); err != nil {
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

func TestReceiptR2StrongMatchLinksEvidenceWithoutDuplicate(t *testing.T) {
	t.Parallel()
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
	value.Confidence = .35
	value.Tax = ptr("5000")
	validation.ArithmeticOK = false
	stamp := testsupport.UniqueStamp()
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
	var arithmeticOK bool
	if err := fixture.pool.QueryRow(ctx, `SELECT (metadata_json->>'arithmetic_ok')::boolean FROM transaction_proposal WHERE source_event_id=$1`, secondSource).Scan(&arithmeticOK); err != nil || arithmeticOK {
		t.Fatalf("linked receipt must retain its arithmetic quality signal: ok=%t err=%v", arithmeticOK, err)
	}
}

func TestClearNewReceiptAutoConfirmsWithoutReview(t *testing.T) {
	t.Parallel()
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

func TestReceiptArithmeticMismatchConfirmsPrintedTotalWithoutReview(t *testing.T) {
	t.Parallel()
	fixture := seedReceiptFixture(t, "Receipt mismatch")
	ctx := context.Background()
	slug := fixture.categorySlug
	printedAt := receiptTime().Format(time.RFC3339)
	value := receiptExtraction{Merchant: "Indomaret", TransactionAt: &printedAt, Total: "57500", Subtotal: ptr("50000"), Tax: ptr("5000"), Currency: "IDR", CategorySlug: &slug, CategoryConfidence: .95, Confidence: .95}
	validation, err := validateReceipt(value, receiptTime())
	if err != nil || !validation.ArithmeticAvailable || validation.ArithmeticOK || !validation.DateKnown {
		t.Fatalf("arithmetic mismatch must preserve observed facts: %+v %v", validation, err)
	}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var amount, category string
	var transactionAt time.Time
	var arithmeticOK bool
	var reviews int
	if err := fixture.pool.QueryRow(ctx, `SELECT t.amount::text,t.category_id::text,t.transaction_at,(p.metadata_json->>'arithmetic_ok')::boolean,
		(SELECT count(*) FROM review_item WHERE household_id=$1)
		FROM transaction t JOIN transaction_proposal p ON p.household_id=t.household_id
		WHERE t.household_id=$1 AND t.status='CONFIRMED' AND p.proposal_status='ACCEPTED'`, fixture.householdID).Scan(&amount, &category, &transactionAt, &arithmeticOK, &reviews); err != nil {
		t.Fatal(err)
	}
	if amount != "57500" || category != fixture.categoryID || !transactionAt.Equal(receiptTime()) || arithmeticOK || reviews != 0 {
		t.Fatalf("printed facts must confirm without review; amount=%s category=%s date=%s arithmetic_ok=%t reviews=%d", amount, category, transactionAt, arithmeticOK, reviews)
	}
}

func TestReceiptWithUnresolvedCategoryStaysInReview(t *testing.T) {
	t.Parallel()
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

func TestReceiptWithoutPrintedDateStaysInReview(t *testing.T) {
	t.Parallel()
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

func TestReceiptAutoConfirmKillSwitchGatesConfirmation(t *testing.T) {
	t.Parallel()
	fixture := seedReceiptFixture(t, "Receipt kill switch")
	ctx := context.Background()
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Currency: "IDR", CategorySlug: &slug, CategoryConfidence: 0.95, Confidence: 0.95}
	value.Subtotal, value.Tax = ptr("50000"), ptr("5000")
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true, ArithmeticAvailable: true, ArithmeticOK: false}
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

func TestAutoConfirmedReceiptQueuesABindableNoticeToTheUploadChat(t *testing.T) {
	t.Parallel()
	fixture := seedReceiptFixture(t, "Receipt notice")
	ctx := context.Background()
	chat := testsupport.UniqueStamp() % 9_000_000_000
	if _, err := fixture.pool.Exec(ctx, `UPDATE source_event SET telegram_chat_id=$2,telegram_message_id=555 WHERE id=$1`, fixture.sourceID, chat); err != nil {
		t.Fatal(err)
	}
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Subtotal: ptr("50000"), Tax: ptr("7500"), Currency: "IDR", CategorySlug: &slug, CategoryConfidence: 0.95, Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true, ArithmeticAvailable: true, ArithmeticOK: true}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
		t.Fatal(err)
	}
	var jobs int
	var gotChat, replyTo int64
	var text, bind string
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) OVER(),(payload_json->>'chat_id')::bigint,(payload_json->>'reply_to_message_id')::bigint,payload_json->>'text',payload_json->>'bind_document_id'
		FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'bind_document_id'=$1`, fixture.documentID).Scan(&jobs, &gotChat, &replyTo, &text, &bind); err != nil {
		t.Fatalf("no bound notice queued: %v", err)
	}
	if jobs != 1 || gotChat != chat || replyTo != 555 || bind != fixture.documentID {
		t.Fatalf("notice jobs=%d chat=%d reply=%d bind=%s", jobs, gotChat, replyTo, bind)
	}
	if !strings.HasPrefix(text, "Struk Indomaret Rp57.500 sudah tersimpan.") {
		t.Fatalf("notice text = %q", text)
	}
}

func TestLinkedReceiptQueuesALinkedNoticeAndStillCreatesNoDuplicate(t *testing.T) {
	t.Parallel()
	fixture := seedReceiptFixture(t, "Receipt link notice")
	ctx := context.Background()
	slug := fixture.categorySlug
	value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Subtotal: ptr("50000"), Tax: ptr("7500"), Currency: "IDR", CategorySlug: &slug, CategoryConfidence: 0.95, Confidence: 0.95}
	validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true, ArithmeticAvailable: true, ArithmeticOK: true}
	category := []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, category); err != nil {
		t.Fatal(err)
	}
	stamp := testsupport.UniqueStamp()
	chat := stamp % 9_000_000_000
	var secondSource, attachmentID, secondDocument string
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_chat_id,telegram_message_id) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSING',$4,556) RETURNING id`, fixture.householdID, fmt.Sprintf("link-notice-%d", stamp), []byte(fmt.Sprintf("link-notice-%d", stamp)), chat).Scan(&secondSource); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',3,1,1,$3) RETURNING id`, fixture.householdID, []byte(fmt.Sprintf("link-hash-%d", stamp)), fmt.Sprintf("test/link-%d.jpg", stamp)).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status,document_type) VALUES($1,$2,$3,'RECEIVED','RECEIPT') RETURNING id`, fixture.householdID, secondSource, attachmentID).Scan(&secondDocument); err != nil {
		t.Fatal(err)
	}
	if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, secondDocument, fixture.householdID, secondSource, value, "test-model", validation, category); err != nil {
		t.Fatal(err)
	}
	var text string
	if err := fixture.pool.QueryRow(ctx, `SELECT payload_json->>'text' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'bind_document_id'=$1`, secondDocument).Scan(&text); err != nil {
		t.Fatalf("no linked notice queued: %v", err)
	}
	if !strings.HasPrefix(text, "Struk Indomaret Rp57.500 dilampirkan ke transaksi yang sudah ada.") {
		t.Fatalf("linked notice = %q", text)
	}
	var transactions int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&transactions); err != nil || transactions != 1 {
		t.Fatalf("transactions=%d err=%v, want the notice to add no ledger row", transactions, err)
	}
}

func TestLowConfidenceReceiptParksInReviewInsteadOfAutoConfirming(t *testing.T) {
	t.Parallel()
	for name, confidence := range map[string][2]float64{"extraction": {.84, .95}, "category": {.95, .84}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := seedReceiptFixture(t, "Receipt low confidence "+name)
			ctx := context.Background()
			slug := fixture.categorySlug
			value := receiptExtraction{Merchant: "Indomaret", Total: "57500", Currency: "IDR", CategorySlug: &slug, Confidence: confidence[0], CategoryConfidence: confidence[1]}
			validation := receiptValidation{TransactionAt: receiptTime(), DateKnown: true}
			if err := (&Processor{pool: fixture.pool}).persistReceipt(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, value, "test-model", validation, []categoryOption{{ID: fixture.categoryID, Slug: fixture.categorySlug}}); err != nil {
				t.Fatal(err)
			}
			var confirmed, needsReview int
			if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='CONFIRMED'),count(*) FILTER (WHERE status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&confirmed, &needsReview); err != nil {
				t.Fatal(err)
			}
			if confirmed != 0 || needsReview != 1 {
				t.Fatalf("low confidence receipt must park in review: confirmed=%d needs_review=%d", confirmed, needsReview)
			}
		})
	}
}
