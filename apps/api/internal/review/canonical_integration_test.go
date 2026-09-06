package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestResolveResidualAllocationValidationAndStaleBasis(t *testing.T) {
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
	household, user, _ := seedTransferReviewOwner(t, pool, stamp)
	var source, salarySource, start, end, review, account, foreign string
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),'{}','PROCESSED') RETURNING id`, household, fmt.Sprint(stamp)).Scan(&source))
	must(pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Test','test',true) RETURNING id`, household, user).Scan(&salarySource))
	for _, row := range []struct {
		date, period string
		out          *string
	}{{"2026-01-01", "2026-01-01", &start}, {"2026-02-01", "2026-02-01", &end}} {
		var tx string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,$2::date,'GENERAL',now()) RETURNING id`, household, row.date).Scan(&tx))
		must(pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$4::date,1000000,$5,'CONFIRMED',$6) RETURNING id`, salarySource, household, row.period, row.date, tx, source).Scan(row.out))
	}
	var caseID string
	must(pool.QueryRow(ctx, `INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,'2026-01-01','2026-02-01',1000000,0,0,1000000) RETURNING id`, household, start, end).Scan(&caseID))
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,cycle_residual_case_id,review_type,status) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','OPEN') RETURNING id`, household, caseID).Scan(&review))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Savings','ASSET','BANK','SAVINGS') RETURNING id`, household).Scan(&account))
	var otherHousehold string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Other %d", stamp)).Scan(&otherHousehold))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Other','ASSET','BANK','SAVINGS') RETURNING id`, otherHousehold).Scan(&foreign))
	resolve := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+review+"/resolve", bytes.NewBufferString(body))
		r.SetPathValue("id", review)
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}))
		w := httptest.NewRecorder()
		NewHandler(pool).Resolve(w, r)
		return w
	}
	if w := resolve(`{"action":"ALLOCATE_RETAINED_BALANCE","values":{"allocations":[{"wealthAccountId":"` + account + `","amountIdr":"999999"}]}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("mismatch=%d %s", w.Code, w.Body.String())
	}
	if w := resolve(`{"action":"ALLOCATE_RETAINED_BALANCE","values":{"allocations":[{"wealthAccountId":"` + foreign + `","amountIdr":"1000000"}]}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("foreign=%d %s", w.Code, w.Body.String())
	}
	if w := resolve(`{"action":"ALLOCATE_RETAINED_BALANCE","values":{"allocations":[{"wealthAccountId":"` + account + `","amountIdr":"500000"},{"wealthAccountId":"` + account + `","amountIdr":"500000"}]}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("duplicate=%d %s", w.Code, w.Body.String())
	}
	_, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',500000,'2026-01-15','GENERAL',now())`, household)
	must(err)
	if w := resolve(`{"action":"ALLOCATE_RETAINED_BALANCE","values":{"allocations":[{"wealthAccountId":"` + account + `","amountIdr":"1000000"}]}}`); w.Code != http.StatusConflict {
		t.Fatalf("stale=%d %s", w.Code, w.Body.String())
	}
	var residual string
	var allocations int
	must(pool.QueryRow(ctx, `SELECT basis_residual_idr::text,(SELECT count(*) FROM cycle_residual_allocation WHERE cycle_residual_case_id=$1) FROM cycle_residual_case WHERE id=$1`, caseID).Scan(&residual, &allocations))
	if residual != "500000" || allocations != 0 {
		t.Fatalf("residual=%s allocations=%d", residual, allocations)
	}
}

