package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PRD 37: a Telegram review must carry the same ReviewDecision contract the Inbox
// renders, so the card asks for the unresolved dimension only. This drives the one
// place reviews are created and reads the stored decision back.
func TestTelegramReviewStoresTheReviewDecisionContract(t *testing.T) {
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
	var householdID, userID, transactionID string
	if err = pool.QueryRow(ctx, "INSERT INTO household(name) VALUES($1) RETURNING id", fmt.Sprintf("tg decision %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO \"user\"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id", fmt.Sprintf("tg-decision-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')", householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)", stamp, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',54000,now()) RETURNING id", householdID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = EnqueueReviewRequest(ctx, tx, transactionID, "AMBIGUOUS_CATEGORY", stamp, 0, "message"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var raw string
	if err = pool.QueryRow(ctx, "SELECT decision::text FROM review_item WHERE transaction_id=$1", transactionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var decision struct {
		ReasonCode      string         `json:"reasonCode"`
		KnownFacts      map[string]any `json:"knownFacts"`
		MissingFacts    []string       `json:"missingFacts"`
		InteractionMode string         `json:"interactionMode"`
		AllowedActions  []string       `json:"allowedActions"`
	}
	if err = json.Unmarshal([]byte(raw), &decision); err != nil {
		t.Fatalf("stored decision must be valid JSON: %v (%s)", err, raw)
	}
	if decision.ReasonCode != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("reason code=%q", decision.ReasonCode)
	}
	if decision.InteractionMode != "SINGLE_FIELD" {
		t.Fatalf("category is a single missing field, mode=%q", decision.InteractionMode)
	}
	if decision.KnownFacts["amount_idr"] != "54000" {
		t.Fatalf("the known amount must travel with the review: %v", decision.KnownFacts)
	}
	if len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "category" {
		t.Fatalf("a category review must ask only for the category: %v", decision.MissingFacts)
	}
	if decision.InteractionMode == "" || len(decision.AllowedActions) == 0 {
		t.Fatalf("the decision must name an interaction mode and allowed actions: %+v", decision)
	}
	if len(decision.AllowedActions) != 2 || decision.AllowedActions[0] != "CONFIRM_REVIEW" || decision.AllowedActions[1] != "IGNORE" {
		t.Fatalf("category review actions=%v; want the shared accept/ignore contract", decision.AllowedActions)
	}

	var duplicateTransactionID string
	if err = pool.QueryRow(ctx, "INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',54000,now()) RETURNING id", householdID).Scan(&duplicateTransactionID); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = EnqueueReviewRequest(ctx, tx, duplicateTransactionID, "POSSIBLE_DUPLICATE", stamp, 0, "possible duplicate"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT decision::text FROM review_item WHERE transaction_id=$1", duplicateTransactionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(raw), &decision); err != nil {
		t.Fatalf("duplicate decision must be valid JSON: %v (%s)", err, raw)
	}
	if decision.ReasonCode != "POSSIBLE_DUPLICATE" || len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "duplicate_relationship" || decision.InteractionMode != "BOUNDED_CHOICE" {
		t.Fatalf("duplicate review contract is incomplete: %+v", decision)
	}
	// PRD §26 R3: candidate merge, confirm-as-new, or ignore. This reason code
	// never advertises transfer-specific CONFIRM_NEW_TRANSFER.
	wantActions := []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"}
	if len(decision.AllowedActions) != len(wantActions) {
		t.Fatalf("duplicate review actions=%v", decision.AllowedActions)
	}
	for i, action := range wantActions {
		if decision.AllowedActions[i] != action {
			t.Fatalf("duplicate review actions=%v, want=%v", decision.AllowedActions, wantActions)
		}
	}
}

// Free-text clarification is still a single unresolved fact, not an excuse to
// omit the ReviewDecision contract.
func TestTelegramReviewStoresSingleFieldDecision(t *testing.T) {
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
	var householdID, transactionID string
	if err = pool.QueryRow(ctx, "INSERT INTO household(name) VALUES($1) RETURNING id", fmt.Sprintf("tg free text %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',54000,now()) RETURNING id", householdID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = EnqueueReviewRequest(ctx, tx, transactionID, "UNKNOWN_PURPOSE", stamp, 0, "message"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var raw string
	if err = pool.QueryRow(ctx, "SELECT COALESCE(decision::text,'') FROM review_item WHERE transaction_id=$1", transactionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var decision struct {
		ReasonCode      string   `json:"reasonCode"`
		MissingFacts    []string `json:"missingFacts"`
		InteractionMode string   `json:"interactionMode"`
		AllowedActions  []string `json:"allowedActions"`
	}
	if err = json.Unmarshal([]byte(raw), &decision); err != nil {
		t.Fatalf("single-field review must store valid decision JSON: %v (%s)", err, raw)
	}
	if decision.ReasonCode != "UNKNOWN_PURPOSE" || len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "transaction_semantics" || decision.InteractionMode != "SINGLE_FIELD" {
		t.Fatalf("single-field decision=%+v", decision)
	}
	if len(decision.AllowedActions) != 2 || decision.AllowedActions[0] != "CONFIRM_REVIEW" || decision.AllowedActions[1] != "IGNORE" {
		t.Fatalf("single-field actions=%v", decision.AllowedActions)
	}
}
