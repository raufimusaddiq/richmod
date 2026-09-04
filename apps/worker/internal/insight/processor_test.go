package insight

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type nativeInsightGateway struct {
	call    gateway.ToolCall
	options gateway.NativeToolOptions
	prompt  string
}

func (g *nativeInsightGateway) Structured(context.Context, string, string, string, any, map[string]any, any) (gateway.Metadata, error) {
	return gateway.Metadata{}, fmt.Errorf("structured output must not be used for insight generation")
}

func (g *nativeInsightGateway) NativeToolCall(_ context.Context, _ string, prompt string, _ any, tools []gateway.ToolDefinition, options ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	if len(tools) != 1 || tools[0].Name != "write_financial_insight" {
		return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("unexpected insight tools")
	}
	if len(options) == 1 {
		g.options = options[0]
	}
	g.prompt = prompt
	return g.call, gateway.Metadata{Model: "insight-model"}, nil
}

func TestCompletenessThreshold(t *testing.T) {
	if !belowThreshold("0.6999", "0.7000") || belowThreshold("0.7000", "0.7000") {
		t.Fatal("unexpected completeness threshold")
	}
}

func TestValidateOutput(t *testing.T) {
	text, confidence, err := validateOutput(output{Summary: "Arus kas positif.", Observations: []observation{{Title: "Makan", Detail: "Naik dibanding rata-rata."}}, Recommendation: "Tetapkan batas makan berdasarkan rata-rata pengeluaran yang tersedia.", Confidence: .82})
	if err != nil || confidence != .82 || !strings.Contains(text, "• Makan") || !strings.Contains(text, "Rekomendasi: Tetapkan batas makan") {
		t.Fatalf("unexpected output: %q %v %v", text, confidence, err)
	}
}

func TestValidateOutputRequiresOneRecommendationParagraph(t *testing.T) {
	for _, recommendation := range []string{"", "Kurangi pengeluaran makan.\nPantau kembali besok."} {
		if _, _, err := validateOutput(output{Summary: "Ringkasan.", Recommendation: recommendation, Confidence: .8}); err == nil {
			t.Fatalf("accepted invalid recommendation: %q", recommendation)
		}
	}
}

func TestInsightSchemaRequiresRecommendation(t *testing.T) {
	schema := insightSchema()
	required, ok := schema["required"].([]string)
	if !ok || !contains(required, "recommendation") {
		t.Fatalf("schema does not require recommendation: %#v", schema)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestGenerateInsightRequiresOneNativeTool(t *testing.T) {
	llm := &nativeInsightGateway{call: gateway.ToolCall{Name: "write_financial_insight", Arguments: json.RawMessage(`{"summary":"Arus kas positif.","observations":[{"title":"Makan","detail":"Naik dibanding rata-rata."}],"recommendation":"Tetapkan batas makan berdasarkan rata-rata pengeluaran yang tersedia.","data_quality_warning":"","confidence":0.82}`)}}
	result, metadata, err := (&Processor{gateway: llm}).generate(context.Background(), "insight-1", json.RawMessage(`{"expense":"1000"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !llm.options.Required || result.Summary != "Arus kas positif." || metadata.Model != "insight-model" || !strings.Contains(llm.prompt, "exactly one concise recommendation paragraph") || !strings.Contains(llm.prompt, "supplied deterministic aggregate facts") {
		t.Fatalf("options=%+v result=%+v metadata=%+v", llm.options, result, metadata)
	}
}

func TestGenerateInsightRejectsInvalidNativeArguments(t *testing.T) {
	for _, arguments := range []string{
		`{"summary":"ok","observations":[],"recommendation":"Gunakan data yang tersedia.","data_quality_warning":"","confidence":0.8,"extra":true}`,
		`{"summary":"ok","observations":[],"recommendation":"Gunakan data yang tersedia.","data_quality_warning":"","confidence":0.8}{}`,
	} {
		llm := &nativeInsightGateway{call: gateway.ToolCall{Name: "write_financial_insight", Arguments: json.RawMessage(arguments)}}
		if _, _, err := (&Processor{gateway: llm}).generate(context.Background(), "insight-1", nil); err == nil {
			t.Fatalf("accepted invalid arguments: %s", arguments)
		}
	}
}
