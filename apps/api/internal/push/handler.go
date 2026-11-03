package push

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/pushsupport"
)

// Handler owns the caller-scoped subscription API. The public key is injected
// once at startup; an empty key means push is disabled, not an error.
type Handler struct {
	pool      *pgxpool.Pool
	publicKey string
}

// NewHandler derives the advertised key through the same Parse predicate the
// worker's sender uses. A private key without a valid subject is disabled, so
// the client is never told push is on when delivery cannot start.
func NewHandler(pool *pgxpool.Pool, privateKey, subject string) *Handler {
	handler := &Handler{pool: pool}
	if config, err := pushsupport.Parse(privateKey, subject); err == nil {
		handler.publicKey = config.PublicKey
	}
	return handler
}

// PublicKey is unauthenticated by design: browsers need it before subscribing
// and it is not a secret.
func (h *Handler) PublicKey(w http.ResponseWriter, _ *http.Request) {
	if h.publicKey == "" {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "publicKey": h.publicKey})
}

type subscriptionView struct {
	Endpoint string `json:"endpoint"`
	Active   bool   `json:"active"`
}

// List returns the caller's own subscriptions in the caller's household,
// including retired rows. The client needs both: an endpoint the server does
// not know means another account registered it (or it was pruned), and a
// retired row must not render as active. It is scoped by the same
// (user, household) predicate the sender uses.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !principal.HasHousehold || principal.UserID == "" || principal.HouseholdID == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "household membership required"})
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT endpoint,disabled_at IS NULL
		FROM push_subscriptions
		WHERE user_id=$1 AND household_id=$2
		ORDER BY created_at DESC
		LIMIT 100`, principal.UserID, principal.HouseholdID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to list subscriptions"})
		return
	}
	defer rows.Close()
	items := make([]subscriptionView, 0)
	for rows.Next() {
		var item subscriptionView
		if err := rows.Scan(&item.Endpoint, &item.Active); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to list subscriptions"})
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to list subscriptions"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": items})
}

type subscriptionRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !principal.HasHousehold || principal.UserID == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "household membership required"})
		return
	}
	var request subscriptionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid subscription"})
		return
	}
	endpoint := strings.TrimSpace(request.Endpoint)
	if validateEndpoint(r.Context(), endpoint) != nil || !validKey(request.Keys.P256DH, 65) || !validKey(request.Keys.Auth, 16) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid subscription"})
		return
	}
	id, err := pushsupport.UpsertSubscription(r.Context(), h.pool, principal.UserID, principal.HouseholdID, endpoint,
		strings.TrimSpace(request.Keys.P256DH), strings.TrimSpace(request.Keys.Auth), truncate(r.UserAgent(), 500))
	if errors.Is(err, pgx.ErrNoRows) {
		// No row means either the caller is not an active member or the endpoint
		// already belongs to another user. Never reveal which.
		var member bool
		if checkErr := h.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM household_member WHERE user_id=$1 AND household_id=$2 AND active)`, principal.UserID, principal.HouseholdID).Scan(&member); checkErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to save subscription"})
			return
		}
		if member {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "subscription already registered"})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "household membership required"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to save subscription"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

type endpointRequest struct {
	Endpoint string `json:"endpoint"`
}

// Unsubscribe disables only the caller's own row. The row is retained as
// evidence of the device that used to receive notifications.
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "authentication required"})
		return
	}
	var request endpointRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request); err != nil || strings.TrimSpace(request.Endpoint) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid subscription"})
		return
	}
	if _, err := h.pool.Exec(r.Context(), `UPDATE push_subscriptions SET disabled_at=now() WHERE endpoint=$1 AND user_id=$2`, strings.TrimSpace(request.Endpoint), principal.UserID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to remove subscription"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Heartbeat records that the device still holds this subscription. It is
// caller-scoped and optional for clients.
func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "authentication required"})
		return
	}
	var request endpointRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request); err != nil || strings.TrimSpace(request.Endpoint) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid subscription"})
		return
	}
	if _, err := h.pool.Exec(r.Context(), `UPDATE push_subscriptions SET last_seen_at=now() WHERE endpoint=$1 AND user_id=$2 AND disabled_at IS NULL`, strings.TrimSpace(request.Endpoint), principal.UserID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to update subscription"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validKey(value string, length int) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(strings.TrimSpace(value))
	}
	return err == nil && len(decoded) == length
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
