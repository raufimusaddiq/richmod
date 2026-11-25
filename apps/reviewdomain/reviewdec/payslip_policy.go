package reviewdec

import "slices"

func PayslipPolicy(decision Decision, reason string, hasPrimary bool) Decision {
	switch {
	case reason == "MISSING_PAY_DATE" && !hasPrimary:
		decision.AllowedActions = []string{"SET_PAY_DATE", "PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}
		if !slices.Contains(decision.MissingFacts, "salary_classification") {
			decision.MissingFacts = append(slices.Clone(decision.MissingFacts), "salary_classification")
		}
		decision.DecisionClass = ClassHumanPolicyChoice
		decision.InteractionMode = ModePolicyChoice
		decision.WhyNotAuto = "pay date is absent and the first salary source requires household classification"
	case reason == "MISSING_PAY_DATE":
		decision.AllowedActions = []string{"SET_PAY_DATE", "IGNORE"}
		decision.MissingFacts = slices.DeleteFunc(slices.Clone(decision.MissingFacts), func(fact string) bool { return fact == "salary_classification" })
		decision.DecisionClass = ClassEvidenceGap
		decision.InteractionMode = ModeSingleField
		decision.WhyNotAuto = kindByReason["MISSING_PAY_DATE"].Why
	case reason == "PAYSLIP_CONFIRMATION" && hasPrimary:
		decision.AllowedActions = []string{"HOUSEHOLD_POLICY", "ORDINARY_INCOME", "IGNORE"}
		if decision.Provenance["conflictingSalaryEvent"] == true {
			decision.AllowedActions = []string{"ORDINARY_INCOME", "IGNORE"}
		}
		decision.WhyNotAuto = payslipHeldReason(decision.Provenance)
	}
	return decision
}

func payslipHeldReason(provenance map[string]any) string {
	switch {
	case provenance["conflictingSalaryEvent"] == true:
		return "a salary for this payroll period is already recorded with different facts"
	case provenance["extractionBelowAutoConfirmFloor"] == true:
		return "the payslip was not read confidently enough to record automatically"
	case provenance["arithmeticOK"] == false:
		return "the payslip's gross pay, allowances and deductions do not add up to net pay"
	}
	return "the payslip needs confirmation before it is recorded as salary"
}