func TestListPreservesCanonicalReviewMetadata(t *testing.T) {
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
	household, user, _ := seedTransferReviewOwner(t, pool, stamp)
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	var source, attachment, documentID, wealthID, observationID, wealthReview string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("observation-%d", stamp), []byte(fmt.Sprintf("observation-%d", stamp))).Scan(&source))
	must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, household, []byte(fmt.Sprintf("attachment-%d", stamp)), fmt.Sprintf("%s/observation.png", household)).Scan(&attachment))
	must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'WEALTH_OBSERVATION','NEEDS_REVIEW') RETURNING id`, household, source, attachment).Scan(&documentID))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'Reksadana','Bibit','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, household).Scan(&wealthID))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_observation(household_id,document_id,resolved_wealth_account_id,institution,account_hint,observed_value_idr) VALUES($1,$2,$3,'Bibit','Reksadana',42700000) RETURNING id`, household, documentID, wealthID).Scan(&observationID))
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,wealth_observation_id,review_type,status) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN') RETURNING id`, household, observationID).Scan(&wealthReview))

	var salarySource, startEvent, endEvent, salarySourceEvent, residualCase string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'PROCESSED') RETURNING id`, household, fmt.Sprintf("salary-%d", stamp), []byte(fmt.Sprintf("salary-%d", stamp))).Scan(&salarySourceEvent))
	must(pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Test','test',true) RETURNING id`, household, user).Scan(&salarySource))
	for _, row := range []struct {
		date, period string
		into         *string
	}{{"2026-08-25", "2026-08-01", &startEvent}, {"2026-09-25", "2026-09-01", &endEvent}} {
		var transactionID string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,$2::date,'GENERAL',now()) RETURNING id`, household, row.date).Scan(&transactionID))
		must(pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$4::date,1000000,$5,'CONFIRMED',$6) RETURNING id`, salarySource, household, row.period, row.date, transactionID, salarySourceEvent).Scan(row.into))
	}
	must(pool.QueryRow(ctx, `INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,'2026-08-25','2026-09-25',1000000,0,0,1000000) RETURNING id`, household, startEvent, endEvent).Scan(&residualCase))
	_, err = pool.Exec(ctx, `INSERT INTO review_item(household_id,cycle_residual_case_id,review_type,status) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','OPEN')`, household, residualCase)
	must(err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews", nil)
	req = req.WithContext(auth.ContextWithPrincipal(req.Context(), auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}))
	res := httptest.NewRecorder()
	NewHandler(pool).List(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var reviews []struct {
		ID, ReviewType, CycleStart, CycleEnd, WealthObservationID, ResolvedWealthAccountID, Institution, AccountHint string
	}
	must(json.Unmarshal(res.Body.Bytes(), &reviews))
	var gotWealth, gotResidual bool
	for _, value := range reviews {
		if value.ID == wealthReview {
			gotWealth = value.ReviewType == "WEALTH_OBSERVATION_CONFIRMATION" && value.WealthObservationID == observationID && value.ResolvedWealthAccountID == wealthID && value.Institution == "Bibit" && value.AccountHint == "Reksadana"
		}
		if value.ReviewType == "CYCLE_RESIDUAL_ALLOCATION" {
			gotResidual = value.CycleStart == "2026-08-25" && value.CycleEnd == "2026-09-25"
		}
	}
	if !gotWealth || !gotResidual {
		t.Fatalf("wealth=%t residual=%t reviews=%s", gotWealth, gotResidual, res.Body.String())
	}
}

