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
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/pushsupport"
)

// These tests are intentionally self-contained so they can be run against the
// pre-fix commit to demonstrate the behavioural difference.

type blkFake struct {
	mu     sync.Mutex
	paths  []string
	counts map[string]int
	status func(path string, call int) int
}

func (f *blkFake) RoundTrip(request *http.Request) (*http.Response, error) {
	f.mu.Lock()
	if f.counts == nil {
		f.counts = make(map[string]int)
	}
	f.counts[request.URL.Path]++
	call := f.counts[request.URL.Path]
	f.paths = append(f.paths, request.URL.Path)
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

func (f *blkFake) snapshot() (int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paths), append([]string(nil), f.paths...)
}

func TestBlockerSSRFSenderRefusesPrivateEndpoint(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	blkSubscription(t, pool, household, user, "https://10.0.0.1/private?"+user)
	blkOutbox(t, pool, household, user)
	fake := &blkFake{}
	sender := blkSender(t, pool, fake)

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count, _ := fake.snapshot(); count != 0 {
		t.Fatalf("private endpoint was contacted %d times", count)
	}
	var sent bool
	var lastError *string
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL,last_error FROM push_outbox WHERE user_id=$1`, user).Scan(&sent, &lastError); err != nil {
		t.Fatal(err)
	}
	if sent {
		t.Fatal("private endpoint row was marked sent")
	}
	if lastError == nil || !strings.Contains(*lastError, "not allowed") {
		t.Fatalf("last_error=%v", lastError)
	}
}

func TestBlockerMembershipRecheckedAtSend(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, active := blkMember(t, pool)
	removed := blkUser(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, household, removed); err != nil {
		t.Fatal(err)
	}
	blkSubscription(t, pool, household, active, "https://1.1.1.1/active?"+active)
	blkSubscription(t, pool, household, removed, "https://1.1.1.1/removed?"+removed)
	if _, err := pool.Exec(ctx, `INSERT INTO integration_action(household_id,integration_type,action_type,status,title,dedupe_key) VALUES($1,'EMAIL_FORWARDING','VERIFY_FORWARDING','OPEN','Verify','membership')`, household); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE household_member SET active=false WHERE household_id=$1 AND user_id=$2`, household, removed); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE push_outbox SET available_at=now() WHERE household_id=$1`, household); err != nil {
		t.Fatal(err)
	}
	fake := &blkFake{}
	sender := blkSender(t, pool, fake)

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, paths := fake.snapshot()
	if len(paths) != 1 || paths[0] != "/active" {
		t.Fatalf("paths=%v", paths)
	}
	var abandoned bool
	var sent bool
	var lastError *string
	if err := pool.QueryRow(ctx, `SELECT abandoned_at IS NOT NULL,sent_at IS NOT NULL,last_error FROM push_outbox WHERE household_id=$1 AND user_id=$2`, household, removed).Scan(&abandoned, &sent, &lastError); err != nil {
		t.Fatal(err)
	}
	if !abandoned || sent || lastError == nil || *lastError != "recipient is not an active household member" {
		t.Fatalf("removed member abandoned=%v sent=%v last_error=%v", abandoned, sent, lastError)
	}
	var activeSent bool
	if err := pool.QueryRow(ctx, `SELECT sent_at IS NOT NULL FROM push_outbox WHERE household_id=$1 AND user_id=$2`, household, active).Scan(&activeSent); err != nil {
		t.Fatal(err)
	}
	if !activeSent {
		t.Fatal("active member did not receive the notification")
	}
}

func TestBlockerPerSubscriptionRetry(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	blkSubscription(t, pool, household, user, "https://1.1.1.1/ok?"+user)
	blkSubscription(t, pool, household, user, "https://1.1.1.1/fail?"+user)
	blkOutbox(t, pool, household, user)
	fake := &blkFake{status: func(path string, call int) int {
		if path == "/fail" && call == 1 {
			return http.StatusInternalServerError
		}
		return http.StatusCreated
	}}
	sender := blkSender(t, pool, fake)

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var sent bool
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL FROM push_outbox WHERE user_id=$1`, user).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if sent {
		t.Fatal("one device's success marked the row sent")
	}
	before, _ := fake.snapshot()
	if before != 2 {
		t.Fatalf("first pass requests=%d", before)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE push_outbox SET available_at=now() WHERE user_id=$1`, user); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	count, paths := fake.snapshot()
	second := paths[before:count]
	if len(second) != 1 || second[0] != "/fail" {
		t.Fatalf("retry paths=%v", second)
	}
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL FROM push_outbox WHERE user_id=$1`, user).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Fatal("row stayed unsent after the failing device succeeded")
	}
}

