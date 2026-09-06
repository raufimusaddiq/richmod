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
	FundingAccountHint      string                    `json:"fundingAccountHint,omitempty"`
	ProviderAccountHint     string                    `json:"providerAccountHint,omitempty"`
	TransferCandidates      []transferReviewCandidate `json:"transferCandidates,omitempty"`
	ProposedPurpose         string                    `json:"proposedPurpose,omitempty"`
	ProposedWealthAccountID string                    `json:"proposedWealthAccountId,omitempty"`
	AllowedActions          []string                  `json:"allowedActions"`
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
	rows, err := h.pool.Query(ctx, `SELECT ri.id,ri.review_type,ri.status,CASE WHEN ri.proposal_id IS NOT NULL THEN 'proposal' WHEN ri.source_event_id IS NOT NULL THEN 'source_event' WHEN ri.document_id IS NOT NULL THEN 'document' WHEN ri.wealth_observation_id IS NOT NULL THEN 'wealth_observation' ELSE 'cycle_residual_case' END,COALESCE(ri.proposal_id,ri.source_event_id,ri.document_id,ri.wealth_observation_id,ri.cycle_residual_case_id)::text,COALESCE(p.description,p.counterparty_raw,be.output_json->>'description',be.output_json->>'merchant',be.output_json->>'counterparty',CASE WHEN wo.id IS NOT NULL THEN 'Konfirmasi nilai Wealth dari dokumen' WHEN ri.review_type='FINANCIAL_EMAIL_RESOLUTION' THEN 'Pilih rekening untuk bukti email finansial' WHEN ri.cycle_residual_case_id IS NOT NULL THEN 'Sisa salary cycle perlu direkonsiliasi' END,'Bukti keuangan perlu ditinjau'),COALESCE(be.output_json->>'amount_idr',wo.observed_value_idr::text,crc.basis_residual_idr::text,trc.amount_idr::text,(SELECT facts_json->>'amount_idr' FROM financial_email_observation WHERE source_event_id=ri.source_event_id AND status='REVIEW' ORDER BY ordinal LIMIT 1),''),COALESCE(be.output_json->>'channel',''),COALESCE(crc.cycle_start::text,''),COALESCE(crc.cycle_end::text,''),COALESCE(wo.id::text,''),COALESCE(wo.resolved_wealth_account_id::text,''),COALESCE(wo.institution,''),COALESCE(wo.account_hint,''),COALESCE((SELECT id::text FROM financial_email_observation WHERE source_event_id=ri.source_event_id AND status='REVIEW' ORDER BY ordinal LIMIT 1),''),COALESCE((SELECT facts_json->>'funding_account_hint' FROM financial_email_observation WHERE source_event_id=ri.source_event_id AND status='REVIEW' ORDER BY ordinal LIMIT 1),''),COALESCE((SELECT facts_json->>'provider_account_hint' FROM financial_email_observation WHERE source_event_id=ri.source_event_id AND status='REVIEW' ORDER BY ordinal LIMIT 1),''),COALESCE(trc.proposed_purpose,''),COALESCE(trc.proposed_wealth_account_id::text,''),COALESCE((SELECT jsonb_agg(jsonb_build_object('id',t.id,'type',t.type,'status',t.status,'amount',t.amount::text,'transactionAt',t.transaction_at,'description',t.description,'purpose',COALESCE(t.purpose,''),'wealthAccountId',COALESCE(t.related_wealth_account_id::text,'')) ORDER BY t.transaction_at,t.id) FROM transaction t WHERE t.id=ANY(trc.candidate_transaction_ids)),'[]'::jsonb),ri.created_at FROM review_item ri LEFT JOIN transaction_proposal p ON p.id=ri.proposal_id LEFT JOIN bank_email_extraction be ON be.source_event_id=ri.source_event_id LEFT JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id LEFT JOIN wealth_observation wo ON wo.id=ri.wealth_observation_id LEFT JOIN transfer_reconciliation_case trc ON trc.source_event_id=ri.source_event_id WHERE ri.household_id=$1 AND ri.status IN ('PENDING_SEND','OPEN') AND ri.transaction_id IS NULL ORDER BY ri.created_at DESC`, household)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]canonicalReview, 0)
	for rows.Next() {
		var v canonicalReview
		var candidatesJSON []byte
		if err := rows.Scan(&v.ID, &v.ReviewType, &v.Status, &v.SubjectType, &v.SubjectID, &v.Summary, &v.AmountIDR, &v.Channel, &v.CycleStart, &v.CycleEnd, &v.WealthObservationID, &v.ResolvedWealthAccountID, &v.Institution, &v.AccountHint, &v.FinancialObservationID, &v.FundingAccountHint, &v.ProviderAccountHint, &v.ProposedPurpose, &v.ProposedWealthAccountID, &candidatesJSON, &v.CreatedAt); err != nil {
			return nil, err
		}
		if len(candidatesJSON) > 0 && string(candidatesJSON) != "null" && json.Unmarshal(candidatesJSON, &v.TransferCandidates) != nil {
			return nil, errors.New("invalid transfer reconciliation candidates")
		}
		v.AllowedActions = canonicalActions(v.ReviewType)
		if v.ReviewType == "TRANSFER_CLASSIFICATION" && (len(v.TransferCandidates) > 0 || v.ProposedPurpose != "") {
			v.AllowedActions = []string{"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"}
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
	var proposal, source, document, transaction, residualCase, wealthObservation *string
	err = tx.QueryRow(r.Context(), `SELECT review_type,status,proposal_id,source_event_id,document_id,transaction_id,cycle_residual_case_id,wealth_observation_id FROM review_item WHERE id=$1 AND household_id=$2 FOR UPDATE`, r.PathValue("id"), household).Scan(&kind, &status, &proposal, &source, &document, &transaction, &residualCase, &wealthObservation)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "review not found"})
		return
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		writeJSON(w, 409, map[string]string{"error": "review is already resolved"})
		return
	}
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
		var values struct {
			AccountID       string `json:"accountId"`
			WealthAccountID string `json:"wealthAccountId"`
		}
		if json.Unmarshal(in.Values, &values) != nil || values.AccountID == "" || values.WealthAccountID == "" {
			writeJSON(w, 400, map[string]string{"error": "account and Wealth Account are required"})
			return
		}
		var valid bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM account WHERE id=$1 AND household_id=$3 AND active) AND EXISTS(SELECT 1 FROM wealth_account WHERE id=$2 AND household_id=$3 AND active)`, values.AccountID, values.WealthAccountID, household).Scan(&valid); err != nil || !valid {
			writeJSON(w, 400, map[string]string{"error": "invalid household financial entities"})
			return
		}
		var observationID, fundingHint, providerHint string
		if err = tx.QueryRow(r.Context(), `SELECT id::text,COALESCE(facts_json->>'funding_account_hint',''),COALESCE(facts_json->>'provider_account_hint','') FROM financial_email_observation WHERE source_event_id=$1 AND household_id=$2 AND status='REVIEW' ORDER BY ordinal LIMIT 1 FOR UPDATE`, *source, household).Scan(&observationID, &fundingHint, &providerHint); err != nil {
			writeJSON(w, 409, map[string]string{"error": "financial observation is unavailable"})
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE financial_email_observation SET resolved_account_id=$2,resolved_wealth_account_id=$3,status='PENDING',updated_at=now() WHERE id=$1`, observationID, values.AccountID, values.WealthAccountID); err == nil {
			err = learnEntityAlias(r.Context(), tx, household, "ACCOUNT", values.AccountID, fundingHint)
		}
		if err == nil {
			err = learnEntityAlias(r.Context(), tx, household, "WEALTH_ACCOUNT", values.WealthAccountID, providerHint)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='SET_FINANCIAL_EMAIL_ENTITIES',resolution_values=$3::jsonb,updated_at=now() WHERE id=$1`, r.PathValue("id"), p.UserID, string(in.Values))
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
		if err = h.resolveTransferReconciliation(r, tx, p.UserID, household, r.PathValue("id"), *source, in.Action, in.Values); err != nil {
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
		if wealthObservation != nil {
			if _, err = tx.Exec(r.Context(), `UPDATE wealth_observation SET status='DISMISSED',updated_at=now() WHERE id=$1 AND household_id=$2`, *wealthObservation, household); err != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to dismiss wealth observation"})
				return
			}
		}
		if proposal != nil {
			_, err = tx.Exec(r.Context(), `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id=$1`, *proposal)
		}
		if err == nil && source != nil {
			_, err = tx.Exec(r.Context(), `UPDATE source_event SET processing_status='IGNORED' WHERE id=$1`, *source)
		}
		if err == nil && document != nil {
			_, err = tx.Exec(r.Context(), `UPDATE document SET status='NEEDS_REVIEW',updated_at=now() WHERE id=$1`, *document)
		}
		if err == nil && source != nil {
			_, err = tx.Exec(r.Context(), `UPDATE transfer_reconciliation_case SET status='DISMISSED',resolved_at=now(),resolved_by_user_id=$2,updated_at=now() WHERE source_event_id=$1 AND status='OPEN'`, *source, p.UserID)
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
				_, err = tx.Exec(r.Context(), `UPDATE transaction_proposal SET transaction_at=$2::date,updated_at=now() WHERE id=$1`, *proposal, date)
				if err == nil {
					err = h.resolvePayslip(r, tx, household, p.UserID, *proposal, *source, *document, strings.ToUpper(v.Choice))
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
	if in.Action == "PRIMARY_SALARY" && source != nil {
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
	if entityType == "ACCOUNT" {
		_, err := tx.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,account_id,alias,normalized_alias,source) VALUES($1,'ACCOUNT',$2,$3,$4,'REVIEW_LEARNED') ON CONFLICT (household_id,entity_type,normalized_alias) WHERE active DO UPDATE SET account_id=EXCLUDED.account_id,wealth_account_id=NULL,alias=EXCLUDED.alias,source='REVIEW_LEARNED',updated_at=now()`, household, entityID, strings.TrimSpace(alias), normalized)
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,wealth_account_id,alias,normalized_alias,source) VALUES($1,'WEALTH_ACCOUNT',$2,$3,$4,'REVIEW_LEARNED') ON CONFLICT (household_id,entity_type,normalized_alias) WHERE active DO UPDATE SET wealth_account_id=EXCLUDED.wealth_account_id,account_id=NULL,alias=EXCLUDED.alias,source='REVIEW_LEARNED',updated_at=now()`, household, entityID, strings.TrimSpace(alias), normalized)
	return err
}

func (h *Handler) resolveTransferReconciliation(r *http.Request, tx pgx.Tx, user, household, reviewID, sourceID, action string, raw json.RawMessage) error {
	var accountID, amount, description, purpose, wealthID string
	var at time.Time
	var candidates []string
	if err := tx.QueryRow(r.Context(), `SELECT account_id::text,amount_idr::text,COALESCE(description,''),proposed_purpose,COALESCE(proposed_wealth_account_id::text,''),transaction_at,candidate_transaction_ids FROM transfer_reconciliation_case WHERE household_id=$1 AND source_event_id=$2 AND status='OPEN' FOR UPDATE`, household, sourceID).Scan(&accountID, &amount, &description, &purpose, &wealthID, &at, &candidates); err != nil {
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
			if err := finalizeTransferReviewLifecycle(r.Context(), tx, household, user, transactionID, "TRANSFER", "ACCEPTED", "PROCESSED", nil, "TRANSFER_RECONCILED", "TRANSFER_RECONCILED"); err != nil {
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
	if _, err := tx.Exec(r.Context(), `UPDATE source_event SET processing_status='PROCESSED',parser_name=CASE WHEN source_type='FINANCIAL_EMAIL' THEN 'financial-email-reconciliation' ELSE 'telegram-transfer' END,parser_version='1' WHERE id=$1 AND household_id=$2`, sourceID, household); err != nil {
		return err
	}
	_, err := tx.Exec(r.Context(), `UPDATE transfer_reconciliation_case SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,updated_at=now() WHERE source_event_id=$1`, sourceID, user)
	return err
}

var errInvalid = &reviewResolutionError{}

type reviewResolutionError struct{}

func (*reviewResolutionError) Error() string { return "invalid resolution" }
func (h *Handler) resolvePayslip(r *http.Request, tx pgx.Tx, household, user, proposal, source, document, choice string) error {
	if choice != "PRIMARY_SALARY" && choice != "ORDINARY_INCOME" {
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
	if _, err := tx.Exec(r.Context(), `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,currency,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$4::date,$5,'IDR',$6,'CONFIRMED',$7) ON CONFLICT (salary_source_id,payroll_period) DO NOTHING`, salarySource, household, period+"-01", at, amount, transaction, source); err != nil {
		return err
	}
	return nil
}
