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

// IR-02 accepts the partial supply of a compound stored residual only once both
// dimensions are present, and the canonical transaction stays NEEDS_REVIEW until
// then. This is the API path a legacy or stale card would use, so it must be
// proven at the handler boundary rather than through the blocker helper alone.
func TestConfirmRefusesPartialCompoundResidualUntilAllSupplied(t *testing.T) {
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
	householdID, userID, categoryID := seedTransferReviewOwner(t, pool, stamp)
	var transactionID string
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',63000,'IDR',now()) RETURNING id`, householdID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	decision, _ := json.Marshal(map[string]any{
		"version":         1,
		"reasonCode":      "TRANSACTION_FACTS_MISSING",
		"knownFacts":      map[string]any{"amount_idr": "63000"},
		"missingFacts":    []string{"category", "transaction_at"},
		"allowedActions":  []string{"CONFIRM_REVIEW", "IGNORE"},
		"interactionMode": "SINGLE_FIELD",
	})
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN',$3)`, householdID, transactionID, decision); err != nil {
		t.Fatal(err)
	}

	principal := auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}
	handler := NewHandler(pool)
	confirm := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+transactionID+"/confirm", bytes.NewBufferString(body))
		request.SetPathValue("id", transactionID)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
		response := httptest.NewRecorder()
		handler.Confirm(response, request)
		return response
	}

	categoryOnly := confirm(fmt.Sprintf(`{"categoryId":%q}`, categoryID))
	if categoryOnly.Code != http.StatusConflict {
		t.Fatalf("category-only confirm on a compound residual must be refused, got %d %s", categoryOnly.Code, categoryOnly.Body.String())
	}
	var body struct {
		MissingFacts []string `json:"missingFacts"`
	}
	if err := json.Unmarshal(categoryOnly.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.MissingFacts) != 1 || body.MissingFacts[0] != "transaction_at" {
		t.Fatalf("refusal must name only the still-unresolved date, got %v", body.MissingFacts)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transactionID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" {
		t.Fatalf("partial confirm changed canonical status to %s", status)
	}

	complete := confirm(fmt.Sprintf(`{"categoryId":%q,"transactionAt":"2026-09-20"}`, categoryID))
	if complete.Code != http.StatusNoContent {
		t.Fatalf("fully supplied compound residual must confirm, got %d %s", complete.Code, complete.Body.String())
	}
	var confirmedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT status,transaction_at FROM transaction WHERE id=$1`, transactionID).Scan(&status, &confirmedAt); err != nil {
		t.Fatal(err)
	}
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || confirmedAt.In(jakarta).Format("2006-01-02") != "2026-09-20" {
		t.Fatalf("status=%s transaction_at=%s; want CONFIRMED on the supplied date", status, confirmedAt.In(jakarta).Format("2006-01-02"))
	}
	var open int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE transaction_id=$1 AND status='OPEN'`, transactionID).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if open != 0 {
		t.Fatalf("%d review items remain open after a complete confirm", open)
	}
}
