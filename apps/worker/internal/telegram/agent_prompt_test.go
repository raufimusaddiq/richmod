package telegram

import (
	"strings"
	"testing"
)

func TestConversationalAgentPromptTargetsTelegramReplies(t *testing.T) {
	for _, phrase := range []string{"directly inside Telegram", "only the user-facing message", "no JSON", "default to Indonesian", "<untrusted_ledger_text>"} {
		if !strings.Contains(conversationalAgentPrompt, phrase) {
			t.Fatalf("prompt missing %q", phrase)
		}
	}
}
