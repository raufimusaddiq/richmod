package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

type laneFixture struct {
	t               *testing.T
	pool            *pgxpool.Pool
	household, user string
	stamp           int64
}

func newLaneFixture(t *testing.T) *laneFixture {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	stamp := time.Now().UnixNano()
	household, user, _ := seedTransferReviewOwner(t, pool, stamp)
	return &laneFixture{t: t, pool: pool, household: household, user: user, stamp: stamp}
}

func (f *laneFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *laneFixture) principal(r *http.Request) *http.Request {
	return r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: f.user, Memberships: []auth.Membership{{HouseholdID: f.household, Role: "OWNER"}}}))
}

type laneItem struct {
	ID             string   `json:"id"`
	ReviewType     string   `json:"reviewType"`
	AllowedActions []string `json:"allowedActions"`
	Presentation   string   `json:"presentation"`
	PurposeChoices []string `json:"purposeChoices"`
}

func (f *laneFixture) list(cursor string) ([]laneItem, http.Header) {
	f.t.Helper()
	path := "/api/v1/reviews"
	if cursor != "" {
		path += "?cursor=" + cursor
	}
	r := f.principal(httptest.NewRequest(http.MethodGet, path, nil))
	w := httptest.NewRecorder()
	NewHandler(f.pool).List(w, r)
	if w.Code != http.StatusOK {
		f.t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}
	var items []laneItem
	f.must(json.Unmarshal(w.Body.Bytes(), &items))
	return items, w.Header()
}

func (f *laneFixture) item(id string) laneItem {
	f.t.Helper()
	items, _ := f.list("")
	for _, value := range items {
		if value.ID == id {
			return value
		}
	}
	f.t.Fatalf("review %s not listed", id)
	return laneItem{}
}

func (f *laneFixture) resolve(id, body string) *httptest.ResponseRecorder {
	r := f.principal(httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+id+"/resolve", bytes.NewBufferString(body)))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	NewHandler(f.pool).Resolve(w, r)
	return w
}

func (f *laneFixture) reopen(id string) *httptest.ResponseRecorder {
	r := f.principal(httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+id+"/reopen", nil))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	NewHandler(f.pool).Reopen(w, r)
	return w
}

func (f *laneFixture) source(kind, status string) string {
	f.t.Helper()
	f.stamp++
	var id string
	f.must(f.pool.QueryRow(context.Background(), `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,$2,$3,now(),$4,$5) RETURNING id`, f.household, kind, fmt.Sprintf("lane-%d", f.stamp), []byte(fmt.Sprintf("lane-%d", f.stamp)), status).Scan(&id))
	return id
}

func (f *laneFixture) status(source string) string {
	f.t.Helper()
	var status string
	f.must(f.pool.QueryRow(context.Background(), `SELECT processing_status FROM source_event WHERE id=$1`, source).Scan(&status))
	return status
}

