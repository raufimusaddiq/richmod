package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func TestEveryTelegramCardHasOneTitleAndAReason(t *testing.T) {
	for _, reviewType := range reviewdec.TelegramReasons() {
		decision, ok := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000")
		if !ok {
			decision = reviewdec.Decision{ReasonCode: reviewType, AllowedActions: reviewdec.TelegramActions(reviewType)}
		}
		card := buildReviewCard(decision, reviewType, ReviewQuestion("25000", "Tins Mart"), reviewSubject{})
		text := card.Text()
		if card.Title == "" || !strings.HasPrefix(text, card.Title) {
			t.Fatalf("%s card has no leading title: %q", reviewType, text)
		}
		titles := 0
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "🟡") || strings.HasPrefix(line, "🧾") {
				titles++
			}
		}
		if titles != 1 {
			t.Fatalf("%s card stacks %d titles: %q", reviewType, titles, text)
		}
		if strings.Count(text, "Alasan: ") != 1 {
			t.Fatalf("%s card does not explain why it was not auto-confirmed: %q", reviewType, text)
		}
		if strings.Contains(text, "transaksi ini\n") || strings.Contains(text, "Perlu ditinjau") {
			t.Fatalf("%s card keeps the old stacked prompt: %q", reviewType, text)
		}
	}
}

func TestUnknownMerchantCardIsCoherent(t *testing.T) {
	decision, _ := reviewdec.Preset("UNKNOWN_MERCHANT", "transaction", "t")
	decision.WhyNotAuto = "no supported merchant-to-category mapping or decisive category ruling was available"
	card := buildReviewCard(decision, "UNKNOWN_MERCHANT", ReviewQuestion("25000", ""), reviewSubject{TransactionType: "EXPENSE"})
	markup := stepOrFallback(decision, "UNKNOWN_MERCHANT", ReviewQuestion("25000", ""), reviewSubject{TransactionType: "EXPENSE"}).executable(cardActions(decision, "UNKNOWN_MERCHANT")).Markup()
	text := card.Text()
	want := "🟡 Ini bayar ke mana?\n\nNominal: Rp25.000\n\nAlasan: Belum ada kategori tersimpan untuk merchant ini.\n\nKetik nama toko atau merchant-nya."
	if text != want {
		t.Fatalf("card=\n%s\nwant\n%s", text, want)
	}
	if strings.Contains(text, "kategori di bawah") || strings.Contains(text, "Merchant:") {
		t.Fatalf("card mentions a chooser it does not carry: %q", text)
	}
	if len(markup.InlineKeyboard) != 1 || markup.InlineKeyboard[0][0].CallbackData != "review:asset" || markup.InlineKeyboard[0][1].CallbackData != "review:ignore" {
		t.Fatalf("markup=%#v", markup)
	}
}

func TestCardOmitsButtonsTheKindCannotExecute(t *testing.T) {
	rows := [][]InlineKeyboardButton{
		{{Text: "Akun", CallbackData: callbackCycle + "1000"}},
		{{Text: "Bagi", CallbackData: callbackCycleSplit}},
		{{Text: "Biarkan saja", CallbackData: callbackLeave}},
		{{Text: "Ada transaksi", CallbackData: callbackCycleMissing}},
	}
	kept := Step{Kind: stepChoice, Fact: factCycle, Rows: rows}.executable([]string{"ALLOCATE_RETAINED_BALANCE"})
	if len(kept.Rows) != 2 || kept.Rows[1][0].CallbackData != callbackCycleSplit {
		t.Fatalf("a cycle card offered buttons its contract cannot execute: %#v", kept.Rows)
	}
	kept = Step{Kind: stepChoice, Fact: factCycle, Rows: rows}.executable([]string{"LEAVE_UNALLOCATED"})
	if len(kept.Rows) != 1 || kept.Rows[0][0].CallbackData != callbackLeave {
		t.Fatalf("a cycle card without allocation offered %#v", kept.Rows)
	}
	reply, _ := reviewdec.Preset("MISSING_PAY_DATE", "proposal", "p")
	reply.AllowedActions = []string{"SET_PAY_DATE"}
	reply.MissingFacts = []string{"transaction_at"}
	step, ok := deriveStep(reply, "MISSING_PAY_DATE", "", reviewSubject{})
	if !ok || step.Fact != factDate {
		t.Fatalf("a pay date review did not derive a date step: %#v", step)
	}
	for _, button := range step.buttons() {
		if button.CallbackData == callbackIgnore {
			t.Fatalf("a contract without IGNORE still offered Abaikan: %#v", step.Rows)
		}
	}
}

