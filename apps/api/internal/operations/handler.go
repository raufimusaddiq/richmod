package operations

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(principal.Memberships) == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	householdID := ""
	for _, membership := range principal.Memberships {
		if membership.Role == "OWNER" {
			householdID = membership.HouseholdID
			break
		}
	}
	if householdID == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "owner role required"})
		return
	}
	var pendingJobs, runningJobs, failedJobs, openReviews int
	var lastHeartbeat *time.Time
	var gmailStatus *string
	var gmailUpdated *time.Time
	err := h.pool.QueryRow(r.Context(), `
		SELECT
		  (SELECT count(*) FROM job WHERE status='PENDING'),
		  (SELECT count(*) FROM job WHERE status='RUNNING'),
		  (SELECT count(*) FROM job WHERE status='FAILED'),
		  (SELECT count(*) FROM review_request WHERE household_id=$1 AND status IN ('PENDING_SEND','OPEN')),
		  (SELECT max(last_seen_at) FROM worker_heartbeat),
		  (SELECT status FROM gmail_integration WHERE household_id=$1),
		  (SELECT updated_at FROM gmail_integration WHERE household_id=$1)`, householdID).
		Scan(&pendingJobs, &runningJobs, &failedJobs, &openReviews, &lastHeartbeat, &gmailStatus, &gmailUpdated)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to load operational status"})
		return
	}
	workerHealthy := lastHeartbeat != nil && time.Since(*lastHeartbeat) < time.Minute
	status := "healthy"
	if !workerHealthy || failedJobs > 0 {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        status,
		"worker":        map[string]any{"healthy": workerHealthy, "lastHeartbeatAt": lastHeartbeat},
		"jobs":          map[string]int{"pending": pendingJobs, "running": runningJobs, "failed": failedJobs},
		"reviewBacklog": openReviews,
		"gmail":         map[string]any{"status": gmailStatus, "updatedAt": gmailUpdated},
		"checkedAt":     time.Now(),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
