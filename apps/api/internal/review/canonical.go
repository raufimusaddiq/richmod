package review

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"golang.org/x/text/unicode/norm"
)

type canonicalReview struct {
	ID                      string                    `json:"id"`
	ReviewType              string                    `json:"reviewType"`
	Status                  string                    `json:"status"`
	SubjectType             string                    `json:"subjectType"`
	SubjectID               string                    `json:"subjectId"`
	Summary                 string                    `json:"summary"`
	AmountIDR               string                    `json:"amountIdr,omitempty"`
	Channel                 string                    `json:"channel,omitempty"`
	CycleStart              string                    `json:"cycleStart,omitempty"`
	CycleEnd                string                    `json:"cycleEnd,omitempty"`
	WealthObservationID     string                    `json:"wealthObservationId,omitempty"`
	ResolvedWealthAccountID string                    `json:"resolvedWealthAccountId,omitempty"`
	Institution             string                    `json:"institution,omitempty"`
	AccountHint             string                    `json:"accountHint,omitempty"`
	FinancialObservationID  string                    `json:"financialObservationId,omitempty"`
	ResolvedAccountID       string                    `json:"resolvedAccountId,omitempty"`
	FundingAccountHint      string                    `json:"fundingAccountHint,omitempty"`
	ProviderAccountHint     string                    `json:"providerAccountHint,omitempty"`
	TransferCandidates      []transferReviewCandidate `json:"transferCandidates,omitempty"`
	ProposedPurpose         string                    `json:"proposedPurpose,omitempty"`
	ProposedWealthAccountID string                    `json:"proposedWealthAccountId,omitempty"`
	AllowedActions          []string                  `json:"allowedActions"`
	Decision                json.RawMessage           `json:"decision,omitempty"`
	CreatedAt               time.Time                 `json:"createdAt"`
}

type transferReviewCandidate struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Status        string    `json:"status"`
	Amount        string    `json:"amount"`
	TransactionAt time.Time `json:"transactionAt"`
	Description   *string   `json:"description"`
	Purpose       string    `json:"purpose,omitempty"`
	WealthAccount string    `json:"wealthAccountId,omitempty"`
}

