package telegram

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/plainlabel"
)

var (
	evalOpeners   = regexp.MustCompile(`(?i)^\s*(?:tentu|baik,|baik\s+(?:aku|saya)|berdasarkan|terima kasih|certainly|of course|sure[,!]|great question|as an ai|sebagai asisten)`)
	evalMutation  = regexp.MustCompile(`(?i)✅|\b(?:sudah|telah)\s+(?:ku)?(?:catat|simpan|ubah|ganti|hapus)|\btercatat\b|\btersimpan\b|(?:has|have) been (?:recorded|saved|updated|changed)|\bi(?:'ve| have)\s+(?:recorded|saved|logged|updated)`)
	evalHelpWords = regexp.MustCompile(`(?i)/help|keuangan|finance|financ`)
	evalDigit     = regexp.MustCompile(`\d`)
)

func evalContent(scenario evalScenario) map[string]any {
	turn := map[string]any{
		"current_user_text":        untrustedUser(scenario.text),
		"current_jakarta_datetime": "2026-11-26T09:00:00+07:00",
		"recent_turns":             []any{},
		"recent_transactions":      []any{},
		"allowed_category_slugs":   []string{"makan", "transport", "belanja", "tagihan"},
		"active_review_count":      0,
		"has_salary_choice":        false,
		"has_merchant_learning":    false,
	}
	if scenario.previous != "" {
		turn["recent_turns"] = []any{map[string]any{"role": "USER", "text": scenario.text}, map[string]any{"role": "ASSISTANT", "text": scenario.previous}}
	}
	if len(scenario.openCards) > 0 {
		turn["open_review_cards"] = scenario.openCards
	}
	markRepeatedQuestion(turn, scenario.repeated)
	results := make([]agentToolResult, 0, len(scenario.tools))
	for _, result := range scenario.tools {
		result.Facts = plainlabel.Facts(result.Facts)
		results = append(results, result)
	}
	return map[string]any{"turn_context": turn, "agent_tool_results": results, "supported_languages": []string{"id", "en"}}
}

func nonEmptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

func wordSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(strings.ToLower(text)) {
		set[strings.Trim(word, ".,!?;:")] = true
	}
	return set
}

