package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testVAPIDPrivate, testP256DH, testAuth = newTestPushFixture()

func newTestPushFixture() (string, string, string) {
	vapidKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	deviceKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(vapidKey.Bytes()),
		base64.RawURLEncoding.EncodeToString(deviceKey.PublicKey().Bytes()),
		base64.RawURLEncoding.EncodeToString(auth)
}

type fakeTransport struct {
	mu       sync.Mutex
	requests []string
	status   func(path string, call int) int
}

func (f *fakeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request.URL.Path)
	call := len(f.requests)
	f.mu.Unlock()
	status := http.StatusCreated
	if f.status != nil {
		status = f.status(request.URL.Path, call)
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
		Request:    request,
	}, nil
}

func (f *fakeTransport) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeTransport) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func TestSenderDeliversAndMarksSent(t *testing.T) {
	pool := testPool(t)
	cleanOutbox(t, pool)
	household, user := seedMember(t, pool)
	fake := &fakeTransport{}
	seedSubscription(t, pool, household, user, "https://1.1.1.1/ok?"+user)
	seedOutbox(t, pool, household, user, "REVIEW")
	sender := testSender(t, pool)
	sender.client = &http.Client{Transport: fake}

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.count() != 1 {
		t.Fatalf("requests=%d", fake.count())
	}
	var sent bool
	var attempts int
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL,attempts FROM push_outbox WHERE user_id=$1`, user).Scan(&sent, &attempts); err != nil {
		t.Fatal(err)
	}
	if !sent || attempts != 1 {
		t.Fatalf("sent=%v attempts=%d", sent, attempts)
	}
}

func TestSenderRetriesServerErrorThenSucceeds(t *testing.T) {
	pool := testPool(t)
	cleanOutbox(t, pool)
	household, user := seedMember(t, pool)
	fake := &fakeTransport{status: func(_ string, call int) int {
		if call == 1 {
			return http.StatusInternalServerError
		}
		return http.StatusCreated
	}}
	seedSubscription(t, pool, household, user, "https://1.1.1.1/retry?"+user)
	seedOutbox(t, pool, household, user, "REVIEW")
	sender := testSender(t, pool)
	sender.client = &http.Client{Transport: fake}

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var sent bool
	var lastError *string
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL,last_error FROM push_outbox WHERE user_id=$1`, user).Scan(&sent, &lastError); err != nil {
		t.Fatal(err)
	}
	if sent || lastError == nil || *lastError != "push service returned 500" {
		t.Fatalf("sent=%v last_error=%v", sent, lastError)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE push_outbox SET available_at=now() WHERE user_id=$1`, user); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL,attempts FROM push_outbox WHERE user_id=$1`, user).Scan(&sent, &attempts); err != nil {
		t.Fatal(err)
	}
	if !sent || fake.count() != 2 {
		t.Fatalf("sent=%v calls=%d", sent, fake.count())
	}
}

func TestSenderDisablesSubscriptionOnGone(t *testing.T) {
	pool := testPool(t)
	cleanOutbox(t, pool)
	household, user := seedMember(t, pool)
	fake := &fakeTransport{status: func(string, int) int { return http.StatusGone }}
	subscription := seedSubscription(t, pool, household, user, "https://1.1.1.1/gone?"+user)
	seedOutbox(t, pool, household, user, "REVIEW")
	sender := testSender(t, pool)
	sender.client = &http.Client{Transport: fake}

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var disabled bool
	if err := pool.QueryRow(context.Background(), `SELECT disabled_at IS NOT NULL FROM push_subscriptions WHERE id=$1`, subscription).Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if !disabled {
		t.Fatal("subscription was not disabled on 410")
	}
}

func TestSenderAbandonsAfterMaxAttempts(t *testing.T) {
	pool := testPool(t)
	cleanOutbox(t, pool)
	household, user := seedMember(t, pool)
	fake := &fakeTransport{status: func(string, int) int { return http.StatusBadGateway }}
	seedSubscription(t, pool, household, user, "https://1.1.1.1/fail?"+user)
	seedOutbox(t, pool, household, user, "REVIEW")
	sender := testSender(t, pool)
	sender.client = &http.Client{Transport: fake}
	sender.maxAttempts = 2

	for range 2 {
		if _, err := sender.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(context.Background(), `UPDATE push_outbox SET available_at=now() WHERE user_id=$1`, user); err != nil {
			t.Fatal(err)
		}
	}
	var abandoned bool
	var lastError *string
	if err := pool.QueryRow(context.Background(), `SELECT abandoned_at IS NOT NULL,last_error FROM push_outbox WHERE user_id=$1`, user).Scan(&abandoned, &lastError); err != nil {
		t.Fatal(err)
	}
	if !abandoned || lastError == nil || *lastError != "push service returned 502" {
		t.Fatalf("abandoned=%v last_error=%v", abandoned, lastError)
	}
}

