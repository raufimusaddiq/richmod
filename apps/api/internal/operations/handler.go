package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// probeTimeout bounds every dependency probe. The status endpoint fans out to
// ~25 independent queries; a slow or unreachable one degrades its section
// instead of holding the whole response past the budget.
const probeTimeout = 100 * time.Millisecond

// statusBudget is the wall-clock ceiling the handler must stay under even when
// every dependency is slow. It is the acceptance budget for the endpoint.
const statusBudget = 150 * time.Millisecond

type rowScanner interface{ Scan(dest ...any) error }

type rowsScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// queryer is the slice of pgx the status handler needs, so a test can inject a
// slow dependency without a database.
type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) rowScanner
	Query(ctx context.Context, sql string, args ...any) (rowsScanner, error)
}

type poolQueryer struct{ pool *pgxpool.Pool }

func (p poolQueryer) QueryRow(ctx context.Context, sql string, args ...any) rowScanner {
	return p.pool.QueryRow(ctx, sql, args...)
}

func (p poolQueryer) Query(ctx context.Context, sql string, args ...any) (rowsScanner, error) {
	return p.pool.Query(ctx, sql, args...)
}

type Handler struct {
	pool              queryer
	gatewayConfigured bool
	protocol          string
}

func NewHandler(pool *pgxpool.Pool, gatewayOptions ...any) *Handler {
	handler := &Handler{pool: poolQueryer{pool: pool}}
	if len(gatewayOptions) > 0 {
		handler.gatewayConfigured, _ = gatewayOptions[0].(bool)
	}
	if len(gatewayOptions) > 1 {
		handler.protocol, _ = gatewayOptions[1].(string)
	}
	if handler.protocol == "" {
		handler.protocol = "responses"
	}
	return handler
}

// probe runs fn under a bounded timeout so one slow dependency cannot hold the
// response past its budget.
func probe(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return fn(ctx)
}

// runProbes runs independent probes concurrently and returns their joined
// errors. Each probe is bounded by probeTimeout.
func runProbes(ctx context.Context, probes ...func(context.Context) error) error {
	errs := make([]error, len(probes))
	var wg sync.WaitGroup
	for i, fn := range probes {
		wg.Add(1)
		go func(i int, fn func(context.Context) error) {
			defer wg.Done()
			errs[i] = probe(ctx, fn)
		}(i, fn)
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !principal.HasHousehold {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	if principal.HouseholdRole != "OWNER" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "owner role required"})
		return
	}
	// Queue figures cover this household's own jobs only; the platform-wide
	// queue is a Super Admin concern (GET /api/v1/admin/overview). The worker
	// heartbeat is shared infrastructure and carries no household data.
	householdID := principal.HouseholdID
	var pendingJobs, runningJobs, failedJobs, openReviews int
	var lastHeartbeat *time.Time
	lanes := map[string]any{}
	var judgment judgmentAggregate
	var product productAggregate

	// The four sections are independent, so they run concurrently. A slow or
	// failing section degrades to its zero value and marks the status degraded
	// rather than blocking the response.
	probeErr := runProbes(r.Context(),
		func(ctx context.Context) error {
			return h.pool.QueryRow(ctx, `
				SELECT
				  (SELECT count(*) FROM job WHERE household_id=$1 AND status='PENDING'),
				  (SELECT count(*) FROM job WHERE household_id=$1 AND status='RUNNING'),
				  (SELECT count(*) FROM job WHERE household_id=$1 AND status='FAILED' AND updated_at>=now()-interval '24 hours'),
				  (SELECT count(*) FROM review_request WHERE household_id=$1 AND status IN ('PENDING_SEND','OPEN')),
				  (SELECT max(last_seen_at) FROM worker_heartbeat)`, householdID).
				Scan(&pendingJobs, &runningJobs, &failedJobs, &openReviews, &lastHeartbeat)
		},
		func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, `
				SELECT lane,
				 count(*) FILTER (WHERE status='PENDING'),
				 count(*) FILTER (WHERE status='RUNNING'),
				 min(run_after) FILTER (WHERE status='PENDING' AND run_after<=now()),
				 coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM (finished_at-started_at))*1000) FILTER (WHERE finished_at>=now()-interval '24 hours' AND started_at IS NOT NULL),0),
				 coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY extract(epoch FROM (finished_at-started_at))*1000) FILTER (WHERE finished_at>=now()-interval '24 hours' AND started_at IS NOT NULL),0)
				FROM job WHERE household_id=$1 GROUP BY lane ORDER BY lane`, householdID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var lane string
				var pending, running int
				var oldest *time.Time
				var p50, p95 float64
				if err := rows.Scan(&lane, &pending, &running, &oldest, &p50, &p95); err != nil {
					return err
				}
				var oldestAge *int64
				if oldest != nil {
					age := max(int64(0), time.Since(*oldest).Milliseconds())
					oldestAge = &age
				}
				lanes[lane] = map[string]any{"pending": pending, "running": running, "oldestDueAgeMs": oldestAge, "executionP50Ms": p50, "executionP95Ms": p95}
			}
			return rows.Err()
		},
		func(ctx context.Context) error {
			var err error
			judgment, err = h.loadJudgmentAggregate(ctx, householdID)
			return err
		},
		func(ctx context.Context) error {
			var err error
			product, err = h.loadProductAggregate(ctx, householdID)
			return err
		},
	)

	workerHealthy := lastHeartbeat != nil && time.Since(*lastHeartbeat) < time.Minute
	status := "healthy"
	if !workerHealthy || failedJobs > 0 || probeErr != nil {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        status,
		"worker":        map[string]any{"healthy": workerHealthy, "lastHeartbeatAt": lastHeartbeat},
		"jobs":          map[string]any{"pending": pendingJobs, "running": runningJobs, "recentFailures": failedJobs, "lanes": lanes},
		"reviewBacklog": openReviews,
		"llmGateway":    map[string]any{"configured": h.gatewayConfigured, "mode": "cloud-gateway-only", "protocol": h.protocol},
		"judgment":      judgment,
		"product":       product,
		"checkedAt":     time.Now(),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
