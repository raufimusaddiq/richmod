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
		Provenance:     map[string]any{},
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
		base.AllowedActions = []string{"REPROCESS_DOCUMENT", "IGNORE"}
		base.InteractionMode = ModeSingleField
		base.WhyNotAuto = "the document extraction could not be validated deterministically"
	case "DOCUMENT_CLASSIFICATION":
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"document_type"}
		base.AllowedActions = []string{"REPROCESS_DOCUMENT", "IGNORE"}
		base.InteractionMode = ModeSingleField
		base.WhyNotAuto = "the document type could not be classified with enough confidence"
	case "UNKNOWN_BANK_TEMPLATE":
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"transaction_semantics"}
		base.AllowedActions = []string{"COMPLETE_BANK_FACTS", "IGNORE"}
		base.InteractionMode = ModeBoundedChoice
		base.WhyNotAuto = "the bank evidence could not be mapped to a supported transaction type"
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
	case "MISSING_TRANSACTION_DATE":
		// PRD §10: a source fact that is genuinely absent. The internal
		// received-at fallback is provenance, not a known transaction time.
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"transaction_at"}
		base.AllowedActions = []string{"SET_PAY_DATE", "IGNORE"}
		base.InteractionMode = ModeSingleField
		base.WhyNotAuto = "the source did not print or state the transaction date"
	case "TRANSACTION_FACTS_MISSING":
		// Compound residual: category plus date. One reason code must not hide
		// either dimension, so the contract names both explicitly.
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"category", "transaction_at"}
		base.AllowedActions = []string{"CONFIRM_REVIEW", "SET_PAY_DATE", "IGNORE"}
		base.InteractionMode = ModeSingleField
		base.WhyNotAuto = "the source did not support a category or a transaction date"
	case "TRANSFER_CLASSIFICATION":
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"transfer_relationship"}
		base.AllowedActions = []string{"CLASSIFY_TRANSFER", "MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"}
		base.InteractionMode = ModeBoundedChoice
		base.WhyNotAuto = "the transfer relationship is ambiguous, so the household must classify it"
	case "FINANCIAL_EMAIL_RESOLUTION":
		// The producer writes a decision naming only the still-unresolved entity
		// (funding_account and/or wealth_account); this preset is the shape used when
		// no narrower decision is available, so the review still carries the bounded
		// entity-resolution action rather than a zero decision.
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"funding_account", "wealth_account"}
		base.AllowedActions = []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"}
		base.InteractionMode = ModeBoundedChoice
		base.WhyNotAuto = "an entity the evidence identifies only in prose must be bound by the household"
	case "UNKNOWN_MERCHANT", "AMBIGUOUS_CATEGORY":
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"category"}
		if reason == "UNKNOWN_MERCHANT" {
			base.KnownFacts["merchant"] = nil
			base.WhyNotAuto = "merchant is not present in the evidence; category still requires a human decision"
		} else {
			base.WhyNotAuto = "the category is not supported strongly enough to confirm"
		}
		base.AllowedActions = []string{"CONFIRM_REVIEW", "IGNORE"}
		base.InteractionMode = ModeSingleField
	case "UNKNOWN_PURPOSE":
		base.DecisionClass = ClassEvidenceGap
		base.MissingFacts = []string{"transaction_semantics"}
		base.AllowedActions = []string{"CONFIRM_REVIEW", "IGNORE"}
		base.InteractionMode = ModeSingleField
		base.WhyNotAuto = "the transaction purpose is not supported strongly enough to classify"
	case "MANUAL_CORRECTION":
		base.DecisionClass = ClassCorrectionConfirmation
		base.MissingFacts = []string{"correction_details"}
		base.AllowedActions = []string{"CONFIRM_REVIEW", "IGNORE"}
		base.InteractionMode = ModeSingleField
		base.WhyNotAuto = "the extracted payroll transaction needs a human correction or confirmation"
	case "CONFLICTING_EVIDENCE":
		base.DecisionClass = ClassDuplicateAmbiguity
		base.MissingFacts = []string{"duplicate_relationship"}
		base.AllowedActions = []string{"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"}
		base.InteractionMode = ModeConflictResolution
		base.WhyNotAuto = "two sources disagree about the same provider reference, so Go refuses to guess"
	case "POSSIBLE_DUPLICATE":
		base.DecisionClass = ClassDuplicateAmbiguity
		base.MissingFacts = []string{"duplicate_relationship"}
		base.AllowedActions = []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"}
		base.InteractionMode = ModeBoundedChoice
		base.WhyNotAuto = "a plausible duplicate exists; choose the matching event, confirm as new, or ignore"
	default:
		return Decision{}, false
	}
	return base, true
}
