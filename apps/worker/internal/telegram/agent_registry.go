package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"slices"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/analyticscore"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type agentToolClass string

const (
	agentToolRead       agentToolClass = "READ"
	agentToolSideEffect agentToolClass = "SIDE_EFFECT"
)

var agentReadTools = map[string]struct{}{
	"query_spending": {}, "query_cashflow": {}, "query_savings": {},
	"query_wealth": {}, "list_wealth_accounts": {}, "search_transactions": {},
	"list_review_items": {}, "get_evidence_context": {}, "get_transaction_details": {}, toolShowReviewCard: {},
}

func init() {
	for _, tool := range analyticscore.Tools() {
		agentReadTools[tool.Name] = struct{}{}
	}
}

var agentSideEffectTools = map[string]struct{}{
	"record_transaction": {}, "record_transaction_batch": {}, "record_transfer": {},
	"propose_transaction_correction": {}, "resolve_review": {}, "resolve_salary_choice": {},
	"resolve_merchant_learning": {}, "confirm_pending_action": {}, "cancel_pending_action": {},
	"pending_batch_decision": {}, "dispute_evidence_link": {},
}

func agentToolClassFor(name string) (agentToolClass, bool) {
	if _, ok := agentReadTools[name]; ok {
		return agentToolRead, true
	}
	if _, ok := agentSideEffectTools[name]; ok {
		return agentToolSideEffect, true
	}
	return "", false
}

func readOnlyAgentTools(tools []gateway.ToolDefinition) []gateway.ToolDefinition {
	readOnly := make([]gateway.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if class, known := agentToolClassFor(tool.Name); known && class == agentToolSideEffect {
			continue
		}
		readOnly = append(readOnly, tool)
	}
	return readOnly
}

func AgentFinanceTools(categories []string, hasPendingAction, hasPendingBatch, hasActiveReview bool, reviewType string, hasSalaryChoice, hasMerchantLearning bool, reviewMode string) []gateway.ToolDefinition {
	return agentFinanceTools(categories, hasPendingAction, hasPendingBatch, hasActiveReview, reviewType, hasSalaryChoice, hasMerchantLearning, reviewMode)
}

func agentFinanceTools(categories []string, hasPendingAction, hasPendingBatch, hasActiveReview bool, reviewType string, hasSalaryChoice, hasMerchantLearning bool, reviewMode string) []gateway.ToolDefinition {
	base := NativeFinanceTools(categories, hasPendingAction, hasPendingBatch, hasActiveReview, reviewType, hasSalaryChoice, hasMerchantLearning, reviewMode)
	tools := make([]gateway.ToolDefinition, 0, len(base)+4)
	cycleResidual := hasActiveReview && (reviewMode == "CYCLE_RESIDUAL" || reviewType == cycleReviewType)
	for _, tool := range base {
		if tool.Name == "ask_clarification" || tool.Name == "finance_help" || tool.Name == "finance_out_of_scope" {
			continue
		}
		if cycleResidual && tool.Name == "resolve_review" {
			continue
		}
		if _, ok := agentToolClassFor(tool.Name); ok {
			tools = append(tools, tool)
		}
	}

	nullString := map[string]any{"type": []string{"string", "null"}}
	tools = append(tools,
		gateway.ToolDefinition{Name: "get_transaction_details", Description: "Return model-safe authoritative details for one opaque transaction reference from recent results. Never use or request a database UUID.", Parameters: objectSchema(map[string]any{"target_ref": map[string]any{"type": "string"}}, []string{"target_ref"})},
	)

	if hasPendingBatch {
		tools = append(tools, gateway.ToolDefinition{
			Name:        "pending_batch_decision",
			Description: "Act on the one pending transaction batch shown in turn_context.pending_batch. Call it only when the user's message is about that batch: CONFIRM records every item, CANCEL drops the batch, UPDATE changes one item by its item_ref (set confirm_after_update when the user also confirmed). Do not call it for unrelated messages: answer or record those normally and the batch stays pending.",
			Parameters: objectSchema(map[string]any{
				"action":               map[string]any{"type": "string", "enum": []string{"CONFIRM", "CANCEL", "UPDATE"}},
				"item_ref":             nullString,
				"amount_idr":           amountSchema(true),
				"merchant":             nullString,
				"category_slug":        categorySchema(),
				"description":          nullString,
				"confirm_after_update": map[string]any{"type": "boolean"},
			}, []string{"action", "item_ref", "amount_idr", "merchant", "category_slug", "description", "confirm_after_update"}),
		})
	}
	return tools
}