func similarity(a, b string) float64 {
	left, right := wordSet(a), wordSet(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	shared := 0
	for word := range left {
		if right[word] {
			shared++
		}
	}
	return float64(shared) / float64(len(left)+len(right)-shared)
}

func evalCheck(scenario evalScenario, reply string) []string {
	var failures []string
	if guarded, fault := guardReply(reply, ""); fault != replyFaultNone || guarded != strings.TrimSpace(reply) {
		failures = append(failures, "output guard: "+string(fault))
	}
	lines := nonEmptyLines(reply)
	if len(lines) < 1 || len(lines) > 4 {
		failures = append(failures, fmt.Sprintf("line count %d outside 1-4", len(lines)))
	}
	if len(strings.Fields(reply)) > 90 {
		failures = append(failures, "longer than 90 words")
	}
	if len(lines) > 0 {
		first := lines[0]
		if evalOpeners.MatchString(first) {
			failures = append(failures, "opens with filler instead of the answer")
		}
		if len(scenario.leadTerms) > 0 {
			found := false
			for _, term := range scenario.leadTerms {
				if strings.Contains(strings.ToLower(first), strings.ToLower(term)) {
					found = true
				}
			}
			if !found {
				failures = append(failures, "first line does not lead with the answer")
			}
		}
	}
	if !scenario.mutationMade && !scenario.existingFacts && evalMutation.MatchString(reply) {
		failures = append(failures, "claims a change that was not made")
	}
	if scenario.needsHelp && !evalHelpWords.MatchString(reply) {
		failures = append(failures, "out-of-scope reply does not point to what the assistant can do")
	}
	if scenario.needsDigit && !evalDigit.MatchString(reply) {
		failures = append(failures, "answer carries no figure")
	}
	if scenario.repeated && scenario.previous != "" && similarity(reply, scenario.previous) > 0.8 {
		failures = append(failures, "repeats the earlier answer")
	}
	return failures
}

type evalFakeGateway struct {
	reply func(evalScenario) string
	index int
}

func (g *evalFakeGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	content, _ := request.Content.(map[string]any)
	turn, _ := content["turn_context"].(map[string]any)
	text, _ := turn["current_user_text"].(string)
	for _, scenario := range evalScenarios {
		if strings.Contains(text, scenario.text) {
			return gateway.AgentResponse{Text: g.reply(scenario)}, nil
		}
	}
	return gateway.AgentResponse{}, fmt.Errorf("unknown scenario")
}

func runPromptEval(ctx context.Context, model conversationalGateway) map[string][]string {
	failures := map[string][]string{}
	for _, scenario := range evalScenarios {
		callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		response, err := model.AgentTurn(callCtx, "prompt-eval", gateway.AgentRequest{SystemPrompt: conversationalAgentPrompt, Content: evalContent(scenario)})
		cancel()
		if err != nil {
			failures[scenario.name] = []string{"gateway error: " + err.Error()}
			continue
		}
		if problems := evalCheck(scenario, response.Text); len(problems) > 0 {
			failures[scenario.name] = problems
		}
	}
	return failures
}

func TestPromptEvalHarnessAcceptsGoodRepliesAndCatchesBadOnes(t *testing.T) {
	if len(evalScenarios) < 15 {
		t.Fatalf("eval needs at least 15 scenarios, has %d", len(evalScenarios))
	}
	if failures := runPromptEval(context.Background(), &evalFakeGateway{reply: func(s evalScenario) string { return s.goodReply }}); len(failures) != 0 {
		t.Fatalf("known-good replies failed: %v", failures)
	}
	bad := []string{
		"",
		"Masih perlu .",
		"Berdasarkan hasil query, kategori FOOD_DINING naik.",
		"Balas pesan ini untuk melanjutkan.",
		"✅ Sudah kucatat semuanya.",
		"Satu.\nDua.\nTiga.\nEmpat.\nLima.",
	}
	for index, reply := range bad {
		failures := runPromptEval(context.Background(), &evalFakeGateway{reply: func(evalScenario) string { return reply }})
		if len(failures) < len(evalScenarios)/2 {
			t.Fatalf("bad reply %d was accepted by %d of %d scenarios", index, len(evalScenarios)-len(failures), len(evalScenarios))
		}
	}
}

func TestPromptEvalLive(t *testing.T) {
	if os.Getenv("RICHMOD_PROMPT_EVAL") != "1" {
		t.Skip("set RICHMOD_PROMPT_EVAL=1 with RICHMOD_EVAL_BASE_URL, RICHMOD_EVAL_API_KEY and RICHMOD_EVAL_MODELS to replay the conversations against the LiteRouter gateway")
	}
	baseURL, apiKey := os.Getenv("RICHMOD_EVAL_BASE_URL"), os.Getenv("RICHMOD_EVAL_API_KEY")
	models := strings.Split(os.Getenv("RICHMOD_EVAL_MODELS"), ",")
	if baseURL == "" || apiKey == "" || strings.TrimSpace(models[0]) == "" {
		t.Fatal("RICHMOD_EVAL_BASE_URL, RICHMOD_EVAL_API_KEY and RICHMOD_EVAL_MODELS (small,large) are required")
	}
	for _, name := range models {
		name = strings.TrimSpace(name)
		t.Run(name, func(t *testing.T) {
			failures := runPromptEval(context.Background(), gateway.New(baseURL, apiKey, name))
			for _, scenario := range evalScenarios {
				if problems, failed := failures[scenario.name]; failed {
					t.Errorf("%s: %s", scenario.name, strings.Join(problems, "; "))
				}
			}
			t.Logf("%d of %d scenarios passed", len(evalScenarios)-len(failures), len(evalScenarios))
		})
	}
}
