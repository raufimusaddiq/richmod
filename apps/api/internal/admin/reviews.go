package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// ReviewOpsSummary, ReviewOpsBreakdown, and ReviewOpsProjections are the
// read-only Super Admin aggregates for rollout health. They answer the
// rollout/DoD questions from the Admin surface instead of manual SQL. Nothing
// here returns financial evidence: amounts, merchants, counterparties, email
// bodies, document content, and prompt text are never selected.

// resolutionSurfaceSQL attributes a resolved review to TELEGRAM, USER (Web), or
// SYSTEM. Every resolver writes its audit row in the same database transaction
// as the review_item update, so an audit row in the household whose created_at
// equals resolved_at (both are the transaction's now()) carries the resolving
// surface whatever action name that resolver uses. The legacy entity match on
// the dedicated resolution actions covers rows audited outside that
// transaction; resolution_action covers resolvers that wrote no audit row.
// Unresolved items have no surface.
const resolutionSurfaceSQL = `LEFT JOIN LATERAL (SELECT CASE WHEN ri.resolved_at IS NULL THEN NULL ELSE COALESCE(
	(SELECT CASE a.actor_type WHEN 'TELEGRAM' THEN 'TELEGRAM' WHEN 'USER' THEN 'USER' ELSE 'SYSTEM' END
	 FROM audit_log a
	 WHERE a.household_id=ri.household_id AND a.actor_type IN ('TELEGRAM','USER','SYSTEM','WORKER')
	   AND (a.created_at=ri.resolved_at OR (a.action IN ('RESOLVE_REVIEW','CLASSIFY_TRANSFER','RECONCILE_TELEGRAM_TRANSFER') AND a.entity_id IN (ri.id,ri.transaction_id,ri.source_event_id)))
	 ORDER BY a.created_at=ri.resolved_at DESC,a.actor_type IN ('TELEGRAM','USER') DESC,a.created_at DESC LIMIT 1),
	CASE WHEN ri.resolution_action LIKE 'TELEGRAM\_%' THEN 'TELEGRAM'
	     WHEN ri.resolution_action IN ('LEGACY_TRANSACTION_RESOLVED','RECONCILED_TERMINAL_TRANSACTION','EMAIL_RECEIVED_AT_FALLBACK') THEN 'SYSTEM' END) END AS surface) s ON true`

// telegramCapabilitySQL reports whether a stored ReviewDecision's ordinary
// allowed_actions ($2) are all completable from Telegram. It mirrors the
// action-level gate in the worker, so a delivered card whose only button lives
// on Web is not counted as actionable coverage.
const telegramCapabilitySQL = `jsonb_path_exists(ri.decision,'$.allowedActions[*]') AND NOT EXISTS (
	SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(ri.decision->'allowedActions')='array' THEN ri.decision->'allowedActions' ELSE '[]'::jsonb END) a(action)
	WHERE a.action <> 'IGNORE' AND NOT (a.action = ANY($2::text[]))
)`

const telegramRecipientSQL = `EXISTS (SELECT 1 FROM telegram_identity ti JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active WHERE ti.household_id=ri.household_id AND ti.active)`

const deliveredCardSQL = `EXISTS (SELECT 1 FROM review_request rr JOIN review_request_recipient rc ON rc.review_request_id=rr.id WHERE rr.review_item_id=ri.id AND rc.telegram_message_id IS NOT NULL)`

// coverageEligibleSQL is the TARC denominator: reviews created in range ($1)
// in a household with an active Telegram recipient. Reviews without a stored
// ReviewDecision predate the decision contract, so their Telegram capability
// cannot be measured; they are reported separately as legacy. Reviews the
// system settled without a person never needed a Telegram card.
const coverageEligibleSQL = `ri.created_at>=$1 AND ri.decision IS NOT NULL AND s.surface IS DISTINCT FROM 'SYSTEM' AND ` + telegramRecipientSQL

// coverageActionableSQL is the TARC numerator: an eligible review whose
// Telegram card was delivered and is fully completable in Telegram.
const coverageActionableSQL = coverageEligibleSQL + ` AND ` + deliveredCardSQL + ` AND ` + telegramCapabilitySQL

// coverageSQL returns eligible, actionable, and legacy (unmeasurable) counts
// for reviews created in range. It is range-based rather than a snapshot of
// open reviews, so it stays measurable once the inbox is empty.
const coverageSQL = `SELECT count(*) FILTER(WHERE ` + coverageEligibleSQL + `),
	count(*) FILTER(WHERE ` + coverageActionableSQL + `),
	count(*) FILTER(WHERE ri.decision IS NULL)
	FROM review_item ri ` + resolutionSurfaceSQL + ` WHERE ri.created_at>=$1`

// webEscapeBaseSQL is the Web Escape Rate denominator: decision-backed reviews
// resolved in range by a person after a Telegram projection existed.
const webEscapeBaseSQL = `ri.resolved_at>=$1 AND ri.decision IS NOT NULL AND s.surface IN ('USER','TELEGRAM') AND EXISTS(SELECT 1 FROM review_request rr WHERE rr.review_item_id=ri.id)`

// webEscapeSQL counts Web resolutions whose ordinary actions could not all be
// completed in Telegram. A fully Telegram-capable review that was still
// resolved on Web is a voluntary switch, not a mandatory escape.
const webEscapeSQL = webEscapeBaseSQL + ` AND s.surface='USER' AND NOT (` + telegramCapabilitySQL + `)`

// webEscapeCountsSQL returns (denominator, escapes), optionally for one review
// type ($3; empty means all types).
const webEscapeCountsSQL = `SELECT count(*) FILTER(WHERE ` + webEscapeBaseSQL + `),count(*) FILTER(WHERE ` + webEscapeSQL + `)
	FROM review_item ri ` + resolutionSurfaceSQL + ` WHERE ri.resolved_at>=$1 AND ($3='' OR ri.review_type=$3)`

