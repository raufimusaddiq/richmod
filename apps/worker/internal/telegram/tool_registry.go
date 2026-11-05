package telegram

import (
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/analyticscore"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func NativeFinanceTools(categories []string, hasPendingAction, hasPendingBatch, hasActiveReview bool, reviewType string, hasSalaryChoice, hasMerchantLearning bool, _ string) []gateway.ToolDefinition {
	stringType := map[string]any{"type": "string"}
	nullString := map[string]any{"type": []string{"string", "null"}}
	localTime := map[string]any{"type": []string{"string", "null"}, "description": "Exact local HH:MM (24-hour), or PAGI, SIANG, SORE, MALAM when the user named a time of day. Null when no time was stated."}
	dateRef := map[string]any{"type": []string{"string", "null"}, "enum": []any{"TODAY", "YESTERDAY", "EXPLICIT", nil}}
	period := map[string]any{"type": "string", "enum": []string{"TODAY", "THIS_WEEK", "LAST_WEEK", "THIS_MONTH", "LAST_MONTH", "CURRENT_CYCLE", "PREVIOUS_CYCLE", "CUSTOM"}}
	// The household's category list is sent once, in turn_context
	// allowed_category_slugs, instead of as an enum in every schema that takes a
	// category: Go validates the slug anyway and answers INVALID_CATEGORY.
	category := categorySchema()
	_ = categories
	entry := objectSchema(map[string]any{"type": map[string]any{"type": "string", "enum": []string{"INCOME", "EXPENSE"}}, "amount_idr": stringType, "merchant": nullString, "category_slug": category, "description": nullString, "note": nullString, "date_reference": dateRef, "date_provenance": map[string]any{"type": []string{"string", "null"}, "enum": []any{"USER_STATED", "NOT_USER_STATED", nil}}, "explicit_date": nullString, "local_time": localTime, "ambiguous": map[string]any{"type": "boolean"}, "confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1}, "category_confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1}}, []string{"type", "amount_idr", "merchant", "category_slug", "description", "note", "date_reference", "date_provenance", "explicit_date", "local_time", "ambiguous", "confidence", "category_confidence"})
	periodProps := map[string]any{"period": period, "from_date": nullString, "to_date": nullString}
	tools := []gateway.ToolDefinition{
		{Name: "record_transaction", Description: "Record one observed household IDR income or expense intent. For a clearly named purchased item or service, select the best matching allowed category. Preserve explicit past dates; use PAGI, SIANG, SORE, or MALAM for a named time of day. Go validates and persists.", Parameters: entry},
		{Name: "record_transaction_batch", Description: "Stage 1-10 observed household IDR transactions for explicit confirmation.", Parameters: objectSchema(map[string]any{"items": map[string]any{"type": "array", "minItems": 1, "maxItems": 10, "items": entry}}, []string{"items"})},
		{Name: "query_spending", Description: "Ask Go for deterministic confirmed net expense for a bounded period, with its breakdown by household category (largest first, up to 20). Use it for totals and for what the money went to; never infer totals yourself.", Parameters: objectSchema(periodProps, []string{"period", "from_date", "to_date"})},
		{Name: "query_cashflow", Description: "Ask Go for deterministic confirmed income, expense and net cash flow for a bounded period, plus the open review count and the largest expense counterparty.", Parameters: objectSchema(periodProps, []string{"period", "from_date", "to_date"})},
		{Name: "record_transfer", Description: "Record an observed whole-IDR transfer between the household's own accounts, using human-readable account hints only. Supply amount, source, destination Wealth hint, and date/time; omit the destination hint for a plain transfer between two transaction accounts. Go resolves each hint uniquely inside this household, decides the canonical transfer purpose itself, reconciles existing canonical transactions, and never accepts or guesses database IDs.", Parameters: objectSchema(map[string]any{"amount_idr": stringType, "source_account_hint": stringType, "destination_wealth_account_hint": nullString, "date_reference": dateRef, "explicit_date": nullString, "local_time": nullString, "description": nullString}, []string{"amount_idr", "source_account_hint", "destination_wealth_account_hint", "date_reference", "explicit_date", "local_time", "description"})},
		{Name: "query_savings", Description: "Ask Go for deterministic confirmed savings totals, using canonical savings purposes only.", Parameters: objectSchema(periodProps, []string{"period", "from_date", "to_date"})},
		{Name: "query_wealth", Description: "Ask Go for net worth from the latest complete Wealth Snapshot and its observed timestamp.", Parameters: objectSchema(map[string]any{}, []string{})},
		{Name: "list_wealth_accounts", Description: "Ask Go for active household Wealth Accounts.", Parameters: objectSchema(map[string]any{}, []string{})},
		{Name: "search_transactions", Description: "Find household transactions in a bounded period, returned with opaque refs. search_text matches merchant, description or category; null lists all. order_by RECENT (default) is newest first over income/expense/refund; LARGEST is the biggest confirmed expenses/refunds first. limit 1-10.", Parameters: objectSchema(map[string]any{"period": period, "from_date": nullString, "to_date": nullString, "search_text": nullString, "order_by": map[string]any{"type": []string{"string", "null"}, "enum": []any{"RECENT", "LARGEST", nil}}, "limit": map[string]any{"type": []string{"integer", "null"}, "minimum": 1, "maximum": 10}}, []string{"period", "from_date", "to_date", "search_text", "order_by", "limit"})},
		{Name: "list_review_items", Description: "List active household review items relevant to this Telegram user/chat.", Parameters: objectSchema(map[string]any{}, []string{})},
		{Name: "get_evidence_context", Description: "Read the current state of one receipt or document Richmod already showed you, by its opaque evidence_ref. Go returns its observed, canonical and workflow blocks. It changes no financial or review state; it only refreshes the ref's expiry. Use it before answering about a document whose state may have changed.", Parameters: objectSchema(map[string]any{"evidence_ref": stringType}, []string{"evidence_ref"})},
		{Name: "ask_clarification", Description: "Ask for missing finance details without guessing.", Parameters: objectSchema(map[string]any{"topic": map[string]any{"type": "string", "enum": []string{"TRANSACTION", "PERIOD", "TARGET", "CATEGORY", "REVIEW"}}, "missing_fields": map[string]any{"type": "array", "items": stringType}}, []string{"topic", "missing_fields"})},
		{Name: "finance_help", Description: "Show Richmod finance command examples.", Parameters: objectSchema(map[string]any{}, []string{})},
		{Name: "finance_out_of_scope", Description: "Reject unsupported non-finance or out-of-MVP requests.", Parameters: objectSchema(map[string]any{"reason": map[string]any{"type": "string", "enum": []string{"NON_FINANCE", "INVESTMENT_ACTION_UNSUPPORTED", "SYSTEM_REQUEST", "UNSUPPORTED_LANGUAGE"}}}, []string{"reason"})},
		{Name: "propose_transaction_correction", Description: "Stage a correction of one recorded transaction for the user's button confirmation, using a target_ref from results or search criteria; Go resolves one target. Fields left null stay unchanged. amount_idr and merchant can change only a transaction the household entered itself (Telegram or web); for bank, provider or document-backed transactions Go answers SOURCE_EVIDENCE_FIXES_AMOUNT.", Parameters: objectSchema(map[string]any{"target_ref": nullString, "search_text": nullString, "period": period, "from_date": nullString, "to_date": nullString, "category_slug": category, "description": nullString, "amount_idr": nullString, "merchant": nullString, "date_reference": dateRef, "explicit_date": nullString, "local_time": localTime}, []string{"target_ref", "search_text", "period", "from_date", "to_date", "category_slug", "description", "amount_idr", "merchant", "date_reference", "explicit_date", "local_time"})},
	}
	if hasPendingAction {
		tools = append(tools, gateway.ToolDefinition{Name: "confirm_pending_action", Description: "Confirm the one active server-bound pending correction.", Parameters: objectSchema(map[string]any{}, []string{})}, gateway.ToolDefinition{Name: "cancel_pending_action", Description: "Cancel the one active server-bound pending correction.", Parameters: objectSchema(map[string]any{}, []string{})})
	}
	if hasActiveReview {
		tools = append(tools, gateway.ToolDefinition{Name: "resolve_review", Description: "Resolve one active server-bound finance review. Candidate references are opaque values supplied in active_review; never use database IDs. RECORD_ASSET_PURCHASE reclassifies mistaken Wealth balance extraction only when the user supplies the source account and transaction time.", Parameters: objectSchema(map[string]any{"action": map[string]any{"type": "string", "enum": reviewdec.AgentActions(reviewType)}, "candidate_ref": nullString, "source_account_hint": nullString, "wealth_account_hint": nullString, "category_slug": category, "merchant": nullString, "description": nullString, "pay_date": nullString, "amount_idr": nullString, "transaction_at": map[string]any{"type": []string{"string", "null"}, "description": "The transaction date the user gave for a review missing it, as YYYY-MM-DD. Null when the review does not need a date."}, "allocations": map[string]any{"type": "array", "items": objectSchema(map[string]any{"wealth_account_id": stringType, "amount_idr": stringType, "note": nullString}, []string{"wealth_account_id", "amount_idr", "note"})}}, []string{"action", "candidate_ref", "source_account_hint", "wealth_account_hint", "category_slug", "merchant", "description", "pay_date", "amount_idr", "transaction_at", "allocations"})})
	}
	if hasSalaryChoice {
		tools = append(tools, gateway.ToolDefinition{Name: "resolve_salary_choice", Description: "Resolve pending payslip classification.", Parameters: objectSchema(map[string]any{"choice": map[string]any{"type": "string", "enum": []string{"PRIMARY", "ORDINARY", "IGNORE"}}}, []string{"choice"})})
	}
	if hasMerchantLearning {
		tools = append(tools, gateway.ToolDefinition{Name: "resolve_merchant_learning", Description: "Confirm whether to remember this merchant category rule.", Parameters: objectSchema(map[string]any{"remember": map[string]any{"type": "boolean"}}, []string{"remember"})})
	}
	// Shared analytical READ tools over the same deterministic fact engine as the
	// analytics API. Read-only; channel-neutral (BDR-006).
	for _, tool := range analyticscore.Tools() {
		tools = append(tools, gateway.ToolDefinition{Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters})
	}
	return tools
}

