package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestCreateHouseholdForExistingUsersWithoutActiveHousehold(t *testing.T) {
	pool := provisionPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var adminID, adminHousehold string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at,is_super_admin) VALUES($1,'Admin','x',now(),true) RETURNING id`, fmt.Sprintf("existing-admin-%d@example.test", stamp)).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Existing admin home %d", stamp)).Scan(&adminHousehold); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, adminHousehold, adminID); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(pool, false, "responses")
	superAdmin := auth.Principal{UserID: adminID, IsSuperAdmin: true, HasHousehold: true, HouseholdID: adminHousehold, HouseholdRole: "OWNER"}
	create := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/households", bytes.NewBufferString(body))
		request = request.WithContext(auth.ContextWithPrincipal(ctx, superAdmin))
		response := httptest.NewRecorder()
		h.Require(http.HandlerFunc(h.CreateHousehold)).ServeHTTP(response, request)
		return response
	}

	googleEmail := fmt.Sprintf("google-new-%d@example.test", stamp)
	var googleUser string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,google_sub,google_linked_at) VALUES($1,'Google','google-hash',$2,now()) RETURNING id`, googleEmail, fmt.Sprintf("admin-sub-%d", stamp)).Scan(&googleUser); err != nil {
		t.Fatal(err)
	}
	response := create(fmt.Sprintf(`{"householdName":"Google Home %d","ownerName":"Google Owner","ownerEmail":%q}`, stamp, googleEmail))
	if response.Code != http.StatusCreated {
		t.Fatalf("create for google user: %d %s", response.Code, response.Body.String())
	}
	var created createdHousehold
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Owner.ID != googleUser || !strings.HasPrefix(created.Invite.Link, "/invite#") {
		t.Fatalf("unexpected response: %+v", created)
	}
	var googleLinked, enrolled bool
	var memberRole string
	if err := pool.QueryRow(ctx, `SELECT u.google_sub IS NOT NULL,(u.password_initialized_at IS NOT NULL OR u.google_sub IS NOT NULL),hm.role FROM "user" u JOIN household_member hm ON hm.user_id=u.id AND hm.household_id=$2 WHERE u.id=$1`, googleUser, created.Household.ID).Scan(&googleLinked, &enrolled, &memberRole); err != nil {
		t.Fatal(err)
	}
	if !googleLinked || !enrolled || memberRole != "OWNER" {
		t.Fatalf("googleLinked=%v enrolled=%v role=%s", googleLinked, enrolled, memberRole)
	}

	if response := create(fmt.Sprintf(`{"householdName":"Again %d","ownerName":"Google Owner","ownerEmail":%q}`, stamp, googleEmail)); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "USER_ALREADY_EXISTS") {
		t.Fatalf("second household for the same user: %d %s", response.Code, response.Body.String())
	}

	var oldHousehold, formerUser string
	formerEmail := fmt.Sprintf("former-owner-%d@example.test", stamp)
	telegramID := stamp%6_000_000_000 + 211
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Old home %d", stamp)).Scan(&oldHousehold); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES($1,'Former','former-hash',now()) RETURNING id`, formerEmail).Scan(&formerUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role,active,deactivated_at) VALUES($1,$2,'MEMBER',false,now())`, oldHousehold, formerUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id,active) VALUES($1,$2,$3,false)`, telegramID, oldHousehold, formerUser); err != nil {
		t.Fatal(err)
	}
	response = create(fmt.Sprintf(`{"householdName":"Moved Home %d","ownerName":"Former Owner","ownerEmail":%q,"telegramUserId":%d}`, stamp, formerEmail, telegramID))
	if response.Code != http.StatusCreated {
		t.Fatalf("create for reassigned member: %d %s", response.Code, response.Body.String())
	}
	var moved createdHousehold
	if err := json.Unmarshal(response.Body.Bytes(), &moved); err != nil {
		t.Fatal(err)
	}
	if moved.Owner.ID != formerUser || !moved.Owner.TelegramLinked || !strings.HasPrefix(moved.Invite.Link, "/invite#") {
		t.Fatalf("unexpected response: %+v", moved)
	}
	var identityHousehold string
	var identityActive bool
	if err := pool.QueryRow(ctx, `SELECT household_id::text,active FROM telegram_identity WHERE telegram_user_id=$1`, telegramID).Scan(&identityHousehold, &identityActive); err != nil {
		t.Fatal(err)
	}
	if identityHousehold != moved.Household.ID || !identityActive {
		t.Fatalf("telegram identity household=%s active=%v, want %s true", identityHousehold, identityActive, moved.Household.ID)
	}
}
