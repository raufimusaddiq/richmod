package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	OIDCFlowCookieName   = "finance_oidc"
	OIDCInviteCookieName = "finance_oidc_invite"
	GoogleInvitePath     = "/api/v1/auth/google/invite"
	GoogleCallbackPath   = "/api/v1/auth/google/callback"
	oidcFlowTTL          = 10 * time.Minute
	maxFlows             = 4096
)

type GoogleConfig struct {
	Endpoints    Endpoints
	ClientID     string
	ClientSecret string
	WebOrigin    string
	HTTPClient   *http.Client
	Now          func() time.Time
}

type googleFlow struct {
	nonce       string
	verifier    string
	browserHash [32]byte
	inviteToken string
	expires     time.Time
}

type flowStore struct {
	mu    sync.Mutex
	flows map[[32]byte]googleFlow
}

func newFlowStore() *flowStore {
	return &flowStore{flows: make(map[[32]byte]googleFlow)}
}

func (s *flowStore) put(stateHash [32]byte, flow googleFlow, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.flows) >= maxFlows {
		for key, existing := range s.flows {
			if !now.Before(existing.expires) {
				delete(s.flows, key)
			}
		}
		if len(s.flows) >= maxFlows {
			return false
		}
	}
	s.flows[stateHash] = flow
	return true
}

func (s *flowStore) take(stateHash [32]byte, now time.Time) (googleFlow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow, ok := s.flows[stateHash]
	if !ok {
		return googleFlow{}, false
	}
	delete(s.flows, stateHash)
	if !now.Before(flow.expires) {
		return googleFlow{}, false
	}
	return flow, true
}

type googleRuntime struct {
	cfg   GoogleConfig
	keys  *keySet
	flows *flowStore
}

func (h *Handler) WithGoogle(cfg GoogleConfig) *Handler {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: exchangeLimit}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if len(cfg.Endpoints.Issued) == 0 && cfg.Endpoints.Auth == "" {
		cfg.Endpoints = GoogleEndpoints()
	}
	h.google = &googleRuntime{
		cfg:   cfg,
		keys:  newKeySet(cfg.HTTPClient, cfg.Endpoints.JWKS, cfg.Now),
		flows: newFlowStore(),
	}
	return h
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func sha256Of(value string) [32]byte {
	return sha256.Sum256([]byte(value))
}

func (g *googleRuntime) redirectURI() string {
	return strings.TrimRight(g.cfg.WebOrigin, "/") + GoogleCallbackPath
}

