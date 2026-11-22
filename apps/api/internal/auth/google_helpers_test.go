package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testClientID     = "test-client-id.apps.googleusercontent.com"
	testClientSecret = "test-client-secret"
	testWebOrigin    = "https://finance.example.test"
)

type fakeCode struct {
	claims    map[string]any
	challenge string
}

type fakeGoogle struct {
	t        *testing.T
	server   *httptest.Server
	key      *rsa.PrivateKey
	kid      string
	mu       sync.Mutex
	codes    map[string]fakeCode
	jwksHits int
	jwksKeys func() []map[string]string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGoogle{t: t, key: key, kid: "test-key-1", codes: map[string]fakeCode{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", f.serveJWKS)
	mux.HandleFunc("/token", f.serveToken)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGoogle) endpoints() Endpoints {
	return Endpoints{
		Issuer: "https://accounts.google.com",
		Auth:   f.server.URL + "/auth",
		Token:  f.server.URL + "/token",
		JWKS:   f.server.URL + "/jwks",
		Issued: []string{"https://accounts.google.com", "accounts.google.com"},
	}
}

func (f *fakeGoogle) publicJWK(kid string) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"kid": kid,
		"n":   base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes()),
	}
}

func (f *fakeGoogle) serveJWKS(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.jwksHits++
	keys := []map[string]string{f.publicJWK(f.kid)}
	if f.jwksKeys != nil {
		keys = f.jwksKeys()
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
}

func (f *fakeGoogle) hits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jwksHits
}

func (f *fakeGoogle) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	entry, ok := f.codes[r.PostForm.Get("code")]
	delete(f.codes, r.PostForm.Get("code"))
	f.mu.Unlock()
	verifierDigest := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	valid := ok &&
		r.Method == http.MethodPost &&
		r.PostForm.Get("grant_type") == "authorization_code" &&
		r.PostForm.Get("client_id") == testClientID &&
		r.PostForm.Get("client_secret") == testClientSecret &&
		r.PostForm.Get("redirect_uri") == testWebOrigin+GoogleCallbackPath &&
		base64.RawURLEncoding.EncodeToString(verifierDigest[:]) == entry.challenge
	if !valid {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id_token": f.sign(entry.claims), "access_token": "unused"})
}

func (f *fakeGoogle) signWith(key *rsa.PrivateKey, header map[string]any, claims map[string]any) string {
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (f *fakeGoogle) sign(claims map[string]any) string {
	return f.signWith(f.key, map[string]any{"alg": "RS256", "typ": "JWT", "kid": f.kid}, claims)
}

type googleIdentity struct {
	sub   string
	email string
	name  string
}

func baseClaims(identity googleIdentity, nonce string, now time.Time) map[string]any {
	return map[string]any{
		"iss":            "https://accounts.google.com",
		"aud":            testClientID,
		"azp":            testClientID,
		"sub":            identity.sub,
		"email":          identity.email,
		"email_verified": true,
		"name":           identity.name,
		"nonce":          nonce,
		"iat":            now.Unix(),
		"exp":            now.Add(time.Hour).Unix(),
	}
}

func (f *fakeGoogle) approve(location string, identity googleIdentity, tweak func(map[string]any)) string {
	f.t.Helper()
	parsed, err := url.Parse(location)
	if err != nil {
		f.t.Fatal(err)
	}
	query := parsed.Query()
	claims := baseClaims(identity, query.Get("nonce"), time.Now())
	if tweak != nil {
		tweak(claims)
	}
	rawCode := make([]byte, 12)
	_, _ = rand.Read(rawCode)
	code := base64.RawURLEncoding.EncodeToString(rawCode)
	f.mu.Lock()
	f.codes[code] = fakeCode{claims: claims, challenge: query.Get("code_challenge")}
	f.mu.Unlock()
	return code
}

func newTestHandler(f *fakeGoogle, service *Service) *Handler {
	return NewHandler(service, true).WithGoogle(GoogleConfig{
		Endpoints:    f.endpoints(),
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		WebOrigin:    testWebOrigin,
	})
}

func prepareInvite(t *testing.T, h *Handler, body string) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, GoogleInvitePath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.GooglePrepareInvite(rec, req)
	var inviteCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == OIDCInviteCookieName {
			inviteCookie = c
		}
	}
	return rec, inviteCookie
}

func startFlow(t *testing.T, h *Handler, invite string) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/start", nil)
	if invite != "" {
		prepRec, inviteCookie := prepareInvite(t, h, `{"token":`+mustJSON(invite)+`}`)
		if prepRec.Code != http.StatusNoContent || inviteCookie == nil {
			t.Fatalf("prepare invite status=%d cookie=%v", prepRec.Code, inviteCookie)
		}
		req.AddCookie(&http.Cookie{Name: inviteCookie.Name, Value: inviteCookie.Value})
	}
	rec := httptest.NewRecorder()
	h.GoogleStart(rec, req)
	var flowCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == OIDCFlowCookieName {
			flowCookie = c
		}
	}
	return rec, flowCookie
}

func mustJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func callback(t *testing.T, h *Handler, params url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, GoogleCallbackPath+"?"+params.Encode(), nil)
	for _, c := range cookies {
		if c != nil {
			req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	rec := httptest.NewRecorder()
	h.GoogleCallback(rec, req)
	return rec
}

type flowResult struct {
	rec      *httptest.ResponseRecorder
	location string
	session  *http.Cookie
	flow     *http.Cookie
}

func runGoogleFlow(t *testing.T, h *Handler, f *fakeGoogle, invite string, identity googleIdentity, tweak func(map[string]any), extra ...*http.Cookie) flowResult {
	t.Helper()
	start, flowCookie := startFlow(t, h, invite)
	if start.Code != http.StatusFound {
		t.Fatalf("start status=%d location=%q", start.Code, start.Header().Get("Location"))
	}
	location := start.Header().Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	code := f.approve(location, identity, tweak)
	cookies := append([]*http.Cookie{flowCookie}, extra...)
	rec := callback(t, h, url.Values{"state": {parsed.Query().Get("state")}, "code": {code}}, cookies...)
	result := flowResult{rec: rec, location: rec.Header().Get("Location")}
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case SessionCookieName:
			result.session = c
		case OIDCFlowCookieName:
			result.flow = c
		}
	}
	return result
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
