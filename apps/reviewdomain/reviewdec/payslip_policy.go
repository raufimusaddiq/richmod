package reviewdec

import "slices"

// PayslipPolicy adjusts a payslip review's policy-dependent fields for whether the
// household already has a primary salary source. The stored decision is a snapshot
// of that policy, so both the creating worker and the Review Inbox read apply this
// one function; neither keeps its own copy of the rules (ADR-054).
//
//   - MISSING_PAY_DATE with no primary salary also needs the first-salary choice.
//   - MISSING_PAY_DATE with a primary salary asks for the date only.
//   - PAYSLIP_CONFIRMATION with a primary salary cannot promote this slip, so the
//     dead PRIMARY_SALARY action is dropped rather than advertised.
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
		decision.AllowedActions = slices.DeleteFunc(slices.Clone(decision.AllowedActions), func(action string) bool { return action == "PRIMARY_SALARY" })
	}
	return decision
}
