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

type financialResolutionFixture struct {
	pool                                 *pgxpool.Pool
	household, user, review, observation string
	account, wealthAccount, otherWealth  string
}

// seedFinancialResolution models the PRD §12 state: the evidence resolved the
// funding account, the provider Wealth Account is still unresolved, and one
// observation-scoped review is open.
func seedFinancialResolution(t *testing.T, resolved bool) financialResolutionFixture {
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
	household, user, _ := seedTransferReviewOwner(t, pool, stamp)
	fixture := financialResolutionFixture{pool: pool, household: household, user: user}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank Jago','BANK','FULL_LEDGER') RETURNING id`, household).Scan(&fixture.account))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Bibit Growth','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, household).Scan(&fixture.wealthAccount))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Reksa Dana Pasar Uang','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, household).Scan(&fixture.otherWealth))
	var source, financialSource string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'FINANCIAL_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("financial-resolution-%d", stamp), []byte(fmt.Sprintf("financial-resolution-%d", stamp))).Scan(&source))
	must(pool.QueryRow(ctx, `INSERT INTO financial_email_source(household_id,provider_name,sender_address,capabilities,status,created_by_user_id) VALUES($1,'Generic Investment Provider','provider@example.test',ARRAY['CASH_MOVEMENT'],'ACTIVE',$2) RETURNING id`, household, user).Scan(&financialSource))
	if _, err := pool.Exec(ctx, `INSERT INTO financial_email_event(source_event_id,financial_source_id,observed_sender,message_id,subject) VALUES($1,$2,'provider@example.test',$3,'Transaksi reksa dana')`, source, financialSource, fmt.Sprintf("message-%d", stamp)); err != nil {
		t.Fatal(err)
	}
	resolvedAccount := any(nil)
	if resolved {
		resolvedAccount = fixture.account
	}
	must(pool.QueryRow(ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status,resolved_account_id) VALUES($1,$2,0,'CASH_MOVEMENT',jsonb_build_object('kind','CASH_MOVEMENT','movement_type','CONTRIBUTION','amount_idr','3000000','funding_account_hint','Bank Jago','provider_account_hint','Bibit Growth'),'REVIEW',$3) RETURNING id`, household, source, resolvedAccount).Scan(&fixture.observation))
	missingFact := "account"
	knownFacts := map[string]any{"wealth_account": "Bibit Growth"}
	if resolved {
		missingFact = "wealth_account"
		knownFacts = map[string]any{"account": "Bank Jago"}
	}
	decision, _ := json.Marshal(map[string]any{
		"version": 1, "reasonCode": "FINANCIAL_EMAIL_RESOLUTION", "decisionClass": "EVIDENCE_GAP",
		"knownFacts": knownFacts, "missingFacts": []string{missingFact},
		"allowedActions": []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"}, "interactionMode": "SINGLE_FIELD",
	})
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,financial_email_observation_id,review_type,status,decision) VALUES($1,$2,'FINANCIAL_EMAIL_RESOLUTION','OPEN',$3) RETURNING id`, household, fixture.observation, decision).Scan(&fixture.review))
	return fixture
}

func seedFinancialResolutionValues(t *testing.T, resolved bool) financialResolutionFixture {
	t.Helper()
	return seedFinancialResolution(t, resolved)
}

// UIRC-02 B: a partial first pick must persist, stay pending, and not close the
// review; the same canonical operation is what Telegram calls.
func TestFinancialResolutionPartialPickStaysOpen(t *testing.T) {
	fixture := seedFinancialResolutionValues(t, true)
	ctx := context.Background()
	if w := fixture.resolve(t, `{"action":"SET_FINANCIAL_EMAIL_ENTITIES","values":{"wealthAccountId":"`+fixture.wealthAccount+`"}}`); w.Code != http.StatusNoContent {
		t.Fatalf("resolved-on-the-final-entity must be 204: %d %s", w.Code, w.Body.String())
	}
	var status string
	if err := fixture.pool.QueryRow(ctx, `SELECT status FROM financial_email_observation WHERE id=$1`, fixture.observation).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "PENDING" {
		t.Fatalf("final entity must queue the observation for replay: %s", status)
	}
}

