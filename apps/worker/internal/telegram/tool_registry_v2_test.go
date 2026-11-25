package telegram

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestNativeV2ConditionalToolsAndCorrectionReference(t *testing.T) {
	tools := NativeFinanceTools([]string{"belanja"}, false, false, false, "", true, true, "")
	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tool.Name] = true
	}
	if !seen["resolve_salary_choice"] || !seen["resolve_merchant_learning"] {
		t.Fatal("conditional native tools missing")
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

func TestReviewActionMatrixIsBoundedByType(t *testing.T) {
	for _, kind := range []string{"PAYSLIP_CONFIRMATION", "MISSING_PAY_DATE"} {
		if got := reviewdec.AgentActions(kind); strings.Join(got, ",") != "IGNORE" {
			t.Fatalf("%s actions=%v", kind, got)
		}
	}
	if got := reviewdec.AgentActions("UNKNOWN_BANK_TEMPLATE"); strings.Join(got, ",") != "COMPLETE_BANK_FACTS,IGNORE" {
		t.Fatalf("bank actions=%v", got)
	}
	for _, kind := range []string{"DOCUMENT_EXTRACTION_LOW_CONFIDENCE", "DOCUMENT_CLASSIFICATION"} {
		if got := reviewdec.AgentActions(kind); strings.Join(got, ",") != "REPROCESS_DOCUMENT,IGNORE" {
			t.Fatalf("%s actions=%v", kind, got)
		}
	}
	if got := reviewdec.AgentActions("TRANSFER_RECONCILIATION"); strings.Join(got, ",") != "MERGE_EXISTING,CONFIRM_NEW_TRANSFER,IGNORE" {
		t.Fatalf("reconciliation actions=%v", got)
	}
	if got := reviewdec.AgentActions("WEALTH_OBSERVATION"); strings.Join(got, ",") != "SET_WEALTH_ACCOUNT,RECORD_ASSET_PURCHASE,IGNORE" {
		t.Fatalf("wealth actions=%v", got)
	}
	if got := reviewdec.AgentActions("AMBIGUOUS_CATEGORY"); strings.Join(got, ",") != "CONFIRM,IGNORE" {
		t.Fatalf("expense review actions=%v", got)
	}
	for _, tool := range NativeFinanceTools(nil, false, false, true, "WEALTH_OBSERVATION", false, false, "WEALTH_OBSERVATION") {
		if tool.Name == "resolve_review" {
			encoded, _ := json.Marshal(tool.Parameters)
			if !strings.Contains(string(encoded), "RECORD_ASSET_PURCHASE") || !strings.Contains(string(encoded), "source_account_hint") {
				t.Fatalf("wealth review schema=%s", encoded)
			}
			return
		}
	}
	t.Fatal("wealth resolve_review missing")
}

func TestNativeReviewSchemaUsesOpaqueReconciliationReferences(t *testing.T) {
	tools := NativeFinanceTools(nil, false, false, true, "TRANSFER_RECONCILIATION", false, false, "TRANSFER_RECONCILIATION")
	for _, tool := range tools {
		if tool.Name != "resolve_review" {
			continue
		}
		encoded, _ := json.Marshal(tool.Parameters)
		text := string(encoded)
		if !strings.Contains(text, "candidate_ref") || strings.Contains(text, "transaction_id") || !strings.Contains(text, "MERGE_EXISTING") {
			t.Fatalf("schema=%s", text)
		}
		return
	}
	t.Fatal("resolve_review missing")
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
	tools := NativeFinanceTools(nil, false, false, false, "", true, true, "")
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
