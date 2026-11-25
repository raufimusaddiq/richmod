package telegram

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestNativeV2CorrectionReference(t *testing.T) {
	tools := NativeFinanceTools([]string{"belanja"}, false, false)
	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tool.Name] = true
	}
	args, _ := json.Marshal(map[string]any{"target_ref": "tx_2", "search_text": nil, "period": "THIS_MONTH", "from_date": nil, "to_date": nil, "category_slug": nil, "description": "koreksi", "date_reference": nil, "explicit_date": nil, "local_time": nil})
	call := gateway.ToolCall{Name: "propose_transaction_correction", Arguments: args}
	if _, err := ValidateNativeToolCall(call); err != nil {
		t.Fatalf("reference correction rejected: %v", err)
	}
	args, _ = json.Marshal(map[string]any{"target_ref": "tx_2", "search_text": nil, "period": nil, "from_date": nil, "to_date": nil, "category_slug": nil, "description": "koreksi", "date_reference": nil, "explicit_date": nil, "local_time": nil})
	if _, err := ValidateNativeToolCall(gateway.ToolCall{Name: "propose_transaction_correction", Arguments: args}); err != nil {
		t.Fatalf("reference correction without period rejected: %v", err)
	}
	args, _ = json.Marshal(map[string]any{"target_ref": nil, "search_text": "kopi", "period": nil, "from_date": nil, "to_date": nil, "category_slug": nil, "description": "koreksi", "date_reference": nil, "explicit_date": nil, "local_time": nil})
	if _, err := ValidateNativeToolCall(gateway.ToolCall{Name: "propose_transaction_correction", Arguments: args}); err == nil || !strings.Contains(err.Error(), "CURRENT_CYCLE") {
		t.Fatalf("search correction without period: err=%v, want the allowed periods named", err)
	}
}

func TestRecordTransferContractCarriesNoPurpose(t *testing.T) {
	internal := transferArgs{Amount: "3000000", SourceAccountHint: "Jago", DateReference: "TODAY"}
	if err := validateTypedArgs(&internal); err != nil {
		t.Fatalf("destination-less transfer rejected: %v", err)
	}
	wrong := "RDN"
	investment := transferArgs{Amount: "3000000", SourceAccountHint: "Jago", DestinationWealthAccountHint: &wrong, DateReference: "TODAY"}
	if err := validateTypedArgs(&investment); err != nil {
		t.Fatalf("hinted investment transfer rejected: %v", err)
	}
	tools := NativeFinanceTools(nil, false, false)
	for _, tool := range tools {
		if tool.Name != "record_transfer" {
			continue
		}
		encoded, _ := json.Marshal(tool.Parameters)
		if strings.Contains(string(encoded), `"purpose"`) {
			t.Fatalf("record_transfer must not expose a purpose argument: %s", encoded)
		}
		return
	}
	t.Fatal("record_transfer missing")
}
