package emailingress

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

func TestProvisionedConfirmationCreatesOnlyIntegrationAction(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	create := func(label string) (string, string) {
		var household, user string
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1||substr(gen_random_uuid()::text,1,8)) RETURNING id`, label).Scan(&household); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES((substr(gen_random_uuid()::text,1,8)||'@test.invalid'),$1,'x',now()) RETURNING id`, label).Scan(&user); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
			t.Fatal(err)
		}
		return household, user
	}
	householdA, userA := create("Control A ")
	householdB, userB := create("Control B ")
	service := NewService(pool, "richmod.link", []string{"mx.test"})
	addressA, err := service.Provision(ctx, householdA, userA)
	if err != nil {
		t.Fatal(err)
	}
	addressB, err := service.Provision(ctx, householdB, userB)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: Gmail Team <forwarding-noreply@google.com>\r\nSubject: (Gmail Forwarding Confirmation - Receive Mail from owner@example.com\r\nMessage-ID: <control-confirmation@test.invalid>\r\nContent-Type: text/html\r\n\r\n<a href=\"https://mail-settings.google.com/mail/vf-abc123\">Confirm</a> Confirmation code: 123456789")
	inputA := signedInput(raw, addressA.Address, "forwarder@test.invalid", "control/"+householdA+".eml", "secret")
	if err := service.Deliver(ctx, inputA); err != nil {
		t.Fatal(err)
	}
	if err := service.Deliver(ctx, inputA); err != nil {
		t.Fatal(err)
	}
	if err := service.Deliver(ctx, signedInput(raw, addressB.Address, "forwarder@test.invalid", "control/"+householdB+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	for household, want := range map[string]int{householdA: 1, householdB: 1} {
		var actions, sources, events, jobs, reviews, proposals int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM integration_action WHERE household_id=$1 AND status='OPEN'`, household).Scan(&actions); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1`, household).Scan(&sources); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM bank_email_event be JOIN source_event se ON se.id=be.source_event_id WHERE se.household_id=$1`, household).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE payload_json->>'source_event_id' IN (SELECT id::text FROM source_event WHERE household_id=$1)`, household).Scan(&jobs); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1`, household).Scan(&reviews); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM transaction_proposal WHERE household_id=$1`, household).Scan(&proposals); err != nil {
			t.Fatal(err)
		}
		if actions != want || sources != 0 || events != 0 || jobs != 0 || reviews != 0 || proposals != 0 {
			t.Fatalf("household %s actions=%d sources=%d events=%d jobs=%d reviews=%d proposals=%d", household, actions, sources, events, jobs, reviews, proposals)
		}
	}
	unknown := []byte("From: Gmail Team <forwarding-noreply@google.com>\r\nSubject: Other Gmail message\r\nContent-Type: text/plain\r\n\r\nNo setup")
	if err := service.Deliver(ctx, signedInput(unknown, addressA.Address, "forwarder@test.invalid", "control/unknown-"+householdA+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	var actions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM integration_action WHERE household_id=$1`, householdA).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if actions != 1 {
		t.Fatalf("unrecognized provisioned email created action: %d", actions)
	}
	if err := service.Activate(ctx, householdA, userA); err != nil {
		t.Fatal(err)
	}
	if err := service.Deliver(ctx, signedInput(raw, addressA.Address, "forwarder@test.invalid", "control/active-"+householdA+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM integration_action WHERE household_id=$1`, householdA).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if actions != 1 {
		t.Fatalf("active delivery created control action: %d", actions)
	}
}

