package review

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestListSuggestedCategoriesRankByMerchantHistory(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Now().UnixNano()
	household, user, _ := seedTransferReviewOwner(t, pool, stamp)
	foreign, foreignUser, foreignCategory := seedTransferReviewOwner(t, pool, stamp+1)
	_ = foreignUser
	category := func(owner, name string, active bool) string {
		var id string
		must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug,active) VALUES($1,$2,$3,$4) RETURNING id`, owner, name, fmt.Sprintf("c%d-%s", time.Now().UnixNano(), strings.ToLower(name)), active).Scan(&id))
		return id
	}
	food, transport, shopping, bills, retired := category(household, "Makan", true), category(household, "Transport", true), category(household, "Belanja", true), category(household, "Tagihan", true), category(household, "Lama", false)
	var grab, other string
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Grab') RETURNING id`, household).Scan(&grab))
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Toko Lain') RETURNING id`, household).Scan(&other))
	var foreignGrab string
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Grab') RETURNING id`, foreign).Scan(&foreignGrab))

	insert := func(owner, merchant, categoryID, status, description string, ago time.Duration) string {
		var id string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id,category_id,description,confirmed_at,voided_at) VALUES($1,'EXPENSE',$2,10000,now()-$3::interval,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,NULLIF($6,''),CASE WHEN $2='CONFIRMED' THEN now() END,CASE WHEN $2='VOIDED' THEN now() END) RETURNING id`, owner, status, fmt.Sprintf("%d seconds", int(ago.Seconds())), merchant, categoryID, description).Scan(&id))
		return id
	}
	insert(household, grab, transport, "CONFIRMED", "", 48*time.Hour)
	insert(household, grab, transport, "CONFIRMED", "", 40*time.Hour)
	insert(household, grab, food, "CONFIRMED", "", 3*time.Hour)
	insert(household, grab, food, "CONFIRMED", "", 2*time.Hour)
	insert(household, grab, shopping, "CONFIRMED", "", 1*time.Hour)
	insert(household, grab, bills, "VOIDED", "", time.Hour)
	insert(household, grab, bills, "VOIDED", "", time.Hour)
	insert(household, grab, bills, "VOIDED", "", time.Hour)
	insert(household, grab, retired, "CONFIRMED", "", time.Hour)
	insert(household, grab, retired, "CONFIRMED", "", time.Hour)
	insert(household, grab, retired, "CONFIRMED", "", time.Hour)
	insert(foreign, foreignGrab, foreignCategory, "CONFIRMED", "", time.Hour)
	insert(foreign, foreignGrab, foreignCategory, "CONFIRMED", "", time.Hour)
	insert(household, other, bills, "CONFIRMED", "", 5*time.Hour)
	insert(household, other, bills, "CONFIRMED", "", 6*time.Hour)

	withMerchant := insert(household, grab, "", "NEEDS_REVIEW", "GRAB RIDE", time.Minute)
	noHistory := insert(household, "", "", "NEEDS_REVIEW", "WARUNG BARU", time.Minute)
	insert(household, "", transport, "CONFIRMED", "Ojek Pangkalan", 30*time.Hour)
	byDescription := insert(household, "", "", "NEEDS_REVIEW", "  ojek   pangkalan ", time.Minute)
	categorized := insert(household, grab, food, "NEEDS_REVIEW", "SUDAH ADA", time.Minute)
	for _, id := range []string{withMerchant, noHistory, byDescription, categorized} {
		_, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSACTION_FACTS_MISSING','OPEN','{"reasonCode":"TRANSACTION_FACTS_MISSING","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}')`, household, id)
		must(err)
	}
	_, err = pool.Exec(ctx, `UPDATE review_item SET decision='{"reasonCode":"LOW_CONFIDENCE","missingFacts":[],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}' WHERE transaction_id=$1`, categorized)
	must(err)

	list := func(owner, userID string) map[string][]suggestedCategory {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews", nil)
		req = req.WithContext(auth.ContextWithPrincipal(req.Context(), auth.Principal{UserID: userID, HouseholdID: owner, HasHousehold: true}))
		res := httptest.NewRecorder()
		NewHandler(pool).List(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
		}
		var items []struct {
			ID                  string              `json:"id"`
			SuggestedCategories []suggestedCategory `json:"suggestedCategories"`
		}
		must(json.Unmarshal(res.Body.Bytes(), &items))
		result := map[string][]suggestedCategory{}
		for _, value := range items {
			result[value.ID] = value.SuggestedCategories
		}
		return result
	}
	got := list(household, user)

	merchantRank := got[withMerchant]
	if len(merchantRank) != 3 {
		t.Fatalf("want 3 suggestions, got %+v", merchantRank)
	}
	if merchantRank[0].ID != food || merchantRank[0].Count != 2 || merchantRank[0].Reason != suggestionMerchantHistory {
		t.Fatalf("rank 1 must be most-recent of the tied most-frequent: %+v", merchantRank)
	}
	if merchantRank[1].ID != transport || merchantRank[1].Count != 2 || merchantRank[1].Reason != suggestionMerchantHistory {
		t.Fatalf("rank 2: %+v", merchantRank)
	}
	if merchantRank[2].ID != shopping || merchantRank[2].Count != 1 || merchantRank[2].Reason != suggestionMerchantHistory {
		t.Fatalf("rank 3: %+v", merchantRank)
	}
	for _, value := range merchantRank {
		if value.ID == retired || value.ID == bills || value.ID == foreignCategory {
			t.Fatalf("inactive, voided-only or foreign category suggested: %+v", merchantRank)
		}
	}

	fill := got[noHistory]
	if len(fill) == 0 {
		t.Fatal("expected a household-recent fill")
	}
	for _, value := range fill {
		if value.Reason != suggestionHouseholdRecent || value.ID == retired || value.ID == foreignCategory {
			t.Fatalf("unexpected fill: %+v", fill)
		}
	}
	if fill[0].ID != food && fill[0].ID != transport && fill[0].ID != bills {
		t.Fatalf("fill must come from this household's confirmed history: %+v", fill)
	}

	byDesc := got[byDescription]
	if len(byDesc) == 0 || byDesc[0].ID != transport || byDesc[0].Reason != suggestionMerchantHistory {
		t.Fatalf("description match: %+v", byDesc)
	}
	if _, present := got[categorized]; !present || len(got[categorized]) != 0 {
		t.Fatalf("an already-categorized review must carry no suggestions: %+v", got[categorized])
	}

	foreignTx := insert(foreign, foreignGrab, "", "NEEDS_REVIEW", "GRAB RIDE", time.Minute)
	_, err = pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSACTION_FACTS_MISSING','OPEN','{"reasonCode":"TRANSACTION_FACTS_MISSING","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}')`, foreign, foreignTx)
	must(err)
	foreignRank := list(foreign, foreignUser)[foreignTx]
	if len(foreignRank) != 1 || foreignRank[0].ID != foreignCategory {
		t.Fatalf("foreign household ranking leaked or missed: %+v", foreignRank)
	}
}
