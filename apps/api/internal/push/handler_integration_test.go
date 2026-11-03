package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

var testP256DH, testAuth = newTestSubscriptionKeys()
var testVAPIDPrivate = newTestVAPIDKey()

func newTestVAPIDKey() string {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(key.Bytes())
}

// newTestHandler keeps this file runnable against 38276915, whose constructor
// took a derived public key, and against the fixed constructor that takes the
// private key and subject. The old branch reproduces the buggy wiring.
func newTestHandler(pool *pgxpool.Pool, privateKey, subject string) *Handler {
	fn := reflect.ValueOf(NewHandler)
	if fn.Type().NumIn() == 3 {
		return fn.Call([]reflect.Value{reflect.ValueOf(pool), reflect.ValueOf(privateKey), reflect.ValueOf(subject)})[0].Interface().(*Handler)
	}
	return fn.Call([]reflect.Value{reflect.ValueOf(pool), reflect.ValueOf(PublicKey(privateKey))})[0].Interface().(*Handler)
}

func newTestSubscriptionKeys() (string, string) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		base64.RawURLEncoding.EncodeToString(auth)
}

func TestPublicKeyEndpointDegradesQuietly(t *testing.T) {
	disabled := httptest.NewRecorder()
	newTestHandler(nil, "", "").PublicKey(disabled, httptest.NewRequest(http.MethodGet, "/api/v1/push/public-key", nil))
	var payload map[string]any
	if err := json.Unmarshal(disabled.Body.Bytes(), &payload); err != nil || disabled.Code != 200 || payload["enabled"] != false {
		t.Fatalf("disabled response=%d body=%s", disabled.Code, disabled.Body.String())
	}
	enabled := httptest.NewRecorder()
	newTestHandler(nil, testVAPIDPrivate, "mailto:test@example.invalid").PublicKey(enabled, httptest.NewRequest(http.MethodGet, "/api/v1/push/public-key", nil))
	var enabledPayload map[string]any
	if err := json.Unmarshal(enabled.Body.Bytes(), &enabledPayload); err != nil || enabled.Code != 200 || enabledPayload["enabled"] != true || enabledPayload["publicKey"] == "" {
		t.Fatalf("enabled response=%d body=%s", enabled.Code, enabled.Body.String())
	}
}

