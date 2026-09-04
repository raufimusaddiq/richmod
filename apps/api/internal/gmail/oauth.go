package gmail

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

const gmailReadonlyScope = "https://www.googleapis.com/auth/gmail.readonly"

type OAuthClient struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

type Handler struct {
	pool                 *pgxpool.Pool
	client               OAuthClient
	mailbox              string
	key                  []byte
	httpClient           *http.Client
	pubsubAudience       string
	pubsubServiceAccount string
	verifyToken          func(context.Context, string, string) (tokenClaims, error)
}

func LoadOAuthClient(path string) (OAuthClient, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return OAuthClient{}, fmt.Errorf("read Google OAuth client: %w", err)
	}
	var document struct {
		Web struct {
			ClientID     string   `json:"client_id"`
			ClientSecret string   `json:"client_secret"`
			RedirectURIs []string `json:"redirect_uris"`
		} `json:"web"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return OAuthClient{}, fmt.Errorf("decode Google OAuth client: %w", err)
	}
	if document.Web.ClientID == "" || document.Web.ClientSecret == "" || len(document.Web.RedirectURIs) != 1 {
		return OAuthClient{}, fmt.Errorf("Google OAuth file must contain one web redirect URI")
	}
	return OAuthClient{ClientID: document.Web.ClientID, ClientSecret: document.Web.ClientSecret, RedirectURI: document.Web.RedirectURIs[0]}, nil
}

func NewHandler(pool *pgxpool.Pool, client OAuthClient, mailbox, encryptionKeyHex string) (*Handler, error) {
	key, err := hex.DecodeString(encryptionKeyHex)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("GMAIL_TOKEN_ENCRYPTION_KEY must be 32-byte hex")
	}
	if strings.TrimSpace(mailbox) == "" {
		return nil, fmt.Errorf("GMAIL_MAILBOX is required")
	}
	return &Handler{pool: pool, client: client, mailbox: strings.ToLower(strings.TrimSpace(mailbox)), key: key, httpClient: &http.Client{Timeout: 20 * time.Second}}, nil
}

func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(principal.Memberships) == 0 {
		http.Error(w, "household membership required", http.StatusForbidden)
		return
	}
	var emailIngressActive bool
	if err := h.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM email_ingress_address WHERE household_id=$1 AND purpose='BANK_EMAIL' AND status='ACTIVE')`, principal.Memberships[0].HouseholdID).Scan(&emailIngressActive); err != nil {
		http.Error(w, "unable to check email provider", http.StatusInternalServerError)
		return
	}
	if emailIngressActive {
		http.Error(w, "Cloudflare email ingress is already active", http.StatusConflict)
		return
	}
	stateRaw := make([]byte, 32)
	if _, err := rand.Read(stateRaw); err != nil {
		http.Error(w, "unable to start Gmail connection", http.StatusInternalServerError)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(stateRaw)
	stateHash := sha256.Sum256([]byte(state))
	if _, err := h.pool.Exec(r.Context(), `INSERT INTO gmail_oauth_state (state_hash,household_id,user_id,expires_at) VALUES ($1,$2,$3,now()+interval '10 minutes')`, stateHash[:], principal.Memberships[0].HouseholdID, principal.UserID); err != nil {
		http.Error(w, "unable to start Gmail connection", http.StatusInternalServerError)
		return
	}
	query := url.Values{
		"client_id":     {h.client.ClientID},
		"redirect_uri":  {h.client.RedirectURI},
		"response_type": {"code"},
		"scope":         {gmailReadonlyScope},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
		"login_hint":    {h.mailbox},
	}
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+query.Encode(), http.StatusFound)
}