// telegramCompleteActions is the shared ordinary-action vocabulary with a
// Telegram terminal or continuation lane.
var telegramCompleteActions = reviewdomain.TelegramCompleteActions()

func ratio(numerator, denominator int) *float64 {
	if denominator == 0 {
		return nil
	}
	rate := float64(numerator) / float64(denominator)
	return &rate
}

// ReviewOpsSummary is GET /api/v1/admin/reviews/summary.
func (h *Handler) ReviewOpsSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := adminRange(r.URL.Query().Get("range"))
	var out struct {
		OpenReviews                    int      `json:"openReviews"`
		EligibleTelegramReviews        int      `json:"eligibleTelegramReviews"`
		ActionableTelegramProjections  int      `json:"actionableTelegramProjections"`
		LegacyUnmeasuredReviews        int      `json:"legacyUnmeasuredReviews"`
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
	// transaction-backed item) the transaction still awaits review. This is the
	// only point-in-time count; every other signal is range-based.
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM review_item ri LEFT JOIN transaction t ON t.id=ri.transaction_id WHERE ri.status IN ('OPEN','PENDING_SEND') AND (ri.transaction_id IS NULL OR t.status='NEEDS_REVIEW')`).Scan(&out.OpenReviews); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	if err := h.pool.QueryRow(ctx, coverageSQL, start, telegramCompleteActions).Scan(&out.EligibleTelegramReviews, &out.ActionableTelegramProjections, &out.LegacyUnmeasuredReviews); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	out.TelegramActionableCoverageRate = ratio(out.ActionableTelegramProjections, out.EligibleTelegramReviews)
	// Delivery aggregates come from the review send jobs in range.
	if err := h.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='SUCCEEDED'),count(*) FILTER(WHERE status='FAILED'),coalesce(sum(GREATEST(attempts-1,0)),0),CASE WHEN count(*)=0 THEN NULL ELSE count(*) FILTER(WHERE status='SUCCEEDED')::float/count(*) END FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json ? 'review_request_id' AND created_at>=$1`, start).Scan(&out.DeliveryAttempts, &out.DeliverySucceeded, &out.DeliveryFailed, &out.DeliveryRetried, &out.DeliverySuccessRate); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	// Resolution surface comes from the audit row the resolving surface wrote,
	// never from the resolver's Telegram identity: one person may resolve from
	// either channel. Latency still uses the canonical review window.
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE s.surface='TELEGRAM'),count(*) FILTER(WHERE s.surface='USER'),count(*) FILTER(WHERE s.surface='SYSTEM'),percentile_cont(.5) within group(order by extract(epoch FROM ri.resolved_at-ri.created_at)*1000),percentile_cont(.95) within group(order by extract(epoch FROM ri.resolved_at-ri.created_at)*1000) FROM review_item ri `+resolutionSurfaceSQL+` WHERE ri.resolved_at>=$1`, start).Scan(&out.ResolvedByTelegram, &out.ResolvedByWeb, &out.ResolvedBySystem, &out.ResolutionLatencyP50Ms, &out.ResolutionLatencyP95Ms); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='STALE_REVIEW_ACTION' AND created_at>=$1`, start).Scan(&out.StaleActionAttempts); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	var telegramPathResolved, webEscapes int
	if err := h.pool.QueryRow(ctx, webEscapeCountsSQL, start, telegramCompleteActions, "").Scan(&telegramPathResolved, &webEscapes); err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	out.WebEscapeRate = ratio(webEscapes, telegramPathResolved)
	writeJSON(w, 200, out)
}

// ReviewOpsBreakdown is GET /api/v1/admin/reviews/breakdown.
func (h *Handler) ReviewOpsBreakdown(w http.ResponseWriter, r *http.Request) {
	start := adminRange(r.URL.Query().Get("range"))
	rows, err := h.pool.Query(r.Context(), `
		SELECT ri.review_type,
		       count(*) FILTER(WHERE ri.created_at>=$1),
		       count(*) FILTER(WHERE ri.status IN ('OPEN','PENDING_SEND') AND (ri.transaction_id IS NULL OR t.status='NEEDS_REVIEW')),
		       count(*) FILTER(WHERE `+coverageEligibleSQL+`),
		       count(*) FILTER(WHERE `+coverageActionableSQL+`),
		       count(*) FILTER(WHERE ri.resolved_at>=$1 AND s.surface='TELEGRAM'),
		       count(*) FILTER(WHERE ri.resolved_at>=$1 AND s.surface='USER'),
		       count(*) FILTER(WHERE ri.resolved_at>=$1 AND s.surface='SYSTEM'),
		       count(*) FILTER(WHERE `+webEscapeBaseSQL+`),
		       count(*) FILTER(WHERE `+webEscapeSQL+`)
		FROM review_item ri LEFT JOIN transaction t ON t.id=ri.transaction_id
		`+resolutionSurfaceSQL+`
		GROUP BY ri.review_type ORDER BY ri.review_type`, start, telegramCompleteActions)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var rt string
		var created, open, eligible, projected, resolvedTelegram, resolvedWeb, resolvedSystem, telegramPathResolved, webEscapes int
		if err := rows.Scan(&rt, &created, &open, &eligible, &projected, &resolvedTelegram, &resolvedWeb, &resolvedSystem, &telegramPathResolved, &webEscapes); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		coverage := ratio(projected, eligible)
		webEscape := ratio(webEscapes, telegramPathResolved)
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
		       COALESCE(s.surface,'') AS resolved_surface
		FROM review_request rr JOIN review_item ri ON ri.id=rr.review_item_id
		`+resolutionSurfaceSQL+`
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
