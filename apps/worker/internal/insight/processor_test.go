package insight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type analystGateway struct {
	responses []gateway.AgentResponse
	requests  []gateway.AgentRequest
	err       error
	budgets   []time.Duration
}

func (g *analystGateway) AgentTurn(ctx context.Context, _ string, req gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.requests = append(g.requests, req)
	deadline, _ := ctx.Deadline()
	g.budgets = append(g.budgets, time.Until(deadline))
	if g.err != nil {
		return gateway.AgentResponse{}, g.err
	}
	i := len(g.requests) - 1
	if i >= len(g.responses) {
		return gateway.AgentResponse{}, fmt.Errorf("unexpected phase")
	}
	return g.responses[i], nil
}

func analyticalCall(id, name string) gateway.ToolCall {
	return gateway.ToolCall{CallID: id, Name: name, Arguments: json.RawMessage(`{"cycle_start":"2026-08-01"}`)}
}

func overviewPhase() gateway.AgentResponse {
	return gateway.AgentResponse{ResponseID: "phase1", ToolCalls: []gateway.ToolCall{analyticalCall("overview", "get_cycle_overview"), analyticalCall("quality", "get_cycle_data_quality")}}
}

func renderPhase(message string) gateway.AgentResponse {
	raw, _ := json.Marshal(map[string]string{"message": message})
	return gateway.AgentResponse{Metadata: gateway.Metadata{Model: "analyst-model"}, ToolCalls: []gateway.ToolCall{{CallID: "render", Name: renderToolName, Arguments: raw}}}
}

func safeFacts(_ context.Context, _ string, _ json.RawMessage) (map[string]any, error) {
	return map[string]any{"data_completeness": "1.0000", "expense": "1400000", "median3": "1350000"}, nil
}

