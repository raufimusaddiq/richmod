package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

type screenshotFixture struct {
	pool                              *pgxpool.Pool
	householdID, sourceID, documentID string
	categoryID, secondCategoryID      string
	categorySlug, secondCategorySlug  string
	chatID                            int64
}

func seedScreenshotFixture(t *testing.T, label string) screenshotFixture {
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
	fixture := screenshotFixture{pool: pool, categorySlug: "food-and-drink", secondCategorySlug: "groceries"}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("%s %d", label, stamp)).Scan(&fixture.householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','food-and-drink') RETURNING id`, fixture.householdID).Scan(&fixture.categoryID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Belanja Rumah','groceries') RETURNING id`, fixture.householdID).Scan(&fixture.secondCategoryID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSING') RETURNING id`, fixture.householdID, fmt.Sprintf("screenshot-rows-%d", stamp), []byte(fmt.Sprintf("screenshot-rows-%d", stamp))).Scan(&fixture.sourceID); err != nil {
		t.Fatal(err)
	}
	var attachmentID string
	if err := pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',3,1,1,$3) RETURNING id`, fixture.householdID, []byte(fmt.Sprintf("screenshot-hash-%d", stamp)), fmt.Sprintf("test/screenshot-%d.jpg", stamp)).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status,document_type) VALUES($1,$2,$3,'RECEIVED','TRANSACTION_HISTORY_SCREENSHOT') RETURNING id`, fixture.householdID, fixture.sourceID, attachmentID).Scan(&fixture.documentID); err != nil {
		t.Fatal(err)
	}
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Recipient','test-only-hash') RETURNING id`, fmt.Sprintf("screenshot-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	fixture.chatID = stamp % 1000000000
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, fixture.householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, fixture.chatID, fixture.householdID, userID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func screenshotRowTime() time.Time {
	return time.Date(2026, 9, 23, 15, 30, 0, 0, time.FixedZone("WIB", 7*3600))
}

