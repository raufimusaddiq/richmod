package reviewdec

// Preset returns the ReviewDecision contract for a review reason whose unresolved
// dimension is fixed by product policy rather than per-event evidence. Review-creating
// paths call it instead of hand-rolling a Decision, so every review explains itself
// the same way; the contract is declared once in the kind table (kinds.go).
//
// It reports false for an unknown reason or one that is only a surface key; callers
// treat that as no-contract rather than storing the zero decision.
func Preset(reason, subjectType, subjectID string) (Decision, bool) {
	kind, ok := kindByReason[reason]
	if !ok || len(kind.Actions) == 0 {
		return Decision{}, false
	}
	decision := Decision{
		Version:         Version,
		Subject:         Subject{Type: subjectType, ID: subjectID},
		ReasonCode:      reason,
		DecisionSource:  SourceDeterministic,
		DecisionClass:   kind.Class,
		KnownFacts:      map[string]any{},
		ProposedFacts:   map[string]any{},
		MissingFacts:    []string{},
		Provenance:      map[string]any{},
		EvidenceRefs:    []EvidenceRef{{Kind: subjectType, ID: subjectID}},
		Consequence:     kind.Consequence,
		AffectedFacts:   cloneStrings(kind.Affected),
		AllowedActions:  cloneStrings(kind.Actions),
		InteractionMode: kind.Mode,
		WhyNotAuto:      kind.Why,
	}
	if kind.Missing != nil {
		decision.MissingFacts = cloneStrings(kind.Missing)
	}
	if len(kind.Choices) > 0 {
		decision.Choices = append([]Choice(nil), kind.Choices...)
	}
	for _, key := range kind.KnownNil {
		decision.KnownFacts[key] = nil
	}
	return decision, true
}
