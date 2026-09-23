package reviewdec

// Presets are the ReviewDecision contracts for review reasons whose unresolved
// dimension is fixed by product policy rather than by per-event evidence (PRD
// §7, §16). Each review-creating path calls the preset instead of hand-rolling a
// Decision, so every review explains the same way why it exists and no path can
// add a required field the others do not have.
//
// A preset reports false for a reason it does not know; callers must treat that
// as no-contract rather than storing the zero decision.
func Preset(reason, subjectType, subjectID string) (Decision, bool) {
	base := Decision{
		Version:        Version,
		Subject:        Subject{Type: subjectType, ID: subjectID},
		ReasonCode:     reason,
		DecisionSource: SourceDeterministic,
		KnownFacts:     map[string]any{},
		MissingFacts:   []string{},
		EvidenceRefs:   []EvidenceRef{{Kind: subjectType, ID: subjectID}},
	}
	switch reason {
	case "CYCLE_RESIDUAL_ALLOCATION":
		base.DecisionClass = ClassHumanPolicyChoice
		base.MissingFacts = []string{"residual_allocation"}
		base.AllowedActions = []string{"ALLOCATE_RETAINED_BALANCE", "TRANSACTION_MISSING", "LEAVE_UNALLOCATED"}
		base.InteractionMode = ModePolicyChoice
		base.WhyNotAuto = "allocating retained cycle money is household intent and must not be inferred"
	case "WEALTH_OBSERVATION_CONFIRMATION":
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"wealth_snapshot_confirmation"}
		base.AllowedActions = []string{"PREPARE_SNAPSHOT", "SET_WEALTH_ACCOUNT", "IGNORE"}
		base.InteractionMode = ModeBoundedChoice
		base.WhyNotAuto = "a wealth value is an observation; the complete active account set must be confirmed"
	case "DOCUMENT_EXTRACTION_LOW_CONFIDENCE":
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"document_extraction"}
		base.AllowedActions = []string{"COMPLETE_BANK_FACTS", "IGNORE"}
		base.InteractionMode = ModeSingleField
		base.WhyNotAuto = "the document extraction could not be validated deterministically"
	case "DOCUMENT_CLASSIFICATION":
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"document_type"}
		base.AllowedActions = []string{"COMPLETE_BANK_FACTS", "IGNORE"}
		base.InteractionMode = ModeSingleField
		base.WhyNotAuto = "the document type could not be classified with enough confidence"
	case "PAYSLIP_CONFIRMATION":
		base.DecisionClass = ClassHumanPolicyChoice
		base.MissingFacts = []string{"salary_classification"}
		base.AllowedActions = []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}
		base.InteractionMode = ModePolicyChoice
		base.WhyNotAuto = "the first salary from a source is a human policy choice"
	case "MISSING_PAY_DATE":
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"transaction_at"}
		base.AllowedActions = []string{"SET_PAY_DATE", "IGNORE"}
		base.InteractionMode = ModeSingleField
		base.WhyNotAuto = "the pay date is not present in the evidence"
	default:
		return Decision{}, false
	}
	return base, true
}
