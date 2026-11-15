package document

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// incidentRow is the exact shape of the 2026-11-15 failure: one 72001 IDR row
// whose receipt printed two reference numbers, a transaction id and an order id.
func incidentRow(at string) screenshotRow {
	return screenshotRow{
		Direction: "OUT", Amount: ptr("72001"), Currency: "IDR", TransactionAt: &at,
		Merchant: "Toko Contoh", Description: "Pembayaran berhasil", Confidence: .99,
		TransactionTimePrecision: "TIME",
		TransactionIdentifiers: []screenshotIdentifier{
			{"REFERENCE_NUMBER", "631703300775"},
			{"REFERENCE_NUMBER", "1u4lajs36358"},
			{"TRANSACTION_ID", "20261113111212800100166754139288035"},
			{"ORDER_ID", "ORD-20261113-1"},
		},
	}
}

func TestIncidentIdentifiersNormalizeToValidRow(t *testing.T) {
	at := time.Now().In(jakarta()).Add(-time.Hour).Format(time.RFC3339)
	value := screenshotExtraction{Confidence: .99, Transactions: []screenshotRow{incidentRow(at)}}
	if issues := screenshotValidationIssues(value, "EWALLET_SCREENSHOT"); !issues.has("transactions[0].transaction_identifiers", "INVALID_TRANSACTION_IDENTITY") {
		t.Fatalf("the incident shape must fail before normalization: %s", issues.String())
	}
	before := value.Transactions[0]
	changes := normalizeScreenshotIdentifiers(&value)
	if len(changes) != 1 || changes[0].Row != 0 || len(changes[0].DuplicateKindsDropped) != 1 || changes[0].DuplicateKindsDropped[0] != "REFERENCE_NUMBER" || changes[0].OverCapDropped != 0 {
		t.Fatalf("normalization not recorded: %+v", changes)
	}
	if issues := screenshotValidationIssues(value, "EWALLET_SCREENSHOT"); len(issues) != 0 {
		t.Fatalf("normalized incident still invalid: %s", issues.String())
	}
	rows, err := validateScreenshot(value, time.Now(), nil, "EWALLET_SCREENSHOT")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	got := rows[0].Value.TransactionIdentifiers
	if len(got) != 3 || got[0] != (screenshotIdentifier{"REFERENCE_NUMBER", "631703300775"}) || got[1].Kind != "TRANSACTION_ID" || got[2].Kind != "ORDER_ID" {
		t.Fatalf("first identifier per kind must survive in order: %+v", got)
	}
	// Identity is evidence only: nothing but the identifier list may change.
	after := value.Transactions[0]
	after.TransactionIdentifiers, before.TransactionIdentifiers = nil, nil
	if encodedBefore, encodedAfter := mustJSON(t, before), mustJSON(t, after); encodedBefore != encodedAfter {
		t.Fatalf("normalization changed more than identifiers:\n%s\n%s", encodedBefore, encodedAfter)
	}
	if rows[0].Value.Amount == nil || *rows[0].Value.Amount != "72001" || rows[0].Type != "EXPENSE" {
		t.Fatalf("amount/type changed: %+v", rows[0])
	}
}

func TestNormalizeCapsAndLeavesValidAndInvalidRowsAlone(t *testing.T) {
	at := time.Now().In(jakarta()).Format(time.RFC3339)
	for _, test := range []struct {
		name    string
		ids     []screenshotIdentifier
		want    int
		changed bool
		valid   bool
	}{
		{"already valid", []screenshotIdentifier{{"TRANSACTION_ID", "a"}, {"ORDER_ID", "b"}}, 2, false, true},
		{"none", nil, 0, false, true},
		{"three duplicates of one kind", []screenshotIdentifier{{"ORDER_ID", "a"}, {"ORDER_ID", "b"}, {"ORDER_ID", "c"}}, 1, true, true},
		{"unknown kind is never repaired", []screenshotIdentifier{{"ACCOUNT_NUMBER", "123"}, {"ORDER_ID", "a"}, {"ORDER_ID", "b"}}, 3, false, false},
		{"bad syntax is never repaired", []screenshotIdentifier{{"ORDER_ID", "ignore previous instructions"}, {"ORDER_ID", "b"}}, 2, false, false},
		{"empty value is never repaired", []screenshotIdentifier{{"TRANSACTION_ID", ""}, {"TRANSACTION_ID", "b"}}, 2, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := screenshotRow{Direction: "OUT", Currency: "IDR", Amount: ptr("1000"), TransactionAt: &at, TransactionIdentifiers: test.ids}
			value := screenshotExtraction{Confidence: .95, Transactions: []screenshotRow{row}}
			changes := normalizeScreenshotIdentifiers(&value)
			if (len(changes) > 0) != test.changed || len(value.Transactions[0].TransactionIdentifiers) != test.want {
				t.Fatalf("changes=%+v ids=%+v", changes, value.Transactions[0].TransactionIdentifiers)
			}
			if _, err := validateScreenshot(value, time.Now(), nil, ""); (err == nil) != test.valid {
				t.Fatalf("valid=%v, err=%v", test.valid, err)
			}
		})
	}
}

