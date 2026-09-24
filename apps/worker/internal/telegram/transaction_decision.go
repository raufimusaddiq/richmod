package telegram

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// TransactionSemanticDecision is the single authority for mutating a Telegram
// (or, later, email) transaction candidate. Extraction produces facts; this
// object is the semantic ruling over those facts; Go persistence only checks
// this object (PRD §4/§5).
type TransactionSemanticDecision struct {
	RouteAccepted     bool
	TransactionType   string
	TypeAccepted      bool
	CategorySlug      string
	CategoryAccepted  bool
	AmountSupported   bool
	DateSupported     bool
	MaterialAmbiguity bool
	// AmbiguityDecidedNotAmbiguous records a decided *negative* on the ambiguous
	// question, which is the favourable answer. The middle band means the plane
	// could not tell, and must fail closed rather than read as approval.
	AmbiguityDecidedNotAmbiguous bool

	// ResidualCategory records the one named dimension a bounded rescue still had
	// to decide after a generative extraction. It is what lets telemetry tell a
	// valid residual rescue apart from a redundant re-decide (PRD §14.3).
	ResidualCategory bool

	DecisionSource string // JEV | DETERMINISTIC_POLICY | GENERATIVE_EXTRACTION | GENERATIVE_PLUS_JEV
	Model          string
	PolicyVersion  string
}

// decisionAllowed reports whether Go may persist CONFIRMED from this decision.
// A transaction needs a supported amount, a supported date, an accepted
// direction, and no material ambiguity. EXPENSE additionally needs an accepted
// category because category is user-visible ledger data.
func (d TransactionSemanticDecision) decisionAllowed() bool {
	if !d.RouteAccepted || !d.TypeAccepted || !d.AmountSupported || !d.DateSupported {
		return false
	}
	// The ambiguity question is inverted, so only an affirmative *negative* clears
	// it. An exact merchant-category match is decided by deterministic policy
	// without ever asking the plane, and that path is already unambiguous by
	// construction, so it is exempt rather than forced to answer a question it
	// never asked (PRD 6).
	if d.DecisionSource != "DETERMINISTIC_POLICY" && !d.AmbiguityDecidedNotAmbiguous {
		return false
	}
	if d.TransactionType == "INCOME" {
		return true
	}
	return d.TransactionType == "EXPENSE" && d.CategoryAccepted && strings.TrimSpace(d.CategorySlug) != ""
}

// transactionQuestions builds the one shared bounded bundle. The harvested fast
// path (inside the initial route request) and the post-extraction evaluator both
// use it, so a transaction decision cannot diverge by channel (PRD §5/§10).
func transactionQuestions(categories []string, typeHint string) map[string]judgment.Question {
	questions := map[string]judgment.Question{
		"transaction_type":   {Type: "choice", Instructions: "Choose the transaction direction. Use INCOME for money received and EXPENSE for money spent. Use OTHER_OR_UNCLEAR if ambiguous.", Criteria: judgmentTypeCriteria},
		"amount_support":     {Type: "noul", Instructions: "Does the amount clearly belong to the transaction the user asked to record?"},
		"date_support":       {Type: "noul", Instructions: "Does the resolved date clearly match when this transaction happened?"},
		"material_ambiguity": {Type: "noul", Instructions: "Is the request genuinely ambiguous (two or more plausible readings, targets, or amounts)?"},
	}
	// The category question is part of the bundle whenever the turn could be an
	// expense. Both channels therefore send identical question sets: the fast
	// path only pre-harvests EXPENSE/INCOME candidates, and the post-extraction
	// path may not know the direction until this same request answers it.
	if len(categories) > 0 && typeHint != "INCOME" {
		questions["category"] = judgment.Question{Type: "choice", Instructions: "Choose the best active expense category for this purchased item. Use OTHER_OR_UNCLEAR only when no category is safe.", Criteria: judgment.CategoryCriteria(categories)}
	}
	return questions
}

// evaluateTransactionSemantics is the one evaluator used after arbitrary
// extraction. It calls the same builder and mapper as the harvested fast path,
// so both channels consume identical policy (PRD §5).
func (p *Processor) evaluateTransactionSemantics(ctx context.Context, requestID string, state map[string]any, categories []string) (TransactionSemanticDecision, error) {
	typeHint := ""
	if hint, ok := state["transaction_type_hint"].(string); ok {
		typeHint = hint
	}
	result, err := p.evaluate(ctx, judgmentTaskTransaction, requestID, judgment.Request{State: state, Questions: transactionQuestions(categories, typeHint)})
	if err != nil {
		return TransactionSemanticDecision{}, err
	}
	return transactionDecisionFromAnswers(result, simpleTransactionCandidate{}, categories), nil
}

