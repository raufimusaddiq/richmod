package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testIdentity = googleIdentity{sub: "google-sub-1", email: "someone@example.test", name: "Some One"}

func TestGoogleStartRedirectsToGoogleWithPKCE(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	rec, flowCookie := startFlow(t, h, "")
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control=%q", rec.Header().Get("Cache-Control"))
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(location.String(), f.endpoints().Auth+"?") {
		t.Fatalf("location=%q", location)
	}
	query := location.Query()
	want := map[string]string{
		"response_type":         "code",
		"client_id":             testClientID,
		"redirect_uri":          testWebOrigin + GoogleCallbackPath,
		"scope":                 "openid email profile",
		"code_challenge_method": "S256",
		"prompt":                "select_account",
	}
	for key, value := range want {
		if query.Get(key) != value {
			t.Fatalf("%s=%q want %q", key, query.Get(key), value)
		}
	}
	for _, key := range []string{"state", "nonce", "code_challenge"} {
		if query.Get(key) == "" {
			t.Fatalf("missing %s", key)
		}
	}
	if flowCookie == nil || flowCookie.Value == "" || !flowCookie.HttpOnly || !flowCookie.Secure || flowCookie.Path != "/" || flowCookie.MaxAge != 600 || flowCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("flow cookie=%+v", flowCookie)
	}
	if flowCookie.Value == query.Get("state") {
		t.Fatal("browser binding must differ from state")
	}
}

func TestGoogleStartRandomnessIsUniquePerFlow(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		rec, cookie := startFlow(t, h, "")
		query, _ := url.Parse(rec.Header().Get("Location"))
		for _, value := range []string{query.Query().Get("state"), query.Query().Get("nonce"), query.Query().Get("code_challenge"), cookie.Value} {
			if seen[value] {
				t.Fatalf("value %q repeated across flows", value)
			}
			seen[value] = true
		}
	}
}

func TestGoogleStartUnavailableWithoutCredentials(t *testing.T) {
	for name, handler := range map[string]*Handler{
		"unconfigured": NewHandler(nil, true),
		"no secret":    NewHandler(nil, true).WithGoogle(GoogleConfig{ClientID: testClientID, WebOrigin: testWebOrigin}),
		"no id":        NewHandler(nil, true).WithGoogle(GoogleConfig{ClientSecret: testClientSecret, WebOrigin: testWebOrigin}),
	} {
		t.Run(name, func(t *testing.T) {
			rec, cookie := startFlow(t, handler, "")
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login?error=google_unavailable" {
				t.Fatalf("status=%d location=%q", rec.Code, rec.Header().Get("Location"))
			}
			if cookie != nil {
				t.Fatal("flow cookie must not be set when Google is unavailable")
			}
		})
	}
}

func TestGoogleStartRefusesWhenFlowStoreIsFull(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	for i := 0; i < maxFlows; i++ {
		var key [32]byte
		key[0], key[1] = byte(i), byte(i>>8)
		h.google.flows.flows[key] = googleFlow{expires: time.Now().Add(time.Hour)}
	}
	rec, cookie := startFlow(t, h, "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login?error=google_failed" || cookie != nil {
		t.Fatalf("status=%d location=%q cookie=%v", rec.Code, rec.Header().Get("Location"), cookie)
	}
}

func TestGoogleStartEvictsExpiredFlowsWhenFull(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	for i := 0; i < maxFlows; i++ {
		var key [32]byte
		key[0], key[1] = byte(i), byte(i>>8)
		h.google.flows.flows[key] = googleFlow{expires: time.Now().Add(-time.Minute)}
	}
	rec, _ := startFlow(t, h, "")
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func assertRejected(t *testing.T, result flowResult, want string) {
	t.Helper()
	if result.rec.Code != http.StatusSeeOther || result.location != want {
		t.Fatalf("status=%d location=%q want %q", result.rec.Code, result.location, want)
	}
	if result.session != nil {
		t.Fatal("a session cookie was issued for a rejected sign-in")
	}
	if result.flow == nil || result.flow.MaxAge >= 0 {
		t.Fatalf("flow cookie not cleared: %+v", result.flow)
	}
	if result.rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control=%q", result.rec.Header().Get("Cache-Control"))
	}
}

func TestGoogleCallbackRejectsInvalidIDTokens(t *testing.T) {
	cases := map[string]func(map[string]any){
		"unverified email":       func(c map[string]any) { c["email_verified"] = false },
		"missing email verified": func(c map[string]any) { delete(c, "email_verified") },
		"wrong nonce":            func(c map[string]any) { c["nonce"] = "someone-elses-nonce" },
		"missing nonce":          func(c map[string]any) { delete(c, "nonce") },
		"wrong audience":         func(c map[string]any) { c["aud"] = "another-client" },
		"audience list without azp": func(c map[string]any) {
			c["aud"] = []string{testClientID, "another-client"}
			delete(c, "azp")
		},
		"audience list with foreign azp": func(c map[string]any) {
			c["aud"] = []string{testClientID, "another-client"}
			c["azp"] = "another-client"
		},
		"wrong issuer":      func(c map[string]any) { c["iss"] = "https://evil.example" },
		"expired":           func(c map[string]any) { c["exp"] = time.Now().Add(-2 * time.Minute).Unix() },
		"issued in future":  func(c map[string]any) { c["iat"] = time.Now().Add(5 * time.Minute).Unix() },
		"missing subject":   func(c map[string]any) { delete(c, "sub") },
		"overlong subject":  func(c map[string]any) { c["sub"] = strings.Repeat("9", 256) },
		"missing email":     func(c map[string]any) { delete(c, "email") },
		"missing exp":       func(c map[string]any) { delete(c, "exp") },
		"missing iat":       func(c map[string]any) { delete(c, "iat") },
		"audience mismatch": func(c map[string]any) { c["aud"] = []string{"another-client"} },
	}
	for name, tweak := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeGoogle(t)
			h := newTestHandler(f, nil)
			assertRejected(t, runGoogleFlow(t, h, f, "", testIdentity, tweak), "/login?error=google_failed")
		})
	}
}

