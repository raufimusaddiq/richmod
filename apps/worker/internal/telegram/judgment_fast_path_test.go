package telegram

import (
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

func TestJudgmentPeriodChoiceMapsToExactRange(t *testing.T) {
	// Wednesday 2026-09-23 10:00 Jakarta.
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, jakartaLocation())
	processor := &Processor{}
	criteria := judgment.ChoiceCriteria(judgmentPeriodCriteria)
	policy := judgmentPolicy.Route
	for period, wantFrom := range map[string]string{
		"TODAY":      "2026-09-23",
		"THIS_WEEK":  "2026-09-21",
		"LAST_WEEK":  "2026-09-14",
		"THIS_MONTH": "2026-09-01",
		"LAST_MONTH": "2026-08-01",
	} {
		answer := judgment.Answer{Type: "choice", Choice: period, HasConfidence: true, Confidence: 0.95}
		answer.Distribution = map[string]float64{}
		for label := range criteria {
			answer.Distribution[label] = 0.001
		}
		answer.Distribution[period] = 1 - 0.001*float64(len(criteria)-1)
		answer.Probability = answer.Distribution[period]
		if !judgment.AcceptChoice(answer, criteria, policy) {
			t.Fatalf("period %s answer should be accepted: %+v", period, answer)
		}
		rangeValue, ok := processor.resolveJudgmentPeriod(nil, "household", now, answer)
		if !ok {
			t.Fatalf("period %s should resolve", period)
		}
		if got := rangeValue.From.In(jakartaLocation()).Format("2006-01-02"); got != wantFrom {
			t.Fatalf("period %s from=%s want %s", period, got, wantFrom)
		}
	}

	// An unclear period must never silently become THIS_MONTH.
	unclear := judgment.Answer{Type: "choice", Choice: "CUSTOM_OR_UNCLEAR", HasConfidence: true, Confidence: 0.95, Distribution: map[string]float64{}}
	for label := range criteria {
		unclear.Distribution[label] = 0.001
	}
	unclear.Distribution["CUSTOM_OR_UNCLEAR"] = 1 - 0.001*float64(len(criteria)-1)
	unclear.Probability = unclear.Distribution["CUSTOM_OR_UNCLEAR"]
	if _, ok := processor.resolveJudgmentPeriod(nil, "household", now, unclear); ok {
		t.Fatal("CUSTOM_OR_UNCLEAR must not resolve to a default period")
	}
}

func TestOnlyAggregateReadRoutesConsumePeriod(t *testing.T) {
	// An unclear period may only block the aggregate READ routes, never wealth,
	// transaction, or review routes.
	aggregate := map[string]bool{"READ_SPENDING": true, "READ_CASHFLOW": true, "READ_SAVINGS": true}
	for _, route := range judgmentRoutes {
		if got := routeConsumesPeriod(route); got != aggregate[route] {
			t.Fatalf("routeConsumesPeriod(%q) = %v", route, got)
		}
	}
}

func TestBoundedJudgmentWorkflowsOnlyHandleFactFreeChoices(t *testing.T) {
	// A pending-batch UPDATE needs arbitrary replacement values, so Jev must not
	// own it: the bounded handler reports "not handled" and the generative
	// update_pending_batch path keeps its server-bound validation.
	if isReviewAction("TRANSFER_CLASSIFICATION", "UPDATE") {
		t.Fatal("UPDATE is not a bounded review action")
	}
	if !isReviewAction("AMBIGUOUS_CATEGORY", "CONFIRM") || !isReviewAction("TRANSFER_CLASSIFICATION", "IGNORE") {
		t.Fatal("fact-free review actions must stay bounded")
	}
	if reviewActionNeedsArguments("CONFIRM") || reviewActionNeedsArguments("IGNORE") {
		t.Fatal("fact-free actions must not require generative argument extraction")
	}
	if !reviewActionNeedsArguments("ASSET_PURCHASE") || !reviewActionNeedsArguments("EXPENSE") {
		t.Fatal("argument-bearing actions must defer to generative extraction")
	}
}

func TestHarvestSimpleTransaction(t *testing.T) {
	tests := []struct {
		text, amount string
	}{
		{text: "catat makan siang 50rb hari ini", amount: "50000"},
		{text: "jajan gorengan 5k", amount: "5000"},
		{text: "beli reksa dana 3 juta kemarin", amount: "3000000"},
		{text: "barusan beli kopi 25k", amount: "25000"},
	}
	for _, test := range tests {
		got, ok := harvestSimpleTransaction(test.text)
		if !ok || got.Amount != test.amount || got.Text != test.text {
			t.Fatalf("harvestSimpleTransaction(%q) = %#v, %v", test.text, got, ok)
		}
	}
	if _, ok := harvestSimpleTransaction("makan 50rb dan parkir 5rb"); ok {
		t.Fatal("multiple amounts must use generative extraction")
	}
	// The k shorthand is a currency suffix only when it stands alone. A unit
	// glued to the number (5kg) is a quantity, not Rp5.000.
	if got, ok := harvestSimpleTransaction("beras 5kg"); ok {
		t.Fatalf("a glued unit must not harvest a currency amount: %#v", got)
	}
}