func TestIncomeCategoryReviewRendersTheIncomeQuestion(t *testing.T) {
	decision, _ := reviewdec.Preset("AMBIGUOUS_CATEGORY", "transaction", "t")
	card := buildReviewCard(decision, "AMBIGUOUS_CATEGORY", "Nominal: Rp750.000", reviewSubject{TransactionType: "INCOME"})
	if card.Mode != "step" || !strings.Contains(card.Instruction, "Benar") || strings.Contains(card.Text(), "kategori di bawah") {
		t.Fatalf("income card=%#v", card)
	}
	step, _ := deriveStep(decision, "AMBIGUOUS_CATEGORY", "", reviewSubject{TransactionType: "INCOME"})
	if step.Rows[0][0].CallbackData != callbackConfirm || step.Rows[0][1].CallbackData != callbackChange {
		t.Fatalf("income step=%#v", step)
	}
}

func TestWhyLineTranslatesStoredReasons(t *testing.T) {
	for _, reviewType := range reviewdec.TelegramReasons() {
		decision, ok := reviewdec.Preset(reviewType, "transaction", "t")
		if !ok {
			continue
		}
		if line := reviewdec.WhyLine(decision); line == "" || line == decision.WhyNotAuto {
			t.Fatalf("%s has no Indonesian reason line: %q", reviewType, line)
		}
	}
	if line := reviewdec.WhyLine(reviewdec.Decision{ReasonCode: "AMBIGUOUS_CATEGORY", WhyNotAuto: "the bounded category decision did not reach a confident, well-separated choice"}); line != "Kategori belum cukup yakin untuk dipilih otomatis." {
		t.Fatalf("bespoke reason=%q", line)
	}
	if line := reviewdec.WhyLine(reviewdec.Decision{ReasonCode: "NOT_A_KIND", WhyNotAuto: "anything"}); line != "" {
		t.Fatalf("unknown kind invented a reason: %q", line)
	}
}

func TestParseReplyDate(t *testing.T) {
	now := time.Date(2026, 11, 5, 9, 0, 0, 0, jakartaLocation())
	for input, want := range map[string]string{
		"25 Okt":            "2026-10-25",
		"25 oktober 2025":   "2025-10-25",
		"25 Oct":            "2026-10-25",
		"25/10":             "2026-10-25",
		"25/10/2026":        "2026-10-25",
		"25-10-26":          "2026-10-25",
		"2026-10-25":        "2026-10-25",
		"tanggal 1 Des":     "2026-12-01",
		"tgl. 25/10":        "2026-10-25",
		"Tgl 25/10":         "2026-10-25",
		"kemarin":           "2026-11-04",
		"Kemarin.":          "2026-11-04",
		"hari ini":          "2026-11-05",
		"kemarin lusa":      "2026-11-03",
		"25 September 2026": "2026-09-25",
		"20 Jan":            "2026-01-20",
		"25 Des":            "2025-12-25",
	} {
		got, ok := parseDateValue(input, now, 31)
		if !ok || got.Format("2006-01-02") != want {
			t.Errorf("parseDateValue(%q) = %s,%v want %s", input, got.Format("2006-01-02"), ok, want)
		}
	}
	for _, input := range []string{"", "31/02", "gajian tanggal 25", "besok mungkin", "25 Foo", "13/13", "kemarin sore jam 5", "1.250.000"} {
		if got, ok := parseDateValue(input, now, 31); ok {
			t.Errorf("parseDateValue(%q) accepted %s", input, got.Format("2006-01-02"))
		}
	}
}

func TestCardDateAnswerRejectsNormalizedLeapDay(t *testing.T) {
	now := time.Date(2028, 2, 10, 9, 0, 0, 0, jakartaLocation())
	if got, ok := parseDateValue("29/2", now, 0); ok {
		t.Fatalf("29/2 on 2028-02-10 returned %s; want no date", got.Format("2006-01-02"))
	}
}

func TestPayslipWithADisputedEvidenceLinkStillGetsAStepCardWithoutAReplyPrompt(t *testing.T) {
	decision, _ := reviewdec.Preset("MISSING_PAY_DATE", "proposal", "p")
	decision.Provenance = map[string]any{"evidence_link_dispute": true}
	step, ok := deriveStep(decision, "MISSING_PAY_DATE", "", reviewSubject{})
	if !ok || strings.Contains(step.Text(), "Balas pesan ini") || len(step.buttons()) != 1 || step.buttons()[0].CallbackData != callbackIgnore {
		t.Fatalf("ok=%v step=%q buttons=%#v", ok, step.Text(), step.buttons())
	}
}
