package telegram

import (
	"context"
	"testing"
	"time"
)

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

func TestModelAmountMustAppearInUserText(t *testing.T) {
	processor := &Processor{}
	value := validatedExtraction{Type: "EXPENSE", Amount: "15000000", Merchant: "PLN", CategorySlug: "dining", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 24, 12, 30, 0, 0, jakartaLocation())}
	decision, err := processor.semanticDecisionForRecord(context.Background(), newRecordState("bayar listrik 1,5jt"), value, []string{"dining"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.AmountSupported || decision.decisionAllowed() {
		t.Fatalf("an amount the user did not state must stay in review, got %+v", decision)
	}
}

func TestModelDateAfterTodayStaysInReview(t *testing.T) {
	processor := &Processor{}
	value := validatedExtraction{Type: "EXPENSE", Amount: "25000", Merchant: "Sepatu", CategorySlug: "dining", DateProvenance: "USER_STATED", TransactionAt: time.Date(2026, 9, 25, 12, 0, 0, 0, jakartaLocation())}
	decision, err := processor.semanticDecisionForRecord(context.Background(), newRecordState("sepatu 25rb 25 Desember"), value, []string{"dining"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.DateSupported || decision.decisionAllowed() {
		t.Fatalf("a transaction after today must stay in review, got %+v", decision)
	}
}
