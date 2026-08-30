package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var heartbeat *time.Time
	var workers int
	if err := h.pool.QueryRow(ctx, `SELECT max(last_seen_at),count(*) FILTER (WHERE last_seen_at > now()-interval '60 seconds') FROM worker_heartbeat`).Scan(&heartbeat, &workers); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	lanes := []map[string]any{}
	pending := 0
	running := 0
	for _, lane := range []string{"INTERACTIVE", "DEFAULT", "BACKGROUND"} {
		var p, run int
		var oldest *float64
		var p50, p95 *float64
		err := h.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='PENDING'),count(*) FILTER(WHERE status='RUNNING'),extract(epoch FROM now()-min(run_after) FILTER(WHERE status='PENDING' AND run_after<=now()))*1000,percentile_cont(.5) within group(order by extract(epoch FROM finished_at-started_at)*1000) FILTER(WHERE finished_at>now()-interval '24 hours' AND started_at IS NOT NULL),percentile_cont(.95) within group(order by extract(epoch FROM finished_at-started_at)*1000) FILTER(WHERE finished_at>now()-interval '24 hours' AND started_at IS NOT NULL) FROM job WHERE lane=$1`, lane).Scan(&p, &run, &oldest, &p50, &p95)
		if err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		pending += p
		running += run
		lanes = append(lanes, map[string]any{"lane": lane, "pending": p, "running": run, "oldestDueAgeMs": oldest, "executionP50Ms": p50, "executionP95Ms": p95})
	}
	var failed, calls, llmFailed, input, output int
	var successRate *float64
	var llmP95 *float64
	var cost *string
	if err := h.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM job WHERE status='FAILED' AND updated_at>now()-interval '24 hours'),count(*),count(*) FILTER(WHERE status='FAILED'),coalesce(sum(input_tokens),0),coalesce(sum(output_tokens),0),CASE WHEN count(*)=0 THEN NULL ELSE count(*) FILTER(WHERE status='SUCCEEDED')::float/count(*) END,percentile_cont(.95) within group(order by duration_ms),CASE WHEN count(cost)=0 THEN NULL ELSE sum(cost)::text END FROM llm_call WHERE created_at>now()-interval '24 hours'`).Scan(&failed, &calls, &llmFailed, &input, &output, &successRate, &llmP95, &cost); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	var reviews, households, users, activeUsers, gmail, telegram int
	if err := h.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM review_item WHERE status IN ('OPEN','PENDING_SEND')),(SELECT count(*) FROM household),(SELECT count(*) FROM "user"),(SELECT count(*) FROM "user" WHERE active),(SELECT count(*) FROM gmail_integration WHERE status IN ('CONNECTED','WATCH_ACTIVE')),(SELECT count(*) FROM telegram_identity)`).Scan(&reviews, &households, &users, &activeUsers, &gmail, &telegram); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	status := "HEALTHY"
	healthy := heartbeat != nil && time.Since(*heartbeat) < workerHealthyAfter
	if !healthy {
		status = "DEGRADED"
	}
	for _, item := range lanes {
		age, _ := item["oldestDueAgeMs"].(*float64)
		threshold := defaultDueAge
		if item["lane"] == "INTERACTIVE" {
			threshold = interactiveDueAge
		}
		if item["lane"] == "BACKGROUND" {
			threshold = backgroundDueAge
		}
		if age != nil && time.Duration(*age)*time.Millisecond > threshold {
			status = "DEGRADED"
		}
	}
	if heartbeat == nil || (heartbeat != nil && time.Since(*heartbeat) > 5*workerHealthyAfter) {
		status = "UNHEALTHY"
	}
	writeJSON(w, 200, map[string]any{"status": status, "checkedAt": time.Now().UTC(), "worker": map[string]any{"healthy": healthy, "activeWorkers": workers, "lastHeartbeatAt": heartbeat}, "jobs": map[string]any{"pending": pending, "running": running, "failed24h": failed, "lanes": lanes}, "llm": map[string]any{"calls24h": calls, "failed24h": llmFailed, "successRate": successRate, "p95DurationMs": llmP95, "inputTokens": input, "outputTokens": output, "cost": cost}, "reviews": map[string]any{"open": reviews}, "households": map[string]any{"total": households, "active": households}, "users": map[string]any{"total": users, "active": activeUsers}, "integrations": map[string]any{"gmailConnected": gmail, "telegramLinked": telegram, "llmGatewayConfigured": h.gatewayConfigured, "llmProtocol": h.protocol}})
}

func (h *Handler) Jobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := pageLimit(r, 50, 100)
	cursor, ok := parseCursor(q.Get("cursor"))
	from, _ := dateBound(q.Get("from"), false)
	to, _ := dateBound(q.Get("to"), true)
	rows, err := h.pool.Query(r.Context(), `SELECT id,type,lane,status,attempts,max_attempts,created_at,updated_at,started_at,finished_at FROM job WHERE ($1='' OR status=$1) AND ($2='' OR lane=$2) AND ($3='' OR type=$3) AND ($4::timestamptz IS NULL OR updated_at>=$4) AND ($5::timestamptz IS NULL OR updated_at<$5) AND ($6='' OR id::text ILIKE '%'||$6||'%') AND (NOT $7 OR (updated_at,id)<($8,$9::uuid)) ORDER BY updated_at DESC,id DESC LIMIT $10`, q.Get("status"), q.Get("lane"), q.Get("type"), nullableTime(from), nullableTime(to), strings.TrimSpace(q.Get("q")), ok, nullableTime(cursor.Time), nullableString(cursor.ID), limit+1)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, typ, lane, status string
		var attempts, max int
		var created, updated time.Time
		var started, finished *time.Time
		if err := rows.Scan(&id, &typ, &lane, &status, &attempts, &max, &created, &updated, &started, &finished); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		item := map[string]any{"id": id, "type": typ, "lane": lane, "status": status, "attempts": attempts, "maxAttempts": max, "createdAt": created, "updatedAt": updated, "startedAt": started, "finishedAt": finished}
		items = append(items, item)
	}
	next := ""
	if len(items) > limit {
		last := items[limit-1]
		next = makeCursor(last["updatedAt"].(time.Time), last["id"].(string))
		items = items[:limit]
	}
	writeJSON(w, 200, map[string]any{"items": items, "nextCursor": next})
}

func (h *Handler) Job(w http.ResponseWriter, r *http.Request) {
	var id, typ, lane, status, lockedBy string
	var attempts, max int
	var raw json.RawMessage
	var runAfter, created, updated time.Time
	var locked, started, finished *time.Time
	err := h.pool.QueryRow(r.Context(), `SELECT id,type,lane,status,attempts,max_attempts,payload_json,run_after,locked_at,locked_by,created_at,updated_at,started_at,finished_at FROM job WHERE id=$1`, r.PathValue("id")).Scan(&id, &typ, &lane, &status, &attempts, &max, &raw, &runAfter, &locked, &lockedBy, &created, &updated, &started, &finished)
	if err != nil {
		writeError(w, 404, "JOB_NOT_FOUND")
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT attempt,lane,job_type,error_class,failed_at,retried_at,duration_ms FROM job_retry_log WHERE job_id=$1 ORDER BY attempt`, id)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	retries := []map[string]any{}
	for rows.Next() {
		var attempt, duration int
		var l, t, e string
		var failed time.Time
		var retried *time.Time
		if err := rows.Scan(&attempt, &l, &t, &e, &failed, &retried, &duration); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		retries = append(retries, map[string]any{"attempt": attempt, "lane": l, "jobType": t, "errorClass": e, "failedAt": failed, "retriedAt": retried, "durationMs": duration})
	}
	writeJSON(w, 200, map[string]any{"id": id, "type": typ, "lane": lane, "status": status, "attempts": attempts, "maxAttempts": max, "runAfter": runAfter, "lockedAt": locked, "lockedBy": lockedBy, "createdAt": created, "updatedAt": updated, "startedAt": started, "finishedAt": finished, "references": safeJobRefs(raw), "retries": retries})
}

