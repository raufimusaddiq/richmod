package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

// Every Telegram-projectable kind renders one coherent card: exactly one
// title, one Indonesian reason line, and no stacked producer prompt.
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

// R6: the unknown-merchant card asks for the merchant only, and its
// instruction names exactly the buttons the card carries.
func TestUnknownMerchantCardIsCoherent(t *testing.T) {
	decision, _ := reviewdec.Preset("UNKNOWN_MERCHANT", "transaction", "t")
	decision.WhyNotAuto = "no supported merchant-to-category mapping or decisive category ruling was available"
	card := buildReviewCard(decision, "UNKNOWN_MERCHANT", ReviewQuestion("25000", ""), reviewSubject{TransactionType: "EXPENSE"})
	markup, err := reviewCardMarkup(context.Background(), nil, "", "UNKNOWN_MERCHANT", decision, card, reviewSubject{})
	if err != nil {
		t.Fatal(err)
	}
	text := card.Text()
	want := "🟡 Nama merchant belum ada\n\nNominal: Rp25.000\n\nAlasan: Belum ada kategori tersimpan untuk merchant ini.\n\nBalas pesan ini (Reply) dengan nama merchant. Jika ini pembelian aset, pilih Beli aset."
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

// A card never shows a button its kind cannot execute: a cycle residual has
// no IGNORE in its contract, so it has no Abaikan, only its own dismissal.
func TestCardOmitsButtonsTheKindCannotExecute(t *testing.T) {
	decision, _ := reviewdec.Preset("CYCLE_RESIDUAL_ALLOCATION", "cycle_residual_case", "c")
	card := buildReviewCard(decision, "CYCLE_RESIDUAL_ALLOCATION", "", reviewSubject{})
	markup, err := reviewCardMarkup(context.Background(), nil, "", "CYCLE_RESIDUAL_ALLOCATION", decision, card, reviewSubject{})
	if err != nil {
		t.Fatal(err)
	}
	if markup == nil || len(markup.InlineKeyboard) != 1 || markup.InlineKeyboard[0][0].CallbackData != "review:leave" {
		t.Fatalf("cycle markup=%#v", markup)
	}
	decision.AllowedActions = []string{"ALLOCATE_RETAINED_BALANCE"}
	if markup, _ = reviewCardMarkup(context.Background(), nil, "", "CYCLE_RESIDUAL_ALLOCATION", decision, card, reviewSubject{}); markup != nil {
		t.Fatalf("a cycle card without LEAVE_UNALLOCATED offered %#v", markup)
	}
	reply, _ := reviewdec.Preset("MISSING_PAY_DATE", "proposal", "p")
	reply.AllowedActions = []string{"SET_PAY_DATE"}
	card = buildReviewCard(reply, "MISSING_PAY_DATE", "", reviewSubject{})
	if markup, _ = reviewCardMarkup(context.Background(), nil, "", "MISSING_PAY_DATE", reply, card, reviewSubject{}); markup != nil {
		t.Fatalf("a contract without IGNORE still offered Abaikan: %#v", markup)
	}
}

// An income card asks Benar/Ubah and says so; it is never a category chooser.
func TestIncomeCategoryReviewRendersTheIncomeQuestion(t *testing.T) {
	decision, _ := reviewdec.Preset("AMBIGUOUS_CATEGORY", "transaction", "t")
	card := buildReviewCard(decision, "AMBIGUOUS_CATEGORY", "Nominal: Rp750.000", reviewSubject{TransactionType: "INCOME"})
	if card.Mode != "income" || !strings.Contains(card.Instruction, "Benar") || strings.Contains(card.Text(), "kategori di bawah") {
		t.Fatalf("income card=%#v", card)
	}
	markup := incomeConfirmMarkup()
	if markup.InlineKeyboard[0][0].CallbackData != "review:confirm" || markup.InlineKeyboard[0][1].CallbackData != "review:change" {
		t.Fatalf("income markup=%#v", markup)
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
		"kemarin":           "2026-11-04",
		"Kemarin.":          "2026-11-04",
		"hari ini":          "2026-11-05",
		"kemarin lusa":      "2026-11-03",
		"25 September 2026": "2026-09-25",
		"20 Jan":            "2026-01-20",
		// More than a month ahead without a year means last year's date.
		"25 Des": "2025-12-25",
	} {
		got, ok := parseReplyDate(input, now)
		if !ok || got.Format("2006-01-02") != want {
			t.Errorf("parseReplyDate(%q) = %s,%v want %s", input, got.Format("2006-01-02"), ok, want)
		}
	}
	for _, input := range []string{"", "31/02", "gajian tanggal 25", "besok mungkin", "25 Foo", "13/13", "kemarin sore jam 5", "1.250.000"} {
		if got, ok := parseReplyDate(input, now); ok {
			t.Errorf("parseReplyDate(%q) accepted %s", input, got.Format("2006-01-02"))
		}
	}
}

// A cycle card whose contract has no LEAVE_UNALLOCATED neither shows the button
// nor names it.
func TestCycleInstructionNamesOnlyItsButtons(t *testing.T) {
	decision, _ := reviewdec.Preset("CYCLE_RESIDUAL_ALLOCATION", "cycle_residual_case", "c")
	if card := buildReviewCard(decision, "CYCLE_RESIDUAL_ALLOCATION", "", reviewSubject{}); !strings.Contains(card.Instruction, "Biarkan saja") {
		t.Fatalf("instruction=%q", card.Instruction)
	}
	decision.AllowedActions = []string{"ALLOCATE_RETAINED_BALANCE", "TRANSACTION_MISSING"}
	if card := buildReviewCard(decision, "CYCLE_RESIDUAL_ALLOCATION", "", reviewSubject{}); strings.Contains(card.Instruction, "Biarkan") {
		t.Fatalf("instruction names an absent button: %q", card.Instruction)
	}
}
