package telegram

import (
	"strings"
	"testing"
)

func TestConversationalAgentPromptTargetsTelegramReplies(t *testing.T) {
	for _, phrase := range []string{"directly inside Telegram", "only the user-facing message", "no JSON", "default to Indonesian", "<untrusted_ledger_text>", "get_cycle_overview", "category_ref", "prescriptive financial advice"} {
		if !strings.Contains(conversationalAgentPrompt, phrase) {
			t.Fatalf("prompt missing %q", phrase)
		}
	}
}

func TestPromptTellsTheModelNotToQuoteFiguresFromCompactedTurns(t *testing.T) {
	for _, phrase := range []string{"recent_turns are listed oldest first", "marked compacted", "never quote a financial figure"} {
		if !strings.Contains(conversationalAgentPrompt, phrase) {
			t.Fatalf("prompt missing %q", phrase)
		}
	}
}

func TestPromptTellsTheModelThatEarlierCategoryRefsAreExpired(t *testing.T) {
	for _, phrase := range []string{"valid only in the turn whose get_cycle_changes issued it", "call get_cycle_changes again", "never reuse a ref from an earlier turn"} {
		if !strings.Contains(conversationalAgentPrompt, phrase) {
			t.Fatalf("prompt missing %q", phrase)
		}
	}
}
