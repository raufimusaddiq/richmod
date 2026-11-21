package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// Editable fields of an existing transaction (PATCH /api/v1/transactions/{id}).
// Amount, time, type, currency and evidence links are deliberately locked:
// correcting those is void + re-create so evidence stays intact.
//
// Review policy: a transaction with an open review item is rejected with 409
// (code REVIEW_OPEN, "selesaikan tinjauan dulu"). Review resolution owns those
// facts; this endpoint never routes around it. Transfers keep their account and
// have no category, so those two fields are rejected for TRANSFER. An unchanged
// current category/account is accepted even if deactivated since; only a change
// TO an inactive one is rejected.
const (
	codeInvalidRequest   = "INVALID_REQUEST"
	codeUnknownField     = "UNKNOWN_FIELD"
	codeLockedField      = "LOCKED_FIELD"
	codeInvalidCategory  = "INVALID_CATEGORY"
	codeInvalidAccount   = "INVALID_ACCOUNT"
	codeInvalidField     = "INVALID_FIELD"
	codeEmptyEdit        = "EMPTY_EDIT"
	codeNotFound         = "NOT_FOUND"
	codeVoided           = "TRANSACTION_VOIDED"
	codeReviewOpen       = "REVIEW_OPEN"
	codeStale            = "STALE_EDIT"
	codePreconditionNeed = "IF_MATCH_REQUIRED"
	codeTransferLocked   = "TRANSFER_FIELD_LOCKED"
)

// maxMerchantNameRunes mirrors the merchant.normalized_name CHECK (1..160).
const maxMerchantNameRunes = 160

var lockedEditFields = map[string]bool{
	"amount": true, "transactionAt": true, "type": true, "currency": true, "status": true,
	"purpose": true, "relatedWealthAccountId": true, "merchantId": true, "evidence": true,
}

// editError is a deterministic, client-mappable failure.
type editError struct {
	status int
	code   string
	msg    string
}

func (e editError) Error() string { return e.msg }

type optString struct {
	set   bool
	value *string
}

type editInput struct {
	categoryID, accountID, description, note, merchantName optString
}

// parseEditBody decodes the PATCH body. A field present as null clears it
// (category/account/note); absent fields are left untouched.
func parseEditBody(body []byte) (editInput, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return editInput{}, editError{http.StatusBadRequest, codeInvalidRequest, "invalid edit request"}
	}
	var in editInput
	for key, value := range raw {
		var target *optString
		switch key {
		case "categoryId":
			target = &in.categoryID
		case "accountId":
			target = &in.accountID
		case "description":
			target = &in.description
		case "note":
			target = &in.note
		case "merchantName":
			target = &in.merchantName
		default:
			if lockedEditFields[key] {
				return editInput{}, editError{http.StatusBadRequest, codeLockedField, "field " + key + " cannot be edited"}
			}
			return editInput{}, editError{http.StatusBadRequest, codeUnknownField, "unknown field " + key}
		}
		target.set = true
		if string(value) == "null" {
			continue
		}
		var s string
		if err := json.Unmarshal(value, &s); err != nil {
			return editInput{}, editError{http.StatusBadRequest, codeInvalidField, key + " must be a string or null"}
		}
		s = strings.TrimSpace(s)
		if key == "categoryId" || key == "accountId" {
			if s == "" {
				continue // empty string clears, like null
			}
			if uuid.Validate(s) != nil {
				return editInput{}, editError{http.StatusBadRequest, codeInvalidField, key + " must be a UUID"}
			}
		}
		if key == "note" && s == "" {
			continue
		}
		if key == "merchantName" {
			s = strings.Join(strings.Fields(s), " ")
		}
		target.value = &s
	}
	if !in.categoryID.set && !in.accountID.set && !in.description.set && !in.note.set && !in.merchantName.set {
		return editInput{}, editError{http.StatusBadRequest, codeEmptyEdit, "no editable fields supplied"}
	}
	if in.description.set && (in.description.value == nil || *in.description.value == "" || len(*in.description.value) > 500) {
		return editInput{}, editError{http.StatusBadRequest, codeInvalidField, "description is required and must not exceed 500 characters"}
	}
	if in.merchantName.set && (in.merchantName.value == nil || *in.merchantName.value == "" || len([]rune(*in.merchantName.value)) > maxMerchantNameRunes) {
		return editInput{}, editError{http.StatusBadRequest, codeInvalidField, "merchantName is required and must not exceed 160 characters"}
	}
	if in.note.set && in.note.value != nil && len(*in.note.value) > 2000 {
		return editInput{}, editError{http.StatusBadRequest, codeInvalidField, "note must not exceed 2000 characters"}
	}
	return in, nil
}

