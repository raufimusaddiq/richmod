package emailingress

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
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
