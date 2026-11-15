package telegram

import (
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

// Reads the review reason set from the production package that owns the presets, so no second hand-maintained
// list exists.
var producibleReviewTypes = reviewdec.ActiveReasons()

func TestEveryProducibleReviewTypeHasARenderableDecision(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		decision, ok := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000")
		if !ok {
			t.Fatalf("%s is active producer coverage with no ReviewDecision preset", reviewType)
		}
		_, _, mode := renderReviewPresentation(decision, reviewType, "context")
		if mode == "" {
			t.Fatalf("%s produced no markup mode", reviewType)
		}
		if contains(decision.MissingFacts, "transfer_relationship") && mode != "transfer" {
			t.Fatalf("%s transfer decision rendered as %q, want the transfer chooser", reviewType, mode)
		}
		if mode == "category" && !isCategoryOnly(decision) {
			t.Fatalf("%s rendered as a category chooser without a category-only decision", reviewType)
		}
	}
}

// Mirrors the switch in EnqueueReviewRequest; a mode without a case silently falls back to the generic Ubah
// detail/Abaikan keyboard.
var markupModesHandledAtCreation = map[string]bool{
	"category":              true,
	"reply":                 true,
	"duplicate":             true,
	"salary":                true,
	"transfer":              true,
	"document":              true,
	"financial_email":       true,
	"financial_email_facts": true,
	"receipt_quality":       true,
	"cycle":                 true,
	"income":                true,
}

func TestRenderedMarkupModesAreHandledAtCreation(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		decision, _ := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000")
		_, _, mode := renderReviewPresentation(decision, reviewType, "context")
		if !markupModesHandledAtCreation[mode] {
			t.Fatalf("%s rendered mode %q with no creation markup case", reviewType, mode)
		}
	}
}

func TestTransactionDatePromptKeepsItsSupportedFormat(t *testing.T) {
	decision, _ := reviewdec.Preset("MISSING_TRANSACTION_DATE", "transaction", "00000000-0000-0000-0000-000000000000")
	_, message, _ := renderReviewPresentation(decision, "MISSING_TRANSACTION_DATE", "")
	if !strings.Contains(message, "YYYY-MM-DD") {
		t.Fatalf("transaction date prompt does not match its parser: %q", message)
	}
}

func TestPayslipReviewTypesCanProject(t *testing.T) {
	for _, kind := range []string{"PAYSLIP_CONFIRMATION", "MISSING_PAY_DATE"} {
		if !TelegramCompletableReviewType(kind) {
			t.Fatalf("%s cannot project", kind)
		}
	}
}

// Every active reason needs a decision preset so a new producer gets the shared contract, not a zero decision.
func TestActiveReasonsArePresetBacked(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		if _, ok := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000"); !ok {
			t.Fatalf("%s is active producer coverage with no ReviewDecision preset", reviewType)
		}
	}
}

// Action-level gate: every ordinary action a review offers must be completable from Telegram.
// Maps actions to the lane they reach, not the wire value; CONFIRM_REVIEW and CLASSIFY_TRANSFER are presets.
func telegramReviewLanes() map[string]bool {
	telegramLanes := map[string]bool{"CONFIRM_REVIEW": true, "IGNORE": true, "CLASSIFY_TRANSFER": true}
	for _, action := range reviewdec.AllAgentActions() {
		telegramLanes[action] = true
	}
	// MERGE_EXISTING and CONFIRM_NEW_TRANSFER are case-bound actions, not presets.
	telegramLanes["MERGE_EXISTING"] = true
	telegramLanes["CONFIRM_NEW_TRANSFER"] = true
	// SET_TRANSACTION_DATE completes through the date reply lane (AWAITING_DATE), which confirms with the date.
	telegramLanes["SET_TRANSACTION_DATE"] = true
	telegramLanes["SET_AMOUNT"] = true
	for _, action := range []string{"OWN_ACCOUNT", "HOUSEHOLD_ACCOUNT", "INVESTMENT_ACCOUNT", "EXPENSE", "ASSET_PURCHASE"} {
		telegramLanes[action] = true
	}
	return telegramLanes
}