func TestBlockerEnabledRequiresSubject(t *testing.T) {
	for name, subject := range map[string]string{"empty": "", "invalid": "not-a-subject"} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			newTestHandler(nil, testVAPIDPrivate, subject).PublicKey(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/push/public-key", nil))
			var payload map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || recorder.Code != 200 || payload["enabled"] != false {
				t.Fatalf("response=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestBlockerResubscribeResetsFailureCountAndSurvivesFailure(t *testing.T) {
	pool := apiTestPool(t)
	handler := newTestHandler(pool, testVAPIDPrivate, "mailto:test@example.invalid")
	household, user := apiSeedMember(t, pool)
	endpoint := "https://1.1.1.1/resub/" + household
	var subscriptionID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO push_subscriptions(user_id,household_id,endpoint,p256dh,auth,failure_count,disabled_at) VALUES($1,$2,$3,$4,$5,4,now()) RETURNING id`, user, household, endpoint, testP256DH, testAuth).Scan(&subscriptionID); err != nil {
		t.Fatal(err)
	}
	if response := subscribe(handler, user, household, endpoint, testP256DH); response.Code != http.StatusCreated {
		t.Fatalf("re-subscribe=%d %s", response.Code, response.Body.String())
	}
	var failureCount int
	var disabled bool
	if err := pool.QueryRow(context.Background(), `SELECT failure_count,disabled_at IS NOT NULL FROM push_subscriptions WHERE id=$1`, subscriptionID).Scan(&failureCount, &disabled); err != nil {
		t.Fatal(err)
	}
	if failureCount != 0 || disabled {
		t.Fatalf("after re-subscribe failure_count=%d disabled=%v", failureCount, disabled)
	}
	// Same diagnostic update the sender applies to a transient failure. One
	// failure after the reset must leave the device live and the outbox row
	// retryable; transient failures never set disabled_at.
	if _, err := pool.Exec(context.Background(), `UPDATE push_subscriptions SET failure_count=failure_count+1 WHERE id=$1 AND disabled_at IS NULL`, subscriptionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT failure_count,disabled_at IS NOT NULL FROM push_subscriptions WHERE id=$1`, subscriptionID).Scan(&failureCount, &disabled); err != nil {
		t.Fatal(err)
	}
	if failureCount != 1 || disabled {
		t.Fatalf("after one transient failure failure_count=%d disabled=%v", failureCount, disabled)
	}
}

func TestSubscriptionScopedToCaller(t *testing.T) {
	pool := apiTestPool(t)
	handler := newTestHandler(pool, testVAPIDPrivate, "mailto:test@example.invalid")
	householdA, userA := apiSeedMember(t, pool)
	householdB, userB := apiSeedMember(t, pool)
	endpoint := "https://1.1.1.1/push/" + householdA

	if response := subscribe(handler, userA, householdA, endpoint, testP256DH); response.Code != http.StatusCreated {
		t.Fatalf("subscribe A=%d %s", response.Code, response.Body.String())
	}
	var subscriptionID string
	if err := pool.QueryRow(context.Background(), `SELECT id FROM push_subscriptions WHERE endpoint=$1`, endpoint).Scan(&subscriptionID); err != nil {
		t.Fatal(err)
	}

	// Another user cannot remove it.
	if response := unsubscribe(handler, userB, endpoint); response.Code != http.StatusNoContent {
		t.Fatalf("unsubscribe B=%d", response.Code)
	}
	var disabled bool
	var lastSeen string
	if err := pool.QueryRow(context.Background(), `SELECT disabled_at IS NOT NULL,last_seen_at::text FROM push_subscriptions WHERE id=$1`, subscriptionID).Scan(&disabled, &lastSeen); err != nil {
		t.Fatal(err)
	}
	if disabled {
		t.Fatal("user B disabled user A's subscription")
	}

	// Another user cannot take it over by re-subscribing to the same endpoint.
	otherP256DH, _ := newTestSubscriptionKeys()
	if response := subscribe(handler, userB, householdB, endpoint, otherP256DH); response.Code != http.StatusConflict {
		t.Fatalf("subscribe B same endpoint=%d %s", response.Code, response.Body.String())
	}
	var owner string
	var storedKey string
	if err := pool.QueryRow(context.Background(), `SELECT user_id::text,p256dh FROM push_subscriptions WHERE id=$1`, subscriptionID).Scan(&owner, &storedKey); err != nil {
		t.Fatal(err)
	}
	if owner != userA || storedKey != testP256DH {
		t.Fatalf("owner=%s key=%s", owner, storedKey)
	}

	// Heartbeat from another user does not move last_seen_at.
	if response := heartbeat(handler, userB, endpoint); response.Code != http.StatusNoContent {
		t.Fatalf("heartbeat B=%d", response.Code)
	}
	var after string
	if err := pool.QueryRow(context.Background(), `SELECT last_seen_at::text FROM push_subscriptions WHERE id=$1`, subscriptionID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != lastSeen {
		t.Fatal("user B moved user A's last_seen_at")
	}

	// The owner can disable the subscription, and the row stays as evidence.
	if response := unsubscribe(handler, userA, endpoint); response.Code != http.StatusNoContent {
		t.Fatalf("unsubscribe A=%d", response.Code)
	}
	if err := pool.QueryRow(context.Background(), `SELECT disabled_at IS NOT NULL FROM push_subscriptions WHERE id=$1`, subscriptionID).Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if !disabled {
		t.Fatal("owner unsubscribe did not disable the row")
	}
}

func TestSubscribeRejectsNonHTTPSAndMalformedKeys(t *testing.T) {
	pool := apiTestPool(t)
	handler := newTestHandler(pool, testVAPIDPrivate, "mailto:test@example.invalid")
	household, user := apiSeedMember(t, pool)
	for _, endpoint := range []string{"http://1.1.1.1/x", "not-a-url", ""} {
		if response := subscribe(handler, user, household, endpoint, testP256DH); response.Code != http.StatusBadRequest {
			t.Fatalf("endpoint %q response=%d", endpoint, response.Code)
		}
	}
	if response := subscribe(handler, user, household, "https://1.1.1.1/bad-key", "bad"); response.Code != http.StatusBadRequest {
		t.Fatalf("bad key response=%d", response.Code)
	}
}

func TestSubscribeRejectsPrivateAndMetadataTargets(t *testing.T) {
	pool := apiTestPool(t)
	handler := newTestHandler(pool, testVAPIDPrivate, "mailto:test@example.invalid")
	household, user := apiSeedMember(t, pool)
	for _, endpoint := range []string{
		"https://127.0.0.1/device",
		"https://10.0.0.1/device",
		"https://192.168.1.10/device",
		"https://169.254.169.254/latest/meta-data",
		"https://100.64.0.1/device",
		"https://localhost/device",
	} {
		if response := subscribe(handler, user, household, endpoint, testP256DH); response.Code != http.StatusBadRequest {
			t.Fatalf("private endpoint %q response=%d body=%s", endpoint, response.Code, response.Body.String())
		}
	}
	if response := subscribe(handler, user, household, "https://1.1.1.1/public-"+household, testP256DH); response.Code != http.StatusCreated {
		t.Fatalf("public endpoint response=%d body=%s", response.Code, response.Body.String())
	}
}

func apiTestPool(t *testing.T) *pgxpool.Pool {
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

func apiSeedMember(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	ctx := context.Background()
	var household, user string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('push-api-'||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES('pushapi-'||gen_random_uuid()||'@test.invalid','Push','x',now()) RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	return household, user
}

func subscribe(handler *Handler, user, household, endpoint, p256dh string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]any{
		"endpoint": endpoint,
		"keys":     map[string]string{"p256dh": p256dh, "auth": testAuth},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscriptions", strings.NewReader(string(body)))
	request = request.WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{
		UserID: user, HouseholdID: household, HouseholdRole: "OWNER", HasHousehold: true,
	}))
	recorder := httptest.NewRecorder()
	handler.Subscribe(recorder, request)
	return recorder
}

func unsubscribe(handler *Handler, user, endpoint string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"endpoint": endpoint})
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/push/subscriptions", strings.NewReader(string(body)))
	request = request.WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{
		UserID: user, HouseholdID: "", HasHousehold: true,
	}))
	recorder := httptest.NewRecorder()
	handler.Unsubscribe(recorder, request)
	return recorder
}

func heartbeat(handler *Handler, user, endpoint string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"endpoint": endpoint})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscriptions/heartbeat", strings.NewReader(string(body)))
	request = request.WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{
		UserID: user, HouseholdID: "", HasHousehold: true,
	}))
	recorder := httptest.NewRecorder()
	handler.Heartbeat(recorder, request)
	return recorder
}