func TestBlockerExpiredRowNotDelivered(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	blkSubscription(t, pool, household, user, "https://1.1.1.1/expired?"+user)
	if _, err := pool.Exec(context.Background(), `INSERT INTO push_outbox(household_id,user_id,kind,payload,created_at,available_at) VALUES($1,$2,'REVIEW','{}',now()-interval '2 hours',now())`, household, user); err != nil {
		t.Fatal(err)
	}
	fake := &blkFake{}
	sender := blkSender(t, pool, fake)

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count, _ := fake.snapshot(); count != 0 {
		t.Fatalf("expired row was delivered %d times", count)
	}
	var abandoned bool
	var sent bool
	var lastError *string
	if err := pool.QueryRow(context.Background(), `SELECT abandoned_at IS NOT NULL,sent_at IS NOT NULL,last_error FROM push_outbox WHERE user_id=$1`, user).Scan(&abandoned, &sent, &lastError); err != nil {
		t.Fatal(err)
	}
	if !abandoned || sent || lastError == nil || !strings.Contains(*lastError, "expired") {
		t.Fatalf("abandoned=%v sent=%v last_error=%v", abandoned, sent, lastError)
	}
}

func TestBlockerCrossHouseholdSubscriptionNotDelivered(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	var otherHousehold string
	if err := pool.QueryRow(context.Background(), `INSERT INTO household(name) VALUES('other-'||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&otherHousehold); err != nil {
		t.Fatal(err)
	}
	blkSubscription(t, pool, household, user, "https://1.1.1.1/right-household?"+user)
	blkSubscription(t, pool, otherHousehold, user, "https://1.1.1.1/wrong-household?"+user)
	blkOutbox(t, pool, household, user)
	fake := &blkFake{}
	sender := blkSender(t, pool, fake)
	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, paths := fake.snapshot()
	if len(paths) != 1 || paths[0] != "/right-household" {
		t.Fatalf("cross-household delivery paths=%v", paths)
	}
}

func TestBlockerReSubscribeMovesHouseholdAndRetiresPending(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	ctx := context.Background()
	var otherHousehold string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('moved-'||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&otherHousehold); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role,active) VALUES($1,$2,'MEMBER',false)`, otherHousehold, user); err != nil {
		t.Fatal(err)
	}
	endpoint := "https://1.1.1.1/moved?" + user
	p256dh, auth := blkDeviceKeys(t)
	var subscriptionID string
	if err := pool.QueryRow(ctx, `INSERT INTO push_subscriptions(user_id,household_id,endpoint,p256dh,auth) VALUES($1,$2,$3,$4,$5) RETURNING id`, user, household, endpoint, p256dh, auth).Scan(&subscriptionID); err != nil {
		t.Fatal(err)
	}
	var oldOutboxID int64
	if err := pool.QueryRow(ctx, `INSERT INTO push_outbox(household_id,user_id,kind,payload) VALUES($1,$2,'REVIEW','{}') RETURNING id`, household, user).Scan(&oldOutboxID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO push_outbox_delivery(outbox_id,subscription_id,status,attempts) VALUES($1,$2,'PENDING',1)`, oldOutboxID, subscriptionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE household_member SET active=false WHERE household_id=$1 AND user_id=$2`, household, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE household_member SET active=true WHERE household_id=$1 AND user_id=$2`, otherHousehold, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pushsupport.UpsertSubscription(ctx, pool, user, otherHousehold, endpoint, p256dh, auth, "move"); err != nil {
		t.Fatal(err)
	}
	var storedHousehold string
	var pending int
	if err := pool.QueryRow(ctx, `SELECT household_id::text,(SELECT count(*) FROM push_outbox_delivery WHERE subscription_id=$1 AND status='PENDING') FROM push_subscriptions WHERE id=$1`, subscriptionID).Scan(&storedHousehold, &pending); err != nil {
		t.Fatal(err)
	}
	if storedHousehold != otherHousehold {
		t.Fatalf("subscription household=%s, want %s", storedHousehold, otherHousehold)
	}
	if pending != 0 {
		t.Fatalf("old pending deliveries=%d, want 0", pending)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO integration_action(household_id,integration_type,action_type,status,title,dedupe_key) VALUES($1,'EMAIL_FORWARDING','VERIFY_FORWARDING','OPEN','Verify',$2)`, otherHousehold, "move-"+otherHousehold); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE push_outbox SET available_at=now() WHERE household_id=$1`, otherHousehold); err != nil {
		t.Fatal(err)
	}
	fake := &blkFake{}
	sender := blkSender(t, pool, fake)
	if _, err := sender.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if count, paths := fake.snapshot(); count != 1 || paths[0] != "/moved" {
		t.Fatalf("new-household delivery count=%d paths=%v", count, paths)
	}
	var newSent bool
	if err := pool.QueryRow(ctx, `SELECT count(*) > 0 FROM push_outbox WHERE household_id=$1 AND user_id=$2 AND sent_at IS NOT NULL`, otherHousehold, user).Scan(&newSent); err != nil {
		t.Fatal(err)
	}
	if !newSent {
		t.Fatal("new-household event was not delivered")
	}
}

func TestBlockerRefreshKeepsActivePeriod(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	endpoint := "https://1.1.1.1/refresh?" + user
	p256dh, auth := blkDeviceKeys(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO push_subscriptions(user_id,household_id,endpoint,p256dh,auth) VALUES($1,$2,$3,$4,$5)`, user, household, endpoint, p256dh, auth); err != nil {
		t.Fatal(err)
	}
	var outboxID int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO push_outbox(household_id,user_id,kind,payload) VALUES($1,$2,'REVIEW','{}') RETURNING id`, household, user).Scan(&outboxID); err != nil {
		t.Fatal(err)
	}
	if _, err := pushsupport.UpsertSubscription(context.Background(), pool, user, household, endpoint, p256dh, auth, "refresh"); err != nil {
		t.Fatal(err)
	}
	fake := &blkFake{}
	sender := blkSender(t, pool, fake)
	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count, _ := fake.snapshot(); count != 1 {
		t.Fatalf("pending event after refresh delivered %d times, want 1", count)
	}
	var sent bool
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL FROM push_outbox WHERE id=$1`, outboxID).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Fatal("pending event after refresh was not marked sent")
	}
}

