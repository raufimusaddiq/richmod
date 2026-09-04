package integrationaction

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestActionAuthorizationAndHouseholdScope(t *testing.T) {
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
	var householdA, householdB, owner, member, otherOwner, actionID string
	for name, target := range map[string]*string{"Action A ": &householdA, "Action B ": &householdB} {
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1||substr(gen_random_uuid()::text,1,8)) RETURNING id`, name).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	for label, target := range map[string]*string{"owner-action": &owner, "member-action": &member, "other-action": &otherOwner} {
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES(($1||'-'||substr(gen_random_uuid()::text,1,8)||'@test.invalid'),$1,'x',now()) RETURNING id`, label).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER'),($1,$3,'MEMBER'),($4,$5,'OWNER')`, householdA, owner, member, householdB, otherOwner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO integration_action(household_id,integration_type,action_type,status,title,description,action_url,action_code,dedupe_key) VALUES($1,'EMAIL_FORWARDING','VERIFY_FORWARDING','OPEN','Verify','Description','https://mail-settings.google.com/mail/vf-test','123456','test') RETURNING id`, householdA).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(pool)
	request := func(method, path, userID, household, role string) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		r.SetPathValue("id", actionID)
		return r.WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: household, Role: role}}}))
	}
	memberResponse := httptest.NewRecorder()
	handler.List(memberResponse, request(http.MethodGet, "/api/v1/integration-actions", member, householdA, "MEMBER"))
	var memberItems []Action
	_ = json.Unmarshal(memberResponse.Body.Bytes(), &memberItems)
	if memberResponse.Code != 200 || len(memberItems) != 1 || memberItems[0].ActionURL != nil || memberItems[0].ActionCode != nil {
		t.Fatalf("member response=%d items=%#v", memberResponse.Code, memberItems)
	}
	ownerResponse := httptest.NewRecorder()
	handler.List(ownerResponse, request(http.MethodGet, "/api/v1/integration-actions", owner, householdA, "OWNER"))
	var ownerItems []Action
	_ = json.Unmarshal(ownerResponse.Body.Bytes(), &ownerItems)
	if ownerResponse.Code != 200 || len(ownerItems) != 1 || ownerItems[0].ActionURL == nil || ownerItems[0].ActionCode == nil {
		t.Fatalf("owner response=%d items=%#v", ownerResponse.Code, ownerItems)
	}
	otherResponse := httptest.NewRecorder()
	handler.List(otherResponse, request(http.MethodGet, "/api/v1/integration-actions", otherOwner, householdB, "OWNER"))
	var otherItems []Action
	_ = json.Unmarshal(otherResponse.Body.Bytes(), &otherItems)
	if otherResponse.Code != 200 || len(otherItems) != 0 {
		t.Fatalf("other household response=%d items=%#v", otherResponse.Code, otherItems)
	}
	memberResolve := httptest.NewRecorder()
	handler.Resolve(memberResolve, request(http.MethodPost, "/api/v1/integration-actions/"+actionID+"/resolve", member, householdA, "MEMBER"))
	if memberResolve.Code != 403 {
		t.Fatalf("member resolve=%d", memberResolve.Code)
	}
	otherResolve := httptest.NewRecorder()
	handler.Resolve(otherResolve, request(http.MethodPost, "/api/v1/integration-actions/"+actionID+"/resolve", otherOwner, householdB, "OWNER"))
	if otherResolve.Code != 404 {
		t.Fatalf("other resolve=%d", otherResolve.Code)
	}
	ownerResolve := httptest.NewRecorder()
	handler.Resolve(ownerResolve, request(http.MethodPost, "/api/v1/integration-actions/"+actionID+"/resolve", owner, householdA, "OWNER"))
	if ownerResolve.Code != 204 {
		t.Fatalf("owner resolve=%d body=%s", ownerResolve.Code, ownerResolve.Body.String())
	}
}
