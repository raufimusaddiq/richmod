package household

import (
	"bytes"
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
	"github.com/raufimusaddiq/richmod/apps/api/internal/telegram"
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

// A household created without a Telegram ID must not strand its owner: the
// owner links through the same one-time invite members use (ADR-059).
func TestOwnerWithoutTelegramLinksThroughInvite(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	householdID, ownerID, owner := seedIdentity(t, pool, "OWNER")
	handler := NewHandler(pool, "richmod_bot")
	invite := func() *httptest.ResponseRecorder {
		request := requestWithPrincipal(http.MethodPost, "/api/v1/household/members/"+ownerID+"/telegram-invite", nil, owner)
		request.SetPathValue("id", ownerID)
		response := httptest.NewRecorder()
		handler.CreateInvite(response, request)
		return response
	}
	response := invite()
	if response.Code != http.StatusCreated {
		t.Fatalf("owner invite status=%d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.Token == "" {
		t.Fatalf("token=%q err=%v", created.Token, err)
	}
	stamp := time.Now().UnixNano()
	telegramID := stamp%8_000_000_000 + 1_000_000_000
	linked, err := telegram.NewPostgreSQLStore(pool).Link(ctx, telegram.CaptureInput{UpdateID: stamp, TelegramUserID: telegramID}, created.Token)
	if err != nil || !linked {
		t.Fatalf("linked=%v err=%v", linked, err)
	}
	var boundUser string
	if err = pool.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, telegramID, householdID).Scan(&boundUser); err != nil || boundUser != ownerID {
		t.Fatalf("bound=%s err=%v", boundUser, err)
	}
	// Linked is final: the 1:1 rule still refuses a second invite.
	if response = invite(); response.Code != http.StatusConflict {
		t.Fatalf("second invite status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDuplicateMemberEmailExplainsWhy(t *testing.T) {
	pool := integrationPool(t)
	_, _, owner := seedIdentity(t, pool, "OWNER")
	_, _, otherOwner := seedIdentity(t, pool, "OWNER")
	handler := NewHandler(pool, "richmod_bot")
	email := fmt.Sprintf("dup-%d@example.test", time.Now().UnixNano())
	add := func(p auth.Principal) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.Members(w, requestWithPrincipal(http.MethodPost, "/api/v1/household/members", map[string]any{"displayName": "Family", "email": email}, p))
		return w
	}
	if w := add(owner); w.Code != http.StatusCreated {
		t.Fatalf("first add status=%d body=%s", w.Code, w.Body.String())
	}
	same := add(owner)
	if same.Code != http.StatusConflict || !strings.Contains(same.Body.String(), "sudah menjadi anggota rumah tangga ini") {
		t.Fatalf("same household status=%d body=%s", same.Code, same.Body.String())
	}
	other := add(otherOwner)
	if other.Code != http.StatusConflict || !strings.Contains(other.Body.String(), "dipakai akun lain") {
		t.Fatalf("other household status=%d body=%s", other.Code, other.Body.String())
	}
}

func TestDuplicateMemberEmailDoesNotDeadlockSmallPool(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	seedPool := integrationPool(t)
	_, _, owner := seedIdentity(t, seedPool, "OWNER")
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	small, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(small.Close)
	handler := NewHandler(small, "richmod_bot")
	email := fmt.Sprintf("small-%d@example.test", time.Now().UnixNano())
	for i, want := range []int{http.StatusCreated, http.StatusConflict} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req := requestWithPrincipal(http.MethodPost, "/api/v1/household/members", map[string]any{"displayName": "Family", "email": email}, owner).WithContext(auth.ContextWithPrincipal(ctx, owner))
		w := httptest.NewRecorder()
		handler.Members(w, req)
		cancel()
		if w.Code != want {
			t.Fatalf("attempt %d status=%d want %d body=%s", i, w.Code, want, w.Body.String())
		}
		if want == http.StatusConflict && !strings.Contains(w.Body.String(), "sudah menjadi anggota rumah tangga ini") {
			t.Fatalf("duplicate lookup did not complete on a one-connection pool: %s", w.Body.String())
		}
	}
}

func TestMemberListShowsInviteState(t *testing.T) {
	pool := integrationPool(t)
	householdID, _, owner := seedIdentity(t, pool, "OWNER")
	handler := NewHandler(pool, "richmod_bot")
	email := fmt.Sprintf("invitee-%d@example.test", time.Now().UnixNano())
	add := httptest.NewRecorder()
	handler.Members(add, requestWithPrincipal(http.MethodPost, "/api/v1/household/members", map[string]any{"displayName": "Invitee", "email": email}, owner))
	if add.Code != http.StatusCreated {
		t.Fatalf("add status=%d body=%s", add.Code, add.Body.String())
	}
	var created struct{ ID string }
	_ = json.Unmarshal(add.Body.Bytes(), &created)
	listed := func() map[string]any {
		w := httptest.NewRecorder()
		handler.Members(w, requestWithPrincipal(http.MethodGet, "/api/v1/household/members", nil, owner))
		var items []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item["id"] == created.ID {
				return item
			}
		}
		t.Fatal("member missing from list")
		return nil
	}
	before := listed()
	if before["passwordInitialized"] != false || before["dashboardInviteExpiresAt"] != nil || before["telegramInviteExpiresAt"] != nil {
		t.Fatalf("fresh member state wrong: %v", before)
	}
	for _, path := range []string{"/api/v1/household/members/%s/dashboard-invite", "/api/v1/household/members/%s/telegram-invite"} {
		req := requestWithPrincipal(http.MethodPost, fmt.Sprintf(path, created.ID), nil, owner)
		req.SetPathValue("id", created.ID)
		w := httptest.NewRecorder()
		if strings.Contains(path, "dashboard") {
			handler.CreateDashboardInvite(w, req)
		} else {
			handler.CreateInvite(w, req)
		}
		if w.Code != http.StatusCreated {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
	after := listed()
	if after["dashboardInviteExpiresAt"] == nil || after["telegramInviteExpiresAt"] == nil || after["dashboardInviteStatus"] != "PENDING" {
		t.Fatalf("pending invites not reported: %v", after)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE "user" SET password_initialized_at=now() WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	if listed()["passwordInitialized"] != true {
		t.Fatal("passwordInitialized not reported")
	}
	_ = householdID
}
