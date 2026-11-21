package uitelemetry

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

const RateLimitPerMinute = 120

const maxBody = 512

var screens = map[string]bool{"home": true, "transactions": true, "analytics": true, "wealth": true, "inbox": true, "documents": true, "household": true, "settings": true, "admin": true}
var actions = map[string]bool{"SCREEN_VIEW": true}

type Handler struct {
	pool   *pgxpool.Pool
	record func(ctx context.Context, household, screen, action string) error
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	h := &Handler{pool: pool}
	h.record = h.insert
	return h
}

type request struct {
	Screen string `json:"screen"`
	Action string `json:"action"`
}

func Valid(screen, action string) bool { return screens[screen] && actions[action] }

func (h *Handler) insert(ctx context.Context, household, screen, action string) error {
	_, err := h.pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,event_type,screen,action) VALUES($1,'UI_EVENT',$2,$3)`, household, screen, action)
	return err
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		writeError(w, http.StatusForbidden, "household membership required")
		return
	}
	var in request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil || decoder.More() || !Valid(in.Screen, in.Action) {
		writeError(w, http.StatusBadRequest, "invalid telemetry event")
		return
	}
	if err := h.record(r.Context(), p.HouseholdID, in.Screen, in.Action); err != nil {
		writeError(w, http.StatusInternalServerError, "unable to record telemetry event")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
