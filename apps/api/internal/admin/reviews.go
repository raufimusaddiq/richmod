package admin

import (
	"net/http"
	"strings"
	"time"
)

// ReviewOpsSummary, ReviewOpsBreakdown, and ReviewOpsProjections are the
// read-only Super Admin aggregates for rollout health (UIR-09). They answer the
// rollout/DoD questions from the Admin surface instead of manual SQL. Nothing
// here returns financial evidence: amounts, merchants, counterparties, email
// bodies, document content, and prompt text are never selected.

// ReviewOpsSummary is GET /api/v1/admin/reviews/summary.
func (h *Handler) ReviewOpsSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := adminRange(r.URL.Query().Get("range"))
	var out struct {
		OpenReviews                    int      `json:"openReviews"`
		EligibleTelegramReviews        int      `json:"eligibleTelegramReviews"`
		ActionableTelegramProjections  int      `json:"actionableTelegramProjections"`
		TelegramActionableCoverageRate *float64 `json:"telegramActionableCoverageRate"`
		WebEscapeRate                  *float64 `json:"webEscapeRate"`
		DeliveryAttempts               int      `json:"deliveryAttempts"`
		DeliverySucceeded              int      `json:"deliverySucceeded"`
		DeliveryFailed                 int      `json:"deliveryFailed"`
		DeliveryRetried                int      `json:"deliveryRetried"`
		DeliverySuccessRate            *float64 `json:"deliverySuccessRate"`
		StaleActionAttempts            int      `json:"staleActionAttempts"`
		ResolutionLatencyP50Ms         *float64 `json:"resolutionLatencyP50Ms"`
		ResolutionLatencyP95Ms         *float64 `json:"resolutionLatencyP95Ms"`
		ResolvedByTelegram             int      `json:"resolvedByTelegram"`
		ResolvedByWeb                  int      `json:"resolvedByWeb"`
		ResolvedBySystem               int      `json:"resolvedBySystem"`
	}
	// Open reviews: an item is open when it is not terminal AND (for a
	// transaction-backed item) the transaction still awaits review.
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM review_item ri LEFT JOIN transaction t ON t.id=ri.transaction_id WHERE ri.status IN ('OPEN','PENDING_SEND') AND (ri.transaction_id IS NULL OR t.status='NEEDS_REVIEW')`).Scan(&out.OpenReviews); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	// Eligible: open reviews in a household that has an active Telegram recipient.
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM review_item ri LEFT JOIN transaction t ON t.id=ri.transaction_id WHERE ri.status IN ('OPEN','PENDING_SEND') AND (ri.transaction_id IS NULL OR t.status='NEEDS_REVIEW') AND EXISTS (SELECT 1 FROM telegram_identity ti JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active WHERE ti.household_id=ri.household_id AND ti.active)`).Scan(&out.EligibleTelegramReviews); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	// Actionable projection: an open review_request whose delivered card carries a
	// markup (an answerable card), for a still-open item.
	if err := h.pool.QueryRow(ctx, `SELECT count(DISTINCT ri.id) FROM review_item ri JOIN review_request rr ON rr.review_item_id=ri.id JOIN review_request_recipient rc ON rc.review_request_id=rr.id LEFT JOIN transaction t ON t.id=ri.transaction_id WHERE ri.status IN ('OPEN','PENDING_SEND') AND (ri.transaction_id IS NULL OR t.status='NEEDS_REVIEW') AND rr.status IN ('PENDING_SEND','OPEN') AND rc.telegram_message_id IS NOT NULL`).Scan(&out.ActionableTelegramProjections); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	// Delivery aggregates come from the review send jobs in range.
	if err := h.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='SUCCEEDED'),count(*) FILTER(WHERE status='FAILED'),coalesce(sum(GREATEST(attempts-1,0)),0),CASE WHEN count(*)=0 THEN NULL ELSE count(*) FILTER(WHERE status='SUCCEEDED')::float/count(*) END FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json ? 'review_request_id' AND created_at>=$1`, start).Scan(&out.DeliveryAttempts, &out.DeliverySucceeded, &out.DeliveryFailed, &out.DeliveryRetried, &out.DeliverySuccessRate); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	if out.EligibleTelegramReviews == 0 {
		out.TelegramActionableCoverageRate = nil
	} else {
		rate := float64(out.ActionableTelegramProjections) / float64(out.EligibleTelegramReviews)
		out.TelegramActionableCoverageRate = &rate
	}
	// Resolutions in range, split by surface, plus latency from review creation to
	// resolution. Latency uses resolved_at-created_at, the canonical review window.
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE resolved_at IS NOT NULL AND resolved_by_user_id IS NOT NULL AND EXISTS(SELECT 1 FROM telegram_identity ti WHERE ti.user_id=review_item.resolved_by_user_id AND ti.household_id=review_item.household_id)),count(*) FILTER(WHERE resolved_at IS NOT NULL AND resolved_by_user_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM telegram_identity ti WHERE ti.user_id=review_item.resolved_by_user_id AND ti.household_id=review_item.household_id)),count(*) FILTER(WHERE resolved_at IS NOT NULL AND resolved_by_user_id IS NULL),percentile_cont(.5) within group(order by extract(epoch FROM resolved_at-created_at)*1000) FILTER(WHERE resolved_at IS NOT NULL),percentile_cont(.95) within group(order by extract(epoch FROM resolved_at-created_at)*1000) FILTER(WHERE resolved_at IS NOT NULL) FROM review_item WHERE resolved_at>=$1`, start).Scan(&out.ResolvedByTelegram, &out.ResolvedByWeb, &out.ResolvedBySystem, &out.ResolutionLatencyP50Ms, &out.ResolutionLatencyP95Ms); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='STALE_REVIEW_ACTION' AND created_at>=$1`, start).Scan(&out.StaleActionAttempts); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	// Web escape: an item resolved through the Review Inbox instead of Telegram,
	// excluding voluntary "view details". Ordinary blockers resolved on Web are
	// the numerator; all resolutions in range are the denominator.
	var webEscapes, totalResolved int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE resolved_by_user_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM telegram_identity ti WHERE ti.user_id=review_item.resolved_by_user_id AND ti.household_id=review_item.household_id)),count(*) FROM review_item WHERE resolved_at>=$1`, start).Scan(&webEscapes, &totalResolved); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	if totalResolved > 0 {
		rate := float64(webEscapes) / float64(totalResolved)
		out.WebEscapeRate = &rate
	}
	writeJSON(w, 200, out)
}

