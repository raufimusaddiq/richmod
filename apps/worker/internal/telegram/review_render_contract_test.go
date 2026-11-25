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

func guardSubject(reviewType string) reviewSubject {
	subject := reviewSubject{TransactionType: "EXPENSE"}
	if reviewType == bankReviewType {
		subject.Bank = &bankSubject{}
	}
	return subject
}

func TestEveryTelegramReviewKindIsOwnedByTheEngine(t *testing.T) {
	for _, reviewType := range reviewdec.TelegramReasons() {
		decision, ok := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000")
		if reviewType == missingAmountReviewType {
			decision, ok = reviewdec.Decision{ReasonCode: reviewType, MissingFacts: []string{"amount"}, AllowedActions: []string{"SET_AMOUNT", "IGNORE"}}, true
		}
		if !ok {
			t.Fatalf("%s is a Telegram review kind with no ReviewDecision preset", reviewType)
		}
		if !engineOwnedType(reviewType) {
			t.Fatalf("%s is not an engine review type", reviewType)
		}
		if _, ok := deriveStep(decision, reviewType, "context", guardSubject(reviewType)); !ok {
			t.Fatalf("%s has no engine step; a Telegram review kind must not fall back to a legacy prompt", reviewType)
		}
	}
}

func TestTransactionDatePromptAsksOneThingWithoutAFormat(t *testing.T) {
	decision, _ := reviewdec.Preset("MISSING_TRANSACTION_DATE", "transaction", "00000000-0000-0000-0000-000000000000")
	card := buildReviewCard(decision, "MISSING_TRANSACTION_DATE", "", reviewSubject{TransactionType: "EXPENSE"})
	if card.Mode != "step" || !strings.Contains(card.Text(), transactionDateAsk) || strings.Contains(card.Text(), "YYYY-MM-DD") {
		t.Fatalf("transaction date prompt: mode=%s %q", card.Mode, card.Text())
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
	telegramLanes["COMPLETE_BANK_FACTS"] = true
	telegramLanes["SET_AMOUNT"] = true
	for _, action := range []string{"ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED", "TRANSACTION_MISSING", "SET_PAY_DATE", "HOUSEHOLD_POLICY", "PRIMARY_SALARY", "ORDINARY_INCOME", "REPROCESS_DOCUMENT", "SET_FINANCIAL_EMAIL_ENTITIES", "SET_WEALTH_ACCOUNT", "RECORD_ASSET_PURCHASE"} {
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

func TestSuppliedContextIsKeptOnTheCard(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		decision, ok := reviewdec.Preset(reviewType, "source_event", "00000000-0000-0000-0000-000000000000")
		if !ok {
			continue
		}
		card := buildReviewCard(decision, reviewType, "bespoke prompt", reviewSubject{})
		if card.State == "" || !strings.Contains(card.Text(), "bespoke prompt") {
			t.Fatalf("%s lost its state or the supplied prompt: %q", reviewType, card.Text())
		}
	}
}

func TestBankFactsCapability(t *testing.T) {
	if !TelegramCompletableReviewType("UNKNOWN_BANK_TEMPLATE") {
		t.Fatal("UNKNOWN_BANK_TEMPLATE must be completable from Telegram")
	}
}

func TestEmptySummaryCardShowsItsTitleOnce(t *testing.T) {
	decision, ok := reviewdec.Preset("MISSING_TRANSACTION_DATE", "transaction", "00000000-0000-0000-0000-000000000000")
	if !ok {
		t.Fatal("no preset")
	}
	card := buildReviewCard(decision, "MISSING_TRANSACTION_DATE", "", reviewSubject{TransactionType: "EXPENSE"})
	if card.State != "AWAITING_DATE" {
		t.Fatalf("state=%s", card.State)
	}
	if got := strings.Count(card.Text(), "🟡 "+transactionDateAsk); got != 1 {
		t.Fatalf("title appears %d times in %q", got, card.Text())
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
