package residual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Payload struct {
	HouseholdID      string `json:"household_id"`
	EndSalaryEventID string `json:"end_salary_event_id"`
}

type Processor struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Processor { return &Processor{pool: pool} }

func Decode(b json.RawMessage) (Payload, error) {
	var p Payload
	if err := json.Unmarshal(b, &p); err != nil || p.HouseholdID == "" || p.EndSalaryEventID == "" {
		return p, fmt.Errorf("invalid residual payload")
	}
	return p, nil
}

func (p *Processor) Generate(ctx context.Context, v Payload) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var start, cycleStart, cycleEnd, income, expense, savings, residual string
	err = tx.QueryRow(ctx, `WITH bounds AS (SELECT se.pay_date end_date,(SELECT se2.pay_date FROM salary_event se2 JOIN salary_source ss2 ON ss2.id=se2.salary_source_id WHERE se2.household_id=se.household_id AND se2.status='CONFIRMED' AND ss2.is_primary AND se2.pay_date<se.pay_date ORDER BY se2.pay_date DESC LIMIT 1) start_date,(SELECT se2.id FROM salary_event se2 JOIN salary_source ss2 ON ss2.id=se2.salary_source_id WHERE se2.household_id=se.household_id AND se2.status='CONFIRMED' AND ss2.is_primary AND se2.pay_date<se.pay_date ORDER BY se2.pay_date DESC LIMIT 1) start_id FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id WHERE se.id=$1 AND se.household_id=$2 AND se.status='CONFIRMED' AND ss.is_primary) SELECT start_id,b.start_date::text,b.end_date::text,COALESCE(sum(t.amount) FILTER(WHERE t.type='INCOME'),0)::text,COALESCE(sum(CASE WHEN t.type='EXPENSE' THEN t.amount WHEN t.type='REFUND' THEN -t.amount ELSE 0 END),0)::text,COALESCE(sum(t.amount) FILTER(WHERE t.type='TRANSFER' AND t.purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE')),0)::text FROM bounds b LEFT JOIN transaction t ON t.household_id=$2 AND t.status='CONFIRMED' AND t.transaction_at >= (b.start_date::timestamp AT TIME ZONE 'Asia/Jakarta') AND t.transaction_at < (b.end_date::timestamp AT TIME ZONE 'Asia/Jakarta') GROUP BY start_id,b.start_date,b.end_date`, v.EndSalaryEventID, v.HouseholdID).Scan(&start, &cycleStart, &cycleEnd, &income, &expense, &savings)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx) // stale/non-primary residual jobs are harmless no-ops.
	}
	if err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT ($1::numeric-$2::numeric-$3::numeric)::text`, income, expense, savings).Scan(&residual); err != nil {
		return err
	}
	n, ok := new(big.Int).SetString(residual, 10)
	if !ok {
		return fmt.Errorf("invalid residual")
	}
	if n.Sign() <= 0 {
		return tx.Commit(ctx)
	}
	var caseID string
	var created bool
	if err = tx.QueryRow(ctx, `INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(household_id,start_salary_event_id,end_salary_event_id) DO UPDATE SET basis_income_idr=excluded.basis_income_idr,basis_expense_idr=excluded.basis_expense_idr,basis_savings_idr=excluded.basis_savings_idr,basis_residual_idr=excluded.basis_residual_idr,updated_at=now() RETURNING id,xmax=0`, v.HouseholdID, start, v.EndSalaryEventID, cycleStart, cycleEnd, income, expense, savings, residual).Scan(&caseID, &created); err != nil {
		return err
	}
	if created {
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'WORKER','CYCLE_RESIDUAL_CASE_CREATE','cycle_residual_case',$2,jsonb_build_object('residual_idr',$3::text))`, v.HouseholdID, caseID, residual); err != nil {
			return err
		}
	}
	var reviewID string
	err = tx.QueryRow(ctx, `INSERT INTO review_item(household_id,review_type,status,cycle_residual_case_id) VALUES($1,'CYCLE_RESIDUAL_ALLOCATION','PENDING_SEND',$2) ON CONFLICT DO NOTHING RETURNING id`, v.HouseholdID, caseID).Scan(&reviewID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return tx.Commit(ctx)
		}
		return err
	}
	var requestID string
	if err = tx.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,review_type,status) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','PENDING_SEND') RETURNING id`, v.HouseholdID, reviewID).Scan(&requestID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT telegram_user_id FROM telegram_identity WHERE household_id=$1 AND active ORDER BY created_at LIMIT 10`, v.HouseholdID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var chat int64
		if err = rows.Scan(&chat); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id) VALUES($1,$2)`, requestID, chat); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
