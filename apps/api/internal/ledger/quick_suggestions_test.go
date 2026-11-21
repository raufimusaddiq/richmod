package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestBuildQuickSuggestionsEmpty(t *testing.T) {
	got := buildQuickSuggestions(nil)
	encoded, _ := json.Marshal(got)
	if string(encoded) != `{"frequent":[],"recentCategories":[],"descriptionMap":{}}` {
		t.Fatalf("empty response must be explicit empty collections: %s", encoded)
	}
}

func TestBuildQuickSuggestionsRanksDeterministically(t *testing.T) {
	// Newest first, as the query returns them.
	rows := []quickRow{
		{Description: "  Kopi   Pagi ", Amount: "25000", Type: "EXPENSE", CategoryID: "cat-food", AccountID: "acc-1"},
		{Description: "Parkir", Amount: "5000", Type: "EXPENSE", CategoryID: "cat-transport"},
		{Description: "kopi pagi", Amount: "25000", Type: "EXPENSE", CategoryID: "cat-food", AccountID: "acc-1"},
		{Description: "Kopi pagi", Amount: "30000", Type: "EXPENSE", CategoryID: "cat-snack"},
		{Description: "Gaji", Amount: "9000000", Type: "INCOME"},
		{Description: "   ", Amount: "1", Type: "EXPENSE", CategoryID: "cat-blank"},
	}
	got := buildQuickSuggestions(rows)
	first := got.Frequent[0]
	if first.Description != "Kopi   Pagi" || first.Amount != "25000" || first.CategoryID == nil || *first.CategoryID != "cat-food" || first.AccountID == nil || *first.AccountID != "acc-1" {
		t.Fatalf("the repeated entry ranks first with its newest values: %#v", first)
	}
	var income *quickFrequent
	for i := range got.Frequent {
		if got.Frequent[i].Description == "Gaji" {
			income = &got.Frequent[i]
		}
	}
	if income == nil || income.CategoryID != nil || income.AccountID != nil {
		t.Fatalf("an entry without category/account keeps them null: %#v", income)
	}
	if want := []string{"cat-food", "cat-transport", "cat-snack"}; !reflect.DeepEqual(got.RecentCategories, want) {
		t.Fatalf("recentCategories=%v want %v", got.RecentCategories, want)
	}
	// "kopi pagi" was categorised food twice and snack once.
	if got.DescriptionMap["kopi pagi"] != "cat-food" || got.DescriptionMap["parkir"] != "cat-transport" {
		t.Fatalf("descriptionMap=%v", got.DescriptionMap)
	}
	if _, ok := got.DescriptionMap["gaji"]; ok {
		t.Fatalf("a description without a category is not mapped: %v", got.DescriptionMap)
	}
}

func TestBuildQuickSuggestionsCaps(t *testing.T) {
	var rows []quickRow
	for i := 0; i < 80; i++ {
		rows = append(rows, quickRow{Description: fmt.Sprintf("toko %d", i), Amount: "1000", Type: "EXPENSE", CategoryID: fmt.Sprintf("cat-%d", i)})
	}
	got := buildQuickSuggestions(rows)
	if len(got.Frequent) != 5 || len(got.RecentCategories) != 6 || len(got.DescriptionMap) != 50 {
		t.Fatalf("caps: frequent=%d categories=%d map=%d", len(got.Frequent), len(got.RecentCategories), len(got.DescriptionMap))
	}
	if _, ok := got.DescriptionMap["toko 0"]; !ok {
		t.Fatal("the cap keeps the most recent patterns")
	}
	if _, ok := got.DescriptionMap["toko 79"]; ok {
		t.Fatal("the cap drops the oldest patterns")
	}
}