func TestGoogleCallbackRejectsForgedTokens(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	start, _ := startFlow(t, h, "")
	location := start.Header().Get("Location")
	parsed, _ := url.Parse(location)
	code := f.approve(location, testIdentity, nil)
	f.mu.Lock()
	entry := f.codes[code]
	f.mu.Unlock()
	attackerKey := newFakeGoogle(t)
	forged := attackerKey.signWith(attackerKey.key, map[string]any{"alg": "RS256", "kid": f.kid}, entry.claims)
	ks := newKeySet(http.DefaultClient, f.endpoints().JWKS, time.Now)
	if _, err := ks.verify(context.Background(), f.endpoints(), testClientID, forged, parsed.Query().Get("nonce")); err == nil {
		t.Fatal("token signed by a foreign key was accepted")
	}
	none := "eyJhbGciOiJub25lIiwia2lkIjoidGVzdC1rZXktMSJ9." + strings.Split(f.sign(entry.claims), ".")[1] + "."
	if _, err := ks.verify(context.Background(), f.endpoints(), testClientID, none, parsed.Query().Get("nonce")); err == nil {
		t.Fatal("unsigned token was accepted")
	}
	hs := f.signWith(f.key, map[string]any{"alg": "HS256", "kid": f.kid}, entry.claims)
	if _, err := ks.verify(context.Background(), f.endpoints(), testClientID, hs, parsed.Query().Get("nonce")); err == nil {
		t.Fatal("non-RS256 token was accepted")
	}
}

func TestVerifyAcceptsStringTrueAndAudienceListWithAzp(t *testing.T) {
	f := newFakeGoogle(t)
	ks := newKeySet(http.DefaultClient, f.endpoints().JWKS, time.Now)
	claims := baseClaims(testIdentity, "n-1", time.Now())
	claims["email_verified"] = "true"
	claims["aud"] = []string{testClientID, "another-client"}
	got, err := ks.verify(context.Background(), f.endpoints(), testClientID, f.sign(claims), "n-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != testIdentity.sub || got.Email != testIdentity.email || got.Name != testIdentity.name {
		t.Fatalf("claims=%+v", got)
	}
	claims["email_verified"] = "false"
	if _, err := ks.verify(context.Background(), f.endpoints(), testClientID, f.sign(claims), "n-1"); err == nil {
		t.Fatal(`email_verified "false" was accepted`)
	}
}