func TestBlockerPendingDeliveryRetiredOnReactivate(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	endpoint := "https://1.1.1.1/reactivate?" + user
	p256dh, auth := blkDeviceKeys(t)
	var subscriptionID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO push_subscriptions(user_id,household_id,endpoint,p256dh,auth) VALUES($1,$2,$3,$4,$5) RETURNING id`, user, household, endpoint, p256dh, auth).Scan(&subscriptionID); err != nil {
		t.Fatal(err)
	}
	var outboxID int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO push_outbox(household_id,user_id,kind,payload) VALUES($1,$2,'REVIEW','{}') RETURNING id`, household, user).Scan(&outboxID); err != nil {
		t.Fatal(err)
	}
	failing := true
	fake := &blkFake{status: func(string, int) int {
		if failing {
			return http.StatusInternalServerError
		}
		return http.StatusCreated
	}}
	sender := blkSender(t, pool, fake)
	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE push_subscriptions SET disabled_at=now() WHERE id=$1`, subscriptionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pushsupport.UpsertSubscription(context.Background(), pool, user, household, endpoint, p256dh, auth, "reactivate"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE push_outbox SET available_at=now() WHERE id=$1`, outboxID); err != nil {
		t.Fatal(err)
	}
	failing = false
	before, _ := fake.snapshot()
	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if after, _ := fake.snapshot(); after != before {
		t.Fatalf("retry from the disabled period sent %d notification(s)", after-before)
	}
	var pending int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM push_outbox_delivery WHERE outbox_id=$1 AND status='PENDING'`, outboxID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	var sent, abandoned bool
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL,abandoned_at IS NOT NULL FROM push_outbox WHERE id=$1`, outboxID).Scan(&sent, &abandoned); err != nil {
		t.Fatal(err)
	}
	if pending != 0 || sent || !abandoned {
		t.Fatalf("pending=%d sent=%v abandoned=%v", pending, sent, abandoned)
	}
}