// A bank-email source review offers bank fact completion (never document
// reprocessing, which needs a document), and every offered action executes.
func TestBankEmailLowConfidenceOffersOnlyExecutableActions(t *testing.T) {
	f := newLaneFixture(t)
	ctx := context.Background()
	decision := `{"version":1,"reasonCode":"DOCUMENT_EXTRACTION_LOW_CONFIDENCE","knownFacts":{},"missingFacts":["amount"],"allowedActions":["COMPLETE_BANK_FACTS","IGNORE"]}`
	newItem := func() (string, string) {
		source := f.source("BANK_EMAIL", "NEEDS_REVIEW")
		var id string
		f.must(f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'DOCUMENT_EXTRACTION_LOW_CONFIDENCE','OPEN',$3::jsonb) RETURNING id`, f.household, source, decision).Scan(&id))
		return id, source
	}
	complete, _ := newItem()
	if got := f.item(complete).AllowedActions; !slices.Equal(got, []string{"COMPLETE_BANK_FACTS", "IGNORE"}) {
		t.Fatalf("offered=%v", got)
	}
	if w := f.resolve(complete, `{"action":"REPROCESS_DOCUMENT"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("an action that is not offered must be refused: %d %s", w.Code, w.Body.String())
	}
	if w := f.resolve(complete, `{"action":"COMPLETE_BANK_FACTS","values":{"amountIdr":"54000","transactionAt":"2026-10-19T06:30:00Z"}}`); w.Code != http.StatusAccepted {
		t.Fatalf("complete=%d %s", w.Code, w.Body.String())
	}
	ignored, source := newItem()
	if w := f.resolve(ignored, `{"action":"IGNORE"}`); w.Code != http.StatusNoContent {
		t.Fatalf("ignore=%d %s", w.Code, w.Body.String())
	}
	if got := f.status(source); got != "IGNORED" {
		t.Fatalf("ignored bank source=%s", got)
	}
}

// A provider email whose facts are unsupported is a notice: it is not counted
// as a decision, and acknowledging it settles the source through the shared
// lifecycle.
func TestFinancialEmailFactsIsANoticeOutsideTheDecisionCount(t *testing.T) {
	f := newLaneFixture(t)
	ctx := context.Background()
	source := f.source("FINANCIAL_EMAIL", "NEEDS_REVIEW")
	var observation, notice string
	f.must(f.pool.QueryRow(ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status) VALUES($1,$2,0,'CASH_MOVEMENT','{}'::jsonb,'REVIEW') RETURNING id`, f.household, source).Scan(&observation))
	f.must(f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,financial_email_observation_id,review_type,status,decision) VALUES($1,$2,'FINANCIAL_EMAIL_FACTS','OPEN','{"version":1,"reasonCode":"FINANCIAL_EMAIL_FACTS","knownFacts":{},"missingFacts":["evidence_support"],"allowedActions":["IGNORE"]}'::jsonb) RETURNING id`, f.household, observation).Scan(&notice))
	items, header := f.list("")
	var listed laneItem
	for _, value := range items {
		if value.ID == notice {
			listed = value
		}
	}
	if listed.Presentation != reviewdomain.PresentationNotice {
		t.Fatalf("presentation=%q", listed.Presentation)
	}
	decisions, _ := strconv.Atoi(header.Get("X-Decision-Count"))
	if decisions != 0 {
		t.Fatalf("a notice must not count as a decision: X-Decision-Count=%d", decisions)
	}
	if w := f.resolve(notice, `{"action":"IGNORE"}`); w.Code != http.StatusNoContent {
		t.Fatalf("acknowledge=%d %s", w.Code, w.Body.String())
	}
	if got := f.status(source); got != "IGNORED" {
		t.Fatalf("source=%s", got)
	}
}

