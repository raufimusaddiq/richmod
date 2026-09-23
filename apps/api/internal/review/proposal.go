package review

import "encoding/json"

// proposalFacts extracts the server-owned proposal and the exact unresolved
// dimensions from a stored ReviewDecision (PRD §13.1, §13.4). The Inbox renders
// the proposal and asks only for what this returns, so a card can never demand a
// fact the review already holds (PRD §3.3). A missing or unreadable decision
// yields an empty proposal rather than a guessed one.
func proposalFacts(decision []byte) (map[string]any, []string) {
	if len(decision) == 0 {
		return nil, nil
	}
	var stored struct {
		ProposedFacts map[string]any `json:"proposedFacts"`
		MissingFacts  []string       `json:"missingFacts"`
	}
	if err := json.Unmarshal(decision, &stored); err != nil {
		return nil, nil
	}
	return stored.ProposedFacts, stored.MissingFacts
}

// missingField reports whether one dimension is genuinely unresolved, so a card
// renders an input only for what the decision named (PRD §13.4).
func missingField(missing []string, field string) bool {
	for _, name := range missing {
		if name == field {
			return true
		}
	}
	return false
}