func TestBlockerReSubscribeDoesNotReplayBacklog(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	endpoint := "https://1.1.1.1/backlog?" + user
	device, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	p256dh := base64.RawURLEncoding.EncodeToString(device.PublicKey().Bytes())
	authEncoded := base64.RawURLEncoding.EncodeToString(auth)
	if _, err := pool.Exec(context.Background(), `INSERT INTO push_subscriptions(user_id,household_id,endpoint,p256dh,auth,created_at,disabled_at,failure_count) VALUES($1,$2,$3,$4,$5,now()-interval '30 days',now()-interval '1 day',4)`, user, household, endpoint, p256dh, authEncoded); err != nil {
		t.Fatal(err)
	}
	var backlogID int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO push_outbox(household_id,user_id,kind,payload) VALUES($1,$2,'REVIEW','{}') RETURNING id`, household, user).Scan(&backlogID); err != nil {
		t.Fatal(err)
	}
	if _, err := pushsupport.UpsertSubscription(context.Background(), pool, user, household, endpoint, p256dh, authEncoded, "test"); err != nil {
		t.Fatal(err)
	}
	fake := &blkFake{}
	sender := blkSender(t, pool, fake)
	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count, _ := fake.snapshot(); count != 0 {
		t.Fatalf("backlog created while disabled was delivered %d times", count)
	}

	var afterID int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO push_outbox(household_id,user_id,kind,payload) VALUES($1,$2,'REVIEW','{}') RETURNING id`, household, user).Scan(&afterID); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count, _ := fake.snapshot(); count != 1 {
		t.Fatalf("event after re-subscribe delivered %d times, want 1", count)
	}
	var sent bool
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL FROM push_outbox WHERE id=$1`, afterID).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Fatal("event after re-subscribe was not marked sent")
	}
}

func TestBlockerFreshSubscriptionDelivers(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	blkSubscription(t, pool, household, user, "https://1.1.1.1/fresh?"+user)
	blkOutbox(t, pool, household, user)
	fake := &blkFake{}
	sender := blkSender(t, pool, fake)
	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count, _ := fake.snapshot(); count != 1 {
		t.Fatalf("fresh subscription delivered %d times, want 1", count)
	}
}

func TestBlockerNoLiveSubscriptionMeansNoOutboxRow(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	ctx := context.Background()
	insertAction := func(key string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO integration_action(household_id,integration_type,action_type,status,title,dedupe_key) VALUES($1,'EMAIL_FORWARDING','VERIFY_FORWARDING','OPEN','Verify',$2)`, household, key); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		t.Helper()
		var value int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM push_outbox WHERE household_id=$1`, household).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	insertAction("no-sub")
	if got := count(); got != 0 {
		t.Fatalf("no-subscription outbox rows=%d, want 0", got)
	}
	subscription := blkSubscription(t, pool, household, user, "https://1.1.1.1/gated?"+user)
	if _, err := pool.Exec(ctx, `UPDATE push_subscriptions SET disabled_at=now() WHERE id=$1`, subscription); err != nil {
		t.Fatal(err)
	}
	insertAction("disabled-sub")
	if got := count(); got != 0 {
		t.Fatalf("disabled-subscription outbox rows=%d, want 0", got)
	}
	if _, err := pool.Exec(ctx, `UPDATE push_subscriptions SET disabled_at=NULL WHERE id=$1`, subscription); err != nil {
		t.Fatal(err)
	}
	insertAction("live-sub")
	if got := count(); got != 1 {
		t.Fatalf("live-subscription outbox rows=%d, want 1", got)
	}
}

func TestBlockerTransientFailuresDoNotDisableSubscription(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	subscription := blkSubscription(t, pool, household, user, "https://1.1.1.1/transient?"+user)
	blkOutbox(t, pool, household, user)
	failing := true
	fake := &blkFake{status: func(string, int) int {
		if failing {
			return http.StatusServiceUnavailable
		}
		return http.StatusCreated
	}}
	sender := blkSender(t, pool, fake)

	for range 5 {
		if _, err := sender.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(context.Background(), `UPDATE push_outbox SET available_at=now() WHERE user_id=$1`, user); err != nil {
			t.Fatal(err)
		}
	}
	var failureCount int
	var disabled bool
	if err := pool.QueryRow(context.Background(), `SELECT failure_count,disabled_at IS NOT NULL FROM push_subscriptions WHERE id=$1`, subscription).Scan(&failureCount, &disabled); err != nil {
		t.Fatal(err)
	}
	if disabled {
		t.Fatalf("transient failures disabled the subscription after %d failures", failureCount)
	}
	if failureCount != 5 {
		t.Fatalf("failure_count=%d, want 5", failureCount)
	}

	// Recovery: a new event must reach the same device and reset the diagnostic.
	failing = false
	blkOutbox(t, pool, household, user)
	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var sent int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM push_outbox WHERE user_id=$1 AND sent_at IS NOT NULL`, user).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("sent rows after recovery=%d, want 1", sent)
	}
	if err := pool.QueryRow(context.Background(), `SELECT failure_count,disabled_at IS NOT NULL FROM push_subscriptions WHERE id=$1`, subscription).Scan(&failureCount, &disabled); err != nil {
		t.Fatal(err)
	}
	if disabled || failureCount != 0 {
		t.Fatalf("after recovery failure_count=%d disabled=%v", failureCount, disabled)
	}
}