// Choosing the wealth account is one action that resolves the review; the
// observation is accepted for the next snapshot, the document and source are
// settled, and the decision can be reopened.
func TestWealthAccountChoiceResolvesInOneStepAndReopens(t *testing.T) {
	f := newLaneFixture(t)
	ctx := context.Background()
	seed := func() (string, string, string, string) {
		source := f.source("TELEGRAM_IMAGE", "NEEDS_REVIEW")
		var attachment, document, observation, item string
		f.must(f.pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, f.household, []byte(source), f.household+"/"+source+".png").Scan(&attachment))
		f.must(f.pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'WEALTH_OBSERVATION','NEEDS_REVIEW') RETURNING id`, f.household, source, attachment).Scan(&document))
		f.must(f.pool.QueryRow(ctx, `INSERT INTO wealth_observation(household_id,document_id,institution,account_hint,observed_value_idr) VALUES($1,$2,'Bibit','Reksadana',42700000) RETURNING id`, f.household, document).Scan(&observation))
		f.must(f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,wealth_observation_id,review_type,status,decision) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN','{"version":1,"reasonCode":"WEALTH_OBSERVATION_CONFIRMATION","knownFacts":{},"missingFacts":["wealth_snapshot_confirmation"],"allowedActions":["SET_WEALTH_ACCOUNT","IGNORE"]}'::jsonb) RETURNING id`, f.household, observation).Scan(&item))
		return source, document, observation, item
	}
	var wealth string
	f.must(f.pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'Reksadana','Bibit','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, f.household).Scan(&wealth))
	source, document, observation, item := seed()
	if w := f.resolve(item, `{"action":"SET_WEALTH_ACCOUNT","values":{"wealthAccountId":"`+wealth+`"}}`); w.Code != http.StatusOK {
		t.Fatalf("set account=%d %s", w.Code, w.Body.String())
	}
	var itemStatus, observationStatus, documentStatus string
	f.must(f.pool.QueryRow(ctx, `SELECT (SELECT status FROM review_item WHERE id=$1),(SELECT status FROM wealth_observation WHERE id=$2),(SELECT status FROM document WHERE id=$3)`, item, observation, document).Scan(&itemStatus, &observationStatus, &documentStatus))
	if itemStatus != "RESOLVED" || observationStatus != "ACCEPTED" || documentStatus != "EXTRACTED" || f.status(source) != "PROCESSED" {
		t.Fatalf("item=%s observation=%s document=%s source=%s", itemStatus, observationStatus, documentStatus, f.status(source))
	}
	var transactions int
	f.must(f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND amount=42700000`, f.household).Scan(&transactions))
	if transactions != 0 {
		t.Fatal("a wealth observation never becomes a transaction")
	}
	if w := f.reopen(item); w.Code != http.StatusOK {
		t.Fatalf("reopen=%d %s", w.Code, w.Body.String())
	}
	var open int
	f.must(f.pool.QueryRow(ctx, `SELECT (SELECT status FROM wealth_observation WHERE id=$1),(SELECT count(*) FROM review_item WHERE wealth_observation_id=$1 AND status='OPEN')`, observation).Scan(&observationStatus, &open))
	if observationStatus != "PENDING" || open != 1 || f.status(source) != "NEEDS_REVIEW" {
		t.Fatalf("reopened observation=%s open=%d source=%s", observationStatus, open, f.status(source))
	}
	if w := f.reopen(item); w.Code != http.StatusConflict {
		t.Fatalf("an already reopened decision cannot be reopened twice: %d", w.Code)
	}
	// IGNORE settles the document instead of leaving it NEEDS_REVIEW (T13).
	source, document, _, item = seed()
	if w := f.resolve(item, `{"action":"IGNORE"}`); w.Code != http.StatusNoContent {
		t.Fatalf("ignore=%d %s", w.Code, w.Body.String())
	}
	f.must(f.pool.QueryRow(ctx, `SELECT status FROM document WHERE id=$1`, document).Scan(&documentStatus))
	if documentStatus != "EXTRACTED" || f.status(source) != "IGNORED" {
		t.Fatalf("ignored wealth document=%s source=%s", documentStatus, f.status(source))
	}
}

// TRANSACTION_MISSING has a defined outcome: the residual review resolves as
// "transaksi belum tercatat", and recording a transaction in that cycle reopens
// it with the recomputed residual. No synthetic transaction is written.
func TestResidualTransactionMissingResolvesAndReturnsWithTheNewResidual(t *testing.T) {
	f := newLaneFixture(t)
	ctx := context.Background()
	salaryEventSource := f.source("TELEGRAM_TEXT", "PROCESSED")
	var salarySource, start, end, caseID, item string
	f.must(f.pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Test','test',true) RETURNING id`, f.household, f.user).Scan(&salarySource))
	for _, row := range []struct {
		date string
		into *string
	}{{"2026-08-25", &start}, {"2026-09-25", &end}} {
		var transaction string
		f.must(f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,$2::date,'GENERAL',now()) RETURNING id`, f.household, row.date).Scan(&transaction))
		f.must(f.pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$3::date,1000000,$4,'CONFIRMED',$5) RETURNING id`, salarySource, f.household, row.date, transaction, salaryEventSource).Scan(row.into))
	}
	f.must(f.pool.QueryRow(ctx, `INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,'2026-08-25','2026-09-25',1000000,0,0,1000000) RETURNING id`, f.household, start, end).Scan(&caseID))
	f.must(f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,cycle_residual_case_id,review_type,status,decision) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','OPEN','{"version":1,"reasonCode":"CYCLE_RESIDUAL_ALLOCATION","knownFacts":{},"missingFacts":["residual_allocation"],"allowedActions":["ALLOCATE_RETAINED_BALANCE","TRANSACTION_MISSING","LEAVE_UNALLOCATED"]}'::jsonb) RETURNING id`, f.household, caseID).Scan(&item))
	w := f.resolve(item, `{"action":"TRANSACTION_MISSING"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("transaction missing=%d %s", w.Code, w.Body.String())
	}
	var route struct{ Route, CycleStart string }
	f.must(json.Unmarshal(w.Body.Bytes(), &route))
	var status, resolution string
	f.must(f.pool.QueryRow(ctx, `SELECT status,resolution_action FROM review_item WHERE id=$1`, item).Scan(&status, &resolution))
	if status != "RESOLVED" || resolution != "TRANSACTION_MISSING" || route.Route != "CANONICAL_TRANSACTION_FLOW" || route.CycleStart != "2026-08-25" {
		t.Fatalf("status=%s resolution=%s route=%+v", status, resolution, route)
	}
	// The household records the missing expense in that cycle.
	tx, err := f.pool.Begin(ctx)
	f.must(err)
	defer tx.Rollback(ctx)
	at := time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC)
	_, err = tx.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',300000,$2,'GENERAL',now())`, f.household, at)
	f.must(err)
	f.must(reviewdomain.RefreshOpenCycleResiduals(ctx, tx, f.household, at, f.user))
	f.must(tx.Commit(ctx))
	var reopened, residual string
	f.must(f.pool.QueryRow(ctx, `SELECT ri.decision->'knownFacts'->>'residual_idr',c.basis_residual_idr::text FROM review_item ri JOIN cycle_residual_case c ON c.id=ri.cycle_residual_case_id WHERE ri.cycle_residual_case_id=$1 AND ri.status='OPEN'`, caseID).Scan(&reopened, &residual))
	if reopened != "700000" || residual != "700000" {
		t.Fatalf("reopened residual=%s basis=%s", reopened, residual)
	}
}

