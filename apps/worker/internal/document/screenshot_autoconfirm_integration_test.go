package document

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	stamp := time.Now().UnixNano()
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
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, fixture.chatID, fixture.householdID, userID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func screenshotRowTime() time.Time {
	return time.Date(2026, 9, 23, 15, 30, 0, 0, time.FixedZone("WIB", 7*3600))
}

// PRD S3 / 17: a row that plausibly matches an existing transaction must link
// evidence, never write a second CONFIRMED transaction beside it. A row with
// candidates but no single decisive match is exactly that case.
func TestScreenshotRowWithUnresolvedCandidatesStaysInReview(t *testing.T) {
	fixture := seedScreenshotFixture(t, "Screenshot ambiguous row")
	ctx := context.Background()
	categoryID := fixture.categoryID
	row := screenshotDataRow("EXPENSE", "54000", "Indomaret")
	row.CategoryID, row.CategoryDecided = &categoryID, true
	row.Candidates = []matchCandidate{{ID: "a", Score: 0.95}, {ID: "b", Score: 0.90}}
	provenance := rowChoiceProvenance{Model: "stub-jev", PolicyVersion: ScreenshotRowCategoryPolicyVersion, Questions: 1, Decided: 1, QuestionKeys: []string{"row_001"}}
	fixture.persist(t, provenance, []validatedScreenshotRow{row})
	var confirmed, needsReview int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='CONFIRMED'),count(*) FILTER (WHERE status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&confirmed, &needsReview); err != nil {
		t.Fatal(err)
	}
	if confirmed != 0 || needsReview != 1 {
		t.Fatalf("an ambiguous row must not be recorded as a second transaction: confirmed=%d needs_review=%d", confirmed, needsReview)
	}
}

func screenshotDataRow(rowType, amount, merchant string) validatedScreenshotRow {
	return validatedScreenshotRow{Value: screenshotRow{Amount: amount, Currency: "IDR", Merchant: merchant, Confidence: .95}, Type: rowType, TransactionAt: screenshotRowTime(), DateKnown: true}
}

func (f screenshotFixture) persist(t *testing.T, provenance rowChoiceProvenance, rows []validatedScreenshotRow) {
	t.Helper()
	if err := (&Processor{pool: f.pool}).persistScreenshot(context.Background(), f.documentID, f.householdID, f.sourceID, "TRANSACTION_HISTORY_SCREENSHOT", screenshotExtraction{Confidence: .95}, "test-model", provenance, rows); err != nil {
		t.Fatal(err)
	}
}

