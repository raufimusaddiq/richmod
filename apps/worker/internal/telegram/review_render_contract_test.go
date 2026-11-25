package telegram

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

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
		if card := buildReviewCard(decision, reviewType, "context", reviewSubject{TransactionType: "UNCLASSIFIED"}); reviewType == "TRANSFER_CLASSIFICATION" && card.Mode != "step" {
			t.Fatalf("%s transfer decision rendered as %q, want the step engine card", reviewType, card.Mode)
		}
		if mode == "category" && !isCategoryOnly(decision) {
			t.Fatalf("%s rendered as a category chooser without a category-only decision", reviewType)
		}
	}
}

func TestRenderedMarkupModesAreHandledAtCreation(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		decision, _ := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000")
		_, _, mode := renderReviewPresentation(decision, reviewType, "context")
		if !reviewMarkupModeHandled(mode) {
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

func TestActiveReasonsArePresetBacked(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		if _, ok := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000"); !ok {
			t.Fatalf("%s is active producer coverage with no ReviewDecision preset", reviewType)
		}
	}
}

func telegramReviewLanes() map[string]bool {
	telegramLanes := map[string]bool{"CONFIRM_REVIEW": true, "IGNORE": true, "CLASSIFY_TRANSFER": true}
	for _, action := range reviewdec.AllAgentActions() {
		telegramLanes[action] = true
	}
	telegramLanes["MERGE_EXISTING"] = true
	telegramLanes["CONFIRM_NEW_TRANSFER"] = true
	telegramLanes["SET_TRANSACTION_DATE"] = true
	telegramLanes["SET_AMOUNT"] = true
	for _, action := range []string{"ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED", "TRANSACTION_MISSING", "SET_PAY_DATE", "HOUSEHOLD_POLICY", "PRIMARY_SALARY", "ORDINARY_INCOME"} {
		telegramLanes[action] = true
	}
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

func TestEveryProducedReviewTypeIsTelegramCompletable(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		if !TelegramCompletableReviewType(reviewType) {
			t.Fatalf("%s is producible but has no Telegram completion lane", reviewType)
		}
	}
}

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
		wantMode := "reply"
		switch {
		case engineOwnsDecision(decision, reviewType, reviewSubject{}):
			wantMode = "step"
		case contains(decision.AllowedActions, "REPROCESS_DOCUMENT"):
			wantMode = "document"
		case contains(decision.AllowedActions, "SET_FINANCIAL_EMAIL_ENTITIES"):
			wantMode = "financial_email"
		case decision.Consequence == reviewdec.QualitySignal:
			wantMode = "receipt_quality"
		case decision.ReasonCode == "FINANCIAL_EMAIL_FACTS":
			wantMode = "financial_email_facts"
		case isCategoryOnly(decision):
			wantMode = "category"
		case decision.InteractionMode == reviewdec.ModeConflictResolution || contains(decision.MissingFacts, "duplicate_relationship"):
			wantMode = "duplicate"
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

func TestBankAccountChooserPaginatesBeyondEightItems(t *testing.T) {
	accounts := make([]reviewdomain.BankAccountChoice, 12)
	for i := range accounts {
		accounts[i] = reviewdomain.BankAccountChoice{ID: fmt.Sprintf("acc-%d", i), Name: fmt.Sprintf("Account %d", i)}
	}
	page0 := bankAccountChoicesMarkup(accounts, 0)
	if len(page0.InlineKeyboard) != 9 {
		t.Fatalf("page 0 rows = %d, want 9 (8 accounts + pager)", len(page0.InlineKeyboard))
	}
	if page0.InlineKeyboard[0][0].CallbackData != "review:bank:acc-0" || page0.InlineKeyboard[7][0].CallbackData != "review:bank:acc-7" {
		t.Fatalf("page 0 account callbacks = %+v", page0.InlineKeyboard[:8])
	}
	if len(page0.InlineKeyboard[8]) != 1 || page0.InlineKeyboard[8][0].CallbackData != "review:bankpage:1" {
		t.Fatalf("page 0 pager = %+v", page0.InlineKeyboard[8])
	}
	page1 := bankAccountChoicesMarkup(accounts, 1)
	if len(page1.InlineKeyboard) != 5 {
		t.Fatalf("page 1 rows = %d, want 5 (4 accounts + pager)", len(page1.InlineKeyboard))
	}
	if page1.InlineKeyboard[0][0].CallbackData != "review:bank:acc-8" || page1.InlineKeyboard[3][0].CallbackData != "review:bank:acc-11" {
		t.Fatalf("page 1 account callbacks = %+v", page1.InlineKeyboard[:4])
	}
	if len(page1.InlineKeyboard[4]) != 1 || page1.InlineKeyboard[4][0].CallbackData != "review:bankpage:0" {
		t.Fatalf("page 1 pager = %+v", page1.InlineKeyboard[4])
	}
}

func TestDuplicateChooserPaginatesWithGlobalIndices(t *testing.T) {
	candidates := make([]string, 11)
	amounts := make([]string, 11)
	for i := range candidates {
		candidates[i] = fmt.Sprintf("tx-%d", i)
		amounts[i] = fmt.Sprintf("%d", (i+1)*1000)
	}
	page0 := duplicateChoicesMarkupPage(candidates, amounts, 0)
	if len(page0.InlineKeyboard) != 10 {
		t.Fatalf("page 0 rows = %d, want 10 (8 candidates + pager + actions)", len(page0.InlineKeyboard))
	}
	if page0.InlineKeyboard[0][0].CallbackData != "review:dup:merge:0" || page0.InlineKeyboard[7][0].CallbackData != "review:dup:merge:7" {
		t.Fatalf("page 0 merge callbacks = %+v", page0.InlineKeyboard[:8])
	}
	if len(page0.InlineKeyboard[8]) != 1 || page0.InlineKeyboard[8][0].CallbackData != "review:duppage:1" {
		t.Fatalf("page 0 pager = %+v", page0.InlineKeyboard[8])
	}
	page1 := duplicateChoicesMarkupPage(candidates, amounts, 1)
	if len(page1.InlineKeyboard) != 5 {
		t.Fatalf("page 1 rows = %d, want 5 (3 candidates + pager + actions)", len(page1.InlineKeyboard))
	}
	if page1.InlineKeyboard[0][0].CallbackData != "review:dup:merge:8" || page1.InlineKeyboard[2][0].CallbackData != "review:dup:merge:10" {
		t.Fatalf("page 1 global merge callbacks = %+v", page1.InlineKeyboard[:3])
	}
	if len(page1.InlineKeyboard[3]) != 1 || page1.InlineKeyboard[3][0].CallbackData != "review:duppage:0" {
		t.Fatalf("page 1 pager = %+v", page1.InlineKeyboard[3])
	}
}

func TestAgentToolResultDecodesMissingFieldsAsStringSlice(t *testing.T) {
	raw := []byte(`{"status":"RESIDUAL_FACTS_REQUIRED","review":{"required":true,"missing_fields":["category","transaction_at"]}}`)
	var decoded agentToolResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := missingFields(decoded.Review)
	if !ok || len(got) != 2 || got[0] != "category" || got[1] != "transaction_at" {
		t.Fatalf("missingFields = %v,%v; want [category transaction_at],true", got, ok)
	}
	if reply, handled := agentMutationReply(decoded); !handled || reply == "" {
		t.Fatalf("agentMutationReply = %q,%v", reply, handled)
	}
}
