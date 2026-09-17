package document

import (
	"strings"
	"testing"
	"time"
)

func TestEvidenceContextPromptIsSafeAndBounded(t *testing.T) {
	received := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	e := EvidenceContext{
		SourceType:    "TELEGRAM_IMAGE",
		ReceivedAt:    received,
		Timezone:      householdTimezone,
		Caption:       "bayar listrik\n\x00\x1b[31mred",
		FileName:      "IMG_0001.jpg",
		Categories:    []categoryOption{{ID: "internal-category-id", Slug: "utilities"}},
		MerchantHints: []merchantHint{{RawName: "Solaria"}},
		AccountHints:  []string{"BCA Utama"},
	}
	prompt := e.promptText()
	for _, banned := range []string{"internal-category-id", "household_id", "transaction_id", "\x00", "\x1b"} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("prompt leaked forbidden content %q: %s", banned, prompt)
		}
	}
	for _, expected := range []string{"TELEGRAM_IMAGE", "Asia/Jakarta", "utilities", "Solaria", "BCA Utama", "<evidence>"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q: %s", expected, prompt)
		}
	}
	if len(prompt) > 4096 {
		t.Fatalf("prompt length = %d", len(prompt))
	}
}

func TestSanitizeEvidenceTextCapsAndStripsControl(t *testing.T) {
	long := strings.Repeat("a", 900)
	if got := sanitizeEvidenceText("\x07  a\n\tb  " + long); len([]rune(got)) != 500 || strings.ContainsAny(got, "\x07\n\t") {
		t.Fatalf("sanitize = %q (len %d)", got[:min(20, len(got))], len([]rune(got)))
	}
}
