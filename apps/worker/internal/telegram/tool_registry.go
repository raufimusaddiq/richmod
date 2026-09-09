package telegram

import (
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func NativeFinanceTools(categories []string, hasPendingAction, hasPendingBatch, hasActiveReview bool, reviewType string, hasSalaryChoice, hasMerchantLearning bool, reviewMode string) []gateway.ToolDefinition {
	if reviewMode == "" {
		reviewMode = reviewType
	}
	stringType := map[string]any{"type": "string"}
	nullString := map[string]any{"type": []string{"string", "null"}}
	localTime := map[string]any{"type": []string{"string", "null"}, "description": "Exact local HH:MM, or PAGI, SIANG, SORE, MALAM when the user named a time of day."}
	dateRef := map[string]any{"type": "string", "enum": []string{"TODAY", "YESTERDAY", "EXPLICIT"}}
	period := map[string]any{"type": "string", "enum": []string{"TODAY", "THIS_WEEK", "LAST_WEEK", "THIS_MONTH", "LAST_MONTH", "CURRENT_CYCLE", "PREVIOUS_CYCLE", "CUSTOM"}}
	category := map[string]any{"type": []string{"string", "null"}}
	if len(categories) > 0 {
		values := make([]any, 0, len(categories)+2)
		values = append(values, nil, "")
		for _, slug := range categories {
			values = append(values, slug)
		}
		category["enum"] = values
	}
	entry := objectSchema(map[string]any{"type": map[string]any{"type": "string", "enum": []string{"INCOME", "EXPENSE"}}, "amount_idr": stringType, "merchant": nullString, "category_slug": category, "description": nullString, "note": nullString, "date_reference": dateRef, "explicit_date": nullString, "local_time": localTime, "confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1}, "category_confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1}}, []string{"type", "amount_idr", "merchant", "category_slug", "description", "note", "date_reference", "explicit_date", "local_time", "confidence", "category_confidence"})
	periodProps := map[string]any{"period": period, "from_date": nullString, "to_date": nullString}
	tools := []gateway.ToolDefinition{
		{Name: "record_transaction", Description: "Record one observed household IDR income or expense intent. For a clearly named purchased item or service, select the best matching allowed category. Preserve explicit past dates; use PAGI, SIANG, SORE, or MALAM for a named time of day. Go validates and persists.", Parameters: entry},
		{Name: "record_transaction_batch", Description: "Stage 1-10 observed household IDR transactions for explicit confirmation.", Parameters: objectSchema(map[string]any{"items": map[string]any{"type": "array", "minItems": 1, "maxItems": 10, "items": entry}}, []string{"items"})},
		{Name: "query_spending", Description: "Ask Go for deterministic expense totals for a bounded period.", Parameters: objectSchema(periodProps, []string{"period", "from_date", "to_date"})},
		{Name: "query_cashflow", Description: "Ask Go for deterministic cash-flow totals for a bounded period.", Parameters: objectSchema(periodProps, []string{"period", "from_date", "to_date"})},
		{Name: "record_transfer", Description: "Record a confirmed whole-IDR transfer using human-readable account hints only. Go resolves each hint uniquely inside this household, reconciles existing canonical transactions, and never accepts or guesses database IDs. destination_wealth_account_hint must be null for INTERNAL_TRANSFER and is required otherwise.", Parameters: objectSchema(map[string]any{"amount_idr": stringType, "source_account_hint": stringType, "destination_wealth_account_hint": nullString, "purpose": map[string]any{"type": "string", "enum": []string{"SAVINGS_TRANSFER", "INVESTMENT_CONTRIBUTION", "ASSET_PURCHASE", "DEBT_PRINCIPAL_PAYMENT", "INTERNAL_TRANSFER"}}, "date_reference": dateRef, "explicit_date": nullString, "local_time": nullString, "description": nullString}, []string{"amount_idr", "source_account_hint", "destination_wealth_account_hint", "purpose", "date_reference", "explicit_date", "local_time", "description"})},
		{Name: "query_savings", Description: "Ask Go for deterministic confirmed savings totals, using canonical savings purposes only.", Parameters: objectSchema(periodProps, []string{"period", "from_date", "to_date"})},
		{Name: "query_wealth", Description: "Ask Go for net worth from the latest complete Wealth Snapshot and its observed timestamp.", Parameters: objectSchema(map[string]any{}, []string{})},
		{Name: "list_wealth_accounts", Description: "Ask Go for active household Wealth Accounts.", Parameters: objectSchema(map[string]any{}, []string{})},
		{Name: "search_transactions", Description: "Search household transactions by text and period. Go returns bounded results.", Parameters: objectSchema(map[string]any{"period": period, "from_date": nullString, "to_date": nullString, "search_text": stringType}, []string{"period", "from_date", "to_date", "search_text"})},
		{Name: "list_review_items", Description: "List active household review items relevant to this Telegram user/chat.", Parameters: objectSchema(map[string]any{}, []string{})},
		{Name: "get_finance_insight", Description: "Read or start canonical aggregate-only insight for a bounded period.", Parameters: objectSchema(periodProps, []string{"period", "from_date", "to_date"})},
		{Name: "ask_clarification", Description: "Ask for missing finance details without guessing.", Parameters: objectSchema(map[string]any{"topic": map[string]any{"type": "string", "enum": []string{"TRANSACTION", "PERIOD", "TARGET", "CATEGORY", "REVIEW"}}, "missing_fields": map[string]any{"type": "array", "items": stringType}}, []string{"topic", "missing_fields"})},
		{Name: "finance_help", Description: "Show Richmod finance command examples.", Parameters: objectSchema(map[string]any{}, []string{})},
		{Name: "finance_out_of_scope", Description: "Reject unsupported non-finance or out-of-MVP requests.", Parameters: objectSchema(map[string]any{"reason": map[string]any{"type": "string", "enum": []string{"NON_FINANCE", "INVESTMENT_ACTION_UNSUPPORTED", "SYSTEM_REQUEST", "UNSUPPORTED_LANGUAGE"}}}, []string{"reason"})},
		{Name: "propose_transaction_correction", Description: "Propose a correction using a private target_ref from search results or search criteria; Go resolves one target.", Parameters: objectSchema(map[string]any{"target_ref": nullString, "search_text": nullString, "period": period, "from_date": nullString, "to_date": nullString, "category_slug": category, "description": nullString, "date_reference": nullString, "explicit_date": nullString, "local_time": localTime}, []string{"target_ref", "search_text", "period", "from_date", "to_date", "category_slug", "description", "date_reference", "explicit_date", "local_time"})},
	}
	if hasPendingAction {
		tools = append(tools, gateway.ToolDefinition{Name: "confirm_pending_action", Description: "Confirm the one active server-bound pending correction.", Parameters: objectSchema(map[string]any{}, []string{})}, gateway.ToolDefinition{Name: "cancel_pending_action", Description: "Cancel the one active server-bound pending correction.", Parameters: objectSchema(map[string]any{}, []string{})})
	}
	if hasPendingBatch {
		tools = append(tools, gateway.ToolDefinition{Name: "confirm_pending_batch", Description: "Confirm the one active server-bound transaction batch.", Parameters: objectSchema(map[string]any{}, []string{})}, gateway.ToolDefinition{Name: "cancel_pending_batch", Description: "Cancel the one active server-bound transaction batch.", Parameters: objectSchema(map[string]any{}, []string{})})
	}
	if hasActiveReview {
		tools = append(tools, gateway.ToolDefinition{Name: "resolve_review", Description: "Resolve one active server-bound finance review. Candidate references are opaque values supplied in active_review; never use database IDs.", Parameters: objectSchema(map[string]any{"action": map[string]any{"type": "string", "enum": reviewActionsForType(reviewMode)}, "candidate_ref": nullString, "wealth_account_hint": nullString, "category_slug": category, "merchant": nullString, "description": nullString, "pay_date": nullString, "amount_idr": nullString, "transaction_at": nullString, "allocations": map[string]any{"type": "array", "items": objectSchema(map[string]any{"wealth_account_id": stringType, "amount_idr": stringType, "note": nullString}, []string{"wealth_account_id", "amount_idr", "note"})}}, []string{"action", "candidate_ref", "wealth_account_hint", "category_slug", "merchant", "description", "pay_date", "amount_idr", "transaction_at", "allocations"})})
	}
	if hasSalaryChoice {
		tools = append(tools, gateway.ToolDefinition{Name: "resolve_salary_choice", Description: "Resolve pending payslip classification.", Parameters: objectSchema(map[string]any{"choice": map[string]any{"type": "string", "enum": []string{"PRIMARY", "ORDINARY", "IGNORE"}}}, []string{"choice"})})
	}
	if hasMerchantLearning {
		tools = append(tools, gateway.ToolDefinition{Name: "resolve_merchant_learning", Description: "Confirm whether to remember this merchant category rule.", Parameters: objectSchema(map[string]any{"remember": map[string]any{"type": "boolean"}}, []string{"remember"})})
	}
	return tools
}

