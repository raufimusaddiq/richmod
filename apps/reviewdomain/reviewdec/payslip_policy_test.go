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
	// Reading back a stored decision must converge, whichever way policy moved.
	if again := PayslipPolicy(first, "MISSING_PAY_DATE", false); !reflect.DeepEqual(again.MissingFacts, first.MissingFacts) {
		t.Fatalf("not idempotent: %v", again.MissingFacts)
	}
	if back := PayslipPolicy(first, "MISSING_PAY_DATE", true); !reflect.DeepEqual(back.MissingFacts, []string{"transaction_at"}) || back.DecisionClass != ClassEvidenceGap {
		t.Fatalf("policy change did not refresh a stored decision: %+v", back)
	}
	confirm, _ := Preset("PAYSLIP_CONFIRMATION", "proposal", "id")
	if got := PayslipPolicy(confirm, "PAYSLIP_CONFIRMATION", true); !reflect.DeepEqual(got.AllowedActions, []string{"ORDINARY_INCOME", "IGNORE"}) {
		t.Fatalf("primary-known confirmation: %v", got.AllowedActions)
	}
	if got := PayslipPolicy(confirm, "PAYSLIP_CONFIRMATION", false); !reflect.DeepEqual(got.AllowedActions, confirm.AllowedActions) {
		t.Fatalf("first-salary confirmation changed: %v", got.AllowedActions)
	}
	if len(confirm.AllowedActions) != 3 {
		t.Fatal("PayslipPolicy mutated its input")
	}
}