func TestBlockerNotFoundStillDisablesSubscription(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	subscription := blkSubscription(t, pool, household, user, "https://1.1.1.1/not-found?"+user)
	blkOutbox(t, pool, household, user)
	fake := &blkFake{status: func(string, int) int { return http.StatusNotFound }}
	sender := blkSender(t, pool, fake)

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var disabled bool
	if err := pool.QueryRow(context.Background(), `SELECT disabled_at IS NOT NULL FROM push_subscriptions WHERE id=$1`, subscription).Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if !disabled {
		t.Fatal("404 did not disable the subscription")
	}
}

func TestBlockerResubscribedDeviceRetriesAfterOne500(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	subscription := blkSubscription(t, pool, household, user, "https://1.1.1.1/one-500?"+user)
	blkOutbox(t, pool, household, user)
	fake := &blkFake{status: func(string, int) int { return http.StatusInternalServerError }}
	sender := blkSender(t, pool, fake)

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var failureCount int
	var disabled bool
	if err := pool.QueryRow(context.Background(), `SELECT failure_count,disabled_at IS NOT NULL FROM push_subscriptions WHERE id=$1`, subscription).Scan(&failureCount, &disabled); err != nil {
		t.Fatal(err)
	}
	if failureCount != 1 || disabled {
		t.Fatalf("failure_count=%d disabled=%v", failureCount, disabled)
	}
	var sent bool
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL FROM push_outbox WHERE user_id=$1`, user).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if sent {
		t.Fatal("row was not left retryable after a transient failure")
	}
}

func TestBlockerSubscriptionCreatedAfterEvent(t *testing.T) {
	pool := blkPool(t)
	blkClean(t, pool)
	household, user := blkMember(t, pool)
	blkOutbox(t, pool, household, user)
	blkSubscription(t, pool, household, user, "https://1.1.1.1/late?"+user)
	fake := &blkFake{}
	sender := blkSender(t, pool, fake)

	if _, err := sender.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count, _ := fake.snapshot(); count != 0 {
		t.Fatalf("event predating the subscription was delivered %d times", count)
	}
	var sent bool
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL FROM push_outbox WHERE user_id=$1`, user).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Fatal("row with no eligible subscription was not retired")
	}
}

func blkPool(t *testing.T) *pgxpool.Pool {
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

func blkClean(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE push_outbox SET sent_at=now() WHERE sent_at IS NULL AND abandoned_at IS NULL`); err != nil {
		t.Fatal(err)
	}
}

func blkMember(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	ctx := context.Background()
	var household string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('blocker-'||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	user := blkUser(t, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	return household, user
}

func blkUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var user string
	if err := pool.QueryRow(context.Background(), `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES('blocker-'||gen_random_uuid()||'@test.invalid','Blocker','x',now()) RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	return user
}

func blkDeviceKeys(t *testing.T) (string, string) {
	t.Helper()
	device, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(device.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(auth)
}

func blkSubscription(t *testing.T, pool *pgxpool.Pool, household, user, endpoint string) string {
	t.Helper()
	device, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO push_subscriptions(user_id,household_id,endpoint,p256dh,auth) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		user, household, endpoint,
		base64.RawURLEncoding.EncodeToString(device.PublicKey().Bytes()),
		base64.RawURLEncoding.EncodeToString(auth)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func blkOutbox(t *testing.T, pool *pgxpool.Pool, household, user string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO push_outbox(household_id,user_id,kind,payload) VALUES($1,$2,'REVIEW','{}')`, household, user); err != nil {
		t.Fatal(err)
	}
}

func blkSender(t *testing.T, pool *pgxpool.Pool, fake *blkFake) *Sender {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := NewSender(pool, base64.RawURLEncoding.EncodeToString(key.Bytes()), "mailto:test@example.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	sender.client = &http.Client{Transport: fake}
	sender.backoff = func(int) time.Duration { return time.Second }
	sender.batch = 1000
	return sender
}