func (h *Handler) LLMSummary(w http.ResponseWriter, r *http.Request) {
	rangeStart := adminRange(r.URL.Query().Get("range"))
	var calls, failed, input, output int
	var rate, p50, p95 *float64
	var cost *string
	err := h.pool.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE status='FAILED'),coalesce(sum(input_tokens),0),coalesce(sum(output_tokens),0),CASE WHEN count(*)=0 THEN NULL ELSE count(*) FILTER(WHERE status='SUCCEEDED')::float/count(*) END,percentile_cont(.5) within group(order by duration_ms),percentile_cont(.95) within group(order by duration_ms),CASE WHEN count(cost)=0 THEN NULL ELSE sum(cost)::text END FROM llm_call WHERE created_at >= $1`, rangeStart).Scan(&calls, &failed, &input, &output, &rate, &p50, &p95, &cost)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT task,count(*),count(*) FILTER(WHERE status='FAILED'),percentile_cont(.5) within group(order by duration_ms),percentile_cont(.95) within group(order by duration_ms),coalesce(sum(input_tokens+output_tokens),0),CASE WHEN count(cost)=0 THEN NULL ELSE sum(cost)::text END FROM llm_call WHERE created_at >=$1 GROUP BY task ORDER BY count(*) DESC`, rangeStart)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	tasks := []map[string]any{}
	for rows.Next() {
		var task string
		var c, f, tokens int
		var a, b *float64
		var cst *string
		if err := rows.Scan(&task, &c, &f, &a, &b, &tokens, &cst); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		tasks = append(tasks, map[string]any{"task": task, "calls": c, "failed": f, "p50DurationMs": a, "p95DurationMs": b, "tokens": tokens, "cost": cst})
	}
	writeJSON(w, 200, map[string]any{"calls": calls, "failed": failed, "successRate": rate, "p50DurationMs": p50, "p95DurationMs": p95, "inputTokens": input, "outputTokens": output, "cost": cost, "tasks": tasks})
}

