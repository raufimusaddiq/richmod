package reviewdec

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// TestPresetContractsGolden pins the stored ReviewDecision for every active review
// reason. The agent, Telegram and the Review Inbox all read this stored shape, so
// a refactor of where review semantics are declared must leave it byte-identical.
func TestPresetContractsGolden(t *testing.T) {
	out := map[string]json.RawMessage{}
	for _, reason := range ActiveReasons() {
		decision, ok := Preset(reason, "test", "subject")
		if !ok {
			t.Fatalf("active reason %s has no preset", reason)
		}
		encoded, err := decision.JSON()
		if err != nil {
			t.Fatalf("%s: %v", reason, err)
		}
		out[reason] = encoded
	}
	got, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const path = "testdata/preset_contracts.golden.json"
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("preset contracts changed; review the diff and rerun with -update if intended\n%s", got)
	}
}
