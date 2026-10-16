package review

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func reviewTestHTTP(t *testing.T, pool *pgxpool.Pool, principal auth.Principal) func(string, string, map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewHandler(pool)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /reviews", handler.List)
	mux.HandleFunc("POST /reviews/{id}/classify-transfer", handler.ClassifyTransfer)
	mux.HandleFunc("POST /reviews/{id}/confirm", handler.Confirm)
	return func(method, path string, values map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), principal))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
}

func TestWebTransferClassificationPersistsFallbackResidual(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, classification := range []string{"OWN_ACCOUNT", "HOUSEHOLD_ACCOUNT"} {
		t.Run(classification, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			household, user, _ := seedTransferReviewOwner(t, pool, time.Now().UnixNano())
			transaction, source, proposal := seedUnclassifiedTransfer(t, pool, household, time.Now().UnixNano())
			_, err := pool.Exec(ctx, `UPDATE transaction SET type='INCOME' WHERE id=$1`, transaction)
			must(err)
			_, err = pool.Exec(ctx, `UPDATE transaction_proposal SET proposed_type='INCOME',metadata_json='{"date_known":false}' WHERE id=$1`, proposal)
			must(err)
			_, err = pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN','{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","missingFacts":["transfer_relationship"],"allowedActions":["CLASSIFY_TRANSFER","IGNORE"]}')`, household, transaction)
			must(err)
			call := reviewTestHTTP(t, pool, auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}})
			w := call(http.MethodPost, "/reviews/"+transaction+"/classify-transfer", map[string]any{"classification": classification})
			var conflict struct {
				MissingFacts []string `json:"missingFacts"`
			}
			must(json.Unmarshal(w.Body.Bytes(), &conflict))
			if w.Code != http.StatusConflict || !slices.Equal(conflict.MissingFacts, []string{"transaction_at"}) {
				t.Fatalf("partial classification: status=%d missing=%v", w.Code, conflict.MissingFacts)
			}
			w = call(http.MethodGet, "/reviews", nil)
			if w.Code != http.StatusOK {
				t.Fatalf("reload: status=%d body=%s", w.Code, w.Body.String())
			}
			var items []item
			must(json.Unmarshal(w.Body.Bytes(), &items))
			if len(items) != 1 || items[0].Type != "TRANSFER" || items[0].KnownFacts["relationship_choice"] != classification || !slices.Equal(items[0].MissingFacts, []string{"transaction_at"}) {
				t.Fatalf("accepted Web answer lost on reload: items=%d", len(items))
			}
			var status, proposalStatus, proposalType, storedChoice, sourceStatus string
			must(pool.QueryRow(ctx, `SELECT t.status,p.proposal_status,p.proposed_type,p.metadata_json->>'transfer_classification',s.processing_status FROM transaction t,transaction_proposal p,source_event s WHERE t.id=$1 AND p.id=$2 AND s.id=$3`, transaction, proposal, source).Scan(&status, &proposalStatus, &proposalType, &storedChoice, &sourceStatus))
			if status != "NEEDS_REVIEW" || proposalStatus != "NEEDS_REVIEW" || proposalType != "TRANSFER" || storedChoice != classification || sourceStatus != "NEEDS_REVIEW" {
				t.Fatalf("partial persistence: transaction=%s proposal=%s/%s choice=%s source=%s", status, proposalStatus, proposalType, storedChoice, sourceStatus)
			}
			w = call(http.MethodPost, "/reviews/"+transaction+"/confirm", map[string]any{"transactionAt": "2026-10-03"})
			if w.Code != http.StatusNoContent {
				t.Fatalf("date continuation: status=%d body=%s", w.Code, w.Body.String())
			}
			var kind, purpose, reviewStatus, dateKnown string
			must(pool.QueryRow(ctx, `SELECT t.type,t.purpose,t.status,ri.status,p.proposal_status,p.metadata_json->>'date_known' FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id JOIN transaction_proposal p ON p.id=$2 WHERE t.id=$1`, transaction, proposal).Scan(&kind, &purpose, &status, &reviewStatus, &proposalStatus, &dateKnown))
			if kind != "TRANSFER" || purpose != "INTERNAL_TRANSFER" || status != "CONFIRMED" || reviewStatus != "RESOLVED" || proposalStatus != "ACCEPTED" || dateKnown != "true" {
				t.Fatalf("continuation lost classification: %s/%s/%s review=%s proposal=%s dateKnown=%s", kind, purpose, status, reviewStatus, proposalStatus, dateKnown)
			}
			w = call(http.MethodPost, "/reviews/"+transaction+"/confirm", map[string]any{"transactionAt": "2026-10-03"})
			if w.Code != http.StatusNotFound {
				t.Fatalf("repeat confirmation: status=%d", w.Code)
			}
			var partialAudits, transactions int
			must(pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND entity_id=$2 AND action='UPDATE_REVIEW_FACTS'`, household, transaction).Scan(&partialAudits))
			must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, household).Scan(&transactions))
			if partialAudits != 1 || transactions != 1 {
				t.Fatalf("partial audit=%d transactions=%d", partialAudits, transactions)
			}
		})
	}
}

func TestWebLegacyAccountReplacementRequiresRepairableClassification(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, choice := range []struct{ classification, purpose string }{
		{"ASSET_PURCHASE", "ASSET_PURCHASE"},
		{"INVESTMENT_ACCOUNT", "INVESTMENT_CONTRIBUTION"},
	} {
		t.Run(choice.classification, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			household, user, _ := seedTransferReviewOwner(t, pool, time.Now().UnixNano())
			transaction, _, _ := seedUnclassifiedTransfer(t, pool, household, time.Now().UnixNano())
			var oldAccount, replacement string
			must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Synthetic old account','ASSET','GOLD','INVESTMENT') RETURNING id`, household).Scan(&oldAccount))
			must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Synthetic replacement','ASSET','GOLD','INVESTMENT') RETURNING id`, household).Scan(&replacement))
			_, err := pool.Exec(ctx, `UPDATE transaction SET type='TRANSFER',purpose=$2,related_wealth_account_id=$3 WHERE id=$1`, transaction, choice.purpose, oldAccount)
			must(err)
			_, err = pool.Exec(ctx, `UPDATE wealth_account SET active=false WHERE id=$1`, oldAccount)
			must(err)
			_, err = pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN','{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","missingFacts":["wealth_account"],"allowedActions":["CLASSIFY_TRANSFER","IGNORE"]}')`, household, transaction)
			must(err)
			call := reviewTestHTTP(t, pool, auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}})
			values := map[string]any{"wealthAccountId": replacement}
			w := call(http.MethodPost, "/reviews/"+transaction+"/classify-transfer", values)
			var conflict struct {
				MissingFacts []string `json:"missingFacts"`
			}
			must(json.Unmarshal(w.Body.Bytes(), &conflict))
			if w.Code != http.StatusConflict || !slices.Contains(conflict.MissingFacts, "transfer_relationship") {
				t.Fatalf("legacy re-ask: status=%d missing=%v", w.Code, conflict.MissingFacts)
			}
			values["classification"] = "NOT_A_CLASSIFICATION"
			if w = call(http.MethodPost, "/reviews/"+transaction+"/classify-transfer", values); w.Code != http.StatusBadRequest {
				t.Fatalf("invalid nonempty classification accepted: %d", w.Code)
			}
			values["classification"] = choice.classification
			w = call(http.MethodPost, "/reviews/"+transaction+"/classify-transfer", values)
			if w.Code != http.StatusNoContent {
				t.Fatalf("legacy recovery: status=%d body=%s", w.Code, w.Body.String())
			}
			var status, purpose, selected string
			must(pool.QueryRow(ctx, `SELECT status,purpose,related_wealth_account_id::text FROM transaction WHERE id=$1`, transaction).Scan(&status, &purpose, &selected))
			if status != "CONFIRMED" || purpose != choice.purpose || selected != replacement {
				t.Fatalf("legacy recovery: status=%s purpose=%s replacementMatches=%v", status, purpose, selected == replacement)
			}
		})
	}
}