func (h *Handler) LLMCalls(w http.ResponseWriter, r *http.Request) {
	start := adminRange(r.URL.Query().Get("range"))
	limit := pageLimit(r, 50, 100)
	rows, err := h.pool.Query(r.Context(), `SELECT id,household_id,task,protocol,model,status,error_class,duration_ms,input_tokens,output_tokens,cost,attempt,created_at FROM llm_call WHERE created_at >=$1 AND ($2='' OR task=$2) AND ($3='' OR status=$3) ORDER BY created_at DESC LIMIT $4`, start, r.URL.Query().Get("task"), r.URL.Query().Get("status"), limit)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, task, protocol, status string
		var household, model, errorClass *string
		var duration int64
		var input, output, attempt int
		var cost *string
		var created time.Time
		if err := rows.Scan(&id, &household, &task, &protocol, &model, &status, &errorClass, &duration, &input, &output, &cost, &attempt, &created); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		items = append(items, map[string]any{"id": id, "householdId": household, "task": task, "protocol": protocol, "model": model, "status": status, "errorClass": errorClass, "durationMs": duration, "inputTokens": input, "outputTokens": output, "cost": cost, "attempt": attempt, "createdAt": created})
	}
	writeJSON(w, 200, map[string]any{"items": items, "nextCursor": ""})
}

func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	limit := pageLimit(r, 50, 100)
	rows, err := h.pool.Query(r.Context(), `SELECT * FROM (SELECT 'JOB_RETRY' event_type,'WARN' severity,job_type component,error_class,job_id::text reference_id,failed_at created_at FROM job_retry_log UNION ALL SELECT 'JOB_FAILED','ERROR',type,'FAILED',id::text,updated_at FROM job WHERE status='FAILED' UNION ALL SELECT 'LLM_FAILED','ERROR',task,coalesce(error_class,'FAILED'),id::text,created_at FROM llm_call WHERE status='FAILED' UNION ALL SELECT 'SOURCE_FAILED','ERROR',source_type,'FAILED',id::text,created_at FROM source_event WHERE processing_status='FAILED') events WHERE ($1='' OR event_type=$1) AND ($2='' OR severity=$2) AND ($3='' OR component=$3) AND ($4='' OR reference_id ILIKE '%'||$4||'%') ORDER BY created_at DESC LIMIT $5`, r.URL.Query().Get("type"), r.URL.Query().Get("severity"), r.URL.Query().Get("component"), r.URL.Query().Get("q"), limit)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var typ, severity, component, errorClass, ref string
		var created time.Time
		if err := rows.Scan(&typ, &severity, &component, &errorClass, &ref, &created); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		items = append(items, map[string]any{"type": typ, "severity": severity, "component": component, "errorClass": errorClass, "referenceId": ref, "createdAt": created})
	}
	writeJSON(w, 200, map[string]any{"items": items, "nextCursor": ""})
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
func adminRange(value string) time.Time {
	switch value {
	case "7d":
		return time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		return time.Now().Add(-30 * 24 * time.Hour)
	default:
		return time.Now().Add(-24 * time.Hour)
	}
}