// resolveResidualTransactionDecision asks the bounded plane only for the
// dimensions the caller names as unresolved, using the generative extraction as
// the shared state snapshot. It exists so a complete extraction can skip the
// full semantic replay while still letting Jev rescue a genuinely open fact
// (ADR-045 "Residual bounded rescue"); the question set is exactly the residual
// set, never the whole transaction bundle again.
func (p *Processor) resolveResidualTransactionDecision(ctx context.Context, requestID, userText string, value validatedExtraction, categories []string, jevDimensions []string, missing []string) (TransactionSemanticDecision, error) {
	questions := map[string]judgment.Question{}
	for _, dimension := range jevDimensions {
		switch dimension {
		case "transaction_at":
			// Date is not a bounded semantic choice. Go must ask the user,
			// never ask the model to guess when the transaction happened.
			return TransactionSemanticDecision{}, nil
		case "category":
			if len(categories) > 0 {
				questions["category"] = judgment.Question{Type: "choice", Instructions: "Choose the best active expense category for this purchased item. Use OTHER_OR_UNCLEAR only when no category is safe.", Criteria: judgment.CategoryCriteria(categories)}
			}
		}
	}
	if len(questions) == 0 {
		return TransactionSemanticDecision{}, nil
	}
	state := map[string]any{
		"user_text":              "<untrusted_user_message>" + userText + "</untrusted_user_message>",
		"amount_candidates":      []string{value.Amount},
		"merchant":               value.Merchant,
		"description":            value.Description,
		"transaction_type_hint":  value.Type,
		"allowed_category_slugs": categories,
		"residual_dimensions":    jevDimensions,
	}
	result, err := p.evaluate(ctx, judgmentTaskTransaction, requestID, judgment.Request{State: state, Questions: questions})
	if err != nil {
		return TransactionSemanticDecision{}, err
	}
	decision := transactionDecisionFromAnswers(result, simpleTransactionCandidate{}, categories)
	if len(jevDimensions) == 1 && jevDimensions[0] == "category" {
		if answer, exists := result.Answers["category"]; exists && answer.Choice != "OTHER_OR_UNCLEAR" && judgment.AcceptChoice(answer, judgment.CategoryCriteria(categories), judgmentPolicy.Category) && contains(categories, answer.Choice) {
			decision.CategorySlug, decision.CategoryAccepted = answer.Choice, true
		}
		// Amount/date/type were already accepted deterministically; the only thing
		// Jev was asked to own is the category.
		decision.RouteAccepted = true
		decision.TransactionType = value.Type
		decision.TypeAccepted = true
		decision.AmountSupported = true
		decision.DateSupported = !contains(missing, "transaction_at")
		decision.AmbiguityDecidedNotAmbiguous = true
		decision.ResidualCategory = true
		if decision.CategoryAccepted {
			decision.DecisionSource = "GENERATIVE_PLUS_JEV"
		} else {
			decision.DecisionSource = "GENERATIVE_EXTRACTION"
		}
	}
	return decision, nil
}