// UIRC-02 B: Web IGNORE must take the same lifecycle as the Telegram callback.
func TestFinancialResolutionIgnoreLifecycle(t *testing.T) {
	fixture := seedFinancialResolutionValues(t, true)
	ctx := context.Background()
	if w := fixture.resolve(t, `{"action":"IGNORE","values":{}}`); w.Code != http.StatusNoContent {
		t.Fatalf("ignore must resolve: %d %s", w.Code, w.Body.String())
	}
	var observationStatus, itemStatus, sourceStatus string
	var openRequests int
	if err := fixture.pool.QueryRow(ctx, `SELECT fo.status,ri.status,se.processing_status,(SELECT count(*) FROM review_request WHERE review_item_id=ri.id AND status IN ('OPEN','PENDING_SEND')) FROM financial_email_observation fo JOIN review_item ri ON ri.financial_email_observation_id=fo.id JOIN source_event se ON se.id=fo.source_event_id WHERE fo.id=$1`, fixture.observation).Scan(&observationStatus, &itemStatus, &sourceStatus, &openRequests); err != nil {
		t.Fatal(err)
	}
	if observationStatus != "IGNORED" || itemStatus != "RESOLVED" || sourceStatus != "IGNORED" || openRequests != 0 {
		t.Fatalf("ignore lifecycle observation=%s item=%s source=%s openRequests=%d", observationStatus, itemStatus, sourceStatus, openRequests)
	}
}

func (f financialResolutionFixture) resolve(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+f.review+"/resolve", bytes.NewBufferString(body))
	r.SetPathValue("id", f.review)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: f.user, Memberships: []auth.Membership{{HouseholdID: f.household, Role: "OWNER"}}}))
	w := httptest.NewRecorder()
	NewHandler(f.pool).Resolve(w, r)
	return w
}

// PRD §12: the request supplies only the unresolved entity, and the persisted
// entity is merged back in rather than asked for again.
func TestFinancialResolutionAcceptsOnlyTheUnresolvedEntity(t *testing.T) {
	fixture := seedFinancialResolution(t, true)
	ctx := context.Background()
	if w := fixture.resolve(t, `{"action":"SET_FINANCIAL_EMAIL_ENTITIES","values":{"wealthAccountId":"`+fixture.wealthAccount+`","human_supplied_fields":["account"]}}`); w.Code != http.StatusNoContent {
		t.Fatalf("partial resolution must be accepted: %d %s", w.Code, w.Body.String())
	}
	var account, wealth, status string
	var transactionID *string
	if err := fixture.pool.QueryRow(ctx, `SELECT COALESCE(resolved_account_id::text,''),COALESCE(resolved_wealth_account_id::text,''),status,transaction_id::text FROM financial_email_observation WHERE id=$1`, fixture.observation).Scan(&account, &wealth, &status, &transactionID); err != nil {
		t.Fatal(err)
	}
	if account != fixture.account || wealth != fixture.wealthAccount || status != "PENDING" || transactionID != nil {
		t.Fatalf("merged observation must carry both entities and wait for the worker: account=%s wealth=%s status=%s", account, wealth, status)
	}
	var reviewStatus, resolutionAction string
	if err := fixture.pool.QueryRow(ctx, `SELECT status,COALESCE(resolution_action,'') FROM review_item WHERE id=$1`, fixture.review).Scan(&reviewStatus, &resolutionAction); err != nil {
		t.Fatal(err)
	}
	if reviewStatus != "RESOLVED" || resolutionAction != "SET_FINANCIAL_EMAIL_ENTITIES" {
		t.Fatalf("review must resolve: status=%s action=%s", reviewStatus, resolutionAction)
	}
	var changedFields []string
	if err := fixture.pool.QueryRow(ctx, `SELECT changed_fields FROM product_telemetry_event WHERE review_item_id=$1 AND event_type='REVIEW_TURN'`, fixture.review).Scan(&changedFields); err != nil {
		t.Fatal(err)
	}
	if len(changedFields) != 1 || changedFields[0] != "wealth_account" {
		t.Fatalf("telemetry must count only the user-supplied entity, not the merged known account: %v", changedFields)
	}
	var jobs int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='PROCESS_FINANCIAL_EMAIL' AND payload_json->>'source_event_id'=(SELECT source_event_id::text FROM financial_email_observation WHERE id=$1)`, fixture.observation).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("resolution must re-enqueue the observation for deterministic processing, got %d jobs", jobs)
	}
	var aliasTarget string
	if err := fixture.pool.QueryRow(ctx, `SELECT COALESCE(wealth_account_id::text,'') FROM financial_entity_alias WHERE household_id=$1 AND entity_type='WEALTH_ACCOUNT' AND active`, fixture.household).Scan(&aliasTarget); err != nil && err.Error() != "no rows in result set" {
		t.Fatal(err)
	}
	if aliasTarget != fixture.wealthAccount {
		t.Fatalf("the newly resolved provider hint must be learned, got %q", aliasTarget)
	}
}

// A partial resolution must not rewrite an entity the household already bound.
func TestFinancialResolutionKeepsAlreadyKnownEntityAlias(t *testing.T) {
	fixture := seedFinancialResolution(t, false)
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,account_id,alias,normalized_alias,source) VALUES($1,'ACCOUNT',$2,'Bank Jago','bank jago','USER')`, fixture.household, fixture.account); err != nil {
		t.Fatal(err)
	}
	if w := fixture.resolve(t, `{"action":"SET_FINANCIAL_EMAIL_ENTITIES","values":{"accountId":"`+fixture.account+`","wealthAccountId":"`+fixture.otherWealth+`"}}`); w.Code != http.StatusNoContent {
		t.Fatalf("full resolution must still be accepted: %d %s", w.Code, w.Body.String())
	}
	var aliasSource string
	if err := fixture.pool.QueryRow(ctx, `SELECT source FROM financial_entity_alias WHERE household_id=$1 AND entity_type='ACCOUNT' AND normalized_alias='bank jago'`, fixture.household).Scan(&aliasSource); err != nil {
		t.Fatal(err)
	}
	if aliasSource != "USER" {
		t.Fatalf("an existing mapping must not be rewritten by a resolution, got %s", aliasSource)
	}
}