func TestQuickSuggestionsRequiresHousehold(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/quick-suggestions", nil)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: "no-household"}))
	response := httptest.NewRecorder()
	NewHandler(nil).QuickSuggestions(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestQuickSuggestionsHouseholdScopedAndActiveOnly(t *testing.T) {
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
	newHousehold := func(label string) (household, user string) {
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Quick %s %d", label, stamp)).Scan(&household); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Quick Test','unused') RETURNING id`, fmt.Sprintf("quick-%s-%d@example.test", label, stamp)).Scan(&user); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
			t.Fatal(err)
		}
		return household, user
	}
	call := func(household, user string) quickSuggestions {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/quick-suggestions", nil)
		principal := auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
		response := httptest.NewRecorder()
		NewHandler(pool).QuickSuggestions(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		var out quickSuggestions
		if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	householdA, userA := newHousehold("a")
	householdB, userB := newHousehold("b")

	empty := call(householdB, userB)
	if len(empty.Frequent) != 0 || len(empty.RecentCategories) != 0 || len(empty.DescriptionMap) != 0 {
		t.Fatalf("an empty household gets empty suggestions: %#v", empty)
	}

	var active, inactive, otherHousehold, activeAccount, inactiveAccount string
	if err := pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id`, householdA).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug,active) VALUES($1,'Lama','lama',false) RETURNING id`, householdA).Scan(&inactive); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Rahasia','rahasia') RETURNING id`, householdB).Scan(&otherHousehold); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Aktif','BANK','SPENDING_ONLY') RETURNING id`, householdA).Scan(&activeAccount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy,active) VALUES($1,'Lama','BANK','SPENDING_ONLY',false) RETURNING id`, householdA).Scan(&inactiveAccount); err != nil {
		t.Fatal(err)
	}
	insert := func(household, status, kind, description string, category, account any, amount int) {
		if _, err := pool.Exec(ctx, `INSERT INTO transaction(household_id,account_id,category_id,type,status,amount,transaction_at,description,confirmed_at) VALUES($1,$2,$3,$4,$5,$6,now(),$7,now())`, household, account, category, kind, status, amount, description); err != nil {
			t.Fatal(err)
		}
	}
	insert(householdA, "CONFIRMED", "EXPENSE", "Nasi padang", active, activeAccount, 30000)
	insert(householdA, "CONFIRMED", "EXPENSE", "Warung lama", inactive, inactiveAccount, 12000)
	insert(householdA, "VOIDED", "EXPENSE", "Dibatalkan", active, nil, 99000)
	insert(householdA, "CONFIRMED", "TRANSFER", "Pindah tabungan", nil, activeAccount, 500000)
	insert(householdB, "CONFIRMED", "EXPENSE", "Punya tetangga", otherHousehold, nil, 77000)

	got := call(householdA, userA)
	descriptions := map[string]quickFrequent{}
	for _, item := range got.Frequent {
		descriptions[item.Description] = item
	}
	if len(got.Frequent) != 2 || descriptions["Nasi padang"].CategoryID == nil || *descriptions["Nasi padang"].CategoryID != active {
		t.Fatalf("frequent=%#v", got.Frequent)
	}
	if lama := descriptions["Warung lama"]; lama.CategoryID != nil || lama.AccountID != nil {
		t.Fatalf("an inactive category/account is never suggested: %#v", lama)
	}
	if !reflect.DeepEqual(got.RecentCategories, []string{active}) {
		t.Fatalf("recentCategories=%v", got.RecentCategories)
	}
	if !reflect.DeepEqual(got.DescriptionMap, map[string]string{"nasi padang": active}) {
		t.Fatalf("descriptionMap=%v", got.DescriptionMap)
	}
	for _, leak := range []string{"Dibatalkan", "Pindah tabungan", "Punya tetangga"} {
		if _, ok := descriptions[leak]; ok {
			t.Fatalf("%q must not appear: %#v", leak, got.Frequent)
		}
	}
	if body, _ := json.Marshal(got); strings.Contains(string(body), otherHousehold) {
		t.Fatalf("another household's category leaked: %s", body)
	}
}