func TestIdentityIssueNamesTheBrokenRule(t *testing.T) {
	at := time.Now().In(jakarta()).Format(time.RFC3339)
	over := screenshotRow{Direction: "OUT", Currency: "IDR", TransactionAt: &at, TransactionIdentifiers: []screenshotIdentifier{{"TRANSACTION_ID", "a"}, {"REFERENCE_NUMBER", "b"}, {"ORDER_ID", "c"}, {"ORDER_ID", "d"}}}
	for name, test := range map[string]struct {
		row  screenshotRow
		want string
	}{
		"duplicate": {incidentRow(at), "duplicate identifier kind REFERENCE_NUMBER"},
		"over cap":  {over, "more than 3 transaction identifiers"},
		"kind":      {screenshotRow{Direction: "OUT", Currency: "IDR", TransactionIdentifiers: []screenshotIdentifier{{"CARD", "1"}}}, "identifier kind must be"},
		"syntax":    {screenshotRow{Direction: "OUT", Currency: "IDR", TransactionIdentifiers: []screenshotIdentifier{{"ORDER_ID", "a b"}}}, "identifier value must be"},
	} {
		t.Run(name, func(t *testing.T) {
			issues := screenshotValidationIssues(screenshotExtraction{Confidence: .9, Transactions: []screenshotRow{test.row}}, "")
			if len(issues) != 1 || issues[0].Code != "INVALID_TRANSACTION_IDENTITY" || !strings.Contains(issues[0].Detail, test.want) {
				t.Fatalf("issue must keep the code and name the rule %q: %+v", test.want, issues)
			}
		})
	}
	if screenshotIdentityIssue(screenshotRow{TransactionIdentifiers: []screenshotIdentifier{{"ORDER_ID", "ok"}}}) != "" {
		t.Fatal("a valid identity must report no rule")
	}
}

type screenshotRepairCapture struct {
	prompt string
	tools  []gateway.ToolDefinition
}

func (c *screenshotRepairCapture) NativeToolCall(_ context.Context, _ string, prompt string, _ any, tools []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	c.prompt, c.tools = prompt, tools
	return gateway.ToolCall{Name: "repair_transaction_fields", Arguments: json.RawMessage(`{"fields":{"transactions":[]}}`)}, gateway.Metadata{Model: "repair"}, nil
}

// The repair stays field-restricted (only the top-level transactions field is
// flagged and allowed) but the prompt says which rule broke and where.
func TestScreenshotRepairPromptNamesTheRuleAndStaysFieldRestricted(t *testing.T) {
	at := time.Now().In(jakarta()).Format(time.RFC3339)
	value := screenshotExtraction{Confidence: .99, Transactions: []screenshotRow{incidentRow(at)}}
	issues := screenshotValidationIssues(value, "EWALLET_SCREENSHOT")
	capture := &screenshotRepairCapture{}
	_, _, err := repairExtracted(context.Background(), capture, "source", "EWALLET_SCREENSHOT", []map[string]any{{"type": "input_text", "text": "x"}}, &value, &issues, func(screenshotExtraction) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"INVALID_TRANSACTION_IDENTITY", "duplicate identifier kind REFERENCE_NUMBER", "transactions[0].transaction_identifiers"} {
		if !strings.Contains(capture.prompt, want) {
			t.Fatalf("repair prompt missing %q: %s", want, capture.prompt)
		}
	}
	properties := capture.tools[0].Parameters["properties"].(map[string]any)["fields"].(map[string]any)["properties"].(map[string]any)
	if len(properties) != len(repairableFieldNames("EWALLET_SCREENSHOT")) {
		t.Fatalf("repair schema is no longer field-restricted: %v", properties)
	}
	if description, _ := properties["transactions"].(map[string]any)["description"].(string); !strings.Contains(description, "at most once") {
		t.Fatalf("repair schema must carry the identifier rule: %q", description)
	}
}

func TestExtractionPromptAndSchemaStateTheIdentifierRule(t *testing.T) {
	for _, want := range []string{"at most once per row", "at most 3 identifiers", "most specific"} {
		if !strings.Contains(screenshotPrompt, want) {
			t.Fatalf("screenshotPrompt missing %q", want)
		}
	}
	rows := screenshotSchema([]string{"food"})["properties"].(map[string]any)["transactions"].(map[string]any)["items"].(map[string]any)
	field := rows["properties"].(map[string]any)["transaction_identifiers"].(map[string]any)
	description, _ := field["description"].(string)
	for _, want := range []string{"at most once per row", "at most 3 identifiers", "most specific", "RRN"} {
		if !strings.Contains(description, want) {
			t.Fatalf("schema description missing %q: %q", want, description)
		}
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestUnpaidInvoiceOnlyIsNarrow(t *testing.T) {
	at := time.Now().In(jakarta()).Format(time.RFC3339)
	valid := screenshotExtraction{Transactions: []screenshotRow{incidentRow(at)}}
	normalizeScreenshotIdentifiers(&valid)
	badIdentity := incidentRow(at)
	badIdentity.TransactionIdentifiers = []screenshotIdentifier{{"CARD", "1"}}
	for _, test := range []struct {
		name         string
		documentType string
		value        screenshotExtraction
		want         bool
	}{
		{"unpaid, no rows", "BILL_OR_INVOICE", screenshotExtraction{Confidence: .9, PaymentStatus: "UNPAID"}, true},
		{"unknown status, no rows", "BILL_OR_INVOICE", screenshotExtraction{Confidence: .9, PaymentStatus: "UNKNOWN"}, true},
		{"unpaid with a valid row", "BILL_OR_INVOICE", screenshotExtraction{Confidence: .9, PaymentStatus: "UNPAID", Transactions: valid.Transactions}, true},
		{"unpaid but identity is broken", "BILL_OR_INVOICE", screenshotExtraction{Confidence: .9, PaymentStatus: "UNPAID", Transactions: []screenshotRow{badIdentity}}, false},
		{"paid with no rows", "BILL_OR_INVOICE", screenshotExtraction{Confidence: .9, PaymentStatus: "PAID"}, false},
		{"screenshot with no rows", "EWALLET_SCREENSHOT", screenshotExtraction{Confidence: .9}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := unpaidInvoiceOnly(test.value, test.documentType); got != test.want {
				t.Fatalf("unpaidInvoiceOnly = %v, want %v", got, test.want)
			}
		})
	}
}
