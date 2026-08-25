package household

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

func integrationPool(t *testing.T) *pgxpool.Pool {
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

func seedIdentity(t *testing.T, pool *pgxpool.Pool, role string) (string, string, auth.Principal) {
	t.Helper()
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var householdID, userID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Household %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Test User','unused') RETURNING id`, fmt.Sprintf("member-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,$3)`, householdID, userID, role); err != nil {
		t.Fatal(err)
	}
	p := auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: role}}}
	return householdID, userID, p
}

func requestWithPrincipal(method, target string, body any, principal auth.Principal) *http.Request {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, target, bytes.NewReader(raw))
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	return r.WithContext(auth.ContextWithPrincipal(r.Context(), principal))
}

func TestOwnerAddsMemberAndMemberCannot(t *testing.T) {
	pool := integrationPool(t)
	householdID, _, owner := seedIdentity(t, pool, "OWNER")
	handler := NewHandler(pool, "richmod_bot")
	stamp := time.Now().UnixNano()
	response := httptest.NewRecorder()
	handler.Members(response, requestWithPrincipal(http.MethodPost, "/api/v1/household/members", map[string]any{"displayName": "Spouse", "email": fmt.Sprintf("spouse-%d@example.test", stamp)}, owner))
	if response.Code != http.StatusCreated {
		t.Fatalf("owner status=%d body=%s", response.Code, response.Body.String())
	}
	var memberID string
	if err := pool.QueryRow(context.Background(), `SELECT hm.user_id FROM household_member hm JOIN "user" u ON u.id=hm.user_id WHERE hm.household_id=$1 AND u.display_name='Spouse'`, householdID).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	var audits int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE household_id=$1 AND entity_type='household_member' AND entity_id=$2`, householdID, memberID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}

	_, _, member := seedIdentity(t, pool, "MEMBER")
	denied := httptest.NewRecorder()
	handler.Members(denied, requestWithPrincipal(http.MethodPost, "/api/v1/household/members", map[string]any{"displayName": "Denied", "email": fmt.Sprintf("denied-%d@example.test", stamp)}, member))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("member status=%d", denied.Code)
	}
}

func TestOwnerCannotDeactivateCrossHouseholdMember(t *testing.T) {
	pool := integrationPool(t)
	_, _, owner := seedIdentity(t, pool, "OWNER")
	otherHousehold, otherUser, _ := seedIdentity(t, pool, "MEMBER")
	_ = otherHousehold
	handler := NewHandler(pool, "")
	request := requestWithPrincipal(http.MethodPatch, "/api/v1/household/members/"+otherUser, map[string]any{"active": false}, owner)
	request.SetPathValue("id", otherUser)
	response := httptest.NewRecorder()
	handler.PatchMember(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var active bool
	if err := pool.QueryRow(context.Background(), `SELECT active FROM household_member WHERE user_id=$1`, otherUser).Scan(&active); err != nil || !active {
		t.Fatalf("active=%v err=%v", active, err)
	}
}
