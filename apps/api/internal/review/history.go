package review

import (
	"errors"
	"net/http"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

type resolvedReview struct {
	ID               string    `json:"id"`
	ReviewType       string    `json:"reviewType"`
	ResolutionAction string    `json:"resolutionAction"`
	ResolvedAt       time.Time `json:"resolvedAt"`
	AmountIDR        string    `json:"amountIdr,omitempty"`
	Summary          string    `json:"summary"`
	Reopenable       bool      `json:"reopenable"`
}

// Resolved lists the household's most recently resolved reviews so a decision
// can be reopened where reviewdomain.ReopenReview allows it. Reopenable mirrors
// that rule; the POST remains the authority and refuses anything else.
func (h *Handler) Resolved(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT ri.id::text,ri.review_type,COALESCE(ri.resolution_action,''),ri.resolved_at,
		COALESCE(t.amount::text,wo.observed_value_idr::text,crc.basis_residual_idr::text,p.amount::text,feo.facts_json->>'amount_idr',''),
		COALESCE(t.description,p.description,p.counterparty_raw,wo.institution,'Tinjauan selesai'),
		(NOT EXISTS (SELECT 1 FROM review_item n WHERE n.household_id=ri.household_id AND n.created_at>ri.created_at AND n.id<>ri.id AND (
			n.transaction_id=ri.transaction_id OR n.cycle_residual_case_id=ri.cycle_residual_case_id OR n.wealth_observation_id=ri.wealth_observation_id
			OR n.financial_email_observation_id=ri.financial_email_observation_id OR n.proposal_id=ri.proposal_id OR n.source_event_id=ri.source_event_id OR n.document_id=ri.document_id))
		AND (CASE
			WHEN ri.transaction_id IS NOT NULL THEN t.status='NEEDS_REVIEW'
			WHEN ri.cycle_residual_case_id IS NOT NULL THEN ri.resolution_action IN ('LEAVE_UNALLOCATED','TRANSACTION_MISSING','NO_LONGER_APPLICABLE')
			WHEN ri.wealth_observation_id IS NOT NULL THEN wo.status IN ('ACCEPTED','DISMISSED')
			WHEN ri.financial_email_observation_id IS NOT NULL THEN feo.status IN ('IGNORED','REVIEW') AND feo.transaction_id IS NULL
			WHEN ri.proposal_id IS NOT NULL THEN p.proposal_status='REJECTED'
			ELSE COALESCE(s.processing_status,ds.processing_status)='IGNORED' AND NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.source_event_id=COALESCE(s.id,ds.id) AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE')
		END)) AS reopenable
		FROM review_item ri
		LEFT JOIN transaction t ON t.id=ri.transaction_id AND t.household_id=ri.household_id
		LEFT JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id AND crc.household_id=ri.household_id
		LEFT JOIN wealth_observation wo ON wo.id=ri.wealth_observation_id AND wo.household_id=ri.household_id
		LEFT JOIN financial_email_observation feo ON feo.id=ri.financial_email_observation_id AND feo.household_id=ri.household_id
		LEFT JOIN transaction_proposal p ON p.id=ri.proposal_id AND p.household_id=ri.household_id
		LEFT JOIN source_event s ON s.id=ri.source_event_id AND s.household_id=ri.household_id
		LEFT JOIN document d ON d.id=ri.document_id AND d.household_id=ri.household_id
		LEFT JOIN source_event ds ON ds.id=d.source_event_id AND ds.household_id=ri.household_id
		WHERE ri.household_id=$1 AND ri.status='RESOLVED' AND ri.resolved_at > now()-interval '30 days'
		ORDER BY ri.resolved_at DESC,ri.id DESC LIMIT 30`, household)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list resolved reviews"})
		return
	}
	defer rows.Close()
	out := make([]resolvedReview, 0)
	for rows.Next() {
		var v resolvedReview
		var reopenable *bool
		if err := rows.Scan(&v.ID, &v.ReviewType, &v.ResolutionAction, &v.ResolvedAt, &v.AmountIDR, &v.Summary, &reopenable); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list resolved reviews"})
			return
		}
		v.Reopenable = reopenable != nil && *reopenable
		out = append(out, v)
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list resolved reviews"})
		return
	}
	writeJSON(w, 200, out)
}

// Reopen reverses one resolved review through the shared domain operation and
// returns the new open review item.
func (h *Handler) Reopen(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to reopen review"})
		return
	}
	defer tx.Rollback(r.Context())
	id, err := reviewdomain.ReopenReview(r.Context(), tx, reviewdomain.ReopenCommand{HouseholdID: household, ActorUserID: p.UserID, ReviewItemID: r.PathValue("id")})
	if errors.Is(err, reviewdomain.ErrReopenNotReversible) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "keputusan ini tidak dapat dibuka kembali dari tinjauan"})
		return
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to reopen review"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}
