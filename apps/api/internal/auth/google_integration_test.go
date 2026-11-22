package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var googleSeq atomic.Int64

type googleEnv struct {
	t       *testing.T
	pool    *pgxpool.Pool
	service *Service
	handler *Handler
	fake    *fakeGoogle
	stamp   int64
}

func newGoogleEnv(t *testing.T) *googleEnv {
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
	fake := newFakeGoogle(t)
	service := NewService(pool)
	return &googleEnv{
		t:       t,
		pool:    pool,
		service: service,
		handler: newTestHandler(fake, service),
		fake:    fake,
		stamp:   uniqueStamp()*1000 + googleSeq.Add(1),
	}
}

func (e *googleEnv) email(label string) string {
	return fmt.Sprintf("%s-%d@example.test", label, e.stamp)
}

func (e *googleEnv) sub(label string) string {
	return fmt.Sprintf("sub-%s-%d", label, e.stamp)
}

func (e *googleEnv) household() string {
	e.t.Helper()
	var id string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Google %d", e.stamp)).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

type seedUser struct {
	email       string
	initialized bool
	superAdmin  bool
	active      bool
	googleSub   string
}

func (e *googleEnv) user(u seedUser) string {
	e.t.Helper()
	hash, err := HashPassword("seed-password-123456")
	if err != nil {
		e.t.Fatal(err)
	}
	var id string
	var initializedAt *time.Time
	if u.initialized {
		now := time.Now()
		initializedAt = &now
	}
	var sub, linkedAt any
	if u.googleSub != "" {
		sub, linkedAt = u.googleSub, time.Now()
	}
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at,is_super_admin,active,google_sub,google_linked_at) VALUES($1,'Seed User',$2,$3,$4,$5,$6,$7) RETURNING id`,
		u.email, hash, initializedAt, u.superAdmin, u.active, sub, linkedAt).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *googleEnv) member(householdID, userID, role string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,$3)`, householdID, userID, role); err != nil {
		e.t.Fatal(err)
	}
}