// semanticDecisionForRecord is the post-generation routing for one Telegram
// record_transaction call. It accepts a complete extraction directly, and spends
// a Jev call only on dimensions that remain genuinely unresolved, so a clear
// generative result never pays a full second semantic pass (PRD §7.2, ADR-045).
//
// It deliberately does NOT go through resolveTransactionDecision: that function
// sends the whole bundle (direction, amount/date support, ambiguity, category),
// which is the redundant replay. Residual-first routing is the contract here.
// Batch confirmation stays on resolveTransactionDecision because that path is an
// explicit human confirmation, not an autonomous extraction.
func (p *Processor) semanticDecisionForRecord(ctx context.Context, state *agentState, value validatedExtraction, categories []string, exactCategory bool) (TransactionSemanticDecision, error) {
	if direct, ok := directAcceptanceDecision(value, categories, state.Now, state.Update.Message.Text); ok {
		if exactCategory {
			direct.DecisionSource = "DETERMINISTIC_POLICY"
		}
		p.metrics.recordDecision(ctx, judgmentTaskTransaction, judgmentOutcomeAccepted)
		return direct, nil
	}
	if p.judgment == nil {
		state.ResidualDimensions = unresolvedTransactionDimensions(value.Type, strings.TrimSpace(value.CategorySlug) != "" && contains(categories, value.CategorySlug))
		if !userTextSupportsDate(state.Update.Message.Text, value.TransactionAt, state.Now) {
			state.ResidualDimensions = appendIfMissing(state.ResidualDimensions, "transaction_at")
		}
		p.metrics.recordDecision(ctx, judgmentTaskTransaction, judgmentOutcomeJudgmentUnavailable)
		return TransactionSemanticDecision{DecisionSource: "JUDGMENT_UNAVAILABLE", PolicyVersion: judgmentPolicy.Version}, nil
	}
	categoryKnown := strings.TrimSpace(value.CategorySlug) != "" && contains(categories, value.CategorySlug)
	dateSupported := userTextSupportsDate(state.Update.Message.Text, value.TransactionAt, state.Now)
	missing := unresolvedTransactionDimensions(value.Type, categoryKnown)
	if !dateSupported {
		missing = append([]string{"transaction_at"}, missing...)
	}
	if value.Ambiguous {
		// The extractor declared unresolved ambiguity without naming a safe
		// bounded dimension. Preserve the facts for minimal review; do not ask Jev
		// to guess which fact the extractor meant.
		return TransactionSemanticDecision{DecisionSource: "GENERATIVE_EXTRACTION", PolicyVersion: judgmentPolicy.Version}, nil
	}
	if len(missing) == 0 && value.Type == "EXPENSE" && strings.TrimSpace(value.CategorySlug) != "" {
		// A generative category that is not in Go's active household candidate set
		// is invalid, not a new semantic question for the model to grade.
		return TransactionSemanticDecision{}, nil
	}
	if len(missing) == 0 {
		return TransactionSemanticDecision{}, nil
	}
	if state != nil {
		state.ResidualDimensions = missing
	}
	jevDimensions := unresolvedTransactionDimensions(value.Type, categoryKnown)
	if !dateSupported {
		// Date provenance is a user fact, not an eligible semantic rescue.
		// Preserve the residual for review without sending a category judgment
		// that could accidentally imply the whole transaction is accepted.
		return TransactionSemanticDecision{DecisionSource: "GENERATIVE_EXTRACTION", PolicyVersion: judgmentPolicy.Version}, nil
	}
	if len(jevDimensions) > 0 {
		if trace := turnTraceFrom(ctx); trace != nil {
			trace.recordResidual(jevDimensions)
		}
		decision, err := p.resolveResidualTransactionDecision(ctx, state.SourceEventID, state.Update.Message.Text, value, categories, jevDimensions, missing)
		if decision.CategoryAccepted {
			state.ResidualDimensions = removeString(missing, "category")
		}
		return decision, err
	}
	return TransactionSemanticDecision{DecisionSource: "GENERATIVE_EXTRACTION", PolicyVersion: judgmentPolicy.Version}, nil
}

func appendIfMissing(values []string, target string) []string {
	if contains(values, target) {
		return values
	}
	return append(values, target)
}

