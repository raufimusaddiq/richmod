package reviewdec

import "testing"

func TestPresetsHaveCompleteKnownContracts(t *testing.T) {
	for _, reason := range []string{
		"CYCLE_RESIDUAL_ALLOCATION", "WEALTH_OBSERVATION_CONFIRMATION",
		"DOCUMENT_EXTRACTION_LOW_CONFIDENCE", "DOCUMENT_CLASSIFICATION",
		"PAYSLIP_CONFIRMATION", "MISSING_PAY_DATE", "TRANSFER_CLASSIFICATION",
		"CONFLICTING_EVIDENCE", "POSSIBLE_DUPLICATE",
		"UNKNOWN_MERCHANT", "AMBIGUOUS_CATEGORY",
	} {
		decision, ok := Preset(reason, "test", "subject")
		if !ok || decision.ReasonCode != reason || decision.DecisionClass == "" || decision.InteractionMode == "" || decision.WhyNotAuto == "" || len(decision.AllowedActions) == 0 {
			t.Fatalf("incomplete preset %s: %+v, ok=%t", reason, decision, ok)
		}
		decision.Provenance["test"] = true
	}
	if _, ok := Preset("UNKNOWN_REASON", "test", "subject"); ok {
		t.Fatal("unknown reasons must not produce a persistable zero decision")
	}
}

func TestPossibleDuplicateOffersBoundedRelationshipChoices(t *testing.T) {
	decision, ok := Preset("POSSIBLE_DUPLICATE", "transaction", "subject")
	if !ok || len(decision.AllowedActions) != 3 || decision.AllowedActions[0] != "MERGE_EXISTING" || decision.AllowedActions[1] != "CONFIRM_NEW_TRANSFER" || decision.AllowedActions[2] != "IGNORE" {
		t.Fatalf("unexpected duplicate contract: %+v, ok=%t", decision, ok)
	}
}
