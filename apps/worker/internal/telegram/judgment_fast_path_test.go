package telegram

import (
	"os"
	"strings"
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
	// Guard the ordering bug: only the aggregate READ routes may be gated on a
	// usable reporting period, so an unclear period can never block wealth,
	// transaction, or review routes.
	source, err := os.ReadFile("judgment_fast_path.go")
	if err != nil {
		t.Fatal(err)
	}
	guard := `if answer.Choice == "READ_SPENDING" || answer.Choice == "READ_CASHFLOW" || answer.Choice == "READ_SAVINGS" {`
	if !strings.Contains(string(source), guard) {
		t.Fatal("period resolution must be restricted to the aggregate READ routes")
	}
}

func TestBoundedJudgmentWorkflowsOnlyHandleFactFreeChoices(t *testing.T) {
	// A pending-batch UPDATE needs arbitrary replacement values, so Jev must not
	// own it: the bounded handler reports "not handled" and the generative
	// update_pending_batch path keeps its server-bound validation.
	if boundedReviewAction("UPDATE") {
		t.Fatal("UPDATE is not a bounded review action")
	}
	if !boundedReviewAction("CONFIRM") || !boundedReviewAction("IGNORE") {
		t.Fatal("fact-free review actions must stay bounded")
	}
}

func TestHarvestSimpleTransaction(t *testing.T) {
	tests := []struct {
		text, amount, date, explicit, merchant string
	}{
		{text: "catat makan siang 50rb hari ini", amount: "50000", date: "TODAY", merchant: "makan siang"},
		{text: "jajan gorengan 5k", amount: "5000", date: "TODAY", merchant: "jajan gorengan"},
		{text: "beli reksa dana 3 juta kemarin", amount: "3000000", date: "YESTERDAY", merchant: "beli reksa dana"},
		{text: "gaji 8.000.000 tanggal 2026-09-21", amount: "8000000", date: "EXPLICIT", explicit: "2026-09-21", merchant: "gaji tanggal"},
	}
	for _, test := range tests {
		got, ok := harvestSimpleTransaction(test.text)
		if !ok || got.Amount != test.amount || got.DateRef != test.date || got.ExplicitDate != test.explicit || got.Merchant != test.merchant {
			t.Fatalf("harvestSimpleTransaction(%q) = %#v, %v", test.text, got, ok)
		}
	}
	if _, ok := harvestSimpleTransaction("makan 50rb dan parkir 5rb"); ok {
		t.Fatal("multiple amounts must use generative extraction")
	}
	// The k shorthand is a currency suffix only when it stands alone. A unit
	// glued to the number (5kg) is a quantity, not Rp5.000 (PRD §24 T1).
	if got, ok := harvestSimpleTransaction("beras 5kg"); ok {
		t.Fatalf("a glued unit must not harvest a currency amount: %#v", got)
	}
}