func TestScreenshotRowWithUnresolvedCandidatesStaysInReview(t *testing.T) {
	t.Parallel()
	fixture := seedScreenshotFixture(t, "Screenshot ambiguous row")
	ctx := context.Background()
	categoryID := fixture.categoryID
	row := screenshotDataRow("EXPENSE", "54000", "Indomaret")
	row.CategoryID, row.CategoryDecided = &categoryID, true
	seeded := seedScreenshotCandidates(t, fixture, 2)
	matches, err := (&Processor{pool: fixture.pool}).findScreenshotMatches(ctx, fixture.householdID, row)
	if err != nil || len(matches) != 2 {
		t.Fatalf("candidates=%v err=%v", matches, err)
	}
	row.Candidates = matches
	fixture.persist(t, []validatedScreenshotRow{row})
	var confirmed, needsReview int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='CONFIRMED'),count(*) FILTER (WHERE status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1 AND NOT id=ANY($2::uuid[])`, fixture.householdID, seeded).Scan(&confirmed, &needsReview); err != nil {
		t.Fatal(err)
	}
	if confirmed != 0 || needsReview != 1 {
		t.Fatalf("an ambiguous row must not be recorded as a second transaction: confirmed=%d needs_review=%d", confirmed, needsReview)
	}
}

func seedScreenshotCandidates(t *testing.T, f screenshotFixture, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := range n {
		var id string
		if err := f.pool.QueryRow(context.Background(), `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',54000,'IDR',$2,now()) RETURNING id`, f.householdID, screenshotRowTime().Add(-time.Duration(10*(i+1))*time.Minute)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestScreenshotWriteRetriesWhenANonChosenCandidateVanished(t *testing.T) {
	t.Parallel()
	fixture := seedScreenshotFixture(t, "Screenshot vanished candidate")
	ctx := context.Background()
	categoryID := fixture.categoryID
	row := screenshotDataRow("EXPENSE", "54000", "Indomaret")
	row.CategoryID, row.CategoryDecided = &categoryID, true
	seeded := seedScreenshotCandidates(t, fixture, 2)
	matches, err := (&Processor{pool: fixture.pool}).findScreenshotMatches(ctx, fixture.householdID, row)
	if err != nil || len(matches) != 2 || row.Matched != nil {
		t.Fatalf("candidates=%v err=%v", matches, err)
	}
	row.Candidates = matches
	if _, err := fixture.pool.Exec(ctx, `UPDATE transaction SET status='VOIDED',voided_at=now(),confirmed_at=NULL WHERE id=$1`, seeded[0]); err != nil {
		t.Fatal(err)
	}
	err = (&Processor{pool: fixture.pool}).persistScreenshot(ctx, fixture.documentID, fixture.householdID, fixture.sourceID, "TRANSACTION_HISTORY_SCREENSHOT", screenshotExtraction{Confidence: .95}, "test-model", []validatedScreenshotRow{row})
	if !errors.Is(err, reviewdomain.ErrLedgerRaced) {
		t.Fatalf("a vanished non-chosen candidate must force a retry: %v", err)
	}
	var written int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND NOT id=ANY($2::uuid[])`, fixture.householdID, seeded).Scan(&written); err != nil {
		t.Fatal(err)
	}
	if written != 0 {
		t.Fatalf("a raced write must not record anything: %d", written)
	}
}

func screenshotDataRow(rowType, amount, merchant string) validatedScreenshotRow {
	return validatedScreenshotRow{Value: screenshotRow{Amount: &amount, Currency: "IDR", Merchant: merchant, Confidence: .95, CategoryConfidence: .95}, Type: rowType, TransactionAt: screenshotRowTime(), DateKnown: true}
}

func (f screenshotFixture) persist(t *testing.T, rows []validatedScreenshotRow) {
	t.Helper()
	if err := (&Processor{pool: f.pool}).persistScreenshot(context.Background(), f.documentID, f.householdID, f.sourceID, "TRANSACTION_HISTORY_SCREENSHOT", screenshotExtraction{Confidence: .95}, "test-model", rows); err != nil {
		t.Fatal(err)
	}
}

func TestScreenshotMissingAmountReviewFinalizesWithoutSentinel(t *testing.T) {
	t.Parallel()
	f := seedScreenshotFixture(t, "Screenshot absent amount")
	ctx := context.Background()
	category := f.categoryID
	row := screenshotDataRow("EXPENSE", "54000", "Warung")
	row.Value.Amount = nil
	row.Value.Direction, row.Value.CategorySlug = "OUT", &f.categorySlug
	row.CategoryID, row.CategoryDecided = &category, true
	f.persist(t, []validatedScreenshotRow{row})
	var proposalID, reviewID, userID string
	var amount *string
	if err := f.pool.QueryRow(ctx, `SELECT p.id::text,ri.id::text,p.amount::text,(SELECT user_id::text FROM telegram_identity WHERE household_id=p.household_id LIMIT 1) FROM transaction_proposal p JOIN review_item ri ON ri.proposal_id=p.id WHERE p.source_event_id=$1`, f.sourceID).Scan(&proposalID, &reviewID, &amount, &userID); err != nil {
		t.Fatal(err)
	}
	if amount != nil {
		t.Fatalf("missing amount was stored as %q", *amount)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("no canonical transaction may exist before the user supplies an amount")
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	zero := "0"
	if _, err := reviewdomain.ResolveMissingAmountProposal(ctx, tx, reviewdomain.MissingAmountCommand{HouseholdID: f.householdID, UserID: userID, ReviewItemID: reviewID, ProposalID: proposalID, SourceEventID: f.sourceID, ActorType: "USER", AmountIDR: &zero}); err != reviewdomain.ErrMissingAmountReviewInvalid {
		t.Fatalf("zero must be rejected: %v", err)
	}
	amountIDR := "54000"
	result, err := reviewdomain.ResolveMissingAmountProposal(ctx, tx, reviewdomain.MissingAmountCommand{HouseholdID: f.householdID, UserID: userID, ReviewItemID: reviewID, ProposalID: proposalID, SourceEventID: f.sourceID, ActorType: "USER", AmountIDR: &amountIDR})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var status, recorded string
	if err := f.pool.QueryRow(ctx, `SELECT status,amount::text FROM transaction WHERE id=$1`, result.TransactionID).Scan(&status, &recorded); err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || recorded != amountIDR {
		t.Fatalf("transaction status=%s amount=%s", status, recorded)
	}
	retry, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Rollback(ctx)
	if _, err := reviewdomain.ResolveMissingAmountProposal(ctx, retry, reviewdomain.MissingAmountCommand{HouseholdID: f.householdID, UserID: userID, ReviewItemID: reviewID, ProposalID: proposalID, SourceEventID: f.sourceID, ActorType: "USER", AmountIDR: &amountIDR}); err != reviewdomain.ErrMissingAmountReviewInvalid {
		t.Fatalf("review must not finalize twice: %v", err)
	}
}

func TestScreenshotMissingAmountDuplicateAmbiguousMerchantKeepsMerchantGap(t *testing.T) {
	t.Parallel()
	f := seedScreenshotFixture(t, "Screenshot ambiguous merchant amount")
	ctx := context.Background()
	long := "1128 5635 31"
	aliasRaw := "JOHN DOE BANK MANDIRI 1128 5635 31"
	var shortMerchant, longMerchant string
	if err := f.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE SHORT') RETURNING id`, f.householdID).Scan(&shortMerchant); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE LONG') RETURNING id`, f.householdID).Scan(&longMerchant); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id) VALUES($1,'JOHN DOE',$2),($1,$3,$4)`, f.householdID, shortMerchant, aliasRaw, longMerchant); err != nil {
		t.Fatal(err)
	}
	category := f.categoryID
	row := screenshotDataRow("EXPENSE", "54000", long)
	row.Value.Amount = nil
	row.Value.Direction, row.Value.CategorySlug = "OUT", &f.categorySlug
	row.CategoryID, row.CategoryDecided = &category, true
	f.persist(t, []validatedScreenshotRow{row})
	var proposalID, reviewID, userID string
	if err := f.pool.QueryRow(ctx, `SELECT p.id::text,ri.id::text,(SELECT user_id::text FROM telegram_identity WHERE household_id=p.household_id LIMIT 1) FROM transaction_proposal p JOIN review_item ri ON ri.proposal_id=p.id WHERE p.source_event_id=$1`, f.sourceID).Scan(&proposalID, &reviewID, &userID); err != nil {
		t.Fatal(err)
	}
	var prior string
	if err := f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',54000,'IDR',now(),now()) RETURNING id`, f.householdID).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(decision,'{knownFacts,prior_match_transaction_ids}',jsonb_build_array($2::text)) WHERE id=$1`, reviewID, prior); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	amount := "54000"
	result, err := reviewdomain.ResolveMissingAmountProposal(ctx, tx, reviewdomain.MissingAmountCommand{HouseholdID: f.householdID, UserID: userID, ReviewItemID: reviewID, ProposalID: proposalID, SourceEventID: f.sourceID, ActorType: "USER", AmountIDR: &amount})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var status, transactionCategory string
	var merchantID *string
	if err := f.pool.QueryRow(ctx, `SELECT status,category_id::text,merchant_id::text FROM transaction WHERE id=$1`, result.TransactionID).Scan(&status, &transactionCategory, &merchantID); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" || merchantID != nil || transactionCategory != category {
		t.Fatalf("ambiguous merchant transaction status=%s category=%s merchant=%v", status, transactionCategory, merchantID)
	}
	var reviewType string
	var duplicateGap, merchantGap, knownMerchant bool
	if err := f.pool.QueryRow(ctx, `SELECT review_type,decision->'missingFacts' ? 'duplicate_relationship',decision->'missingFacts' ? 'merchant',decision->'knownFacts' ? 'merchant' FROM review_item WHERE household_id=$1 AND transaction_id=$2 AND review_type='POSSIBLE_DUPLICATE' AND status IN ('OPEN','PENDING_SEND')`, f.householdID, result.TransactionID).Scan(&reviewType, &duplicateGap, &merchantGap, &knownMerchant); err != nil {
		t.Fatal(err)
	}
	if reviewType != "POSSIBLE_DUPLICATE" || !duplicateGap || !merchantGap || knownMerchant {
		t.Fatalf("review type=%s duplicateGap=%t merchantGap=%t knownMerchant=%t", reviewType, duplicateGap, merchantGap, knownMerchant)
	}
}

