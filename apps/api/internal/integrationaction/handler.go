package integrationaction

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

type Handler struct{ pool *pgxpool.Pool }

type Action struct {
	ID              string     `json:"id"`
	IntegrationType string     `json:"integrationType"`
	ActionType      string     `json:"actionType"`
	Status          string     `json:"status"`
	Title           string     `json:"title"`
	Description     *string    `json:"description"`
	ActionURL       *string    `json:"actionUrl"`
	ActionCode      *string    `json:"actionCode"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	ExpiresAt       *time.Time `json:"expiresAt"`
}

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, householdID, ok := principal(r)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "household membership required"})
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,integration_type,action_type,status,title,description,action_url,action_code,created_at,updated_at,expires_at FROM integration_action WHERE household_id=$1 AND status='OPEN' ORDER BY created_at DESC`, householdID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load integration actions"})
		return
	}
	defer rows.Close()
	items := make([]Action, 0)
	for rows.Next() {
		var item Action
		if err := rows.Scan(&item.ID, &item.IntegrationType, &item.ActionType, &item.Status, &item.Title, &item.Description, &item.ActionURL, &item.ActionCode, &item.CreatedAt, &item.UpdatedAt, &item.ExpiresAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to load integration actions"})
			return
		}
		if p.Memberships[0].Role != "OWNER" {
			item.ActionURL = nil
			item.ActionCode = nil
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load integration actions"})
		return
	}
	writeJSON(w, 200, items)
}

func (h *Handler) Resolve(w http.ResponseWriter, r *http.Request) {
	p, householdID, ok := principal(r)
	if !ok {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	if p.Memberships[0].Role != "OWNER" {
		writeJSON(w, 403, map[string]string{"error": "owner role required"})
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to resolve integration action"})
		return
	}
	defer tx.Rollback(r.Context())
	var title string
	err = tx.QueryRow(r.Context(), `UPDATE integration_action SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$3,updated_at=now() WHERE id=$1 AND household_id=$2 AND status='OPEN' RETURNING title`, r.PathValue("id"), householdID, p.UserID).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "integration action not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to resolve integration action"})
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'RESOLVE_INTEGRATION_ACTION','integration_action',$3,jsonb_build_object('status','RESOLVED','title',$4::text))`, householdID, p.UserID, r.PathValue("id"), title); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to resolve integration action"})
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to resolve integration action"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func principal(r *http.Request) (auth.Principal, string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		return auth.Principal{}, "", false
	}
	return p, p.Memberships[0].HouseholdID, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