func TestResolveTelegramTransferReconciliation(t *testing.T) {
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
	household, user, _ := seedTransferReviewOwner(t, pool, stamp)
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	var account, wealth, existing, bankSource, proposal, candidateItem, candidateRequest string
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Jago','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'RDN','ASSET','BROKERAGE','INVESTMENT') RETURNING id`, household).Scan(&wealth))
	at := time.Date(2026, 9, 3, 10, 0, 0, 0, time.FixedZone("WIB", 7*60*60))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,transaction_at,purpose) VALUES($1,$2,'UNCLASSIFIED','NEEDS_REVIEW',3000000,$3,'GENERAL') RETURNING id`, household, account, at).Scan(&existing))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("bank-reconcile-%d", stamp), []byte(fmt.Sprintf("bank-reconcile-%d", stamp))).Scan(&bankSource))
	must(pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,transaction_at,confidence,proposal_status) VALUES($1,$2,'UNCLASSIFIED',3000000,$3,.9,'NEEDS_REVIEW') RETURNING id`, household, bankSource, at).Scan(&proposal))
	must(pool.QueryRow(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'BANK_EMAIL',jsonb_build_object('proposal_id',$3::uuid)) RETURNING id`, existing, bankSource, proposal).Scan(new(string)))
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN') RETURNING id`, household, existing).Scan(&candidateItem))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status) VALUES($1,$2,$3,'TRANSFER_CLASSIFICATION','OPEN') RETURNING id`, household, candidateItem, existing).Scan(&candidateRequest))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_CATEGORY')`, candidateRequest)
		return err
	}())

	newCase := func(suffix string, candidates []string) (string, string) {
		var source, review string
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("telegram-reconcile-%s-%d", suffix, stamp), []byte(fmt.Sprintf("telegram-reconcile-%s-%d", suffix, stamp))).Scan(&source))
		_, err = pool.Exec(ctx, `INSERT INTO transfer_reconciliation_case(household_id,source_event_id,account_id,amount_idr,transaction_at,description,proposed_purpose,proposed_wealth_account_id,candidate_transaction_ids) VALUES($1,$2,$3,3000000,$4,'top up RDN','INVESTMENT_CONTRIBUTION',$5,$6::uuid[])`, household, source, account, at, wealth, candidates)
		must(err)
		must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN') RETURNING id`, household, source).Scan(&review))
		return source, review
	}
	resolve := func(review, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+review+"/resolve", bytes.NewBufferString(body))
		req.SetPathValue("id", review)
		req = req.WithContext(auth.ContextWithPrincipal(req.Context(), auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}))
		res := httptest.NewRecorder()
		NewHandler(pool).Resolve(res, req)
		return res
	}
	source, review := newCase("merge", []string{existing})
	if res := resolve(review, `{"action":"MERGE_EXISTING","values":{"transactionId":"`+existing+`"}}`); res.Code != http.StatusNoContent {
		t.Fatalf("merge status=%d body=%s", res.Code, res.Body.String())
	}
	var kind, status, purpose, linkedSource, caseStatus, proposalStatus, bankStatus, itemStatus, requestStatus, conversationState string
	must(pool.QueryRow(ctx, `SELECT t.type,t.status,t.purpose,e.source_event_id::text,(SELECT status FROM transfer_reconciliation_case WHERE source_event_id=$2) FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE t.id=$1`, existing, source).Scan(&kind, &status, &purpose, &linkedSource, &caseStatus))
	must(pool.QueryRow(ctx, `SELECT p.proposal_status,s.processing_status,ri.status,rr.status,rc.state FROM transaction_proposal p JOIN source_event s ON s.id=p.source_event_id JOIN review_item ri ON ri.id=$4 JOIN review_request rr ON rr.id=$5 JOIN review_conversation rc ON rc.review_request_id=rr.id WHERE p.id=$1 AND s.id=$2 AND ri.transaction_id=$3`, proposal, bankSource, existing, candidateItem, candidateRequest).Scan(&proposalStatus, &bankStatus, &itemStatus, &requestStatus, &conversationState))
	if kind != "TRANSFER" || status != "CONFIRMED" || purpose != "INVESTMENT_CONTRIBUTION" || linkedSource != source || caseStatus != "RESOLVED" {
		t.Fatalf("merge=%s/%s/%s source=%s case=%s", kind, status, purpose, linkedSource, caseStatus)
	}
	if proposalStatus != "ACCEPTED" || bankStatus != "PROCESSED" || itemStatus != "RESOLVED" || requestStatus != "RESOLVED" || conversationState != "RESOLVED" {
		t.Fatalf("lifecycle proposal=%s source=%s item=%s request=%s conversation=%s", proposalStatus, bankStatus, itemStatus, requestStatus, conversationState)
	}

	_, newReview := newCase("new", []string{existing})
	if res := resolve(newReview, `{"action":"CONFIRM_NEW_TRANSFER","values":{}}`); res.Code != http.StatusNoContent {
		t.Fatalf("new status=%d body=%s", res.Code, res.Body.String())
	}
	var count int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND amount=3000000`, household).Scan(&count))
	if count != 2 {
		t.Fatalf("transactions=%d", count)
	}
}

func TestResolveUnknownBankTemplateIgnore(t *testing.T) {
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
	householdID, userID, _ := seedTransferReviewOwner(t, pool, stamp)
	var sourceID, reviewID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("unknown-bank-%d", stamp), []byte(fmt.Sprintf("unknown-bank-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN') RETURNING id`, householdID, sourceID).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/resolve", bytes.NewBufferString(`{"action":"IGNORE","values":{}}`))
	request.SetPathValue("id", reviewID)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}))
	response := httptest.NewRecorder()
	NewHandler(pool).Resolve(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	var sourceStatus, reviewStatus, resolution string
	if err := pool.QueryRow(ctx, `SELECT s.processing_status,ri.status,ri.resolution_action FROM review_item ri JOIN source_event s ON s.id=ri.source_event_id WHERE ri.id=$1`, reviewID).Scan(&sourceStatus, &reviewStatus, &resolution); err != nil {
		t.Fatal(err)
	}
	if sourceStatus != "IGNORED" || reviewStatus != "RESOLVED" || resolution != "IGNORE" {
		t.Fatalf("source=%s review=%s resolution=%s", sourceStatus, reviewStatus, resolution)
	}
}
