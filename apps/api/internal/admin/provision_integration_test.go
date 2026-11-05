package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func provisionPool(t *testing.T) *pgxpool.Pool {
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

type createdHousehold struct {
	Household struct{ ID, Name string } `json:"household"`
	Owner     struct {
		ID, Email, DisplayName string
		TelegramLinked         bool `json:"telegramLinked"`
	} `json:"owner"`
	Invite struct {
		Link      string    `json:"link"`
		ExpiresAt time.Time `json:"expiresAt"`
	} `json:"invite"`
}

// TestSuperAdminCreatesHouseholdAndOwnerEnrollsByInvite covers the console
// path end to end: authorization, provisioning, audit, the invite-only owner
// enrollment, reissue, and every conflict and validation refusal.
func TestSuperAdminCreatesHouseholdAndOwnerEnrollsByInvite(t *testing.T) {
	pool := provisionPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var adminID, adminHousehold string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at,is_super_admin) VALUES($1,'Admin','x',now(),true) RETURNING id`, fmt.Sprintf("provision-admin-%d@example.test", stamp)).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Admin home %d", stamp)).Scan(&adminHousehold); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, adminHousehold, adminID); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(pool, false, "responses")
	superAdmin := auth.Principal{UserID: adminID, IsSuperAdmin: true, HasHousehold: true, HouseholdID: adminHousehold, HouseholdRole: "OWNER"}
	call := func(p auth.Principal, path, body string, handler http.HandlerFunc, pathValue string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		if pathValue != "" {
			request.SetPathValue("householdId", pathValue)
		}
		request = request.WithContext(auth.ContextWithPrincipal(ctx, p))
		response := httptest.NewRecorder()
		h.Require(handler).ServeHTTP(response, request)
		return response
	}
	ownerEmail := fmt.Sprintf("new-owner-%d@example.test", stamp)
	telegramID := stamp%6_000_000_000 + 17
	body := fmt.Sprintf(`{"householdName":"Keluarga Baru %d","ownerName":"Pemilik Baru","ownerEmail":%q,"telegramUserId":%d}`, stamp, ownerEmail, telegramID)

	// A household owner who is not a Super Admin cannot provision tenants.
	owner := superAdmin
	owner.IsSuperAdmin = false
	if response := call(owner, "/api/v1/admin/households", body, h.CreateHousehold, ""); response.Code != http.StatusForbidden {
		t.Fatalf("non-super-admin: %d %s", response.Code, response.Body.String())
	}

	response := call(superAdmin, "/api/v1/admin/households", body, h.CreateHousehold, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	var created createdHousehold
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Household.ID == "" || created.Household.ID == adminHousehold || !strings.HasPrefix(created.Invite.Link, "/invite#") || !created.Owner.TelegramLinked {
		t.Fatalf("unexpected response: %+v", created)
	}
	var role string
	var initialized, adminIsMember bool
	var categories, householdAudits, platformAudits int
	if err := pool.QueryRow(ctx, `SELECT hm.role,u.password_initialized_at IS NOT NULL,
		EXISTS(SELECT 1 FROM household_member WHERE household_id=$1 AND user_id=$3),
		(SELECT count(*) FROM category WHERE household_id=$1),
		(SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='CREATE_HOUSEHOLD' AND actor_type='USER' AND actor_id=$3),
		(SELECT count(*) FROM platform_audit_log WHERE action='ADMIN_CREATE_HOUSEHOLD' AND entity_id=$1::text AND actor_user_id=$3)
		FROM household_member hm JOIN "user" u ON u.id=hm.user_id JOIN telegram_identity ti ON ti.user_id=u.id AND ti.household_id=hm.household_id AND ti.telegram_user_id=$4
		WHERE hm.household_id=$1 AND u.email=$2`, created.Household.ID, ownerEmail, adminID, telegramID).Scan(&role, &initialized, &adminIsMember, &categories, &householdAudits, &platformAudits); err != nil {
		t.Fatal(err)
	}
	if role != "OWNER" || initialized || adminIsMember || categories == 0 || householdAudits != 1 || platformAudits != 1 {
		t.Fatalf("role=%s initialized=%v adminIsMember=%v categories=%d audits=%d/%d", role, initialized, adminIsMember, categories, householdAudits, platformAudits)
	}

	// The owner cannot log in until enrolling through the invite.
	service := auth.NewService(pool)
	if _, _, _, err := service.Login(ctx, ownerEmail, "anything-at-all-123"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("login before enrollment: %v", err)
	}
	// Reissue while not enrolled replaces the first invite.
	reissue := call(superAdmin, "/api/v1/admin/households/x/owner-invite", "", h.ReissueOwnerInvite, created.Household.ID)
	if reissue.Code != http.StatusOK {
		t.Fatalf("reissue: %d %s", reissue.Code, reissue.Body.String())
	}
	var reissued struct{ Link string }
	if err := json.Unmarshal(reissue.Body.Bytes(), &reissued); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.AcceptDashboardInvite(ctx, strings.TrimPrefix(created.Invite.Link, "/invite#"), "a-strong-owner-password"); err == nil {
		t.Fatal("the replaced invite still enrolled the owner")
	}
	principal, _, _, err := service.AcceptDashboardInvite(ctx, strings.TrimPrefix(reissued.Link, "/invite#"), "a-strong-owner-password")
	if err != nil {
		t.Fatalf("accept reissued invite: %v", err)
	}
	if !principal.HasHousehold || principal.HouseholdID != created.Household.ID || principal.HouseholdRole != "OWNER" || principal.IsSuperAdmin {
		t.Fatalf("enrolled principal = %+v", principal)
	}
	if response := call(superAdmin, "/api/v1/admin/households/x/owner-invite", "", h.ReissueOwnerInvite, created.Household.ID); response.Code != http.StatusConflict {
		t.Fatalf("reissue after enrollment: %d %s", response.Code, response.Body.String())
	}

	// Refusals: an existing user is never attached, a linked Telegram ID is
	// never reused, and malformed input is rejected.
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"existing email", fmt.Sprintf(`{"householdName":"Lain","ownerName":"Lain","ownerEmail":%q}`, ownerEmail), http.StatusConflict, "USER_ALREADY_EXISTS"},
		{"super admin's own email", fmt.Sprintf(`{"householdName":"Lain","ownerName":"Lain","ownerEmail":"provision-admin-%d@example.test"}`, stamp), http.StatusConflict, "USER_ALREADY_EXISTS"},
		{"linked Telegram ID", fmt.Sprintf(`{"householdName":"Lain","ownerName":"Lain","ownerEmail":"other-%d@example.test","telegramUserId":%d}`, stamp, telegramID), http.StatusConflict, "TELEGRAM_ID_ALREADY_LINKED"},
		{"missing name", fmt.Sprintf(`{"householdName":" ","ownerName":"Lain","ownerEmail":"x-%d@example.test"}`, stamp), http.StatusBadRequest, "INVALID_HOUSEHOLD_REQUEST"},
		{"bad email", `{"householdName":"Lain","ownerName":"Lain","ownerEmail":"not-an-email"}`, http.StatusBadRequest, "INVALID_HOUSEHOLD_REQUEST"},
		{"dotless domain", `{"householdName":"Lain","ownerName":"Lain","ownerEmail":"a@b"}`, http.StatusBadRequest, "INVALID_HOUSEHOLD_REQUEST"},
		{"display-name email", `{"householdName":"Lain","ownerName":"Lain","ownerEmail":"Lain <lain@example.test>"}`, http.StatusBadRequest, "INVALID_HOUSEHOLD_REQUEST"},
		{"negative Telegram ID", fmt.Sprintf(`{"householdName":"Lain","ownerName":"Lain","ownerEmail":"y-%d@example.test","telegramUserId":-1}`, stamp), http.StatusBadRequest, "INVALID_HOUSEHOLD_REQUEST"},
		{"unknown field", fmt.Sprintf(`{"householdName":"Lain","ownerName":"Lain","ownerEmail":"z-%d@example.test","role":"SUPER"}`, stamp), http.StatusBadRequest, "INVALID_HOUSEHOLD_REQUEST"},
	} {
		response := call(superAdmin, "/api/v1/admin/households", tc.body, h.CreateHousehold, "")
		if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.code) {
			t.Errorf("%s: %d %s, want %d %s", tc.name, response.Code, response.Body.String(), tc.status, tc.code)
		}
	}
	if response := call(superAdmin, "/api/v1/admin/households/x/owner-invite", "", h.ReissueOwnerInvite, "00000000-0000-4000-8000-000000000000"); response.Code != http.StatusNotFound {
		t.Fatalf("reissue for unknown household: %d %s", response.Code, response.Body.String())
	}

	// A removed member's Telegram ID is an inactive identity, not a link: it is
	// rebound to the new owner instead of being refused forever.
	var formerMember string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Former','x') RETURNING id`, fmt.Sprintf("former-%d@example.test", stamp)).Scan(&formerMember); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role,active,deactivated_at) VALUES($1,$2,'MEMBER',false,now())`, adminHousehold, formerMember); err != nil {
		t.Fatal(err)
	}
	retiredTelegram := telegramID + 1
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id,active) VALUES($1,$2,$3,false)`, retiredTelegram, adminHousehold, formerMember); err != nil {
		t.Fatal(err)
	}
	rebindEmail := fmt.Sprintf("rebind-owner-%d@example.test", stamp)
	response = call(superAdmin, "/api/v1/admin/households", fmt.Sprintf(`{"householdName":"Rebind %d","ownerName":"Rebind","ownerEmail":%q,"telegramUserId":%d}`, stamp, rebindEmail, retiredTelegram), h.CreateHousehold, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create with a retired Telegram ID: %d %s", response.Code, response.Body.String())
	}
	var rebound createdHousehold
	if err := json.Unmarshal(response.Body.Bytes(), &rebound); err != nil {
		t.Fatal(err)
	}
	var identityHousehold, identityUser string
	var identityActive bool
	if err := pool.QueryRow(ctx, `SELECT household_id::text,user_id::text,active FROM telegram_identity WHERE telegram_user_id=$1`, retiredTelegram).Scan(&identityHousehold, &identityUser, &identityActive); err != nil {
		t.Fatal(err)
	}
	if identityHousehold != rebound.Household.ID || identityUser != rebound.Owner.ID || !identityActive {
		t.Fatalf("retired identity not rebound: household=%s user=%s active=%v", identityHousehold, identityUser, identityActive)
	}
}
