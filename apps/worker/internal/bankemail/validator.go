package bankemail

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

var idrInteger = regexp.MustCompile(`^[0-9]+$`)
var missingNames = map[string]bool{"amount_idr": true, "transaction_at": true, "merchant": true, "counterparty": true, "reference": true, "description": true, "direction": true, "channel": true}

func ValidateEmitBankTransaction(call gateway.ToolCall) (Extraction, error) {
	if call.Name != "emit_bank_transaction" {
		return Extraction{}, fmt.Errorf("unexpected bank email tool")
	}
	var raw struct {
		Kind          string            `json:"kind"`
		Direction     *string           `json:"direction"`
		Channel       *string           `json:"channel"`
		AmountIDR     *string           `json:"amount_idr"`
		TransactionAt *string           `json:"transaction_at"`
		Merchant      *string           `json:"merchant"`
		Counterparty  *string           `json:"counterparty"`
		Reference     *string           `json:"reference"`
		Description   *string           `json:"description"`
		MissingFields []string          `json:"missing_fields"`
		Confidence    float64           `json:"confidence"`
		Review        *ReviewSuggestion `json:"review"`
	}
	dec := json.NewDecoder(bytes.NewReader(call.Arguments))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Extraction{}, fmt.Errorf("invalid emit_bank_transaction arguments: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Extraction{}, fmt.Errorf("trailing tool arguments")
		}
		return Extraction{}, fmt.Errorf("invalid trailing tool arguments")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &fields); err != nil {
		return Extraction{}, fmt.Errorf("invalid emit_bank_transaction object")
	}
	for _, name := range []string{"kind", "direction", "channel", "amount_idr", "transaction_at", "merchant", "counterparty", "reference", "description", "missing_fields", "confidence"} {
		value, ok := fields[name]
		if !ok || string(value) == "null" && name != "direction" && name != "channel" && name != "amount_idr" && name != "transaction_at" && name != "merchant" && name != "counterparty" && name != "reference" && name != "description" {
			return Extraction{}, fmt.Errorf("missing required field %s", name)
		}
	}
	if raw.Kind != "TRANSACTION" && raw.Kind != "NON_TRANSACTION" && raw.Kind != "UNKNOWN" {
		return Extraction{}, fmt.Errorf("invalid bank email kind")
	}
	if raw.Direction != nil && !oneOf(*raw.Direction, "OUTGOING", "INCOMING", "INTERNAL", "UNKNOWN") {
		return Extraction{}, fmt.Errorf("invalid bank email direction")
	}
	if raw.Channel != nil && !oneOf(*raw.Channel, "DEBIT_CARD", "MERCHANT_PAYMENT", "QR", "TRANSFER", "ATM", "BANK_FEE", "INTERNAL_TRANSFER", "RDN", "OTHER", "UNKNOWN") {
		return Extraction{}, fmt.Errorf("invalid bank email channel")
	}
	if raw.AmountIDR != nil {
		if !idrInteger.MatchString(*raw.AmountIDR) {
			return Extraction{}, fmt.Errorf("amount must be a whole IDR integer")
		}
		amount, ok := new(big.Int).SetString(*raw.AmountIDR, 10)
		if !ok || amount.Sign() <= 0 || len(*raw.AmountIDR) > 20 {
			return Extraction{}, fmt.Errorf("amount must be a positive IDR integer")
		}
	}
	var at *time.Time
	if raw.TransactionAt != nil {
		parsed, err := parseStrictBankRFC3339(*raw.TransactionAt)
		if err != nil {
			return Extraction{}, fmt.Errorf("invalid transaction time")
		}
		at = &parsed
	}
	for name, value := range map[string]*string{"merchant": raw.Merchant, "counterparty": raw.Counterparty, "reference": raw.Reference, "description": raw.Description} {
		if value != nil && len([]rune(*value)) > 500 {
			return Extraction{}, fmt.Errorf("%s is too long", name)
		}
	}
	if len(raw.MissingFields) > 8 {
		return Extraction{}, fmt.Errorf("too many missing fields")
	}
	seen := map[string]bool{}
	for _, name := range raw.MissingFields {
		if !missingNames[name] || seen[name] {
			return Extraction{}, fmt.Errorf("invalid missing field")
		}
		seen[name] = true
	}
	if raw.Confidence < 0 || raw.Confidence > 1 {
		return Extraction{}, fmt.Errorf("invalid confidence")
	}
	if raw.Review != nil {
		if len([]rune(strings.TrimSpace(raw.Review.Summary))) == 0 || len([]rune(raw.Review.Summary)) > 500 {
			return Extraction{}, fmt.Errorf("invalid review summary")
		}
		seen = map[string]bool{}
		for _, name := range raw.Review.MissingFields {
			if !missingNames[name] || seen[name] {
				return Extraction{}, fmt.Errorf("invalid review missing field")
			}
			seen[name] = true
		}
		seen = map[string]bool{}
		for _, action := range raw.Review.SuggestedActions {
			if !oneOf(action, "USE_EMAIL_RECEIVED_AT", "ENTER_TRANSACTION_TIME", "IGNORE") || seen[action] {
				return Extraction{}, fmt.Errorf("invalid review suggested action")
			}
			seen[action] = true
		}
	}
	return Extraction{Kind: raw.Kind, Direction: raw.Direction, Channel: raw.Channel, AmountIDR: raw.AmountIDR, TransactionAt: at, Merchant: raw.Merchant, Counterparty: raw.Counterparty, Reference: raw.Reference, Description: raw.Description, MissingFields: raw.MissingFields, Confidence: raw.Confidence, Review: raw.Review}, nil
}

func parseStrictBankRFC3339(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("bank transaction_at must be RFC3339 with explicit timezone")
	}
	return parsed, nil
}

func oneOf(value string, values ...string) bool {
	for _, item := range values {
		if value == item {
			return true
		}
	}
	return false
}
