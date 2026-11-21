package reviewdec

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
