package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	jwksTTL       = time.Hour
	jwksMinRetry  = time.Minute
	exchangeLimit = 10 * time.Second
	clockSkew     = 60 * time.Second
	maxBody       = 1 << 20
)

type Endpoints struct {
	Issuer string
	Auth   string
	Token  string
	JWKS   string
	Issued []string
}

func GoogleEndpoints() Endpoints {
	return Endpoints{
		Issuer: "https://accounts.google.com",
		Auth:   "https://accounts.google.com/o/oauth2/v2/auth",
		Token:  "https://oauth2.googleapis.com/token",
		JWKS:   "https://www.googleapis.com/oauth2/v3/certs",
		Issued: []string{"https://accounts.google.com", "accounts.google.com"},
	}
}

type GoogleClaims struct {
	Subject string
	Email   string
	Name    string
}

var errOIDC = errors.New("oidc verification failed")

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type keySet struct {
	client  *http.Client
	url     string
	now     func() time.Time
	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
	tried   time.Time
}

func newKeySet(client *http.Client, jwksURL string, now func() time.Time) *keySet {
	return &keySet{client: client, url: jwksURL, now: now}
}

func (k *keySet) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	fresh := k.keys != nil && now.Sub(k.fetched) < jwksTTL
	if fresh {
		if key, ok := k.keys[kid]; ok {
			return key, nil
		}
	}
	if !k.tried.IsZero() && now.Sub(k.tried) < jwksMinRetry {
		return nil, errOIDC
	}
	k.tried = now
	keys, err := k.fetch(ctx)
	if err != nil {
		return nil, err
	}
	k.keys = keys
	k.fetched = now
	key, ok := keys[kid]
	if !ok {
		return nil, errOIDC
	}
	return key, nil
}

func (k *keySet) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	ctx, cancel := context.WithTimeout(ctx, exchangeLimit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&doc); err != nil {
		return nil, err
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, entry := range doc.Keys {
		if entry.Kty != "RSA" || (entry.Alg != "RS256" && entry.Alg != "") || (entry.Use != "sig" && entry.Use != "") || entry.Kid == "" {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(entry.N)
		if err != nil {
			continue
		}
		e, err := base64.RawURLEncoding.DecodeString(entry.E)
		if err != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent<<8 | int(b)
		}
		if exponent < 3 {
			continue
		}
		keys[entry.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}
	}
	return keys, nil
}

type tokenHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type tokenPayload struct {
	Issuer        string          `json:"iss"`
	Subject       string          `json:"sub"`
	Audience      json.RawMessage `json:"aud"`
	AuthorizedFor string          `json:"azp"`
	Expires       int64           `json:"exp"`
	IssuedAt      int64           `json:"iat"`
	Nonce         string          `json:"nonce"`
	Email         string          `json:"email"`
	EmailVerified json.RawMessage `json:"email_verified"`
	Name          string          `json:"name"`
}

func audiences(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

func emailVerified(raw json.RawMessage) bool {
	var flag bool
	if json.Unmarshal(raw, &flag) == nil {
		return flag
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text == "true"
	}
	return false
}

func (k *keySet) verify(ctx context.Context, ep Endpoints, clientID, rawToken, nonce string) (GoogleClaims, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return GoogleClaims{}, errOIDC
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return GoogleClaims{}, errOIDC
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return GoogleClaims{}, errOIDC
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return GoogleClaims{}, errOIDC
	}
	var header tokenHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Alg != "RS256" || header.Kid == "" {
		return GoogleClaims{}, errOIDC
	}
	key, err := k.key(ctx, header.Kid)
	if err != nil {
		return GoogleClaims{}, errOIDC
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return GoogleClaims{}, errOIDC
	}
	var payload tokenPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return GoogleClaims{}, errOIDC
	}
	issuerOK := false
	for _, issued := range ep.Issued {
		if payload.Issuer == issued {
			issuerOK = true
			break
		}
	}
	if !issuerOK {
		return GoogleClaims{}, errOIDC
	}
	aud := audiences(payload.Audience)
	audOK := false
	for _, a := range aud {
		if a == clientID {
			audOK = true
			break
		}
	}
	if !audOK || clientID == "" {
		return GoogleClaims{}, errOIDC
	}
	if len(aud) > 1 && payload.AuthorizedFor != clientID {
		return GoogleClaims{}, errOIDC
	}
	now := k.now()
	if payload.Expires == 0 || !now.Before(time.Unix(payload.Expires, 0).Add(clockSkew)) {
		return GoogleClaims{}, errOIDC
	}
	if payload.IssuedAt == 0 || time.Unix(payload.IssuedAt, 0).After(now.Add(clockSkew)) {
		return GoogleClaims{}, errOIDC
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(payload.Nonce), []byte(nonce)) != 1 {
		return GoogleClaims{}, errOIDC
	}
	if payload.Subject == "" || len(payload.Subject) > 255 || payload.Email == "" || !emailVerified(payload.EmailVerified) {
		return GoogleClaims{}, errOIDC
	}
	return GoogleClaims{Subject: payload.Subject, Email: payload.Email, Name: payload.Name}, nil
}

func pkceChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func exchange(ctx context.Context, client *http.Client, ep Endpoints, clientID, clientSecret, redirectURI, code, verifier string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, exchangeLimit)
	defer cancel()
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint status %d", resp.StatusCode)
	}
	var out struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil {
		return "", err
	}
	if out.IDToken == "" {
		return "", errOIDC
	}
	return out.IDToken, nil
}
