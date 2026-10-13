package reviewdec

// Kind is the single declaration of one review kind (ADR-054). Every surface that
// needs to know what a review kind asks reads this table instead of keeping its
// own copy: the creation-time ReviewDecision (Preset), the producing-reason set
// (ActiveReasons), the Review Inbox fallback actions (InboxActions) and the
// agent's resolve_review action enum (AgentActions).
//
// The three action lists are deliberately separate fields. They are different
// surface projections that already differ per kind (vocabulary, capability and
// taxonomy), pinned by the golden tests; they are declared side by side so the
// differences are visible in one row rather than hidden in three switches.
type Kind struct {
	Reason string

	// Stored contract. A kind with no Actions has no producer preset: it exists
	// only as a surface key (an Inbox or agent kind that is not a stored reason).
	Class       string
	Missing     []string
	Actions     []string
	Mode        string
	Why         string
	Consequence Consequence
	Affected    []string
	// KnownNil names known facts that are present but unknown (stored as null).
	KnownNil []string

	// Inbox is what the Review Inbox offers when an item carries no stored
	// actions. Empty means IGNORE only.
	Inbox []string
	// Agent is the resolve_review enum for the agent. Empty means CONFIRM, IGNORE.
	Agent []string
}

var kinds = []Kind{
	{
		Reason: "CYCLE_RESIDUAL_ALLOCATION", Class: ClassHumanPolicyChoice,
		Missing: []string{"residual_allocation"},
		Actions: []string{"ALLOCATE_RETAINED_BALANCE", "TRANSACTION_MISSING", "LEAVE_UNALLOCATED"},
		Mode:    ModePolicyChoice,
		Why:     "allocating retained cycle money is household intent and must not be inferred",
		Inbox:   []string{"ALLOCATE_RETAINED_BALANCE", "TRANSACTION_MISSING", "LEAVE_UNALLOCATED"},
		Agent:   []string{"ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED", "TRANSACTION_MISSING"},
	},
	{
		// PREPARE_SNAPSHOT is voluntary richer-workflow navigation, not a canonical
		// completion: it must not appear in the allowed actions or the remaining
		// actions would look unavailable.
		Reason: "WEALTH_OBSERVATION_CONFIRMATION", Class: ClassEvidenceGap,
		Missing: []string{"wealth_snapshot_confirmation"},
		Actions: []string{"SET_WEALTH_ACCOUNT", "IGNORE"},
		Mode:    ModeBoundedChoice,
		Why:     "a wealth value is an observation; the complete active account set must be confirmed",
		Inbox:   []string{"SET_WEALTH_ACCOUNT", "IGNORE"},
	},
	{
		Reason: "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", Class: ClassEvidenceGap,
		Missing: []string{"document_extraction"},
		Actions: []string{"REPROCESS_DOCUMENT", "IGNORE"},
		Mode:    ModeSingleField,
		Why:     "the document extraction could not be validated deterministically",
		Inbox:   []string{"REPROCESS_DOCUMENT", "IGNORE"},
		Agent:   []string{"REPROCESS_DOCUMENT", "IGNORE"},
	},
	{
		Reason: "DOCUMENT_CLASSIFICATION", Class: ClassEvidenceGap,
		Missing: []string{"document_type"},
		Actions: []string{"REPROCESS_DOCUMENT", "IGNORE"},
		Mode:    ModeSingleField,
		Why:     "the document type could not be classified with enough confidence",
		Inbox:   []string{"REPROCESS_DOCUMENT", "IGNORE"},
		Agent:   []string{"REPROCESS_DOCUMENT", "IGNORE"},
	},
	{
		Reason: "UNKNOWN_BANK_TEMPLATE", Class: ClassEvidenceGap,
		Missing: []string{"transaction_semantics"},
		Actions: []string{"COMPLETE_BANK_FACTS", "IGNORE"},
		Mode:    ModeBoundedChoice,
		Why:     "the bank evidence could not be mapped to a supported transaction type",
		Inbox:   []string{"COMPLETE_BANK_FACTS", "IGNORE"},
		Agent:   []string{"COMPLETE_BANK_FACTS", "IGNORE"},
	},
	{
		Reason: "PAYSLIP_CONFIRMATION", Class: ClassHumanPolicyChoice,
		Missing: []string{"salary_classification"},
		Actions: []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"},
		Mode:    ModePolicyChoice,
		Why:     "the first salary from a source is a human policy choice",
		Inbox:   []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"},
		Agent:   []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"},
	},
	{
		Reason: "MISSING_PAY_DATE", Class: ClassEvidenceGap,
		Missing: []string{"transaction_at"},
		Actions: []string{"SET_PAY_DATE", "IGNORE"},
		Mode:    ModeSingleField,
		Why:     "the pay date is not present in the evidence",
		Inbox:   []string{"SET_PAY_DATE", "IGNORE"},
		Agent:   []string{"SET_PAY_DATE", "IGNORE"},
	},
	{
		// A source fact that is genuinely absent. The internal received-at fallback
		// is provenance, not a known transaction time.
		Reason: "MISSING_TRANSACTION_DATE", Class: ClassEvidenceGap,
		Missing: []string{"transaction_at"},
		Actions: []string{"SET_PAY_DATE", "IGNORE"},
		Mode:    ModeSingleField,
		Why:     "the source did not print or state the transaction date",
	},
	{
		// Compound residual: category plus date. One reason code must not hide
		// either dimension, so the contract names both explicitly.
		Reason: "TRANSACTION_FACTS_MISSING", Class: ClassEvidenceGap,
		Missing: []string{"category", "transaction_at"},
		Actions: []string{"CONFIRM_REVIEW", "SET_PAY_DATE", "IGNORE"},
		Mode:    ModeSingleField,
		Why:     "the source did not support a category or a transaction date",
	},
	{
		// The ordinary completion vocabulary is the classification the shared
		// classifier accepts (Review Inbox CLASSIFY_TRANSFER, Telegram
		// OWN_ACCOUNT/HOUSEHOLD_ACCOUNT/EXPENSE/ASSET_PURCHASE). MERGE_EXISTING and
		// CONFIRM_NEW_TRANSFER belong to the reconciliation chooser, which the
		// surfaces add only when the case actually carries candidates.
		Reason: "TRANSFER_CLASSIFICATION", Class: ClassEvidenceGap,
		Missing: []string{"transfer_relationship"},
		Actions: []string{"CLASSIFY_TRANSFER", "OWN_ACCOUNT", "HOUSEHOLD_ACCOUNT", "INVESTMENT_ACCOUNT", "EXPENSE", "ASSET_PURCHASE", "IGNORE"},
		Mode:    ModeBoundedChoice,
		Why:     "the transfer relationship is ambiguous, so the household must classify it",
		Agent:   []string{"EXPENSE", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER", "INVESTMENT_TRANSFER", "ASSET_PURCHASE", "IGNORE"},
	},
	{
		// The producer writes a decision naming only the still-unresolved entity
		// (funding_account and/or wealth_account); this is the shape used when no
		// narrower decision is available, so the review still carries the bounded
		// entity-resolution action rather than a zero decision.
		Reason: "FINANCIAL_EMAIL_RESOLUTION", Class: ClassEvidenceGap,
		Missing: []string{"funding_account", "wealth_account"},
		Actions: []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"},
		Mode:    ModeBoundedChoice,
		Why:     "an entity the evidence identifies only in prose must be bound by the household",
		Inbox:   []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"},
		Agent:   []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"},
	},
	{
		// The producer overrides Missing/Affected/Consequence with the exact
		// predicate that failed; this default holds the action contract for a review
		// whose bounded plane could not resolve anything. No canonical transaction
		// was written, so the only bounded action is to acknowledge it.
		Reason: "FINANCIAL_EMAIL_FACTS", Class: ClassEvidenceGap,
		Missing: []string{"evidence_support"},
		Actions: []string{"IGNORE"},
		Mode:    ModeSingleField,
		Why:     "the provider email does not support all extracted financial facts; no transaction was written",
		Agent:   []string{"IGNORE"},
	},
	{
		Reason: "UNKNOWN_MERCHANT", Class: ClassEvidenceGap,
		Missing:  []string{"merchant", "category"},
		Actions:  []string{"CONFIRM_REVIEW", "IGNORE"},
		Mode:     ModeSingleField,
		Why:      "merchant is not present in the evidence; collect it before resolving the category",
		KnownNil: []string{"merchant"},
		Agent:    []string{"CONFIRM", "ASSET_PURCHASE", "IGNORE"},
	},
	{
		Reason: "AMBIGUOUS_CATEGORY", Class: ClassEvidenceGap,
		Missing: []string{"category"},
		Actions: []string{"CONFIRM_REVIEW", "IGNORE"},
		Mode:    ModeSingleField,
		Why:     "the category is not supported strongly enough to confirm",
		Agent:   []string{"CONFIRM", "ASSET_PURCHASE", "IGNORE"},
	},
	{
		Reason: "UNKNOWN_PURPOSE", Class: ClassEvidenceGap,
		Missing: []string{"transaction_semantics"},
		Actions: []string{"CONFIRM_REVIEW", "IGNORE"},
		Mode:    ModeSingleField,
		Why:     "the transaction purpose is not supported strongly enough to classify",
	},
	{
		Reason: "MANUAL_CORRECTION", Class: ClassCorrectionConfirmation,
		Missing: []string{"correction_details"},
		Actions: []string{"CONFIRM_REVIEW", "IGNORE"},
		Mode:    ModeSingleField,
		Why:     "the extracted payroll transaction needs a human correction or confirmation",
	},
	{
		Reason: "CONFLICTING_EVIDENCE", Class: ClassDuplicateAmbiguity,
		Missing: []string{"duplicate_relationship"},
		Actions: []string{"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"},
		Mode:    ModeConflictResolution,
		Why:     "two sources disagree about the same provider reference, so Go refuses to guess",
	},
	{
		// A document that may repeat a recorded transaction can merge into one of
		// the server-computed candidates (an opaque ref), be kept as a separate
		// event, or be dismissed.
		Reason: "POSSIBLE_DUPLICATE", Class: ClassDuplicateAmbiguity,
		Missing: []string{"duplicate_relationship"},
		Actions: []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"},
		Mode:    ModeBoundedChoice,
		Why:     "a plausible duplicate exists; choose the matching event, confirm as new, or ignore",
		Agent:   []string{"MERGE_EXISTING", "CONFIRM", "IGNORE"},
	},
	{
		// The receipt's own total, date, and category may all be known; only the
		// line arithmetic disagrees, and that cannot prove the printed total wrong.
		Reason: "RECEIPT_MISMATCH", Class: ClassCorrectionConfirmation,
		Consequence: QualitySignal, Affected: []string{"receipt_arithmetic"},
		Actions: []string{"CONFIRM_REVIEW", "IGNORE"},
		Mode:    ModeOneTapConfirmation,
		Why:     "the printed lines do not reconcile with the printed total",
	},

	// Surface-only kinds: no producer preset. They are keys one surface uses for a
	// review that is not stored under this name.
	{Reason: "MISSING_AMOUNT", Inbox: []string{"SET_AMOUNT", "IGNORE"}},
	{Reason: "SALARY_SOURCE_CONFIRMATION", Agent: []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}},
	{Reason: "TRANSFER_RECONCILIATION", Agent: []string{"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"}},
	{Reason: "WEALTH_OBSERVATION", Agent: []string{"SET_WEALTH_ACCOUNT", "RECORD_ASSET_PURCHASE", "IGNORE"}},
}