// The transaction review list is paged with a keyset cursor instead of being
// silently capped at 100.
func TestTransactionReviewListPagesWithACursor(t *testing.T) {
	f := newLaneFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range reviewPageSize + 1 {
		_, err := f.pool.Exec(ctx, `WITH t AS (INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',$2,$3) RETURNING id) INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) SELECT $1,id,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","knownFacts":{},"missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb FROM t`, f.household, 1000+i, base.Add(time.Duration(i)*time.Minute))
		f.must(err)
	}
	first, header := f.list("")
	cursor := header.Get("X-Next-Cursor")
	if len(first) != reviewPageSize || cursor == "" || header.Get("X-Decision-Count") != strconv.Itoa(reviewPageSize+1) {
		t.Fatalf("first page=%d cursor=%q count=%s", len(first), cursor, header.Get("X-Decision-Count"))
	}
	second, header := f.list(cursor)
	if len(second) != 1 || header.Get("X-Next-Cursor") != "" || slices.ContainsFunc(first, func(item laneItem) bool { return item.ID == second[0].ID }) {
		t.Fatalf("second page=%d next=%q", len(second), header.Get("X-Next-Cursor"))
	}
}

// A transfer case with neither candidates nor a proposed purpose offers a new
// transfer with the bounded purpose choices instead of IGNORE only.
func TestTransferCaseWithoutPurposeOffersBoundedNewTransfer(t *testing.T) {
	f := newLaneFixture(t)
	ctx := context.Background()
	source := f.source("TELEGRAM_TEXT", "NEEDS_REVIEW")
	var account, item string
	f.must(f.pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening','BANK','FULL_LEDGER') RETURNING id`, f.household).Scan(&account))
	_, err := f.pool.Exec(ctx, `INSERT INTO transfer_reconciliation_case(household_id,source_event_id,account_id,amount_idr,transaction_at,description,proposed_purpose,candidate_transaction_ids) VALUES($1,$2,$3,250000,now(),'pindah dana','','{}'::uuid[])`, f.household, source, account)
	f.must(err)
	f.must(f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN','{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","knownFacts":{},"missingFacts":["transfer_relationship"],"allowedActions":["CLASSIFY_TRANSFER","IGNORE"]}'::jsonb) RETURNING id`, f.household, source).Scan(&item))
	listed := f.item(item)
	if !slices.Equal(listed.AllowedActions, []string{"CONFIRM_NEW_TRANSFER", "IGNORE"}) || !slices.Contains(listed.PurposeChoices, "INTERNAL_TRANSFER") {
		t.Fatalf("offered=%v purposes=%v", listed.AllowedActions, listed.PurposeChoices)
	}
	if w := f.resolve(item, `{"action":"CONFIRM_NEW_TRANSFER","values":{"purpose":"NOT_A_PURPOSE"}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("an unbounded purpose must be refused: %d", w.Code)
	}
	if w := f.resolve(item, `{"action":"CONFIRM_NEW_TRANSFER","values":{"purpose":"INTERNAL_TRANSFER"}}`); w.Code != http.StatusNoContent {
		t.Fatalf("confirm new=%d %s", w.Code, w.Body.String())
	}
	var purpose string
	f.must(f.pool.QueryRow(ctx, `SELECT t.purpose FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1`, source).Scan(&purpose))
	if purpose != "INTERNAL_TRANSFER" || f.status(source) != "PROCESSED" {
		t.Fatalf("purpose=%s source=%s", purpose, f.status(source))
	}
}

// A source that never printed a date is answered with a bounded one-tap date
// that Go resolves (SET_TRANSACTION_DATE), not a date the user cannot know.
func TestMissingTransactionDateTakesAOneTapDateChoice(t *testing.T) {
	f := newLaneFixture(t)
	ctx := context.Background()
	source := f.source("TELEGRAM_IMAGE", "NEEDS_REVIEW")
	var category, transaction string
	f.must(f.pool.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 LIMIT 1`, f.household).Scan(&category))
	f.must(f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,category_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',25000,now(),$2) RETURNING id`, f.household, category).Scan(&transaction))
	_, err := f.pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,'RECEIPT_IMAGE',.9)`, transaction, source)
	f.must(err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'MISSING_TRANSACTION_DATE','OPEN','{"version":1,"reasonCode":"MISSING_TRANSACTION_DATE","knownFacts":{"transaction_time_source":"RECEIVED_AT_FALLBACK"},"missingFacts":["transaction_at"],"allowedActions":["SET_TRANSACTION_DATE","IGNORE"]}'::jsonb)`, f.household, transaction)
	f.must(err)
	r := f.principal(httptest.NewRequest(http.MethodGet, "/api/v1/reviews", nil))
	w := httptest.NewRecorder()
	NewHandler(f.pool).List(w, r)
	var items []struct {
		ID          string                    `json:"id"`
		DateChoices []reviewdomain.DateChoice `json:"dateChoices"`
	}
	f.must(json.Unmarshal(w.Body.Bytes(), &items))
	if len(items) != 1 || len(items[0].DateChoices) < 2 || items[0].DateChoices[1].Key != reviewdomain.DateChoiceYesterday {
		t.Fatalf("date choices=%+v", items)
	}
	r = f.principal(httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+transaction+"/confirm", bytes.NewBufferString(`{"dateChoice":"YESTERDAY"}`)))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", transaction)
	w = httptest.NewRecorder()
	NewHandler(f.pool).Confirm(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("confirm=%d %s", w.Code, w.Body.String())
	}
	var day string
	f.must(f.pool.QueryRow(ctx, `SELECT (transaction_at AT TIME ZONE 'Asia/Jakarta')::date::text FROM transaction WHERE id=$1`, transaction).Scan(&day))
	if want := items[0].DateChoices[1].Date; day != want {
		t.Fatalf("transaction date=%s want %s", day, want)
	}
}