// ReviewOpsBreakdown is GET /api/v1/admin/reviews/breakdown.
func (h *Handler) ReviewOpsBreakdown(w http.ResponseWriter, r *http.Request) {
	start := adminRange(r.URL.Query().Get("range"))
	rows, err := h.pool.Query(r.Context(), `
		SELECT ri.review_type,
		       count(*) FILTER(WHERE ri.created_at>=$1),
		       count(*) FILTER(WHERE ri.status IN ('OPEN','PENDING_SEND') AND (ri.transaction_id IS NULL OR t.status='NEEDS_REVIEW')),
		       count(*) FILTER(WHERE ri.created_at>=$1 AND (ri.status IN ('OPEN','PENDING_SEND') AND (ri.transaction_id IS NULL OR t.status='NEEDS_REVIEW')) AND EXISTS (SELECT 1 FROM telegram_identity ti JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active WHERE ti.household_id=ri.household_id AND ti.active)),
		       count(DISTINCT ri.id) FILTER(WHERE ri.created_at>=$1 AND ri.status IN ('OPEN','PENDING_SEND') AND (ri.transaction_id IS NULL OR t.status='NEEDS_REVIEW') AND EXISTS (SELECT 1 FROM review_request rr JOIN review_request_recipient rc ON rc.review_request_id=rr.id WHERE rr.review_item_id=ri.id AND rr.status IN ('PENDING_SEND','OPEN') AND rc.telegram_message_id IS NOT NULL)),
		       count(*) FILTER(WHERE ri.resolved_at>=$1 AND ri.resolved_by_user_id IS NOT NULL AND EXISTS(SELECT 1 FROM telegram_identity ti WHERE ti.user_id=ri.resolved_by_user_id AND ti.household_id=ri.household_id)),
		       count(*) FILTER(WHERE ri.resolved_at>=$1 AND ri.resolved_by_user_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM telegram_identity ti WHERE ti.user_id=ri.resolved_by_user_id AND ti.household_id=ri.household_id)),
		       count(*) FILTER(WHERE ri.resolved_at>=$1 AND ri.resolved_by_user_id IS NULL)
		FROM review_item ri LEFT JOIN transaction t ON t.id=ri.transaction_id
		GROUP BY ri.review_type ORDER BY ri.review_type`, start)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var rt string
		var created, open, eligible, projected, resolvedTelegram, resolvedWeb, resolvedSystem int
		if err := rows.Scan(&rt, &created, &open, &eligible, &projected, &resolvedTelegram, &resolvedWeb, &resolvedSystem); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		var coverage *float64
		if eligible > 0 {
			rate := float64(projected) / float64(eligible)
			coverage = &rate
		}
		resolved := resolvedTelegram + resolvedWeb + resolvedSystem
		var webEscape *float64
		if resolved > 0 {
			rate := float64(resolvedWeb) / float64(resolved)
			webEscape = &rate
		}
		var deliveryFailed int
		_ = h.pool.QueryRow(r.Context(), `SELECT count(*) FROM job j JOIN review_request rr ON rr.id::text=j.payload_json->>'review_request_id' WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.status='FAILED' AND j.created_at>=$1 AND rr.review_item_id IN (SELECT id FROM review_item WHERE review_type=$2)`, start, rt).Scan(&deliveryFailed)
		out = append(out, map[string]any{
			"reviewType": rt, "created": created, "open": open, "telegramEligible": eligible,
			"actionableProjected": projected, "coverageRate": coverage,
			"resolvedTelegram": resolvedTelegram, "resolvedWeb": resolvedWeb, "resolvedSystem": resolvedSystem,
			"webEscapeRate": webEscape, "deliveryFailed": deliveryFailed,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	writeJSON(w, 200, map[string]any{"range": r.URL.Query().Get("range"), "rows": out})
}

// ReviewOpsProjections is GET /api/v1/admin/reviews/projections.
func (h *Handler) ReviewOpsProjections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	start := adminRange(q.Get("range"))
	limit := pageLimit(r, 50, 200)
	cursor, ok := parseCursor(q.Get("cursor"))
	rows, err := h.pool.Query(r.Context(), `
		SELECT rr.id,ri.review_type,ri.status,rr.status,
		       COALESCE(rc.telegram_message_id,0)>0 AS delivered,
		       (SELECT count(*) FROM job j WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.payload_json->>'review_request_id'=rr.id::text AND j.status='FAILED') AS failed,
		       (SELECT count(*) FROM job j WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.payload_json->>'review_request_id'=rr.id::text AND j.status='SUCCEEDED') AS sent,
		       rr.created_at,COALESCE(rr.resolved_at,rr.created_at),
		       COALESCE((SELECT a.actor_type FROM audit_log a WHERE a.entity_id=ri.id AND a.action='RESOLVE_REVIEW' ORDER BY a.created_at DESC LIMIT 1),'') AS resolved_surface
		FROM review_request rr JOIN review_item ri ON ri.id=rr.review_item_id
		LEFT JOIN review_request_recipient rc ON rc.review_request_id=rr.id
		WHERE rr.created_at>=$1 AND ($2='' OR rr.status=$2) AND ($3='' OR ri.review_type=$3) AND ($4='' OR rr.id::text ILIKE '%'||$4||'%') AND (NOT $5 OR (rr.created_at,rr.id)<($6,$7::uuid))
		ORDER BY rr.created_at DESC,rr.id DESC LIMIT $8`, start, strings.TrimSpace(q.Get("status")), strings.TrimSpace(q.Get("reviewType")), strings.TrimSpace(q.Get("q")), ok, nullableTime(cursor.Time), nullableString(cursor.ID), limit+1)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, reviewType, reviewStatus, projectionStatus, resolvedSurface string
		var delivered bool
		var failed, sent int
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &reviewType, &reviewStatus, &projectionStatus, &delivered, &failed, &sent, &createdAt, &updatedAt, &resolvedSurface); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		deliveryStatus := "PENDING"
		switch {
		case sent > 0:
			deliveryStatus = "SUCCEEDED"
		case failed > 0:
			deliveryStatus = "FAILED"
		case delivered:
			deliveryStatus = "SUCCEEDED"
		}
		items = append(items, map[string]any{
			"projectionId":     id,
			"reviewType":       reviewType,
			"reviewStatus":     reviewStatus,
			"projectionStatus": projectionStatus,
			"deliveryStatus":   deliveryStatus,
			"retryCount":       failed,
			"delivered":        delivered,
			"createdAt":        createdAt,
			"updatedAt":        updatedAt,
			"ageMs":            time.Since(createdAt).Milliseconds(),
			"resolvedSurface":  resolvedSurface,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		nextCursor = makeCursor(last["createdAt"].(time.Time), last["projectionId"].(string))
	}
	writeJSON(w, 200, map[string]any{"items": items, "nextCursor": nextCursor})
}
