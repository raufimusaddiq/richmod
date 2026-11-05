package document

import "testing"

// The locked re-check validates the whole decided candidate set, not just the
// chosen match: a non-chosen candidate that vanished (voided or merged by
// another lane) since the decision forces a retry too, so a stale candidate
// list can never decide the write.
func TestScreenshotRecheckValidatesTheWholeCandidateSet(t *testing.T) {
	chosen := matchCandidate{ID: "chosen", Score: 0.95}
	runnerUp := matchCandidate{ID: "runner-up", Score: 0.75}
	decided := []matchCandidate{chosen, runnerUp}
	if !sameScreenshotCandidates(decided, []matchCandidate{chosen, runnerUp}) {
		t.Fatal("an unchanged candidate set is the same decision")
	}
	for name, now := range map[string][]matchCandidate{
		"non-chosen candidate vanished": {chosen},
		"chosen candidate vanished":     {runnerUp},
		"new candidate appeared":        {chosen, runnerUp, {ID: "new", Score: 0.7}},
		"non-chosen score changed":      {chosen, {ID: "runner-up", Score: 0.9}},
		"chosen score changed":          {{ID: "chosen", Score: 0.8}, runnerUp},
		"everything vanished":           nil,
	} {
		if sameScreenshotCandidates(decided, now) {
			t.Fatalf("%s: a changed candidate set must force a re-decision", name)
		}
	}
	if !sameScreenshotCandidates(nil, nil) {
		t.Fatal("a row that had no candidates and still has none is unchanged")
	}
}
