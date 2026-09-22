package telegram

import (
	"encoding/json"
	"fmt"
	"io"
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
	"pending_batch_decision": {},
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

func AgentFinanceTools(categories []string, hasPendingAction, hasPendingBatch, hasActiveReview bool, reviewType string, hasSalaryChoice, hasMerchantLearning bool, reviewMode string) []gateway.ToolDefinition {
	return agentFinanceTools(categories, hasPendingAction, hasPendingBatch, hasActiveReview, reviewType, hasSalaryChoice, hasMerchantLearning, reviewMode, true)
}

// agentFinanceTools keeps the judgment-plane configuration explicit so the
// degraded READ-only surface is a deliberate parameter instead of a hidden
// package-global default.
func agentFinanceTools(categories []string, hasPendingAction, hasPendingBatch, hasActiveReview bool, reviewType string, hasSalaryChoice, hasMerchantLearning bool, reviewMode string, judgmentConfigured bool) []gateway.ToolDefinition {
	base := NativeFinanceTools(categories, hasPendingAction, hasPendingBatch, hasActiveReview, reviewType, hasSalaryChoice, hasMerchantLearning, reviewMode)
	tools := make([]gateway.ToolDefinition, 0, len(base)+4)
	cycleResidual := hasActiveReview && (reviewMode == "CYCLE_RESIDUAL" || reviewType == "CYCLE_RESIDUAL_ALLOCATION")
	// When the judgment plane is unavailable, no bounded semantic mutation is
	// authorized. The conversational surface keeps READ tools only, so a
	// provider outage can never turn into a hidden LLM auto-mutation (PRD §8).
	readOnly := !judgmentConfigured
	for _, tool := range base {
		if readOnly {
			if class, known := agentToolClassFor(tool.Name); known && class == agentToolSideEffect {
				continue
			}
		}
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
	period := map[string]any{"type": "string", "enum": []string{"TODAY", "THIS_WEEK", "LAST_WEEK", "THIS_MONTH", "LAST_MONTH", "CURRENT_CYCLE", "PREVIOUS_CYCLE", "CUSTOM"}}
	periodProps := map[string]any{"period": period, "from_date": nullString, "to_date": nullString}
	tools = append(tools,
		gateway.ToolDefinition{Name: "get_category_breakdown", Description: "Return deterministic confirmed expense/refund totals grouped by household category for a bounded period. Use this to explain what changed; never infer totals yourself.", Parameters: objectSchema(periodProps, []string{"period", "from_date", "to_date"})},
		gateway.ToolDefinition{Name: "get_largest_transactions", Description: "Return the largest confirmed expense/refund transactions for a bounded period using opaque transaction references.", Parameters: objectSchema(map[string]any{"period": period, "from_date": nullString, "to_date": nullString, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 10}}, []string{"period", "from_date", "to_date", "limit"})},
		gateway.ToolDefinition{Name: "get_transaction_details", Description: "Return model-safe authoritative details for one opaque transaction reference from recent results. Never use or request a database UUID.", Parameters: objectSchema(map[string]any{"target_ref": map[string]any{"type": "string"}}, []string{"target_ref"})},
	)

	if cycleResidual && !readOnly {
		allocation := objectSchema(map[string]any{
			"wealth_account_hint": map[string]any{"type": "string", "description": "Human-readable Wealth Account name/institution hint. Never use a database UUID."},
			"amount_idr":          map[string]any{"type": "string"},
			"note":                nullString,
		}, []string{"wealth_account_hint", "amount_idr", "note"})
		tools = append(tools, gateway.ToolDefinition{
			Name:        "resolve_review",
			Description: "Resolve the server-bound salary-cycle residual review. Wealth allocations use human-readable account hints; Go resolves them uniquely and keeps canonical IDs private.",
			Parameters: objectSchema(map[string]any{
				"action":              map[string]any{"type": "string", "enum": []string{"ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED", "TRANSACTION_MISSING"}},
				"candidate_ref":       nullString,
				"source_account_hint": nullString,
				"wealth_account_hint": nullString,
				"category_slug":       nullString,
				"merchant":            nullString,
				"description":         nullString,
				"pay_date":            nullString,
				"amount_idr":          nullString,
				"transaction_at":      nullString,
				"allocations": map[string]any{
					"type":  "array",
					"items": allocation,
				},
			}, []string{"action", "candidate_ref", "source_account_hint", "wealth_account_hint", "category_slug", "merchant", "description", "pay_date", "amount_idr", "transaction_at", "allocations"}),
		})
	}

	if hasPendingBatch && !readOnly {
		category := map[string]any{"type": []string{"string", "null"}}
		if len(categories) > 0 {
			values := make([]any, 0, len(categories)+1)
			values = append(values, nil)
			for _, slug := range categories {
				values = append(values, slug)
			}
			category["enum"] = values
		}
		tools = append(tools, gateway.ToolDefinition{
			Name:        "pending_batch_decision",
			Description: "Required decision for the one active pending transaction batch. Call this for every user reply while the batch is pending. Map natural confirmations such as iya, iya bener, betul, benar, setuju, oke to CONFIRM; refusals to CANCEL; requested changes to UPDATE; unrelated questions to DEFER. Never answer plain text before calling this tool.",
			Parameters: objectSchema(map[string]any{
				"action":        map[string]any{"type": "string", "enum": []string{"CONFIRM", "CANCEL", "UPDATE", "DEFER"}},
				"item_ref":      map[string]any{"type": []string{"string", "null"}},
				"amount_idr":    map[string]any{"type": []string{"string", "null"}},
				"merchant":      map[string]any{"type": []string{"string", "null"}},
				"category_slug": category,
				"description":   map[string]any{"type": []string{"string", "null"}},
			}, []string{"action", "item_ref", "amount_idr", "merchant", "category_slug", "description"}),
		})
		tools = append(tools, gateway.ToolDefinition{
			Name:        "update_pending_batch",
			Description: "Update exactly one item in the server-bound pending transaction batch using its item_ref. Optionally confirm the entire batch after the deterministic update. This is one logical side effect.",
			Parameters: objectSchema(map[string]any{
				"item_ref":             map[string]any{"type": "string"},
				"amount_idr":           nullString,
				"merchant":             nullString,
				"category_slug":        category,
				"description":          nullString,
				"confirm_after_update": map[string]any{"type": "boolean"},
			}, []string{"item_ref", "amount_idr", "merchant", "category_slug", "description", "confirm_after_update"}),
		})
	}
	return tools
}

func validateAgentToolCall(call gateway.ToolCall) (map[string]any, error) {
	switch call.Name {
	case "get_category_breakdown":
		return decodeAgentArgs[periodArgs](call, validatePeriodArgs)
	case "get_largest_transactions":
		args, err := decodeAgentArgsRaw[largestTransactionsArgs](call)
		if err != nil {
			return nil, err
		}
		if err := validatePeriodArgs(args.periodArgs); err != nil {
			return nil, err
		}
		if args.Limit < 1 || args.Limit > 10 {
			return nil, fmt.Errorf("limit outside range")
		}
		return remarshal(args), nil
	case "get_transaction_details":
		args, err := decodeAgentArgsRaw[transactionDetailsArgs](call)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(args.TargetRef) == "" || len(args.TargetRef) > 40 {
			return nil, fmt.Errorf("invalid transaction reference")
		}
		return remarshal(args), nil
	case "update_pending_batch":
		args, err := decodeAgentArgsRaw[updatePendingBatchArgs](call)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(args.ItemRef, "batch_") {
			return nil, fmt.Errorf("invalid batch item reference")
		}
		if args.Amount != nil {
			value, ok := new(big.Int).SetString(*args.Amount, 10)
			if !ok || value.Sign() <= 0 || value.String() != *args.Amount {
				return nil, fmt.Errorf("invalid batch amount")
			}
		}
		if args.Amount == nil && args.Merchant == nil && args.CategorySlug == nil && args.Description == nil && !args.ConfirmAfterUpdate {
			return nil, fmt.Errorf("pending batch update is empty")
		}
		return remarshal(args), nil
	case "pending_batch_decision":
		args, err := decodeAgentArgsRaw[pendingBatchDecisionArgs](call)
		if err != nil {
			return nil, err
		}
		switch args.Action {
		case "CONFIRM", "CANCEL", "DEFER":
			if args.ItemRef != nil || args.Amount != nil || args.Merchant != nil || args.CategorySlug != nil || args.Description != nil {
				return nil, fmt.Errorf("unexpected update fields for %s", args.Action)
			}
		case "UPDATE":
			if args.ItemRef == nil || !strings.HasPrefix(*args.ItemRef, "batch_") {
				return nil, fmt.Errorf("update requires batch item_ref")
			}
			if args.Amount == nil && args.Merchant == nil && args.CategorySlug == nil && args.Description == nil {
				return nil, fmt.Errorf("update is empty")
			}
			if args.Amount != nil {
				value, ok := new(big.Int).SetString(*args.Amount, 10)
				if !ok || value.Sign() <= 0 || value.String() != *args.Amount {
					return nil, fmt.Errorf("invalid batch amount")
				}
			}
		default:
			return nil, fmt.Errorf("invalid pending batch action")
		}
		return remarshal(args), nil
	case "resolve_review":
		args, err := decodeAgentArgsRaw[agentResolveReviewArgs](call)
		if err != nil {
			return nil, err
		}
		if !containsAgentReviewAction(args.Action) {
			return nil, fmt.Errorf("invalid review action")
		}
		for _, allocation := range args.Allocations {
			if strings.TrimSpace(allocation.WealthAccountHint) == "" {
				return nil, fmt.Errorf("missing wealth account hint")
			}
			amount, ok := new(big.Int).SetString(allocation.AmountIDR, 10)
			if !ok || amount.Sign() <= 0 || amount.String() != allocation.AmountIDR {
				return nil, fmt.Errorf("invalid residual allocation amount")
			}
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

type transactionDetailsArgs struct {
	TargetRef string `json:"target_ref"`
}

type updatePendingBatchArgs struct {
	ItemRef            string  `json:"item_ref"`
	Amount             *string `json:"amount_idr"`
	Merchant           *string `json:"merchant"`
	CategorySlug       *string `json:"category_slug"`
	Description        *string `json:"description"`
	ConfirmAfterUpdate bool    `json:"confirm_after_update"`
}

type pendingBatchDecisionArgs struct {
	Action       string  `json:"action"`
	ItemRef      *string `json:"item_ref"`
	Amount       *string `json:"amount_idr"`
	Merchant     *string `json:"merchant"`
	CategorySlug *string `json:"category_slug"`
	Description  *string `json:"description"`
}

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

func containsAgentReviewAction(action string) bool {
	for _, allowed := range reviewActions() {
		if action == allowed {
			return true
		}
	}
	return false
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

func decodeAgentArgs[T any](call gateway.ToolCall, validate func(T) error) (map[string]any, error) {
	args, err := decodeAgentArgsRaw[T](call)
	if err != nil {
		return nil, err
	}
	if err := validate(args); err != nil {
		return nil, err
	}
	return remarshal(args), nil
}

func validatePeriodArgs(args periodArgs) error {
	valid := map[string]bool{"TODAY": true, "THIS_WEEK": true, "LAST_WEEK": true, "THIS_MONTH": true, "LAST_MONTH": true, "CURRENT_CYCLE": true, "PREVIOUS_CYCLE": true, "CUSTOM": true}
	if !valid[args.Period] {
		return fmt.Errorf("invalid period")
	}
	if args.Period == "CUSTOM" && (args.FromDate == nil || args.ToDate == nil || strings.TrimSpace(*args.FromDate) == "" || strings.TrimSpace(*args.ToDate) == "") {
		return fmt.Errorf("custom period requires dates")
	}
	return nil
}
