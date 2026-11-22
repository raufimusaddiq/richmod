package household

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func seedStandaloneUser(t *testing.T, pool *pgxpool.Pool, label string, active, superAdmin bool) (string, string) {
	t.Helper()
	email := fmt.Sprintf("%s-%d@example.test", label, uniqueStamp())
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO "user"(email,display_name,password_hash,active,is_super_admin,google_sub,google_linked_at) VALUES($1,'Google Name','unused',$2,$3,$4,now()) RETURNING id`, email, active, superAdmin, "sub-"+email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id, email
}

func addMember(t *testing.T, handler *Handler, owner auth.Principal, email string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.Members(response, requestWithPrincipal(http.MethodPost, "/api/v1/household/members", map[string]any{"displayName": "Anggota Baru", "email": email}, owner))
	return response
}

func TestOwnerAddsExistingHouseholdlessGoogleUserAsMember(t *testing.T) {
	t.Parallel()
	pool := integrationPool(t)
	householdID, _, owner := seedIdentity(t, pool, "OWNER")
	handler := NewHandler(pool, "richmod_bot")
	userID, email := seedStandaloneUser(t, pool, "waiting", true, false)

	response := addMember(t, handler, owner, strings.ToUpper(email[:1])+email[1:])
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var role, name string
	var active bool
	if err := pool.QueryRow(context.Background(), `SELECT hm.role,hm.active,u.display_name FROM household_member hm JOIN "user" u ON u.id=hm.user_id WHERE hm.household_id=$1 AND hm.user_id=$2`, householdID, userID).Scan(&role, &active, &name); err != nil {
		t.Fatal(err)
	}
	if role != "MEMBER" || !active || name != "Anggota Baru" {
		t.Fatalf("role=%s active=%v name=%q", role, active, name)
	}
	var audits int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE household_id=$1 AND entity_type='household_member' AND entity_id=$2 AND action='CREATE'`, householdID, userID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}
	var googleLinked bool
	if err := pool.QueryRow(context.Background(), `SELECT google_sub IS NOT NULL FROM "user" WHERE id=$1`, userID).Scan(&googleLinked); err != nil || !googleLinked {
		t.Fatalf("google link lost: %v %v", googleLinked, err)
	}
}

func TestOwnerCanAddUserRemovedFromAnotherHousehold(t *testing.T) {
	t.Parallel()
	pool := integrationPool(t)
	otherHouseholdID, otherUserID, _ := seedIdentity(t, pool, "MEMBER")
	if _, err := pool.Exec(context.Background(), `UPDATE household_member SET active=false,deactivated_at=now() WHERE household_id=$1 AND user_id=$2`, otherHouseholdID, otherUserID); err != nil {
		t.Fatal(err)
	}
	var email string
	if err := pool.QueryRow(context.Background(), `SELECT email FROM "user" WHERE id=$1`, otherUserID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	householdID, _, owner := seedIdentity(t, pool, "OWNER")
	response := addMember(t, NewHandler(pool, "richmod_bot"), owner, email)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var active int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM household_member WHERE user_id=$1 AND active`, otherUserID).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active memberships=%d err=%v", active, err)
	}
	var inNew bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM household_member WHERE household_id=$1 AND user_id=$2 AND active)`, householdID, otherUserID).Scan(&inNew); err != nil || !inNew {
		t.Fatalf("membership in new household missing: %v %v", inNew, err)
	}
}

func TestOwnerCannotClaimUnavailableExistingAccounts(t *testing.T) {
	t.Parallel()
	pool := integrationPool(t)
	_, _, owner := seedIdentity(t, pool, "OWNER")
	householdID := owner.Household.HouseholdID
	handler := NewHandler(pool, "richmod_bot")
	const otherAccount = "email sudah dipakai akun lain; gunakan email berbeda untuk anggota ini"
	const sameHousehold = "email sudah menjadi anggota rumah tangga ini; lihat daftar anggota untuk membuat tautan undangan"

	_, inactiveEmail := seedStandaloneUser(t, pool, "inactive", false, false)
	_, adminEmail := seedStandaloneUser(t, pool, "admin", true, true)
	_, elsewhereID, _ := seedIdentity(t, pool, "MEMBER")
	var elsewhereEmail string
	if err := pool.QueryRow(context.Background(), `SELECT email FROM "user" WHERE id=$1`, elsewhereID).Scan(&elsewhereEmail); err != nil {
		t.Fatal(err)
	}
	removedID, removedEmail := seedStandaloneUser(t, pool, "removed", true, false)
	if _, err := pool.Exec(context.Background(), `INSERT INTO household_member(household_id,user_id,role,active,deactivated_at) VALUES($1,$2,'MEMBER',false,now())`, householdID, removedID); err != nil {
		t.Fatal(err)
	}
	var ownerEmail string
	if err := pool.QueryRow(context.Background(), `SELECT email FROM "user" WHERE id=$1`, owner.UserID).Scan(&ownerEmail); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		email string
		want  string
	}{
		"deactivated account":                {inactiveEmail, otherAccount},
		"super admin":                        {adminEmail, otherAccount},
		"active member of another household": {elsewhereEmail, otherAccount},
		"inactive member of this household":  {removedEmail, sameHousehold},
		"owner of this household":            {ownerEmail, sameHousehold},
	} {
		t.Run(name, func(t *testing.T) {
			response := addMember(t, handler, owner, tc.email)
			if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	var memberships int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM household_member WHERE household_id=$1 AND active`, householdID).Scan(&memberships); err != nil || memberships != 1 {
		t.Fatalf("active memberships=%d err=%v", memberships, err)
	}
}