func validateAgentToolCall(call gateway.ToolCall, reviewType, reviewMode string) (map[string]any, error) {
	switch call.Name {
	case "get_transaction_details":
		args, err := decodeAgentArgsRaw[transactionDetailsArgs](call)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(args.TargetRef) == "" || len(args.TargetRef) > 40 {
			return nil, fmt.Errorf("target_ref must be a transaction ref copied from context or results, like a1b2c3d4_p1r1_tx1")
		}
		return remarshal(args), nil
	case toolShowReviewCard:
		return validateShowCardArgs(call)
	case "dispute_evidence_link":
		if _, err := decodeAgentArgsRaw[struct{}](call); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	case "pending_batch_decision":
		args, err := decodeAgentArgsRaw[pendingBatchDecisionArgs](call)
		if err != nil {
			return nil, err
		}
		switch args.Action {
		case "CONFIRM", "CANCEL":
			if args.ItemRef != nil || args.Amount != nil || args.Merchant != nil || args.CategorySlug != nil || args.Description != nil || args.ConfirmAfterUpdate {
				return nil, fmt.Errorf("%s takes no item fields; use UPDATE to change an item", args.Action)
			}
		case "UPDATE":
			if args.ItemRef == nil || !batchItemRefPattern.MatchString(*args.ItemRef) {
				return nil, fmt.Errorf("UPDATE needs item_ref from pending_batch, like batch_1")
			}
			if args.Amount == nil && args.Merchant == nil && args.CategorySlug == nil && args.Description == nil {
				return nil, fmt.Errorf("UPDATE needs at least one changed field")
			}
			if args.Amount != nil {
				value, ok := new(big.Int).SetString(*args.Amount, 10)
				if !ok || value.Sign() <= 0 || value.String() != *args.Amount {
					return nil, fmt.Errorf("amount_idr must be a positive whole rupiah number without separators")
				}
			}
		default:
			return nil, fmt.Errorf("action must be CONFIRM, CANCEL or UPDATE")
		}
		return remarshal(args), nil
	case "resolve_review":
		args, err := decodeAgentArgsRaw[agentResolveReviewArgs](call)
		if err != nil {
			return nil, err
		}
		if !containsAgentReviewAction(args.Action, reviewType, reviewMode) {
			return nil, fmt.Errorf("action must be one of %s, got %q", strings.Join(allowedAgentReviewActions(reviewType, reviewMode), ", "), args.Action)
		}
		for _, allocation := range args.Allocations {
			if strings.TrimSpace(allocation.WealthAccountHint) == "" {
				return nil, fmt.Errorf("each allocation needs wealth_account_hint, a Wealth Account name from list_wealth_accounts")
			}
			amount, ok := new(big.Int).SetString(allocation.AmountIDR, 10)
			if !ok || amount.Sign() <= 0 || amount.String() != allocation.AmountIDR {
				return nil, fmt.Errorf("allocation amount_idr must be a positive whole rupiah number without separators, e.g. 250000")
			}
		}
		return remarshal(args), nil
	default:
		return ValidateNativeToolCall(call)
	}
}

type transactionDetailsArgs struct {
	TargetRef string `json:"target_ref"`
}

type pendingBatchDecisionArgs struct {
	Action             string  `json:"action"`
	ItemRef            *string `json:"item_ref"`
	Amount             *string `json:"amount_idr"`
	Merchant           *string `json:"merchant"`
	CategorySlug       *string `json:"category_slug"`
	Description        *string `json:"description"`
	ConfirmAfterUpdate bool    `json:"confirm_after_update"`
}

var batchItemRefPattern = regexp.MustCompile(`^batch_(10|[1-9])$`)

type agentResidualAllocationInput struct {
	WealthAccountHint string  `json:"wealth_account_hint"`
	AmountIDR         string  `json:"amount_idr"`
	Note              *string `json:"note"`
}

type agentResolveReviewArgs struct {
	Action        string                         `json:"action"`
	CandidateRef  *string                        `json:"candidate_ref"`
	SourceHint    *string                        `json:"source_account_hint"`
	WealthHint    *string                        `json:"wealth_account_hint"`
	CategorySlug  *string                        `json:"category_slug"`
	Merchant      *string                        `json:"merchant"`
	Description   *string                        `json:"description"`
	PayDate       *string                        `json:"pay_date"`
	AmountIDR     *string                        `json:"amount_idr"`
	TransactionAt *string                        `json:"transaction_at"`
	Allocations   []agentResidualAllocationInput `json:"allocations"`
}

func containsAgentReviewAction(action, reviewType, reviewMode string) bool {
	return slices.Contains(allowedAgentReviewActions(reviewType, reviewMode), action)
}

func allowedAgentReviewActions(reviewType, reviewMode string) []string {
	if reviewType != "" || reviewMode != "" {
		return reviewActionEnum(reviewType, reviewMode)
	}
	return reviewdec.AllAgentActions()
}

func decodeAgentArgsRaw[T any](call gateway.ToolCall) (T, error) {
	var zero T
	if call.Name == "" {
		return zero, fmt.Errorf("missing tool name")
	}
	decoder := json.NewDecoder(strings.NewReader(string(call.Arguments)))
	decoder.DisallowUnknownFields()
	var out T
	if err := decoder.Decode(&out); err != nil {
		return zero, fmt.Errorf("decode %s arguments: %w", call.Name, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return zero, fmt.Errorf("decode %s arguments: trailing JSON", call.Name)
	}
	return out, nil
}
