package admin

import (
	"bytes"
	"context"
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

func TestAddMemberEnforcesOneActiveHousehold(t *testing.T) {
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
	var adminID, householdA, householdB, householdC, inactiveUserID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,is_super_admin) VALUES($1,'Admin','!',true) RETURNING id`, fmt.Sprintf("admin-invariant-%d@example.test", stamp)).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	for name, destination := range map[string]*string{"A": &householdA, "B": &householdB, "C": &householdC} {
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Admin invariant %s %d", name, stamp)).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}

	handler := NewHandler(pool, false, "responses")
	call := func(householdID, email, displayName string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/households/"+householdID+"/members", bytes.NewBufferString(fmt.Sprintf(`{"email":%q,"displayName":%q}`, email, displayName)))
		request.SetPathValue("householdId", householdID)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: adminID, IsSuperAdmin: true}))
		response := httptest.NewRecorder()
		handler.AddMember(response, request)
		return response
	}

	memberEmail := fmt.Sprintf("member-invariant-%d@example.test", stamp)
	if response := call(householdA, memberEmail, "Member"); response.Code != http.StatusCreated {
		t.Fatalf("zero-membership add status=%d body=%s", response.Code, response.Body.String())
	}
	if response := call(householdA, memberEmail, "Member"); response.Code != http.StatusCreated {
		t.Fatalf("same-household idempotent add status=%d body=%s", response.Code, response.Body.String())
	}
	if response := call(householdB, memberEmail, "Member"); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "USER_ALREADY_HAS_HOUSEHOLD") {
		t.Fatalf("cross-household add status=%d body=%s", response.Code, response.Body.String())
	}

	inactiveEmail := fmt.Sprintf("inactive-invariant-%d@example.test", stamp)
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Inactive Member','!') RETURNING id`, inactiveEmail).Scan(&inactiveUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role,active,deactivated_at) VALUES($1,$2,'MEMBER',false,now())`, householdC, inactiveUserID); err != nil {
		t.Fatal(err)
	}
	if response := call(householdC, inactiveEmail, "Inactive Member"); response.Code != http.StatusCreated {
		t.Fatalf("inactive target reactivation status=%d body=%s", response.Code, response.Body.String())
	}
	var active bool
	if err := pool.QueryRow(ctx, `SELECT active FROM household_member WHERE household_id=$1 AND user_id=$2`, householdC, inactiveUserID).Scan(&active); err != nil || !active {
		t.Fatalf("reactivated membership active=%v err=%v", active, err)
	}
}
