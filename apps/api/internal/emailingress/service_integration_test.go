package emailingress

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
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
	again, err := service.Provision(ctx, household, user)
	if err != nil {
		t.Fatal(err)
	}
	if again.Address != address.Address {
		t.Fatalf("idempotent provision returned %q then %q", address.Address, again.Address)
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
	if err := service.Activate(ctx, household, user); err != nil {
		t.Fatal(err)
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
	if _, err := pool.Exec(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Shared Sender','alerts@test.invalid',$2)`, otherHousehold, otherUser); err != nil {
		t.Fatal(err)
	}
	otherAddress, err := service.Provision(ctx, otherHousehold, otherUser)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Activate(ctx, otherHousehold, otherUser); err != nil {
		t.Fatal(err)
	}
	otherRaw := []byte("From: Test Bank <alerts@test.invalid>\r\nSubject: Household B\r\nMessage-ID: <household-b-" + otherHousehold + "@test.invalid>\r\nAuthentication-Results: mx.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nAmount IDR 300")
	if err := service.Deliver(ctx, signedInput(otherRaw, otherAddress.Address, "forwarder@gmail.test", "test/raw-household-b-"+otherHousehold+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, otherHousehold).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("same sender through recipient B created %d household B events", count)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, household).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recipient B changed household A event count to %d", count)
	}
	unmatchedRaw := []byte("From: Other Bank <other-alerts@test.invalid>\r\nSubject: Isolation\r\nMessage-ID: <isolation-" + household + "@test.invalid>\r\nAuthentication-Results: mx.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nAmount IDR 200")
	if err := service.Deliver(ctx, signedInput(unmatchedRaw, address.Address, "forwarder@gmail.test", "test/raw-isolation-"+household+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, otherHousehold).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recipient A routed %d events to household B", count)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, household).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("listener from household B mutated household A: %d", count)
	}
	untrustedRaw := []byte("From: Test Bank <alerts@test.invalid>\r\nSubject: Untrusted\r\nMessage-ID: <untrusted-" + household + "@test.invalid>\r\nAuthentication-Results: attacker.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nAmount IDR 400")
	if err := service.Deliver(ctx, signedInput(untrustedRaw, address.Address, "forwarder@gmail.test", "test/raw-untrusted-"+household+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, household).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("untrusted authentication created financial source: %d", count)
	}
	var reason string
	if err := pool.QueryRow(ctx, `SELECT reason_code FROM email_ingress_delivery WHERE household_id=$1 AND status='IGNORED_AUTH' ORDER BY created_at DESC LIMIT 1`, household).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "UNTRUSTED_AUTHENTICATION" {
		t.Fatalf("untrusted reason = %q", reason)
	}
	_ = listener
}

func TestProvisionedWithoutAuthservAndActiveFailsClosed(t *testing.T) {
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
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('auth bootstrap '||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES(('auth-bootstrap-'||substr(gen_random_uuid()::text,1,8)||'@test.invalid'),'Auth Bootstrap','x',now()) RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Generic','alerts@financial.test',$2)`, household, user); err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, "richmod.link", nil)
	address, err := service.Provision(ctx, household, user)
	if err != nil {
		t.Fatal(err)
	}
	provisionedRaw := []byte("From: Alerts <alerts@financial.test>\r\nSubject: Bootstrap\r\nContent-Type: text/plain\r\n\r\nIDR 100")
	if err := service.Deliver(ctx, signedInput(provisionedRaw, address.Address, "forwarder@test.invalid", "bootstrap/"+household+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_ingress_delivery WHERE household_id=$1 AND status='PROVISIONED_RECEIVED'`, household).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("provisioned deliveries = %d", count)
	}
	if err := service.Activate(ctx, household, user); err != nil {
		t.Fatal(err)
	}
	activeRaw := []byte("From: Alerts <alerts@financial.test>\r\nSubject: Active without auth config\r\nMessage-ID: <missing-auth-" + household + "@test.invalid>\r\nAuthentication-Results: mx.test; dkim=pass; dmarc=pass\r\nContent-Type: text/plain\r\n\r\nIDR 200")
	if err := service.Deliver(ctx, signedInput(activeRaw, address.Address, "forwarder@test.invalid", "active-missing-auth/"+household+".eml", "secret")); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, household).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("active ingress without auth config created %d sources", count)
	}
	var reason string
	if err := pool.QueryRow(ctx, `SELECT reason_code FROM email_ingress_delivery WHERE household_id=$1 AND status='IGNORED_AUTH' ORDER BY created_at DESC LIMIT 1`, household).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "TRUSTED_AUTHSERV_UNCONFIGURED" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestUnknownRecipientReturnsAcceptedWithoutMutation(t *testing.T) {
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
	raw := []byte("From: Alerts <alerts@financial.test>\r\nSubject: Unknown\r\nContent-Type: text/plain\r\n\r\nIDR 100")
	recipient := "h_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa@richmod.link"
	envelope := "forwarder@test.invalid"
	timestamp := time.Now().UTC().Unix()
	digest := sha256.Sum256(raw)
	canonical := strconv.FormatInt(timestamp, 10) + "\n" + recipient + "\n" + envelope + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte(canonical))
	request := httptest.NewRequest(http.MethodPost, "/finance/v1/email/inbond", io.NopCloser(strings.NewReader(string(raw))))
	request.Header.Set("Content-Type", "message/rfc822")
	request.Header.Set("X-Richmod-Timestamp", strconv.FormatInt(timestamp, 10))
	request.Header.Set("X-Richmod-Recipient", recipient)
	request.Header.Set("X-Richmod-Envelope-From", envelope)
	request.Header.Set("X-Richmod-Content-SHA256", hex.EncodeToString(digest[:]))
	request.Header.Set("X-Richmod-Signature", hex.EncodeToString(mac.Sum(nil)))
	request.Header.Set("X-Richmod-Object-Key", "unknown/test.eml")
	response := httptest.NewRecorder()
	NewHandler(NewService(pool, "richmod.link", nil), "secret").Inbound(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d", response.Code)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_ingress_delivery WHERE object_key='unknown/test.eml'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unknown recipient created %d deliveries", count)
	}
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
