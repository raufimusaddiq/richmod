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
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
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

// Closing a failed-source action acknowledges the failure: the source event is
// finalized in the same transaction, only for that household, only while it is
// still unfinished, and the change is audited.
func TestResolvingAFailedSourceActionFinalizesItsSourceEvent(t *testing.T) {
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

	var householdA, householdB, ownerA, ownerB string
	for name, target := range map[string]*string{"Source A ": &householdA, "Source B ": &householdB} {
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1||substr(gen_random_uuid()::text,1,8)) RETURNING id`, name).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	for label, target := range map[string]*string{"owner-source-a": &ownerA, "owner-source-b": &ownerB} {
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES(($1||'-'||substr(gen_random_uuid()::text,1,8)||'@test.invalid'),$1,'x',now()) RETURNING id`, label).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER'),($3,$4,'OWNER')`, householdA, ownerA, householdB, ownerB); err != nil {
		t.Fatal(err)
	}
	source := func(household, status string) string {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',gen_random_uuid()::text,now(),'\x00',$2) RETURNING id::text`, household, status).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	record := func(household, event string) string {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := reviewdomain.RecordFailedSourceAction(ctx, tx, reviewdomain.FailedSource{HouseholdID: household, SourceEventID: event, SourceType: "BANK_EMAIL", Reason: "INVALID"}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		var id string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM integration_action WHERE household_id=$1 AND dedupe_key=$2`, household, event).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	status := func(id string) string {
		var value string
		if err := pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, id).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	resolve := func(actionID, user, household string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/integration-actions/"+actionID+"/resolve", nil)
		r.SetPathValue("id", actionID)
		r = r.WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}))
		w := httptest.NewRecorder()
		NewHandler(pool).Resolve(w, r)
		return w.Code
	}

	failedA, failedB, finished := source(householdA, "FAILED"), source(householdB, "FAILED"), source(householdA, "PROCESSED")
	actionA, actionB, actionFinished := record(householdA, failedA), record(householdB, failedB), record(householdA, finished)

	// The same item recorded twice is one item.
	record(householdA, failedA)
	var items int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM integration_action WHERE household_id=$1 AND dedupe_key=$2`, householdA, failedA).Scan(&items); err != nil || items != 1 {
		t.Fatalf("recording twice must keep one item, got %d (%v)", items, err)
	}

	// Another household cannot close it, and nothing changes.
	if code := resolve(actionA, ownerB, householdB); code != 404 {
		t.Fatalf("a foreign household must get 404, got %d", code)
	}
	if status(failedA) != "FAILED" {
		t.Fatalf("a foreign resolve must not touch the event, got %s", status(failedA))
	}

	// The owner closes it: the event is finalized and the change is audited.
	if code := resolve(actionA, ownerA, householdA); code != 204 {
		t.Fatalf("owner resolve=%d", code)
	}
	if status(failedA) != "IGNORED" {
		t.Fatalf("closing the action must finalize its event, got %s", status(failedA))
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='IGNORE_FAILED_SOURCE' AND entity_id=$2::uuid`, householdA, failedA).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("finalizing the event must be audited once, got %d (%v)", audits, err)
	}

	// Another household's event is untouched; an event that finished in the meantime keeps its state.
	if status(failedB) != "FAILED" {
		t.Fatalf("the other household's event must be untouched, got %s", status(failedB))
	}
	if code := resolve(actionFinished, ownerA, householdA); code != 204 {
		t.Fatalf("resolve of an action whose event finished=%d", code)
	}
	if status(finished) != "PROCESSED" {
		t.Fatalf("a finished event must keep PROCESSED, got %s", status(finished))
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='IGNORE_FAILED_SOURCE' AND entity_id=$2::uuid`, householdA, finished).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("no event change means no event audit row, got %d (%v)", audits, err)
	}
	_ = actionB
}