func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" || r.URL.Query().Get("state") == "" {
		http.Error(w, "Google authorization was not completed", http.StatusBadRequest)
		return
	}
	stateHash := sha256.Sum256([]byte(r.URL.Query().Get("state")))
	var householdID, userID string
	err := h.pool.QueryRow(r.Context(), `DELETE FROM gmail_oauth_state WHERE state_hash=$1 AND expires_at>now() RETURNING household_id,user_id`, stateHash[:]).Scan(&householdID, &userID)
	if err != nil {
		http.Error(w, "invalid or expired OAuth state", http.StatusBadRequest)
		return
	}
	token, err := h.exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "unable to exchange Google authorization", http.StatusBadGateway)
		return
	}
	if token.RefreshToken == "" {
		http.Error(w, "Google did not provide offline access", http.StatusBadRequest)
		return
	}
	mailbox, err := h.profile(r.Context(), token.AccessToken)
	if err != nil || !strings.EqualFold(mailbox, h.mailbox) {
		http.Error(w, "authorized mailbox does not match configured mailbox", http.StatusForbidden)
		return
	}
	encrypted, err := encrypt(h.key, householdID, token.RefreshToken)
	if err != nil {
		http.Error(w, "unable to protect Google credential", http.StatusInternalServerError)
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		http.Error(w, "unable to save Gmail connection", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	var activeIngressID string
	err = tx.QueryRow(r.Context(), `SELECT id FROM email_ingress_address WHERE household_id=$1 AND purpose='BANK_EMAIL' AND status='ACTIVE' FOR SHARE`, householdID).Scan(&activeIngressID)
	if err == nil {
		http.Error(w, "Cloudflare email ingress is already active", http.StatusConflict)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "unable to verify email provider", http.StatusInternalServerError)
		return
	}
	_, err = tx.Exec(r.Context(), `
		INSERT INTO gmail_integration (household_id,mailbox,encrypted_refresh_token,granted_scope,status,connected_by_user_id)
		VALUES ($1,$2,$3,$4,'CONNECTED',$5)
		ON CONFLICT (household_id) DO UPDATE SET mailbox=excluded.mailbox,encrypted_refresh_token=excluded.encrypted_refresh_token,granted_scope=excluded.granted_scope,status='CONNECTED',connected_by_user_id=excluded.connected_by_user_id,updated_at=now()`, householdID, h.mailbox, encrypted, token.Scope, userID)
	if err != nil {
		http.Error(w, "unable to save Gmail connection", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_log (household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES ($1,'USER',$2,'CONNECT_GMAIL','gmail_integration',$1,jsonb_build_object('mailbox',$3::text,'scope',$4::text))`, householdID, userID, h.mailbox, token.Scope); err != nil {
		http.Error(w, "unable to audit Gmail connection", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "unable to save Gmail connection", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/?gmail=connected", http.StatusFound)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (h *Handler) exchange(ctx context.Context, code string) (tokenResponse, error) {
	form := url.Values{"code": {code}, "client_id": {h.client.ClientID}, "client_secret": {h.client.ClientSecret}, "redirect_uri": {h.client.RedirectURI}, "grant_type": {"authorization_code"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := h.httpClient.Do(req)
	if err != nil {
		return tokenResponse{}, errors.New("Google token request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return tokenResponse{}, fmt.Errorf("Google token endpoint returned HTTP %d", response.StatusCode)
	}
	var token tokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token); err != nil {
		return tokenResponse{}, err
	}
	return token, nil
}

func (h *Handler) profile(ctx context.Context, accessToken string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://gmail.googleapis.com/gmail/v1/users/me/profile", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := h.httpClient.Do(req)
	if err != nil {
		return "", errors.New("Gmail profile request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Gmail profile returned HTTP %d", response.StatusCode)
	}
	var profile struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&profile); err != nil {
		return "", err
	}
	return profile.EmailAddress, nil
}

func encrypt(key []byte, householdID, plaintext string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plaintext), []byte(householdID)), nil
}

func decrypt(key []byte, householdID string, ciphertext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted token")
	}
	plaintext, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], []byte(householdID))
	return string(plaintext), err
}
