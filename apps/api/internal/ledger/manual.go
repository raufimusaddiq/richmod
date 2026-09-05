package ledger

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

type manualInput struct {
	Type          string  `json:"type"`
	Amount        string  `json:"amount"`
	TransactionAt string  `json:"transactionAt"`
	Description   string  `json:"description"`
	Note          string  `json:"note"`
	AccountID     *string `json:"accountId"`
	CategoryID    *string `json:"categoryId"`
}

type validationError struct{ message string }

func (e validationError) Error() string { return e.message }

func (h *Handler) CreateManualTransaction(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !principal.HasHousehold {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	var input manualInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid transaction request"})
		return
	}
	transactionID, err := h.create(r.Context(), principal, input)
	if err != nil {
		var invalid validationError
		if errors.As(err, &invalid) {
			writeJSON(w, 400, map[string]string{"error": invalid.Error()})
			return
		}
		writeJSON(w, 500, map[string]string{"error": "unable to create transaction"})
		return
	}
	writeJSON(w, 201, map[string]string{"id": transactionID, "status": "CONFIRMED", "currency": "IDR"})
}

func (h *Handler) create(ctx context.Context, principal auth.Principal, input manualInput) (string, error) {
	if input.Type != "INCOME" && input.Type != "EXPENSE" {
		return "", validationError{"type must be INCOME or EXPENSE"}
	}
	amount, ok := new(big.Int).SetString(input.Amount, 10)
	if !ok || amount.Sign() <= 0 || amount.String() != input.Amount {
		return "", validationError{"amount must be a positive whole IDR amount"}
	}
	input.Description = strings.TrimSpace(input.Description)
	if input.Description == "" || len(input.Description) > 500 {
		return "", validationError{"description is required and must not exceed 500 characters"}
	}
	transactionAt := time.Now().In(clock.HouseholdLocation())
	if input.TransactionAt != "" {
		parsed, err := time.Parse(time.RFC3339, input.TransactionAt)
		if err != nil {
			return "", validationError{"transactionAt must be RFC3339"}
		}
		transactionAt = parsed
	}
	tx, err := h.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	householdID := principal.HouseholdID
	if input.AccountID != nil {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account WHERE id=$1 AND household_id=$2 AND active)`, *input.AccountID, householdID).Scan(&exists); err != nil {
			return "", fmt.Errorf("validate account: %w", err)
		}
		if !exists {
			return "", validationError{"accountId must identify an active household account"}
		}
	}
	if input.CategoryID != nil {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM category WHERE id=$1 AND household_id=$2 AND active)`, *input.CategoryID, householdID).Scan(&exists); err != nil {
			return "", fmt.Errorf("validate category: %w", err)
		}
		if !exists {
			return "", validationError{"categoryId must identify an active household category"}
		}
	}
	var transactionID string
	if err := tx.QueryRow(ctx, `INSERT INTO transaction (household_id,account_id,category_id,type,status,amount,currency,transaction_at,description,note,created_by_user_id,confirmed_at) VALUES ($1,$2,$3,$4,'CONFIRMED',$5,'IDR',$6,$7,$8,$9,now()) RETURNING id`, householdID, input.AccountID, input.CategoryID, input.Type, amount.String(), transactionAt, input.Description, input.Note, principal.UserID).Scan(&transactionID); err != nil {
		return "", fmt.Errorf("create transaction: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate evidence hash: %w", err)
	}
	hash := sha256.Sum256(raw)
	var sourceID string
	if err := tx.QueryRow(ctx, `INSERT INTO source_event (household_id,source_type,received_at,payload_hash,processing_status) VALUES ($1,'WEB_MANUAL',now(),$2,'PROCESSED') RETURNING id`, householdID, hash[:]).Scan(&sourceID); err != nil {
		return "", fmt.Errorf("create source event: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence (transaction_id,source_event_id,evidence_type,metadata_json) VALUES ($1,$2,'MANUAL_ENTRY','{}')`, transactionID, sourceID); err != nil {
		return "", fmt.Errorf("attach evidence: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES ($1,'USER',$2,'CREATE','transaction',$3,jsonb_build_object('type',$4::text,'amount',$5::text,'currency','IDR','accountId',$6::text,'categoryId',$7::text))`, householdID, principal.UserID, transactionID, input.Type, amount.String(), input.AccountID, input.CategoryID); err != nil {
		return "", fmt.Errorf("audit transaction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}
	return transactionID, nil
}

func decodeJSON(r *http.Request, out any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func writeJSON(w http.ResponseWriter, code int, out any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(out)
}