func (h *Handler) EditTransaction(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	fail := func(e editError) {
		writeJSON(w, e.status, map[string]string{"error": e.msg, "code": e.code})
	}
	if uuid.Validate(id) != nil {
		fail(editError{http.StatusNotFound, codeNotFound, "transaction not found"})
		return
	}
	match := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`)
	if match == "" {
		fail(editError{http.StatusPreconditionRequired, codePreconditionNeed, "If-Match with the transaction updatedAt is required"})
		return
	}
	expected, err := time.Parse(time.RFC3339Nano, match)
	if err != nil {
		fail(editError{http.StatusBadRequest, codeInvalidRequest, "If-Match must be the transaction updatedAt"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var body json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(editError{http.StatusBadRequest, codeInvalidRequest, "invalid edit request"})
		return
	}
	input, err := parseEditBody(body)
	if err == nil {
		err = h.edit(r.Context(), p, household, id, expected, input)
	}
	if err != nil {
		var ee editError
		if errors.As(err, &ee) {
			fail(ee)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to update transaction", "code": "INTERNAL"})
		return
	}
	v, err := h.load(r.Context(), household, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to load transaction", "code": "INTERNAL"})
		return
	}
	writeJSON(w, 200, v)
}

func (h *Handler) edit(ctx context.Context, p auth.Principal, household, id string, expected time.Time, in editInput) error {
	tx, err := h.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin edit: %w", err)
	}
	defer tx.Rollback(ctx)
	var (
		kind, status                              string
		accountID, categoryID                     *string
		description, note                         *string
		merchantID, counterparty, merchantCurrent *string
		updatedAt                                 time.Time
		reviewOpen                                bool
	)
	err = tx.QueryRow(ctx, `SELECT t.type,t.status,t.account_id::text,t.category_id::text,t.description,t.note,t.merchant_id::text,t.counterparty_name,m.normalized_name,t.updated_at,
		EXISTS(SELECT 1 FROM review_item ri WHERE ri.transaction_id=t.id AND ri.household_id=t.household_id AND ri.status IN ('OPEN','PENDING_SEND'))
		FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id AND m.household_id=t.household_id WHERE t.id=$1 AND t.household_id=$2 FOR UPDATE OF t`, id, household).
		Scan(&kind, &status, &accountID, &categoryID, &description, &note, &merchantID, &counterparty, &merchantCurrent, &updatedAt, &reviewOpen)
	if errors.Is(err, pgx.ErrNoRows) {
		return editError{http.StatusNotFound, codeNotFound, "transaction not found"}
	}
	if err != nil {
		return fmt.Errorf("lock transaction: %w", err)
	}
	if status == "VOIDED" {
		return editError{http.StatusConflict, codeVoided, "voided transactions cannot be edited"}
	}
	if !updatedAt.Equal(expected) {
		return editError{http.StatusConflict, codeStale, "transaction changed since it was loaded"}
	}
	if reviewOpen || status == "NEEDS_REVIEW" {
		return editError{http.StatusConflict, codeReviewOpen, "resolve the open review first (selesaikan tinjauan dulu)"}
	}
	if kind == "TRANSFER" && (in.categoryID.set || in.accountID.set || in.merchantName.set) {
		return editError{http.StatusConflict, codeTransferLocked, "a transfer's category, account and place cannot be edited"}
	}
	if in.categoryID.set && in.categoryID.value != nil && !equalPtr(in.categoryID.value, categoryID) {
		var okCategory bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM category WHERE id=$1 AND household_id=$2 AND active)`, *in.categoryID.value, household).Scan(&okCategory); err != nil {
			return fmt.Errorf("validate category: %w", err)
		}
		if !okCategory {
			return editError{http.StatusBadRequest, codeInvalidCategory, "categoryId must identify an active household category"}
		}
	}
	if in.accountID.set && in.accountID.value != nil && !equalPtr(in.accountID.value, accountID) {
		var okAccount bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account WHERE id=$1 AND household_id=$2 AND active)`, *in.accountID.value, household).Scan(&okAccount); err != nil {
			return fmt.Errorf("validate account: %w", err)
		}
		if !okAccount {
			return editError{http.StatusBadRequest, codeInvalidAccount, "accountId must identify an active household account"}
		}
	}
	// "Tempat · Ubah": resolve (or create) the household's clean merchant and point the
	// raw source name's alias at it, so the next transaction from that raw name inherits it.
	// counterparty_name stays untouched as preserved source evidence.
	var newMerchantID *string
	var aliasChange map[string]any
	if in.merchantName.set {
		name := *in.merchantName.value
		var resolved string
		if err := tx.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET updated_at=merchant.updated_at RETURNING id::text`, household, name).Scan(&resolved); err != nil {
			return fmt.Errorf("resolve merchant: %w", err)
		}
		if !equalPtr(&resolved, merchantID) {
			newMerchantID = &resolved
			raw := firstNonBlank(counterparty, merchantCurrent, description)
			if raw != "" {
				var oldAlias *string
				var aliasExisted bool
				err := tx.QueryRow(ctx, `SELECT normalized_merchant_id::text FROM merchant_alias WHERE household_id=$1 AND lower(regexp_replace(btrim(raw_name),'[[:space:]]+',' ','g'))=lower(regexp_replace(btrim($2),'[[:space:]]+',' ','g')) FOR UPDATE`, household, raw).Scan(&oldAlias)
				switch {
				case err == nil:
					aliasExisted = true
				case !errors.Is(err, pgx.ErrNoRows):
					return fmt.Errorf("read merchant alias: %w", err)
				}
				if _, err := tx.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,created_from_user_confirmation) VALUES($1,left(regexp_replace(btrim($2),'[[:space:]]+',' ','g'),160),$3,true) ON CONFLICT(household_id,(lower(regexp_replace(btrim(raw_name),'[[:space:]]+',' ','g')))) DO UPDATE SET normalized_merchant_id=excluded.normalized_merchant_id,created_from_user_confirmation=true`, household, raw, resolved); err != nil {
					return fmt.Errorf("upsert merchant alias: %w", err)
				}
				aliasChange = map[string]any{"rawName": raw, "existed": aliasExisted, "before": oldAlias, "after": resolved}
			}
		}
	}
	before, after := map[string]any{}, map[string]any{}
	record := func(field string, old, next *string) {
		if !equalPtr(old, next) {
			before[field], after[field] = old, next
		}
	}
	newCategory, newAccount, newDescription, newNote := categoryID, accountID, description, note
	if in.categoryID.set {
		newCategory = in.categoryID.value
	}
	if in.accountID.set {
		newAccount = in.accountID.value
	}
	if in.description.set {
		newDescription = in.description.value
	}
	if in.note.set {
		newNote = in.note.value
	}
	record("categoryId", categoryID, newCategory)
	record("accountId", accountID, newAccount)
	record("description", description, newDescription)
	record("note", note, newNote)
	if newMerchantID != nil {
		before["merchantName"], after["merchantName"] = merchantCurrent, in.merchantName.value
	}
	if len(after) > 0 {
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if _, err := tx.Exec(ctx, `UPDATE transaction SET category_id=$2::uuid,account_id=$3::uuid,description=$4,note=$5,merchant_id=COALESCE($6::uuid,merchant_id),updated_at=now() WHERE id=$1`, id, newCategory, newAccount, newDescription, newNote, newMerchantID); err != nil {
			return fmt.Errorf("update transaction: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,before_json,after_json) VALUES($1,'USER',$2,'UPDATE_DETAILS','transaction',$3,$4::jsonb,$5::jsonb)`, household, p.UserID, id, string(beforeJSON), string(afterJSON)); err != nil {
			return fmt.Errorf("audit edit: %w", err)
		}
		if aliasChange != nil {
			aliasJSON, _ := json.Marshal(aliasChange)
			if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'UPDATE_MERCHANT_ALIAS','merchant_alias',$3,$4::jsonb)`, household, p.UserID, id, string(aliasJSON)); err != nil {
				return fmt.Errorf("audit merchant alias: %w", err)
			}
		}
		// A detail edit has no residual side effect: analytics read the ledger
		// live and the cycle basis sums confirmed transactions household-wide.
	}
	return tx.Commit(ctx)
}

func equalPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func firstNonBlank(values ...*string) string {
	for _, value := range values {
		if value != nil {
			if cleaned := strings.Join(strings.Fields(*value), " "); cleaned != "" {
				return cleaned
			}
		}
	}
	return ""
}
