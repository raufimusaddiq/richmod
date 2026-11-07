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
	if cache := response.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("invite response Cache-Control = %q, want no-store", cache)
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
	if reissue.Code != http.StatusOK || !strings.Contains(reissue.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("reissue: %d %s (Cache-Control %q)", reissue.Code, reissue.Body.String(), reissue.Header().Get("Cache-Control"))
	}
	var reissued struct{ Link string }
	if err := json.Unmarshal(reissue.Body.Bytes(), &reissued); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.AcceptDashboardInvite(ctx, strings.TrimPrefix(created.Invite.Link, "/invite#"), "a-strong-owner-password"); err == nil {
		t.Fatal("the replaced invite still enrolled the owner")
	}
	principal, ownerSession, _, err := service.AcceptDashboardInvite(ctx, strings.TrimPrefix(reissued.Link, "/invite#"), "a-strong-owner-password")
	if err != nil {
		t.Fatalf("accept reissued invite: %v", err)
	}
	if !principal.HasHousehold || principal.HouseholdID != created.Household.ID || principal.HouseholdRole != "OWNER" || principal.IsSuperAdmin {
		t.Fatalf("enrolled principal = %+v", principal)
	}
	if response := call(superAdmin, "/api/v1/admin/households/x/owner-invite", "", h.ReissueOwnerInvite, created.Household.ID); response.Code != http.StatusConflict {
		t.Fatalf("reissue after enrollment: %d %s", response.Code, response.Body.String())
	}

	// A forgotten owner password is reset only by an explicit request, and
	// accepting the link replaces the password and ends the old session.
	if _, _, err := service.Authenticate(ctx, ownerSession); err != nil {
		t.Fatalf("owner session before reset: %v", err)
	}
	resetCall := call(superAdmin, "/api/v1/admin/households/x/owner-invite", `{"reset":true}`, h.ReissueOwnerInvite, created.Household.ID)
	if resetCall.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", resetCall.Code, resetCall.Body.String())
	}
	var resetInvite struct{ Link string }
	if err := json.Unmarshal(resetCall.Body.Bytes(), &resetInvite); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.Login(ctx, ownerEmail, "a-strong-owner-password"); err != nil {
		t.Fatalf("the old password must keep working until the reset link is used: %v", err)
	}
	if _, newSession, _, err := service.AcceptDashboardInvite(ctx, strings.TrimPrefix(resetInvite.Link, "/invite#"), "a-brand-new-owner-password"); err != nil {
		t.Fatalf("accept reset link: %v", err)
	} else if _, _, err := service.Authenticate(ctx, newSession); err != nil {
		t.Fatalf("session issued by the reset: %v", err)
	}
	if _, _, err := service.Authenticate(ctx, ownerSession); err == nil {
		t.Fatal("the pre-reset session survived a password reset")
	}
	if _, _, _, err := service.Login(ctx, ownerEmail, "a-strong-owner-password"); err == nil {
		t.Fatal("the old password still works after a reset")
	}
	var resetAudits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_audit_log WHERE action='ADMIN_RESET_OWNER_PASSWORD' AND entity_id=$1::text`, created.Household.ID).Scan(&resetAudits); err != nil || resetAudits != 1 {
		t.Fatalf("reset audits=%d err=%v", resetAudits, err)
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

	// Telegram ID and user are bound 1:1 for good: a removed member's
	// (inactive) Telegram ID is still bound and is never given to a new owner.
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
	response = call(superAdmin, "/api/v1/admin/households", fmt.Sprintf(`{"householdName":"Retired %d","ownerName":"Retired","ownerEmail":"retired-owner-%d@example.test","telegramUserId":%d}`, stamp, stamp, retiredTelegram), h.CreateHousehold, "")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "TELEGRAM_ID_ALREADY_LINKED") {
		t.Fatalf("create with a removed member's Telegram ID: %d %s", response.Code, response.Body.String())
	}
	var identityUser string
	var identityActive bool
	if err := pool.QueryRow(ctx, `SELECT user_id::text,active FROM telegram_identity WHERE telegram_user_id=$1`, retiredTelegram).Scan(&identityUser, &identityActive); err != nil {
		t.Fatal(err)
	}
	if identityUser != formerMember || identityActive {
		t.Fatalf("bound Telegram ID changed: user=%s active=%v", identityUser, identityActive)
	}
}