// Nothing to resolve is a client error, not a silently empty resolution.
func TestFinancialResolutionRejectsEmptyValues(t *testing.T) {
	fixture := seedFinancialResolution(t, true)
	if w := fixture.resolve(t, `{"action":"SET_FINANCIAL_EMAIL_ENTITIES","values":{}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("an empty resolution must be rejected: %d %s", w.Code, w.Body.String())
	}
	if w := fixture.resolve(t, `{"action":"SET_FINANCIAL_EMAIL_ENTITIES","values":null}`); w.Code != http.StatusBadRequest {
		t.Fatalf("a null resolution must be rejected: %d %s", w.Code, w.Body.String())
	}
}

// The list must expose the already resolved entity, so the Inbox can render a
// partial card and ask only for what is missing (PRD §12, §13.4).
// PRD §28 F1/F2: the review must name exactly the one unresolved entity, never
// both. F1 has the funding account resolved; F2 the Wealth Account.
func TestFinancialProviderEmailAsksOnlyTheUnresolvedEntity(t *testing.T) {
	for _, test := range []struct {
		name        string
		resolved    bool
		missingFact string
	}{
		{"F1 wealth unresolved", true, "wealth_account"},
		{"F2 source unresolved", false, "account"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := seedFinancialResolution(t, test.resolved)
			if !test.resolved {
				// F2: the evidence resolved the provider Wealth Account but not the
				// funding account, and the stored decision names only the gap.
				if _, err := fixture.pool.Exec(context.Background(), `UPDATE financial_email_observation SET resolved_wealth_account_id=$2 WHERE id=$1`, fixture.observation, fixture.wealthAccount); err != nil {
					t.Fatal(err)
				}
				decision, _ := json.Marshal(map[string]any{
					"version": 1, "reasonCode": "FINANCIAL_EMAIL_RESOLUTION", "decisionClass": "EVIDENCE_GAP",
					"knownFacts": map[string]any{"wealth_account": "Bibit Growth"}, "missingFacts": []string{"account"},
					"allowedActions": []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"}, "interactionMode": "SINGLE_FIELD",
				})
				if _, err := fixture.pool.Exec(context.Background(), `UPDATE review_item SET decision=$2::jsonb WHERE id=$1`, fixture.review, decision); err != nil {
					t.Fatal(err)
				}
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews", nil)
			req = req.WithContext(auth.ContextWithPrincipal(req.Context(), auth.Principal{UserID: fixture.user, Memberships: []auth.Membership{{HouseholdID: fixture.household, Role: "OWNER"}}}))
			res := httptest.NewRecorder()
			NewHandler(fixture.pool).List(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			var items []struct {
				ReviewType   string   `json:"reviewType"`
				MissingFacts []string `json:"missingFacts"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &items); err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if item.ReviewType != "FINANCIAL_EMAIL_RESOLUTION" {
					continue
				}
				if len(item.MissingFacts) != 1 || item.MissingFacts[0] != test.missingFact {
					t.Fatalf("review must ask only for %s, got %v", test.missingFact, item.MissingFacts)
				}
				return
			}
			t.Fatal("the open financial resolution review must appear in the list")
		})
	}
}

func TestFinancialResolutionListItemExposesPartialState(t *testing.T) {
	fixture := seedFinancialResolution(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews", nil)
	req = req.WithContext(auth.ContextWithPrincipal(req.Context(), auth.Principal{UserID: fixture.user, Memberships: []auth.Membership{{HouseholdID: fixture.household, Role: "OWNER"}}}))
	res := httptest.NewRecorder()
	NewHandler(fixture.pool).List(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var items []struct {
		ReviewType        string   `json:"reviewType"`
		ResolvedAccountID string   `json:"resolvedAccountId"`
		AllowedActions    []string `json:"allowedActions"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ReviewType != "FINANCIAL_EMAIL_RESOLUTION" {
			continue
		}
		if item.ResolvedAccountID != fixture.account {
			t.Fatalf("the list must expose the already resolved entity, got %q", item.ResolvedAccountID)
		}
		return
	}
	t.Fatal("the open financial resolution review must appear in the list")
}
