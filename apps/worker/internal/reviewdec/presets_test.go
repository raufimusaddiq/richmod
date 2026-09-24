package reviewdec

import "testing"

func TestPresetsHaveCompleteKnownContracts(t *testing.T) {
	for _, reason := range []string{
		"CYCLE_RESIDUAL_ALLOCATION", "WEALTH_OBSERVATION_CONFIRMATION",
		"DOCUMENT_EXTRACTION_LOW_CONFIDENCE", "DOCUMENT_CLASSIFICATION",
		"PAYSLIP_CONFIRMATION", "MISSING_PAY_DATE", "TRANSFER_CLASSIFICATION",
		"CONFLICTING_EVIDENCE",
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