func TestScreenshotMissingAmountCanBeIgnoredWithoutInventingAmount(t *testing.T) {
	t.Parallel()
	f := seedScreenshotFixture(t, "Screenshot absent amount ignored")
	row := screenshotDataRow("EXPENSE", "54000", "Warung")
	row.Value.Amount = nil
	row.Value.Direction = "OUT"
	f.persist(t, []validatedScreenshotRow{row})
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='REJECTED' WHERE source_event_id=$1 AND amount IS NULL AND proposal_status='NEEDS_REVIEW'`, f.sourceID); err != nil {
		t.Fatalf("a rejected screenshot proposal must be allowed to keep its missing amount: %v", err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("ignoring an amount-missing row must not create a canonical transaction")
	}
}

func TestMissingAmountIncomeProjectsAndKeepsDuplicateReview(t *testing.T) {
	t.Parallel()
	f := seedScreenshotFixture(t, "Screenshot absent amount income")
	ctx := context.Background()
	row := screenshotDataRow("INCOME", "5000000", "Teman")
	row.Value.Amount, row.Value.Direction, row.Value.CategorySlug, row.CategoryID = nil, "IN", nil, nil
	f.persist(t, []validatedScreenshotRow{row})
	var proposalID, reviewID, userID, requestState string
	if err := f.pool.QueryRow(ctx, `SELECT p.id::text,ri.id::text,(SELECT user_id::text FROM telegram_identity WHERE household_id=p.household_id LIMIT 1),COALESCE((SELECT c.state FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id WHERE r.review_item_id=ri.id LIMIT 1),'') FROM transaction_proposal p JOIN review_item ri ON ri.proposal_id=p.id WHERE p.source_event_id=$1`, f.sourceID).Scan(&proposalID, &reviewID, &userID, &requestState); err != nil {
		t.Fatal(err)
	}
	if requestState == "" {
		t.Fatal("an income missing-amount row must project a completable Telegram card")
	}
	printedAt := screenshotRowTime()
	if _, err := f.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,source_confidence,classification_confidence,confirmed_at) VALUES($1,'INCOME','CONFIRMED',5000000,'IDR',$2,'Honor',.95,.95,now())`, f.householdID, printedAt); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	amount, day := "5000000", printedAt.AddDate(0, 0, 2).Format("2006-01-02")
	result, err := reviewdomain.ResolveMissingAmountProposal(ctx, tx, reviewdomain.MissingAmountCommand{HouseholdID: f.householdID, UserID: userID, ReviewItemID: reviewID, ProposalID: proposalID, SourceEventID: f.sourceID, ActorType: "USER", AmountIDR: &amount, TransactionDate: day, IncomeConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var duplicates int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE transaction_id=$1 AND review_type='POSSIBLE_DUPLICATE' AND status='OPEN'`, result.TransactionID).Scan(&duplicates); err != nil {
		t.Fatal(err)
	}
	if duplicates != 1 {
		t.Fatalf("the ingest-visible duplicate must stay in duplicate review, got %d", duplicates)
	}
}

func TestScreenshotRowAutoConfirmKillSwitchGatesConfirmation(t *testing.T) {
	t.Parallel()
	fixture := seedScreenshotFixture(t, "Screenshot kill switch")
	categoryID := fixture.categoryID
	row := screenshotDataRow("EXPENSE", "54000", "Indomaret")
	row.CategoryID, row.CategoryDecided = &categoryID, true
	processor := &Processor{pool: fixture.pool}
	processor.SetRowAutoConfirm(false)
	if err := processor.persistScreenshot(context.Background(), fixture.documentID, fixture.householdID, fixture.sourceID, "TRANSACTION_HISTORY_SCREENSHOT", screenshotExtraction{Confidence: .95}, "test-model", []validatedScreenshotRow{row}); err != nil {
		t.Fatal(err)
	}
	var confirmed, needsReview int
	if err := fixture.pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE status='CONFIRMED'),count(*) FILTER (WHERE status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&confirmed, &needsReview); err != nil {
		t.Fatal(err)
	}
	if confirmed != 0 || needsReview != 1 {
		t.Fatalf("the screenshot switch off must park the clear row: confirmed=%d needs_review=%d", confirmed, needsReview)
	}
}

func TestScreenshotRowsAutoConfirmClearRowsOnly(t *testing.T) {
	t.Parallel()
	fixture := seedScreenshotFixture(t, "Screenshot rows")
	ctx := context.Background()
	clear := screenshotDataRow("EXPENSE", "54000", "Indomaret")
	categoryID := fixture.categoryID
	clear.CategoryID, clear.CategoryDecided = &categoryID, true
	incoming := screenshotDataRow("INCOME", "100000", "Teman")
	undecided := screenshotDataRow("EXPENSE", "25000", "Warung Bu Tini")
	fixture.persist(t, []validatedScreenshotRow{clear, incoming, undecided})

	var confirmed, needsReview int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='CONFIRMED'),count(*) FILTER (WHERE status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&confirmed, &needsReview); err != nil {
		t.Fatal(err)
	}
	if confirmed != 1 || needsReview != 2 {
		t.Fatalf("clear row must be recorded and uncertain rows must wait: confirmed=%d needs_review=%d", confirmed, needsReview)
	}
	var category, confirmedAt *string
	if err := fixture.pool.QueryRow(ctx, `SELECT category_id::text,confirmed_at::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, fixture.householdID).Scan(&category, &confirmedAt); err != nil {
		t.Fatal(err)
	}
	if category == nil || *category != fixture.categoryID || confirmedAt == nil {
		t.Fatalf("auto-confirmed row must carry its decisive category and confirmation time")
	}
	var documented, sourced string
	if err := fixture.pool.QueryRow(ctx, `SELECT d.status,s.processing_status FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, fixture.documentID).Scan(&documented, &sourced); err != nil {
		t.Fatal(err)
	}
	if documented != "NEEDS_REVIEW" || sourced != "NEEDS_REVIEW" {
		t.Fatalf("a batch with unresolved rows stays in review: document=%s source=%s", documented, sourced)
	}
	assertScreenshotSummary(t, fixture, "3 transaksi ditemukan.", "✅ 1 tersimpan", "⏳ 2 menunggu jawabanmu")
	assertScreenshotReviewDecisions(t, fixture, 2)
}

func TestScreenshotBatchWithOnlyClearRowsNeedsNoReview(t *testing.T) {
	t.Parallel()
	fixture := seedScreenshotFixture(t, "Screenshot clean batch")
	categoryID := fixture.categoryID
	rows := []validatedScreenshotRow{screenshotDataRow("EXPENSE", "54000", "Indomaret"), screenshotDataRow("EXPENSE", "18000", "Kopi Kenangan")}
	for index := range rows {
		rows[index].CategoryID, rows[index].CategoryDecided = &categoryID, true
	}
	fixture.persist(t, rows)

	var confirmed, reviews int
	if err := fixture.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'),(SELECT count(*) FROM review_item WHERE household_id=$1)`, fixture.householdID).Scan(&confirmed, &reviews); err != nil {
		t.Fatal(err)
	}
	if confirmed != 2 || reviews != 0 {
		t.Fatalf("a clear batch must record both rows without a review: confirmed=%d reviews=%d", confirmed, reviews)
	}
	var documented, sourced string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT d.status,s.processing_status FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, fixture.documentID).Scan(&documented, &sourced); err != nil {
		t.Fatal(err)
	}
	if documented != "EXTRACTED" || sourced != "PROCESSED" {
		t.Fatalf("a clear batch reaches a terminal state: document=%s source=%s", documented, sourced)
	}
	assertScreenshotSummary(t, fixture, "2 transaksi ditemukan.", "✅ 2 tersimpan")
	var summary string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT COALESCE(max(payload_json->>'text'),'') FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1`, fmt.Sprint(fixture.chatID)).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(summary, "butuh keputusan") {
		t.Fatalf("no question may be asked for a clear batch: %q", summary)
	}
}

type decisionFacts struct {
	MissingFacts []string       `json:"missingFacts"`
	ReasonCode   string         `json:"reasonCode"`
	KnownFacts   map[string]any `json:"knownFacts"`
}

func assertScreenshotSummary(t *testing.T, fixture screenshotFixture, wants ...string) {
	t.Helper()
	var summary string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT COALESCE(max(payload_json->>'text'),'') FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id' IS NULL AND payload_json->>'chat_id'=$1`, fmt.Sprint(fixture.chatID)).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	for _, want := range wants {
		if !strings.Contains(summary, want) {
			t.Fatalf("batch summary %q must contain %q", summary, want)
		}
	}
}

