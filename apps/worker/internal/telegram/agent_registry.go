package telegram

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

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
	"list_review_items": {}, "get_finance_insight": {},
	"get_category_breakdown": {}, "get_largest_transactions": {}, "get_transaction_details": {},
}

var agentSideEffectTools = map[string]struct{}{
	"record_transaction": {}, "record_transaction_batch": {}, "record_transfer": {},
	"propose_transaction_correction": {}, "resolve_review": {}, "resolve_salary_choice": {},
	"resolve_merchant_learning": {}, "confirm_pending_action": {}, "cancel_pending_action": {},
	"confirm_pending_batch": {}, "cancel_pending_batch": {}, "update_pending_batch": {},
}

func agentToolClassFor(name string) (agentToolClass, bool) {
	if _, ok := agentReadTools[name]; ok { return agentToolRead, true }
	if _, ok := agentSideEffectTools[name]; ok { return agentToolSideEffect, true }
	return "", false
}

func AgentFinanceTools(categories []string, hasPendingAction, hasPendingBatch, hasActiveReview bool, reviewType string, hasSalaryChoice, hasMerchantLearning bool, reviewMode string) []gateway.ToolDefinition {
	base := NativeFinanceTools(categories, hasPendingAction, hasPendingBatch, hasActiveReview, reviewType, hasSalaryChoice, hasMerchantLearning, reviewMode)
	tools := make([]gateway.ToolDefinition, 0, len(base)+4)
	for _, tool := range base {
		// Conversation/help/clarification are ordinary assistant text in Sprint 1.
		if tool.Name == "ask_clarification" || tool.Name == "finance_help" || tool.Name == "finance_out_of_scope" {
			continue
		}
		if _, ok := agentToolClassFor(tool.Name); ok {
			tools = append(tools, tool)
		}
	}
	nullString := map[string]any{"type": []string{"string", "null"}}
	period := map[string]any{"type": "string", "enum": []string{"TODAY", "THIS_WEEK", "LAST_WEEK", "THIS_MONTH", "LAST_MONTH", "CURRENT_CYCLE", "PREVIOUS_CYCLE", "CUSTOM"}}
	periodProps := map[string]any{"period": period, "from_date": nullString, "to_date": nullString}
	tools = append(tools,
		gateway.ToolDefinition{Name: "get_category_breakdown", Description: "Return deterministic confirmed expense/refund totals grouped by household category for a bounded period. Use this to explain what changed; never infer totals yourself.", Parameters: objectSchema(periodProps, []string{"period", "from_date", "to_date"})},
		gateway.ToolDefinition{Name: "get_largest_transactions", Description: "Return the largest confirmed expense/refund transactions for a bounded period using opaque transaction references.", Parameters: objectSchema(map[string]any{"period": period, "from_date": nullString, "to_date": nullString, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 10}}, []string{"period", "from_date", "to_date", "limit"})},
		gateway.ToolDefinition{Name: "get_transaction_details", Description: "Return model-safe authoritative details for one opaque transaction reference from recent results. Never use or request a database UUID.", Parameters: objectSchema(map[string]any{"target_ref": map[string]any{"type": "string"}}, []string{"target_ref"})},
	)
	if hasPendingBatch {
		category := map[string]any{"type": []string{"string", "null"}}
		if len(categories) > 0 {
			values := make([]any, 0, len(categories)+1); values = append(values, nil)
			for _, slug := range categories { values = append(values, slug) }
			category["enum"] = values
		}
		tools = append(tools, gateway.ToolDefinition{Name: "update_pending_batch", Description: "Update exactly one item in the server-bound pending transaction batch using its item_ref. Optionally confirm the entire batch after the deterministic update. This is one logical side effect.", Parameters: objectSchema(map[string]any{
			"item_ref": map[string]any{"type": "string"}, "amount_idr": nullString, "merchant": nullString,
			"category_slug": category, "description": nullString, "confirm_after_update": map[string]any{"type": "boolean"},
		}, []string{"item_ref", "amount_idr", "merchant", "category_slug", "description", "confirm_after_update"})})
	}
	return tools
}

