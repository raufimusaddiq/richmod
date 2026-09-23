package bankemail

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
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
		Distribution:  map[string]float64{"food-and-drink": top, "groceries": other},
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
