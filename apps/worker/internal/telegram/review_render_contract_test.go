package telegram

import (
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// producibleReviewTypes reads the review reason set from the production package
// that also owns the presets, so a new producer cannot be added to a second
// hand-maintained list here (UIRC-05).
var producibleReviewTypes = reviewdec.ActiveReasons()

func TestEveryProducibleReviewTypeHasARenderableDecision(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		decision, ok := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000")
		if !ok {
			// A review type with no preset must still render as a bound reply rather
			// than a category chooser, so the zero decision is acceptable; what is not
			// acceptable is a category prompt.
			state, _, mode := renderReviewPresentation(reviewdec.Decision{}, reviewType, "context")
			if mode == "category" {
				t.Fatalf("%s has no decision but rendered as a category chooser (state=%s)", reviewType, state)
			}
			continue
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

// markupModesHandledAtCreation mirrors the switch in EnqueueReviewRequest. Every
// mode the renderer can emit must have a case there, or the review silently falls
// back to the generic Ubah detail/Abaikan keyboard.
var markupModesHandledAtCreation = map[string]bool{
	"category":        true,
	"reply":           true,
	"duplicate":       true,
	"salary":          true,
	"transfer":        true,
	"document":        true,
	"financial_email": true,
	"receipt_quality": true,
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

func TestPayslipReviewUsesIndonesianPrompts(t *testing.T) {
	for _, reviewType := range []string{"PAYSLIP_CONFIRMATION", "MISSING_PAY_DATE"} {
		decision, ok := reviewdec.Preset(reviewType, "source_event", "00000000-0000-0000-0000-000000000000")
		if !ok {
			t.Fatalf("missing preset for %s", reviewType)
		}
		_, message, _ := renderReviewPresentation(decision, reviewType, "")
		if strings.Contains(message, "salary") || strings.Contains(message, "transaction date") {
			t.Fatalf("%s has non-Indonesian UI: %q", reviewType, message)
		}
		if reviewType == "MISSING_PAY_DATE" && !strings.Contains(message, "25 September 2026") {
			t.Fatalf("missing Indonesian date example: %q", message)
		}
	}
}

func TestTransactionDatePromptKeepsItsSupportedFormat(t *testing.T) {
	decision, _ := reviewdec.Preset("MISSING_TRANSACTION_DATE", "transaction", "00000000-0000-0000-0000-000000000000")
	_, message, _ := renderReviewPresentation(decision, "MISSING_TRANSACTION_DATE", "")
	if !strings.Contains(message, "tanggal transaksi (YYYY-MM-DD)") || strings.Contains(message, "September") {
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

// compatibilityOnlyReviewTypes are schema CHECK values kept for rows written
// before the current producers existed. They must still render (an old open item
// has to display) but they are not active producer coverage, so they are not in
// reviewdec.ActiveReasons and need no Telegram completion lane.
var compatibilityOnlyReviewTypes = []string{
	"SALARY_SOURCE_CONFIRMATION",
	"INVOICE_PAYMENT_STATUS",
	"UNKNOWN_EMAIL_TEMPLATE",
}

// TestCompatibilityOnlyReviewTypesAreNotProducerCoverage pins the tagging: a
// compatibility value must not be claimed as active coverage, and an active
// reason must not be parked in the compatibility bucket to dodge the capability
// gate.
func TestCompatibilityOnlyReviewTypesAreNotProducerCoverage(t *testing.T) {
	active := map[string]bool{}
	for _, reviewType := range producibleReviewTypes {
		active[reviewType] = true
	}
	for _, reviewType := range compatibilityOnlyReviewTypes {
		if active[reviewType] {
			t.Fatalf("%s is both compatibility-only and active producer coverage", reviewType)
		}
	}
}

// TestActiveReasonsArePresetBacked requires every active reason to have a
// decision preset, so a producer that starts emitting one still gets the shared
// contract rather than a zero decision.
func TestActiveReasonsArePresetBacked(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		if _, ok := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000"); !ok {
			t.Fatalf("%s is active producer coverage with no ReviewDecision preset", reviewType)
		}
	}
}

// TestEveryOrdinaryAllowedActionHasATelegramLane is the action-level gate. The
// type-level check below only proves the review can be projected; this one proves
// each ordinary action it offers can actually be carried out from Telegram.
// CONFIRM_REVIEW is the preset spelling of the reply/callback confirm lane, which
// the Telegram and agent resolvers handle as CONFIRM (UIRC-05).
// telegramReviewLanes is the ordinary-action vocabulary with a Telegram terminal
// or continuation path. CONFIRM_REVIEW and CLASSIFY_TRANSFER are the preset
// spellings the surfaces translate to their own callback vocabulary, so the entry
// names the lane the action reaches rather than the wire value Telegram receives.
func telegramReviewLanes() map[string]bool {
	telegramLanes := map[string]bool{"CONFIRM_REVIEW": true, "IGNORE": true, "CLASSIFY_TRANSFER": true}
	for _, action := range reviewActions() {
		telegramLanes[action] = true
	}
	// MERGE_EXISTING and CONFIRM_NEW_TRANSFER are case-bound actions, not presets.
	telegramLanes["MERGE_EXISTING"] = true
	telegramLanes["CONFIRM_NEW_TRANSFER"] = true
	// Classification uses the shared classifier through Telegram intent values.
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

// TestEveryProducedReviewTypeIsTelegramCompletable pins UIR-10 exit criterion 1:
// every review_type a producer can emit must be resolvable to completion from
// Telegram. Adding a producer without a completion lane fails here.
func TestEveryProducedReviewTypeIsTelegramCompletable(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		if !TelegramCompletableReviewType(reviewType) {
			t.Fatalf("%s is producible but has no Telegram completion lane", reviewType)
		}
	}
}

// TestUnregisteredProducerFailsTheGate proves the gate is structural rather than
// descriptive: a producer emitting a reason that is not registered as active
// coverage, and has no Telegram completion lane, must be rejected the same way
// the real fixture is.
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

// TestUnregisteredOrdinaryActionFailsTheGate pins the action-level half of
// UIRC-05: a registered review type whose ordinary allowed action has no
// Telegram lane must fail independently of the type-level gate, or a bounded
// chooser could offer a button nothing can complete.
func TestUnregisteredOrdinaryActionFailsTheGate(t *testing.T) {
	const deadEndAction = "TEST_ONLY_DEAD_END_ACTION"
	if contains(reviewActions(), deadEndAction) {
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

// TestSuppliedContextKeepsItsMarkupMode proves the UIR-02 shared projection does
// not drop a review's supplied prompt: when a producer supplies its own message,
// the decision still selects the markup and state, so a source/document review
// arrives as an actionable card instead of an unanswerable notice. A category or
// transfer review uses the provider's summary as the card body unchanged; a
// detail/date/duplicate review wraps the summary in the decision prompt.
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
		// The mode must follow the decision, not the review type: a supplied
		// prompt must not turn a bounded chooser into a free-form reply or back.
		wantMode := "reply"
		switch {
		case contains(decision.AllowedActions, "REPROCESS_DOCUMENT"):
			wantMode = "document"
		case contains(decision.AllowedActions, "SET_FINANCIAL_EMAIL_ENTITIES"):
			wantMode = "financial_email"
		case decision.Consequence == reviewdec.QualitySignal:
			wantMode = "receipt_quality"
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

// TestBankFactsReplyParserAndCapability pins the UIR-10 bank-email fix: the
// UNKNOWN_BANK_TEMPLATE card must be completable from Telegram, and a natural
// reply ("54000 2026-09-23T13:45:00+07:00", either order) must yield both facts.
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
	// A separator-bearing amount is ambiguous IDR; it must be rejected rather
	// than silently reshaped into a different canonical value.
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
