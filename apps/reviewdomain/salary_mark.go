package reviewdomain

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type MarkSalaryCommand struct {
	HouseholdID   string
	UserID        string
	TransactionID string
}

type MarkSalaryResult struct {
	SalaryEventID string
	Period        string
	Primary       bool
}

var (
	ErrSalaryMarkNotFound   = errors.New("reviewdomain: transaction not found")
	ErrSalaryMarkIneligible = errors.New("reviewdomain: transaction is not a confirmed payslip income")
	ErrSalaryMarkConflict   = errors.New("reviewdomain: salary for this transaction or payroll period is already recorded")
)

type RowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const payslipSalaryMarkable = `t.type='INCOME' AND t.status='CONFIRMED' AND NULLIF(btrim(COALESCE(t.counterparty_name,'')),'') IS NOT NULL
	AND EXISTS(SELECT 1 FROM transaction_evidence te
		JOIN document d ON d.id=NULLIF(te.metadata_json->>'document_id','')::uuid AND d.household_id=t.household_id
		JOIN document_extraction de ON de.document_id=d.id AND de.stage='PAYSLIP'
		WHERE te.transaction_id=t.id AND te.evidence_type='PAYSLIP_IMAGE' AND COALESCE(de.output_json->>'period','') ~ '^[0-9]{4}-[0-9]{2}$')
	AND NOT EXISTS(SELECT 1 FROM salary_event se WHERE se.transaction_id=t.id)`

func CanMarkPayslipIncomeAsSalary(ctx context.Context, q RowQuerier, householdID, transactionID string) (bool, error) {
	var markable bool
	err := q.QueryRow(ctx, `SELECT `+payslipSalaryMarkable+` FROM transaction t WHERE t.id=$1 AND t.household_id=$2`, transactionID, householdID).Scan(&markable)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return markable, err
}

func MarkPayslipIncomeAsSalary(ctx context.Context, tx pgx.Tx, cmd MarkSalaryCommand) (MarkSalaryResult, error) {
	var out MarkSalaryResult
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text),0)`, cmd.HouseholdID); err != nil {
		return out, err
	}
	var kind, status string
	var at time.Time
	var hasSalary bool
	err := tx.QueryRow(ctx, `SELECT t.type,t.status,t.transaction_at,EXISTS(SELECT 1 FROM salary_event se WHERE se.transaction_id=t.id) FROM transaction t WHERE t.id=$1 AND t.household_id=$2 FOR UPDATE OF t`, cmd.TransactionID, cmd.HouseholdID).Scan(&kind, &status, &at, &hasSalary)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrSalaryMarkNotFound
	}
	if err != nil {
		return out, err
	}
	if kind != "INCOME" || status != "CONFIRMED" {
		return out, ErrSalaryMarkIneligible
	}
	if hasSalary {
		return out, ErrSalaryMarkConflict
	}
	facts, ok, err := LoadPayslipFacts(ctx, tx, cmd.HouseholdID, cmd.TransactionID)
	if err != nil {
		return out, err
	}
	if _, periodErr := time.Parse("2006-01", facts.Period); !ok || periodErr != nil || cleanText(facts.Employer, 160) == "" {
		return out, ErrSalaryMarkIneligible
	}
	salary, err := RecordSalaryEvent(ctx, tx, SalaryCommand{HouseholdID: cmd.HouseholdID, UserID: cmd.UserID, Employer: facts.Employer, Period: facts.Period, PayDate: at, NetPay: facts.NetPay, Transaction: cmd.TransactionID, SourceEvent: facts.SourceEventID})
	if err != nil {
		return out, err
	}
	if salary.SalaryEventID == "" {
		return out, ErrSalaryMarkConflict
	}
	if salary.Primary {
		if _, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json,max_attempts) VALUES('GENERATE_CYCLE_RESIDUAL_REVIEW',jsonb_build_object('household_id',$1::uuid,'end_salary_event_id',$2::uuid),5) ON CONFLICT DO NOTHING`, cmd.HouseholdID, salary.SalaryEventID); err != nil {
			return out, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'MARK_PAYSLIP_SALARY','transaction',$3,jsonb_build_object('salary_event_id',$4::uuid,'period',$5::text,'primary',$6::boolean))`, cmd.HouseholdID, nullableUUID(cmd.UserID), cmd.TransactionID, salary.SalaryEventID, facts.Period, salary.Primary); err != nil {
		return out, err
	}
	out.SalaryEventID, out.Period, out.Primary = salary.SalaryEventID, facts.Period, salary.Primary
	return out, nil
}
