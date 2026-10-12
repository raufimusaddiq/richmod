package bankemail

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/categoryctx"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// T7: a provider/Jev failure is a machine failure. It must surface as a
// retryable error and must not be converted into a category review.
func TestT7CategoryProviderErrorIsRetryableMachineFailure(t *testing.T) {
	ctx, pool := categoryBoundaryPool(t)
	householdID := createCategoryBoundaryHousehold(t, ctx, pool)
	if _, err := pool.Exec(ctx, "INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','makanan-minuman')", householdID); err != nil {
		t.Fatal(err)
	}
	var sourceEventID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("T7 %d", time.Now().UnixNano()), []byte("T7")).Scan(&sourceEventID); err != nil {
		t.Fatal(err)
	}
	verifier := &stubVerifier{err: errors.New("provider down")}
	result := PolicyResult{Status: "NEEDS_REVIEW", ReviewType: "AMBIGUOUS_CATEGORY"}
	kept, err := (&Processor{pool: pool, verifier: verifier}).applyCategoryDecision(ctx, sourceEventID, householdID, outgoingCard("54000", "Toko"), result)
	if err == nil {
		t.Fatalf("provider error must propagate, got kept=%+v", kept)
	}
	if kept.AutoConfirm || kept.Status == "CONFIRMED" {
		t.Fatalf("a provider failure must never confirm a canonical mutation: %+v", kept)
	}
	var itemCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE source_event_id=$1`, sourceEventID).Scan(&itemCount); err != nil {
		t.Fatal(err)
	}
	if itemCount != 0 {
		t.Fatalf("provider failure created %d review items", itemCount)
	}
	var requestCount, recipientCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM review_request r JOIN review_item i ON i.id=r.review_item_id WHERE i.source_event_id=$1`, sourceEventID).Scan(&requestCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id JOIN review_item i ON i.id=r.review_item_id WHERE i.source_event_id=$1`, sourceEventID).Scan(&recipientCount); err != nil {
		t.Fatal(err)
	}
	if requestCount != 0 || recipientCount != 0 {
		t.Fatalf("provider failure created review delivery work: requests=%d recipients=%d", requestCount, recipientCount)
	}
}

func TestCategoryVerifierAbsenceIsMachineFailureWhenEvidenceExists(t *testing.T) {
	ctx, pool := categoryBoundaryPool(t)
	householdID := createCategoryBoundaryHousehold(t, ctx, pool)
	if _, err := pool.Exec(ctx, "INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','makanan-minuman')", householdID); err != nil {
		t.Fatal(err)
	}
	result := PolicyResult{Status: "NEEDS_REVIEW", ReviewType: "AMBIGUOUS_CATEGORY"}
	_, err := (&Processor{pool: pool}).applyCategoryDecision(ctx, "se", householdID, outgoingCard("54000", "Toko"), result)
	if !errors.Is(err, errVerifierUnconfigured) {
		t.Fatalf("missing required verifier must be machine failure, got %v", err)
	}
}

// T8: a category DB query failure is also a machine failure, not a semantic
// undecided verdict. The resolver must return the error, not an empty answer.
func TestT8CategoryDBQueryFailureIsMachineFailure(t *testing.T) {
	ctx, pool := categoryBoundaryPool(t)
	// A bogus household id makes the active-category query fail structurally.
	category, provenance, err := (&Processor{pool: pool, verifier: &stubVerifier{answers: map[string]judgment.Answer{"category": choice("makanan-minuman", judgment.CategoryCriteria([]judgment.CategoryChoice{{Slug: "makanan-minuman"}}))}}}).resolveNewMerchantCategory(ctx, "se", "not-a-uuid", outgoingCard("54000", "Toko"))
	if err == nil {
		t.Fatalf("a category DB failure must propagate, got category=%q provenance=%+v", category, provenance)
	}
}

// T9: a successful but genuinely undecided answer keeps the residual category
// review; the difference from T7/T8 is the presence of a bounded answer.
func TestT9UndecidedCategoryKeepsResidualReview(t *testing.T) {
	ctx, pool := categoryBoundaryPool(t)
	householdID := createCategoryBoundaryHousehold(t, ctx, pool)
	if _, err := pool.Exec(ctx, "INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','makanan-minuman')", householdID); err != nil {
		t.Fatal(err)
	}
	undecided := PolicyResult{Status: "NEEDS_REVIEW", ReviewType: "AMBIGUOUS_CATEGORY"}
	kept, err := (&Processor{pool: pool, verifier: &stubVerifier{answers: map[string]judgment.Answer{"category": choice("OTHER_OR_UNCLEAR", judgment.CategoryCriteria([]judgment.CategoryChoice{{Slug: "makanan-minuman"}}))}}}).applyCategoryDecision(ctx, "se", householdID, outgoingCard("54000", "Toko"), undecided)
	if err != nil {
		t.Fatalf("an undecided answer is not a machine failure: %v", err)
	}
	if kept.ReviewType != "AMBIGUOUS_CATEGORY" || kept.AutoConfirm {
		t.Fatalf("undecided must keep the category residual: %+v", kept)
	}
}

// T10: a decisive answer resolves the canonical category id and confirms.
func TestT10DecisiveCategoryConfirms(t *testing.T) {
	ctx, pool := categoryBoundaryPool(t)
	householdID := createCategoryBoundaryHousehold(t, ctx, pool)
	var categoryID string
	if err := pool.QueryRow(ctx, "INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','makanan-minuman') RETURNING id", householdID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	result := PolicyResult{Status: "NEEDS_REVIEW", ReviewType: "AMBIGUOUS_CATEGORY"}
	decided, err := (&Processor{pool: pool, verifier: &stubVerifier{answers: map[string]judgment.Answer{"category": choice("makanan-minuman", judgment.CategoryCriteria([]judgment.CategoryChoice{{Slug: "makanan-minuman"}}))}}}).applyCategoryDecision(ctx, "se", householdID, outgoingCard("54000", "Toko"), result)
	if err != nil {
		t.Fatal(err)
	}
	if !decided.AutoConfirm || decided.CategoryID != categoryID || decided.ReviewType != "" {
		t.Fatalf("decisive category must confirm: %+v", decided)
	}
}

func categoryBoundaryPool(t *testing.T) (context.Context, *pgxpool.Pool) {
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
	return ctx, pool
}

func createCategoryBoundaryHousehold(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var householdID string
	if err := pool.QueryRow(ctx, "INSERT INTO household(name) VALUES($1) RETURNING id", fmt.Sprintf("category boundary %d", time.Now().UnixNano())).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	return householdID
}

// The category question carries the household's own category names, how this
// household already filed the same merchant (sub-categories roll up to the
// offered parent), and the local purchase time. Other households, unconfirmed
// transactions and other merchants are never part of the precedent.
func TestCategoryQuestionCarriesHouseholdPrecedent(t *testing.T) {
	ctx, pool := categoryBoundaryPool(t)
	householdID := createCategoryBoundaryHousehold(t, ctx, pool)
	otherHousehold := createCategoryBoundaryHousehold(t, ctx, pool)
	var parentID, childID, otherParent string
	if err := pool.QueryRow(ctx, "INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','makanan-minuman') RETURNING id", householdID).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO category(household_id,parent_id,name,slug) VALUES($1,$2,'Kopi & Camilan','kopi-camilan') RETURNING id", householdID, parentID).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO category(household_id,name,slug) VALUES($1,'Transportasi','transportasi') RETURNING id", householdID).Scan(&otherParent); err != nil {
		t.Fatal(err)
	}
	var otherHouseholdCategory string
	if err := pool.QueryRow(ctx, "INSERT INTO category(household_id,name,slug) VALUES($1,'Lainnya','lainnya') RETURNING id", otherHousehold).Scan(&otherHouseholdCategory); err != nil {
		t.Fatal(err)
	}
	insert := func(household, merchant, status, category string) {
		t.Helper()
		var merchantID string
		if err := pool.QueryRow(ctx, "INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET updated_at=now() RETURNING id", household, merchant).Scan(&merchantID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,category_id,confirmed_at) VALUES($1,'EXPENSE',$2,25000,'IDR',now(),$3,$4,CASE WHEN $2='CONFIRMED' THEN now() END)", household, status, merchantID, category); err != nil {
			t.Fatal(err)
		}
	}
	insert(householdID, "Kopi Tuku", "CONFIRMED", childID)
	insert(householdID, "KOPI TUKU", "CONFIRMED", parentID)
	insert(householdID, "Kopi Tuku", "NEEDS_REVIEW", otherParent)
	insert(householdID, "Gojek", "CONFIRMED", otherParent)
	insert(otherHousehold, "Kopi Tuku", "CONFIRMED", otherHouseholdCategory)

	verifier := &stubVerifier{answers: map[string]judgment.Answer{"category": choice("OTHER_OR_UNCLEAR", judgment.CategoryCriteria([]judgment.CategoryChoice{{Slug: "makanan-minuman"}, {Slug: "transportasi"}}))}}
	if _, _, err := (&Processor{pool: pool, verifier: verifier}).resolveNewMerchantCategory(ctx, "se", householdID, outgoingCard("54000", "Kopi  Tuku")); err != nil {
		t.Fatal(err)
	}
	state, ok := verifier.request.State.(map[string]any)
	if !ok {
		t.Fatalf("unexpected state %T", verifier.request.State)
	}
	history, _ := state[categoryctx.StateKey].(map[string]any)
	categories, _ := history["categories"].(map[string]any)
	if history["confirmed_before"] != 2 || len(categories) != 1 || categories["makanan-minuman"] != 2 {
		t.Fatalf("precedent must count only this household's confirmed same-merchant expenses, rolled up to offered categories: %+v", history)
	}
	if state["local_time"] == nil {
		t.Fatalf("the purchase moment is part of the question: %+v", state)
	}
	criteria, _ := verifier.request.Questions["category"].Criteria.(map[string]any)
	if criteria["makanan-minuman"] != "Makanan & Minuman" || criteria["transportasi"] != "Transportasi" {
		t.Fatalf("categories must be described by the household's own names: %+v", criteria)
	}
	if _, offered := criteria["kopi-camilan"]; offered {
		t.Fatalf("the bank-email choice stays top-level categories: %+v", criteria)
	}
}