func (e *googleEnv) invite(householdID, userID, createdBy, token string, expires time.Time) string {
	e.t.Helper()
	var id string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO dashboard_account_invite(household_id,user_id,token_hash,status,expires_at,created_by_user_id) VALUES($1,$2,$3,'PENDING',$4,$5) RETURNING id`,
		householdID, userID, hashToken(token), expires, createdBy).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *googleEnv) inviteStatus(id string) string {
	e.t.Helper()
	var status string
	if err := e.pool.QueryRow(context.Background(), `SELECT status FROM dashboard_account_invite WHERE id=$1`, id).Scan(&status); err != nil {
		e.t.Fatal(err)
	}
	return status
}

type userRow struct {
	id         string
	googleSub  *string
	linked     bool
	superAdmin bool
	name       string
	initiated  bool
}

func (e *googleEnv) userByEmail(email string) (userRow, bool) {
	e.t.Helper()
	var row userRow
	err := e.pool.QueryRow(context.Background(), `SELECT id,google_sub,google_linked_at IS NOT NULL,is_super_admin,display_name,password_initialized_at IS NOT NULL FROM "user" WHERE email=$1`, email).Scan(&row.id, &row.googleSub, &row.linked, &row.superAdmin, &row.name, &row.initiated)
	if err != nil {
		return userRow{}, false
	}
	return row, true
}

func (e *googleEnv) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *googleEnv) assertSignedIn(result flowResult) Principal {
	e.t.Helper()
	if result.rec.Code != http.StatusSeeOther || result.location != "/" {
		e.t.Fatalf("status=%d location=%q", result.rec.Code, result.location)
	}
	if result.session == nil || result.session.Value == "" || !result.session.HttpOnly || !result.session.Secure {
		e.t.Fatalf("session cookie=%+v", result.session)
	}
	if result.flow == nil || result.flow.MaxAge >= 0 {
		e.t.Fatalf("flow cookie not cleared: %+v", result.flow)
	}
	principal, _, err := e.service.Authenticate(context.Background(), result.session.Value)
	if err != nil {
		e.t.Fatalf("issued session does not authenticate: %v", err)
	}
	return principal
}

func TestGoogleLoginCreatesHouseholdlessUserForUnknownEmail(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	identity := googleIdentity{sub: e.sub("new"), email: e.email("newuser"), name: "New Person"}
	result := runGoogleFlow(t, e.handler, e.fake, "", identity, func(c map[string]any) { c["email"] = "  " + identity.email + " " })
	principal := e.assertSignedIn(result)
	if principal.HasHousehold || principal.Household != nil || principal.IsSuperAdmin {
		t.Fatalf("principal=%+v", principal)
	}
	if principal.Email != identity.email || principal.DisplayName != "New Person" {
		t.Fatalf("principal=%+v", principal)
	}
	row, ok := e.userByEmail(identity.email)
	if !ok || row.googleSub == nil || *row.googleSub != identity.sub || !row.linked || row.superAdmin || row.initiated {
		t.Fatalf("row=%+v ok=%v", row, ok)
	}
	again := runGoogleFlow(t, e.handler, e.fake, "", identity, nil)
	second := e.assertSignedIn(again)
	if second.UserID != principal.UserID {
		t.Fatal("a second Google sign-in created a different user")
	}
	if n := e.count(`SELECT count(*) FROM "user" WHERE google_sub=$1`, identity.sub); n != 1 {
		t.Fatalf("users for sub=%d", n)
	}
}

func TestGoogleLoginDerivesDisplayNameFromEmailWhenNameMissing(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	identity := googleIdentity{sub: e.sub("noname"), email: e.email("noname")}
	e.assertSignedIn(runGoogleFlow(t, e.handler, e.fake, "", identity, func(c map[string]any) { delete(c, "name") }))
	row, _ := e.userByEmail(identity.email)
	if row.name != fmt.Sprintf("noname-%d", e.stamp) {
		t.Fatalf("name=%q", row.name)
	}
}

func TestGoogleLoginLinksEnrolledMemberByEmail(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	householdID := e.household()
	email := e.email("owner")
	userID := e.user(seedUser{email: email, initialized: true, active: true})
	e.member(householdID, userID, "OWNER")
	identity := googleIdentity{sub: e.sub("owner"), email: email, name: "Different Name"}
	principal := e.assertSignedIn(runGoogleFlow(t, e.handler, e.fake, "", identity, func(c map[string]any) { c["email"] = "  " + email[:1] + email[1:] }))
	if principal.UserID != userID || !principal.HasHousehold || principal.HouseholdID != householdID || principal.HouseholdRole != "OWNER" {
		t.Fatalf("principal=%+v", principal)
	}
	row, _ := e.userByEmail(email)
	if row.googleSub == nil || *row.googleSub != identity.sub || !row.linked || !row.initiated || row.name != "Seed User" {
		t.Fatalf("row=%+v", row)
	}
	if n := e.count(`SELECT count(*) FROM "user" WHERE email=$1`, email); n != 1 {
		t.Fatalf("duplicate users=%d", n)
	}
}

func TestGoogleLoginMatchesEmailCaseInsensitively(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	email := e.email("casing")
	userID := e.user(seedUser{email: email, initialized: true, active: true})
	identity := googleIdentity{sub: e.sub("casing"), email: "CASING-" + fmt.Sprint(e.stamp) + "@Example.TEST"}
	principal := e.assertSignedIn(runGoogleFlow(t, e.handler, e.fake, "", identity, nil))
	if principal.UserID != userID {
		t.Fatalf("mixed-case email created a different user: %+v", principal)
	}
}

func TestGoogleLoginKeepsSuperAdmin(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	email := e.email("root")
	userID := e.user(seedUser{email: email, initialized: true, active: true, superAdmin: true})
	identity := googleIdentity{sub: e.sub("root"), email: email, name: "Root"}
	principal := e.assertSignedIn(runGoogleFlow(t, e.handler, e.fake, "", identity, nil))
	if principal.UserID != userID || !principal.IsSuperAdmin || principal.HasHousehold {
		t.Fatalf("principal=%+v", principal)
	}
	row, _ := e.userByEmail(email)
	if !row.superAdmin || row.googleSub == nil {
		t.Fatalf("row=%+v", row)
	}
}

func TestGoogleLoginReusesLinkedAccountEvenWhenEmailChanged(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	email := e.email("linked")
	sub := e.sub("linked")
	userID := e.user(seedUser{email: email, initialized: true, active: true, googleSub: sub})
	identity := googleIdentity{sub: sub, email: e.email("renamed"), name: "Renamed"}
	principal := e.assertSignedIn(runGoogleFlow(t, e.handler, e.fake, "", identity, nil))
	if principal.UserID != userID || principal.Email != email {
		t.Fatalf("principal=%+v", principal)
	}
	if _, ok := e.userByEmail(identity.email); ok {
		t.Fatal("a user was created for the changed Google email")
	}
}

func TestGoogleLoginRequiresInviteForUnenrolledMember(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	householdID := e.household()
	email := e.email("pending")
	userID := e.user(seedUser{email: email, initialized: false, active: true})
	e.member(householdID, userID, "MEMBER")
	identity := googleIdentity{sub: e.sub("pending"), email: email, name: "Pending"}
	assertRejected(t, runGoogleFlow(t, e.handler, e.fake, "", identity, nil), "/login?error=invite_required")
	row, _ := e.userByEmail(email)
	if row.googleSub != nil || row.linked {
		t.Fatalf("an unenrolled member was linked without the invite: %+v", row)
	}
	if n := e.count(`SELECT count(*) FROM session WHERE user_id=$1`, userID); n != 0 {
		t.Fatalf("sessions=%d", n)
	}
}

func TestGoogleLoginRejectsConflictingSubjectForSameEmail(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	email := e.email("conflict")
	e.user(seedUser{email: email, initialized: true, active: true, googleSub: e.sub("original")})
	identity := googleIdentity{sub: e.sub("attacker"), email: email}
	assertRejected(t, runGoogleFlow(t, e.handler, e.fake, "", identity, nil), "/login?error=google_failed")
	row, _ := e.userByEmail(email)
	if row.googleSub == nil || *row.googleSub != e.sub("original") {
		t.Fatalf("google subject was replaced: %+v", row)
	}
}

func TestGoogleLoginRejectsInactiveUsers(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	byEmail := e.email("inactive-email")
	e.user(seedUser{email: byEmail, initialized: true, active: false})
	assertRejected(t, runGoogleFlow(t, e.handler, e.fake, "", googleIdentity{sub: e.sub("inactive-email"), email: byEmail}, nil), "/login?error=google_failed")
	bySub := e.email("inactive-sub")
	e.user(seedUser{email: bySub, initialized: true, active: false, googleSub: e.sub("inactive-sub")})
	assertRejected(t, runGoogleFlow(t, e.handler, e.fake, "", googleIdentity{sub: e.sub("inactive-sub"), email: bySub}, nil), "/login?error=google_failed")
	if n := e.count(`SELECT count(*) FROM "user" WHERE email IN ($1,$2)`, byEmail, bySub); n != 2 {
		t.Fatalf("users=%d", n)
	}
	row, _ := e.userByEmail(byEmail)
	if row.googleSub != nil {
		t.Fatal("a deactivated user was linked")
	}
}

func TestGoogleCallbackRevokesPreviousSession(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	email := e.email("switch")
	e.user(seedUser{email: email, initialized: true, active: true})
	_, oldToken, _, err := e.service.Login(context.Background(), email, "seed-password-123456")
	if err != nil {
		t.Fatal(err)
	}
	identity := googleIdentity{sub: e.sub("switch"), email: email}
	e.assertSignedIn(runGoogleFlow(t, e.handler, e.fake, "", identity, nil, &http.Cookie{Name: SessionCookieName, Value: oldToken}))
	if _, _, err := e.service.Authenticate(context.Background(), oldToken); err == nil {
		t.Fatal("the previous session survived a Google sign-in")
	}
}

func TestGoogleInviteAcceptanceLinksAndConsumes(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	householdID := e.household()
	ownerID := e.user(seedUser{email: e.email("inv-owner"), initialized: true, active: true})
	e.member(householdID, ownerID, "OWNER")
	email := e.email("invitee")
	memberID := e.user(seedUser{email: email, initialized: false, active: true})
	e.member(householdID, memberID, "MEMBER")
	token := fmt.Sprintf("invite-token-%d", e.stamp)
	inviteID := e.invite(householdID, memberID, ownerID, token, time.Now().Add(time.Hour))
	identity := googleIdentity{sub: e.sub("invitee"), email: email, name: "Invitee"}
	principal := e.assertSignedIn(runGoogleFlow(t, e.handler, e.fake, token, identity, nil))
	if principal.UserID != memberID || !principal.HasHousehold || principal.HouseholdID != householdID || principal.HouseholdRole != "MEMBER" {
		t.Fatalf("principal=%+v", principal)
	}
	row, _ := e.userByEmail(email)
	if row.googleSub == nil || *row.googleSub != identity.sub || !row.linked || row.initiated {
		t.Fatalf("row=%+v", row)
	}
	if status := e.inviteStatus(inviteID); status != "CONSUMED" {
		t.Fatalf("invite status=%s", status)
	}
	assertRejected(t, runGoogleFlow(t, e.handler, e.fake, token, identity, nil), "/login?error=invite_invalid")
	e.assertSignedIn(runGoogleFlow(t, e.handler, e.fake, "", identity, nil))
}

func TestGoogleInviteRejectsMismatchedGoogleEmail(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	householdID := e.household()
	ownerID := e.user(seedUser{email: e.email("mm-owner"), initialized: true, active: true})
	e.member(householdID, ownerID, "OWNER")
	email := e.email("mm-invitee")
	memberID := e.user(seedUser{email: email, initialized: false, active: true})
	e.member(householdID, memberID, "MEMBER")
	token := fmt.Sprintf("mismatch-token-%d", e.stamp)
	inviteID := e.invite(householdID, memberID, ownerID, token, time.Now().Add(time.Hour))
	other := googleIdentity{sub: e.sub("someone-else"), email: e.email("someone-else"), name: "Someone Else"}
	assertRejected(t, runGoogleFlow(t, e.handler, e.fake, token, other, nil), "/login?error=google_email_mismatch")
	if status := e.inviteStatus(inviteID); status != "PENDING" {
		t.Fatalf("invite status=%s", status)
	}
	row, _ := e.userByEmail(email)
	if row.googleSub != nil || row.linked {
		t.Fatalf("invitee linked to the wrong Google account: %+v", row)
	}
	if _, ok := e.userByEmail(other.email); ok {
		t.Fatal("a user was created from a mismatched invite sign-in")
	}
	right := googleIdentity{sub: e.sub("right"), email: email, name: "Right"}
	e.assertSignedIn(runGoogleFlow(t, e.handler, e.fake, token, right, nil))
}

func TestGoogleInviteRejectsUnknownAndExpiredTokens(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	householdID := e.household()
	ownerID := e.user(seedUser{email: e.email("ex-owner"), initialized: true, active: true})
	e.member(householdID, ownerID, "OWNER")
	email := e.email("ex-invitee")
	memberID := e.user(seedUser{email: email, initialized: false, active: true})
	e.member(householdID, memberID, "MEMBER")
	token := fmt.Sprintf("expired-token-%d", e.stamp)
	e.invite(householdID, memberID, ownerID, token, time.Now().Add(-time.Minute))
	identity := googleIdentity{sub: e.sub("ex-invitee"), email: email}
	assertRejected(t, runGoogleFlow(t, e.handler, e.fake, token, identity, nil), "/login?error=invite_invalid")
	assertRejected(t, runGoogleFlow(t, e.handler, e.fake, "no-such-token", identity, nil), "/login?error=invite_invalid")
	row, _ := e.userByEmail(email)
	if row.googleSub != nil {
		t.Fatal("an expired invite linked a Google account")
	}
}

func TestGoogleInviteRejectsSubjectOwnedByAnotherUser(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	householdID := e.household()
	ownerID := e.user(seedUser{email: e.email("dup-owner"), initialized: true, active: true})
	e.member(householdID, ownerID, "OWNER")
	e.user(seedUser{email: e.email("dup-holder"), initialized: true, active: true, googleSub: e.sub("dup")})
	email := e.email("dup-invitee")
	memberID := e.user(seedUser{email: email, initialized: false, active: true})
	e.member(householdID, memberID, "MEMBER")
	token := fmt.Sprintf("dup-token-%d", e.stamp)
	inviteID := e.invite(householdID, memberID, ownerID, token, time.Now().Add(time.Hour))
	identity := googleIdentity{sub: e.sub("dup"), email: email}
	assertRejected(t, runGoogleFlow(t, e.handler, e.fake, token, identity, nil), "/login?error=invite_invalid")
	if status := e.inviteStatus(inviteID); status != "PENDING" {
		t.Fatalf("invite status=%s", status)
	}
}

func TestGoogleInviteRejectsDeactivatedMember(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	householdID := e.household()
	ownerID := e.user(seedUser{email: e.email("deact-owner"), initialized: true, active: true})
	e.member(householdID, ownerID, "OWNER")
	email := e.email("deact-invitee")
	memberID := e.user(seedUser{email: email, initialized: false, active: true})
	e.member(householdID, memberID, "MEMBER")
	token := fmt.Sprintf("deact-token-%d", e.stamp)
	e.invite(householdID, memberID, ownerID, token, time.Now().Add(time.Hour))
	if _, err := e.pool.Exec(context.Background(), `UPDATE household_member SET active=false,deactivated_at=now() WHERE household_id=$1 AND user_id=$2`, householdID, memberID); err != nil {
		t.Fatal(err)
	}
	assertRejected(t, runGoogleFlow(t, e.handler, e.fake, token, googleIdentity{sub: e.sub("deact"), email: email}, nil), "/login?error=invite_invalid")
}

func TestGoogleResetInviteRevokesExistingSessions(t *testing.T) {
	t.Parallel()
	e := newGoogleEnv(t)
	householdID := e.household()
	ownerID := e.user(seedUser{email: e.email("reset-owner"), initialized: true, active: true})
	e.member(householdID, ownerID, "OWNER")
	email := e.email("reset-member")
	memberID := e.user(seedUser{email: email, initialized: true, active: true})
	e.member(householdID, memberID, "MEMBER")
	_, staleToken, _, err := e.service.Login(context.Background(), email, "seed-password-123456")
	if err != nil {
		t.Fatal(err)
	}
	token := fmt.Sprintf("reset-token-%d", e.stamp)
	e.invite(householdID, memberID, ownerID, token, time.Now().Add(time.Hour))
	identity := googleIdentity{sub: e.sub("reset-member"), email: email}
	e.assertSignedIn(runGoogleFlow(t, e.handler, e.fake, token, identity, nil))
	if _, _, err := e.service.Authenticate(context.Background(), staleToken); err == nil {
		t.Fatal("a session predating the reset invite survived")
	}
}