func TestAnalystNativeDependentReadsNoFillerOrAdviceContract(t *testing.T) {
	g := &analystGateway{responses: []gateway.AgentResponse{
		overviewPhase(),
		{ResponseID: "phase2", ToolCalls: []gateway.ToolCall{analyticalCall("changes", "get_cycle_changes")}},
		{ResponseID: "phase3", ToolCalls: []gateway.ToolCall{{CallID: "merchants", Name: "get_merchant_drivers", Arguments: json.RawMessage(`{"cycle_start":"2026-08-01","category_ref":"category.1"}`)}}},
		renderPhase("Tidak ada perubahan berarti untuk dibahas."),
	}}
	calls := []string{}
	text, metadata, reads, err := (&Processor{gateway: g}).generate(context.Background(), "server-only-insight-id", "2026-08-01", func(ctx context.Context, name string, raw json.RawMessage) (map[string]any, error) {
		calls = append(calls, name)
		return safeFacts(ctx, name, raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "Tidak ada perubahan berarti untuk dibahas." || metadata.Model != "analyst-model" || len(reads) != 4 || len(g.requests) != 4 {
		t.Fatalf("text=%q reads=%d phases=%d", text, len(reads), len(g.requests))
	}
	if strings.Join(calls, ",") != "get_cycle_overview,get_cycle_data_quality,get_cycle_changes,get_merchant_drivers" {
		t.Fatalf("calls=%v", calls)
	}
	for i, req := range g.requests {
		if i > 1 && len(req.ReadHistory) != i-1 {
			t.Fatalf("earlier native READ phases lost: phase=%d history=%d", i, len(req.ReadHistory))
		}
		raw, _ := json.Marshal(req.Content)
		if strings.Contains(string(raw), "1400000") || strings.Contains(string(raw), "server-only-insight-id") {
			t.Fatal("preloaded financial facts/IDs reached model")
		}
		for _, tool := range req.Tools {
			if tool.Name == renderToolName && i == 0 {
				t.Fatal("render exposed before facts")
			}
			schema, _ := json.Marshal(tool.Parameters)
			if strings.Contains(string(schema), "recommendation") || strings.Contains(string(schema), "confidence") || strings.Contains(string(schema), "observations") {
				t.Fatal("structured narrative contract returned")
			}
		}
		if i > 0 && (req.PreviousResponseID == "" || len(req.PreviousToolCalls) == 0 || len(req.ToolOutputs) == 0) {
			t.Fatal("native continuation missing")
		}
	}
}

func TestAnalystRejectsUnexposedUnsafeAndMalformedBatchBeforeReads(t *testing.T) {
	cases := []struct {
		name     string
		response gateway.AgentResponse
	}{
		{"side effect", gateway.AgentResponse{ToolCalls: []gateway.ToolCall{analyticalCall("a", "record_transaction")}}},
		{"unknown", gateway.AgentResponse{ToolCalls: []gateway.ToolCall{analyticalCall("a", "execute_sql")}}},
		{"early render", renderPhase("No data")},
		{"text contract", gateway.AgentResponse{Text: `{"summary":"No change"}`}},
		{"mixed render", gateway.AgentResponse{ToolCalls: []gateway.ToolCall{analyticalCall("a", "get_cycle_overview"), {CallID: "b", Name: renderToolName, Arguments: json.RawMessage(`{"message":"ok"}`)}}}},
		{"unknown argument", gateway.AgentResponse{ToolCalls: []gateway.ToolCall{analyticalCall("a", "get_cycle_overview"), {CallID: "b", Name: "get_cycle_data_quality", Arguments: json.RawMessage(`{"cycle_start":"2026-08-01","extra":true}`)}}}},
		{"wrong period", gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "a", Name: "get_cycle_overview", Arguments: json.RawMessage(`{"cycle_start":"2026-09-01"}`)}}}},
		{"missing id", gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{Name: "get_cycle_overview", Arguments: json.RawMessage(`{"cycle_start":"2026-08-01"}`)}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &analystGateway{responses: []gateway.AgentResponse{tc.response}}
			readCalls := 0
			text, _, _, err := (&Processor{gateway: g}).generate(context.Background(), "id", "2026-08-01", func(ctx context.Context, name string, raw json.RawMessage) (map[string]any, error) {
				readCalls++
				return safeFacts(ctx, name, raw)
			})
			if err == nil || text != "" || readCalls != 0 {
				t.Fatalf("err=%v text=%q calls=%d", err, text, readCalls)
			}
		})
	}
}

func TestAnalystReadAndPhaseLimits(t *testing.T) {
	for _, tc := range []struct {
		name        string
		sizes       []int
		maxExecuted int
	}{
		{"batch", []int{6}, 0},
		{"turn", []int{5, 5, 3}, 10},
		{"phases", []int{1, 1, 1, 1, 1}, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &analystGateway{}
			id := 0
			for _, size := range tc.sizes {
				phase := gateway.AgentResponse{ResponseID: fmt.Sprint(id)}
				for j := 0; j < size; j++ {
					id++
					phase.ToolCalls = append(phase.ToolCalls, analyticalCall(fmt.Sprint(id), "get_cycle_overview"))
				}
				g.responses = append(g.responses, phase)
			}
			calls := 0
			_, _, _, err := (&Processor{gateway: g}).generate(context.Background(), "id", "2026-08-01", func(ctx context.Context, name string, raw json.RawMessage) (map[string]any, error) {
				calls++
				return safeFacts(ctx, name, raw)
			})
			if err == nil || calls != tc.maxExecuted || len(g.requests) > maxPhases {
				t.Fatalf("err=%v calls=%d phases=%d", err, calls, len(g.requests))
			}
		})
	}
}

