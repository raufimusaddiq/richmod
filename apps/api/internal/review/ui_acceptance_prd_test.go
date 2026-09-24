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

// PRD section 29 required Review UI acceptance tests.
//
// U1-U4 are properties of the payload the API hands the Inbox: the client must
// render only what the stored decision named as missing, because the server is
// the authority on what is unresolved (PRD 3.3, 13.4). They are asserted here
// rather than in the browser so they hold for every client, Telegram included.
type reviewUIItem struct {
	MissingFacts   []string       `json:"missingFacts"`
	ProposedFacts  map[string]any `json:"proposedFacts"`
	KnownFacts     map[string]any `json:"knownFacts"`
	WhyNotAuto     string         `json:"whyNotAutoConfirm"`
	AllowedActions []string       `json:"allowedActions"`
}

func reviewUIFixture(t *testing.T) (*pgxpool.Pool, string, string) {
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
	return pool, household, user
}

// seedCategoryReview writes the PRD 29 U1 state: a bank expense whose only
// unresolved fact is the category, with that fact recorded in the decision.
func seedCategoryReview(t *testing.T, pool *pgxpool.Pool, household string) string {
	t.Helper()
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var source, review string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("ui-review-%d", stamp), []byte(fmt.Sprintf("ui-review-%d", stamp))).Scan(&source); err != nil {
		t.Fatal(err)
	}
	decision, _ := json.Marshal(map[string]any{
		"version":           1,
		"reasonCode":        "AMBIGUOUS_CATEGORY",
		"decisionClass":     "EVIDENCE_GAP",
		"knownFacts":        map[string]any{"amount_idr": "54000", "merchant": "Toko Sumber Rejeki"},
		"proposedFacts":     map[string]any{"categorySlug": "rumah"},
		"missingFacts":      []string{"category"},
		"whyNotAutoConfirm": "Dua kategori masih sama-sama masuk akal.",
		"allowedActions":    []string{"COMPLETE_BANK_FACTS", "IGNORE"},
		"interactionMode":   "BOUNDED_CHOICE",
	})
	if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN',$3) RETURNING id`, household, source, decision).Scan(&review); err != nil {
		t.Fatal(err)
	}
	return review
}

// U1 - a decision that names only the category must not cause the API to demand
// merchant, amount, time or account from the client.
func TestReviewU1ListExposesOnlyTheUnresolvedFact(t *testing.T) {
	pool, household, user := reviewUIFixture(t)
	seedCategoryReview(t, pool, household)
	items := listCanonicalReviews(t, pool, household, user)
	if len(items) != 1 {
		t.Fatalf("expected the seeded review, got %d", len(items))
	}
	if len(items[0].MissingFacts) != 1 || items[0].MissingFacts[0] != "category" {
		t.Fatalf("the API must name exactly the unresolved dimension, got %v", items[0].MissingFacts)
	}
	if items[0].ProposedFacts["categorySlug"] != "rumah" {
		t.Fatalf("the proposal must reach the client: %v", items[0].ProposedFacts)
	}
	if items[0].WhyNotAuto == "" {
		t.Fatal("why-not-auto-confirm must reach the client so the card can explain itself")
	}
}

// U4 - known facts are carried as known and never listed as missing. The client
// renders them read-only; the server guarantees it never asks for them.
func TestReviewU4KnownFactsAreNotMissingFacts(t *testing.T) {
	pool, household, user := reviewUIFixture(t)
	seedCategoryReview(t, pool, household)
	item := listCanonicalReviews(t, pool, household, user)[0]
	for _, fact := range []string{"merchant", "amount_idr", "transaction_at", "account"} {
		for _, missing := range item.MissingFacts {
			if missing == fact {
				t.Fatalf("%s is already known and must not be requested again", fact)
			}
		}
	}
	if _, known := item.KnownFacts["merchant"]; !known {
		t.Fatalf("the stored known facts must reach the client: %v", item.KnownFacts)
	}
}

func TestTransactionBackedReviewExposesStoredDecision(t *testing.T) {
	pool, household, user := reviewUIFixture(t)
	ctx := context.Background()
	var sourceID, txID string
	stamp := time.Now().UnixNano()
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),decode(md5($2),'hex'),'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("transaction-review-%d", stamp)).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description) VALUES($1,'EXPENSE','NEEDS_REVIEW',54000,'IDR',now(),'Bank card purchase') RETURNING id`, household).Scan(&txID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'BANK_EMAIL')`, txID, sourceID); err != nil {
		t.Fatal(err)
	}
	decision, _ := json.Marshal(map[string]any{
		"version": 1, "reasonCode": "AMBIGUOUS_CATEGORY", "decisionClass": "EVIDENCE_GAP",
		"knownFacts": map[string]any{"amount_idr": "54000"}, "missingFacts": []string{"category"},
		"whyNotAutoConfirm": "category not decisive", "allowedActions": []string{"CONFIRM_REVIEW", "IGNORE"},
	})
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,source_event_id,review_type,status,decision) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','OPEN',$4)`, household, txID, sourceID, decision); err != nil {
		t.Fatal(err)
	}
	items := listCanonicalReviews(t, pool, household, user)
	if len(items) != 1 || len(items[0].MissingFacts) != 1 || items[0].MissingFacts[0] != "category" || items[0].KnownFacts["amount_idr"] != "54000" || items[0].WhyNotAuto == "" {
		t.Fatalf("transaction-backed Inbox must expose its persisted decision: %+v", items)
	}
}

// U5 - a review resolved through the canonical route reaches the same terminal
// state Telegram writes, because both call this one server-owned handler. This
// pins that there is one resolution path, not two that happen to agree today.
func TestReviewU5WebResolutionWritesCanonicalTerminalState(t *testing.T) {
	pool, household, user := reviewUIFixture(t)
	review := seedCategoryReview(t, pool, household)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+review+"/resolve", bytes.NewBufferString(`{"action":"IGNORE"}`))
	r.SetPathValue("id", review)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}))
	w := httptest.NewRecorder()
	NewHandler(pool).Resolve(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("resolution must succeed: %d %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	var status, action string
	if err := pool.QueryRow(ctx, `SELECT status,COALESCE(resolution_action,'') FROM review_item WHERE id=$1`, review).Scan(&status, &action); err != nil {
		t.Fatal(err)
	}
	if status != "RESOLVED" || action != "IGNORE" {
		t.Fatalf("resolution must write the canonical terminal state: status=%s action=%s", status, action)
	}
	if items := listCanonicalReviews(t, pool, household, user); len(items) != 0 {
		t.Fatalf("a resolved review must leave the open list: %d item(s)", len(items))
	}
}

// listCanonicalReviews calls the real list handler and decodes the payload a
// client actually receives, so the assertions run against production shape.
func listCanonicalReviews(t *testing.T, pool *pgxpool.Pool, household, user string) []reviewUIItem {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/", nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}))
	w := httptest.NewRecorder()
	NewHandler(pool).List(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list must succeed: %d %s", w.Code, w.Body.String())
	}
	var items []reviewUIItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("list payload did not decode: %v", err)
	}
	return items
}