// PRD §33: the screenshot row auto-confirm must be independently disable-able.
// With the switch off, the same clear row waits for a human instead of writing.
func TestScreenshotRowAutoConfirmKillSwitchGatesConfirmation(t *testing.T) {
	fixture := seedScreenshotFixture(t, "Screenshot kill switch")
	categoryID := fixture.categoryID
	row := screenshotDataRow("EXPENSE", "54000", "Indomaret")
	row.CategoryID, row.CategoryDecided = &categoryID, true
	processor := &Processor{pool: fixture.pool}
	processor.SetRowAutoConfirm(false)
	if err := processor.persistScreenshot(context.Background(), fixture.documentID, fixture.householdID, fixture.sourceID, "TRANSACTION_HISTORY_SCREENSHOT", screenshotExtraction{Confidence: .95}, "test-model", rowChoiceProvenance{Model: "stub-jev", PolicyVersion: ScreenshotRowCategoryPolicyVersion, Questions: 1, Decided: 1, QuestionKeys: []string{"row_000"}}, []validatedScreenshotRow{row}); err != nil {
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

// PRD §11.1: an unmatched row with a decisive bounded category and a printed date
// is a new transaction, not an ambiguous one, so it must reach the ledger while
// the genuinely uncertain rows still ask exactly one question.
func TestScreenshotRowsAutoConfirmClearRowsOnly(t *testing.T) {
	fixture := seedScreenshotFixture(t, "Screenshot rows")
	ctx := context.Background()
	clear := screenshotDataRow("EXPENSE", "54000", "Indomaret")
	categoryID := fixture.categoryID
	clear.CategoryID, clear.CategoryDecided = &categoryID, true
	incoming := screenshotDataRow("INCOME", "100000", "Teman")
	undecided := screenshotDataRow("EXPENSE", "25000", "Warung Bu Tini")
	provenance := rowChoiceProvenance{Model: "stub-jev", PolicyVersion: ScreenshotRowCategoryPolicyVersion, Questions: 1, Decided: 0, QuestionKeys: []string{"row_002"}}
	fixture.persist(t, provenance, []validatedScreenshotRow{clear, incoming, undecided})

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
	assertScreenshotSummary(t, fixture, "3 transaksi ditemukan.", "1 berhasil dicatat", "2 butuh keputusan")
	assertScreenshotReviewDecisions(t, fixture, 2)
	var judgements int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM judgment_decision WHERE household_id=$1 AND task='SCREENSHOT_ROW_CATEGORY' AND outcome='REVIEW'`, fixture.householdID).Scan(&judgements); err != nil {
		t.Fatal(err)
	}
	if judgements != 1 {
		t.Fatalf("a clear row must not make an undecided Jev batch look confirmed, got %d review decisions", judgements)
	}
}

// PRD §11.4: a fully clear image records everything and asks for nothing.
func TestScreenshotBatchWithOnlyClearRowsNeedsNoReview(t *testing.T) {
	fixture := seedScreenshotFixture(t, "Screenshot clean batch")
	categoryID := fixture.categoryID
	rows := []validatedScreenshotRow{screenshotDataRow("EXPENSE", "54000", "Indomaret"), screenshotDataRow("EXPENSE", "18000", "Kopi Kenangan")}
	for index := range rows {
		rows[index].CategoryID, rows[index].CategoryDecided = &categoryID, true
	}
	fixture.persist(t, rowChoiceProvenance{PolicyVersion: ScreenshotRowCategoryPolicyVersion}, rows)

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
	assertScreenshotSummary(t, fixture, "2 transaksi ditemukan.", "2 berhasil dicatat")
	var summary string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT COALESCE(max(payload_json->>'text'),'') FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1`, fmt.Sprint(fixture.chatID)).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(summary, "butuh keputusan") {
		t.Fatalf("no question may be asked for a clear batch: %q", summary)
	}
}

func TestScreenshotMixedResidualBatchConfirmsOnlyDecisiveRows(t *testing.T) {
	fixture := seedScreenshotFixture(t, "Screenshot mixed residual batch")
	rows := make([]validatedScreenshotRow, 20)
	for i := range rows {
		rows[i] = screenshotDataRow("EXPENSE", "25000", fmt.Sprintf("Merchant %d", i))
		if i < 17 {
			rows[i].CategoryID, rows[i].CategoryDecided = &fixture.categoryID, true
		}
	}
	rows[17].CategoryID, rows[17].CategoryDecided = &fixture.categoryID, true
	rows[18].CategoryID, rows[18].CategoryDecided = &fixture.secondCategoryID, true
	provenance := rowChoiceProvenance{Model: "stub-jev", PolicyVersion: ScreenshotRowCategoryPolicyVersion, Questions: 3, Decided: 2, QuestionKeys: []string{"row_017", "row_018", "row_019"}}
	fixture.persist(t, provenance, rows)

	var confirmed, reviews int
	if err := fixture.pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE status='CONFIRMED'),count(*) FILTER (WHERE status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1`, fixture.householdID).Scan(&confirmed, &reviews); err != nil {
		t.Fatal(err)
	}
	if confirmed != 19 || reviews != 1 {
		t.Fatalf("17 clear + 2 rescued rows must confirm, one undecided stays in review: confirmed=%d reviews=%d", confirmed, reviews)
	}
	var decisionSource string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT decision->>'decisionSource' FROM review_item WHERE household_id=$1`, fixture.householdID).Scan(&decisionSource); err != nil {
		t.Fatal(err)
	}
	if decisionSource != "GENERATIVE_PLUS_JEV" {
		t.Fatalf("only the questioned residual row should carry Jev provenance, got %q", decisionSource)
	}
	var questioned []string
	if err := fixture.pool.QueryRow(context.Background(), `SELECT question_keys FROM judgment_decision WHERE household_id=$1 AND task='SCREENSHOT_ROW_CATEGORY'`, fixture.householdID).Scan(&questioned); err != nil {
		t.Fatal(err)
	}
	if len(questioned) != 3 || questioned[0] != "row_017" || questioned[1] != "row_018" || questioned[2] != "row_019" {
		t.Fatalf("clear rows must not appear in Jev provenance: %v", questioned)
	}
	assertScreenshotSummary(t, fixture, "20 transaksi ditemukan.", "19 berhasil dicatat", "1 butuh keputusan")
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

// PRD §7/§13.4: an unresolved row stores the decision contract, so the Inbox can
// ask only about the dimension that is genuinely unresolved.
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