func TestVerifyToleratesClockSkewOnly(t *testing.T) {
	f := newFakeGoogle(t)
	ks := newKeySet(http.DefaultClient, f.endpoints().JWKS, time.Now)
	claims := baseClaims(testIdentity, "n-1", time.Now())
	claims["exp"] = time.Now().Add(-30 * time.Second).Unix()
	claims["iat"] = time.Now().Add(30 * time.Second).Unix()
	if _, err := ks.verify(context.Background(), f.endpoints(), testClientID, f.sign(claims), "n-1"); err != nil {
		t.Fatalf("token inside the skew window rejected: %v", err)
	}
	claims["exp"] = time.Now().Add(-90 * time.Second).Unix()
	if _, err := ks.verify(context.Background(), f.endpoints(), testClientID, f.sign(claims), "n-1"); err == nil {
		t.Fatal("token beyond the skew window accepted")
	}
}

func TestKeySetCachesAndRefetchesOnUnknownKid(t *testing.T) {
	f := newFakeGoogle(t)
	var clock atomic.Int64
	base := time.Now()
	now := func() time.Time { return base.Add(time.Duration(clock.Load())) }
	ks := newKeySet(http.DefaultClient, f.endpoints().JWKS, now)
	verify := func() error {
		_, err := ks.verify(context.Background(), f.endpoints(), testClientID, f.sign(baseClaims(testIdentity, "n", now())), "n")
		return err
	}
	if err := verify(); err != nil {
		t.Fatal(err)
	}
	if err := verify(); err != nil || f.hits() != 1 {
		t.Fatalf("err=%v hits=%d want cached key", err, f.hits())
	}
	f.kid = "rotated-key"
	if err := verify(); err == nil || f.hits() != 1 {
		t.Fatalf("unknown kid refetched inside the retry window: err=%v hits=%d", err, f.hits())
	}
	clock.Store(int64(2 * jwksMinRetry))
	f.mu.Lock()
	f.jwksKeys = func() []map[string]string { return []map[string]string{f.publicJWK("rotated-key")} }
	f.mu.Unlock()
	if err := verify(); err != nil || f.hits() != 2 {
		t.Fatalf("rotated key not picked up: err=%v hits=%d", err, f.hits())
	}
	clock.Store(int64(2*jwksMinRetry + jwksTTL))
	if err := verify(); err != nil || f.hits() != 3 {
		t.Fatalf("expired cache not refreshed: err=%v hits=%d", err, f.hits())
	}
}

func TestKeySetTransientFailureDoesNotSuppressNextFetch(t *testing.T) {
	f := newFakeGoogle(t)
	var clock atomic.Int64
	base := time.Now()
	now := func() time.Time { return base.Add(time.Duration(clock.Load())) }
	ks := newKeySet(http.DefaultClient, f.endpoints().JWKS, now)
	verify := func() error {
		_, err := ks.verify(context.Background(), f.endpoints(), testClientID, f.sign(baseClaims(testIdentity, "n", now())), "n")
		return err
	}
	if err := verify(); err != nil {
		t.Fatal(err)
	}
	clock.Store(int64(jwksTTL + time.Second))
	goodURL := ks.url
	ks.url = "http://127.0.0.1:1/unreachable"
	f.kid = "rotated-after-blip"
	f.mu.Lock()
	f.jwksKeys = func() []map[string]string { return []map[string]string{f.publicJWK("rotated-after-blip")} }
	f.mu.Unlock()
	if err := verify(); err == nil {
		t.Fatal("expected verification failure when JWKS fetch fails for a rotated key")
	}
	ks.url = goodURL
	clock.Store(int64(jwksTTL + 2*time.Second))
	if err := verify(); err != nil {
		t.Fatalf("rotated key rejected 1s after transient JWKS failure: %v", err)
	}
}

func TestKeySetIgnoresNonRS256Keys(t *testing.T) {
	f := newFakeGoogle(t)
	f.jwksKeys = func() []map[string]string {
		good := f.publicJWK(f.kid)
		ec := map[string]string{"kty": "EC", "kid": f.kid, "crv": "P-256"}
		encryption := f.publicJWK("enc-key")
		encryption["use"] = "enc"
		other := f.publicJWK("es-key")
		other["alg"] = "ES256"
		return []map[string]string{ec, encryption, other, good}
	}
	ks := newKeySet(http.DefaultClient, f.endpoints().JWKS, time.Now)
	keys, err := ks.fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[f.kid] == nil {
		t.Fatalf("keys=%v", keys)
	}
}

