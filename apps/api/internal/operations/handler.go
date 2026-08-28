package operations

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

type Handler struct {
	pool              *pgxpool.Pool
	gatewayConfigured bool
}

func NewHandler(pool *pgxpool.Pool, gatewayConfigured ...bool) *Handler {
	handler := &Handler{pool: pool}
	if len(gatewayConfigured) > 0 {
		handler.gatewayConfigured = gatewayConfigured[0]
	}
	return handler
}

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
		  (SELECT count(*) FROM job WHERE status='FAILED' AND updated_at>=now()-interval '24 hours'),
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
	lanes := map[string]any{}
	rows, laneErr := h.pool.Query(r.Context(), `
		SELECT lane,
		 count(*) FILTER (WHERE status='PENDING'),
		 count(*) FILTER (WHERE status='RUNNING'),
		 min(run_after) FILTER (WHERE status='PENDING' AND run_after<=now()),
		 coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM (finished_at-started_at))*1000) FILTER (WHERE finished_at>=now()-interval '24 hours' AND started_at IS NOT NULL),0),
		 coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY extract(epoch FROM (finished_at-started_at))*1000) FILTER (WHERE finished_at>=now()-interval '24 hours' AND started_at IS NOT NULL),0)
		FROM job GROUP BY lane ORDER BY lane`)
	if laneErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to load job lanes"})
		return
	}
	defer rows.Close()
	for rows.Next() {
		var lane string
		var pending, running int
		var oldest *time.Time
		var p50, p95 float64
		if rows.Scan(&lane, &pending, &running, &oldest, &p50, &p95) != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to load job lanes"})
			return
		}
		var oldestAge *int64
		if oldest != nil {
			age := max(int64(0), time.Since(*oldest).Milliseconds())
			oldestAge = &age
		}
		lanes[lane] = map[string]any{"pending": pending, "running": running, "oldestDueAgeMs": oldestAge, "executionP50Ms": p50, "executionP95Ms": p95}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        status,
		"worker":        map[string]any{"healthy": workerHealthy, "lastHeartbeatAt": lastHeartbeat},
		"jobs":          map[string]any{"pending": pendingJobs, "running": runningJobs, "recentFailures": failedJobs, "lanes": lanes},
		"reviewBacklog": openReviews,
		"gmail":         map[string]any{"status": gmailStatus, "updatedAt": gmailUpdated},
		"llmGateway":    map[string]any{"configured": h.gatewayConfigured, "mode": "cloud-gateway-only"},
		"checkedAt":     time.Now(),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
