package ledger

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
	"github.com/raufimusaddiq/richmod/apps/api/internal/platform/httpx"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

type transactionView struct {
	ID                     string     `json:"id"`
	Type                   string     `json:"type"`
	Status                 string     `json:"status"`
	Amount                 string     `json:"amount"`
	Currency               string     `json:"currency"`
	TransactionAt          time.Time  `json:"transactionAt"`
	Description            *string    `json:"description"`
	Note                   *string    `json:"note"`
	Counterparty           *string    `json:"counterpartyName"`
	AccountID              *string    `json:"accountId"`
	CategoryID             *string    `json:"categoryId"`
	MerchantID             *string    `json:"merchantId"`
	CategoryName           *string    `json:"categoryName"`
	MerchantName           *string    `json:"merchantName"`
	AccountName            *string    `json:"accountName"`
	MemberName             *string    `json:"memberName"`
	SourceType             *string    `json:"sourceType"`
	Purpose                string     `json:"purpose"`
	RelatedWealthAccountID *string    `json:"relatedWealthAccountId"`
	WealthAccountName      *string    `json:"wealthAccountName"`
	ConfirmedAt            *time.Time `json:"confirmedAt"`
	VoidedAt               *time.Time `json:"voidedAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}

// activeMergeView is an ACTIVE reconciliation merge this transaction takes part in, with the
// other side described for the drawer's "Pisahkan penggabungan" action. Role TARGET means this
// transaction absorbed the other (voiding it is refused); SOURCE means it was folded into the other.
type activeMergeView struct {
	ID                 string    `json:"id"`
	Role               string    `json:"role"`
	OtherTransactionID string    `json:"otherTransactionId"`
	OtherLabel         string    `json:"otherLabel"`
	OtherAmount        string    `json:"otherAmount"`
	OtherTransactionAt time.Time `json:"otherTransactionAt"`
}

type transactionDetail struct {
	transactionView
	ActiveMerges []activeMergeView `json:"activeMerges"`
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
		WITH filtered AS (
			SELECT t.id,t.type,t.status,t.amount::text,t.currency,t.transaction_at,t.description,t.note,t.counterparty_name,
			       t.account_id,t.category_id,t.merchant_id,c.name,m.normalized_name,a.name,u.display_name,e.source_type,
			       t.confirmed_at,t.voided_at,t.purpose,t.related_wealth_account_id,wa.name,t.updated_at,
			       count(*) OVER() AS total
			FROM transaction t
			LEFT JOIN wealth_account wa ON wa.id=t.related_wealth_account_id
			LEFT JOIN category c ON c.id=t.category_id
			LEFT JOIN merchant m ON m.id=t.merchant_id
			LEFT JOIN account a ON a.id=t.account_id
			LEFT JOIN "user" u ON u.id=t.created_by_user_id
			LEFT JOIN LATERAL (
				SELECT s.source_type FROM transaction_evidence te JOIN source_event s ON s.id=te.source_event_id
				WHERE te.transaction_id=t.id AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE' ORDER BY te.created_at LIMIT 1
			) e ON true
			WHERE t.household_id=$1
			  AND ($2::timestamptz IS NULL OR t.transaction_at >= $2)
			  AND ($3::timestamptz IS NULL OR t.transaction_at < $3)
			  AND ($4='' OR t.type=$4 OR ($4='SPENDING' AND t.type IN ('EXPENSE','REFUND')))
			  AND ($5='' OR t.category_id::text=$5 OR ($5='uncategorized' AND t.category_id IS NULL))
			  AND ($6='' OR t.created_by_user_id::text=$6)
			  AND ($7='' OR t.status=$7)
			  AND ($8='' OR t.account_id::text=$8)
			  AND ($9='' OR EXISTS(SELECT 1 FROM transaction_evidence te2 JOIN source_event s2 ON s2.id=te2.source_event_id WHERE te2.transaction_id=t.id AND COALESCE(te2.metadata_json->>'link_status','ACTIVE')='ACTIVE' AND s2.source_type=$9))
			  AND ($10='' OR concat_ws(' ',t.description,t.note,t.counterparty_name,m.normalized_name) ILIKE '%'||$10||'%')
			  AND ($14='' OR t.merchant_id::text=$14 OR ($14='none' AND t.merchant_id IS NULL AND NULLIF(regexp_replace(btrim(COALESCE(t.counterparty_name,'')),'[[:space:]]+',' ','g'),'') IS NULL) OR (t.merchant_id IS NULL AND $14=(SELECT min(a.normalized_merchant_id::text) FROM merchant_alias a WHERE a.household_id=t.household_id AND lower(regexp_replace(btrim(a.raw_name),'[[:space:]]+',' ','g'))=lower(regexp_replace(btrim(COALESCE(t.counterparty_name,'')),'[[:space:]]+',' ','g')) HAVING count(DISTINCT a.normalized_merchant_id)=1)))
			  AND ($15='' OR t.id::text=$15)
		)
		SELECT * FROM filtered
		WHERE ($12::timestamptz IS NULL OR (transaction_at,id) < ($12,NULLIF($13,'')::uuid))
		ORDER BY transaction_at DESC,id DESC LIMIT $11`, household, filters.Start, filters.End, filters.Type, filters.CategoryID, filters.MemberID, filters.Status, filters.AccountID, filters.Source, filters.Search, filters.Limit+1, filters.CursorAt, filters.CursorID, filters.MerchantID, filters.TransactionID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list transactions"})
		return
	}
	defer rows.Close()
	out := make([]transactionView, 0, filters.Limit)
	var total int64
	for rows.Next() {
		var v transactionView
		if err := rows.Scan(&v.ID, &v.Type, &v.Status, &v.Amount, &v.Currency, &v.TransactionAt, &v.Description, &v.Note, &v.Counterparty, &v.AccountID, &v.CategoryID, &v.MerchantID, &v.CategoryName, &v.MerchantName, &v.AccountName, &v.MemberName, &v.SourceType, &v.ConfirmedAt, &v.VoidedAt, &v.Purpose, &v.RelatedWealthAccountID, &v.WealthAccountName, &v.UpdatedAt, &total); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list transactions"})
			return
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list transactions"})
		return
	}
	// The count of every row matching the filters, before the page limit. The window
	// function rides the same query, so a second COUNT round-trip is not needed.
	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	if len(out) > filters.Limit {
		last := out[filters.Limit-1]
		out = out[:filters.Limit]
		w.Header().Set("X-Next-Cursor", transactionCursor(last))
	}
	writeJSON(w, 200, out)
}