func TestGoogleCallbackRejectsStateAndBrowserBinding(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	start, cookie := startFlow(t, h, "")
	location := start.Header().Get("Location")
	parsed, _ := url.Parse(location)
	state := parsed.Query().Get("state")
	code := f.approve(location, testIdentity, nil)

	t.Run("unknown state", func(t *testing.T) {
		rec := callback(t, h, url.Values{"state": {"forged-state"}, "code": {code}}, cookie)
		if rec.Header().Get("Location") != "/login?error=google_failed" {
			t.Fatalf("location=%q", rec.Header().Get("Location"))
		}
	})
	t.Run("missing state", func(t *testing.T) {
		rec := callback(t, h, url.Values{"code": {code}}, cookie)
		if rec.Header().Get("Location") != "/login?error=google_failed" {
			t.Fatalf("location=%q", rec.Header().Get("Location"))
		}
	})
	t.Run("foreign browser cookie", func(t *testing.T) {
		rec := callback(t, h, url.Values{"state": {state}, "code": {code}}, &http.Cookie{Name: OIDCFlowCookieName, Value: "another-browser"})
		if rec.Header().Get("Location") != "/login?error=google_failed" {
			t.Fatalf("location=%q", rec.Header().Get("Location"))
		}
	})

	start2, cookie2 := startFlow(t, h, "")
	parsed2, _ := url.Parse(start2.Header().Get("Location"))
	code2 := f.approve(start2.Header().Get("Location"), testIdentity, nil)
	t.Run("missing browser cookie", func(t *testing.T) {
		rec := callback(t, h, url.Values{"state": {parsed2.Query().Get("state")}, "code": {code2}})
		if rec.Header().Get("Location") != "/login?error=google_failed" {
			t.Fatalf("location=%q", rec.Header().Get("Location"))
		}
		rec = callback(t, h, url.Values{"state": {parsed2.Query().Get("state")}, "code": {code2}}, cookie2)
		if rec.Header().Get("Location") != "/login?error=google_failed" {
			t.Fatalf("a state attempted without its cookie must be burned: location=%q", rec.Header().Get("Location"))
		}
	})
}

func TestGoogleCallbackIsSingleUse(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	start, cookie := startFlow(t, h, "")
	location := start.Header().Get("Location")
	parsed, _ := url.Parse(location)
	state := parsed.Query().Get("state")
	code := f.approve(location, testIdentity, nil)
	first := callback(t, h, url.Values{"state": {state}, "error": {"access_denied"}}, cookie)
	if first.Header().Get("Location") != "/login?error=google_denied" {
		t.Fatalf("location=%q", first.Header().Get("Location"))
	}
	replay := callback(t, h, url.Values{"state": {state}, "code": {code}}, cookie)
	if replay.Header().Get("Location") != "/login?error=google_failed" {
		t.Fatalf("replayed state accepted: location=%q", replay.Header().Get("Location"))
	}
}

func TestGoogleCallbackRejectsExpiredFlow(t *testing.T) {
	f := newFakeGoogle(t)
	var offset atomic.Int64
	h := NewHandler(nil, true).WithGoogle(GoogleConfig{
		Endpoints:    f.endpoints(),
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		WebOrigin:    testWebOrigin,
		Now:          func() time.Time { return time.Now().Add(time.Duration(offset.Load())) },
	})
	start, cookie := startFlow(t, h, "")
	location := start.Header().Get("Location")
	parsed, _ := url.Parse(location)
	code := f.approve(location, testIdentity, nil)
	offset.Store(int64(oidcFlowTTL + time.Second))
	rec := callback(t, h, url.Values{"state": {parsed.Query().Get("state")}, "code": {code}}, cookie)
	if rec.Header().Get("Location") != "/login?error=google_failed" {
		t.Fatalf("expired flow accepted: location=%q", rec.Header().Get("Location"))
	}
}

func TestGoogleCallbackRejectsBadExchange(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	start, cookie := startFlow(t, h, "")
	parsed, _ := url.Parse(start.Header().Get("Location"))
	rec := callback(t, h, url.Values{"state": {parsed.Query().Get("state")}, "code": {"never-issued"}}, cookie)
	if rec.Header().Get("Location") != "/login?error=google_failed" {
		t.Fatalf("location=%q", rec.Header().Get("Location"))
	}
	start, cookie = startFlow(t, h, "")
	parsed, _ = url.Parse(start.Header().Get("Location"))
	rec = callback(t, h, url.Values{"state": {parsed.Query().Get("state")}}, cookie)
	if rec.Header().Get("Location") != "/login?error=google_failed" {
		t.Fatalf("missing code: location=%q", rec.Header().Get("Location"))
	}
}