func reviewActions() []string {
	return []string{"CONFIRM", "IGNORE", "EXPENSE", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER", "INVESTMENT_TRANSFER", "ASSET_PURCHASE", "PRIMARY_SALARY", "ORDINARY_INCOME", "SET_PAY_DATE", "COMPLETE_BANK_FACTS", "ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED", "TRANSACTION_MISSING", "MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "PREPARE_SNAPSHOT", "SET_WEALTH_ACCOUNT"}
}

func reviewActionsForType(kind string) []string {
	switch kind {
	case "TRANSFER_RECONCILIATION":
		return []string{"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"}
	case "WEALTH_OBSERVATION":
		return []string{"PREPARE_SNAPSHOT", "SET_WEALTH_ACCOUNT", "IGNORE"}
	case "TRANSFER_CLASSIFICATION":
		return []string{"EXPENSE", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER", "INVESTMENT_TRANSFER", "ASSET_PURCHASE", "IGNORE"}
	case "CYCLE_RESIDUAL_ALLOCATION":
		return []string{"ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED", "TRANSACTION_MISSING"}
	case "PAYSLIP_CONFIRMATION", "SALARY_SOURCE_CONFIRMATION":
		return []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}
	case "MISSING_PAY_DATE":
		return []string{"SET_PAY_DATE", "IGNORE"}
	case "UNKNOWN_BANK_TEMPLATE", "DOCUMENT_EXTRACTION_LOW_CONFIDENCE":
		return []string{"COMPLETE_BANK_FACTS", "IGNORE"}
	default:
		return []string{"CONFIRM", "IGNORE"}
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}

func ValidateNativeToolCall(call gateway.ToolCall) (map[string]any, error) {
	var target any
	switch call.Name {
	case "record_transaction":
		target = &createArgs{}
	case "record_transaction_batch":
		target = &batchArgs{}
	case "query_spending", "query_cashflow", "query_savings", "get_finance_insight":
		target = &periodArgs{}
	case "record_transfer":
		target = &transferArgs{}
	case "search_transactions":
		target = &searchArgs{}
	case "list_review_items", "query_wealth", "list_wealth_accounts", "finance_help", "confirm_pending_action", "cancel_pending_action", "confirm_pending_batch", "cancel_pending_batch":
		target = &emptyArgs{}
	case "ask_clarification":
		target = &clarifyArgs{}
	case "finance_out_of_scope":
		target = &outOfScopeArgs{}
	case "propose_transaction_correction":
		target = &correctionArgs{}
	case "resolve_review":
		target = &resolveReviewArgs{}
	case "resolve_salary_choice":
		target = &salaryChoiceArgs{}
	case "resolve_merchant_learning":
		target = &merchantLearningArgs{}
	default:
		return nil, fmt.Errorf("unregistered finance tool %q", call.Name)
	}
	decoded, err := decodeNativeArgs(call, target)
	if err != nil {
		return nil, err
	}
	return decoded, validateTypedArgs(target)
}

func decodeNativeArgs(call gateway.ToolCall, target any) (map[string]any, error) {
	encoded, err := json.Marshal(call.Arguments)
	_ = encoded
	if err != nil {
		return nil, err
	}
	switch v := target.(type) {
	case *createArgs:
		out, err := gateway.DecodeToolArguments[createArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *batchArgs:
		out, err := gateway.DecodeToolArguments[batchArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *periodArgs:
		out, err := gateway.DecodeToolArguments[periodArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *transferArgs:
		out, err := gateway.DecodeToolArguments[transferArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *searchArgs:
		out, err := gateway.DecodeToolArguments[searchArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *emptyArgs:
		out, err := gateway.DecodeToolArguments[emptyArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *clarifyArgs:
		out, err := gateway.DecodeToolArguments[clarifyArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *outOfScopeArgs:
		out, err := gateway.DecodeToolArguments[outOfScopeArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *correctionArgs:
		out, err := gateway.DecodeToolArguments[correctionArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *resolveReviewArgs:
		out, err := gateway.DecodeToolArguments[resolveReviewArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *salaryChoiceArgs:
		out, err := gateway.DecodeToolArguments[salaryChoiceArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	case *merchantLearningArgs:
		out, err := gateway.DecodeToolArguments[merchantLearningArgs](call, call.Name)
		*v = out
		return remarshal(out), err
	}
	return nil, fmt.Errorf("unsupported arguments")
}

func remarshal(value any) map[string]any {
	encoded, _ := json.Marshal(value)
	var out map[string]any
	_ = json.Unmarshal(encoded, &out)
	return out
}

type emptyArgs struct{}
type createArgs struct {
	Type               string  `json:"type"`
	Amount             string  `json:"amount_idr"`
	Merchant           *string `json:"merchant"`
	CategorySlug       *string `json:"category_slug"`
	Description        *string `json:"description"`
	Note               *string `json:"note"`
	DateReference      string  `json:"date_reference"`
	ExplicitDate       *string `json:"explicit_date"`
	LocalTime          *string `json:"local_time"`
	Confidence         float64 `json:"confidence"`
	CategoryConfidence float64 `json:"category_confidence"`
}
type batchArgs struct {
	Items []createArgs `json:"items"`
}
type periodArgs struct {
	Period   string  `json:"period"`
	FromDate *string `json:"from_date"`
	ToDate   *string `json:"to_date"`
}
type transferArgs struct {
	Amount                       string  `json:"amount_idr"`
	SourceAccountHint            string  `json:"source_account_hint"`
	DestinationWealthAccountHint *string `json:"destination_wealth_account_hint"`
	Purpose                      string  `json:"purpose"`
	DateReference                string  `json:"date_reference"`
	ExplicitDate                 *string `json:"explicit_date"`
	LocalTime                    *string `json:"local_time"`
	Description                  *string `json:"description"`
}
type searchArgs struct {
	Period     string  `json:"period"`
	FromDate   *string `json:"from_date"`
	ToDate     *string `json:"to_date"`
	SearchText string  `json:"search_text"`
}
type clarifyArgs struct {
	Topic         string   `json:"topic"`
	MissingFields []string `json:"missing_fields"`
}
type outOfScopeArgs struct {
	Reason string `json:"reason"`
}
type correctionArgs struct {
	TargetRef     *string `json:"target_ref"`
	SearchText    *string `json:"search_text"`
	Period        string  `json:"period"`
	FromDate      *string `json:"from_date"`
	ToDate        *string `json:"to_date"`
	CategorySlug  *string `json:"category_slug"`
	Description   *string `json:"description"`
	DateReference *string `json:"date_reference"`
	ExplicitDate  *string `json:"explicit_date"`
	LocalTime     *string `json:"local_time"`
}
type resolveReviewArgs struct {
	Action        string  `json:"action"`
	CandidateRef  *string `json:"candidate_ref"`
	WealthHint    *string `json:"wealth_account_hint"`
	CategorySlug  *string `json:"category_slug"`
	Merchant      *string `json:"merchant"`
	Description   *string `json:"description"`
	PayDate       *string `json:"pay_date"`
	AmountIDR     *string `json:"amount_idr"`
	TransactionAt *string `json:"transaction_at"`
	Allocations   []struct {
		WealthAccountID string  `json:"wealth_account_id"`
		AmountIDR       string  `json:"amount_idr"`
		Note            *string `json:"note"`
	} `json:"allocations"`
}
type salaryChoiceArgs struct {
	Choice string `json:"choice"`
}
type merchantLearningArgs struct {
	Remember bool `json:"remember"`
}

func validateCreate(v createArgs) error {
	if v.Type != "INCOME" && v.Type != "EXPENSE" {
		return fmt.Errorf("type")
	}
	n, ok := new(big.Int).SetString(v.Amount, 10)
	if !ok || n.Sign() <= 0 || n.String() != v.Amount {
		return fmt.Errorf("amount")
	}
	if v.DateReference != "TODAY" && v.DateReference != "YESTERDAY" && v.DateReference != "EXPLICIT" {
		return fmt.Errorf("date_reference")
	}
	if v.Confidence < 0 || v.Confidence > 1 || v.CategoryConfidence < 0 || v.CategoryConfidence > 1 {
		return fmt.Errorf("confidence")
	}
	return nil
}

func validateTypedArgs(value any) error {
	switch v := value.(type) {
	case *createArgs:
		return validateCreate(*v)
	case *batchArgs:
		if len(v.Items) < 1 || len(v.Items) > 10 {
			return fmt.Errorf("batch size")
		}
		for _, item := range v.Items {
			if err := validateCreate(item); err != nil {
				return err
			}
		}
	case *periodArgs:
		if !validPeriod(v.Period) {
			return fmt.Errorf("period")
		}
	case *transferArgs:
		n, ok := new(big.Int).SetString(v.Amount, 10)
		validPurpose := map[string]bool{"SAVINGS_TRANSFER": true, "INVESTMENT_CONTRIBUTION": true, "ASSET_PURCHASE": true, "DEBT_PRINCIPAL_PAYMENT": true, "INTERNAL_TRANSFER": true}[v.Purpose]
		destination := ""
		if v.DestinationWealthAccountHint != nil {
			destination = strings.TrimSpace(*v.DestinationWealthAccountHint)
		}
		if !ok || n.Sign() <= 0 || n.String() != v.Amount || strings.TrimSpace(v.SourceAccountHint) == "" || !validPurpose || (v.Purpose == "INTERNAL_TRANSFER" && destination != "") || (v.Purpose != "INTERNAL_TRANSFER" && destination == "") || (v.DateReference != "TODAY" && v.DateReference != "YESTERDAY" && v.DateReference != "EXPLICIT") {
			return fmt.Errorf("transfer")
		}
	case *searchArgs:
		if !validPeriod(v.Period) || strings.TrimSpace(v.SearchText) == "" {
			return fmt.Errorf("search")
		}
	case *clarifyArgs:
		if strings.TrimSpace(v.Topic) == "" {
			return fmt.Errorf("clarification")
		}
	case *outOfScopeArgs:
		if strings.TrimSpace(v.Reason) == "" {
			return fmt.Errorf("reason")
		}
	case *correctionArgs:
		if (v.TargetRef == nil && strings.TrimSpace(pointerValue(v.SearchText)) == "") || !validPeriod(v.Period) || (v.CategorySlug == nil && v.Description == nil && v.DateReference == nil) {
			return fmt.Errorf("correction")
		}
	case *resolveReviewArgs:
		if !slices.Contains(reviewActions(), v.Action) {
			return fmt.Errorf("review action")
		}
	case *salaryChoiceArgs:
		if v.Choice != "PRIMARY" && v.Choice != "ORDINARY" && v.Choice != "IGNORE" {
			return fmt.Errorf("salary choice")
		}
	}
	return nil
}

func validPeriod(value string) bool {
	return map[string]bool{"TODAY": true, "THIS_WEEK": true, "LAST_WEEK": true, "THIS_MONTH": true, "LAST_MONTH": true, "CURRENT_CYCLE": true, "PREVIOUS_CYCLE": true, "CUSTOM": true}[value]
}
