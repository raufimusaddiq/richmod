package telegram

import (
	"context"
	"encoding/json"
	"strings"

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

	DecisionSource string // JEV | DETERMINISTIC_POLICY
	Model          string
	PolicyVersion  string
}

// decisionAllowed reports whether Go may persist CONFIRMED from this decision.
// A transaction needs a supported amount, a supported date, an accepted
// direction, and no material ambiguity. EXPENSE additionally needs an accepted
// category because category is user-visible ledger data.
func (d TransactionSemanticDecision) decisionAllowed() bool {
	if !d.RouteAccepted || !d.TypeAccepted || !d.AmountSupported || !d.DateSupported || d.MaterialAmbiguity {
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

func noulSupported(answers map[string]judgment.Answer, key string, policy judgment.NoulPolicy) bool {
	answer, ok := answers[key]
	return ok && judgmentSupported(answer, policy)
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
		"transaction_type":   decision.TransactionType,
		"type_accepted":      decision.TypeAccepted,
		"amount_supported":   decision.AmountSupported,
		"date_supported":     decision.DateSupported,
		"category":           decision.CategorySlug,
		"category_accepted":  decision.CategoryAccepted,
		"material_ambiguity": decision.MaterialAmbiguity,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO judgment_decision(household_id,source_event_id,task,model,policy_version,question_keys,answer_summary_json,outcome) VALUES($1,$2,'TRANSACTION_SEMANTICS',NULLIF($3,''),$4,$5,$6::jsonb,$7)`, householdID, sourceEventID, decision.Model, decision.PolicyVersion, []string{"transaction_type", "amount_support", "date_support", "material_ambiguity", "category"}, string(summary), outcome)
	return err
}
