package review

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
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
		return []string{"PREPARE_SNAPSHOT", "SET_WEALTH_ACCOUNT", "IGNORE"}
	}
	if kind == "FINANCIAL_EMAIL_RESOLUTION" {
		return []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"}
	}
	if kind == "UNKNOWN_BANK_TEMPLATE" || kind == "DOCUMENT_EXTRACTION_LOW_CONFIDENCE" {
		return []string{"COMPLETE_BANK_FACTS", "IGNORE"}
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
	storedActions := proposalFacts(decisionJSON).AllowedActions
	if kind == "MISSING_PAY_DATE" && !containsString(storedActions, in.Action) {
		writeJSON(w, 400, map[string]string{"error": "action is not allowed by this review"})
		return
	}
	enqueueSalaryResidual := in.Action == "PRIMARY_SALARY"
	if kind == "WEALTH_OBSERVATION_CONFIRMATION" && wealthObservation != nil {
		if in.Action == "PREPARE_SNAPSHOT" {
			if err = tx.Commit(r.Context()); err != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to prepare wealth snapshot"})
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]any{"action": in.Action, "route": "WEALTH_SNAPSHOT", "wealthObservationId": *wealthObservation})
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
			var valid bool
			if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM wealth_account WHERE id=$1 AND household_id=$2 AND active)`, values.WealthAccountID, household).Scan(&valid); err != nil || !valid {
				writeJSON(w, 400, map[string]string{"error": "invalid household wealth account"})
				return
			}
			var hint string
			if err = tx.QueryRow(r.Context(), `SELECT account_hint FROM wealth_observation WHERE id=$1`, *wealthObservation).Scan(&hint); err != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to set wealth account"})
				return
			}
			if _, err = tx.Exec(r.Context(), `UPDATE wealth_observation SET resolved_wealth_account_id=$2,updated_at=now() WHERE id=$1 AND household_id=$3`, *wealthObservation, values.WealthAccountID, household); err == nil && normalizeEntityAlias(hint) != "" {
				_, err = tx.Exec(r.Context(), `INSERT INTO financial_entity_alias(household_id,entity_type,wealth_account_id,alias,normalized_alias,source) VALUES($1,'WEALTH_ACCOUNT',$2,$3,$4,'REVIEW_LEARNED') ON CONFLICT (household_id,entity_type,normalized_alias) WHERE active DO UPDATE SET wealth_account_id=EXCLUDED.wealth_account_id,alias=EXCLUDED.alias,source='REVIEW_LEARNED',updated_at=now()`, household, values.WealthAccountID, strings.TrimSpace(hint), normalizeEntityAlias(hint))
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
		values.HumanSupplied = nil // Client-supplied telemetry claims are never trusted.
		if values.AccountID != "" {
			values.HumanSupplied = append(values.HumanSupplied, "account")
		}
		if values.WealthAccountID != "" {
			values.HumanSupplied = append(values.HumanSupplied, "wealth_account")
		}
		var observationID, fundingHint, providerHint, knownAccount, knownWealth string
		if err = tx.QueryRow(r.Context(), `SELECT id::text,COALESCE(facts_json->>'funding_account_hint',''),COALESCE(facts_json->>'provider_account_hint',''),COALESCE(resolved_account_id::text,''),COALESCE(resolved_wealth_account_id::text,'') FROM financial_email_observation WHERE id=$1 AND household_id=$2 AND status='REVIEW' FOR UPDATE`, *financialObservation, household).Scan(&observationID, &fundingHint, &providerHint, &knownAccount, &knownWealth); err != nil {
			writeJSON(w, 409, map[string]string{"error": "financial observation is unavailable"})
			return
		}
		// PRD §12/§20.1: the request supplies only the unresolved entities. Already
		// resolved entities are reloaded from persisted state and merged here, so a
		// review that already knows the funding account never asks for it again.
		// An entity that is still unresolved must be supplied: a resolution that
		// leaves one blank would write a half-bound observation.
		if knownAccount == "" && values.AccountID == "" {
			writeJSON(w, 400, map[string]string{"error": "the unresolved funding account is required"})
			return
		}
		if knownWealth == "" && values.WealthAccountID == "" {
			writeJSON(w, 400, map[string]string{"error": "the unresolved Wealth Account is required"})
			return
		}
		accountID, wealthAccountID := values.AccountID, values.WealthAccountID
		if accountID == "" {
			accountID = knownAccount
		}
		if wealthAccountID == "" {
			wealthAccountID = knownWealth
		}
		if accountID != "" {
			var valid bool
			if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM account WHERE id=$1 AND household_id=$2 AND active)`, accountID, household).Scan(&valid); err != nil || !valid {
				writeJSON(w, 400, map[string]string{"error": "invalid household account"})
				return
			}
		}
		if wealthAccountID != "" {
			var valid bool
			if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM wealth_account WHERE id=$1 AND household_id=$2 AND active)`, wealthAccountID, household).Scan(&valid); err != nil || !valid {
				writeJSON(w, 400, map[string]string{"error": "invalid household wealth account"})
				return
			}
		}
		values.AccountID, values.WealthAccountID = accountID, wealthAccountID
		merged, _ := json.Marshal(values)
		if _, err = tx.Exec(r.Context(), `UPDATE financial_email_observation SET resolved_account_id=NULLIF($2,'')::uuid,resolved_wealth_account_id=NULLIF($3,'')::uuid,status='PENDING',updated_at=now() WHERE id=$1`, observationID, values.AccountID, values.WealthAccountID); err == nil {
			err = learnEntityAliasIfNew(r.Context(), tx, household, "ACCOUNT", values.AccountID, fundingHint, knownAccount)
		}
		if err == nil {
			err = learnEntityAliasIfNew(r.Context(), tx, household, "WEALTH_ACCOUNT", values.WealthAccountID, providerHint, knownWealth)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='SET_FINANCIAL_EMAIL_ENTITIES',resolution_values=$3::jsonb,updated_at=now() WHERE id=$1`, r.PathValue("id"), p.UserID, string(merged))
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('PENDING_SEND','OPEN')`, r.PathValue("id"))
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE source_event SET processing_status='RECEIVED' WHERE id=$1 AND household_id=$2`, *source, household)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO job(type,payload_json,max_attempts) VALUES('PROCESS_FINANCIAL_EMAIL',jsonb_build_object('source_event_id',$1::uuid,'financial_source_id',(SELECT financial_source_id FROM financial_email_event WHERE source_event_id=$1)),5)`, *source)
		}
		if err == nil {
			err = audit(r.Context(), tx, household, p.UserID, "RESOLVE_FINANCIAL_EMAIL_ENTITIES", observationID, map[string]any{"accountId": values.AccountID, "wealthAccountId": values.WealthAccountID})
		}
		if err != nil || tx.Commit(r.Context()) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to resolve financial email entities"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if kind == "TRANSFER_CLASSIFICATION" && source != nil && (in.Action == "MERGE_EXISTING" || in.Action == "CONFIRM_NEW_TRANSFER") {
		if err = h.resolveTransferReconciliation(r, tx, p.UserID, household, r.PathValue("id"), *source, financialObservation, in.Action, in.Values); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid or unavailable transfer reconciliation"})
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now() WHERE id=$1`, r.PathValue("id"), p.UserID, in.Action, string(in.Values)); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to finalize transfer reconciliation"})
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('PENDING_SEND','OPEN')`, r.PathValue("id")); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to finalize transfer reconciliation"})
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
		var oldIncome, oldExpense, oldSavings, oldResidual string
		var income, expense, savings, residual string
		err = tx.QueryRow(r.Context(), `SELECT basis_income_idr::text,basis_expense_idr::text,basis_savings_idr::text,basis_residual_idr::text,(SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')),(SELECT COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')),(SELECT COALESCE(sum(amount) FILTER(WHERE type='TRANSFER' AND purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE')),0)::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')),(SELECT (COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)-COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)-COALESCE(sum(amount) FILTER(WHERE type='TRANSFER' AND purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE')),0))::text FROM transaction WHERE household_id=c.household_id AND status='CONFIRMED' AND transaction_at >= (c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta') AND transaction_at < (c.cycle_end::timestamp AT TIME ZONE 'Asia/Jakarta')) FROM cycle_residual_case c WHERE id=$1 AND household_id=$2 FOR UPDATE`, *residualCase, household).Scan(&oldIncome, &oldExpense, &oldSavings, &oldResidual, &income, &expense, &savings, &residual)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "residual case not found"})
			return
		}
		changed := oldIncome != income || oldExpense != expense || oldSavings != savings || oldResidual != residual
		residualInt, ok := new(big.Int).SetString(residual, 10)
		if !ok {
			writeJSON(w, 500, map[string]string{"error": "unable to refresh residual review"})
			return
		}
		if changed {
			if residualInt.Sign() > 0 {
				_, err = tx.Exec(r.Context(), `UPDATE cycle_residual_case SET basis_income_idr=$2,basis_expense_idr=$3,basis_savings_idr=$4,basis_residual_idr=$5,updated_at=now() WHERE id=$1`, *residualCase, income, expense, savings, residual)
				if err == nil {
					err = audit(r.Context(), tx, household, p.UserID, "CYCLE_RESIDUAL_STALE", *residualCase, map[string]any{"incomeIdr": income, "expenseIdr": expense, "savingsIdr": savings, "residualIdr": residual})
				}
				if err != nil || tx.Commit(r.Context()) != nil {
					writeJSON(w, 500, map[string]string{"error": "unable to refresh residual review"})
					return
				}
				writeJSON(w, 409, map[string]string{"error": "residual basis changed; refresh and resolve again"})
				return
			}
			in.Action = "NO_LONGER_APPLICABLE"
		}
		if in.Action == "ALLOCATE_RETAINED_BALANCE" {
			if len(values.Allocations) == 0 {
				writeJSON(w, 400, map[string]string{"error": "allocations are required"})
				return
			}
			total := new(big.Int)
			seen := make(map[string]struct{}, len(values.Allocations))
			for _, allocation := range values.Allocations {
				amount, valid := new(big.Int).SetString(allocation.AmountIDR, 10)
				if allocation.WealthAccountID == "" || !valid || amount.Sign() <= 0 {
					writeJSON(w, 400, map[string]string{"error": "invalid allocation"})
					return
				}
				if _, duplicate := seen[allocation.WealthAccountID]; duplicate {
					writeJSON(w, 400, map[string]string{"error": "wealth accounts must be unique"})
					return
				}
				seen[allocation.WealthAccountID] = struct{}{}
				total.Add(total, amount)
			}
			if total.Cmp(residualInt) != 0 {
				writeJSON(w, 400, map[string]string{"error": "allocation total must equal residual"})
				return
			}
			var validAccounts int
			accountIDs := make([]string, 0, len(seen))
			for id := range seen {
				accountIDs = append(accountIDs, id)
			}
			if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM wealth_account WHERE household_id=$1 AND active AND id=ANY($2::uuid[])`, household, accountIDs).Scan(&validAccounts); err != nil || validAccounts != len(accountIDs) {
				writeJSON(w, 400, map[string]string{"error": "invalid household wealth account"})
				return
			}
			for _, allocation := range values.Allocations {
				if _, err = tx.Exec(r.Context(), `INSERT INTO cycle_residual_allocation(cycle_residual_case_id,wealth_account_id,amount_idr,note,created_by_user_id) VALUES($1,$2,$3,$4,$5)`, *residualCase, allocation.WealthAccountID, allocation.AmountIDR, allocation.Note, p.UserID); err != nil {
					writeJSON(w, 500, map[string]string{"error": "unable to allocate residual"})
					return
				}
			}
		}
		if _, err = tx.Exec(r.Context(), `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now() WHERE id=$1`, r.PathValue("id"), p.UserID, in.Action, string(in.Values)); err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('PENDING_SEND','OPEN')`, r.PathValue("id"))
		}
		if err == nil {
			err = audit(r.Context(), tx, household, p.UserID, "CYCLE_RESIDUAL_"+in.Action, *residualCase, map[string]any{"residualIdr": residual})
		}
		if err != nil || tx.Commit(r.Context()) != nil {
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
	if in.Action == "IGNORE" {
		if financialObservation != nil {
			if _, err = tx.Exec(r.Context(), `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE id=$1 AND household_id=$2`, *financialObservation, household); err != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to ignore financial observation"})
				return
			}
		}
		if wealthObservation != nil {
			if _, err = tx.Exec(r.Context(), `UPDATE wealth_observation SET status='DISMISSED',updated_at=now() WHERE id=$1 AND household_id=$2`, *wealthObservation, household); err != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to dismiss wealth observation"})
				return
			}
			if _, err = tx.Exec(r.Context(), `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE id=(SELECT financial_email_observation_id FROM wealth_observation WHERE id=$1)`, *wealthObservation); err != nil {
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
		if err == nil && source != nil {
			_, err = tx.Exec(r.Context(), `UPDATE transfer_reconciliation_case SET status='DISMISSED',resolved_at=now(),resolved_by_user_id=$2,updated_at=now() WHERE status='OPEN' AND (($3::uuid IS NOT NULL AND financial_email_observation_id=$3::uuid) OR ($3::uuid IS NULL AND source_event_id=$1))`, *source, p.UserID, financialObservation)
		}
	} else if kind == "PAYSLIP_CONFIRMATION" && (in.Action == "PRIMARY_SALARY" || in.Action == "ORDINARY_INCOME") {
		err = h.resolvePayslip(r, tx, household, p.UserID, *proposal, *source, *document, in.Action)
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
				var hasPrimary bool
				if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM salary_source WHERE household_id=$1 AND active AND is_primary)`, household).Scan(&hasPrimary); err == nil {
					choice := strings.ToUpper(strings.TrimSpace(v.Choice))
					if !containsString(storedActions, "SET_PAY_DATE") {
						err = errInvalid
					}
					if choice == "" && hasPrimary {
						choice = "HOUSEHOLD_POLICY"
					} else if choice == "" || (hasPrimary && choice != "") || (!hasPrimary && choice != "PRIMARY_SALARY" && choice != "ORDINARY_INCOME") {
						err = errInvalid
					}
					if (choice == "PRIMARY_SALARY" || choice == "ORDINARY_INCOME") && !containsString(storedActions, choice) {
						err = errInvalid
					}
					if err == nil {
						_, err = tx.Exec(r.Context(), `UPDATE transaction_proposal SET transaction_at=$2::date,updated_at=now() WHERE id=$1`, *proposal, date)
					}
					if err == nil {
						err = h.resolvePayslip(r, tx, household, p.UserID, *proposal, *source, *document, choice)
						enqueueSalaryResidual = choice == "PRIMARY_SALARY" || choice == "HOUSEHOLD_POLICY"
					}
					if err == nil && choice == "HOUSEHOLD_POLICY" {
						in.Values = json.RawMessage(`{"payDate":"` + v.PayDate + `"}`)
					}
				}
			}
		}
	} else {
		err = errInvalid
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid or unavailable review action"})
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now() WHERE id=$1`, r.PathValue("id"), p.UserID, in.Action, string(in.Values))
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('PENDING_SEND','OPEN')`, r.PathValue("id"))
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to finalize review"})
		return
	}
	if audit(r.Context(), tx, household, p.UserID, "RESOLVE_REVIEW", r.PathValue("id"), map[string]any{"action": in.Action}) != nil || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to audit review resolution"})
		return
	}
	// Salary state is already committed. Queue failure is deliberately best-effort;
	// worker catch-up repairs a lost enqueue without rolling salary back.
	if enqueueSalaryResidual && source != nil {
		var salaryEventID string
		if err := h.pool.QueryRow(r.Context(), `SELECT id FROM salary_event WHERE household_id=$1 AND source_event_id=$2 AND status='CONFIRMED' ORDER BY created_at DESC LIMIT 1`, household, *source).Scan(&salaryEventID); err == nil {
			_, _ = h.pool.Exec(r.Context(), `INSERT INTO job(type,payload_json,max_attempts) VALUES('GENERATE_CYCLE_RESIDUAL_REVIEW',jsonb_build_object('household_id',$1::uuid,'end_salary_event_id',$2::uuid),5)`, household, salaryEventID)
		}
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

func learnEntityAlias(ctx context.Context, tx pgx.Tx, household, entityType, entityID, alias string) error {
	normalized := normalizeEntityAlias(alias)
	if normalized == "" {
		return nil
	}
	// A mapping the household set itself is never overwritten by review learning:
	// an explicit user choice outranks an inferred one (PRD §19, 'learning != silent
	// assumption').
	if entityType == "ACCOUNT" {
		_, err := tx.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,account_id,alias,normalized_alias,source) VALUES($1,'ACCOUNT',$2,$3,$4,'REVIEW_LEARNED') ON CONFLICT (household_id,entity_type,normalized_alias) WHERE active DO UPDATE SET account_id=EXCLUDED.account_id,wealth_account_id=NULL,alias=EXCLUDED.alias,source='REVIEW_LEARNED',updated_at=now() WHERE financial_entity_alias.source <> 'USER'`, household, entityID, strings.TrimSpace(alias), normalized)
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,wealth_account_id,alias,normalized_alias,source) VALUES($1,'WEALTH_ACCOUNT',$2,$3,$4,'REVIEW_LEARNED') ON CONFLICT (household_id,entity_type,normalized_alias) WHERE active DO UPDATE SET wealth_account_id=EXCLUDED.wealth_account_id,account_id=NULL,alias=EXCLUDED.alias,source='REVIEW_LEARNED',updated_at=now() WHERE financial_entity_alias.source <> 'USER'`, household, entityID, strings.TrimSpace(alias), normalized)
	return err
}