func TestEveryOrdinaryAllowedActionHasATelegramLane(t *testing.T) {
	telegramLanes := telegramReviewLanes()
	capabilities := map[string]bool{"IGNORE": true}
	for _, action := range reviewdomain.TelegramCompleteActions() {
		capabilities[action] = true
		if !telegramLanes[action] {
			t.Fatalf("Admin marks %s completable but Telegram has no lane", action)
		}
	}
	for _, reviewType := range producibleReviewTypes {
		decision, ok := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000")
		if !ok {
			continue
		}
		for _, action := range decision.AllowedActions {
			if !capabilities[action] || !telegramLanes[action] {
				t.Fatalf("%s offers %q with no Telegram terminal or continuation capability", reviewType, action)
			}
		}
	}
}

// Every review_type a producer can emit must be completable from Telegram; a producer without a lane fails here.
func TestEveryProducedReviewTypeIsTelegramCompletable(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		if !TelegramCompletableReviewType(reviewType) {
			t.Fatalf("%s is producible but has no Telegram completion lane", reviewType)
		}
	}
}

// The gate is structural: a reason not registered as active coverage and with no Telegram lane is rejected
// like the real fixture.
func TestUnregisteredProducerFailsTheGate(t *testing.T) {
	const unregistered = "TEST_ONLY_UNREGISTERED_REVIEW"
	for _, reviewType := range producibleReviewTypes {
		if reviewType == unregistered {
			t.Fatalf("%s must not be registered as active coverage", unregistered)
		}
	}
	if TelegramCompletableReviewType(unregistered) {
		t.Fatalf("gate accepted %s: an unregistered producer reached a Telegram completion lane", unregistered)
	}
	if _, ok := reviewdec.Preset(unregistered, "transaction", "00000000-0000-0000-0000-000000000000"); ok {
		t.Fatalf("gate accepted %s: it resolved a decision preset without being registered", unregistered)
	}
}

// A registered review type whose ordinary action has no Telegram lane must fail independently of the type-level gate.
func TestUnregisteredOrdinaryActionFailsTheGate(t *testing.T) {
	const deadEndAction = "TEST_ONLY_DEAD_END_ACTION"
	if contains(reviewdec.AllAgentActions(), deadEndAction) {
		t.Fatalf("%s must not be a registered ordinary action", deadEndAction)
	}
	decision, ok := reviewdec.Preset("AMBIGUOUS_CATEGORY", "transaction", "00000000-0000-0000-0000-000000000000")
	if !ok {
		t.Fatal("AMBIGUOUS_CATEGORY lost its preset")
	}
	decision.AllowedActions = append(append([]string(nil), decision.AllowedActions...), deadEndAction)
	telegramLanes := telegramReviewLanes()
	rejected := false
	for _, action := range decision.AllowedActions {
		if !telegramLanes[action] {
			rejected = action == deadEndAction
		}
	}
	if !rejected {
		t.Fatalf("the gate accepted %s: an ordinary action without a Telegram lane must fail on its own", deadEndAction)
	}
}

