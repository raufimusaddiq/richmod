package budget

import (
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

type Handler struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, now: time.Now} }

type view struct {
	ID            string  `json:"id"`
	CategoryID    string  `json:"categoryId"`
	CategoryName  string  `json:"categoryName"`
	MonthlyAmount string  `json:"monthlyAmount"`
	Spent         string  `json:"spent"`
	Remaining     string  `json:"remaining"`
	Utilization   string  `json:"utilization"`
	Currency      string  `json:"currency"`
	StartMonth    string  `json:"startMonth"`
	EndMonth      *string `json:"endMonth"`
	Active        bool    `json:"active"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principal(w, r)
	if !ok {
		return
	}
	local := h.now().In(clock.HouseholdLocation())
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, clock.HouseholdLocation())
	end := start.AddDate(0, 1, 0)
	rows, err := h.pool.Query(r.Context(), `
		WITH RECURSIVE applicable AS (
			SELECT id,category_id FROM budget
			WHERE household_id=$1 AND active AND start_month<=$2::date
			  AND (end_month IS NULL OR end_month >= $2::date)
		), tree AS (
			SELECT id AS budget_id,category_id FROM applicable
			UNION ALL
			SELECT tree.budget_id,c.id FROM tree JOIN category c ON c.parent_id=tree.category_id
			WHERE c.household_id=$1
		), spending AS (
			SELECT tree.budget_id,COALESCE(sum(CASE WHEN t.type='EXPENSE' THEN t.amount WHEN t.type='REFUND' THEN -t.amount ELSE 0 END),0) AS spent
			FROM tree LEFT JOIN transaction t ON t.household_id=$1 AND t.category_id=tree.category_id
			  AND t.status='CONFIRMED' AND t.transaction_at >= $3 AND t.transaction_at < $4
			GROUP BY tree.budget_id
		)
		SELECT b.id,b.category_id,c.name,b.monthly_amount::text,COALESCE(s.spent,0)::text,b.currency,
		       to_char(b.start_month,'YYYY-MM'),CASE WHEN b.end_month IS NULL THEN NULL ELSE to_char(b.end_month,'YYYY-MM') END,b.active
		FROM budget b JOIN category c ON c.id=b.category_id LEFT JOIN spending s ON s.budget_id=b.id
		WHERE b.household_id=$1 AND b.active AND b.start_month<=$2::date AND (b.end_month IS NULL OR b.end_month >= $2::date)
		ORDER BY c.sort_order,c.name`, household, start.Format("2006-01-02"), start, end)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list budgets"})
		return
	}
	defer rows.Close()
	result := make([]view, 0)
	for rows.Next() {
		var value view
		if err := rows.Scan(&value.ID, &value.CategoryID, &value.CategoryName, &value.MonthlyAmount, &value.Spent, &value.Currency, &value.StartMonth, &value.EndMonth, &value.Active); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list budgets"})
			return
		}
		value.Remaining = subtract(value.MonthlyAmount, value.Spent)
		value.Utilization = ratio(value.Spent, value.MonthlyAmount)
		result = append(result, value)
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list budgets"})
		return
	}
	writeJSON(w, 200, result)
}

type createInput struct {
	CategoryID    string `json:"categoryId"`
	MonthlyAmount string `json:"monthlyAmount"`
	StartMonth    string `json:"startMonth"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principal(w, r)
	if !ok {
		return
	}
	if p.Memberships[0].Role != "OWNER" {
		writeJSON(w, 403, map[string]string{"error": "owner role required"})
		return
	}
	var input createInput
	if decodeJSON(r, &input) != nil || strings.TrimSpace(input.CategoryID) == "" || !validMoney(input.MonthlyAmount) {
		writeJSON(w, 400, map[string]string{"error": "invalid budget request"})
		return
	}
	start, ok := parseMonth(input.StartMonth, h.now())
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "startMonth must be YYYY-MM"})
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to create budget"})
		return
	}
	defer tx.Rollback(r.Context())
	var categoryName string
	if err := tx.QueryRow(r.Context(), `SELECT name FROM category WHERE id=$1 AND household_id=$2 AND active`, input.CategoryID, household).Scan(&categoryName); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid household category"})
		return
	}
	var id string
	if err := tx.QueryRow(r.Context(), `INSERT INTO budget(household_id,category_id,monthly_amount,start_month,created_by_user_id) VALUES($1,$2,$3,$4,$5) RETURNING id`, household, input.CategoryID, input.MonthlyAmount, start, p.UserID).Scan(&id); err != nil {
		writeJSON(w, 409, map[string]string{"error": "an active budget already exists for this category"})
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'CREATE_BUDGET','budget',$3,jsonb_build_object('category_id',$4::uuid,'category_name',$5::text,'monthly_amount',$6::text,'currency','IDR','start_month',$7::date))`, household, p.UserID, id, input.CategoryID, categoryName, input.MonthlyAmount, start); err != nil || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to audit budget"})
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

type patchInput struct {
	MonthlyAmount *string `json:"monthlyAmount"`
	Active        *bool   `json:"active"`
}

func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principal(w, r)
	if !ok {
		return
	}
	if p.Memberships[0].Role != "OWNER" {
		writeJSON(w, 403, map[string]string{"error": "owner role required"})
		return
	}
	var input patchInput
	if decodeJSON(r, &input) != nil || (input.MonthlyAmount == nil && input.Active == nil) || (input.MonthlyAmount != nil && !validMoney(*input.MonthlyAmount)) {
		writeJSON(w, 400, map[string]string{"error": "invalid budget update"})
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to update budget"})
		return
	}
	defer tx.Rollback(r.Context())
	var beforeAmount string
	var beforeActive bool
	if err := tx.QueryRow(r.Context(), `SELECT monthly_amount::text,active FROM budget WHERE id=$1 AND household_id=$2 FOR UPDATE`, r.PathValue("id"), household).Scan(&beforeAmount, &beforeActive); errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "budget not found"})
		return
	} else if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to update budget"})
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE budget SET monthly_amount=COALESCE($3::numeric,monthly_amount),active=COALESCE($4::boolean,active),updated_at=now() WHERE id=$1 AND household_id=$2`, r.PathValue("id"), household, input.MonthlyAmount, input.Active); err != nil {
		writeJSON(w, 409, map[string]string{"error": "unable to activate duplicate category budget"})
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,before_json,after_json) VALUES($1,'USER',$2,'UPDATE_BUDGET','budget',$3,jsonb_build_object('monthly_amount',$4::text,'active',$5::boolean),jsonb_build_object('monthly_amount',COALESCE($6::text,$4::text),'active',COALESCE($7::boolean,$5::boolean)))`, household, p.UserID, r.PathValue("id"), beforeAmount, beforeActive, input.MonthlyAmount, input.Active); err != nil || tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to audit budget"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validMoney(value string) bool {
	amount, ok := new(big.Int).SetString(value, 10)
	return ok && amount.Sign() > 0 && amount.String() == value && len(value) <= 20
}

func parseMonth(value string, now time.Time) (time.Time, bool) {
	if value == "" {
		local := now.In(clock.HouseholdLocation())
		return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, clock.HouseholdLocation()), true
	}
	parsed, err := time.ParseInLocation("2006-01", value, clock.HouseholdLocation())
	return parsed, err == nil
}

func subtract(left, right string) string {
	a, _ := new(big.Int).SetString(left, 10)
	b, _ := new(big.Int).SetString(right, 10)
	return new(big.Int).Sub(a, b).String()
}

func ratio(numerator, denominator string) string {
	n, _ := new(big.Int).SetString(numerator, 10)
	d, _ := new(big.Int).SetString(denominator, 10)
	if d.Sign() <= 0 {
		return "0.0000"
	}
	return new(big.Rat).SetFrac(n, d).FloatString(4)
}

func principal(w http.ResponseWriter, r *http.Request) (auth.Principal, string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return auth.Principal{}, "", false
	}
	return p, p.Memberships[0].HouseholdID, true
}

func decodeJSON(r *http.Request, output any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("content type must be application/json")
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
