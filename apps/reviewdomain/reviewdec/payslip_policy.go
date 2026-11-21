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
		decision.AllowedActions = slices.DeleteFunc(slices.Clone(decision.AllowedActions), func(action string) bool { return action == "PRIMARY_SALARY" })
	}
	return decision
}
