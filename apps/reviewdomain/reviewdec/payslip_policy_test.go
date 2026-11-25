package reviewdec

import (
	"reflect"
	"testing"
)

func TestPayslipPolicy(t *testing.T) {
	date, _ := Preset("MISSING_PAY_DATE", "proposal", "id")
	if got := PayslipPolicy(date, "MISSING_PAY_DATE", true); !reflect.DeepEqual(got.MissingFacts, []string{"transaction_at"}) || !reflect.DeepEqual(got.AllowedActions, []string{"SET_PAY_DATE", "IGNORE"}) || got.InteractionMode != ModeSingleField {
		t.Fatalf("date-only: %+v", got)
	}
	first := PayslipPolicy(date, "MISSING_PAY_DATE", false)
	if !reflect.DeepEqual(first.MissingFacts, []string{"transaction_at", "salary_classification"}) || !reflect.DeepEqual(first.AllowedActions, []string{"SET_PAY_DATE", "PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}) || first.DecisionClass != ClassHumanPolicyChoice || first.InteractionMode != ModePolicyChoice {
		t.Fatalf("first salary and date: %+v", first)
	}
	if again := PayslipPolicy(first, "MISSING_PAY_DATE", false); !reflect.DeepEqual(again.MissingFacts, first.MissingFacts) {
		t.Fatalf("not idempotent: %v", again.MissingFacts)
	}
	if back := PayslipPolicy(first, "MISSING_PAY_DATE", true); !reflect.DeepEqual(back.MissingFacts, []string{"transaction_at"}) || back.DecisionClass != ClassEvidenceGap {
		t.Fatalf("policy change did not refresh a stored decision: %+v", back)
	}
	confirm, _ := Preset("PAYSLIP_CONFIRMATION", "proposal", "id")
	held := confirm
	held.Provenance = map[string]any{"arithmeticOK": false, "extractionBelowAutoConfirmFloor": false}
	if got := PayslipPolicy(held, "PAYSLIP_CONFIRMATION", true); !reflect.DeepEqual(got.AllowedActions, []string{"HOUSEHOLD_POLICY", "ORDINARY_INCOME", "IGNORE"}) || got.WhyNotAuto != "the payslip's gross pay, allowances and deductions do not add up to net pay" || WhyLine(got) == "" {
		t.Fatalf("primary-known confirmation must offer recording as salary and name why it was held: %+v", got)
	}
	conflict := confirm
	conflict.Provenance = map[string]any{"conflictingSalaryEvent": true}
	if got := PayslipPolicy(conflict, "PAYSLIP_CONFIRMATION", true); !reflect.DeepEqual(got.AllowedActions, []string{"ORDINARY_INCOME", "IGNORE"}) || got.WhyNotAuto != "a salary for this payroll period is already recorded with different facts" {
		t.Fatalf("a period that already has a different salary must not offer recording it as salary: %+v", got)
	}
	if got := PayslipPolicy(confirm, "PAYSLIP_CONFIRMATION", false); !reflect.DeepEqual(got.AllowedActions, confirm.AllowedActions) {
		t.Fatalf("first-salary confirmation changed: %v", got.AllowedActions)
	}
	if len(confirm.AllowedActions) != 3 {
		t.Fatal("PayslipPolicy mutated its input")
	}
}