func TestAnalystProductionReadSequenceAndBudgetBoundaries(t *testing.T) {
	for _, sizes := range [][]int{{2, 3, 4}, {2, 5, 5}, {2, 1, 1, 1}} {
		t.Run(fmt.Sprint(sizes), func(t *testing.T) {
			g := &analystGateway{responses: []gateway.AgentResponse{overviewPhase()}}
			readCount := 2
			for _, size := range sizes[1:] {
				phase := gateway.AgentResponse{ResponseID: fmt.Sprint(readCount)}
				for j := 0; j < size; j++ {
					readCount++
					phase.ToolCalls = append(phase.ToolCalls, analyticalCall(fmt.Sprint(readCount), "get_cycle_changes"))
				}
				g.responses = append(g.responses, phase)
			}
			g.responses = append(g.responses, renderPhase("Pembahasan berdasarkan data yang tersedia."))
			text, _, reads, err := (&Processor{gateway: g}).generate(context.Background(), "id", "2026-08-01", safeFacts)
			if err != nil || text == "" || len(reads) != readCount {
				t.Fatalf("text=%q reads=%d err=%v", text, len(reads), err)
			}
			for i, request := range g.requests {
				remaining := maxReadsPerTurn
				for _, count := range sizes[:min(i, len(sizes))] {
					remaining -= count
				}
				if !strings.Contains(request.SystemPrompt, fmt.Sprintf("%d READs remaining", remaining)) || !strings.Contains(request.SystemPrompt, fmt.Sprintf("%d model phases remaining", maxPhases-i)) {
					t.Fatalf("phase %d missing server-owned budget", i)
				}
				if g.budgets[i] <= 25*time.Second || g.budgets[i] > modelTimeout {
					t.Fatalf("model deadline=%s", g.budgets[i])
				}
			}
			last := g.requests[len(g.requests)-1]
			if readCount == maxReadsPerTurn || len(g.requests) == maxPhases {
				if len(last.Tools) != 1 || last.Tools[0].Name != renderToolName || last.RequiredTool != renderToolName {
					t.Fatal("budget boundary must require rendering, not another READ")
				}
			}
		})
	}
}

func TestAnalystTimeoutReasonIsSafeAndPreservesCause(t *testing.T) {
	g := &analystGateway{err: fmt.Errorf("provider private-detail: %w", context.DeadlineExceeded)}
	_, _, _, err := (&Processor{gateway: g}).generate(context.Background(), "id", "2026-08-01", safeFacts)
	if !errors.Is(err, context.DeadlineExceeded) || err.Error() != "generate insight: gateway_timeout" {
		t.Fatalf("timeout classification=%v", err)
	}
}

func TestAnalystGatewayToolAndDataFailureNeverManufactureProse(t *testing.T) {
	for _, failure := range []string{"gateway", "tool", "data"} {
		t.Run(failure, func(t *testing.T) {
			g := &analystGateway{responses: []gateway.AgentResponse{overviewPhase(), renderPhase("should not render")}}
			if failure == "gateway" {
				g.err = errors.New("unavailable")
			}
			text, _, _, err := (&Processor{gateway: g}).generate(context.Background(), "id", "2026-08-01", func(ctx context.Context, name string, raw json.RawMessage) (map[string]any, error) {
				if failure == "tool" {
					return nil, errors.New("read failed")
				}
				if failure == "data" {
					return map[string]any{"data_completeness": "0.6000"}, nil
				}
				return safeFacts(ctx, name, raw)
			})
			if err == nil || text != "" || len(g.requests) != 1 {
				t.Fatalf("err=%v text=%q phases=%d", err, text, len(g.requests))
			}
		})
	}
}

func TestRenderingOnlyCarriesNaturalProse(t *testing.T) {
	for _, raw := range []string{`{}`, `{"message":""}`, `{"message":"ok","recommendation":"do this"}`, `{"message":"ok"}{}`} {
		if _, err := decodeMessage(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	message := "Tidak ada yang menonjol.\n\nData tetap dapat ditinjau."
	raw, _ := json.Marshal(map[string]string{"message": message})
	if got, err := decodeMessage(raw); err != nil || got != message {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if !belowThreshold("0.6999", "0.7000") || belowThreshold("0.7000", "0.7000") {
		t.Fatal("coverage gate changed")
	}
}
