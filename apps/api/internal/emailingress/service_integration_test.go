package emailingress

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDeliveryProvisionedActiveAndDuplicate(t *testing.T) {
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
	var household, user, listener string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('email ingress test '||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES(('email-ingress-'||substr(gen_random_uuid()::text,1,8)||'@test.invalid'),'Email Test','x',now()) RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Test Bank','alerts@test.invalid',$2) RETURNING id`, household, user).Scan(&listener); err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, "richmod.link", []string{"mx.test"})
	address, err := service.Provision(ctx, household, user)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: Test Bank <alerts@test.invalid>\r\nSubject: Payment\r\nMessage-ID: <email-ingress-test-" + household + "@test.invalid>\r\nAuthentication-Results: mx.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nAmount IDR 100")
	input := signedInput(raw, address.Address, "forwarder@gmail.test", "test/raw-1-"+household+".eml", "secret")
	if err := service.Deliver(ctx, input); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, household).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("provisioned created %d source events", count)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO gmail_integration(household_id,mailbox,encrypted_refresh_token,granted_scope,status,connected_by_user_id,history_id) VALUES($1,$2,$3,'readonly','WATCH_ACTIVE',$4,'100')`, household, "active-"+household+"@test.invalid", []byte("test"), user); err != nil {
		t.Fatal(err)
	}
	if err := service.Activate(ctx, household, user); err != nil {
		t.Fatal(err)
	}
	var gmailStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM gmail_integration WHERE household_id=$1`, household).Scan(&gmailStatus); err != nil {
		t.Fatal(err)
	}
	if gmailStatus != "DISCONNECTED" {
		t.Fatalf("Gmail status = %s", gmailStatus)
	}
	activeRaw := []byte("From: Test Bank <alerts@test.invalid>\r\nSubject: Payment 2\r\nMessage-ID: <email-ingress-active-" + household + "@test.invalid>\r\nAuthentication-Results: mx.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nAmount IDR 200")
	input = signedInput(activeRaw, address.Address, "forwarder@gmail.test", "test/raw-2-"+household+".eml", "secret")
	if err := service.Deliver(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := service.Deliver(ctx, input); err != nil {
		t.Fatal(err)
	}
	changedRaw := []byte("From: Test Bank <alerts@test.invalid>\r\nSubject: Payment 2 changed\r\nMessage-ID: <email-ingress-active-" + household + "@test.invalid>\r\nAuthentication-Results: mx.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nAmount IDR 201")
	if err := service.Deliver(ctx, signedInput(changedRaw, address.Address, "forwarder@gmail.test", "test/raw-3-"+household+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, household).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("active source events = %d", count)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='PROCESS_BANK_EMAIL' AND payload_json->>'source_event_id' IN (SELECT id::text FROM source_event WHERE household_id=$1)`, household).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("bank jobs = %d", count)
	}

	var otherHousehold, otherUser string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('email ingress isolation '||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&otherHousehold); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES(('email-isolation-'||substr(gen_random_uuid()::text,1,8)||'@test.invalid'),'Isolation Test','x',now()) RETURNING id`).Scan(&otherUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, otherHousehold, otherUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Other Bank','other-alerts@test.invalid',$2)`, otherHousehold, otherUser); err != nil {
		t.Fatal(err)
	}
	unmatchedRaw := []byte("From: Other Bank <other-alerts@test.invalid>\r\nSubject: Isolation\r\nMessage-ID: <isolation-" + household + "@test.invalid>\r\nAuthentication-Results: mx.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nAmount IDR 200")
	if err := service.Deliver(ctx, signedInput(unmatchedRaw, address.Address, "forwarder@gmail.test", "test/raw-isolation-"+household+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, otherHousehold).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("recipient A routed %d events to household B", count)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, household).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("listener from household B mutated household A: %d", count)
	}
	_ = listener
}

func signedInput(raw []byte, recipient, envelope, objectKey, secret string) deliveryInput {
	digest := sha256.Sum256(raw)
	timestamp := time.Now().UTC().Unix()
	canonical := strconv.FormatInt(timestamp, 10) + "\n" + recipient + "\n" + envelope + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	parsed, _ := parseMIME(raw)
	return deliveryInput{Signed: SignedRequest{Timestamp: timestamp, Recipient: recipient, EnvelopeFrom: envelope, ContentHash: digest, Signature: mac.Sum(nil), ObjectKey: objectKey}, Email: parsed, Raw: raw}
}
