package review

import (
	"context"
	"encoding/json"
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
	AllowedActions []string  `json:"allowedActions"`
	CreatedAt      time.Time `json:"createdAt"`
}

func (h *Handler) canonicalOpenItems(ctx context.Context, household string) ([]canonicalReview, error) {
	rows, err := h.pool.Query(ctx, `SELECT ri.id,ri.review_type,ri.status,CASE WHEN ri.proposal_id IS NOT NULL THEN 'proposal' WHEN ri.source_event_id IS NOT NULL THEN 'source_event' ELSE 'document' END,COALESCE(ri.proposal_id,ri.source_event_id,ri.document_id)::text,COALESCE(p.description,p.counterparty_raw,'Bukti keuangan perlu ditinjau'),ri.created_at FROM review_item ri LEFT JOIN transaction_proposal p ON p.id=ri.proposal_id WHERE ri.household_id=$1 AND ri.status IN ('PENDING_SEND','OPEN') AND ri.transaction_id IS NULL ORDER BY ri.created_at DESC`, household)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]canonicalReview, 0)
	for rows.Next() {
		var v canonicalReview
		if err := rows.Scan(&v.ID, &v.ReviewType, &v.Status, &v.SubjectType, &v.SubjectID, &v.Summary, &v.CreatedAt); err != nil {
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
	return []string{"CONFIRM", "IGNORE"}
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
	var proposal, source, document, transaction *string
	err = tx.QueryRow(r.Context(), `SELECT review_type,status,proposal_id,source_event_id,document_id,transaction_id FROM review_item WHERE id=$1 AND household_id=$2 FOR UPDATE`, r.PathValue("id"), household).Scan(&kind, &status, &proposal, &source, &document, &transaction)
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
	if in.Action == "IGNORE" {
		_, err = tx.Exec(r.Context(), `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id=$1; UPDATE source_event SET processing_status='IGNORED' WHERE id=$2; UPDATE document SET status='NEEDS_REVIEW',updated_at=now() WHERE id=$3`, proposal, source, document)
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
	_, err = tx.Exec(r.Context(), `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now() WHERE id=$1; UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('PENDING_SEND','OPEN')`, r.PathValue("id"), p.UserID, in.Action, string(in.Values))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to finalize review"})
		return
	}
	if audit(r.Context(), tx, household, p.UserID, "RESOLVE_REVIEW", r.PathValue("id"), map[string]any{"action": in.Action}) != nil || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to audit review resolution"})
		return
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
	if _, err := tx.Exec(r.Context(), `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'PAYSLIP_IMAGE',jsonb_build_object('proposal_id',$3::uuid,'document_id',$4::uuid)); UPDATE transaction_proposal SET proposal_status='ACCEPTED',updated_at=now() WHERE id=$3; UPDATE source_event SET processing_status='PROCESSED' WHERE id=$2; UPDATE document SET status='EXTRACTED',updated_at=now() WHERE id=$4`, transaction, source, proposal, document); err != nil {
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
	if _, err := tx.Exec(r.Context(), `UPDATE salary_source SET is_primary=false,updated_at=now() WHERE household_id=$1 AND active AND id<>$2; UPDATE salary_source SET is_primary=true,updated_at=now() WHERE id=$2; INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,currency,transaction_id,status,source_event_id) VALUES($1,$3,$4::date,$5::date,$6,'IDR',$7,'CONFIRMED',$8) ON CONFLICT (salary_source_id,payroll_period) DO NOTHING`, household, salarySource, household, period+"-01", at, amount, transaction, source); err != nil {
		return err
	}
	return nil
}
