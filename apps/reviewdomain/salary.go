package reviewdomain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type SalaryCommand struct {
	HouseholdID string
	UserID      string
	Employer    string
	Period      string
	PayDate     any
	NetPay      string
	Transaction string
	SourceEvent string
	MakePrimary bool
}

type SalaryResult struct {
	SalarySourceID string
	Primary        bool
	SalaryEventID  string
}

var (
	ErrSalaryEmployerRequired = errors.New("reviewdomain: salary employer is required")
	ErrSalaryPeriodRequired   = errors.New("reviewdomain: salary payroll period is required")
)

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
		if _, err := tx.Exec(ctx, `UPDATE salary_source SET is_primary=true,employer=$2,updated_at=now() WHERE id=$1 AND household_id=$3`, existing, employer, cmd.HouseholdID); err != nil {
			return result, err
		}
		result.SalarySourceID = existing
		result.Primary = true
	} else {
		if err := tx.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,$3,$4,NOT EXISTS(SELECT 1 FROM salary_source WHERE household_id=$1 AND active AND is_primary)) ON CONFLICT (household_id,normalized_employer) WHERE active DO UPDATE SET employer=excluded.employer,updated_at=now() RETURNING id,is_primary`, cmd.HouseholdID, userID, employer, normalized).Scan(&result.SalarySourceID, &result.Primary); err != nil {
			return result, err
		}
	}
	if err := tx.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,currency,transaction_id,status,source_event_id) VALUES($1,$2,to_date($3,'YYYY-MM'),$4::date,$5,'IDR',$6,'CONFIRMED',$7) ON CONFLICT (salary_source_id,payroll_period) DO UPDATE SET transaction_id=EXCLUDED.transaction_id,pay_date=EXCLUDED.pay_date,net_pay=EXCLUDED.net_pay,source_event_id=EXCLUDED.source_event_id,status='CONFIRMED' WHERE salary_event.status='VOIDED' RETURNING id`, result.SalarySourceID, cmd.HouseholdID, period, salaryPayDateArg(cmd.PayDate), cmd.NetPay, cmd.Transaction, cmd.SourceEvent).Scan(&result.SalaryEventID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	return result, nil
}

func PromoteEvidenceDocuments(ctx context.Context, tx pgx.Tx, householdID, transactionID string) error {
	return RefreshTransactionLifecycle(ctx, tx, householdID, transactionID)
}

type PayslipFacts struct {
	Employer      string
	Period        string
	NetPay        string
	SourceEventID string
}

func LoadPayslipFacts(ctx context.Context, tx pgx.Tx, householdID, transactionID string) (PayslipFacts, bool, error) {
	var facts PayslipFacts
	err := tx.QueryRow(ctx, `SELECT COALESCE(t.counterparty_name,''),COALESCE(de.output_json->>'period',''),t.amount::text,te.source_event_id::text
		FROM transaction t
		JOIN transaction_evidence te ON te.transaction_id=t.id
		JOIN document d ON d.id=NULLIF(te.metadata_json->>'document_id','')::uuid AND d.household_id=t.household_id
		JOIN document_extraction de ON de.document_id=d.id AND de.stage='PAYSLIP'
		WHERE t.id=$1 AND t.household_id=$2 AND t.type='INCOME' AND te.evidence_type='PAYSLIP_IMAGE' LIMIT 1`, transactionID, householdID).Scan(&facts.Employer, &facts.Period, &facts.NetPay, &facts.SourceEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return facts, false, nil
	}
	if err != nil {
		return facts, false, err
	}
	return facts, true, nil
}

func salaryPayDateArg(v any) any {
	var t time.Time
	switch x := v.(type) {
	case time.Time:
		t = x
	case *time.Time:
		if x == nil {
			return nil
		}
		t = *x
	default:
		return v
	}
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.FixedZone("WIB", 7*3600)
	}
	return t.In(loc).Format("2006-01-02")
}
