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
	if decision.KnownFacts["amount_idr"] != "54000" {
		t.Fatalf("the known amount must travel with the review: %v", decision.KnownFacts)
	}
	if len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "category" {
		t.Fatalf("a category review must ask only for the category: %v", decision.MissingFacts)
	}
	if decision.InteractionMode == "" || len(decision.AllowedActions) == 0 {
		t.Fatalf("the decision must name an interaction mode and allowed actions: %+v", decision)
	}
	// The allowed actions must be ones a resolver actually accepts; inventing an
	// action name produces a review no client can resolve.
	for _, action := range decision.AllowedActions {
		if !telegramActionKnown(decision.ReasonCode, action) {
			t.Fatalf("action %q is not resolvable for reason %q", action, decision.ReasonCode)
		}
	}
}

// A reason whose question is free text has no bounded actions, so no decision may
// be stored for it. Storing one would assert the wrong unresolved fact.
func TestTelegramReviewWithoutBoundedActionsStoresNoDecision(t *testing.T) {
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
	if raw != "" {
		t.Fatalf("a free-text review must not store a bounded contract: %s", raw)
	}
}
