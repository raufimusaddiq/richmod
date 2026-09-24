package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// newRecordState builds the minimal server state agentRecordTransaction needs to
// route a semantic decision without touching Postgres.
func newRecordState(text string) *agentState {
	state := &agentState{
		SourceEventID: "src",
		HouseholdID:   "hh",
		Now:           time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation()),
		Route:         "NEEDS_GENERATIVE_AGENT",
	}
	state.Update.Message.Text = text
	state.Update.Message.From.ID = 1
	state.Update.Message.Chat.ID = 1
	return state
}

// A complete constrained extraction is accepted directly: no Jev call, no
// generative replay. This is the IR-04 budget the old path always violated.
func TestDirectAcceptanceNeedsNoJudgmentCall(t *testing.T) {
	engine := &stubJudgmentEngine{err: errors.New("must not be called")}
	processor := &Processor{judgment: engine}
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Indomaret", CategorySlug: "dining", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 30, 0, 0, jakartaLocation())}
	decision, err := processor.semanticDecisionForRecord(context.Background(), newRecordState("indomaret 25rb hari ini"), value, []string{"dining", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if engine.calls != 0 {
		t.Fatalf("a complete extraction must not call Jev, calls=%d", engine.calls)
	}
	if !decision.decisionAllowed() || decision.DecisionSource != "GENERATIVE_EXTRACTION" {
		t.Fatalf("expected direct generative acceptance, got %+v", decision)
	}
}

// A category-only residual spends exactly one bounded call and asks only the
// category question, never the whole transaction bundle again.
func TestCategoryResidualAsksOnlyTheCategoryDimension(t *testing.T) {
	engine := &stubJudgmentEngine{categoryChoice: "food-drink"}
	processor := &Processor{judgment: engine}
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Warung Bu Tini", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 30, 0, 0, jakartaLocation())}
	decision, err := processor.semanticDecisionForRecord(context.Background(), newRecordState("warung bu tini 25rb hari ini"), value, []string{"food-drink", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if engine.calls != 1 {
		t.Fatalf("one residual dimension must cost one call, calls=%d", engine.calls)
	}
	if len(engine.request.Questions) != 1 {
		t.Fatalf("residual rescue must ask exactly one question, got %v", engine.request.Questions)
	}
	if _, asked := engine.request.Questions["category"]; !asked {
		t.Fatalf("the residual question must be the category, got %v", engine.request.Questions)
	}
	for _, forbidden := range []string{"transaction_type", "amount_support", "date_support", "material_ambiguity"} {
		if _, asked := engine.request.Questions[forbidden]; asked {
			t.Fatalf("residual rescue must not repeat %q; questions=%v", forbidden, engine.request.Questions)
		}
	}
	if !decision.decisionAllowed() || !decision.ResidualCategory || decision.DecisionSource != "GENERATIVE_PLUS_JEV" {
		t.Fatalf("expected a residual-category confirmation, got %+v", decision)
	}
}

// Missing date is not a semantic question. Jev must not be asked to guess it,
// and the decision must stay unresolved so Go asks the user for that one fact.
func TestMissingDateIsNotSentToJudgment(t *testing.T) {
	engine := &stubJudgmentEngine{categoryChoice: "dining"}
	processor := &Processor{judgment: engine}
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Warung Bu Tini", CategorySlug: "dining", TransactionAt: time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation()), TimePrecision: "OBSERVED_AT_PROCESSING"}
	state := newRecordState("warung bu tini 25rb")
	state.Route = "NEEDS_GENERATIVE_AGENT"
	decision, err := processor.semanticDecisionForRecord(context.Background(), state, value, []string{"dining", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() {
		t.Fatalf("a missing date must not authorize confirmation, got %+v", decision)
	}
	if engine.calls != 0 {
		t.Fatalf("Jev must not be used to guess a missing date, calls=%d", engine.calls)
	}
	if !contains(state.ResidualDimensions, "transaction_at") {
		t.Fatalf("missing user date must persist as a residual, got %v", state.ResidualDimensions)
	}
	for _, question := range engine.request.Questions {
		if question.Type == "noul" {
			t.Fatalf("a missing date must not be guessed by any model question, got %v", engine.request.Questions)
		}
	}
}

func TestSelfDeclaredAmbiguousExtractionCannotDirectConfirm(t *testing.T) {
	engine := &stubJudgmentEngine{err: errors.New("generic ambiguity is not a bounded residual")}
	processor := &Processor{judgment: engine}
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Indomaret", CategorySlug: "dining", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 30, 0, 0, jakartaLocation()), Ambiguous: true}
	decision, err := processor.semanticDecisionForRecord(context.Background(), newRecordState("indomaret 25rb hari ini"), value, []string{"dining"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if engine.calls != 0 || decision.decisionAllowed() {
		t.Fatalf("an unscoped ambiguity must stay in review without Jev replay: calls=%d decision=%+v", engine.calls, decision)
	}
}

func TestRecordTransactionRequiresMutationRoute(t *testing.T) {
	state := newRecordState("makan siang 25rb hari ini")
	state.Route = "READ_SPENDING"
	processor := &Processor{}
	_, _, err := processor.agentRecordTransaction(context.Background(), state, gateway.ToolCall{Name: "record_transaction"}, nil, gateway.Metadata{})
	if err == nil {
		t.Fatal("record_transaction must reject a non-mutation route before touching storage")
	}
}