func TestSenderCoalescesRowsForOneUser(t *testing.T) {
	pool := testPool(t)
	cleanOutbox(t, pool)
	household, user := seedMember(t, pool)
	fake := &fakeTransport{}
	seedSubscription(t, pool, household, user, "https://1.1.1.1/coalesce?"+user)
	seedOutbox(t, pool, household, user, "REVIEW")
	seedOutbox(t, pool, household, user, "INTEGRATION_ACTION")
	sender := testSender(t, pool)
	sender.client = &http.Client{Transport: fake}

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var sent int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM push_outbox WHERE user_id=$1 AND sent_at IS NOT NULL`, user).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if fake.count() != 1 || sent != 2 {
		t.Fatalf("requests=%d sent=%d", fake.count(), sent)
	}
}

func TestReviewOutboxRollsBackWithCreatingTransaction(t *testing.T) {
	pool := testPool(t)
	household, user := seedMember(t, pool)
	seedSubscription(t, pool, household, user, "https://1.1.1.1/rollback?"+household)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var source string
	if err := tx.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,received_at,payload_hash,processing_status) VALUES($1,'SYSTEM',now(),decode(md5(random()::text),'hex'),'NEEDS_REVIEW') RETURNING id`, household).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb)`, household, source); err != nil {
		t.Fatal(err)
	}
	var inside int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM push_outbox WHERE household_id=$1 AND user_id=$2`, household, user).Scan(&inside); err != nil {
		t.Fatal(err)
	}
	if inside != 1 {
		t.Fatalf("outbox rows inside transaction=%d", inside)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM push_outbox WHERE household_id=$1 AND user_id=$2`, household, user).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Fatalf("outbox rows after rollback=%d", after)
	}
}

func TestOneOutboxRowPerActiveMember(t *testing.T) {
	pool := testPool(t)
	household, first := seedMember(t, pool)
	second := seedUser(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, household, second); err != nil {
		t.Fatal(err)
	}
	seedSubscription(t, pool, household, first, "https://1.1.1.1/member-a?"+first)
	seedSubscription(t, pool, household, second, "https://1.1.1.1/member-b?"+second)
	if _, err := pool.Exec(ctx, `INSERT INTO integration_action(household_id,integration_type,action_type,status,title,dedupe_key) VALUES($1,'EMAIL_FORWARDING','VERIFY_FORWARDING','OPEN','Verify','x')`, household); err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{first, second} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM push_outbox WHERE household_id=$1 AND user_id=$2 AND kind='INTEGRATION_ACTION'`, household, member).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("member %s outbox=%d", member, count)
		}
	}
}

func TestSenderSkipsWhenVAPIDMissing(t *testing.T) {
	if _, err := NewSender(nil, "", "", slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("expected disabled error")
	}
}

