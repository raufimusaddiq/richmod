package bankemail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

func bankTestPool(t *testing.T) *pgxpool.Pool {
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
	return pool
}

func seedCategory(t *testing.T, pool *pgxpool.Pool, household, slug, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO category(household_id,name,slug) VALUES($1,$2,$3) RETURNING id`, household, name, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func categoryAnswer(choice string, top, other float64) map[string]judgment.Answer {
	return map[string]judgment.Answer{"category": {
		Type:          "choice",
		Choice:        choice,
		Distribution:  map[string]float64{"food-and-drink": top, "groceries": other, "OTHER_OR_UNCLEAR": 0},
		Confidence:    0.9,
		HasConfidence: true,
	}}
}

// A decisive bounded category answer must auto-confirm a new-merchant expense
// with zero user input (PRD §9.1 B2; Definition of Done "decisive category can
// auto-confirm").
func TestClassifyExpenseCategoryAutoConfirmsDecisiveAnswer(t *testing.T) {
	pool := bankTestPool(t)
	ctx := context.Background()
	var householdID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank category %d", time.Now().UnixNano())).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	seedCategory(t, pool, householdID, "food-and-drink", "Makanan & Minuman")
	seedCategory(t, pool, householdID, "groceries", "Belanja Rumah")
	processor := &Processor{pool: pool, verifier: &stubVerifier{answers: categoryAnswer("food-and-drink", 0.9, 0.1)}}
	categoryID, decided, err := processor.classifyExpenseCategory(ctx, householdID, "evt", Extraction{Merchant: stringPtrFor("Gorengan Pak Tri")})
	if err != nil {
		t.Fatal(err)
	}
	if !decided || categoryID == "" {
		t.Fatal("a decisive category answer must auto-confirm")
	}
}

// A thin-margin answer must not guess; the caller falls back to a category-only
// review (PRD §9.1 B3, §34 "ask one bounded question").
func TestClassifyExpenseCategoryLeavesUndecidedToReview(t *testing.T) {
	pool := bankTestPool(t)
	ctx := context.Background()
	var householdID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank category gap %d", time.Now().UnixNano())).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	seedCategory(t, pool, householdID, "food-and-drink", "Makanan & Minuman")
	seedCategory(t, pool, householdID, "groceries", "Belanja Rumah")
	processor := &Processor{pool: pool, verifier: &stubVerifier{answers: categoryAnswer("food-and-drink", 0.55, 0.45)}}
	if _, decided, err := processor.classifyExpenseCategory(ctx, householdID, "evt", Extraction{Merchant: stringPtrFor("Warung")}); err != nil {
		t.Fatal(err)
	} else if decided {
		t.Fatal("a thin-margin answer must not auto-confirm")
	}
}

// PRD 7: the review a bank-email transaction opens must carry the stored contract,
// so the Inbox can ask only for the unresolved fact. The decision write is the
// last step of persist and only lands if the review row already exists — an
// earlier version updated before EnqueueReviewRequest created the row and matched
// zero rows, silently storing nothing. Drive the real persist path here.
// PRD 7: the review a bank-email transaction opens must carry the stored contract,
// so the Inbox can ask only for the unresolved fact. Drive the real persist path.
func TestBankEmailReviewPersistsTheDecisionContract(t *testing.T) {
	pool := bankTestPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var householdID, userID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank decision %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-decision-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	// persist only enqueues a review when a recipient exists, and that review is the
	// row the decision attaches to.
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, stamp%1000000, householdID, userID); err != nil {
		t.Fatal(err)
	}
	var listenerID, sourceEventID, transactionID string
	if err := pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Test Bank','alerts@test.invalid',$2) RETURNING id`, householdID, userID).Scan(&listenerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("bank-decision-%d", stamp), []byte("decision")).Scan(&sourceEventID); err != nil {
		t.Fatal(err)
	}
	processor := &Processor{pool: pool}
	merchant := "Warung Baru"
	outgoing, channel := "OUTGOING", "QR"
	extraction := Extraction{Kind: "TRANSACTION", Direction: &outgoing, Channel: &channel, AmountIDR: ptr("54000"), TransactionAt: timePtr(), Merchant: &merchant, Confidence: 0.95}
	result := EvaluateBankEmail(Listener{TrackingPolicy: "SPENDING_ONLY"}, extraction, nil)
	if result.ReviewType != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("expected a category review, got %q", result.ReviewType)
	}
	if err := processor.persist(ctx, Listener{ID: listenerID, HouseholdID: householdID, BankName: "Test Bank"}, sourceEventID, extraction, result); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM transaction WHERE household_id=$1`, householdID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT decision FROM review_item WHERE transaction_id=$1`, transactionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		t.Fatalf("stored decision must be valid JSON: %v (%s)", err, raw)
	}
	if decision.ReasonCode != "AMBIGUOUS_CATEGORY" {
		t.Fatalf("reasonCode=%q", decision.ReasonCode)
	}
	if len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "category" {
		t.Fatalf("a category review must ask only for the category: %v", decision.MissingFacts)
	}
	if decision.PolicyVersion != categoryDecisionPolicyVersion {
		t.Fatalf("the stored version must name the policy that decided: %q", decision.PolicyVersion)
	}
}
