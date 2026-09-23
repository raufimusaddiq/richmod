package review

import "encoding/json"

// storedDecision is the server-owned subset of a ReviewDecision the Inbox needs:
// what the review already knows, what Richmod proposes, and the exact dimensions
// still unresolved (PRD 13.1, 13.4).
type storedDecision struct {
	KnownFacts    map[string]any `json:"knownFacts"`
	ProposedFacts map[string]any `json:"proposedFacts"`
	MissingFacts  []string       `json:"missingFacts"`
	WhyNotAuto    string         `json:"whyNotAutoConfirm"`
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
