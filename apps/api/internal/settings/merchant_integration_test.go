package settings

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestMerchantAliasCanBeDisabledOnlyWithinHousehold(t *testing.T) {
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

	seed := func(suffix string) (string, string, string) {
		t.Helper()
		var householdID, userID, aliasID string
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, "Alias "+suffix).Scan(&householdID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("alias-%d-%s@example.test", stamp, suffix)).Scan(&userID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `WITH m AS (INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id) INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,auto_apply,created_from_user_confirmation) SELECT $1,$2,id,true,true FROM m RETURNING id`, householdID, "Merchant "+suffix).Scan(&aliasID); err != nil {
			t.Fatal(err)
		}
		return householdID, userID, aliasID
	}

	householdID, userID, aliasID := seed("owner")
	otherHousehold, otherUser, _ := seed("other")
	handler := NewHandler(pool)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/merchant-aliases/"+aliasID, bytes.NewBufferString(`{"autoApply":false}`))
	request.SetPathValue("id", aliasID)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}))
	response := httptest.NewRecorder()
	handler.PatchMerchantAlias(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var active bool
	if err := pool.QueryRow(ctx, `SELECT auto_apply FROM merchant_alias WHERE id=$1`, aliasID).Scan(&active); err != nil || active {
		t.Fatalf("alias active=%t err=%v", active, err)
	}

	foreign := httptest.NewRequest(http.MethodPatch, "/api/v1/merchant-aliases/"+aliasID, bytes.NewBufferString(`{"autoApply":true}`))
	foreign.SetPathValue("id", aliasID)
	foreign = foreign.WithContext(auth.ContextWithPrincipal(foreign.Context(), auth.Principal{UserID: otherUser, Memberships: []auth.Membership{{HouseholdID: otherHousehold, Role: "OWNER"}}}))
	foreignResponse := httptest.NewRecorder()
	handler.PatchMerchantAlias(foreignResponse, foreign)
	if foreignResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-household status=%d", foreignResponse.Code)
	}
}