// A supplied prompt keeps the decision's markup and state; category and transfer reviews use the summary as
// the body unchanged, detail/date/duplicate wrap it in the decision prompt.
func TestSuppliedContextKeepsItsMarkupMode(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		decision, ok := reviewdec.Preset(reviewType, "source_event", "00000000-0000-0000-0000-000000000000")
		if !ok {
			continue
		}
		state, message, mode := renderReviewPresentation(decision, reviewType, "bespoke prompt")
		if state == "" || mode == "" {
			t.Fatalf("%s lost its state/markup with a supplied prompt", reviewType)
		}
		if !strings.Contains(message, "bespoke prompt") {
			t.Fatalf("%s dropped the supplied prompt: %q", reviewType, message)
		}
		// The mode follows the decision, not the review type, so a supplied prompt cannot turn a bounded
		// chooser into a free-form reply.
		wantMode := "reply"
		switch {
		case contains(decision.AllowedActions, "REPROCESS_DOCUMENT"):
			wantMode = "document"
		case contains(decision.AllowedActions, "SET_FINANCIAL_EMAIL_ENTITIES"):
			wantMode = "financial_email"
		case decision.Consequence == reviewdec.QualitySignal:
			wantMode = "receipt_quality"
		case decision.ReasonCode == "FINANCIAL_EMAIL_FACTS":
			wantMode = "financial_email_facts"
		case decision.ReasonCode == "CYCLE_RESIDUAL_ALLOCATION":
			wantMode = "cycle"
		case isCategoryOnly(decision):
			wantMode = "category"
		case contains(decision.MissingFacts, "transfer_relationship"):
			wantMode = "transfer"
		case decision.InteractionMode == reviewdec.ModeConflictResolution || contains(decision.MissingFacts, "duplicate_relationship"):
			wantMode = "duplicate"
		case contains(decision.MissingFacts, "salary_classification") && contains(decision.AllowedActions, "PRIMARY_SALARY") && contains(decision.AllowedActions, "ORDINARY_INCOME"):
			wantMode = "salary"
		}
		if mode != wantMode {
			t.Fatalf("%s rendered mode %q with a supplied prompt, want %q", reviewType, mode, wantMode)
		}
	}
}
func TestFinancialEmailEntityMarkupPagesLargeAccountSets(t *testing.T) {
	if got := financialEmailPage("review:fepage:2"); got != 2 {
		t.Fatalf("pager parse = %d, want 2", got)
	}
	if got := financialEmailPage("review:fe:account:abc"); got != -1 {
		t.Fatalf("non-pager parsed as %d, want -1", got)
	}
	if dimension, id := financialEmailDimension("review:fe:wealth:w1"); dimension != "wealth" || id != "w1" {
		t.Fatalf("dimension parse = %q %q", dimension, id)
	}
	if dimension, _ := financialEmailDimension("review:ignore"); dimension != "ignore" {
		t.Fatalf("ignore action did not map to the ignore dimension: %q", dimension)
	}
}

// The UNKNOWN_BANK_TEMPLATE card must be completable from Telegram and a natural reply ("54000
// 2026-09-23T13:45:00+07:00", either order) must yield both facts.
func TestBankFactsReplyParserAndCapability(t *testing.T) {
	if !TelegramCompletableReviewType("UNKNOWN_BANK_TEMPLATE") {
		t.Fatal("UNKNOWN_BANK_TEMPLATE must be completable from Telegram")
	}
	for _, text := range []string{
		"54000 2026-09-23T13:45:00+07:00",
		"2026-09-23T13:45:00+07:00 Rp54000",
	} {
		amount, at := parseBankFactsReply(text)
		if amount != "54000" || at != "2026-09-23T13:45:00+07:00" {
			t.Fatalf("parse %q = %q,%q", text, amount, at)
		}
	}
	if amount, at := parseBankFactsReply("lihat nanti ya"); amount != "" || at != "" {
		t.Fatalf("non-fact reply parsed as %q,%q", amount, at)
	}
	// A separator-bearing amount is ambiguous IDR: reject it rather than reshape it.
	for _, text := range []string{
		"54,5 2026-09-23T13:45:00+07:00",
		"12.500,50 2026-09-23T13:45:00+07:00",
		"-54000 2026-09-23T13:45:00+07:00",
		"0 2026-09-23T13:45:00+07:00",
		"999999999999999999999 2026-09-23T13:45:00+07:00",
	} {
		if amount, _ := parseBankFactsReply(text); amount != "" {
			t.Fatalf("separator amount %q parsed as %q, want rejection", text, amount)
		}
	}
}

// A card with no subject summary leads with its prompt once; only the category chooser sends the summary
// verbatim and falls back to the prompt as body.
func TestEmptySummaryCardShowsItsTitleOnce(t *testing.T) {
	decision, ok := reviewdec.Preset("MISSING_TRANSACTION_DATE", "transaction", "00000000-0000-0000-0000-000000000000")
	if !ok {
		t.Fatal("no preset")
	}
	state, message, _ := renderReviewPresentation(decision, "MISSING_TRANSACTION_DATE", "")
	if state != "AWAITING_DATE" {
		t.Fatalf("state=%s", state)
	}
	if got := strings.Count(message, promptTitle(decision)); got != 1 {
		t.Fatalf("title appears %d times in %q", got, message)
	}
}
