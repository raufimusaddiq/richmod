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

// stateStringSlice reads a string slice back out of the shared judgment state.
func stateStringSlice(state any, key string) []string {
	payload, ok := state.(map[string]any)
	if !ok {
		return nil
	}
	values, _ := payload[key].([]string)
	return values
}

func transactionBundle(typ string, amountSupported, dateSupported, ambiguous bool, category string) map[string]judgment.Answer {
	criteria := judgmentTypeCriteria
	answers := map[string]judgment.Answer{
		"transaction_type":   confidentChoice(criteria, typ),
		"amount_support":     decidedNoul(map[bool]float64{true: 0.99, false: 0.02}[amountSupported]),
		"date_support":       decidedNoul(map[bool]float64{true: 0.99, false: 0.02}[dateSupported]),
		"material_ambiguity": decidedNoul(map[bool]float64{true: 0.97, false: 0.02}[ambiguous]),
	}
	if category != "" {
		// CategoryCriteria already appends its own OTHER_OR_UNCLEAR option, so the
		// stubbed distribution automatically covers every server-provided label.
		answers["category"] = confidentChoice(map[string]any{"OTHER_OR_UNCLEAR": "not safe", "dining": "active", "transport": "active"}, category)
	}
	return answers
}

func TestTransactionDecisionRequiresSupportedFacts(t *testing.T) {
	processor := &Processor{judgment: &stubJudgmentEngine{answers: transactionBundle("EXPENSE", true, true, false, "dining"), categoryChoice: "dining"}}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "makan siang 50rb", validatedExtraction{Type: "EXPENSE", Amount: "50000", Merchant: "makan siang"}, []string{"dining", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.decisionAllowed() {
		t.Fatalf("supported expense decision should authorize confirmation: %+v", decision)
	}
	if decision.DecisionSource != "JEV" || decision.PolicyVersion != judgmentPolicyVersion {
		t.Fatalf("decision must carry source and policy version: %+v", decision)
	}
}

func TestTransactionDecisionRejectsUnsupportedAmount(t *testing.T) {
	processor := &Processor{judgment: &stubJudgmentEngine{answers: transactionBundle("EXPENSE", false, true, false, "dining")}}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "makan siang 50rb", validatedExtraction{Type: "EXPENSE", Amount: "50000"}, []string{"dining"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() {
		t.Fatal("unsupported amount must not authorize confirmation")
	}
}

func TestTransactionDecisionRejectsUnsupportedDate(t *testing.T) {
	processor := &Processor{judgment: &stubJudgmentEngine{answers: transactionBundle("INCOME", true, false, false, "")}}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "gaji 8 juta", validatedExtraction{Type: "INCOME", Amount: "8000000"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() {
		t.Fatal("unsupported date must not authorize confirmation")
	}
}

func TestTransactionDecisionRejectsMaterialAmbiguity(t *testing.T) {
	processor := &Processor{judgment: &stubJudgmentEngine{answers: transactionBundle("EXPENSE", true, true, true, "dining")}}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "makan siang 50rb", validatedExtraction{Type: "EXPENSE", Amount: "50000"}, []string{"dining"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() {
		t.Fatal("material ambiguity must not authorize confirmation")
	}
}

func TestTransactionDecisionRejectsMissingExpenseCategory(t *testing.T) {
	processor := &Processor{judgment: &stubJudgmentEngine{answers: transactionBundle("EXPENSE", true, true, false, "")}}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "makan siang 50rb", validatedExtraction{Type: "EXPENSE", Amount: "50000"}, []string{"dining", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() {
		t.Fatal("expense without an accepted category must not auto-confirm")
	}
}

// A generative model must never be able to authorize a mutation by grading its
// own answer: only the semantic decision object counts.
func TestGenerativeConfidenceCannotAuthorizeMutation(t *testing.T) {
	rich := validatedExtraction{Type: "EXPENSE", Amount: "50000", Confidence: 0.99, CategoryConfidence: 0.99}
	denied := TransactionSemanticDecision{}
	if denied.decisionAllowed() {
		t.Fatal("zero decision must not confirm")
	}
	processor := &Processor{judgment: &stubJudgmentEngine{answers: transactionBundle("EXPENSE", false, true, false, "dining")}}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "makan siang 50rb", rich, []string{"dining"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() {
		t.Fatal("high generative confidence must not override Jev policy")
	}
}

// A self-graded high generative confidence must be routed through the bounded
// evaluator instead of skipping straight to confirmation.
func TestSelfReportedGenerativeConfidenceForcesJudgment(t *testing.T) {
	engine := &stubJudgmentEngine{answers: transactionBundle("EXPENSE", true, true, false, "dining"), categoryChoice: "dining"}
	processor := &Processor{judgment: engine}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "bayar kopi", validatedExtraction{Type: "EXPENSE", Amount: "25000", CategorySlug: "dining", Confidence: 0.99, CategoryConfidence: 0.99}, []string{"dining"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if engine.calls != 1 {
		t.Fatalf("self-reported confidence must still require one bounded judgment, calls=%d", engine.calls)
	}
	categoryQuestion, exists := engine.request.Questions["category"]
	if !exists {
		t.Fatalf("evaluator must ask for the category it needs; questions=%v", engine.request.Questions)
	}
	criteria, ok := categoryQuestion.Criteria.(map[string]any)
	if !ok || criteria["dining"] == nil {
		t.Fatalf("category criteria must expose the loaded active slugs: %v", categoryQuestion.Criteria)
	}
	if categories := stateStringSlice(engine.request.State, "allowed_category_slugs"); len(categories) != 1 || categories[0] != "dining" {
		t.Fatalf("evaluator must pass the already-loaded categories through state: %v", engine.request.State)
	}
	if _, exists := engine.request.Questions["material_ambiguity"]; !exists {
		t.Fatalf("evaluator must ask for material ambiguity; questions=%v", engine.request.Questions)
	}
	if !decision.decisionAllowed() {
		t.Fatalf("bounded judgment should authorize the confirmation: %+v questions=%v answers=%v", decision, engine.request.Questions, engine.answers)
	}
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

// Both channels must reach the same evaluator with the same policy version.
func TestBothTransactionChannelsShareOneDecisionPolicy(t *testing.T) {
	categories := []string{"dining", "transport"}
	answers := transactionBundle("EXPENSE", true, true, false, "dining")

	fastEngine := &stubJudgmentEngine{answers: answers, categoryChoice: "dining"}
	fast := &Processor{judgment: fastEngine}
	fastDecision := transactionDecisionFromAnswers(judgment.Result{Model: "stub-jev", Answers: answers}, simpleTransactionCandidate{Amount: "50000"}, categories)

	slowEngine := &stubJudgmentEngine{answers: answers, categoryChoice: "dining"}
	slow := &Processor{judgment: slowEngine}
	slowDecision, err := slow.evaluateTransactionSemantics(context.Background(), "src", map[string]any{"merchant": "makan siang"}, categories)
	if err != nil {
		t.Fatal(err)
	}
	if fast == slow {
		t.Fatal("test setup error")
	}
	if fastDecision.TypeAccepted != slowDecision.TypeAccepted || fastDecision.CategorySlug != slowDecision.CategorySlug || fastDecision.TransactionType != slowDecision.TransactionType || fastDecision.MaterialAmbiguity != slowDecision.MaterialAmbiguity {
		t.Fatalf("channels diverged: fast=%+v slow=%+v", fastDecision, slowDecision)
	}
	if fastDecision.PolicyVersion != slowDecision.PolicyVersion || fastDecision.PolicyVersion != judgmentPolicyVersion {
		t.Fatalf("policy version mismatch: %q vs %q", fastDecision.PolicyVersion, slowDecision.PolicyVersion)
	}
}

// A fact-free proposal with an exact deterministic category skips Jev entirely.
func TestExactCategorySkipsJudgmentWithoutCallingProvider(t *testing.T) {

	engine := &stubJudgmentEngine{err: errors.New("must not be called")}
	processor := &Processor{judgment: engine}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "bayar kopi", validatedExtraction{Type: "EXPENSE", Amount: "25000", CategorySlug: "dining"}, []string{"dining"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if engine.calls != 0 {
		t.Fatalf("exact category should not call the provider, calls=%d", engine.calls)
	}
	if !decision.decisionAllowed() || decision.DecisionSource != "DETERMINISTIC_POLICY" {
		t.Fatalf("expected deterministic decision, got %+v", decision)
	}
}

// One round trip: the post-extraction path must take the category out of the same
// bounded bundle that decides direction, support, and ambiguity, instead of
// asking a separate category-only call after extraction (PRD §10, Sprint B).
func TestPostExtractionDecisionCarriesCategoryInOneCall(t *testing.T) {
	engine := &stubJudgmentEngine{answers: transactionBundle("EXPENSE", true, true, false, "dining"), categoryChoice: "dining"}
	processor := &Processor{judgment: engine}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "makan siang 50rb", validatedExtraction{Type: "EXPENSE", Amount: "50000", Merchant: "makan siang"}, []string{"dining", "transport"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if engine.calls != 1 {
		t.Fatalf("expected exactly one bounded call, got %d", engine.calls)
	}
	if !decision.decisionAllowed() {
		t.Fatalf("expected a confirming decision, got %+v", decision)
	}
	if decision.CategorySlug != "dining" {
		t.Fatalf("category must come from the shared bundle, got %q", decision.CategorySlug)
	}
	if _, asked := engine.request.Questions["category"]; !asked {
		t.Fatalf("the same request must carry the category question, questions=%v", engine.request.Questions)
	}
}

// A confirmed merchant rule is deterministic server state, so the decision is
// authorized with no bounded call at all — and it must carry the matched slug,
// because extraction is allowed to send no category in that case (Hermes PR #99).
func TestConfirmedAliasDecisionCarriesMatchedCategory(t *testing.T) {
	engine := &stubJudgmentEngine{err: errors.New("must not be called")}
	processor := &Processor{judgment: engine}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "shopeefood 50rb", validatedExtraction{Type: "EXPENSE", Amount: "50000", Merchant: "ShopeeFood", CategorySlug: "dining"}, []string{"dining"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if engine.calls != 0 {
		t.Fatalf("confirmed alias must not call the provider, calls=%d", engine.calls)
	}
	if !decision.decisionAllowed() {
		t.Fatalf("confirmed alias should authorize confirmation: %+v", decision)
	}
	if decision.CategorySlug != "dining" {
		t.Fatalf("decision must carry the matched slug, got %q", decision.CategorySlug)
	}
}

// Provider failure is infrastructure failure, not semantic uncertainty: Go must
// fail closed instead of trusting whatever the extractor reported.
func TestJudgmentOutageFailsClosed(t *testing.T) {
	processor := &Processor{judgment: &stubJudgmentEngine{err: errors.New("timeout")}}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "makan siang 50rb", validatedExtraction{Type: "EXPENSE", Amount: "50000", Confidence: 0.99}, []string{"dining"}, false)
	if err == nil {
		t.Fatal("provider outage must surface as an error, not a silent decision")
	}
	if decision.decisionAllowed() {
		t.Fatal("outage must not authorize a mutation")
	}
}

// Without a judgment engine, Go cannot authorize a semantic mutation.
func TestUnconfiguredJudgmentCannotConfirmTransaction(t *testing.T) {
	processor := &Processor{}
	decision, err := processor.resolveTransactionDecision(context.Background(), "src", "hh", "makan siang 50rb", validatedExtraction{Type: "EXPENSE", Amount: "50000", Confidence: 1, CategoryConfidence: 1}, []string{"dining"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.decisionAllowed() || decision.DecisionSource != "JUDGMENT_UNAVAILABLE" {
		t.Fatalf("unconfigured judgment must fail closed, got %+v", decision)
	}
}

// The harvested fast path must ask for route, period, and the transaction
// sub-bundle in ONE request (PRD §10) and must reuse already-loaded categories
// (PRD §11).
func TestInitialJudgmentRequestBundlesSpeculativeTransaction(t *testing.T) {
	processor := &Processor{}
	candidate, ok := harvestSimpleTransaction("catat makan siang 50rb hari ini")
	if !ok {
		t.Fatal("expected a harvestable candidate")
	}
	request := processor.initialJudgmentRequest("catat makan siang 50rb hari ini", &turnAgentContextState{Categories: []string{"dining", "transport"}}, candidate)
	for _, key := range []string{"route", "period", "transaction_type", "amount_support", "date_support", "material_ambiguity", "category"} {
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
// every mutation tool while keeping the deterministic READ tools (PRD §8).
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

// A provider outage may only fall through for an obvious READ question.
func TestReadOnlyFallbackOnlyAllowsReads(t *testing.T) {
	for _, text := range []string{"pengeluaran bulan ini berapa", "show my cashflow", "cari transaksi pamella"} {
		if !readOnlyFallbackRequest(text) {
			t.Fatalf("%q should be allowed to degrade to READ-only", text)
		}
	}
	for _, text := range []string{"catat makan siang 50rb", "transfer 2 juta dari jago ke bibit", "gaji 8 juta hari ini", "iya"} {
		if readOnlyFallbackRequest(text) {
			t.Fatalf("%q must not be treated as a read-only fallback", text)
		}
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
// purpose (PRD §13/§14).
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