var kindByReason = func() map[string]*Kind {
	index := make(map[string]*Kind, len(kinds))
	for i := range kinds {
		index[kinds[i].Reason] = &kinds[i]
	}
	return index
}()

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

// ActiveReasons returns the review reasons that have a live producer, in table
// order. It is a copy so a caller cannot reorder or truncate the list the
// Telegram capability gate reads.
func ActiveReasons() []string {
	reasons := make([]string, 0, len(kinds))
	for i := range kinds {
		if len(kinds[i].Actions) > 0 {
			reasons = append(reasons, kinds[i].Reason)
		}
	}
	return reasons
}

// InboxActions returns the actions the Review Inbox offers for a kind that
// carries no stored actions. An unknown kind may only be ignored.
func InboxActions(reason string) []string {
	if kind, ok := kindByReason[reason]; ok && len(kind.Inbox) > 0 {
		return cloneStrings(kind.Inbox)
	}
	return []string{"IGNORE"}
}

// AgentActions returns the resolve_review action enum for an agent review kind.
// An unlisted kind may be confirmed or ignored.
func AgentActions(reason string) []string {
	if kind, ok := kindByReason[reason]; ok && len(kind.Agent) > 0 {
		return cloneStrings(kind.Agent)
	}
	return []string{"CONFIRM", "IGNORE"}
}

// AllAgentActions returns every action any agent review kind can offer, derived
// from the table so it cannot drift from AgentActions. The agent's native-call
// validator uses it as the closed vocabulary for membership checks.
func AllAgentActions() []string {
	seen := map[string]bool{}
	var all []string
	add := func(actions []string) {
		for _, action := range actions {
			if !seen[action] {
				seen[action] = true
				all = append(all, action)
			}
		}
	}
	add(AgentActions(""))
	for i := range kinds {
		add(kinds[i].Agent)
	}
	return all
}