// learnEntityAliasIfNew records the alias a user supplied for an entity this
// review just resolved. An entity that was already known before the request is
// left alone: its alias was learned when it was first bound, and re-learning it
// here would let a partial resolution quietly rewrite an existing mapping.
func learnEntityAliasIfNew(ctx context.Context, tx pgx.Tx, household, entityType, entityID, alias, known string) error {
	if strings.TrimSpace(known) != "" {
		return nil
	}
	return learnEntityAlias(ctx, tx, household, entityType, entityID, alias)
}

func (h *Handler) resolveTransferReconciliation(r *http.Request, tx pgx.Tx, user, household, reviewID, sourceID string, financialObservation *string, action string, raw json.RawMessage) error {
	var accountID, amount, description, purpose, wealthID string
	var at time.Time
	var candidates []string
	if err := tx.QueryRow(r.Context(), `SELECT account_id::text,amount_idr::text,COALESCE(description,''),proposed_purpose,COALESCE(proposed_wealth_account_id::text,''),transaction_at,candidate_transaction_ids FROM transfer_reconciliation_case WHERE household_id=$1 AND status='OPEN' AND (($3::uuid IS NOT NULL AND financial_email_observation_id=$3::uuid) OR ($3::uuid IS NULL AND source_event_id=$2)) FOR UPDATE`, household, sourceID, financialObservation).Scan(&accountID, &amount, &description, &purpose, &wealthID, &at, &candidates); err != nil {
		return errInvalid
	}
	if financialObservation != nil && len(candidates) > 10 {
		return errInvalid
	}
	var compatible bool
	if err := tx.QueryRow(r.Context(), `SELECT transfer_wealth_compatible($1,NULLIF($2,'')::uuid,$3)`, purpose, wealthID, household).Scan(&compatible); err != nil || !compatible {
		return errInvalid
	}
	var transactionID string
	var sourceType string
	if err := tx.QueryRow(r.Context(), `SELECT source_type FROM source_event WHERE id=$1 AND household_id=$2`, sourceID, household).Scan(&sourceType); err != nil {
		return errInvalid
	}
	if action == "MERGE_EXISTING" {
		var values struct {
			TransactionID string `json:"transactionId"`
		}
		if json.Unmarshal(raw, &values) != nil || values.TransactionID == "" {
			return errInvalid
		}
		var targetAccount, targetType, targetStatus, targetAmount string
		if err := tx.QueryRow(r.Context(), `SELECT account_id::text,type,status,amount::text FROM transaction WHERE id=$1 AND household_id=$2 AND id=ANY($3::uuid[]) FOR UPDATE`, values.TransactionID, household, candidates).Scan(&targetAccount, &targetType, &targetStatus, &targetAmount); err != nil || targetAccount != accountID || targetAmount != amount || (targetType != "TRANSFER" && targetType != "UNCLASSIFIED") || targetStatus == "VOIDED" {
			return errInvalid
		}
		if _, err := tx.Exec(r.Context(), `UPDATE transaction SET type='TRANSFER',status='CONFIRMED',category_id=NULL,purpose=$2,related_wealth_account_id=NULLIF($3,'')::uuid,description=COALESCE(NULLIF(description,''),NULLIF($4,'')),confirmed_at=COALESCE(confirmed_at,now()),updated_at=now() WHERE id=$1`, values.TransactionID, purpose, wealthID, description); err != nil {
			return err
		}
		transactionID = values.TransactionID
		if targetType == "UNCLASSIFIED" || targetStatus == "NEEDS_REVIEW" {
			if err := reviewdomain.FinalizeTransferReviewLifecycle(r.Context(), tx, household, user, transactionID, "TRANSFER", "ACCEPTED", "PROCESSED", nil, "TRANSFER_RECONCILED", "TRANSFER_RECONCILED"); err != nil {
				return err
			}
		}
	} else {
		if err := tx.QueryRow(r.Context(), `INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,description,created_by_user_id,purpose,related_wealth_account_id,confirmed_at) VALUES($1,$2,'TRANSFER','CONFIRMED',$3,'IDR',$4,NULLIF($5,''),$6,$7,NULLIF($8,'')::uuid,now()) RETURNING id`, household, accountID, amount, at, description, user, purpose, wealthID).Scan(&transactionID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,$3,1) ON CONFLICT DO NOTHING`, transactionID, sourceID, sourceType); err != nil {
		return err
	}
	if financialObservation != nil {
		if _, err := tx.Exec(r.Context(), `UPDATE financial_email_observation SET transaction_id=$2,status='APPLIED',updated_at=now() WHERE id=$1 AND household_id=$3`, *financialObservation, transactionID, household); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(r.Context(), `UPDATE source_event SET processing_status=CASE WHEN source_type='FINANCIAL_EMAIL' AND EXISTS(SELECT 1 FROM financial_email_observation WHERE source_event_id=$1 AND status='REVIEW') THEN 'NEEDS_REVIEW' ELSE 'PROCESSED' END,parser_name=CASE WHEN source_type='FINANCIAL_EMAIL' THEN 'financial-email-reconciliation' ELSE 'telegram-transfer' END,parser_version='1' WHERE id=$1 AND household_id=$2`, sourceID, household); err != nil {
		return err
	}
	_, err := tx.Exec(r.Context(), `UPDATE transfer_reconciliation_case SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,updated_at=now() WHERE ($3::uuid IS NOT NULL AND financial_email_observation_id=$3::uuid) OR ($3::uuid IS NULL AND source_event_id=$1)`, sourceID, user, financialObservation)
	return err
}

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

func (h *Handler) resolvePayslip(r *http.Request, tx pgx.Tx, household, user, proposal, source, document, choice string) error {
	if choice != "PRIMARY_SALARY" && choice != "ORDINARY_INCOME" && choice != "HOUSEHOLD_POLICY" {
		return errInvalid
	}
	var amount, employer, period string
	var at time.Time
	if err := tx.QueryRow(r.Context(), `SELECT amount::text,COALESCE(counterparty_raw,''),COALESCE(metadata_json->>'period',''),transaction_at FROM transaction_proposal WHERE id=$1 AND household_id=$2 AND proposal_status='NEEDS_REVIEW' FOR UPDATE`, proposal, household).Scan(&amount, &employer, &period, &at); err != nil {
		return errInvalid
	}
	var transaction string
	if err := tx.QueryRow(r.Context(), `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'INCOME','CONFIRMED',$2,'IDR',$3,'Penghasilan dari slip gaji',NULLIF($4,''),$5,now()) RETURNING id`, household, amount, at, employer, user).Scan(&transaction); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'PAYSLIP_IMAGE',jsonb_build_object('proposal_id',$3::uuid,'document_id',$4::uuid))`, transaction, source, proposal, document); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `UPDATE transaction_proposal SET proposal_status='ACCEPTED',updated_at=now() WHERE id=$1`, proposal); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1`, source); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `UPDATE document SET status='EXTRACTED',updated_at=now() WHERE id=$1`, document); err != nil {
		return err
	}
	if choice == "ORDINARY_INCOME" {
		return nil
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(employer), " "))
	var salarySource string
	if choice == "HOUSEHOLD_POLICY" {
		if err := tx.QueryRow(r.Context(), `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,$3,$4,false) ON CONFLICT (household_id,normalized_employer) WHERE active DO UPDATE SET employer=excluded.employer,updated_at=now() RETURNING id`, household, user, employer, normalized).Scan(&salarySource); err != nil {
			return err
		}
	} else {
		if err := tx.QueryRow(r.Context(), `SELECT id FROM salary_source WHERE household_id=$1 AND normalized_employer=$2 AND active FOR UPDATE`, household, normalized).Scan(&salarySource); err != nil {
			if err := tx.QueryRow(r.Context(), `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,$3,$4,true) RETURNING id`, household, user, employer, normalized).Scan(&salarySource); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), `UPDATE salary_source SET is_primary=false,updated_at=now() WHERE household_id=$1 AND active AND id<>$2`, household, salarySource); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE salary_source SET is_primary=true,updated_at=now() WHERE id=$1`, salarySource); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,currency,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$4::date,$5,'IDR',$6,'CONFIRMED',$7) ON CONFLICT (salary_source_id,payroll_period) DO NOTHING`, salarySource, household, period+"-01", at, amount, transaction, source); err != nil {
		return err
	}
	return nil
}
