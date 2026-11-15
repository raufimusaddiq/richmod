package emailingress

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestBuildSteps(t *testing.T) {
	cases := []struct {
		name                                 string
		hasAddress                           bool
		status                               string
		hasSender, verified                  bool
		address, sender, forwarding, activeS string
	}{
		{"nothing yet", false, "", false, false, StepCurrent, StepBlocked, StepBlocked, StepBlocked},
		{"sender without address stays blocked", false, "", true, false, StepCurrent, StepBlocked, StepBlocked, StepBlocked},
		{"address only", true, StatusProvisioned, false, false, StepDone, StepCurrent, StepCurrent, StepBlocked},
		{"address and sender", true, StatusProvisioned, true, false, StepDone, StepDone, StepCurrent, StepCurrent},
		{"forwarding verified", true, StatusProvisioned, true, true, StepDone, StepDone, StepDone, StepCurrent},
		{"verified but no sender", true, StatusProvisioned, false, true, StepDone, StepCurrent, StepDone, StepBlocked},
		{"activated", true, StatusActive, true, true, StepDone, StepDone, StepDone, StepDone},
	}
	for _, tc := range cases {
		got := buildSteps(tc.hasAddress, tc.status, tc.hasSender, tc.verified)
		want := Steps{Address: tc.address, Sender: tc.sender, Forwarding: tc.forwarding, Activated: tc.activeS}
		if got != want {
			t.Errorf("%s: got %+v want %+v", tc.name, got, want)
		}
	}
}

func TestActivateErrorsMapToSpecific409(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{ErrAddressRequired, "ADDRESS_REQUIRED"}, {ErrSenderRequired, "SENDER_REQUIRED"}} {
		response := httptest.NewRecorder()
		writeActivateError(response, tc.err)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), tc.code) {
			t.Errorf("%v: status %d body %s", tc.err, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	if writeActivateError(response, errors.New("boom")) {
		t.Fatal("unrelated errors must not be handled as prerequisites")
	}
}

func TestIntegrationGetNeedsHousehold(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/email-ingress", nil)
	request = request.WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{UserID: "user"}))
	response := httptest.NewRecorder()
	handler.Integration(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestActivateRequiresSenderAndChecklistTracksSteps(t *testing.T) {
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
	var household, other, user, otherUser string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('checklist '||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('checklist other '||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES((substr(gen_random_uuid()::text,1,8)||'@test.invalid'),'Checklist','x',now()) RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES((substr(gen_random_uuid()::text,1,8)||'@test.invalid'),'Checklist Other','x',now()) RETURNING id`).Scan(&otherUser); err != nil {
		t.Fatal(err)
	}
	for h, u := range map[string]string{household: user, other: otherUser} {
		if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, h, u); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(pool, "richmod.link", []string{"mx.test"})
	expect := func(label string, address *Address, want Steps) {
		t.Helper()
		got, err := service.Checklist(ctx, household, address)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s: got %+v want %+v", label, got, want)
		}
	}

	if err := service.Activate(ctx, household, user); !errors.Is(err, ErrAddressRequired) {
		t.Fatalf("activate without address = %v", err)
	}
	expect("empty", nil, Steps{StepCurrent, StepBlocked, StepBlocked, StepBlocked})

	address, err := service.Provision(ctx, household, user)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Activate(ctx, household, user); !errors.Is(err, ErrSenderRequired) {
		t.Fatalf("activate without sender = %v", err)
	}
	expect("address", &address, Steps{StepDone, StepCurrent, StepCurrent, StepBlocked})

	// Another household's sender never satisfies this household's prerequisite.
	if _, err := pool.Exec(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Other','other@test.invalid',$2)`, other, otherUser); err != nil {
		t.Fatal(err)
	}
	if err := service.Activate(ctx, household, user); !errors.Is(err, ErrSenderRequired) {
		t.Fatalf("activate with foreign sender = %v", err)
	}

	// An inactive listener does not count.
	var listener string
	if err := pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,active,created_by_user_id) VALUES($1,'Inactive','inactive@test.invalid',false,$2) RETURNING id`, household, user).Scan(&listener); err != nil {
		t.Fatal(err)
	}
	if err := service.Activate(ctx, household, user); !errors.Is(err, ErrSenderRequired) {
		t.Fatalf("activate with inactive listener = %v", err)
	}

	// A control (Gmail confirmation) delivery does not verify forwarding.
	confirmation := []byte("From: Gmail Team <forwarding-noreply@google.com>\r\nSubject: (Gmail Forwarding Confirmation - Receive Mail from owner@example.com\r\nMessage-ID: <checklist-confirm-" + household + "@test.invalid>\r\nContent-Type: text/html\r\n\r\n<a href=\"https://mail-settings.google.com/mail/vf-abc123\">Confirm</a> Confirmation code: 123456789")
	if err := service.Deliver(ctx, signedInput(confirmation, address.Address, "forwarder@test.invalid", "checklist/confirm-"+household+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	expect("confirmation only", &address, Steps{StepDone, StepCurrent, StepCurrent, StepBlocked})

	// A real forwarded email does.
	real := []byte("From: Test Bank <alerts@test.invalid>\r\nSubject: Payment\r\nMessage-ID: <checklist-real-" + household + "@test.invalid>\r\nContent-Type: text/plain\r\n\r\nAmount IDR 100")
	if err := service.Deliver(ctx, signedInput(real, address.Address, "forwarder@test.invalid", "checklist/real-"+household+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	expect("forwarded", &address, Steps{StepDone, StepCurrent, StepDone, StepBlocked})

	if _, err := pool.Exec(ctx, `UPDATE bank_email_listener SET active=true WHERE id=$1`, listener); err != nil {
		t.Fatal(err)
	}
	expect("sender", &address, Steps{StepDone, StepDone, StepDone, StepCurrent})
	if err := service.Activate(ctx, household, user); err != nil {
		t.Fatalf("activate with active listener = %v", err)
	}
	address.Status = StatusActive
	expect("activated", &address, Steps{StepDone, StepDone, StepDone, StepDone})
	if err := service.Activate(ctx, household, user); !errors.Is(err, ErrAddressRequired) {
		t.Fatalf("second activate = %v", err)
	}

	// An active financial provider source satisfies the prerequisite too.
	if _, err := pool.Exec(ctx, `UPDATE bank_email_listener SET active=false WHERE household_id=$1`, household); err != nil {
		t.Fatal(err)
	}
	ok, err := hasActiveSender(ctx, pool, household)
	if err != nil || ok {
		t.Fatalf("no active sender expected: %v %v", ok, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO financial_email_source(household_id,provider_name,sender_address,capabilities,status,created_by_user_id) VALUES($1,'Provider','provider@test.invalid',ARRAY['CASH_MOVEMENT'],'ACTIVE',$2)`, household, user); err != nil {
		t.Fatal(err)
	}
	ok, err = hasActiveSender(ctx, pool, household)
	if err != nil || !ok {
		t.Fatalf("active provider source expected: %v %v", ok, err)
	}
}