func assertScreenshotReviewDecisions(t *testing.T, fixture screenshotFixture, want int) {
	t.Helper()
	rows, err := fixture.pool.Query(context.Background(), `SELECT review_type,decision FROM review_item WHERE household_id=$1 AND decision IS NOT NULL`, fixture.householdID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := map[string]decisionFacts{}
	for rows.Next() {
		var reviewType string
		var raw []byte
		if err := rows.Scan(&reviewType, &raw); err != nil {
			t.Fatal(err)
		}
		var facts decisionFacts
		if err := json.Unmarshal(raw, &facts); err != nil {
			t.Fatal(err)
		}
		found[reviewType] = facts
	}
	if len(found) != want {
		t.Fatalf("every unresolved row must store one decision contract, got %d", len(found))
	}
	if category, ok := found["AMBIGUOUS_CATEGORY"]; !ok || len(category.MissingFacts) != 1 || category.MissingFacts[0] != "category" {
		t.Fatalf("a category gap must ask only for the category: %+v", found)
	}
	if transfer, ok := found["TRANSFER_CLASSIFICATION"]; !ok || len(transfer.MissingFacts) != 1 || transfer.MissingFacts[0] != "transfer_relationship" {
		t.Fatalf("an incoming row must ask only for the transfer relationship: %+v", found)
	}
	if amount, ok := found["AMBIGUOUS_CATEGORY"].KnownFacts["amount_idr"]; !ok || amount != "25000" {
		t.Fatalf("known amount must not be asked again: %+v", found["AMBIGUOUS_CATEGORY"].KnownFacts)
	}
}

func TestScreenshotReprocessWithPriorMatchNeverAutoConfirmsNewRow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ status, key string }{{"MERGED", "primary"}, {"REJECTED", "primary"}, {"NEEDS_REVIEW", "primary"}, {"MERGED", "row-001"}, {"REJECTED", "row-001"}, {"MERGED", "merged_into"}, {"REJECTED", "garbage"}} {
		priorStatus, priorKey := tc.status, tc.key
		t.Run(priorStatus+"/"+priorKey, func(t *testing.T) {
			f := seedScreenshotFixture(t, "Screenshot reprocess "+priorStatus)
			ctx := context.Background()
			var originalID, priorProposalID string
			if err := f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,source_confidence,classification_confidence,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',54000,'IDR',$2,$3,.9,.9,now()) RETURNING id`, f.householdID, screenshotRowTime().Add(-100*time.Hour), f.categoryID).Scan(&originalID); err != nil {
				t.Fatal(err)
			}
			link := "matched_transaction_id"
			if priorKey == "merged_into" {
				link, priorKey = "merged_into", "primary"
			}
			if _, err := f.pool.Exec(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,confidence,proposal_status,metadata_json) VALUES($1,$2,'legacy-garbage','EXPENSE',54000,'IDR',$3,.9,'REJECTED','{"matched_transaction_id":"not-a-uuid"}')`, f.householdID, f.sourceID, screenshotRowTime()); err != nil {
				t.Fatal(err)
			}
			if err := f.pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,confidence,proposal_status,metadata_json) VALUES($1,$2,$7,'EXPENSE',54000,'IDR',$3,.9,$4,jsonb_build_object('document_id',$5::uuid,'row_index',0,$8::text,$6::uuid)) RETURNING id`, f.householdID, f.sourceID, screenshotRowTime(), priorStatus, f.documentID, originalID, priorKey, link).Scan(&priorProposalID); err != nil {
				t.Fatal(err)
			}
			categoryID := f.categoryID
			row := screenshotDataRow("EXPENSE", "54000", "Indomaret")
			row.Value.Direction = "OUT"
			row.CategoryID, row.CategoryDecided = &categoryID, true
			if !row.autoConfirmable() {
				t.Fatal("fixture must be auto-confirmable once the matcher finds nothing")
			}
			if err := (&Processor{pool: f.pool}).persistScreenshot(ctx, f.documentID, f.householdID, f.sourceID, "TRANSACTION_HISTORY_SCREENSHOT", screenshotExtraction{Confidence: .95}, "test-model", []validatedScreenshotRow{row}); err != nil {
				t.Fatalf("reprocess must resolve, not fail: %v", err)
			}
			var confirmed, total, evidenceOnOriginal, openReviews int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='CONFIRMED'),count(*) FROM transaction WHERE household_id=$1`, f.householdID).Scan(&confirmed, &total); err != nil {
				t.Fatal(err)
			}
			if confirmed != 1 {
				t.Fatalf("reprocess wrote a duplicate CONFIRMED transaction: confirmed=%d", confirmed)
			}
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND source_event_id=$2`, originalID, f.sourceID).Scan(&evidenceOnOriginal); err != nil {
				t.Fatal(err)
			}
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1 AND review_type='POSSIBLE_DUPLICATE' AND status IN ('OPEN','PENDING_SEND')`, f.householdID).Scan(&openReviews); err != nil {
				t.Fatal(err)
			}
			var carried bool
			if err := f.pool.QueryRow(ctx, `SELECT COALESCE(decision->'knownFacts'->'prior_match_transaction_ids' ? $2,false) FROM review_item WHERE household_id=$1 AND review_type='POSSIBLE_DUPLICATE'`, f.householdID, originalID).Scan(&carried); err != nil || !carried {
				t.Fatalf("review decision must carry the prior match %s: carried=%v err=%v", originalID, carried, err)
			}
			if evidenceOnOriginal == 0 && openReviews == 0 {
				t.Fatal("reprocess must link evidence to the prior match or open a review")
			}
		})
	}
}

func TestScreenshotReprocessMissingAmountKeepsPriorMatch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		priorType string
		offered   bool
	}{{"EXPENSE", true}, {"INCOME", false}} {
		t.Run("prior "+tc.priorType, func(t *testing.T) { reprocessMissingAmountWithPrior(t, tc.priorType, tc.offered) })
	}
}

func reprocessMissingAmountWithPrior(t *testing.T, priorType string, offered bool) {
	f := seedScreenshotFixture(t, "Screenshot reprocess missing amount "+priorType)
	ctx := context.Background()
	var originalID, userID string
	if err := f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,source_confidence,classification_confidence,confirmed_at) VALUES($1,$4,'CONFIRMED',54000,'IDR',$2,$3,.9,.9,now()) RETURNING id`, f.householdID, screenshotRowTime().Add(-100*time.Hour), f.categoryID, priorType).Scan(&originalID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,confidence,proposal_status,metadata_json) VALUES($1,$2,'primary','EXPENSE',54000,'IDR',$3,.9,'MERGED',jsonb_build_object('document_id',$4::uuid,'row_index',0,'matched_transaction_id',$5::uuid))`, f.householdID, f.sourceID, screenshotRowTime(), f.documentID, originalID); err != nil {
		t.Fatal(err)
	}
	categoryID := f.categoryID
	row := screenshotDataRow("EXPENSE", "54000", "Indomaret")
	row.Value.Amount = nil
	row.Value.Direction, row.Value.CategorySlug = "OUT", &f.categorySlug
	row.CategoryID, row.CategoryDecided = &categoryID, true
	f.persist(t, []validatedScreenshotRow{row})
	var proposalID, reviewID string
	if err := f.pool.QueryRow(ctx, `SELECT p.id::text,ri.id::text,(SELECT user_id::text FROM telegram_identity WHERE household_id=p.household_id LIMIT 1) FROM transaction_proposal p JOIN review_item ri ON ri.proposal_id=p.id WHERE p.source_event_id=$1 AND ri.review_type='MISSING_AMOUNT'`, f.sourceID).Scan(&proposalID, &reviewID, &userID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	amountIDR := "58000"
	result, err := reviewdomain.ResolveMissingAmountProposal(ctx, tx, reviewdomain.MissingAmountCommand{HouseholdID: f.householdID, UserID: userID, ReviewItemID: reviewID, ProposalID: proposalID, SourceEventID: f.sourceID, ActorType: "USER", AmountIDR: &amountIDR})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var confirmed int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, f.householdID).Scan(&confirmed); err != nil {
		t.Fatal(err)
	}
	if confirmed != 1 {
		t.Fatalf("supplying the amount wrote a duplicate CONFIRMED transaction: confirmed=%d", confirmed)
	}
	var status string
	var carried bool
	if err := f.pool.QueryRow(ctx, `SELECT t.status,COALESCE(ri.decision->'knownFacts'->'prior_match_transaction_ids' ? $2,false) FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id AND ri.review_type='POSSIBLE_DUPLICATE' AND ri.status='OPEN' WHERE t.id=$1`, result.TransactionID, originalID).Scan(&status, &carried); err != nil {
		t.Fatalf("a POSSIBLE_DUPLICATE review must open for the new row: %v", err)
	}
	if status != "NEEDS_REVIEW" || carried != offered {
		t.Fatalf("new row status=%s, review offers prior match=%v, want offered=%v", status, carried, offered)
	}
}
