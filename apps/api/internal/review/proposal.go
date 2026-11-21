package review

import "encoding/json"

type storedDecision struct {
	ReasonCode     string         `json:"reasonCode"`
	KnownFacts     map[string]any `json:"knownFacts"`
	ProposedFacts  map[string]any `json:"proposedFacts"`
	MissingFacts   []string       `json:"missingFacts"`
	WhyNotAuto     string         `json:"whyNotAutoConfirm"`
	AllowedActions []string       `json:"allowedActions"`
	Provenance     map[string]any `json:"decisionProvenance"`
}

func (s storedDecision) resolvedEntity(key string) string {
	if s.Provenance == nil {
		return ""
	}
	value, _ := s.Provenance[key].(string)
	return value
}

func proposalFacts(decision []byte) storedDecision {
	var stored storedDecision
	if len(decision) == 0 {
		return stored
	}
	if err := json.Unmarshal(decision, &stored); err != nil {
		return storedDecision{}
	}
	return stored
}

func confirmationBlockers(decision []byte, dateSupplied, categorySupplied, merchantSupplied bool) []string {
	stored := proposalFacts(decision)
	var blocked []string
	for _, fact := range stored.MissingFacts {
		switch fact {
		case "transaction_at":
			if !dateSupplied {
				blocked = append(blocked, fact)
			}
		case "category":
			if !categorySupplied {
				blocked = append(blocked, fact)
			}
		case "merchant":
			if !merchantSupplied {
				blocked = append(blocked, fact)
			}
		}
	}
	return blocked
}