func TestGoogleCallbackRejectsPKCEVerifierMismatch(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	start, cookie := startFlow(t, h, "")
	location := start.Header().Get("Location")
	parsed, _ := url.Parse(location)
	code := f.approve(location, testIdentity, nil)
	f.mu.Lock()
	entry := f.codes[code]
	entry.challenge = pkceChallenge("a-different-verifier")
	f.codes[code] = entry
	f.mu.Unlock()
	rec := callback(t, h, url.Values{"state": {parsed.Query().Get("state")}, "code": {code}}, cookie)
	if rec.Header().Get("Location") != "/login?error=google_failed" {
		t.Fatalf("location=%q", rec.Header().Get("Location"))
	}
}

func TestPKCEChallengeMatchesRFC7636Example(t *testing.T) {
	if got := pkceChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challenge=%q", got)
	}
}

func TestGoogleClaimsHelpers(t *testing.T) {
	if got := googleDisplayName(GoogleClaims{Name: "  Budi Santoso "}, "budi@example.test"); got != "Budi Santoso" {
		t.Fatalf("name=%q", got)
	}
	if got := googleDisplayName(GoogleClaims{}, "budi@example.test"); got != "budi" {
		t.Fatalf("fallback=%q", got)
	}
	if got := googleDisplayName(GoogleClaims{Name: strings.Repeat("é", 200)}, "x@example.test"); len([]rune(got)) != 120 {
		t.Fatalf("truncated runes=%d", len([]rune(got)))
	}
	if got := googleDisplayName(GoogleClaims{}, "@example.test"); got != "@example.test" {
		t.Fatalf("empty local part fallback=%q", got)
	}
}

func TestGooglePrepareInviteSetsHttpOnlyCookieAndStartConsumesIt(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	rec, cookie := prepareInvite(t, h, `{"token":"  secret-invite-token  "}`)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache-control=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if cookie == nil || cookie.Value != "secret-invite-token" || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/api/v1/auth/google" || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("invite cookie=%+v", cookie)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/start?invite=url-query-must-be-ignored", nil)
	req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	startRec := httptest.NewRecorder()
	h.GoogleStart(startRec, req)
	var cleared *http.Cookie
	for _, c := range startRec.Result().Cookies() {
		if c.Name == OIDCInviteCookieName {
			cleared = c
		}
	}
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("invite cookie not cleared on /google/start: %+v", cleared)
	}
	parsed, _ := url.Parse(startRec.Header().Get("Location"))
	flow, ok := h.google.flows.take(sha256Of(parsed.Query().Get("state")), time.Now())
	if !ok || flow.inviteToken != "secret-invite-token" {
		t.Fatalf("flow=%+v ok=%v", flow, ok)
	}

	reqQueryOnly := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/start?invite=leaked-in-url", nil)
	startQueryRec := httptest.NewRecorder()
	h.GoogleStart(startQueryRec, reqQueryOnly)
	parsedQuery, _ := url.Parse(startQueryRec.Header().Get("Location"))
	flowQuery, ok := h.google.flows.take(sha256Of(parsedQuery.Query().Get("state")), time.Now())
	if !ok || flowQuery.inviteToken != "" {
		t.Fatalf("query invite parameter must be ignored, got flow=%+v", flowQuery)
	}
}

func TestGooglePrepareInviteValidationAndUnavailable(t *testing.T) {
	unconfigured := NewHandler(nil, true)
	rec, cookie := prepareInvite(t, unconfigured, `{"token":"tok"}`)
	if rec.Code != http.StatusServiceUnavailable || cookie != nil {
		t.Fatalf("unconfigured status=%d cookie=%v", rec.Code, cookie)
	}
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	for _, body := range []string{`{}`, `{"token":"   "}`, `invalid`, `{"token":"a","unknown":1}`} {
		rec, cookie := prepareInvite(t, h, body)
		if rec.Code != http.StatusBadRequest || cookie != nil {
			t.Fatalf("body=%q status=%d cookie=%v", body, rec.Code, cookie)
		}
	}
}

func TestGoogleCallbackClearsInviteCookie(t *testing.T) {
	f := newFakeGoogle(t)
	h := newTestHandler(f, nil)
	rec := callback(t, h, url.Values{"state": {"unknown"}})
	var cleared *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == OIDCInviteCookieName {
			cleared = c
		}
	}
	if cleared == nil || cleared.MaxAge >= 0 || cleared.Path != "/api/v1/auth/google" {
		t.Fatalf("invite cookie not cleared on callback: %+v", cleared)
	}
}
