package household

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

func TestReactivatingDeactivatedMemberSucceeds(t *testing.T) {
	t.Parallel()
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
	stamp := testsupport.UniqueStamp()
	var householdID, owner, member string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Reactivation %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	for i, target := range []*string{&owner, &member} {
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Person','x') RETURNING id`, fmt.Sprintf("reactivation-%d-%d@example.test", stamp, i)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER'),($1,$3,'MEMBER')`, householdID, owner, member); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(pool, "")
	patch := func(body string, want int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/household/members/"+member, bytes.NewBufferString(body))
		request.SetPathValue("id", member)
		request = request.WithContext(auth.ContextWithPrincipal(ctx, auth.Principal{UserID: owner, HasHousehold: true, HouseholdID: householdID, HouseholdRole: "OWNER"}))
		response := httptest.NewRecorder()
		handler.PatchMember(response, request)
		if response.Code != want {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	patch(`{"active":false}`, http.StatusNoContent)
	patch(`{"active":true}`, http.StatusNoContent)
	var active bool
	var deactivatedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT active,deactivated_at FROM household_member WHERE household_id=$1 AND user_id=$2`, householdID, member).Scan(&active, &deactivatedAt); err != nil {
		t.Fatal(err)
	}
	if !active || deactivatedAt != nil {
		t.Fatalf("active=%t deactivatedAt=%v", active, deactivatedAt)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND entity_type='household_member' AND entity_id=$2 AND action='ACTIVATE'`, householdID, member).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("activate audits=%d err=%v", audits, err)
	}
}

func TestDeactivatingMemberRevokesEveryWayBackIn(t *testing.T) {
	t.Parallel()
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
	stamp := testsupport.UniqueStamp()
	var householdID, owner, member string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Deactivation %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	for i, target := range []*string{&owner, &member} {
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Person','x') RETURNING id`, fmt.Sprintf("deactivation-%d-%d@example.test", stamp, i)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER'),($1,$3,'MEMBER')`, householdID, owner, member); err != nil {
		t.Fatal(err)
	}
	token := fmt.Sprintf("invite-token-%d", stamp)
	digest := sha256.Sum256([]byte(token))
	if _, err := pool.Exec(ctx, `INSERT INTO dashboard_account_invite(household_id,user_id,token_hash,status,expires_at,created_by_user_id) VALUES($1,$2,$3,'PENDING',now()+interval '1 day',$4)`, householdID, member, digest[:], owner); err != nil {
		t.Fatal(err)
	}
	sessionDigest := sha256.Sum256([]byte(fmt.Sprintf("session-%d", stamp)))
	if _, err := pool.Exec(ctx, `INSERT INTO session(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 day')`, member, sessionDigest[:]); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/household/members/"+member, bytes.NewBufferString(`{"active":false}`))
	request.SetPathValue("id", member)
	request = request.WithContext(auth.ContextWithPrincipal(ctx, auth.Principal{UserID: owner, HasHousehold: true, HouseholdID: householdID, HouseholdRole: "OWNER"}))
	response := httptest.NewRecorder()
	NewHandler(pool, "").PatchMember(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("deactivate: %d %s", response.Code, response.Body.String())
	}
	var pendingInvites, liveSessions int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM dashboard_account_invite WHERE user_id=$1 AND status='PENDING'),(SELECT count(*) FROM session WHERE user_id=$1 AND revoked_at IS NULL)`, member).Scan(&pendingInvites, &liveSessions); err != nil {
		t.Fatal(err)
	}
	if pendingInvites != 0 || liveSessions != 0 {
		t.Fatalf("after deactivation: pending invites=%d live sessions=%d", pendingInvites, liveSessions)
	}
	if _, err := pool.Exec(ctx, `UPDATE dashboard_account_invite SET status='PENDING',revoked_at=NULL WHERE user_id=$1`, member); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := auth.NewService(pool).AcceptDashboardInvite(ctx, token, "a-long-enough-password"); err == nil {
		t.Fatal("a removed member accepted a dashboard invite")
	}
}
