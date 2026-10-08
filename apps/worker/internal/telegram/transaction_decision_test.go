package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// stubJudgmentEngine answers a fixed bundle and can simulate provider failure.
// Category answers are rebuilt against the criteria the processor actually
// sent, so the stub models a real provider instead of a guessed option set.
type stubJudgmentEngine struct {
	answers map[string]judgment.Answer
	err     error
	calls   int
	request judgment.Request
	// categoryChoice is the label the stub picks when the processor asks a
	// category question with server-provided criteria.
	categoryChoice string
}

func (s *stubJudgmentEngine) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	s.calls++
	s.request = request
	if s.err != nil {
		return judgment.Result{}, s.err
	}
	answers := map[string]judgment.Answer{}
	for key, value := range s.answers {
		answers[key] = value
	}
	if question, exists := request.Questions["category"]; exists {
		criteria, ok := question.Criteria.(map[string]any)
		if !ok {
			return judgment.Result{}, errors.New("stub: category criteria is not a map")
		}
		choice := s.categoryChoice
		if choice == "" {
			choice = "OTHER_OR_UNCLEAR"
		}
		answers["category"] = confidentChoice(criteria, choice)
	}
	return judgment.Result{Model: "stub-jev", Answers: answers}, nil
}

func confidentChoice(criteria map[string]any, label string) judgment.Answer {
	answer := judgment.Answer{Type: "choice", Choice: label, HasConfidence: true, Confidence: 0.95, Distribution: map[string]float64{}}
	for key := range criteria {
		answer.Distribution[key] = 0.001
	}
	answer.Distribution[label] = 1 - 0.001*float64(len(criteria)-1)
	answer.Probability = answer.Distribution[label]
	return answer
}

func decidedNoul(value float64) judgment.Answer {
	return judgment.Answer{Type: "noul", Noul: value, HasNoul: true}
}

// The ambiguity claim is inverted and must not be satisfied by an undecided
// middle-band answer: only a decided negative opens the auto-confirm path.
func TestUndecidedAmbiguityDoesNotAuthorizeConfirmation(t *testing.T) {
	criteria := judgmentTypeCriteria
	decided := transactionDecisionFromAnswers(judgment.Result{Model: "stub-jev", Answers: map[string]judgment.Answer{
		"transaction_type":   confidentChoice(criteria, "EXPENSE"),
		"amount_support":     decidedNoul(0.99),
		"date_support":       decidedNoul(0.99),
		"material_ambiguity": decidedNoul(0.02),
		"category":           confidentChoice(judgment.CategoryCriteria([]string{"dining"}), "dining"),
	}}, simpleTransactionCandidate{Amount: "50000"}, []string{"dining"})
	if !decided.decisionAllowed() {
		t.Fatalf("a decided not-ambiguous ruling must authorize: %+v", decided)
	}

	undecided := transactionDecisionFromAnswers(judgment.Result{Model: "stub-jev", Answers: map[string]judgment.Answer{
		"transaction_type":   confidentChoice(criteria, "EXPENSE"),
		"amount_support":     decidedNoul(0.99),
		"date_support":       decidedNoul(0.99),
		"material_ambiguity": decidedNoul(0.10),
		"category":           confidentChoice(judgment.CategoryCriteria([]string{"dining"}), "dining"),
	}}, simpleTransactionCandidate{Amount: "50000"}, []string{"dining"})
	if undecided.decisionAllowed() {
		t.Fatalf("an undecided ambiguity ruling must fail closed: %+v", undecided)
	}
}