type transactionFilters struct {
	Start, End                         *time.Time
	Type, CategoryID, MemberID, Status string
	AccountID, Source, Search          string
	MerchantID, TransactionID          string
	CursorAt                           *time.Time
	CursorID                           string
	Limit                              int
}

func transactionFiltersFromRequest(r *http.Request) (transactionFilters, error) {
	query := r.URL.Query()
	result := transactionFilters{
		Type: strings.TrimSpace(query.Get("type")), CategoryID: strings.TrimSpace(query.Get("categoryId")),
		MemberID: strings.TrimSpace(query.Get("memberId")), Status: strings.TrimSpace(query.Get("status")),
		AccountID: strings.TrimSpace(query.Get("accountId")), Source: strings.TrimSpace(query.Get("source")),
		MerchantID:    strings.TrimSpace(query.Get("merchantId")),
		TransactionID: strings.TrimSpace(query.Get("id")),
		Search:        strings.TrimSpace(query.Get("q")),
		Limit:         250,
	}
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 250 {
			return result, errors.New("limit must be between 1 and 250")
		}
		result.Limit = limit
	}
	if len(result.Search) > 100 {
		return result, errors.New("search is too long")
	}
	if cursor := strings.TrimSpace(query.Get("cursor")); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return result, errors.New("invalid transaction cursor")
		}
		parts := strings.Split(string(decoded), "|")
		if len(parts) != 2 || parts[1] == "" {
			return result, errors.New("invalid transaction cursor")
		}
		at, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil || uuid.Validate(parts[1]) != nil {
			return result, errors.New("invalid transaction cursor")
		}
		result.CursorAt, result.CursorID = &at, parts[1]
	}
	if result.Type != "" && !oneOf(result.Type, "INCOME", "EXPENSE", "TRANSFER", "REFUND", "ADJUSTMENT", "UNCLASSIFIED", "SPENDING") {
		return result, errors.New("invalid transaction type")
	}
	if result.MerchantID != "" && result.MerchantID != "none" && uuid.Validate(result.MerchantID) != nil {
		return result, errors.New("invalid merchant ID")
	}
	if result.TransactionID != "" && uuid.Validate(result.TransactionID) != nil {
		return result, errors.New("invalid transaction ID")
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

func transactionCursor(value transactionView) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value.TransactionAt.UTC().Format(time.RFC3339Nano) + "|" + value.ID))
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
	// The merge annotation is secondary to the record: if it cannot be read the detail still loads, without
	// the "Pisahkan penggabungan" action, and the void endpoint keeps refusing a merge target on its own.
	merges, err := h.activeMerges(r.Context(), household, v.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "load active merges for transaction detail", "household_id", household, "transaction_id", v.ID, "error", err)
		merges = []activeMergeView{}
	}
	writeJSON(w, 200, transactionDetail{transactionView: v, ActiveMerges: merges})
}

