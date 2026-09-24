package review

import "encoding/json"

// storedDecision is the server-owned subset of a ReviewDecision the Inbox needs:
// what the review already knows, what Richmod proposes, and the exact dimensions
// still unresolved (PRD 13.1, 13.4).
type storedDecision struct {
	ReasonCode     string         `json:"reasonCode"`
	KnownFacts     map[string]any `json:"knownFacts"`
	ProposedFacts  map[string]any `json:"proposedFacts"`
	MissingFacts   []string       `json:"missingFacts"`
	WhyNotAuto     string         `json:"whyNotAutoConfirm"`
	Provenance     map[string]any `json:"provenance"`
	AllowedActions []string       `json:"allowedActions"`
}

// resolvedEntity answers "what has this review already resolved?" from the one
// place the producer records it. Reading it from the decision keeps the Inbox
// field-independent: a producer that resolves a new dimension does not need a new
// list API column (PRD 12, 37).
func (s storedDecision) resolvedEntity(key string) string {
	if s.Provenance == nil {
		return ""
	}
	value, _ := s.Provenance[key].(string)
	return value
}

// proposalFacts extracts that subset from a stored ReviewDecision. The Inbox
// renders the known facts read-only and asks only for what this reports as
// missing, so a card can never demand a fact the review already holds
// (PRD 3.3, 13.4). A missing or unreadable decision yields the zero value rather
// than a guessed one: the card then falls back to the full form.
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

// confirmationBlockers is the IR-02 canonical guard: a confirm may only proceed
// once every residual fact the stored ReviewDecision reported is supplied this
// turn. It runs inside the confirm transaction, so a legacy client or an old
// review card cannot skip a required date/category merely by omitting it.
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