func removeString(values []string, target string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func noulSupported(answers map[string]judgment.Answer, key string, policy judgment.NoulPolicy) bool {
	answer, ok := answers[key]
	return ok && judgmentSupported(answer, policy)
}

// ambiguityVerdict reads the inverted claim. It returns (isAmbiguous,
// decidedNotAmbiguous) so a caller can require an affirmative not-ambiguous
// ruling instead of treating an undecided answer as approval.
func ambiguityVerdict(answers map[string]judgment.Answer, key string, policy judgment.NoulPolicy) (bool, bool) {
	answer, ok := answers[key]
	if !ok {
		return false, false
	}
	ambiguous, decided := judgment.AcceptNoul(answer, policy)
	return ambiguous, decided && !ambiguous
}

// resolveTransactionDecision obtains the semantic decision for an extracted
// transaction. An exact category match from deterministic merchant rules and an
// already-narrowed fact-free proposal skip Jev; everything else must be ruled
// on by the one shared evaluator.
func (p *Processor) resolveTransactionDecision(ctx context.Context, sourceEventID, householdID, userText string, value validatedExtraction, categories []string, exactCategory bool) (TransactionSemanticDecision, error) {
	if value.Confidence < 0 || value.Confidence > 1 || value.Confidence > judgmentPolicy.Ambiguity.High {
		// A generative model may not grade its own answer into mutation authority
		// (PRD §6). A high self-reported confidence is therefore treated as material
		// ambiguity and must be re-decided by the bounded evaluator.
		value.Ambiguous = true
	}
	if exactCategory && !value.Ambiguous {
		p.metrics.recordDecision(ctx, judgmentTaskTransaction, judgmentOutcomeAccepted)
		return TransactionSemanticDecision{
			RouteAccepted: true, TransactionType: value.Type, TypeAccepted: true,
			AmountSupported: true, DateSupported: true, CategoryAccepted: true, CategorySlug: value.CategorySlug,
			DecisionSource: "DETERMINISTIC_POLICY", PolicyVersion: judgmentPolicy.Version,
		}, nil
	}
	if p.judgment == nil {
		// Fail closed. Without the configured judgment plane Go cannot authorize a
		// semantic mutation, so the proposal is preserved for review instead.
		p.metrics.recordDecision(ctx, judgmentTaskTransaction, judgmentOutcomeJudgmentUnavailable)
		return TransactionSemanticDecision{DecisionSource: "JUDGMENT_UNAVAILABLE", PolicyVersion: judgmentPolicy.Version}, nil
	}
	state := map[string]any{
		"user_text":              "<untrusted_user_message>" + userText + "</untrusted_user_message>",
		"amount_candidates":      []string{value.Amount},
		"date_reference":         value.TransactionAt.In(jakartaLocation()).Format("2006-01-02"),
		"transaction_type_hint":  value.Type,
		"category_hint":          value.CategorySlug,
		"merchant":               value.Merchant,
		"description":            value.Description,
		"allowed_category_slugs": categories,
	}
	return p.evaluateTransactionSemantics(ctx, sourceEventID, state, categories)
}

// directAcceptanceDecision is the source acceptance contract for one extracted
// transaction. It replaces "Jev: did you mean what you already said?" with the
// checks the ADR requires before a generative result may continue (ADR-045
// "Deterministic validation"). It returns false when any dimension is genuinely
// unresolved, which is exactly when the bounded evaluator still earns its call.
func directAcceptanceDecision(value validatedExtraction, categories []string, now time.Time, userText string) (TransactionSemanticDecision, bool) {
	// Schema/type and amount format are re-asserted here rather than trusted,
	// because this function is the gate that skips the model.
	if value.Type != "INCOME" && value.Type != "EXPENSE" {
		return TransactionSemanticDecision{}, false
	}
	amount, ok := new(big.Int).SetString(value.Amount, 10)
	if !ok || amount.Sign() <= 0 || amount.String() != value.Amount {
		return TransactionSemanticDecision{}, false
	}
	// Date provenance must be explicit. OBSERVED_AT_PROCESSING means the caller
	// fell back to processing time, which is not an observed transaction time and
	// must never be canonicalized as one (PRD §9).
	if !acceptableDateProvenance(value.DateProvenance) || value.TransactionAt.IsZero() || now.IsZero() || !userTextSupportsDate(userText, value.TransactionAt, now) {
		return TransactionSemanticDecision{}, false
	}
	// Confidence is intentionally not a canonical gate. It cannot authorize an
	// event by itself, and a low/high self-score does not justify asking Jev to
	// repeat a complete, schema-valid extraction (ADR-045 §Deterministic validation).
	if value.Ambiguous {
		return TransactionSemanticDecision{}, false
	}
	// Category: an accepted active household slug is required for EXPENSE. An
	// absent or inactive slug is a genuine residual, not a reason to guess.
	categoryKnown := strings.TrimSpace(value.CategorySlug) != "" && contains(categories, value.CategorySlug)
	residual := unresolvedTransactionDimensions(value.Type, categoryKnown)
	if len(residual) == 0 {
		return TransactionSemanticDecision{
			RouteAccepted: true, TransactionType: value.Type, TypeAccepted: true,
			AmountSupported: true, DateSupported: true,
			CategorySlug: value.CategorySlug, CategoryAccepted: value.Type == "INCOME" || categoryKnown,
			AmbiguityDecidedNotAmbiguous: true, DecisionSource: "GENERATIVE_EXTRACTION",
			PolicyVersion: judgmentPolicy.Version,
		}, true
	}
	return TransactionSemanticDecision{}, false
}

// unresolvedTransactionDimensions names exactly the residual facts that still
// block canonical confirmation. It is the single place that decides what the
// bounded evaluator is allowed to be asked, so a resolved dimension can never be
// re-sent as if it were open (ADR-045 "Residual bounded rescue").
func unresolvedTransactionDimensions(typ string, categoryKnown bool) []string {
	if typ == "EXPENSE" && !categoryKnown {
		return []string{"category"}
	}
	return nil
}

// acceptableDateProvenance reports whether the resolved date came from source
// evidence or an explicit user statement, rather than the processing fallback.
func acceptableDateProvenance(provenance string) bool {
	return provenance == "USER_STATED"
}

// userTextSupportsDate ties the model's constrained date reference back to the
// source event. TODAY/YESTERDAY and explicit ISO dates are accepted only when
// the user actually supplied that date signal; the model cannot manufacture a
// missing date to pass the canonical gate.
func userTextSupportsDate(userText string, at, now time.Time) bool {
	if strings.TrimSpace(userText) == "" || at.IsZero() {
		return false
	}
	text := strings.ToLower(userText)
	localDate := at.In(jakartaLocation()).Format("2006-01-02")
	if explicit := simpleDatePattern.FindString(text); explicit != "" {
		return explicit == localDate
	}
	if strings.Contains(text, "kemarin") || strings.Contains(text, "yesterday") {
		return at.In(jakartaLocation()).Format("2006-01-02") == now.In(jakartaLocation()).AddDate(0, 0, -1).Format("2006-01-02")
	}
	if strings.Contains(text, "hari ini") || strings.Contains(text, "today") {
		return at.In(jakartaLocation()).Format("2006-01-02") == now.In(jakartaLocation()).Format("2006-01-02")
	}
	// Older relative dates are only accepted when the user explicitly used a
	// supported phrase; missing date evidence remains missing.
	for phrase, offset := range map[string]int{"dua hari lalu": -2, "2 hari lalu": -2, "three days ago": -3, "3 hari lalu": -3, "seminggu lalu": -7, "last week": -7} {
		if strings.Contains(text, phrase) {
			return at.In(jakartaLocation()).Format("2006-01-02") == now.In(jakartaLocation()).AddDate(0, 0, offset).Format("2006-01-02")
		}
	}
	return false
}

// turnAgentContextState is the loaded server state shared by the initial
// judgment bundle (PRD §11): categories and pending-workflow flags are read
// once by ProcessAgent and must not be re-queried per fast path.
type turnAgentContextState struct {
	Categories          []string
	HasPendingAction    bool
	HasPendingBatch     bool
	HasSalaryChoice     bool
	HasMerchantLearning bool
	HasPendingWorkflow  bool
	ActiveReviewCount   int
	ExactReply          bool
	// Route is the decided Jev route for this turn, filled by the fast path and
	// read back by ProcessAgent so implicit workflow bindings are narrowed only
	// when the route names that interaction (ADR-038 amendment).
	Route string
}

func (s turnAgentContextState) harvestable() bool {
	return !s.HasPendingWorkflow && !s.ExactReply && s.ActiveReviewCount == 0
}

// recordJudgmentDecision persists bounded decision provenance in the same
// transaction as the canonical mutation, so a decision can never be recorded
// without its outcome (PRD §15/§16). Only bounded decision values are stored —
// never raw user text, email bodies, or model state.
func (p *Processor) recordJudgmentDecision(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, decision TransactionSemanticDecision, confirmed bool) error {
	outcome := "NEEDS_REVIEW"
	if confirmed {
		outcome = "CONFIRMED"
	}
	summary, err := json.Marshal(map[string]any{
		"transaction_type":                decision.TransactionType,
		"type_accepted":                   decision.TypeAccepted,
		"amount_supported":                decision.AmountSupported,
		"date_supported":                  decision.DateSupported,
		"category":                        decision.CategorySlug,
		"category_accepted":               decision.CategoryAccepted,
		"material_ambiguity":              decision.MaterialAmbiguity,
		"ambiguity_decided_not_ambiguous": decision.AmbiguityDecidedNotAmbiguous,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO judgment_decision(household_id,source_event_id,task,model,policy_version,question_keys,answer_summary_json,outcome) VALUES($1,$2,'TRANSACTION_SEMANTICS',NULLIF($3,''),$4,$5,$6::jsonb,$7)`, householdID, sourceEventID, decision.Model, decision.PolicyVersion, []string{"transaction_type", "amount_support", "date_support", "material_ambiguity", "category"}, string(summary), outcome)
	return err
}
