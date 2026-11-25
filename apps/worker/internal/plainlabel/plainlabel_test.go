package plainlabel

import (
	"strings"
	"testing"
)

type typedFact struct {
	Kind    string `json:"kind"`
	Purpose string `json:"purpose"`
}

func TestFactsNeverHandOutCodes(t *testing.T) {
	got := Facts(map[string]any{
		"amount_idr": "25000",
		"period":     map[string]any{"kind": "SALARY_CYCLE"},
		"items":      []typedFact{{Kind: "CYCLE_RESIDUAL_ALLOCATION", Purpose: "ASSET_PURCHASE"}, {Kind: "SOME_NEW_REASON"}},
		"currency":   "IDR",
	})
	encoded := strings.ToUpper(strings.Join(flatten(got), " "))
	for _, code := range []string{"SALARY_CYCLE", "CYCLE_RESIDUAL_ALLOCATION", "ASSET_PURCHASE", "SOME_NEW_REASON"} {
		if strings.Contains(encoded, code) {
			t.Fatalf("%s leaked into %v", code, got)
		}
	}
	if got["amount_idr"] != "25000" || got["currency"] != "IDR" {
		t.Fatalf("plain values must pass through: %v", got)
	}
}

func flatten(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case map[string]any:
		var out []string
		for _, item := range typed {
			out = append(out, flatten(item)...)
		}
		return out
	case []any:
		var out []string
		for _, item := range typed {
			out = append(out, flatten(item)...)
		}
		return out
	}
	return nil
}
