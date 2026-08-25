package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

type transactionView struct {
	ID            string     `json:"id"`
	Type          string     `json:"type"`
	Status        string     `json:"status"`
	Amount        string     `json:"amount"`
	Currency      string     `json:"currency"`
	TransactionAt time.Time  `json:"transactionAt"`
	Description   *string    `json:"description"`
	Note          *string    `json:"note"`
	AccountID     *string    `json:"accountId"`
	CategoryID    *string    `json:"categoryId"`
	MerchantID    *string    `json:"merchantId"`
	ConfirmedAt   *time.Time `json:"confirmedAt"`
	VoidedAt      *time.Time `json:"voidedAt"`
}

func (h *Handler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	_ = p
	rows, err := h.pool.Query(r.Context(), `SELECT id,type,status,amount::text,currency,transaction_at,description,note,account_id,category_id,merchant_id,confirmed_at,voided_at FROM transaction WHERE household_id=$1 ORDER BY transaction_at DESC,id DESC LIMIT 100`, household)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list transactions"})
		return
	}
	defer rows.Close()
	out := make([]transactionView, 0)
	for rows.Next() {
		var v transactionView
		if err := rows.Scan(&v.ID, &v.Type, &v.Status, &v.Amount, &v.Currency, &v.TransactionAt, &v.Description, &v.Note, &v.AccountID, &v.CategoryID, &v.MerchantID, &v.ConfirmedAt, &v.VoidedAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list transactions"})
			return
		}
		out = append(out, v)
	}
	writeJSON(w, 200, out)
}

func (h *Handler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	v, err := h.load(r.Context(), household, r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "transaction not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load transaction"})
		return
	}
	writeJSON(w, 200, v)
}

func (h *Handler) ConfirmTransaction(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "CONFIRMED")
}
func (h *Handler) VoidTransaction(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "VOIDED")
}

func (h *Handler) transition(w http.ResponseWriter, r *http.Request, target string) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to update transaction"})
		return
	}
	defer tx.Rollback(r.Context())
	var before string
	if err := tx.QueryRow(r.Context(), `SELECT status FROM transaction WHERE id=$1 AND household_id=$2 FOR UPDATE`, id, household).Scan(&before); errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "transaction not found"})
		return
	} else if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to update transaction"})
		return
	}
	if !canTransition(before, target) {
		writeJSON(w, 409, map[string]string{"error": "invalid transaction status transition"})
		return
	}
	if target == "CONFIRMED" {
		_, err = tx.Exec(r.Context(), `UPDATE transaction SET status='CONFIRMED',confirmed_at=now(),voided_at=NULL,updated_at=now() WHERE id=$1`, id)
	} else {
		_, err = tx.Exec(r.Context(), `UPDATE transaction SET status='VOIDED',voided_at=now(),confirmed_at=NULL,updated_at=now() WHERE id=$1`, id)
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to update transaction"})
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,before_json,after_json) VALUES($1,'USER',$2,$3,'transaction',$4,jsonb_build_object('status',$5::text),jsonb_build_object('status',$6::text))`, household, p.UserID, target, id, before, target); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to audit transaction"})
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to update transaction"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func canTransition(from, to string) bool {
	if to == "CONFIRMED" {
		return from == "PENDING" || from == "NEEDS_REVIEW"
	}
	if to == "VOIDED" {
		return from != "VOIDED"
	}
	return false
}

func (h *Handler) Evidence(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT e.id,e.evidence_type,e.confidence,e.metadata_json,s.source_type,s.received_at FROM transaction_evidence e JOIN transaction t ON t.id=e.transaction_id JOIN source_event s ON s.id=e.source_event_id WHERE e.transaction_id=$1 AND t.household_id=$2 ORDER BY e.created_at`, r.PathValue("id"), household)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load evidence"})
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, kind, source string
		var confidence *string
		var metadata json.RawMessage
		var received time.Time
		if rows.Scan(&id, &kind, &confidence, &metadata, &source, &received) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to load evidence"})
			return
		}
		out = append(out, map[string]any{"id": id, "evidenceType": kind, "confidence": confidence, "metadata": metadata, "sourceType": source, "receivedAt": received})
	}
	writeJSON(w, 200, out)
}

func (h *Handler) load(ctx context.Context, household, id string) (transactionView, error) {
	var v transactionView
	err := h.pool.QueryRow(ctx, `SELECT id,type,status,amount::text,currency,transaction_at,description,note,account_id,category_id,merchant_id,confirmed_at,voided_at FROM transaction WHERE id=$1 AND household_id=$2`, id, household).Scan(&v.ID, &v.Type, &v.Status, &v.Amount, &v.Currency, &v.TransactionAt, &v.Description, &v.Note, &v.AccountID, &v.CategoryID, &v.MerchantID, &v.ConfirmedAt, &v.VoidedAt)
	return v, err
}
func principalHousehold(w http.ResponseWriter, r *http.Request) (auth.Principal, string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return auth.Principal{}, "", false
	}
	return p, p.Memberships[0].HouseholdID, true
}