func (h *Handler) activeMerges(ctx context.Context, household, id string) ([]activeMergeView, error) {
	rows, err := h.pool.Query(ctx, `SELECT m.id,CASE WHEN m.target_transaction_id=$1 THEN 'TARGET' ELSE 'SOURCE' END,o.id,COALESCE(mm.normalized_name,NULLIF(o.counterparty_name,''),NULLIF(o.description,''),''),o.amount::text,o.transaction_at
		FROM reconciliation_merge m
		JOIN transaction o ON o.household_id=m.household_id AND o.id=CASE WHEN m.target_transaction_id=$1 THEN m.source_transaction_id ELSE m.target_transaction_id END
		LEFT JOIN merchant mm ON mm.id=o.merchant_id
		WHERE m.household_id=$2 AND m.status='ACTIVE' AND $1 IN (m.source_transaction_id,m.target_transaction_id)
		ORDER BY m.created_at,m.id`, id, household)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	merges := make([]activeMergeView, 0)
	for rows.Next() {
		var v activeMergeView
		if err := rows.Scan(&v.ID, &v.Role, &v.OtherTransactionID, &v.OtherLabel, &v.OtherAmount, &v.OtherTransactionAt); err != nil {
			return nil, err
		}
		merges = append(merges, v)
	}
	return merges, rows.Err()
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
	var reviewOpen bool
	var occurredAt time.Time
	if err := tx.QueryRow(r.Context(), `SELECT t.status,t.transaction_at,EXISTS(SELECT 1 FROM review_item ri WHERE ri.transaction_id=t.id AND ri.household_id=t.household_id AND ri.status IN ('OPEN','PENDING_SEND')) FROM transaction t WHERE t.id=$1 AND t.household_id=$2 FOR UPDATE OF t`, id, household).Scan(&before, &occurredAt, &reviewOpen); errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "transaction not found"})
		return
	} else if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to update transaction"})
		return
	}
	if before == "NEEDS_REVIEW" || reviewOpen {
		writeJSON(w, http.StatusConflict, map[string]string{"code": codeReviewOpen, "error": "resolve this transaction through the review endpoint"})
		return
	}
	if !canTransition(before, target) {
		writeJSON(w, 409, map[string]string{"error": "invalid transaction status transition"})
		return
	}
	// The merge flow locks its target row FOR UPDATE, so this read cannot race a merge being
	// created. The database guard (00100) backs it up for every other void path.
	if target == "VOIDED" {
		protected, err := reviewdomain.ActiveMergeTarget(r.Context(), tx, household, id)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to update transaction"})
			return
		}
		if protected {
			httpx.WriteMergeTargetRefusal(w)
			return
		}
	}
	if target == "CONFIRMED" {
		_, err = tx.Exec(r.Context(), `UPDATE transaction SET status='CONFIRMED',confirmed_at=now(),voided_at=NULL,updated_at=now() WHERE id=$1`, id)
	} else {
		_, err = tx.Exec(r.Context(), `UPDATE transaction SET status='VOIDED',voided_at=now(),confirmed_at=NULL,updated_at=now() WHERE id=$1`, id)
	}
	if errors.Is(reviewdomain.MergeTargetError(err), reviewdomain.ErrMergeTargetActive) {
		httpx.WriteMergeTargetRefusal(w)
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to update transaction"})
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,before_json,after_json) VALUES($1,'USER',$2,$3,'transaction',$4,jsonb_build_object('status',$5::text),jsonb_build_object('status',$6::text))`, household, p.UserID, target, id, before, target); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to audit transaction"})
		return
	}
	// A voided CONFIRMED transaction leaves the totals, so an open cycle residual review
	// built on them is recomputed in the same commit (as a newly recorded one does).
	if target == "VOIDED" && before == "CONFIRMED" {
		if err = reviewdomain.RefreshOpenCycleResidualsFor(r.Context(), tx, household, occurredAt, p.UserID, reviewdomain.CycleReopenTransactionVoided); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to update transaction"})
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to update transaction"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func canTransition(from, to string) bool {
	if to == "CONFIRMED" {
		return from == "PENDING"
	}
	if to == "VOIDED" {
		return from == "PENDING" || from == "CONFIRMED"
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
	err := h.pool.QueryRow(ctx, `SELECT t.id,t.type,t.status,t.amount::text,t.currency,t.transaction_at,t.description,t.note,t.counterparty_name,t.account_id,t.category_id,t.merchant_id,c.name,m.normalized_name,a.name,u.display_name,e.source_type,t.confirmed_at,t.voided_at,t.purpose,t.related_wealth_account_id,wa.name,t.updated_at FROM transaction t LEFT JOIN wealth_account wa ON wa.id=t.related_wealth_account_id LEFT JOIN category c ON c.id=t.category_id LEFT JOIN merchant m ON m.id=t.merchant_id LEFT JOIN account a ON a.id=t.account_id LEFT JOIN "user" u ON u.id=t.created_by_user_id LEFT JOIN LATERAL (SELECT s.source_type FROM transaction_evidence te JOIN source_event s ON s.id=te.source_event_id WHERE te.transaction_id=t.id AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE' ORDER BY te.created_at LIMIT 1) e ON true WHERE t.id=$1 AND t.household_id=$2`, id, household).Scan(&v.ID, &v.Type, &v.Status, &v.Amount, &v.Currency, &v.TransactionAt, &v.Description, &v.Note, &v.Counterparty, &v.AccountID, &v.CategoryID, &v.MerchantID, &v.CategoryName, &v.MerchantName, &v.AccountName, &v.MemberName, &v.SourceType, &v.ConfirmedAt, &v.VoidedAt, &v.Purpose, &v.RelatedWealthAccountID, &v.WealthAccountName, &v.UpdatedAt)
	return v, err
}
func principalHousehold(w http.ResponseWriter, r *http.Request) (auth.Principal, string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return auth.Principal{}, "", false
	}
	return p, p.HouseholdID, true
}
