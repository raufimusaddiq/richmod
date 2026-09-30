package insight

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/analyticscore"
)

type Handler struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

const insightPromptVersion = "cycle-analyst-v3"

const existingInsightQuery = `SELECT id FROM insight WHERE household_id=$1 AND period=$2::date AND input_metrics_json->>'period_kind'=$3 AND input_metrics_json->>'period_start'=$4 AND (status='PENDING' OR (status='SUCCEEDED' AND prompt_version=$5 AND created_at>now()-interval '1 hour')) ORDER BY created_at DESC LIMIT 1`

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, now: time.Now} }

// List preserves historical insight rows verbatim. historical explicitly marks
// the retired recommendation/aggregate contract; new commentary is natural text.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principal(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,to_char(period,'YYYY-MM'),status,input_metrics_json,gateway_route,model,prompt_version,generated_text,confidence::text,data_completeness::text,created_at,completed_at FROM insight WHERE household_id=$1 ORDER BY created_at DESC LIMIT 12`, household)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list insights"})
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, period, status, prompt, completeness string
		var metrics json.RawMessage
		var route, model, text, confidence *string
		var created time.Time
		var completed *time.Time
		if err := rows.Scan(&id, &period, &status, &metrics, &route, &model, &prompt, &text, &confidence, &completeness, &created, &completed); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list insights"})
			return
		}
		result = append(result, map[string]any{"id": id, "period": period, "status": status, "metrics": metrics, "gatewayRoute": route, "model": model, "promptVersion": prompt, "historical": prompt != insightPromptVersion, "text": text, "confidence": confidence, "dataCompleteness": completeness, "createdAt": created, "completedAt": completed})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list insights"})
		return
	}
	writeJSON(w, 200, result)
}

func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principal(w, r)
	if !ok {
		return
	}
	if period := r.URL.Query().Get("period"); period != "" && period != "cycle" {
		writeJSON(w, 400, map[string]string{"error": "commentary supports salary cycles only"})
		return
	}
	start := r.URL.Query().Get("cycle_start")
	if start != "" {
		if _, err := time.Parse("2006-01-02", start); err != nil {
			writeJSON(w, 400, map[string]string{"error": "cycle_start must be YYYY-MM-DD"})
			return
		}
	}
	facts, err := analyticscore.Load(r.Context(), h.pool, household, start, h.now())
	if errors.Is(err, analyticscore.ErrCycleNotFound) {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to build deterministic insight facts"})
		return
	}
	if facts.Period.Kind != "SALARY_CYCLE" {
		writeJSON(w, 409, map[string]string{"error": "confirmed salary cycle required"})
		return
	}
	kind := "SALARY_CYCLE"
	if facts.Period.State == "ACTIVE" {
		kind = "CURRENT_CYCLE"
	}
	period := facts.Period.Start
	var existing string
	err = h.pool.QueryRow(r.Context(), existingInsightQuery, household, period, kind, period, insightPromptVersion).Scan(&existing)
	if err == nil {
		writeJSON(w, 200, map[string]string{"id": existing, "status": "EXISTING"})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 500, map[string]string{"error": "unable to check insight rate limit"})
		return
	}

	// The snapshot is server-side audit evidence only. Models retrieve financial
	// data through shared native READ tools, never this preloaded JSON.
	raw, err := json.Marshal(map[string]any{"period_kind": kind, "period_start": period, "period_end": facts.Period.MeasuredUntil, "period_open": facts.Period.State == "ACTIVE", "facts_snapshot": facts})
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to serialize insight facts"})
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to request insight"})
		return
	}
	defer tx.Rollback(r.Context())
	completeness := facts.Completeness()
	var id string
	if err := tx.QueryRow(r.Context(), `INSERT INTO insight(household_id,period,status,input_metrics_json,prompt_version,data_completeness,requested_by_user_id) VALUES($1,$2::date,'PENDING',$3::jsonb,$4,$5,$6) RETURNING id`, household, period, string(raw), insightPromptVersion, completeness, p.UserID).Scan(&id); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeJSON(w, 409, map[string]string{"error": "an insight is already being generated"})
			return
		}
		writeJSON(w, 500, map[string]string{"error": "unable to request insight"})
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO job(type,payload_json,max_attempts) VALUES('GENERATE_INSIGHT',jsonb_build_object('insight_id',$1::uuid),3)`, id); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to enqueue insight"})
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'REQUEST_INSIGHT','insight',$3,jsonb_build_object('period',$4::date,'period_start',$4::date,'period_kind',$5::text,'data_completeness',$6::numeric,'prompt_version',$7::text))`, household, p.UserID, id, period, kind, completeness, insightPromptVersion); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to enqueue insight"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to enqueue insight"})
		return
	}
	writeJSON(w, 202, map[string]string{"id": id, "status": "PENDING"})
}

func principal(w http.ResponseWriter, r *http.Request) (auth.Principal, string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return auth.Principal{}, "", false
	}
	return p, p.HouseholdID, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