// Reopening a transaction review rebuilds its decision from the transaction as
// it is now instead of reusing the old snapshot.
func TestReopenedTransactionReviewCarriesAFreshDecision(t *testing.T) {
	f := newLaneFixture(t)
	ctx := context.Background()
	var category, transaction string
	f.must(f.pool.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 LIMIT 1`, f.household).Scan(&category))
	f.must(f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,category_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',25000,now(),$2) RETURNING id`, f.household, category).Scan(&transaction))
	_, err := f.pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision,resolved_at) VALUES($1,$2,'UNKNOWN_MERCHANT','RESOLVED','{"version":1,"reasonCode":"UNKNOWN_MERCHANT","knownFacts":{"amount_idr":"1"},"missingFacts":["merchant","category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb,now())`, f.household, transaction)
	f.must(err)
	tx, err := f.pool.Begin(ctx)
	f.must(err)
	defer tx.Rollback(ctx)
	f.must(reviewdomain.ReopenTransactionReview(ctx, tx, f.household, transaction))
	f.must(tx.Commit(ctx))
	var missing, amount string
	f.must(f.pool.QueryRow(ctx, `SELECT decision->>'missingFacts',decision->'knownFacts'->>'amount_idr' FROM review_item WHERE transaction_id=$1 AND status='OPEN'`, transaction).Scan(&missing, &amount))
	if missing != `["merchant"]` || amount != "25000" {
		t.Fatalf("reopened decision missing=%s amount=%s", missing, amount)
	}
}