func validateAgentToolCall(call gateway.ToolCall) (map[string]any, error) {
	switch call.Name {
	case "get_category_breakdown":
		return decodeAgentArgs[periodArgs](call, validatePeriodArgs)
	case "get_largest_transactions":
		args, err := decodeAgentArgsRaw[largestTransactionsArgs](call)
		if err != nil { return nil, err }
		if err := validatePeriodArgs(args.periodArgs); err != nil { return nil, err }
		if args.Limit < 1 || args.Limit > 10 { return nil, fmt.Errorf("limit outside range") }
		return remarshal(args), nil
	case "get_transaction_details":
		args, err := decodeAgentArgsRaw[transactionDetailsArgs](call)
		if err != nil { return nil, err }
		if strings.TrimSpace(args.TargetRef) == "" || len(args.TargetRef) > 40 { return nil, fmt.Errorf("invalid transaction reference") }
		return remarshal(args), nil
	case "update_pending_batch":
		args, err := decodeAgentArgsRaw[updatePendingBatchArgs](call)
		if err != nil { return nil, err }
		if !strings.HasPrefix(args.ItemRef, "batch_") { return nil, fmt.Errorf("invalid batch item reference") }
		if args.Amount != nil {
			v, ok := new(big.Int).SetString(*args.Amount, 10)
			if !ok || v.Sign() <= 0 || v.String() != *args.Amount { return nil, fmt.Errorf("invalid batch amount") }
		}
		if args.Amount == nil && args.Merchant == nil && args.CategorySlug == nil && args.Description == nil && !args.ConfirmAfterUpdate {
			return nil, fmt.Errorf("pending batch update is empty")
		}
		return remarshal(args), nil
	default:
		return ValidateNativeToolCall(call)
	}
}

type largestTransactionsArgs struct {
	periodArgs
	Limit int `json:"limit"`
}

type transactionDetailsArgs struct { TargetRef string `json:"target_ref"` }

type updatePendingBatchArgs struct {
	ItemRef string `json:"item_ref"`
	Amount *string `json:"amount_idr"`
	Merchant *string `json:"merchant"`
	CategorySlug *string `json:"category_slug"`
	Description *string `json:"description"`
	ConfirmAfterUpdate bool `json:"confirm_after_update"`
}

func decodeAgentArgsRaw[T any](call gateway.ToolCall) (T, error) {
	var zero T
	if call.Name == "" { return zero, fmt.Errorf("missing tool name") }
	decoder := json.NewDecoder(strings.NewReader(string(call.Arguments)))
	decoder.DisallowUnknownFields()
	var out T
	if err := decoder.Decode(&out); err != nil { return zero, fmt.Errorf("decode %s arguments: %w", call.Name, err) }
	if decoder.More() { return zero, fmt.Errorf("decode %s arguments: trailing JSON", call.Name) }
	return out, nil
}

func decodeAgentArgs[T any](call gateway.ToolCall, validate func(T) error) (map[string]any, error) {
	args, err := decodeAgentArgsRaw[T](call)
	if err != nil { return nil, err }
	if err := validate(args); err != nil { return nil, err }
	return remarshal(args), nil
}

func validatePeriodArgs(args periodArgs) error {
	valid := map[string]bool{"TODAY":true,"THIS_WEEK":true,"LAST_WEEK":true,"THIS_MONTH":true,"LAST_MONTH":true,"CURRENT_CYCLE":true,"PREVIOUS_CYCLE":true,"CUSTOM":true}
	if !valid[args.Period] { return fmt.Errorf("invalid period") }
	if args.Period == "CUSTOM" && (args.FromDate == nil || args.ToDate == nil || strings.TrimSpace(*args.FromDate)=="" || strings.TrimSpace(*args.ToDate)=="") { return fmt.Errorf("custom period requires dates") }
	return nil
}