func (h *Handler) GooglePrepareInvite(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	g := h.google
	if g == nil || g.cfg.ClientID == "" || g.cfg.ClientSecret == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "google_unavailable"})
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid invite request"})
		return
	}
	token := strings.TrimSpace(in.Token)
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid invite request"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     OIDCInviteCookieName,
		Value:    token,
		Path:     "/api/v1/auth/google",
		MaxAge:   int(oidcFlowTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) clearOIDCInviteCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     OIDCInviteCookieName,
		Value:    "",
		Path:     "/api/v1/auth/google",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) GoogleStart(w http.ResponseWriter, r *http.Request) {
	h.clearOIDCInviteCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	g := h.google
	if g == nil || g.cfg.ClientID == "" || g.cfg.ClientSecret == "" {
		http.Redirect(w, r, "/login?error=google_unavailable", http.StatusSeeOther)
		return
	}
	inviteToken := ""
	if inviteCookie, err := r.Cookie(OIDCInviteCookieName); err == nil {
		inviteToken = strings.TrimSpace(inviteCookie.Value)
	}
	state, err1 := randomToken()
	nonce, err2 := randomToken()
	verifier, err3 := randomToken()
	browser, err4 := randomToken()
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		slog.Error("google oidc: generate flow secrets", "error", err)
		http.Redirect(w, r, "/login?error=google_failed", http.StatusSeeOther)
		return
	}
	now := g.cfg.Now()
	flow := googleFlow{
		nonce:       nonce,
		verifier:    verifier,
		browserHash: sha256Of(browser),
		inviteToken: inviteToken,
		expires:     now.Add(oidcFlowTTL),
	}
	if !g.flows.put(sha256Of(state), flow, now) {
		http.Redirect(w, r, "/login?error=google_failed", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     OIDCFlowCookieName,
		Value:    browser,
		Path:     "/",
		MaxAge:   int(oidcFlowTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {g.cfg.ClientID},
		"redirect_uri":          {g.redirectURI()},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
	}
	http.Redirect(w, r, g.cfg.Endpoints.Auth+"?"+query.Encode(), http.StatusFound)
}

func (h *Handler) clearOIDCFlowCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     OIDCFlowCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	h.clearOIDCFlowCookie(w)
	h.clearOIDCInviteCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	g := h.google
	if g == nil || g.cfg.ClientID == "" || g.cfg.ClientSecret == "" {
		http.Redirect(w, r, "/login?error=google_unavailable", http.StatusSeeOther)
		return
	}
	query := r.URL.Query()
	state := query.Get("state")
	if state == "" {
		http.Redirect(w, r, "/login?error=google_failed", http.StatusSeeOther)
		return
	}
	flow, ok := g.flows.take(sha256Of(state), g.cfg.Now())
	if !ok {
		http.Redirect(w, r, "/login?error=google_failed", http.StatusSeeOther)
		return
	}
	browser, err := r.Cookie(OIDCFlowCookieName)
	if err != nil {
		http.Redirect(w, r, "/login?error=google_failed", http.StatusSeeOther)
		return
	}
	browserHash := sha256Of(browser.Value)
	if subtle.ConstantTimeCompare(browserHash[:], flow.browserHash[:]) != 1 {
		http.Redirect(w, r, "/login?error=google_failed", http.StatusSeeOther)
		return
	}
	if query.Get("error") != "" {
		http.Redirect(w, r, "/login?error=google_denied", http.StatusSeeOther)
		return
	}
	code := query.Get("code")
	if code == "" {
		http.Redirect(w, r, "/login?error=google_failed", http.StatusSeeOther)
		return
	}
	rawToken, err := exchange(r.Context(), g.cfg.HTTPClient, g.cfg.Endpoints, g.cfg.ClientID, g.cfg.ClientSecret, g.redirectURI(), code, flow.verifier)
	if err != nil {
		slog.Warn("google oidc: code exchange failed", "error", err)
		http.Redirect(w, r, "/login?error=google_failed", http.StatusSeeOther)
		return
	}
	claims, err := g.keys.verify(r.Context(), g.cfg.Endpoints, g.cfg.ClientID, rawToken, flow.nonce)
	if err != nil {
		slog.Warn("google oidc: id token rejected", "error", err)
		http.Redirect(w, r, "/login?error=google_failed", http.StatusSeeOther)
		return
	}
	var (
		token     string
		expiresAt time.Time
	)
	if flow.inviteToken != "" {
		_, token, expiresAt, err = h.service.AcceptDashboardInviteWithGoogle(r.Context(), flow.inviteToken, claims)
	} else {
		_, token, expiresAt, err = h.service.LoginWithGoogle(r.Context(), claims)
	}
	if err != nil {
		http.Redirect(w, r, "/login?error="+googleErrorCode(err, flow.inviteToken != ""), http.StatusSeeOther)
		return
	}
	if previous, err := r.Cookie(SessionCookieName); err == nil && previous.Value != "" {
		if err := h.service.Logout(r.Context(), previous.Value); err != nil {
			slog.Warn("google oidc: revoke previous session failed", "error", err)
		}
	}
	h.setSessionCookie(w, token, expiresAt)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func googleErrorCode(err error, invite bool) string {
	switch {
	case errors.Is(err, ErrGoogleEmailMismatch):
		return "google_email_mismatch"
	case errors.Is(err, ErrInviteRequired):
		return "invite_required"
	case errors.Is(err, ErrInvalidCredentials) && invite:
		return "invite_invalid"
	case errors.Is(err, ErrTenantInvariantViolation):
		slog.Error("tenant invariant violation", "error_category", "TENANT_INVARIANT_VIOLATION")
		return "google_failed"
	default:
		if !errors.Is(err, ErrInvalidCredentials) {
			slog.Error("google oidc: sign-in failed", "error", err)
		}
		return "google_failed"
	}
}