func (h *Handler) HouseholdOverview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("householdId")
	var name, timezone string
	var created time.Time
	var members, transactions, reviews, gmail, listeners, telegram int
	var last *time.Time
	var primary bool
	err := h.pool.QueryRow(r.Context(), `SELECT h.name,h.timezone,h.created_at,(SELECT count(*) FROM household_member WHERE household_id=h.id AND active),(SELECT count(*) FROM transaction WHERE household_id=h.id),(SELECT count(*) FROM review_item WHERE household_id=h.id AND status IN ('OPEN','PENDING_SEND')),(SELECT max(created_at) FROM source_event WHERE household_id=h.id),(SELECT count(*) FROM gmail_integration WHERE household_id=h.id AND status IN ('CONNECTED','WATCH_ACTIVE')),(SELECT count(*) FROM bank_email_listener WHERE household_id=h.id AND active),(SELECT count(*) FROM telegram_identity ti JOIN household_member hm ON hm.user_id=ti.user_id WHERE hm.household_id=h.id AND hm.active),(SELECT exists(SELECT 1 FROM salary_source WHERE household_id=h.id AND active AND is_primary)) FROM household h WHERE h.id=$1`, id).Scan(&name, &timezone, &created, &members, &transactions, &reviews, &last, &gmail, &listeners, &telegram, &primary)
	if err != nil {
		writeError(w, 404, "HOUSEHOLD_NOT_FOUND")
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,type,lane,status,updated_at FROM job WHERE payload_json->>'household_id'=$1 ORDER BY updated_at DESC LIMIT 10`, id)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	jobs := []map[string]any{}
	for rows.Next() {
		var jid, typ, lane, status string
		var at time.Time
		if err := rows.Scan(&jid, &typ, &lane, &status, &at); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		jobs = append(jobs, map[string]any{"id": jid, "type": typ, "lane": lane, "status": status, "updatedAt": at})
	}
	writeJSON(w, 200, map[string]any{"id": id, "name": name, "timezone": timezone, "createdAt": created, "members": members, "transactions": transactions, "openReviews": reviews, "lastSourceActivityAt": last, "integrations": map[string]any{"gmailConnected": gmail, "activeBankListeners": listeners, "telegramLinked": telegram, "primarySalaryConfigured": primary}, "recentJobs": jobs})
}

func (h *Handler) PlatformAudit(w http.ResponseWriter, r *http.Request) {
	limit := pageLimit(r, 50, 100)
	rows, err := h.pool.Query(r.Context(), `SELECT p.id,p.action,p.entity_type,p.entity_id,p.metadata_json,p.created_at,u.email FROM platform_audit_log p JOIN "user" u ON u.id=p.actor_user_id WHERE ($1='' OR p.action=$1) ORDER BY p.created_at DESC,p.id DESC LIMIT $2`, r.URL.Query().Get("action"), limit)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var action, entityType, entityID, email string
		var metadata json.RawMessage
		var at time.Time
		if err := rows.Scan(&id, &action, &entityType, &entityID, &metadata, &at, &email); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		var safe map[string]any
		_ = json.Unmarshal(metadata, &safe)
		items = append(items, map[string]any{"id": id, "action": action, "entityType": entityType, "entityId": entityID, "metadata": safe, "actorEmail": email, "createdAt": at})
	}
	writeJSON(w, 200, map[string]any{"items": items, "nextCursor": ""})
}

func (h *Handler) HouseholdAudit(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("householdId")
	if id == "" {
		writeError(w, 400, "HOUSEHOLD_ID_REQUIRED")
		return
	}
	limit := pageLimit(r, 50, 100)
	rows, err := h.pool.Query(r.Context(), `SELECT id,action,entity_type,entity_id,created_at FROM audit_log WHERE household_id=$1 ORDER BY created_at DESC LIMIT $2`, id, limit)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var aid, action, entityType, entityID string
		var at time.Time
		if err := rows.Scan(&aid, &action, &entityType, &entityID, &at); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		items = append(items, map[string]any{"id": aid, "action": action, "entityType": entityType, "entityId": entityID, "createdAt": at})
	}
	writeJSON(w, 200, map[string]any{"items": items, "nextCursor": ""})
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
