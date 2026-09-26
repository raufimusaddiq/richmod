package reviewdomain

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// SalaryCommand records one confirmed salary event against a household salary
// source. Web (payslip confirmation) and both Telegram confirm lanes call it, so
// salary_source promotion and salary_event ordering stay identical across
// surfaces (ADR-046).
//
// Callers pass already-validated facts: RecordSalaryEvent never parses user text
// and never creates a transaction, so a bad input cannot fabricate income.
type SalaryCommand struct {
	HouseholdID string
	// UserID owns the salary source. An empty value uses the household owner.
	UserID string
	// Employer is the raw counterparty/employer name.
	Employer string
	// Period is the payroll period as YYYY-MM.
	Period string
	// PayDate is a date-compatible argument (YYYY-MM-DD or a date/time value).
	PayDate any
	// NetPay is NUMERIC text, taken from the confirmed income transaction.
	NetPay      string
	Transaction string
	SourceEvent string
	// MakePrimary promotes this employer to the household's primary salary source,
	// demoting any previous primary.
	MakePrimary bool
}

// SalaryResult reports the salary source the event was recorded against.
type SalaryResult struct {
	SalarySourceID string
	Primary        bool
	// SalaryEventID is the newly inserted salary event, empty when a matching
	// (source, period) event already existed and the insert was skipped.
	SalaryEventID string
}

var (
	// ErrSalaryEmployerRequired reports a salary record with no employer identity.
	ErrSalaryEmployerRequired = errors.New("reviewdomain: salary employer is required")
	// ErrSalaryPeriodRequired reports a salary record with no payroll period.
	ErrSalaryPeriodRequired = errors.New("reviewdomain: salary payroll period is required")
)

// RecordSalaryEvent upserts the salary source for the employer and inserts the
// confirmed salary event. When MakePrimary is false the source still becomes
// primary if the household has no primary yet, which is the behavior both
// Telegram confirm lanes relied on; this keeps the first recorded salary
// primary without an extra caller decision.
func RecordSalaryEvent(ctx context.Context, tx pgx.Tx, cmd SalaryCommand) (SalaryResult, error) {
	var result SalaryResult
	employer := cleanText(cmd.Employer, 160)
	if employer == "" {
		return result, ErrSalaryEmployerRequired
	}
	period := strings.TrimSpace(cmd.Period)
	if period == "" {
		return result, ErrSalaryPeriodRequired
	}
	normalized := strings.ToLower(employer)
	userID := cmd.UserID
	if userID == "" {
		if err := tx.QueryRow(ctx, `SELECT user_id FROM household_member WHERE household_id=$1 AND role='OWNER' ORDER BY created_at LIMIT 1`, cmd.HouseholdID).Scan(&userID); err != nil {
			return result, err
		}
	}
	if cmd.MakePrimary {
		var existing string
		err := tx.QueryRow(ctx, `SELECT id FROM salary_source WHERE household_id=$1 AND normalized_employer=$2 AND active FOR UPDATE`, cmd.HouseholdID, normalized).Scan(&existing)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,$3,$4,true) RETURNING id`, cmd.HouseholdID, userID, employer, normalized).Scan(&existing)
		}
		if err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, `UPDATE salary_source SET is_primary=false,updated_at=now() WHERE household_id=$1 AND active AND id<>$2`, cmd.HouseholdID, existing); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, `UPDATE salary_source SET is_primary=true,employer=$2,updated_at=now() WHERE id=$1`, existing, employer); err != nil {
			return result, err
		}
		result.SalarySourceID = existing
		result.Primary = true
	} else {
		if err := tx.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,$3,$4,NOT EXISTS(SELECT 1 FROM salary_source WHERE household_id=$1 AND active AND is_primary)) ON CONFLICT (household_id,normalized_employer) WHERE active DO UPDATE SET employer=excluded.employer,updated_at=now() RETURNING id,is_primary`, cmd.HouseholdID, userID, employer, normalized).Scan(&result.SalarySourceID, &result.Primary); err != nil {
			return result, err
		}
	}
	// RETURNING id distinguishes a real insert from an ON CONFLICT skip, so a
	// re-confirmation does not enqueue a duplicate residual review.
	if err := tx.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,currency,transaction_id,status,source_event_id) VALUES($1,$2,to_date($3,'YYYY-MM'),$4::date,$5,'IDR',$6,'CONFIRMED',$7) ON CONFLICT (salary_source_id,payroll_period) DO NOTHING RETURNING id`, result.SalarySourceID, cmd.HouseholdID, period, cmd.PayDate, cmd.NetPay, cmd.Transaction, cmd.SourceEvent).Scan(&result.SalaryEventID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	return result, nil
}

// PromoteEvidenceDocuments marks documents linked to one transaction's evidence
// as extracted. Both surfaces performed this after a confirmed document-backed
// review; keeping it here avoids two divergent status filters.
func PromoteEvidenceDocuments(ctx context.Context, tx pgx.Tx, transactionID string) error {
	_, err := tx.Exec(ctx, `UPDATE document d SET status='EXTRACTED',updated_at=now()
		WHERE d.status='NEEDS_REVIEW' AND d.id IN (
			SELECT NULLIF(te.metadata_json->>'document_id','')::uuid FROM transaction_evidence te
			WHERE te.transaction_id=$1 AND te.metadata_json ? 'document_id')`, transactionID)
	return err
}

// PayslipFacts reads the employer and payroll period from the payslip evidence
// attached to one income transaction. Both Telegram confirm lanes derived these
// facts the same way before recording salary.
type PayslipFacts struct {
	Employer string
	Period   string
	NetPay   string
}

// LoadPayslipFacts returns the payslip facts for a transaction, or ok=false when
// the transaction has no payslip evidence to complete.
func LoadPayslipFacts(ctx context.Context, tx pgx.Tx, transactionID string) (PayslipFacts, bool, error) {
	var facts PayslipFacts
	err := tx.QueryRow(ctx, `SELECT COALESCE(t.counterparty_name,''),COALESCE(de.output_json->>'period',''),t.amount::text
		FROM transaction t
		JOIN transaction_evidence te ON te.transaction_id=t.id
		JOIN document_extraction de ON de.document_id=NULLIF(te.metadata_json->>'document_id','')::uuid AND de.stage='PAYSLIP'
		WHERE t.id=$1 AND t.type='INCOME' AND te.evidence_type='PAYSLIP_IMAGE' LIMIT 1`, transactionID).Scan(&facts.Employer, &facts.Period, &facts.NetPay)
	if errors.Is(err, pgx.ErrNoRows) {
		return facts, false, nil
	}
	if err != nil {
		return facts, false, err
	}
	return facts, true, nil
}
