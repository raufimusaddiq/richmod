package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
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
	Counterparty  *string    `json:"counterpartyName"`
	AccountID     *string    `json:"accountId"`
	CategoryID    *string    `json:"categoryId"`
	MerchantID    *string    `json:"merchantId"`
	CategoryName  *string    `json:"categoryName"`
	MerchantName  *string    `json:"merchantName"`
	AccountName   *string    `json:"accountName"`
	MemberName    *string    `json:"memberName"`
	SourceType    *string    `json:"sourceType"`
	ConfirmedAt   *time.Time `json:"confirmedAt"`
	VoidedAt      *time.Time `json:"voidedAt"`
}

func (h *Handler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	_ = p
	filters, err := transactionFiltersFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT t.id,t.type,t.status,t.amount::text,t.currency,t.transaction_at,t.description,t.note,t.counterparty_name,
		       t.account_id,t.category_id,t.merchant_id,c.name,m.normalized_name,a.name,u.display_name,e.source_type,
		       t.confirmed_at,t.voided_at
		FROM transaction t
		LEFT JOIN category c ON c.id=t.category_id
		LEFT JOIN merchant m ON m.id=t.merchant_id
		LEFT JOIN account a ON a.id=t.account_id
		LEFT JOIN "user" u ON u.id=t.created_by_user_id
		LEFT JOIN LATERAL (
			SELECT s.source_type FROM transaction_evidence te JOIN source_event s ON s.id=te.source_event_id
			WHERE te.transaction_id=t.id ORDER BY te.created_at LIMIT 1
		) e ON true
		WHERE t.household_id=$1
		  AND ($2::timestamptz IS NULL OR t.transaction_at >= $2)
		  AND ($3::timestamptz IS NULL OR t.transaction_at < $3)
		  AND ($4='' OR t.type=$4)
		  AND ($5='' OR t.category_id::text=$5)
		  AND ($6='' OR t.created_by_user_id::text=$6)
		  AND ($7='' OR t.status=$7)
		  AND ($8='' OR t.account_id::text=$8)
		  AND ($9='' OR EXISTS(SELECT 1 FROM transaction_evidence te2 JOIN source_event s2 ON s2.id=te2.source_event_id WHERE te2.transaction_id=t.id AND s2.source_type=$9))
		  AND ($10='' OR concat_ws(' ',t.description,t.note,t.counterparty_name,m.normalized_name) ILIKE '%'||$10||'%')
		ORDER BY t.transaction_at DESC,t.id DESC LIMIT 250`, household, filters.Start, filters.End, filters.Type, filters.CategoryID, filters.MemberID, filters.Status, filters.AccountID, filters.Source, filters.Search)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list transactions"})
		return
	}
	defer rows.Close()
	out := make([]transactionView, 0)
	for rows.Next() {
		var v transactionView
		if err := rows.Scan(&v.ID, &v.Type, &v.Status, &v.Amount, &v.Currency, &v.TransactionAt, &v.Description, &v.Note, &v.Counterparty, &v.AccountID, &v.CategoryID, &v.MerchantID, &v.CategoryName, &v.MerchantName, &v.AccountName, &v.MemberName, &v.SourceType, &v.ConfirmedAt, &v.VoidedAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list transactions"})
			return
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list transactions"})
		return
	}
	writeJSON(w, 200, out)
}

type transactionFilters struct {
	Start, End                         *time.Time
	Type, CategoryID, MemberID, Status string
	AccountID, Source, Search          string
}

func transactionFiltersFromRequest(r *http.Request) (transactionFilters, error) {
	query := r.URL.Query()
	result := transactionFilters{
		Type: strings.TrimSpace(query.Get("type")), CategoryID: strings.TrimSpace(query.Get("categoryId")),
		MemberID: strings.TrimSpace(query.Get("memberId")), Status: strings.TrimSpace(query.Get("status")),
		AccountID: strings.TrimSpace(query.Get("accountId")), Source: strings.TrimSpace(query.Get("source")),
		Search: strings.TrimSpace(query.Get("q")),
	}
	if len(result.Search) > 100 {
		return result, errors.New("search is too long")
	}
	if result.Type != "" && !oneOf(result.Type, "INCOME", "EXPENSE", "TRANSFER", "REFUND", "ADJUSTMENT", "UNCLASSIFIED") {
		return result, errors.New("invalid transaction type")
	}
	if result.Status != "" && !oneOf(result.Status, "PENDING", "CONFIRMED", "NEEDS_REVIEW", "VOIDED") {
		return result, errors.New("invalid transaction status")
	}
	if result.Source != "" && !oneOf(result.Source, "BANK_EMAIL", "TELEGRAM_TEXT", "TELEGRAM_IMAGE", "WEB_MANUAL", "WEB_IMAGE", "SYSTEM") {
		return result, errors.New("invalid transaction source")
	}
	for key, target := range map[string]**time.Time{"from": &result.Start, "to": &result.End} {
		if value := strings.TrimSpace(query.Get(key)); value != "" {
			parsed, err := time.ParseInLocation("2006-01-02", value, clock.HouseholdLocation())
			if err != nil {
				return result, errors.New("invalid " + key + " date")
			}
			if key == "to" {
				parsed = parsed.AddDate(0, 0, 1)
			}
			*target = &parsed
		}
	}
	if result.Start != nil && result.End != nil && !result.Start.Before(*result.End) {
		return result, errors.New("from date must not be after to date")
	}
	return result, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
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

func (h *Handler) Audit(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,actor_type,action,before_json,after_json,created_at FROM audit_log WHERE household_id=$1 AND entity_type='transaction' AND entity_id=$2 ORDER BY created_at DESC LIMIT 100`, household, r.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load transaction audit"})
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, actor, action string
		var before, after json.RawMessage
		var created time.Time
		if err := rows.Scan(&id, &actor, &action, &before, &after, &created); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to load transaction audit"})
			return
		}
		result = append(result, map[string]any{"id": id, "actorType": actor, "action": action, "before": before, "after": after, "createdAt": created})
	}
	writeJSON(w, 200, result)
}

func (h *Handler) load(ctx context.Context, household, id string) (transactionView, error) {
	var v transactionView
	err := h.pool.QueryRow(ctx, `SELECT t.id,t.type,t.status,t.amount::text,t.currency,t.transaction_at,t.description,t.note,t.counterparty_name,t.account_id,t.category_id,t.merchant_id,c.name,m.normalized_name,a.name,u.display_name,e.source_type,t.confirmed_at,t.voided_at FROM transaction t LEFT JOIN category c ON c.id=t.category_id LEFT JOIN merchant m ON m.id=t.merchant_id LEFT JOIN account a ON a.id=t.account_id LEFT JOIN "user" u ON u.id=t.created_by_user_id LEFT JOIN LATERAL (SELECT s.source_type FROM transaction_evidence te JOIN source_event s ON s.id=te.source_event_id WHERE te.transaction_id=t.id ORDER BY te.created_at LIMIT 1) e ON true WHERE t.id=$1 AND t.household_id=$2`, id, household).Scan(&v.ID, &v.Type, &v.Status, &v.Amount, &v.Currency, &v.TransactionAt, &v.Description, &v.Note, &v.Counterparty, &v.AccountID, &v.CategoryID, &v.MerchantID, &v.CategoryName, &v.MerchantName, &v.AccountName, &v.MemberName, &v.SourceType, &v.ConfirmedAt, &v.VoidedAt)
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
