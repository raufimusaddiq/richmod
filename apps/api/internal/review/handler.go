package review

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

type candidate struct {
	ID            string    `json:"id"`
	Amount        string    `json:"amount"`
	TransactionAt time.Time `json:"transactionAt"`
	Description   *string   `json:"description"`
	Score         float64   `json:"score"`
}

type item struct {
	ID            string      `json:"id"`
	Type          string      `json:"type"`
	Amount        string      `json:"amount"`
	Currency      string      `json:"currency"`
	TransactionAt time.Time   `json:"transactionAt"`
	Description   *string     `json:"description"`
	Note          *string     `json:"note"`
	CategoryID    *string     `json:"categoryId"`
	AccountID     *string     `json:"accountId"`
	CategoryName  *string     `json:"categoryName"`
	MerchantName  *string     `json:"merchantName"`
	Counterparty  *string     `json:"counterparty"`
	SourceType    *string     `json:"sourceType"`
	Confidence    *string     `json:"confidence"`
	Reason        string      `json:"reason"`
	Candidates    []candidate `json:"candidates"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT t.id,t.type,t.amount::text,t.currency,t.transaction_at,t.description,t.note,
		       t.category_id,t.account_id,c.name,m.normalized_name,t.counterparty_name,s.source_type,p.confidence::text
		FROM transaction t
		LEFT JOIN category c ON c.id=t.category_id
		LEFT JOIN merchant m ON m.id=t.merchant_id
		LEFT JOIN LATERAL (
			SELECT te.source_event_id FROM transaction_evidence te
			WHERE te.transaction_id=t.id ORDER BY te.created_at LIMIT 1
		) evidence ON true
		LEFT JOIN source_event s ON s.id=evidence.source_event_id
		LEFT JOIN transaction_proposal p ON p.source_event_id=s.id
		WHERE t.household_id=$1 AND t.status='NEEDS_REVIEW'
		ORDER BY t.transaction_at DESC,t.id DESC LIMIT 100`, household)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list reviews"})
		return
	}
	defer rows.Close()
	items := make([]item, 0)
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.ID, &value.Type, &value.Amount, &value.Currency, &value.TransactionAt, &value.Description, &value.Note, &value.CategoryID, &value.AccountID, &value.CategoryName, &value.MerchantName, &value.Counterparty, &value.SourceType, &value.Confidence); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list reviews"})
			return
		}
		value.Candidates, err = h.candidates(r.Context(), household, value)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to generate reconciliation candidates"})
			return
		}
		value.Reason = reviewReason(value)
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list reviews"})
		return
	}
	writeJSON(w, 200, items)
}

