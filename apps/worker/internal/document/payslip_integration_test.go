package document

import (
	"context"
	"fmt"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// Quality-only concerns cannot manufacture human work; first-salary policy
// still requires the household, then uses the same salary finalizer.
func TestPayslipQualityAndFirstSalaryPolicy(t *testing.T) {
	for _, first := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing-primary", true: "first-primary"}[first], func(t *testing.T) {
			f := seedScreenshotFixture(t, "SAVR07 payslip")
			ctx := context.Background()
			var user string
			if err := f.pool.QueryRow(ctx, `SELECT user_id::text FROM telegram_identity WHERE household_id=$1`, f.householdID).Scan(&user); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, f.householdID, user); err != nil {
				t.Fatal(err)
			}
			if !first {
				if _, err := f.pool.Exec(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Existing','existing',true)`, f.householdID, user); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.pool.Exec(ctx, `UPDATE document SET document_type='PAYSLIP' WHERE id=$1`, f.documentID); err != nil {
				t.Fatal(err)
			}
			date := "2026-09-25"
			slip := payslipExtraction{Period: "2026-09", Employer: "Employer", NetPay: "16000000", Currency: "IDR", PayDate: &date, Confidence: .21}
			at, arithmetic, err := validatePayslip(slip)
			if err != nil || arithmetic {
				t.Fatalf("valid but arithmetic-unproved slip: %v %t", err, arithmetic)
			}
			if err := (&Processor{pool: f.pool}).persistPayslip(ctx, f.documentID, f.householdID, f.sourceID, slip, "test-model", at, slip.Period, true, arithmetic); err != nil {
				t.Fatal(err)
			}
			var reviews, incomes, events int
			check := func() {
				t.Helper()
				if err := f.pool.QueryRow(ctx, `SELECT
					(SELECT count(*) FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')),
					(SELECT count(*) FROM transaction WHERE household_id=$1 AND type='INCOME' AND status='CONFIRMED'),
					(SELECT count(*) FROM salary_event WHERE household_id=$1 AND status='CONFIRMED')`, f.householdID).Scan(&reviews, &incomes, &events); err != nil {
					t.Fatal(err)
				}
			}
			check()
			if !first {
				if reviews != 0 || incomes != 1 || events != 1 {
					t.Fatalf("quality-only review=%d income=%d events=%d", reviews, incomes, events)
				}
				return
			}
			if reviews != 1 || incomes != 0 || events != 0 {
				t.Fatalf("first salary review=%d income=%d events=%d", reviews, incomes, events)
			}
			var itemID, proposalID, amount, employer, period, knownDate string
			if err := f.pool.QueryRow(ctx, `SELECT id::text,proposal_id::text,decision->'knownFacts'->>'amount_idr',decision->'knownFacts'->>'merchant',decision->'knownFacts'->>'payroll_period',decision->'knownFacts'->>'transaction_at' FROM review_item WHERE document_id=$1`, f.documentID).Scan(&itemID, &proposalID, &amount, &employer, &period, &knownDate); err != nil {
				t.Fatal(err)
			}
			if amount != slip.NetPay || employer != slip.Employer || period != slip.Period || knownDate == "" {
				t.Fatalf("accepted facts lost: %s %s %s %s", amount, employer, period, knownDate)
			}
			tx, err := f.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := reviewdomain.ResolvePayslipProposal(ctx, tx, reviewdomain.PayslipCommand{HouseholdID: f.householdID, UserID: user, ReviewItemID: itemID, ProposalID: proposalID, SourceEventID: f.sourceID, DocumentID: f.documentID, Action: "PRIMARY_SALARY", Choice: "PRIMARY_SALARY"}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			check()
			if reviews != 0 || incomes != 1 || events != 1 {
				t.Fatalf("resolved salary review=%d income=%d events=%d", reviews, incomes, events)
			}
			var jobs int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND payload_json->>'household_id'=$1`, f.householdID).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			if jobs != 1 {
				t.Fatalf("primary salary cycle jobs=%d", jobs)
			}
		})
	}
}

func TestPayslipInvalidMachineOutputDoesNotAskHousehold(t *testing.T) {
	f := seedScreenshotFixture(t, "SAVR08 payslip repair")
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE document SET document_type='PAYSLIP' WHERE id=$1`, f.documentID); err != nil {
		t.Fatal(err)
	}
	p := &Processor{pool: f.pool}
	if err := p.persistInvalidDocumentExtraction(ctx, f.documentID, f.sourceID, "PAYSLIP",
		payslipExtraction{NetPay: "invalid", Confidence: .3}, .3, "test-model", fmt.Errorf("unrepairable extraction")); err != nil {
		t.Fatal(err)
	}
	var documentStatus, sourceStatus string
	var reviews, invalid int
	if err := f.pool.QueryRow(ctx, `SELECT d.status,s.processing_status,
		(SELECT count(*) FROM review_item WHERE document_id=d.id),
		(SELECT count(*) FROM document_extraction WHERE document_id=d.id AND stage='PAYSLIP' AND NOT validated)
		FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, f.documentID).Scan(&documentStatus, &sourceStatus, &reviews, &invalid); err != nil {
		t.Fatal(err)
	}
	if documentStatus != "FAILED" || sourceStatus != "FAILED" || reviews != 0 || invalid != 1 {
		t.Fatalf("machine failure document=%s source=%s reviews=%d evidence=%d", documentStatus, sourceStatus, reviews, invalid)
	}
}