// TestPurgeDeletesOnlyOldTerminalRows seeds an old SENT outbox with an old SENT
// delivery, an old PENDING outbox with an old PENDING delivery, and an old SENT
// outbox whose delivery is still inside the retention window. Only the first
// pair may disappear; the third stays because its delivery row still exists.
func TestPurgeDeletesOnlyOldTerminalRows(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	household, user := seedMember(t, pool)
	subscription := seedSubscription(t, pool, household, user, "https://1.1.1.1/purge?"+user)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	oldSent := seedPurgeOutbox(t, pool, household, user, 30, true)
	seedPurgeDelivery(t, pool, oldSent, subscription, "SENT", 30)

	pending := seedPurgeOutbox(t, pool, household, user, 30, false)
	seedPurgeDelivery(t, pool, pending, subscription, "PENDING", 30)

	// Terminal outbox, delivery still fresh: deleting the outbox would strand
	// the delivery on a missing foreign key, so the sweep must skip it.
	freshDelivery := seedPurgeOutbox(t, pool, household, user, 30, true)
	seedPurgeDelivery(t, pool, freshDelivery, subscription, "SENT", 0)

	if _, err := Purge(ctx, pool, logger, DefaultRetention); err != nil {
		t.Fatal(err)
	}

	for _, row := range []struct {
		query string
		id    any
		want  int
	}{
		{`SELECT count(*) FROM push_outbox WHERE id=$1`, oldSent, 0},
		{`SELECT count(*) FROM push_outbox_delivery WHERE outbox_id=$1`, oldSent, 0},
		{`SELECT count(*) FROM push_outbox WHERE id=$1 AND sent_at IS NULL AND abandoned_at IS NULL`, pending, 1},
		{`SELECT count(*) FROM push_outbox_delivery WHERE outbox_id=$1 AND status='PENDING'`, pending, 1},
		{`SELECT count(*) FROM push_outbox WHERE id=$1`, freshDelivery, 1},
		{`SELECT count(*) FROM push_outbox_delivery WHERE outbox_id=$1`, freshDelivery, 1},
	} {
		var got int
		if err := pool.QueryRow(ctx, row.query, row.id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != row.want {
			t.Fatalf("%s: got %d, want %d", row.query, got, row.want)
		}
	}
}

// TestPurgeRunsWithoutVAPID proves the sweep does not depend on push
// configuration: the Sender cannot even be constructed with an empty key, and
// Purge still removes the row.
func TestPurgeRunsWithoutVAPID(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	household, user := seedMember(t, pool)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := NewSender(pool, "", "", logger); err == nil {
		t.Fatal("expected the sender to be disabled without VAPID keys")
	}

	abandoned := seedPurgeOutbox(t, pool, household, user, 30, false)
	if _, err := pool.Exec(ctx, `UPDATE push_outbox SET abandoned_at=now()-interval '30 days',last_error='expired' WHERE id=$1`, abandoned); err != nil {
		t.Fatal(err)
	}
	removed, err := Purge(ctx, pool, logger, DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM push_outbox WHERE id=$1`, abandoned).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("abandoned row still present")
	}
}

// seedPurgeOutbox inserts one outbox row created ageDays ago; sent marks it
// terminal, otherwise it stays pending.
func seedPurgeOutbox(t *testing.T, pool *pgxpool.Pool, household, user string, ageDays int, sent bool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO push_outbox(household_id,user_id,kind,payload,created_at,sent_at)
		VALUES($1,$2,'REVIEW','{}',now()-($3*interval '1 day'),
			CASE WHEN $4::boolean THEN now()-($3*interval '1 day') END)
		RETURNING id`, household, user, ageDays, sent).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedPurgeDelivery(t *testing.T, pool *pgxpool.Pool, outbox int64, subscription, status string, ageDays int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO push_outbox_delivery(outbox_id,subscription_id,status,created_at,updated_at)
		VALUES($1,$2,$3,now()-($4*interval '1 day'),now()-($4*interval '1 day'))`,
		outbox, subscription, status, ageDays); err != nil {
		t.Fatal(err)
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
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
	return pool
}

func testSender(t *testing.T, pool *pgxpool.Pool) *Sender {
	t.Helper()
	sender, err := NewSender(pool, testVAPIDPrivate, "mailto:test@example.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	sender.backoff = func(int) time.Duration { return time.Second }
	sender.batch = 1000
	return sender
}

// cleanOutbox isolates sender tests from pending rows left by earlier runs or
// by review/integration fixtures in other packages.
func cleanOutbox(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE push_outbox SET sent_at=now() WHERE sent_at IS NULL AND abandoned_at IS NULL`); err != nil {
		t.Fatal(err)
	}
}

func seedMember(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	ctx := context.Background()
	var household string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('push-test-'||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	user := seedUser(t, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	return household, user
}

func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var user string
	if err := pool.QueryRow(context.Background(), `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES('push-'||gen_random_uuid()||'@test.invalid','Push','x',now()) RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	return user
}

func seedSubscription(t *testing.T, pool *pgxpool.Pool, household, user, endpoint string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO push_subscriptions(user_id,household_id,endpoint,p256dh,auth) VALUES($1,$2,$3,$4,$5) RETURNING id`, user, household, endpoint, testP256DH, testAuth).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedOutbox(t *testing.T, pool *pgxpool.Pool, household, user, kind string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO push_outbox(household_id,user_id,kind,payload) VALUES($1,$2,$3,'{}')`, household, user, kind); err != nil {
		t.Fatal(err)
	}
}
