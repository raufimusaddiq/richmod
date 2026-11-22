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
	invite, err := provision.IssueOwnerInvite(r.Context(), tx, created.ID, principal.UserID, time.Now(), true)
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
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusCreated, map[string]any{
		"household": map[string]any{"id": created.ID, "name": in.HouseholdName},
		"owner":     map[string]any{"id": created.OwnerUserID, "email": in.OwnerEmail, "displayName": in.OwnerName, "telegramLinked": in.TelegramUserID > 0},
		"invite":    map[string]any{"link": "/invite#" + invite.Token, "expiresAt": invite.ExpiresAt},
	})
}

func (h *Handler) ReissueOwnerInvite(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	householdID := r.PathValue("householdId")
	var in struct {
		Reset bool `json:"reset"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
			return
		}
	}
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "OWNER_INVITE_FAILED")
		return
	}
	defer tx.Rollback(r.Context())
	invite, err := provision.IssueOwnerInvite(r.Context(), tx, householdID, principal.UserID, time.Now(), in.Reset)
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
	action := "ADMIN_REISSUE_OWNER_INVITE"
	if in.Reset {
		action = "ADMIN_RESET_OWNER_PASSWORD"
	}
	if err := h.audit(r.Context(), tx, principal.UserID, action, "HOUSEHOLD", householdID, map[string]any{"ownerUserId": invite.OwnerUserID, "ownerEmail": invite.OwnerEmail}, r.Header.Get("X-Request-ID")); err != nil {
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

func unusablePasswordHash() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return auth.HashPassword(base64.RawURLEncoding.EncodeToString(raw))
}