// The harvested fast path must ask for route, period, and the transaction
// sub-bundle in ONE request and must reuse already-loaded categories.
func TestInitialJudgmentRequestBundlesSpeculativeTransaction(t *testing.T) {
	processor := &Processor{}
	candidate, ok := harvestSimpleTransaction("catat makan siang 50rb hari ini")
	if !ok {
		t.Fatal("expected a harvestable candidate")
	}
	request := processor.initialJudgmentRequest("catat makan siang 50rb hari ini", &turnAgentContextState{Categories: []string{"dining", "transport"}}, candidate)
	for _, key := range []string{"route", "period", "transaction_type", "amount_support", "date_support", "date_reference", "material_ambiguity", "category"} {
		if _, exists := request.Questions[key]; !exists {
			t.Fatalf("initial bundle missing question %q", key)
		}
	}
	state, ok := request.State.(map[string]any)
	if !ok {
		t.Fatalf("state payload has unexpected type: %T", request.State)
	}
	if state["allowed_category_slugs"] == nil {
		t.Fatal("already-loaded categories must be part of the shared state")
	}
}

func TestInitialJudgmentRequestSkipsTransactionQuestionsWithoutCandidate(t *testing.T) {
	processor := &Processor{}
	request := processor.initialJudgmentRequest("pengeluaran bulan ini berapa", &turnAgentContextState{Categories: []string{"dining"}}, simpleTransactionCandidate{})
	if _, exists := request.Questions["transaction_type"]; exists {
		t.Fatal("a READ turn must not ship speculative transaction questions")
	}
	if _, exists := request.Questions["route"]; !exists {
		t.Fatal("route question is always required")
	}
}

func TestInitialJudgmentRequestIncludesOnlyUniqueReviewMetadata(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		request := (&Processor{}).initialJudgmentRequest("grab", &turnAgentContextState{
			ActiveReviewCount: count, ReviewType: "UNKNOWN_MERCHANT", ReviewConversationState: "AWAITING_MERCHANT",
		}, simpleTransactionCandidate{})
		payload := request.State.(map[string]any)
		if payload["active_review_count"] != count {
			t.Fatalf("review count = %v, want %d", payload["active_review_count"], count)
		}
		review, exists := payload["active_review"].(map[string]any)
		if exists != (count == 1) {
			t.Fatalf("count=%d review=%v", count, review)
		}
		if exists && (len(review) != 3 || review["review_type"] != "UNKNOWN_MERCHANT" || review["conversation_state"] != "AWAITING_MERCHANT" || review["awaiting_field"] != "merchant") {
			t.Fatalf("unexpected model-visible review metadata: %v", review)
		}
	}
}

// A pending workflow or an explicit reply is server-bound; speculative
// transaction harvesting must not run and race that binding.
func TestHarvestingIsSuppressedForServerBoundTurns(t *testing.T) {
	if (turnAgentContextState{}).harvestable() != true {
		t.Fatal("a plain turn should allow harvesting")
	}
	for _, state := range []turnAgentContextState{
		{HasPendingWorkflow: true},
		{ExactReply: true},
		{ActiveReviewCount: 1},
	} {
		if state.harvestable() {
			t.Fatalf("server-bound turn must suppress harvesting: %+v", state)
		}
	}
	processor := &Processor{}
	request := processor.initialJudgmentRequest("ya", &turnAgentContextState{HasPendingBatch: true, HasPendingWorkflow: true, Categories: []string{"dining"}}, simpleTransactionCandidate{})
	if _, exists := request.Questions["transaction_type"]; exists {
		t.Fatal("pending workflow must not receive speculative transaction questions")
	}
}

