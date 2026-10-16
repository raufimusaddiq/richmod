package review

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestConfirmRecoversDeactivatedPartialClassificationCandidate(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, choice := range []struct{ classification, role, fact string }{
		{"ASSET_PURCHASE", "OTHER", "wealth_account"},
		{"INVESTMENT_ACCOUNT", "INVESTMENT", "wealth_account"},
		{"EXPENSE", "", "category"},
	} {
		t.Run(choice.classification, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			household, user, category := seedTransferReviewOwner(t, pool, time.Now().UnixNano())
			transaction, _, proposal := seedUnclassifiedTransfer(t, pool, household, time.Now().UnixNano())
			_, err := pool.Exec(ctx, `UPDATE transaction_proposal SET metadata_json='{"date_known":false}' WHERE id=$1`, proposal)
			must(err)
			_, err = pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN','{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","missingFacts":["transfer_relationship","transaction_at"],"allowedActions":["CLASSIFY_TRANSFER","IGNORE"]}')`, household, transaction)
			must(err)
			var account string
			if choice.role != "" {
				must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Synthetic old asset','ASSET','GOLD',$2) RETURNING id`, household, choice.role).Scan(&account))
				category = ""
			}
			handler := NewHandler(pool)
			principal := auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}
			request := func(action string, values map[string]any) *httptest.ResponseRecorder {
				t.Helper()
				raw, err := json.Marshal(values)
				must(err)
				r := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+transaction+"/"+action, bytes.NewReader(raw))
				r.SetPathValue("id", transaction)
				r = r.WithContext(auth.ContextWithPrincipal(r.Context(), principal))
				w := httptest.NewRecorder()
				if action == "confirm" {
					handler.Confirm(w, r)
				} else {
					handler.ClassifyTransfer(w, r)
				}
				return w
			}
			initial := map[string]any{"classification": choice.classification}
			if account != "" {
				initial["wealthAccountId"] = account
			} else {
				initial["categoryId"] = category
			}
			w := request("classify-transfer", initial)
			if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "transaction_at") {
				t.Fatalf("partial classification: status=%d body=%s", w.Code, w.Body.String())
			}
			var storedChoice, storedCandidate string
			must(pool.QueryRow(ctx, `SELECT COALESCE(ri.decision->'knownFacts'->>'relationship_choice',''),COALESCE(t.related_wealth_account_id::text,t.category_id::text,'') FROM review_item ri JOIN transaction t ON t.id=ri.transaction_id WHERE t.id=$1`, transaction).Scan(&storedChoice, &storedCandidate))
			if storedChoice != choice.classification || storedCandidate == "" || storedCandidate != account && storedCandidate != category {
				t.Fatalf("partial answer discarded: classification=%q candidateRetained=%v", storedChoice, storedCandidate != "")
			}
			if account != "" {
				_, err = pool.Exec(ctx, `UPDATE wealth_account SET active=false WHERE id=$1`, account)
			} else {
				_, err = pool.Exec(ctx, `UPDATE category SET active=false WHERE id=$1`, category)
			}
			must(err)
			w = request("confirm", map[string]any{"transactionAt": "2026-10-03"})
			if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), choice.fact) {
				t.Fatalf("candidate re-ask: status=%d body=%s", w.Code, w.Body.String())
			}
			if account != "" {
				w = request("classify-transfer", map[string]any{"classification": "OWN_ACCOUNT", "transactionAt": "2026-10-03"})
				if w.Code != http.StatusNotFound {
					t.Fatalf("changed transfer purpose accepted: %d", w.Code)
				}
				foreignHousehold, _, _ := seedTransferReviewOwner(t, pool, time.Now().UnixNano())
				var foreignAccount string
				must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Synthetic foreign asset','ASSET','GOLD',$2) RETURNING id`, foreignHousehold, choice.role).Scan(&foreignAccount))
				w = request("classify-transfer", map[string]any{"classification": choice.classification, "wealthAccountId": foreignAccount, "transactionAt": "2026-10-03"})
				if w.Code != http.StatusConflict {
					t.Fatalf("foreign replacement accepted: %d", w.Code)
				}
			}
			var replacement string
			values := map[string]any{"transactionAt": "2026-10-03"}
			action := "confirm"
			if account != "" {
				must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Synthetic replacement asset','ASSET','GOLD',$2) RETURNING id`, household, choice.role).Scan(&replacement))
				values["classification"], values["wealthAccountId"] = choice.classification, replacement
				action = "classify-transfer"
			} else {
				must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Synthetic replacement','replacement') RETURNING id`, household).Scan(&replacement))
				values["categoryId"] = replacement
			}
			w = request(action, values)
			if w.Code != http.StatusNoContent {
				t.Fatalf("replacement: status=%d body=%s", w.Code, w.Body.String())
			}
			var status, selected, reviewStatus, proposalStatus string
			must(pool.QueryRow(ctx, `SELECT t.status,COALESCE(t.related_wealth_account_id,t.category_id)::text,ri.status,p.proposal_status FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id JOIN transaction_proposal p ON p.id=$2 WHERE t.id=$1`, transaction, proposal).Scan(&status, &selected, &reviewStatus, &proposalStatus))
			if status != "CONFIRMED" || selected != replacement || reviewStatus != "RESOLVED" || proposalStatus != "ACCEPTED" {
				t.Fatalf("recovery: status=%s replacementMatches=%v review=%s proposal=%s", status, selected == replacement, reviewStatus, proposalStatus)
			}
		})
	}
}