func (h *Handler) candidates(ctx context.Context, household string, source item) ([]candidate, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT t.id,t.amount::text,t.transaction_at,t.description,t.account_id,t.category_id,
		       COALESCE(m.normalized_name,t.counterparty_name,'')
		FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id
		WHERE t.household_id=$1 AND t.id<>$2 AND t.status='CONFIRMED'
		  AND t.type=$3 AND t.currency=$4 AND t.amount=$5::numeric
		  AND t.transaction_at BETWEEN $6::timestamptz-interval '72 hours' AND $6::timestamptz+interval '72 hours'
		ORDER BY abs(extract(epoch FROM (t.transaction_at-$6::timestamptz))) LIMIT 5`,
		household, source.ID, source.Type, source.Currency, source.Amount, source.TransactionAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]candidate, 0)
	for rows.Next() {
		var value candidate
		var accountID, categoryID *string
		var merchant string
		if err := rows.Scan(&value.ID, &value.Amount, &value.TransactionAt, &value.Description, &accountID, &categoryID, &merchant); err != nil {
			return nil, err
		}
		hours := math.Abs(value.TransactionAt.Sub(source.TransactionAt).Hours())
		accountMatch := source.AccountID != nil && accountID != nil && *source.AccountID == *accountID
		value.Score = reconciliationScore(hours, sameText(merchant, firstText(source.MerchantName, source.Counterparty)), accountMatch, source.CategoryID != nil && categoryID != nil && *source.CategoryID == *categoryID)
		if value.Score >= 0.70 {
			result = append(result, value)
		}
	}
	return result, rows.Err()
}

func reconciliationScore(hours float64, merchantMatch, accountHint, categoryMatch bool) float64 {
	score := 0.45
	switch {
	case hours <= 1:
		score += 0.20
	case hours <= 24:
		score += 0.15
	default:
		score += 0.05
	}
	if merchantMatch {
		score += 0.20
	}
	if accountHint {
		score += 0.10
	}
	if categoryMatch {
		score += 0.05
	}
	return math.Round(score*100) / 100
}

func reviewReason(value item) string {
	if len(value.Candidates) > 0 {
		return "POSSIBLE_DUPLICATE"
	}
	if value.SourceType != nil && *value.SourceType == "BANK_EMAIL" && value.MerchantName == nil {
		return "UNKNOWN_MERCHANT"
	}
	if value.Type == "EXPENSE" && value.CategoryID == nil {
		return "AMBIGUOUS_CATEGORY"
	}
	return "UNKNOWN_PURPOSE"
}

type confirmInput struct {
	CategoryID  *string `json:"categoryId"`
	Description *string `json:"description"`
	Note        *string `json:"note"`
}

func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	var input confirmInput
	if err := decodeOptionalJSON(r, &input); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid review confirmation"})
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to confirm review"})
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	var kind string
	var currentCategory, merchantID *string
	if err := tx.QueryRow(r.Context(), `SELECT type,category_id,merchant_id FROM transaction WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW' FOR UPDATE`, id, household).Scan(&kind, &currentCategory, &merchantID); errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "review not found"})
		return
	} else if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to confirm review"})
		return
	}
	categoryID := currentCategory
	if input.CategoryID != nil {
		var valid string
		if err := tx.QueryRow(r.Context(), `SELECT id FROM category WHERE id=$1 AND household_id=$2 AND active`, *input.CategoryID, household).Scan(&valid); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid household category"})
			return
		}
		categoryID = &valid
	}
	if kind == "EXPENSE" && categoryID == nil {
		writeJSON(w, 400, map[string]string{"error": "expense category is required"})
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE transaction SET status='CONFIRMED',category_id=$2,description=COALESCE(NULLIF($3,''),description),note=COALESCE(NULLIF($4,''),note),confirmed_at=now(),voided_at=NULL,updated_at=now() WHERE id=$1`, id, categoryID, clean(input.Description, 500), clean(input.Note, 1000)); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to confirm review"})
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE transaction_proposal SET proposal_status='ACCEPTED',category_candidate_id=$2,updated_at=now() WHERE source_event_id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)`, id, categoryID); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to confirm review"})
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE source_event SET processing_status='PROCESSED' WHERE id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)`, id); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to confirm review"})
		return
	}
	if merchantID != nil && categoryID != nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO merchant_alias (household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) SELECT $1,normalized_name,id,$3,true,true FROM merchant WHERE id=$2 ON CONFLICT (household_id,raw_name) DO UPDATE SET default_category_id=excluded.default_category_id,auto_apply=true,created_from_user_confirmation=true`, household, merchantID, categoryID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to learn merchant category"})
			return
		}
	}
	if err := audit(r.Context(), tx, household, p.UserID, "CONFIRM_REVIEW", id, map[string]any{"category_id": categoryID}); err != nil || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to confirm review"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to reject review"})
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	result, err := tx.Exec(r.Context(), `UPDATE transaction SET status='VOIDED',confirmed_at=NULL,voided_at=now(),updated_at=now() WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW'`, id, household)
	if err != nil || result.RowsAffected() != 1 {
		writeJSON(w, 404, map[string]string{"error": "review not found"})
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE source_event_id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1); UPDATE source_event SET processing_status='IGNORED' WHERE id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)`, id); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to reject review"})
		return
	}
	if err := audit(r.Context(), tx, household, p.UserID, "REJECT_REVIEW", id, nil); err != nil || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to reject review"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type mergeInput struct {
	TargetTransactionID string `json:"targetTransactionId"`
}

func (h *Handler) Merge(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	var input mergeInput
	if decodeJSON(r, &input) != nil || input.TargetTransactionID == "" || input.TargetTransactionID == r.PathValue("id") {
		writeJSON(w, 400, map[string]string{"error": "invalid merge target"})
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to merge review"})
		return
	}
	defer tx.Rollback(r.Context())
	var sourceType, sourceAmount, sourceCurrency string
	var sourceTime time.Time
	if err := tx.QueryRow(r.Context(), `SELECT type,amount::text,currency,transaction_at FROM transaction WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW' FOR UPDATE`, r.PathValue("id"), household).Scan(&sourceType, &sourceAmount, &sourceCurrency, &sourceTime); err != nil {
		writeJSON(w, 404, map[string]string{"error": "review not found"})
		return
	}
	var targetType, targetAmount, targetCurrency string
	var targetTime time.Time
	if err := tx.QueryRow(r.Context(), `SELECT type,amount::text,currency,transaction_at FROM transaction WHERE id=$1 AND household_id=$2 AND status='CONFIRMED' FOR UPDATE`, input.TargetTransactionID, household).Scan(&targetType, &targetAmount, &targetCurrency, &targetTime); err != nil {
		writeJSON(w, 400, map[string]string{"error": "merge target is not a confirmed household transaction"})
		return
	}
	if sourceType != targetType || sourceAmount != targetAmount || sourceCurrency != targetCurrency || math.Abs(targetTime.Sub(sourceTime).Hours()) > 72 {
		writeJSON(w, 409, map[string]string{"error": "transactions do not satisfy deterministic merge rules"})
		return
	}
	var mergeID string
	if err := tx.QueryRow(r.Context(), `INSERT INTO reconciliation_merge (household_id,source_transaction_id,target_transaction_id,status,created_by_user_id) VALUES ($1,$2,$3,'ACTIVE',$4) RETURNING id`, household, r.PathValue("id"), input.TargetTransactionID, p.UserID).Scan(&mergeID); err != nil {
		writeJSON(w, 409, map[string]string{"error": "review is already merged"})
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT id,source_event_id,evidence_type,confidence,metadata_json FROM transaction_evidence WHERE transaction_id=$1`, r.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to merge evidence"})
		return
	}
	type evidenceCopy struct {
		originalID, sourceEventID, evidenceType string
		confidence                              *string
		metadata                                json.RawMessage
	}
	var evidence []evidenceCopy
	for rows.Next() {
		var value evidenceCopy
		if err := rows.Scan(&value.originalID, &value.sourceEventID, &value.evidenceType, &value.confidence, &value.metadata); err != nil {
			rows.Close()
			writeJSON(w, 500, map[string]string{"error": "unable to merge evidence"})
			return
		}
		evidence = append(evidence, value)
	}
	rows.Close()
	for _, value := range evidence {
		var copiedID string
		err := tx.QueryRow(r.Context(), `INSERT INTO transaction_evidence (transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (transaction_id,source_event_id) DO NOTHING RETURNING id`, input.TargetTransactionID, value.sourceEventID, value.evidenceType, value.confidence, value.metadata).Scan(&copiedID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			rows.Close()
			writeJSON(w, 500, map[string]string{"error": "unable to merge evidence"})
			return
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO reconciliation_merge_evidence (merge_id,original_evidence_id,copied_evidence_id) VALUES ($1,$2,$3)`, mergeID, value.originalID, copiedID); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to merge evidence"})
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `UPDATE transaction SET status='VOIDED',confirmed_at=NULL,voided_at=now(),updated_at=now() WHERE id=$1; UPDATE transaction_proposal SET proposal_status='MERGED',metadata_json=metadata_json||jsonb_build_object('merged_into',$2::uuid),updated_at=now() WHERE source_event_id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)`, r.PathValue("id"), input.TargetTransactionID); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to merge review"})
		return
	}
	if err := audit(r.Context(), tx, household, p.UserID, "MERGE_REVIEW", r.PathValue("id"), map[string]any{"target_transaction_id": input.TargetTransactionID, "merge_id": mergeID}); err != nil || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to merge review"})
		return
	}
	writeJSON(w, 200, map[string]string{"mergeId": mergeID})
}