// categorySchema is a nullable household category slug. The allowed values are
// listed once, in turn_context.allowed_category_slugs.
func categorySchema() map[string]any {
	return map[string]any{"type": []string{"string", "null"}, "description": "A slug from turn_context.allowed_category_slugs, or null when no listed category clearly fits."}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}

func ValidateNativeToolCall(call gateway.ToolCall) (map[string]any, error) {
	if analyticscore.IsRead(call.Name) {
		args, err := analyticscore.DecodeArgs(call.Name, call.Arguments)
		if err != nil {
			return nil, err
		}
		return remarshal(args), nil
	}
	var target any
	switch call.Name {
	case "record_transaction":
		target = &createArgs{}
	case "record_transaction_batch":
		target = &batchArgs{}
	case "query_spending", "query_cashflow", "query_savings":
		target = &periodArgs{}
	case "record_transfer":
		target = &transferArgs{}
	case "search_transactions":
		target = &searchArgs{}
	case "get_evidence_context":
		target = &evidenceArgs{}
	case "list_review_items", "query_wealth", "list_wealth_accounts", "finance_help", "confirm_pending_action", "cancel_pending_action":
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
	case *evidenceArgs:
		out, err := gateway.DecodeToolArguments[evidenceArgs](call, call.Name)
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
	DateProvenance     *string `json:"date_provenance"`
	ExplicitDate       *string `json:"explicit_date"`
	LocalTime          *string `json:"local_time"`
	Ambiguous          bool    `json:"ambiguous"`
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
	DateReference                string  `json:"date_reference"`
	ExplicitDate                 *string `json:"explicit_date"`
	LocalTime                    *string `json:"local_time"`
	Description                  *string `json:"description"`
}

// evidenceArgs carries only an opaque evidence ref. The model can never name a
// document by its database id: the argument is a lookup key Go must resolve.
type evidenceArgs struct {
	EvidenceRef string `json:"evidence_ref"`
}

type searchArgs struct {
	Period     string  `json:"period"`
	FromDate   *string `json:"from_date"`
	ToDate     *string `json:"to_date"`
	SearchText *string `json:"search_text"`
	OrderBy    *string `json:"order_by"`
	Limit      *int    `json:"limit"`
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
	Amount        *string `json:"amount_idr"`
	Merchant      *string `json:"merchant"`
	DateReference *string `json:"date_reference"`
	ExplicitDate  *string `json:"explicit_date"`
	LocalTime     *string `json:"local_time"`
}
type resolveReviewArgs struct {
	Action        string  `json:"action"`
	CandidateRef  *string `json:"candidate_ref"`
	SourceHint    *string `json:"source_account_hint"`
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

// validateCreate rejects a transaction proposal Go cannot interpret, so the
// rejection is an argument error the model can repair inside the turn (ADR-055)
// instead of a failed job. Every date/time rule nativeValidatedExtraction applies
// is checked here first.
func validateCreate(v createArgs) error {
	if v.Type != "INCOME" && v.Type != "EXPENSE" {
		return fmt.Errorf("type must be INCOME or EXPENSE")
	}
	n, ok := new(big.Int).SetString(v.Amount, 10)
	if !ok || n.Sign() <= 0 || n.String() != v.Amount {
		return fmt.Errorf("amount_idr must be a positive whole rupiah number without separators, e.g. 25000")
	}
	if v.DateReference != "" && v.DateReference != "TODAY" && v.DateReference != "YESTERDAY" && v.DateReference != "EXPLICIT" {
		return fmt.Errorf("date_reference must be TODAY, YESTERDAY, EXPLICIT or null")
	}
	if v.DateProvenance != nil && *v.DateProvenance != "USER_STATED" && *v.DateProvenance != "NOT_USER_STATED" {
		return fmt.Errorf("date_provenance must be USER_STATED, NOT_USER_STATED or null")
	}
	provenance := pointerValue(v.DateProvenance)
	if provenance == "USER_STATED" && v.DateReference == "" {
		return fmt.Errorf("date_provenance USER_STATED needs a date_reference")
	}
	if v.DateReference != "" && provenance == "" {
		return fmt.Errorf("date_provenance is required when date_reference is set: USER_STATED if the user said the date, otherwise NOT_USER_STATED")
	}
	if err := validateDateFields(v.DateReference, v.ExplicitDate, v.LocalTime); err != nil {
		return err
	}
	if v.Confidence < 0 || v.Confidence > 1 || v.CategoryConfidence < 0 || v.CategoryConfidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	return nil
}

// validateDateFields checks the shared date arguments of record, correction and
// transfer tools exactly as resolveTransactionTime will read them.
func validateDateFields(dateReference string, explicitDate, localTime *string) error {
	if dateReference == "EXPLICIT" {
		if explicitDate == nil || strings.TrimSpace(*explicitDate) == "" {
			return fmt.Errorf("explicit_date is required for date_reference EXPLICIT, as YYYY-MM-DD")
		}
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(*explicitDate)); err != nil {
			return fmt.Errorf("explicit_date must be YYYY-MM-DD, got %q", *explicitDate)
		}
	}
	if localTime != nil && strings.TrimSpace(*localTime) != "" {
		if _, _, ok := parseLocalTime(*localTime); !ok {
			return fmt.Errorf("local_time must be HH:MM (24-hour) or PAGI, SIANG, SORE, MALAM, got %q", *localTime)
		}
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
		// Purpose is deliberately absent: the generative model does not get to
		// assert it. A missing destination hint is fine here because
		// INTERNAL_TRANSFER legitimately has none.
		if !ok || n.Sign() <= 0 || n.String() != v.Amount || strings.TrimSpace(v.SourceAccountHint) == "" || (v.DateReference != "TODAY" && v.DateReference != "YESTERDAY" && v.DateReference != "EXPLICIT") {
			return fmt.Errorf("transfer")
		}
		if err := validateDateFields(v.DateReference, v.ExplicitDate, v.LocalTime); err != nil {
			return err
		}
	case *evidenceArgs:
		if len(v.EvidenceRef) > 64 || !evidenceRefPattern.MatchString(v.EvidenceRef) {
			return fmt.Errorf("evidence reference")
		}
	case *searchArgs:
		if !validPeriod(v.Period) {
			return fmt.Errorf("period")
		}
		order := pointerValue(v.OrderBy)
		if order != "" && order != "RECENT" && order != "LARGEST" {
			return fmt.Errorf("order_by must be RECENT, LARGEST or null")
		}
		if order != "LARGEST" && strings.TrimSpace(pointerValue(v.SearchText)) == "" {
			return fmt.Errorf("search_text is required unless order_by is LARGEST")
		}
		if v.Limit != nil && (*v.Limit < 1 || *v.Limit > 10) {
			return fmt.Errorf("limit must be 1-10")
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
		if v.TargetRef == nil && strings.TrimSpace(pointerValue(v.SearchText)) == "" {
			return fmt.Errorf("target_ref or search_text is required")
		}
		if !validPeriod(v.Period) {
			return fmt.Errorf("period")
		}
		if v.CategorySlug == nil && v.Description == nil && v.DateReference == nil && v.Amount == nil && v.Merchant == nil {
			return fmt.Errorf("name at least one change: category_slug, description, amount_idr, merchant or a date")
		}
		if v.Amount != nil {
			n, ok := new(big.Int).SetString(*v.Amount, 10)
			if !ok || n.Sign() <= 0 || n.String() != *v.Amount {
				return fmt.Errorf("amount_idr must be a positive whole rupiah number without separators")
			}
		}
		if v.DateReference != nil && *v.DateReference != "" {
			if *v.DateReference != "TODAY" && *v.DateReference != "YESTERDAY" && *v.DateReference != "EXPLICIT" {
				return fmt.Errorf("date_reference must be TODAY, YESTERDAY, EXPLICIT or null")
			}
			if err := validateDateFields(*v.DateReference, v.ExplicitDate, v.LocalTime); err != nil {
				return err
			}
		}
	case *resolveReviewArgs:
		if !slices.Contains(reviewdec.AllAgentActions(), v.Action) {
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
