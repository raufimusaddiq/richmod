package telegram

import (
	"regexp"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/plainlabel"
)

var (
	telegramMechanicsPattern = regexp.MustCompile(`(?i)\b(balas|membalas|reply|klik|ketuk|tap|tekan|menekan|swipe)\b`)
	bareRupiahPattern        = regexp.MustCompile(`\bRp(?:[^0-9]|$)`)
	emptyJoinPattern         = regexp.MustCompile(`·\s*·|^\s*·|·\s*$|:\s*·|·\s*:`)
	leakedValuePattern       = regexp.MustCompile(`<nil>|%!|%[sdvq]\b|\bnull\b|\bundefined\b|\{\{`)
)

var allowedSymbols = map[rune]bool{'✅': true, '🗑': true, '⏳': true, '⭐': true, '🟡': true, '️': true}

func copySymbol(r rune) bool {
	return r >= 0x1F000 || (r >= 0x2300 && r <= 0x23FF) || (r >= 0x2600 && r <= 0x27BF) || (r >= 0x2B00 && r <= 0x2BFF) || (r >= 0x2190 && r <= 0x21FF && r != 0x2192)
}

func copyViolations(text string) []string {
	var problems []string
	add := func(reason string) { problems = append(problems, reason) }
	if plainlabel.UpperCodePattern.MatchString(text) || uuidPattern.MatchString(text) {
		add("internal code")
	}
	if telegramMechanicsPattern.MatchString(text) {
		add("telegram mechanics")
	}
	if bareRupiahPattern.MatchString(text) {
		add("rupiah without a number")
	}
	if leakedValuePattern.MatchString(text) {
		add("leaked placeholder")
	}
	if strings.Contains(text, "  ") || strings.Contains(text, "\n\n\n") || strings.Contains(text, " \n") {
		add("doubled or trailing whitespace")
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "·") {
		add("dangling ending")
	}
	for _, line := range strings.Split(text, "\n") {
		if emptyJoinPattern.MatchString(line) {
			add("empty fragment around a separator")
			break
		}
	}
	for _, r := range text {
		if copySymbol(r) && !allowedSymbols[r] {
			add("symbol outside the fixed set: " + string(r))
			break
		}
	}
	return problems
}

func TestCopyAuditRejectsBrokenCopy(t *testing.T) {
	t.Parallel()
	bad := map[string]string{
		"internal code":           "Status: AMBIGUOUS_CATEGORY",
		"telegram mechanics":      "Balas pesan ini dengan nama akun.",
		"rupiah without a number": "Nominal: Rp",
		"leaked placeholder":      "Akun: <nil>",
		"doubled":                 "Tersimpan:  Kopi",
		"dangling":                "Tersimpan: Kopi ·",
		"empty fragment":          "Kopi · · Rp25.000",
		"symbol":                  "📝 Ketik transaksinya.",
	}
	for name, text := range bad {
		if len(copyViolations(text)) == 0 {
			t.Errorf("%s: %q passed the copy audit", name, text)
		}
	}
	for _, text := range []string{"✅ Tersimpan: Kopi · Rp25.000", "🗑️ Diabaikan. Tidak dicatat.", "🟡 Masuk kategori apa?\n\nAlasan: Belum jelas.", "⭐ Diingat untuk Kopi."} {
		if problems := copyViolations(text); len(problems) != 0 {
			t.Errorf("%q: %v", text, problems)
		}
	}
}