// When the judgment plane is unavailable the conversational surface must lose
// every mutation tool while keeping the deterministic READ tools.
func TestDegradedToolSurfaceHasNoMutationAuthority(t *testing.T) {
	tools := agentFinanceTools([]string{"dining"}, false, true, true, "TRANSFER_CLASSIFICATION", true, true, "TRANSFER_CLASSIFICATION", false)
	for _, tool := range tools {
		if class, known := agentToolClassFor(tool.Name); known && class == agentToolSideEffect {
			t.Fatalf("degraded surface exposed mutation tool %q", tool.Name)
		}
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, required := range []string{"query_spending", "query_cashflow", "query_savings", "query_wealth", "search_transactions", "list_review_items"} {
		if !names[required] {
			t.Fatalf("degraded surface must keep READ tool %q", required)
		}
	}
	configured := agentFinanceTools([]string{"dining"}, false, false, false, "", false, false, "", true)
	if len(configured) <= len(tools) {
		t.Fatal("a configured worker must expose the full tool surface")
	}
}

// The tool catalog must expose the ReadTool/side-effect classification used by
// the degraded surface, so a new mutation tool cannot silently bypass it.
func TestEveryExposedMutationToolIsClassified(t *testing.T) {
	for _, tool := range AgentFinanceTools([]string{"dining"}, true, true, true, "TRANSFER_CLASSIFICATION", true, true, "TRANSFER_CLASSIFICATION") {
		if _, known := agentToolClassFor(tool.Name); !known {
			t.Fatalf("tool %q has no agent tool class", tool.Name)
		}
	}
	_ = gateway.ToolCall{}
}

// stubPurposeEngine answers a transfer-purpose Choice against the criteria the
// processor actually sent, so the stub models a real provider.
type stubPurposeEngine struct {
	choice  string
	err     error
	calls   int
	request judgment.Request
}

func (s *stubPurposeEngine) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	s.calls++
	s.request = request
	if s.err != nil {
		return judgment.Result{}, s.err
	}
	question, ok := request.Questions["purpose"]
	if !ok {
		return judgment.Result{}, errors.New("stub: no purpose question")
	}
	criteria, ok := question.Criteria.(map[string]any)
	if !ok {
		return judgment.Result{}, errors.New("stub: criteria is not a map")
	}
	return judgment.Result{Model: "stub-jev", Answers: map[string]judgment.Answer{"purpose": confidentChoice(criteria, s.choice)}}, nil
}

// The canonical transfer purpose must come from the bounded decision, never from
// the tool contract, and an unclear answer must fail closed instead of picking a
// purpose.
func TestTransferPurposeComesFromJudgmentNotToolArguments(t *testing.T) {
	engine := &stubPurposeEngine{choice: "INVESTMENT_CONTRIBUTION"}
	processor := &Processor{judgment: engine}
	destination := "wealth-1"
	purpose, _, ok, err := processor.resolveTransferPurpose(context.Background(), "src", "beli reksa dana", "3000000", "Bank Jago", "RDN", &destination)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || purpose != "INVESTMENT_CONTRIBUTION" {
		t.Fatalf("expected the bounded purpose, got %q ok=%v", purpose, ok)
	}
	if engine.calls != 1 {
		t.Fatalf("expected one bounded call, got %d", engine.calls)
	}
	if _, offered := engine.request.Questions["purpose"]; !offered {
		t.Fatalf("questions=%v", engine.request.Questions)
	}
	state, _ := engine.request.State.(map[string]any)
	if got := state["destination_kind"]; got != "WEALTH_ACCOUNT" {
		t.Fatalf("destination_kind=%v", got)
	}
}

func TestTransferPurposeUnclearFailsClosed(t *testing.T) {
	engine := &stubPurposeEngine{choice: "OTHER_OR_UNCLEAR"}
	processor := &Processor{judgment: engine}
	purpose, _, ok, err := processor.resolveTransferPurpose(context.Background(), "src", "transfer saja", "2000000", "Jago", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok || purpose != "" {
		t.Fatalf("unclear purpose must fail closed, got %q ok=%v", purpose, ok)
	}
	state, _ := engine.request.State.(map[string]any)
	if got := state["destination_kind"]; got != "NONE" {
		t.Fatalf("a missing destination must be reported as NONE, got %v", got)
	}
}

func TestTransferPurposeProviderFailureIsInfrastructure(t *testing.T) {
	engine := &stubPurposeEngine{err: errors.New("gateway down")}
	processor := &Processor{judgment: engine}
	if _, _, ok, err := processor.resolveTransferPurpose(context.Background(), "src", "d", "1000", "Jago", "RDN", stringPtr("wealth-1")); err == nil || ok {
		t.Fatalf("provider failure must surface as an error, got ok=%v err=%v", ok, err)
	}
}