func (h *Handler) canonicalOpenItems(ctx context.Context, household string) ([]canonicalReview, error) {
	// UIR-08: transaction-bound items are normally delivered over Telegram, so the
	// web Inbox excludes them to avoid double work. Include the orphaned ones — a
	// transaction-bound item whose transaction is already CONFIRMED (no live work)
	// — so a stranded review still surfaces where it can be resolved.
	rows, err := h.pool.Query(ctx, `SELECT ri.id,ri.review_type,ri.status,CASE WHEN ri.transaction_id IS NOT NULL THEN 'transaction' WHEN ri.financial_email_observation_id IS NOT NULL THEN 'financial_email_observation' WHEN ri.proposal_id IS NOT NULL THEN 'proposal' WHEN ri.source_event_id IS NOT NULL THEN 'source_event' WHEN ri.document_id IS NOT NULL THEN 'document' WHEN ri.wealth_observation_id IS NOT NULL THEN 'wealth_observation' ELSE 'cycle_residual_case' END,COALESCE(ri.transaction_id,ri.financial_email_observation_id,ri.proposal_id,ri.source_event_id,ri.document_id,ri.wealth_observation_id,ri.cycle_residual_case_id)::text,COALESCE(p.description,p.counterparty_raw,be.output_json->>'description',be.output_json->>'merchant',be.output_json->>'counterparty',tx.description,CASE WHEN wo.id IS NOT NULL THEN 'Konfirmasi nilai Wealth dari dokumen' WHEN ri.review_type='FINANCIAL_EMAIL_RESOLUTION' THEN 'Pilih rekening untuk bukti email finansial' WHEN ri.cycle_residual_case_id IS NOT NULL THEN 'Sisa salary cycle perlu direkonsiliasi' WHEN tx.id IS NOT NULL THEN 'Transaksi perlu ditinjau' END,'Bukti keuangan perlu ditinjau'),COALESCE(be.output_json->>'amount_idr',wo.observed_value_idr::text,crc.basis_residual_idr::text,trc.amount_idr::text,tx.amount::text,(SELECT facts_json->>'amount_idr' FROM financial_email_observation WHERE id=ri.financial_email_observation_id),''),COALESCE(be.output_json->>'channel',''),COALESCE(crc.cycle_start::text,''),COALESCE(crc.cycle_end::text,''),COALESCE(wo.id::text,''),COALESCE(wo.resolved_wealth_account_id::text,''),COALESCE(wo.institution,''),COALESCE(wo.account_hint,''),COALESCE((SELECT id::text FROM financial_email_observation WHERE id=ri.financial_email_observation_id),''),COALESCE((SELECT COALESCE(resolved_account_id::text,'') FROM financial_email_observation WHERE id=ri.financial_email_observation_id),''),COALESCE((SELECT facts_json->>'funding_account_hint' FROM financial_email_observation WHERE id=ri.financial_email_observation_id),''),COALESCE((SELECT facts_json->>'provider_account_hint' FROM financial_email_observation WHERE id=ri.financial_email_observation_id),''),COALESCE(trc.proposed_purpose,''),COALESCE(trc.proposed_wealth_account_id::text,''),COALESCE((SELECT jsonb_agg(jsonb_build_object('id',t.id,'type',t.type,'status',t.status,'amount',t.amount::text,'transactionAt',t.transaction_at,'description',t.description,'purpose',COALESCE(t.purpose,''),'wealthAccountId',COALESCE(t.related_wealth_account_id::text,'')) ORDER BY t.transaction_at,t.id) FROM transaction t WHERE t.id=ANY(trc.candidate_transaction_ids)),'[]'::jsonb),ri.decision,ri.created_at FROM review_item ri LEFT JOIN transaction tx ON tx.id=ri.transaction_id LEFT JOIN transaction_proposal p ON p.id=ri.proposal_id LEFT JOIN bank_email_extraction be ON be.source_event_id=ri.source_event_id LEFT JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id LEFT JOIN wealth_observation wo ON wo.id=ri.wealth_observation_id LEFT JOIN transfer_reconciliation_case trc ON (ri.financial_email_observation_id IS NOT NULL AND trc.financial_email_observation_id=ri.financial_email_observation_id) OR (ri.financial_email_observation_id IS NULL AND trc.source_event_id=ri.source_event_id) WHERE ri.household_id=$1 AND ri.status IN ('PENDING_SEND','OPEN') AND (ri.transaction_id IS NULL OR (tx.status <> 'NEEDS_REVIEW' AND NOT EXISTS (SELECT 1 FROM review_request rr WHERE rr.review_item_id=ri.id AND rr.status IN ('PENDING_SEND','OPEN')))) ORDER BY ri.created_at DESC`, household)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]canonicalReview, 0)
	for rows.Next() {
		var v canonicalReview
		var candidatesJSON []byte
		if err := rows.Scan(&v.ID, &v.ReviewType, &v.Status, &v.SubjectType, &v.SubjectID, &v.Summary, &v.AmountIDR, &v.Channel, &v.CycleStart, &v.CycleEnd, &v.WealthObservationID, &v.ResolvedWealthAccountID, &v.Institution, &v.AccountHint, &v.FinancialObservationID, &v.ResolvedAccountID, &v.FundingAccountHint, &v.ProviderAccountHint, &v.ProposedPurpose, &v.ProposedWealthAccountID, &candidatesJSON, &v.Decision, &v.CreatedAt); err != nil {
			return nil, err
		}
		if len(candidatesJSON) > 0 && string(candidatesJSON) != "null" && json.Unmarshal(candidatesJSON, &v.TransferCandidates) != nil {
			return nil, errors.New("invalid transfer reconciliation candidates")
		}
		v.AllowedActions = canonicalActions(v.ReviewType)
		if v.ReviewType == "PAYSLIP_CONFIRMATION" || v.ReviewType == "MISSING_PAY_DATE" {
			if actions := proposalFacts(v.Decision).AllowedActions; len(actions) > 0 {
				v.AllowedActions = actions
			}
		}
		if v.ReviewType == "TRANSFER_CLASSIFICATION" && (len(v.TransferCandidates) > 0 || v.ProposedPurpose != "") {
			if v.FinancialObservationID != "" && len(v.TransferCandidates) > 10 {
				v.AllowedActions = []string{"IGNORE"}
			} else {
				v.AllowedActions = []string{"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"}
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func canonicalActions(kind string) []string {
	if kind == "PAYSLIP_CONFIRMATION" {
		return []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}
	}
	if kind == "MISSING_PAY_DATE" {
		// The item mapper adds salary choices only when current household policy
		// leaves classification unresolved (PRD §7.6, E1/E2).
		return []string{"SET_PAY_DATE", "IGNORE"}
	}
	if kind == "CYCLE_RESIDUAL_ALLOCATION" {
		return []string{"ALLOCATE_RETAINED_BALANCE", "TRANSACTION_MISSING", "LEAVE_UNALLOCATED"}
	}
	if kind == "WEALTH_OBSERVATION_CONFIRMATION" {
		// UIRC-01 D: the snapshot editor is optional navigation, so it is not an
		// allowed completion action and never counts as a mandatory Web escape.
		return []string{"SET_WEALTH_ACCOUNT", "IGNORE"}
	}
	if kind == "FINANCIAL_EMAIL_RESOLUTION" {
		return []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"}
	}
	if kind == "UNKNOWN_BANK_TEMPLATE" {
		return []string{"COMPLETE_BANK_FACTS", "IGNORE"}
	}
	if kind == "DOCUMENT_CLASSIFICATION" || kind == "DOCUMENT_EXTRACTION_LOW_CONFIDENCE" {
		return []string{"REPROCESS_DOCUMENT", "IGNORE"}
	}
	return []string{"IGNORE"}
}

type resolveInput struct {
	Action string          `json:"action"`
	Values json.RawMessage `json:"values"`
}

func (h *Handler) Resolve(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	var in resolveInput
	if decodeJSON(r, &in) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid review resolution"})
		return
	}
	in.Action = strings.ToUpper(strings.TrimSpace(in.Action))
	if len(in.Values) == 0 {
		in.Values = []byte(`{}`)
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to resolve review"})
		return
	}
	defer tx.Rollback(r.Context())
	var kind, status string
	var decisionJSON []byte
	var proposal, source, document, transaction, residualCase, wealthObservation, financialObservation *string
	err = tx.QueryRow(r.Context(), `SELECT review_type,status,proposal_id,source_event_id,document_id,transaction_id,cycle_residual_case_id,wealth_observation_id,financial_email_observation_id,decision FROM review_item WHERE id=$1 AND household_id=$2 FOR UPDATE`, r.PathValue("id"), household).Scan(&kind, &status, &proposal, &source, &document, &transaction, &residualCase, &wealthObservation, &financialObservation, &decisionJSON)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "review not found"})
		return
	}
	if financialObservation != nil && source == nil {
		var financialSource string
		if err = tx.QueryRow(r.Context(), `SELECT source_event_id::text FROM financial_email_observation WHERE id=$1 AND household_id=$2`, *financialObservation, household).Scan(&financialSource); err != nil {
			writeJSON(w, 409, map[string]string{"error": "financial observation binding is unavailable"})
			return
		}
		source = &financialSource
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		writeJSON(w, 409, map[string]string{"error": "review is already resolved"})
		return
	}
	if (kind == "PAYSLIP_CONFIRMATION" || kind == "MISSING_PAY_DATE") && proposal != nil && (source == nil || document == nil) {
		var boundSource, boundDocument string
		err = tx.QueryRow(r.Context(), `SELECT p.source_event_id::text,d.id::text FROM transaction_proposal p
			JOIN document d ON d.id=NULLIF(p.metadata_json->>'document_id','')::uuid
			WHERE p.id=$1 AND p.household_id=$2 AND p.proposal_status='NEEDS_REVIEW' AND p.proposed_type='INCOME'
			AND d.household_id=$2 AND d.document_type='PAYSLIP' AND d.source_event_id=p.source_event_id FOR UPDATE OF p,d`, *proposal, household).Scan(&boundSource, &boundDocument)
		if err != nil || (source != nil && *source != boundSource) || (document != nil && *document != boundDocument) {
			writeJSON(w, 409, map[string]string{"error": "payslip review binding is unavailable"})
			return
		}
		source, document = &boundSource, &boundDocument
	}
	if (kind == "PAYSLIP_CONFIRMATION" || kind == "MISSING_PAY_DATE") && (proposal == nil || source == nil) {
		writeJSON(w, 409, map[string]string{"error": "payslip review binding is unavailable"})
		return
	}
	storedActions := proposalFacts(decisionJSON).AllowedActions
	if (kind == "MISSING_PAY_DATE" || kind == "PAYSLIP_CONFIRMATION") && !containsString(storedActions, in.Action) {
		writeJSON(w, 400, map[string]string{"error": "action is not allowed by this review"})
		return
	}
	payslipResolved := false
	if kind == "WEALTH_OBSERVATION_CONFIRMATION" && wealthObservation != nil {
		if in.Action == "PREPARE_SNAPSHOT" {
			writeJSON(w, 400, map[string]string{"error": "snapshot preparation is optional navigation, not a review action"})
			return
		}
		if in.Action == "SET_WEALTH_ACCOUNT" {
			var values struct {
				WealthAccountID string `json:"wealthAccountId"`
			}
			if json.Unmarshal(in.Values, &values) != nil || values.WealthAccountID == "" {
				writeJSON(w, 400, map[string]string{"error": "wealth account is required"})
				return
			}
			var hint string
			if err = tx.QueryRow(r.Context(), `SELECT account_hint FROM wealth_observation WHERE id=$1`, *wealthObservation).Scan(&hint); err != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to set wealth account"})
				return
			}
			// ADR-046: the observation mutation and review-learned alias live in the
			// shared operation; Web keeps only its HTTP mapping.
			if err = reviewdomain.ResolveWealthObservation(r.Context(), tx, reviewdomain.WealthObservationCommand{
				HouseholdID: household, ObservationID: *wealthObservation,
				WealthAccountID: values.WealthAccountID, Alias: normalizeEntityAlias(hint),
			}); errors.Is(err, reviewdomain.ErrWealthAccountInvalid) {
				writeJSON(w, 400, map[string]string{"error": "invalid household wealth account"})
				return
			}
			if err != nil || tx.Commit(r.Context()) != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to set wealth account"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	if kind == "FINANCIAL_EMAIL_RESOLUTION" && source != nil && in.Action == "SET_FINANCIAL_EMAIL_ENTITIES" {
		if financialObservation == nil {
			writeJSON(w, 409, map[string]string{"error": "financial observation binding is missing"})
			return
		}
		var values struct {
			AccountID       string   `json:"accountId"`
			WealthAccountID string   `json:"wealthAccountId"`
			HumanSupplied   []string `json:"human_supplied_fields,omitempty"`
		}
		if json.Unmarshal(in.Values, &values) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid financial entity values"})
			return
		}
		// ADR-046: the merge of supplied and already-resolved entities, the
		// household validation, the alias learning, and the canonical payload all
		// live in the shared operation. Web keeps its HTTP mapping, its review
		// completion, and the replay enqueue.
		result, err := reviewdomain.ResolveFinancialEmailReview(r.Context(), tx, reviewdomain.FinancialEmailCommand{
			HouseholdID: household, ObservationID: *financialObservation, ReviewItemID: r.PathValue("id"),
			AccountID: values.AccountID, WealthAccountID: values.WealthAccountID,
			ActorUserID: p.UserID,
		})
		if err != nil {
			writeJSON(w, financialEmailStatus(err), map[string]string{"error": financialEmailMessage(err)})
			return
		}
		err = audit(r.Context(), tx, household, p.UserID, "RESOLVE_FINANCIAL_EMAIL_ENTITIES", result.ObservationID, map[string]any{"accountId": result.AccountID, "wealthAccountId": result.WealthAccountID})
		if err != nil || tx.Commit(r.Context()) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to resolve financial email entities"})
			return
		}
		if result.Complete {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
		return
	}
	if kind == "TRANSFER_CLASSIFICATION" && source != nil && (in.Action == "MERGE_EXISTING" || in.Action == "CONFIRM_NEW_TRANSFER") {
		var values struct {
			TransactionID string `json:"transactionId"`
		}
		if json.Unmarshal(in.Values, &values) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid transfer candidate"})
			return
		}
		if _, err = reviewdomain.ReconcileTransfer(r.Context(), tx, reviewdomain.TransferReconciliationCommand{HouseholdID: household, ActorUserID: p.UserID, ReviewItemID: r.PathValue("id"), Action: in.Action, CandidateID: values.TransactionID}); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid or unavailable transfer reconciliation"})
			return
		}
		if err = audit(r.Context(), tx, household, p.UserID, "RECONCILE_TELEGRAM_TRANSFER", *source, map[string]any{"action": in.Action}); err != nil || tx.Commit(r.Context()) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to audit transfer reconciliation"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if transaction != nil {
		// UIR-08: an orphaned transaction-bound item — one whose transaction is no
		// longer awaiting review — has no remaining financial work. Acknowledge it
		// (IGNORE) so it stops surfacing without touching the confirmed transaction.
		if in.Action == "IGNORE" {
			var txStatus string
			if err = tx.QueryRow(r.Context(), `SELECT status FROM transaction WHERE id=$1 AND household_id=$2`, *transaction, household).Scan(&txStatus); err != nil {
				writeJSON(w, 404, map[string]string{"error": "review subject not found"})
				return
			}
			if txStatus == "NEEDS_REVIEW" {
				writeJSON(w, 409, map[string]string{"error": "use the existing transaction action for this review"})
				return
			}
			if _, err = tx.Exec(r.Context(), `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='NO_LONGER_APPLICABLE',resolution_values=jsonb_build_object('reason','transaction_already_final'),updated_at=now() WHERE id=$1 AND household_id=$3 AND status IN ('PENDING_SEND','OPEN')`, r.PathValue("id"), p.UserID, household); err == nil {
				_, err = tx.Exec(r.Context(), `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('PENDING_SEND','OPEN')`, r.PathValue("id"))
			}
			if err == nil {
				err = audit(r.Context(), tx, household, p.UserID, "RESOLVE_REVIEW", r.PathValue("id"), map[string]any{"action": in.Action, "reason": "transaction_already_final"})
			}
			if err != nil || tx.Commit(r.Context()) != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to finalize review"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, 409, map[string]string{"error": "use the existing transaction action for this review"})
		return
	}
	if kind == "CYCLE_RESIDUAL_ALLOCATION" && residualCase != nil {
		if in.Action == "TRANSACTION_MISSING" {
			_ = tx.Rollback(r.Context())
			writeJSON(w, http.StatusAccepted, map[string]any{"action": in.Action, "route": "CANONICAL_TRANSACTION_FLOW", "cycleResidualCaseId": *residualCase})
			return
		}
		var values struct {
			Allocations []struct {
				WealthAccountID string `json:"wealthAccountId"`
				AmountIDR       string `json:"amountIdr"`
				Note            string `json:"note"`
			} `json:"allocations"`
		}
		if (in.Action != "LEAVE_UNALLOCATED" && in.Action != "ALLOCATE_RETAINED_BALANCE") || json.Unmarshal(in.Values, &values) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid residual review action"})
			return
		}
		// ADR-046: cycle basis refresh, allocation validation, and completion live
		// in the shared operation; Web keeps only its HTTP mapping and audit.
		allocations := make([]reviewdomain.CycleAllocation, 0, len(values.Allocations))
		for _, allocation := range values.Allocations {
			allocations = append(allocations, reviewdomain.CycleAllocation{WealthAccountID: allocation.WealthAccountID, AmountIDR: allocation.AmountIDR, Note: allocation.Note})
		}
		outcome, err := reviewdomain.ApplyCycleResidual(r.Context(), tx, reviewdomain.CycleResidualCommand{
			HouseholdID: household, CaseID: *residualCase, ReviewItemID: r.PathValue("id"),
			Action: in.Action, Allocations: allocations, ActorUserID: p.UserID,
		})
		if err != nil {
			writeJSON(w, cycleResidualStatus(err), map[string]string{"error": cycleResidualMessage(err)})
			return
		}
		switch outcome.Outcome {
		case reviewdomain.CycleStaleNotApplicable:
			if audit(r.Context(), tx, household, p.UserID, "CYCLE_RESIDUAL_STALE_NOT_APPLICABLE", *residualCase, map[string]any{"residualIdr": outcome.Residual}) != nil || tx.Commit(r.Context()) != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to refresh residual review"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		case reviewdomain.CycleStaleRefreshed:
			if audit(r.Context(), tx, household, p.UserID, "CYCLE_RESIDUAL_STALE", *residualCase, map[string]any{"residualIdr": outcome.Residual}) != nil || tx.Commit(r.Context()) != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to refresh residual review"})
				return
			}
			writeJSON(w, 409, map[string]string{"error": "residual basis changed; refresh and resolve again"})
			return
		}
		if audit(r.Context(), tx, household, p.UserID, "CYCLE_RESIDUAL_"+in.Action, *residualCase, map[string]any{"residualIdr": outcome.Residual}) != nil || tx.Commit(r.Context()) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to resolve residual review"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if (kind == "UNKNOWN_BANK_TEMPLATE" || kind == "DOCUMENT_EXTRACTION_LOW_CONFIDENCE") && in.Action == "COMPLETE_BANK_FACTS" && source != nil {
		var values struct {
			AmountIDR     *string `json:"amountIdr"`
			TransactionAt *string `json:"transactionAt"`
		}
		if json.Unmarshal(in.Values, &values) != nil || (values.AmountIDR == nil && values.TransactionAt == nil) {
			writeJSON(w, 400, map[string]string{"error": "bank facts are required"})
			return
		}
		payload, _ := json.Marshal(map[string]any{"source_event_id": *source, "review_id": r.PathValue("id"), "amount_idr": values.AmountIDR, "transaction_at": values.TransactionAt})
		if _, err = tx.Exec(r.Context(), `INSERT INTO job(type,payload_json) VALUES('COMPLETE_BANK_REVIEW',$1::jsonb)`, string(payload)); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to queue bank review"})
			return
		}
		if audit(r.Context(), tx, household, p.UserID, "COMPLETE_BANK_FACTS_REQUESTED", r.PathValue("id"), map[string]any{}) != nil || tx.Commit(r.Context()) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to queue bank review"})
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if (kind == "DOCUMENT_CLASSIFICATION" || kind == "DOCUMENT_EXTRACTION_LOW_CONFIDENCE") && document != nil && (in.Action == "REPROCESS_DOCUMENT" || in.Action == "IGNORE") {
		result, err := reviewdomain.ResolveDocumentReview(r.Context(), tx, reviewdomain.DocumentReviewCommand{
			HouseholdID: household, UserID: p.UserID, ReviewItemID: r.PathValue("id"), DocumentID: *document, ActorType: "WEB", Action: in.Action,
		})
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid or unavailable review action"})
			return
		}
		if in.Action == "REPROCESS_DOCUMENT" {
			if _, err = tx.Exec(r.Context(), `INSERT INTO job(type,payload_json) SELECT 'PROCESS_DOCUMENT',jsonb_build_object('document_id',$1::uuid) WHERE NOT EXISTS(SELECT 1 FROM job WHERE type='PROCESS_DOCUMENT' AND payload_json->>'document_id'=$1::text AND status IN ('PENDING','RUNNING'))`, *document); err != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to queue document reprocess"})
				return
			}
		}
		if audit(r.Context(), tx, household, p.UserID, "RESOLVE_DOCUMENT_REVIEW", r.PathValue("id"), map[string]any{"action": in.Action, "source_event_id": result.SourceEventID}) != nil || tx.Commit(r.Context()) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to resolve document review"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if in.Action == "IGNORE" && kind == "TRANSFER_CLASSIFICATION" && source != nil {
		_, err = reviewdomain.ReconcileTransfer(r.Context(), tx, reviewdomain.TransferReconciliationCommand{HouseholdID: household, ActorUserID: p.UserID, ReviewItemID: r.PathValue("id"), Action: "IGNORE"})
		if err == nil {
			err = audit(r.Context(), tx, household, p.UserID, "RESOLVE_REVIEW", r.PathValue("id"), map[string]any{"action": "IGNORE"})
		}
		if err != nil || tx.Commit(r.Context()) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid or unavailable transfer reconciliation"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if in.Action == "IGNORE" && kind == "FINANCIAL_EMAIL_RESOLUTION" && financialObservation != nil {
		_, err = reviewdomain.ResolveFinancialEmailReview(r.Context(), tx, reviewdomain.FinancialEmailCommand{HouseholdID: household, ObservationID: *financialObservation, ReviewItemID: r.PathValue("id"), ActorUserID: p.UserID, Ignore: true})
		if err == nil {
			err = audit(r.Context(), tx, household, p.UserID, "RESOLVE_REVIEW", r.PathValue("id"), map[string]any{"action": "IGNORE"})
		}
		if err != nil || tx.Commit(r.Context()) != nil {
			writeJSON(w, 409, map[string]string{"error": "financial observation is unavailable"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if in.Action == "IGNORE" && (kind == "PAYSLIP_CONFIRMATION" || kind == "MISSING_PAY_DATE") {
		_, err = h.resolvePayslip(r, tx, household, p.UserID, r.PathValue("id"), *proposal, *source, *document, "IGNORE", "", nil)
		payslipResolved = err == nil
	} else if in.Action == "IGNORE" {
		if financialObservation != nil {
			if _, err = tx.Exec(r.Context(), `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE id=$1 AND household_id=$2`, *financialObservation, household); err != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to ignore financial observation"})
				return
			}
		}
		if wealthObservation != nil {
			if err = reviewdomain.DismissWealthObservation(r.Context(), tx, reviewdomain.WealthObservationCommand{HouseholdID: household, ObservationID: *wealthObservation, IgnoreFinancialEmail: true}); err != nil && !errors.Is(err, reviewdomain.ErrWealthObservationNotFound) {
				writeJSON(w, 500, map[string]string{"error": "unable to dismiss wealth observation"})
				return
			}
		}
		if proposal != nil {
			_, err = tx.Exec(r.Context(), `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id=$1`, *proposal)
		}
		if err == nil && source != nil {
			_, err = tx.Exec(r.Context(), `UPDATE source_event SET processing_status=CASE WHEN $2::uuid IS NULL THEN 'IGNORED' WHEN EXISTS(SELECT 1 FROM financial_email_observation WHERE source_event_id=$1 AND status IN ('PENDING','REVIEW')) THEN 'NEEDS_REVIEW' WHEN EXISTS(SELECT 1 FROM financial_email_observation WHERE source_event_id=$1 AND status='APPLIED') THEN 'PROCESSED' ELSE 'IGNORED' END WHERE id=$1`, *source, financialObservation)
		}
		if err == nil && document != nil {
			_, err = tx.Exec(r.Context(), `UPDATE document SET status='NEEDS_REVIEW',updated_at=now() WHERE id=$1`, *document)
		}
	} else if kind == "PAYSLIP_CONFIRMATION" && (in.Action == "PRIMARY_SALARY" || in.Action == "ORDINARY_INCOME") {
		_, err = h.resolvePayslip(r, tx, household, p.UserID, r.PathValue("id"), *proposal, *source, *document, in.Action, in.Action, nil)
		payslipResolved = err == nil
	} else if kind == "MISSING_PAY_DATE" && in.Action == "SET_PAY_DATE" {
		var v struct {
			PayDate string `json:"payDate"`
			Choice  string `json:"choice"`
		}
		if json.Unmarshal(in.Values, &v) != nil {
			err = errInvalid
		}
		if err == nil {
			date, parseErr := time.Parse("2006-01-02", v.PayDate)
			if parseErr != nil {
				err = errInvalid
			} else {
				choice := strings.ToUpper(strings.TrimSpace(v.Choice))
				_, err = h.resolvePayslip(r, tx, household, p.UserID, r.PathValue("id"), *proposal, *source, *document, in.Action, choice, &date)
				payslipResolved = err == nil
			}
		}
	} else {
		err = errInvalid
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid or unavailable review action"})
		return
	}
	if !payslipResolved {
		_, err = tx.Exec(r.Context(), `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now() WHERE id=$1`, r.PathValue("id"), p.UserID, in.Action, string(in.Values))
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('PENDING_SEND','OPEN')`, r.PathValue("id"))
		}
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to finalize review"})
		return
	}
	if (!payslipResolved && audit(r.Context(), tx, household, p.UserID, "RESOLVE_REVIEW", r.PathValue("id"), map[string]any{"action": in.Action}) != nil) || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to audit review resolution"})
		return
	}
	w.WriteHeader(204)
}

func normalizeEntityAlias(value string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(norm.NFKC.String(value))) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		} else {
			space = true
		}
	}
	return b.String()
}

// learnEntityAlias and learnEntityAliasIfNew now live in reviewdomain; the
// Review Inbox financial-email resolution calls them there. No local duplicate
// is kept, so an alias-learning change cannot drift from the shared operation.

var errInvalid = &reviewResolutionError{}

type reviewResolutionError struct{}

func (*reviewResolutionError) Error() string { return "invalid resolution" }

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (h *Handler) resolvePayslip(r *http.Request, tx pgx.Tx, household, user, reviewItem, proposal, source, document, action, choice string, payDate *time.Time) (reviewdomain.PayslipResult, error) {
	result, err := reviewdomain.ResolvePayslipProposal(r.Context(), tx, reviewdomain.PayslipCommand{
		HouseholdID: household, UserID: user, ReviewItemID: reviewItem, ProposalID: proposal,
		SourceEventID: source, DocumentID: document, ActorType: "USER", Action: action, Choice: choice, PayDate: payDate,
	})
	if errors.Is(err, reviewdomain.ErrPayslipReviewInvalid) {
		return result, errInvalid
	}
	return result, err
}

// cycleResidualStatus maps a shared cycle-residual error to an HTTP status. The
// shared operation returns typed errors so every surface keeps its own status
// code without owning the validation rules.
func cycleResidualStatus(err error) int {
	switch {
	case errors.Is(err, reviewdomain.ErrCycleCaseNotFound):
		return 404
	case errors.Is(err, reviewdomain.ErrCycleAllocationsRequired),
		errors.Is(err, reviewdomain.ErrCycleAllocationInvalid),
		errors.Is(err, reviewdomain.ErrCycleAllocationDuplicate),
		errors.Is(err, reviewdomain.ErrCycleAllocationMismatch),
		errors.Is(err, reviewdomain.ErrCycleWealthAccountInvalid):
		return 400
	default:
		return 500
	}
}

// cycleResidualMessage returns the client-facing message for a cycle-residual
// error, preserving the wording the Web API already returned.
func cycleResidualMessage(err error) string {
	switch {
	case errors.Is(err, reviewdomain.ErrCycleCaseNotFound):
		return "residual case not found"
	case errors.Is(err, reviewdomain.ErrCycleAllocationsRequired):
		return "allocations are required"
	case errors.Is(err, reviewdomain.ErrCycleAllocationInvalid):
		return "invalid allocation"
	case errors.Is(err, reviewdomain.ErrCycleAllocationDuplicate):
		return "wealth accounts must be unique"
	case errors.Is(err, reviewdomain.ErrCycleAllocationMismatch):
		return "allocation total must equal residual"
	case errors.Is(err, reviewdomain.ErrCycleWealthAccountInvalid):
		return "invalid household wealth account"
	default:
		return "unable to resolve residual review"
	}
}

// financialEmailStatus maps a shared financial-email resolution error to an HTTP
// status so the surface does not re-implement the validation rules.
func financialEmailStatus(err error) int {
	switch {
	case errors.Is(err, reviewdomain.ErrFinancialObservationUnavailable):
		return 409
	case errors.Is(err, reviewdomain.ErrFundingAccountRequired),
		errors.Is(err, reviewdomain.ErrProviderAccountRequired),
		errors.Is(err, reviewdomain.ErrAccountInvalid),
		errors.Is(err, reviewdomain.ErrWealthAccountInvalid):
		return 400
	default:
		return 500
	}
}

// financialEmailMessage returns the client-facing message for a shared
// financial-email resolution error, preserving the previous wording.
func financialEmailMessage(err error) string {
	switch {
	case errors.Is(err, reviewdomain.ErrFinancialObservationUnavailable):
		return "financial observation is unavailable"
	case errors.Is(err, reviewdomain.ErrFundingAccountRequired):
		return "the unresolved funding account is required"
	case errors.Is(err, reviewdomain.ErrProviderAccountRequired):
		return "the unresolved Wealth Account is required"
	case errors.Is(err, reviewdomain.ErrAccountInvalid):
		return "invalid household account"
	case errors.Is(err, reviewdomain.ErrWealthAccountInvalid):
		return "invalid household wealth account"
	default:
		return "unable to resolve financial email entities"
	}
}
