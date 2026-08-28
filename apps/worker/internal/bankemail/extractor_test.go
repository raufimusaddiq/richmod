package bankemail

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type recordingGateway struct {
	called  bool
	request string
	options gateway.NativeToolOptions
}

func (g *recordingGateway) NativeToolCall(_ context.Context, requestID, systemPrompt string, content any, tools []gateway.ToolDefinition, options ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	g.called = true
	encoded, _ := json.Marshal(map[string]any{"request_id": requestID, "prompt": systemPrompt, "content": content, "tools": tools})
	g.request = string(encoded)
	if len(options) == 1 {
		g.options = options[0]
	}
	return gateway.ToolCall{
		ResponseID: "response-1",
		CallID:     "call-1",
		Name:       "emit_bank_transaction",
		Arguments:  json.RawMessage(`{"kind":"TRANSACTION","direction":"OUTGOING","channel":"DEBIT_CARD","amount_idr":"378075","transaction_at":"2026-08-28T09:59:36+07:00","merchant":"TOKOPEDIA","counterparty":null,"reference":null,"description":"d-Card purchase","missing_fields":[],"confidence":0.98}`),
	}, gateway.Metadata{Model: "test-model"}, nil
}

func TestExtractorRequiresOneNativeBankToolCall(t *testing.T) {
	llm := &recordingGateway{}
	extractor := NewExtractor(llm)
	got, metadata, err := extractor.Extract(context.Background(), "source-1", Listener{
		BankName:      "Jenius",
		SenderAddress: "jenius_noreply@smbci.com",
	}, TrustedEmail{
		MessageID:             "message-1",
		Subject:               "d-Card Credit Card Transaction",
		Date:                  "Fri, 28 Aug 2026 09:59:36 +0700",
		AuthenticationResults: "dkim=pass; dmarc=pass",
		Body:                  "Merchant: TOKOPEDIA\nTotal: IDR 378,075.00",
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !llm.called || llm.options.Required != true || llm.options.MaxToolCalls != 1 {
		t.Fatalf("native call contract not enforced: called=%v options=%+v", llm.called, llm.options)
	}
	if llm.options.ReasoningEffort != "none" {
		t.Fatalf("bank extraction reasoning is not disabled: %+v", llm.options)
	}
	if metadata.Model != "test-model" || got.Kind != "TRANSACTION" || got.AmountIDR == nil || *got.AmountIDR != "378075" || got.Merchant == nil || *got.Merchant != "TOKOPEDIA" {
		t.Fatalf("unexpected extraction=%+v metadata=%+v", got, metadata)
	}
	if len(llm.request) == 0 {
		t.Fatal("extractor did not pass trusted context and email content to gateway")
	}
}
