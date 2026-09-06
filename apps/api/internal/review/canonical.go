package review

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type canonicalReview struct {
	ID             string    `json:"id"`
	ReviewType     string    `json:"reviewType"`
	Status         string    `json:"status"`
	SubjectType    string    `json:"subjectType"`
	SubjectID      string    `json:"subjectId"`
	Summary        string    `json:"summary"`
	AmountIDR      string    `json:"amountIdr,omitempty"`
	Channel        string    `json:"channel,omitempty"`
	AllowedActions []string  `json:"allowedActions"`
	CreatedAt      time.Time `json:"createdAt"`
}

func (h *Handler) canonicalOpenItems(ctx context.Context, household string) ([]canonicalReview, error) {
	rows, err := h.pool.Query(ctx, `SELECT ri.id,ri.review_type,ri.status,CASE WHEN ri.proposal_id IS NOT NULL THEN 'proposal' WHEN ri.source_event_id IS NOT NULL THEN 'source_event' WHEN ri.document_id IS NOT NULL THEN 'document' ELSE 'cycle_residual_case' END,COALESCE(ri.proposal_id,ri.source_event_id,ri.document_id,ri.cycle_residual_case_id)::text,COALESCE(p.description,p.counterparty_raw,be.output_json->>'description',be.output_json->>'merchant',be.output_json->>'counterparty',CASE WHEN ri.cycle_residual_case_id IS NOT NULL THEN 'Sisa salary cycle perlu direkonsiliasi' END,'Bukti keuangan perlu ditinjau'),COALESCE(be.output_json->>'amount_idr',crc.basis_residual_idr::text,''),COALESCE(be.output_json->>'channel',''),ri.created_at FROM review_item ri LEFT JOIN transaction_proposal p ON p.id=ri.proposal_id LEFT JOIN bank_email_extraction be ON be.source_event_id=ri.source_event_id LEFT JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id WHERE ri.household_id=$1 AND ri.status IN ('PENDING_SEND','OPEN') AND ri.transaction_id IS NULL ORDER BY ri.created_at DESC`, household)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]canonicalReview, 0)
	for rows.Next() {
		var v canonicalReview
		if err := rows.Scan(&v.ID, &v.ReviewType, &v.Status, &v.SubjectType, &v.SubjectID, &v.Summary, &v.AmountIDR, &v.Channel, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.AllowedActions = canonicalActions(v.ReviewType)
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
	var proposal, source, document, transaction, residualCase *string
	err = tx.QueryRow(r.Context(), `SELECT review_type,status,proposal_id,source_event_id,document_id,transaction_id,cycle_residual_case_id FROM review_item WHERE id=$1 AND household_id=$2 FOR UPDATE`, r.PathValue("id"), household).Scan(&kind, &status, &proposal, &source, &document, &transaction, &residualCase)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "review not found"})
		return
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		writeJSON(w, 409, map[string]string{"error": "review is already resolved"})
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
			in.Action = "LEAVE_UNALLOCATED"
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
		if proposal != nil {
			_, err = tx.Exec(r.Context(), `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id=$1`, *proposal)
		}
		if err == nil && source != nil {
			_, err = tx.Exec(r.Context(), `UPDATE source_event SET processing_status='IGNORED' WHERE id=$1`, *source)
		}
		if err == nil && document != nil {
			_, err = tx.Exec(r.Context(), `UPDATE document SET status='NEEDS_REVIEW',updated_at=now() WHERE id=$1`, *document)
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