func TestForwardingVerificationIsExposedAndAutoResolves(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	create := func(label string) (string, string) {
		var household, user string
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1||substr(gen_random_uuid()::text,1,8)) RETURNING id`, label).Scan(&household); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES((substr(gen_random_uuid()::text,1,8)||'@test.invalid'),$1,'x',now()) RETURNING id`, label).Scan(&user); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
			t.Fatal(err)
		}
		return household, user
	}
	householdA, userA := create("Resolve A ")
	householdB, userB := create("Resolve B ")
	service := NewService(pool, "richmod.link", []string{"mx.test"})
	addressA, err := service.Provision(ctx, householdA, userA)
	if err != nil {
		t.Fatal(err)
	}
	addressB, err := service.Provision(ctx, householdB, userB)
	if err != nil {
		t.Fatal(err)
	}
	control := []byte("From: Gmail Team <forwarding-noreply@google.com>\r\nSubject: (Gmail Forwarding Confirmation - Receive Mail from owner@example.com\r\nMessage-ID: <resolve-confirmation@test.invalid>\r\nContent-Type: text/html\r\n\r\n<a href=\"https://mail.google.com/mail/vf-abc123\">Confirm</a> Confirmation code: 987654321")
	for _, address := range []Address{addressA, addressB} {
		if err := service.Deliver(ctx, signedInput(control, address.Address, "forwarder@test.invalid", "resolve/control-"+address.LocalPart+".eml", "secret")); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := service.OpenForwardingAction(ctx, householdA)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || pending.ActionCode == nil || *pending.ActionCode != "987654321" || pending.ActionURL == nil || *pending.ActionURL != "https://mail.google.com/mail/vf-abc123" {
		t.Fatalf("pending forwarding action not exposed: %+v", pending)
	}
	statusFor := func(role string) map[string]any {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/email-ingress", nil)
		req = req.WithContext(auth.ContextWithPrincipal(ctx, auth.Principal{UserID: userA, HouseholdID: householdA, HasHousehold: true, HouseholdRole: role}))
		NewHandler(service, "secret").Integration(rec, req)
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	if action, _ := statusFor("OWNER")["forwardingAction"].(map[string]any); action == nil || action["actionCode"] != "987654321" || action["actionUrl"] != "https://mail.google.com/mail/vf-abc123" {
		t.Fatalf("owner status payload missing forwarding action: %v", action)
	}
	if action := statusFor("MEMBER")["forwardingAction"]; action != nil {
		t.Fatalf("non-owner saw forwarding action: %v", action)
	}
	// The Gmail confirmation itself must not count as proof that forwarding works.
	if err := service.Deliver(ctx, signedInput(control, addressA.Address, "forwarder@test.invalid", "resolve/control-again-"+householdA+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	if again, err := service.OpenForwardingAction(ctx, householdA); err != nil || again == nil {
		t.Fatalf("control replay resolved the action: %+v err=%v", again, err)
	}
	ordinary := []byte("From: Alice <alice@example.com>\r\nSubject: Hello\r\nMessage-ID: <resolve-ordinary@test.invalid>\r\nContent-Type: text/plain\r\n\r\nforwarded mail")
	input := signedInput(ordinary, addressA.Address, "alice@example.com", "resolve/ordinary-"+householdA+".eml", "secret")
	// The same delivery twice (webhook retry) must resolve once and audit once.
	for i := 0; i < 2; i++ {
		if err := service.Deliver(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	if gone, err := service.OpenForwardingAction(ctx, householdA); err != nil || gone != nil {
		t.Fatalf("action still open after non-control delivery: %+v err=%v", gone, err)
	}
	var status string
	var resolvedBy *string
	if err := pool.QueryRow(ctx, `SELECT status,resolved_by_user_id::text FROM integration_action WHERE household_id=$1`, householdA).Scan(&status, &resolvedBy); err != nil {
		t.Fatal(err)
	}
	if status != "RESOLVED" || resolvedBy != nil {
		t.Fatalf("status=%s resolvedBy=%v", status, resolvedBy)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='AUTO_RESOLVE_INTEGRATION_ACTION' AND actor_type='SYSTEM'`, householdA).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("auto-resolve audits=%d, want 1", audits)
	}
	// A later replay of the confirmation email must not reopen a resolved action.
	if err := service.Deliver(ctx, signedInput(control, addressA.Address, "forwarder@test.invalid", "resolve/control-again-"+householdA+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	if reopened, err := service.OpenForwardingAction(ctx, householdA); err != nil || reopened != nil {
		t.Fatalf("resolved action reopened: %+v err=%v", reopened, err)
	}
	// Household B is untouched by household A's delivery.
	if other, err := service.OpenForwardingAction(ctx, householdB); err != nil || other == nil {
		t.Fatalf("other household's action was resolved: %+v err=%v", other, err)
	}
}

func TestRejectedDeliveriesDoNotResolveForwardingVerification(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var household, user string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Rejected '||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES((substr(gen_random_uuid()::text,1,8)||'@test.invalid'),'Rejected','x',now()) RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Test Bank','alerts@test.invalid',$2)`, household, user); err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, "richmod.link", []string{"mx.test"})
	address, err := service.Provision(ctx, household, user)
	if err != nil {
		t.Fatal(err)
	}
	control := []byte("From: Gmail Team <forwarding-noreply@google.com>\r\nSubject: (Gmail Forwarding Confirmation - Receive Mail from owner@example.com\r\nMessage-ID: <rejected-confirmation@test.invalid>\r\nContent-Type: text/html\r\n\r\n<a href=\"https://mail.google.com/mail/vf-abc123\">Confirm</a> Confirmation code: 555666777")
	if err := service.Deliver(ctx, signedInput(control, address.Address, "forwarder@test.invalid", "rejected/control-"+household+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	if err := service.Activate(ctx, household, user); err != nil {
		t.Fatal(err)
	}
	stillOpen := func(step string) {
		t.Helper()
		if open, err := service.OpenForwardingAction(ctx, household); err != nil || open == nil {
			t.Fatalf("%s resolved the forwarding action: %+v err=%v", step, open, err)
		}
		var audits int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='AUTO_RESOLVE_INTEGRATION_ACTION'`, household).Scan(&audits); err != nil || audits != 0 {
			t.Fatalf("%s wrote %d auto-resolve audit rows (err=%v)", step, audits, err)
		}
	}
	deliver := func(raw []byte, key string) {
		t.Helper()
		if err := service.Deliver(ctx, signedInput(raw, address.Address, "forwarder@test.invalid", "rejected/"+key+"-"+household+".eml", "secret")); err != nil {
			t.Fatal(err)
		}
	}
	untrusted := []byte("From: Test Bank <alerts@test.invalid>\r\nSubject: Untrusted\r\nMessage-ID: <rejected-untrusted@test.invalid>\r\nAuthentication-Results: attacker.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nAmount IDR 400")
	deliver(untrusted, "untrusted")
	stillOpen("untrusted authentication")
	unmatched := []byte("From: Stranger <stranger@test.invalid>\r\nSubject: Unmatched\r\nMessage-ID: <rejected-unmatched@test.invalid>\r\nAuthentication-Results: mx.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nAmount IDR 500")
	deliver(unmatched, "unmatched")
	stillOpen("unmatched sender")
	// Replaying a rejected delivery stays rejected.
	deliver(unmatched, "unmatched")
	stillOpen("replayed unmatched sender")
	var ignored int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_ingress_delivery WHERE household_id=$1 AND status IN ('IGNORED_AUTH','IGNORED_UNMATCHED')`, household).Scan(&ignored); err != nil || ignored != 2 {
		t.Fatalf("ignored deliveries=%d err=%v", ignored, err)
	}
	accepted := []byte("From: Test Bank <alerts@test.invalid>\r\nSubject: Payment\r\nMessage-ID: <rejected-accepted@test.invalid>\r\nAuthentication-Results: mx.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nAmount IDR 100")
	for i := 0; i < 2; i++ {
		deliver(accepted, "accepted")
	}
	if open, err := service.OpenForwardingAction(ctx, household); err != nil || open != nil {
		t.Fatalf("accepted delivery did not resolve the action: %+v err=%v", open, err)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='AUTO_RESOLVE_INTEGRATION_ACTION'`, household).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("auto-resolve audits=%d err=%v, want 1", audits, err)
	}
}

func TestOpenForwardingActionTiebreaksOnID(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var household string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Tiebreak '||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	var lowID, highID string
	for i, key := range []string{"tie-a", "tie-b"} {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO integration_action(household_id,integration_type,action_type,status,title,dedupe_key,created_at) VALUES($1,$2,$3,'OPEN','Verifikasi',$4,'2026-01-01T00:00:00Z') RETURNING id`, household, forwardingIntegrationType, forwardingActionType, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			lowID = id
		} else {
			highID = id
		}
	}
	if lowID > highID {
		lowID, highID = highID, lowID
	}
	service := NewService(pool, "richmod.link", []string{"mx.test"})
	for i := 0; i < 5; i++ {
		got, err := service.OpenForwardingAction(ctx, household)
		if err != nil || got == nil || got.ID != highID {
			t.Fatalf("got %+v err=%v, want id %s", got, err, highID)
		}
	}
}
