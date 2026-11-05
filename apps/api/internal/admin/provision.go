package admin

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/provision"
)

// CreateHousehold provisions a new tenant from the Super Admin console
// (ADR-059). The owner is a new user without a usable password; the response
// carries a single-use, 24-hour invite link the Super Admin hands to the owner,
// so no password ever passes through this API. Creating a household grants the
// Super Admin no access to it (ADR-021).
func (h *Handler) CreateHousehold(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	var body struct {
		HouseholdName  string `json:"householdName"`
		OwnerName      string `json:"ownerName"`
		OwnerEmail     string `json:"ownerEmail"`
		TelegramUserID *int64 `json:"telegramUserId"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_HOUSEHOLD_REQUEST")
		return
	}
	in := provision.HouseholdInput{HouseholdName: body.HouseholdName, OwnerName: body.OwnerName, OwnerEmail: body.OwnerEmail, ActorType: "USER", ActorUserID: principal.UserID, Action: "CREATE_HOUSEHOLD"}
	if body.TelegramUserID != nil {
		if *body.TelegramUserID <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_HOUSEHOLD_REQUEST")
			return
		}
		in.TelegramUserID = *body.TelegramUserID
	}
	if err := in.Normalize(); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_HOUSEHOLD_REQUEST")
		return
	}
	unusable, err := unusablePasswordHash()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HOUSEHOLD_CREATE_FAILED")
		return
	}
	in.PasswordHash = unusable
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HOUSEHOLD_CREATE_FAILED")
		return
	}
	defer tx.Rollback(r.Context())
	created, err := provision.CreateHousehold(r.Context(), tx, in)
	switch {
	case errors.Is(err, provision.ErrUserExists):
		writeError(w, http.StatusConflict, "USER_ALREADY_EXISTS")
		return
	case errors.Is(err, provision.ErrTelegramIDLinked):
		writeError(w, http.StatusConflict, "TELEGRAM_ID_ALREADY_LINKED")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "HOUSEHOLD_CREATE_FAILED")
		return
	}
	invite, err := provision.IssueOwnerInvite(r.Context(), tx, created.ID, principal.UserID, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HOUSEHOLD_CREATE_FAILED")
		return
	}
	if err := h.audit(r.Context(), tx, principal.UserID, "ADMIN_CREATE_HOUSEHOLD", "HOUSEHOLD", created.ID, map[string]any{"householdName": in.HouseholdName, "ownerUserId": created.OwnerUserID, "ownerEmail": in.OwnerEmail, "telegramLinked": in.TelegramUserID > 0}, r.Header.Get("X-Request-ID")); err != nil {
		writeError(w, http.StatusInternalServerError, "ADMIN_AUDIT_FAILED")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "HOUSEHOLD_CREATE_FAILED")
		return
	}
	// The body carries a bearer invite token; it must never be cached.
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusCreated, map[string]any{
		"household": map[string]any{"id": created.ID, "name": in.HouseholdName},
		"owner":     map[string]any{"id": created.OwnerUserID, "email": in.OwnerEmail, "displayName": in.OwnerName, "telegramLinked": in.TelegramUserID > 0},
		"invite":    map[string]any{"link": "/invite#" + invite.Token, "expiresAt": invite.ExpiresAt},
	})
}

// ReissueOwnerInvite replaces a household owner's invite while the owner has
// not enrolled yet. Owners can invite members but nobody else can invite an
// owner, so without this an expired owner invite would strand the household.
// The link is a bearer credential: until the owner enrolls, whoever holds it
// can become that owner, which is why every reissue is platform-audited.
func (h *Handler) ReissueOwnerInvite(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	householdID := r.PathValue("householdId")
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "OWNER_INVITE_FAILED")
		return
	}
	defer tx.Rollback(r.Context())
	invite, err := provision.IssueOwnerInvite(r.Context(), tx, householdID, principal.UserID, time.Now())
	switch {
	case errors.Is(err, provision.ErrOwnerAlreadyActive):
		writeError(w, http.StatusConflict, "OWNER_ALREADY_ENROLLED")
		return
	case errors.Is(err, provision.ErrOwnerNotFound):
		writeError(w, http.StatusNotFound, "HOUSEHOLD_OWNER_NOT_FOUND")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "OWNER_INVITE_FAILED")
		return
	}
	if err := h.audit(r.Context(), tx, principal.UserID, "ADMIN_REISSUE_OWNER_INVITE", "HOUSEHOLD", householdID, map[string]any{"ownerUserId": invite.OwnerUserID, "ownerEmail": invite.OwnerEmail}, r.Header.Get("X-Request-ID")); err != nil {
		writeError(w, http.StatusInternalServerError, "ADMIN_AUDIT_FAILED")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "OWNER_INVITE_FAILED")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"link": "/invite#" + invite.Token, "expiresAt": invite.ExpiresAt})
}

// unusablePasswordHash hashes a random secret nobody knows. Login also refuses
// any user whose password is not initialized, so this is defense in depth.
func unusablePasswordHash() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return auth.HashPassword(base64.RawURLEncoding.EncodeToString(raw))
}
