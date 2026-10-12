package telegram

import (
	"context"
	"testing"
	"time"
)

// newRecordState builds the minimal server state agentRecordTransaction needs to
// route a semantic decision without touching Postgres.
func newRecordState(text string) *agentState {
	state := &agentState{
		SourceEventID: "src",
		HouseholdID:   "hh",
		Now:           time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation()),
	}
	state.Update.Message.Text = text
	state.Update.Message.From.ID = 1
	state.Update.Message.Chat.ID = 1
	return state
}

// A complete constrained extraction is accepted directly by Go validation: no
// generative replay. This is the one-generative-call budget.
func TestDirectAcceptanceNeedsNoReplay(t *testing.T) {
	processor := &Processor{}
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Indomaret", CategorySlug: "dining", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 30, 0, 0, jakartaLocation())}
	decision, err := processor.semanticDecisionForRecord(context.Background(), newRecordState("indomaret 25rb hari ini"), value, []string{"dining", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.decisionAllowed() || decision.DecisionSource != "GENERATIVE_EXTRACTION" {
		t.Fatalf("expected direct generative acceptance, got %+v", decision)
	}
}

// A category that is absent or not an active household slug is a genuine
// residual: Go records the dimension and parks the transaction for review
// instead of guessing.
func TestCategoryResidualIsRecordedAndNotConfirmed(t *testing.T) {
	processor := &Processor{}
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Warung Bu Tini", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 30, 0, 0, jakartaLocation())}
	state := newRecordState("warung bu tini 25rb hari ini")
	decision, err := processor.semanticDecisionForRecord(context.Background(), state, value, []string{"food-drink", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() {
		t.Fatalf("an unknown category must not be confirmed, got %+v", decision)
	}
	if len(state.ResidualDimensions) != 1 || state.ResidualDimensions[0] != "category" {
		t.Fatalf("residual dimensions=%v, want [category]", state.ResidualDimensions)
	}
	if decision.DecisionSource != "GENERATIVE_EXTRACTION" {
		t.Fatalf("decision source=%s, want GENERATIVE_EXTRACTION", decision.DecisionSource)
	}
}

// An absent model date remains structurally unresolved; a model-resolved typed
// date is accepted without Go re-parsing the original language.
func TestTypedSemanticDateDoesNotNeedLexicalProof(t *testing.T) {
	processor := &Processor{}
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Kopi", CategorySlug: "dining", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 0, 0, 0, jakartaLocation())}
	state := newRecordState("barusan beli kopi 25k")
	decision, err := processor.semanticDecisionForRecord(context.Background(), state, value, []string{"dining", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.decisionAllowed() {
		t.Fatalf("accepted typed date must survive lexical mismatch, got %+v", decision)
	}
}

func TestSelfDeclaredAmbiguousExtractionCannotDirectConfirm(t *testing.T) {
	processor := &Processor{}
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Indomaret", CategorySlug: "dining", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 30, 0, 0, jakartaLocation()), Ambiguous: true}
	decision, err := processor.semanticDecisionForRecord(context.Background(), newRecordState("indomaret 25rb hari ini"), value, []string{"dining"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() {
		t.Fatalf("an unscoped ambiguity must stay in review: decision=%+v", decision)
	}
}

// Rollback: disabling post-generative direct acceptance parks a complete
// extraction in review without losing the extracted facts.
func TestPostGenerativeAutoConfirmSwitchParksReview(t *testing.T) {
	processor := &Processor{}
	processor.SetPostGenerativeAutoConfirm(false)
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Indomaret", CategorySlug: "dining", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 30, 0, 0, jakartaLocation())}
	decision, err := processor.semanticDecisionForRecord(context.Background(), newRecordState("indomaret 25rb hari ini"), value, []string{"dining", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() {
		t.Fatalf("disabled switch must not authorize confirmation, got %+v", decision)
	}
}