func (h *Handler) Unmerge(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to reverse merge"})
		return
	}
	defer tx.Rollback(r.Context())
	var sourceID string
	mergeID := r.PathValue("id")
	if err := tx.QueryRow(r.Context(), `UPDATE reconciliation_merge SET status='REVERSED',reversed_at=now() WHERE id=$1 AND household_id=$2 AND status='ACTIVE' RETURNING source_transaction_id`, mergeID, household).Scan(&sourceID); err != nil {
		writeJSON(w, 404, map[string]string{"error": "active merge not found"})
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM transaction_evidence WHERE id IN (SELECT copied_evidence_id FROM reconciliation_merge_evidence WHERE merge_id=$1); UPDATE transaction SET status='NEEDS_REVIEW',confirmed_at=NULL,voided_at=NULL,updated_at=now() WHERE id=$2; UPDATE transaction_proposal SET proposal_status='NEEDS_REVIEW',metadata_json=metadata_json-'merged_into',updated_at=now() WHERE source_event_id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$2)`, mergeID, sourceID); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to reverse merge"})
		return
	}
	if err := audit(r.Context(), tx, household, p.UserID, "REVERSE_MERGE", sourceID, map[string]any{"merge_id": mergeID}); err != nil || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to reverse merge"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func audit(ctx context.Context, tx pgx.Tx, household, user, action, entity string, after any) error {
	raw, _ := json.Marshal(after)
	_, err := tx.Exec(ctx, `INSERT INTO audit_log (household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES ($1,'USER',$2,$3,'transaction',$4,$5::jsonb)`, household, user, action, entity, string(raw))
	return err
}

func principalHousehold(w http.ResponseWriter, r *http.Request) (auth.Principal, string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return auth.Principal{}, "", false
	}
	return p, p.Memberships[0].HouseholdID, true
}

func decodeJSON(r *http.Request, output any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(output)
}

func decodeOptionalJSON(r *http.Request, output any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return decodeJSON(r, output)
}

func clean(value *string, limit int) string {
	if value == nil {
		return ""
	}
	runes := []rune(strings.TrimSpace(*value))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func sameText(left, right string) bool {
	return left != "" && strings.EqualFold(strings.Join(strings.Fields(left), " "), strings.Join(strings.Fields(right), " "))
}

func firstText(values ...*string) string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return *value
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, output any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(output)
}
