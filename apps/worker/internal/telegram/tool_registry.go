package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// NativeFinanceTools is the only tool catalog exposed to the conversational
// model. Implementations are intentionally dispatched by Go, never by the
// model or the gateway.
func NativeFinanceTools() []gateway.ToolDefinition {
	stringType := map[string]any{"type": "string"}
	return []gateway.ToolDefinition{
		{Name: "query_transactions", Description: "Read household ledger transactions for a bounded period.", Parameters: objectSchema(map[string]any{"mode": map[string]any{"type": "string", "enum": []string{"spending", "cashflow", "search", "reviews"}}, "period": stringType, "search_text": map[string]any{"type": []string{"string", "null"}}}, []string{"mode", "period", "search_text"})},
		{Name: "create_transaction", Description: "Create one validated IDR income or expense proposal.", Parameters: objectSchema(map[string]any{"type": map[string]any{"type": "string", "enum": []string{"INCOME", "EXPENSE"}}, "amount_idr": stringType, "merchant": map[string]any{"type": []string{"string", "null"}}, "transaction_at": stringType}, []string{"type", "amount_idr", "merchant", "transaction_at"})},
		{Name: "create_transaction_batch", Description: "Stage multiple validated IDR income or expense entries for one combined confirmation.", Parameters: objectSchema(map[string]any{"items": map[string]any{"type": "array", "minItems": 1, "maxItems": 10, "items": objectSchema(map[string]any{"type": map[string]any{"type": "string", "enum": []string{"INCOME", "EXPENSE"}}, "amount_idr": stringType, "merchant": map[string]any{"type": []string{"string", "null"}}, "transaction_at": stringType}, []string{"type", "amount_idr", "merchant", "transaction_at"})}}, []string{"items"})},
		{Name: "propose_transaction_edit", Description: "Propose an edit to one exact existing transaction; never mutate it.", Parameters: objectSchema(map[string]any{"transaction_id": stringType, "transaction_at": map[string]any{"type": []string{"string", "null"}}, "description": map[string]any{"type": []string{"string", "null"}}}, []string{"transaction_id", "transaction_at", "description"})},
		{Name: "confirm_edit", Description: "Confirm a previously proposed transaction edit.", Parameters: objectSchema(map[string]any{"pending_action_id": stringType}, []string{"pending_action_id"})},
		{Name: "cancel_edit", Description: "Cancel a previously proposed transaction edit.", Parameters: objectSchema(map[string]any{"pending_action_id": stringType}, []string{"pending_action_id"})},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}

func ValidateNativeToolCall(call gateway.ToolCall) (map[string]any, error) {
	allowed := map[string]bool{"query_transactions": true, "create_transaction": true, "create_transaction_batch": true, "propose_transaction_edit": true, "confirm_edit": true, "cancel_edit": true}
	if !allowed[call.Name] {
		return nil, fmt.Errorf("unregistered finance tool %q", call.Name)
	}
	var target any
	switch call.Name {
	case "create_transaction":
		target = &createArgs{}
	case "create_transaction_batch":
		target = &batchArgs{}
	case "query_transactions":
		target = &queryArgs{}
	case "propose_transaction_edit":
		target = &editArgs{}
	case "confirm_edit", "cancel_edit":
		target = &pendingArgs{}
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("invalid arguments for %s", call.Name)
	}
	if err := validateTypedArgs(target); err != nil {
		return nil, fmt.Errorf("invalid arguments for %s: %w", call.Name, err)
	}
	encoded, _ := json.Marshal(target)
	var args map[string]any
	_ = json.Unmarshal(encoded, &args)
	return args, nil
}

type createArgs struct {
	Type          string  `json:"type"`
	Amount        string  `json:"amount_idr"`
	Merchant      *string `json:"merchant"`
	TransactionAt string  `json:"transaction_at"`
}
type batchArgs struct {
	Items []createArgs `json:"items"`
}
type queryArgs struct {
	Mode       string  `json:"mode"`
	Period     string  `json:"period"`
	SearchText *string `json:"search_text"`
}
type editArgs struct {
	TransactionID string  `json:"transaction_id"`
	TransactionAt *string `json:"transaction_at"`
	Description   *string `json:"description"`
}
type pendingArgs struct {
	PendingActionID string `json:"pending_action_id"`
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func validateCreate(v createArgs) error {
	if v.Type != "INCOME" && v.Type != "EXPENSE" {
		return fmt.Errorf("type")
	}
	n, ok := new(big.Int).SetString(v.Amount, 10)
	if !ok || n.Sign() <= 0 || n.String() != v.Amount {
		return fmt.Errorf("amount")
	}
	if _, err := time.Parse(time.RFC3339, v.TransactionAt); err != nil {
		return fmt.Errorf("timestamp")
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
	case *queryArgs:
		if !map[string]bool{"spending": true, "cashflow": true, "search": true, "reviews": true}[v.Mode] || strings.TrimSpace(v.Period) == "" {
			return fmt.Errorf("query")
		}
	case *editArgs:
		if !uuidPattern.MatchString(v.TransactionID) || (v.TransactionAt == nil && v.Description == nil) {
			return fmt.Errorf("edit")
		}
		if v.TransactionAt != nil {
			if _, err := time.Parse(time.RFC3339, *v.TransactionAt); err != nil {
				return err
			}
		}
	case *pendingArgs:
		if !uuidPattern.MatchString(v.PendingActionID) {
			return fmt.Errorf("pending id")
		}
	default:
		return fmt.Errorf("unsupported")
	}
	return nil
}
