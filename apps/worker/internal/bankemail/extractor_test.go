package bankemail

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type recordingGateway struct {
	called  bool
	request string
	options gateway.NativeToolOptions
}

type retryGateway struct{ calls int }

func (g *retryGateway) NativeToolCall(_ context.Context, _ string, _ string, _ any, _ []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	g.calls++
	if g.calls < 2 {
		return gateway.ToolCall{Name: "emit_bank_transaction", Arguments: json.RawMessage(`{"kind":"TRANSACTION","direction":"OUTGOING","channel":"DEBIT_CARD","amount_idr":"bad","transaction_at":null,"merchant":null,"counterparty":null,"reference":null,"description":null,"missing_fields":[],"confidence":0.9}`)}, gateway.Metadata{}, nil
	}
	return gateway.ToolCall{Name: "emit_bank_transaction", Arguments: json.RawMessage(`{"kind":"NON_TRANSACTION","direction":null,"channel":null,"amount_idr":null,"transaction_at":null,"merchant":null,"counterparty":null,"reference":null,"description":null,"missing_fields":[],"confidence":0.9}`)}, gateway.Metadata{Model: "retry-model"}, nil
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
		BankName:      "Example Bank",
		SenderAddress: "notify@example.com",
	}, TrustedEmail{
		MessageID:             "message-1",
		Subject:               "Card Transaction Notification",
		Date:                  "Fri, 28 Aug 2026 09:59:36 +0700",
		AuthenticationResults: "dkim=pass; dmarc=pass",
		Body:                  "Merchant: TOKOPEDIA\nTotal: IDR 378,075.00",
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !llm.called || llm.options.Required != true {
		t.Fatalf("native call contract not enforced: called=%v options=%+v", llm.called, llm.options)
	}
	if llm.options.ReasoningEffort != "" {
		t.Fatalf("bank extraction must omit optional reasoning effort: %+v", llm.options)
	}
	if metadata.Model != "test-model" || got.Kind != "TRANSACTION" || got.AmountIDR == nil || *got.AmountIDR != "378075" || got.Merchant == nil || *got.Merchant != "TOKOPEDIA" {
		t.Fatalf("unexpected extraction=%+v metadata=%+v", got, metadata)
	}
	if len(llm.request) == 0 {
		t.Fatal("extractor did not pass trusted context and email content to gateway")
	}
}

func TestExtractorMakesOneCorrectiveCallForSchemaFailure(t *testing.T) {
	llm := &retryGateway{}
	_, _, err := NewExtractor(llm).Extract(context.Background(), "source-retry", Listener{BankName: "Example Bank", SenderAddress: "notify@example.test"}, TrustedEmail{Body: "notice"})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if llm.calls != 2 {
		t.Fatalf("native call attempts = %d, want 2", llm.calls)
	}
}

type transportFailureGateway struct{ calls int }

func (g *transportFailureGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	g.calls++
	return gateway.ToolCall{}, gateway.Metadata{}, errors.New("timeout")
}

func TestExtractorDoesNotRetryTransportFailureInsideJob(t *testing.T) {
	llm := &transportFailureGateway{}
	_, _, err := NewExtractor(llm).Extract(context.Background(), "source", Listener{BankName: "Bank"}, TrustedEmail{Body: "notice"})
	if err == nil || llm.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, llm.calls)
	}
}

func TestNormalizeVisibleBankEmailText(t *testing.T) {
	got := normalizeVisibleText("<p>Total:&nbsp; IDR 10</p>\n<div>Merchant</div>")
	if got != "Total: IDR 10 Merchant" {
		t.Fatalf("got %q", got)
	}
	if len(normalizeVisibleText(strings.Repeat("x", (32<<10)+100))) > 32<<10 {
		t.Fatal("body was not capped")
	}
}
