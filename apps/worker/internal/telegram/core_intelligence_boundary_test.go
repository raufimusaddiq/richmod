package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// T1/T3/T4: an undecided route on ordinary prose reaches the conversational
// agent with READ-only capability and no canned clarification termination.
func TestUndecidedRouteFallsThroughToAgentWithoutCannedUnclear(t *testing.T) {
	p := &Processor{}
	p.SetJudgment(prdRouteJudgment{route: "OTHER_OR_UNCLEAR"})
	state := &turnAgentContextState{Categories: []string{"dining"}}
	handled, err := p.tryJudgmentFastPath(context.Background(), "src", "hh", telegramUpdate{}, "halo", time.Now(), state)
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("an undecided route must not terminate the turn")
	}
	if state.Route != "OTHER_OR_UNCLEAR" {
		t.Fatalf("route=%q", state.Route)
	}
	tools := readOnlyAgentTools(agentFinanceTools([]string{"dining"}, false, true, true, "TRANSFER_CLASSIFICATION", true, true, "TRANSFER_CLASSIFICATION", true))
	if writes := sideEffectNames(tools); len(writes) != 0 {
		t.Fatalf("undecided route must expose no mutation: %v", writes)
	}
	if len(tools) == 0 {
		t.Fatal("undecided route must still expose READ tools for chat+reads")
	}
}

// T2/T5: a bound workflow route both falls through AND keeps its server-owned
// side-effect capability; only the authority the route established is exposed.
func TestDecisiveRouteKeepsItsOwnCapability(t *testing.T) {
	p := &Processor{}
	p.SetJudgment(prdRouteJudgment{route: "CREATE_TRANSFER"})
	state := &turnAgentContextState{}
	handled, err := p.tryJudgmentFastPath(context.Background(), "src", "hh", telegramUpdate{}, "transfer", time.Now(), state)
	if err != nil || handled {
		t.Fatalf("handled=%t err=%v", handled, err)
	}
	general := agentFinanceTools([]string{"dining"}, false, false, true, "AMBIGUOUS_CATEGORY", false, false, "TRANSACTION", true)
	if writes := sideEffectNames(general); !writes["record_transfer"] {
		t.Fatalf("decisive mutation route must retain its typed tool, writes=%v", writes)
	}
}

// T5: no Go keyword whitelist gates the degraded read path. Two arbitrary
// non-finance sentences must both be treated identically (both reach chat).
func TestDegradedPathIsNotKeywordGated(t *testing.T) {
	for _, text := range []string{"halo", "aku ga bisa chat aja?", "ceritain dong", "aku capek banget hari ini"} {
		tools := readOnlyAgentTools(agentFinanceTools(nil, false, false, false, "", false, false, "", true))
		if writes := sideEffectNames(tools); len(writes) != 0 {
			t.Fatalf("%q exposed mutation", text)
		}
	}
}

func TestExactReplyRetainsBoundMutationAuthorityWhenJevUnavailable(t *testing.T) {
	if mutationAuthorityUnavailable(false, turnAgentContextState{ExactReply: true}) {
		t.Fatal("an exact server-bound reply must retain its bound tool when Jev is unavailable")
	}
	if !mutationAuthorityUnavailable(false, turnAgentContextState{}) {
		t.Fatal("an unbound turn must lose mutation tools when Jev is unavailable")
	}
}

// T12: once a semantic fact is accepted (directAcceptanceDecision) the bounded
// evaluator is not asked again.
func TestAcceptedSemanticFactIsNotReclassified(t *testing.T) {
	engine := &stubJudgmentEngine{err: errors.New("must not be called")}
	p := &Processor{judgment: engine}
	state := newRecordState("indomaret 25rb hari ini")
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Indomaret", CategorySlug: "dining", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 30, 0, 0, jakartaLocation())}
	decision, err := p.semanticDecisionForRecord(context.Background(), state, value, []string{"dining", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if engine.calls != 0 {
		t.Fatalf("accepted fact was re-decided: calls=%d", engine.calls)
	}
	if !decision.decisionAllowed() || decision.DecisionSource != "GENERATIVE_EXTRACTION" {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestLanguageVariantsDoNotChangeAcceptedTypedDate(t *testing.T) {
	for _, text := range []string{"barusan beli kopi 25k", "tadi pagi sarapan 30rb", "pas siang makan 40 ribu", "last night spent 80k"} {
		engine := &stubJudgmentEngine{err: errors.New("accepted date must not be replayed")}
		p := &Processor{judgment: engine}
		state := newRecordState(text)
		value := validatedExtraction{Type: "EXPENSE", Amount: "25000", CategorySlug: "dining", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation())}
		decision, err := p.semanticDecisionForRecord(context.Background(), state, value, []string{"dining"}, false)
		if err != nil || !decision.decisionAllowed() || engine.calls != 0 {
			t.Fatalf("text=%q decision=%+v calls=%d err=%v", text, decision, engine.calls, err)
		}
	}
}

func TestUnrepresentableTypedDateIsRejectedWithoutSubstitution(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation())
	_, err := nativeValidatedExtraction(map[string]any{
		"type": "EXPENSE", "amount_idr": "25000", "date_reference": "EXPLICIT", "explicit_date": "2026-02-30",
	}, now)
	if err == nil {
		t.Fatal("an unrepresentable model date must fail exact canonical validation")
	}
}

// T13: an accepted semantic fact with an inactive canonical category is
// rejected/downgraded by Go, not silently replaced with a guess.
func TestAcceptedFactWithInvalidCanonicalEntityIsRejectedNotGuessed(t *testing.T) {
	engine := &stubJudgmentEngine{answers: map[string]judgment.Answer{}}
	p := &Processor{judgment: engine}
	state := newRecordState("indomaret 25rb hari ini")
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Indomaret", CategorySlug: "not-a-real-slug", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 30, 0, 0, jakartaLocation())}
	decision, err := p.semanticDecisionForRecord(context.Background(), state, value, []string{"dining"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() {
		t.Fatalf("an invalid canonical category must not confirm: %+v", decision)
	}
	if engine.calls != 0 {
		t.Fatalf("Go must reject the exact invalid entity without semantic replay; calls=%d", engine.calls)
	}
	if decision.CategorySlug == "dining" {
		t.Fatal("Go must not substitute a guessed category for the invalid slug")
	}
}
