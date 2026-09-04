package bankemail

import (
	"strings"
	"time"
)

type ShadowBaseline struct {
	Amount        string
	Direction     string
	Channel       string
	TransactionAt time.Time
	Merchant      string
	Policy        string
}

// CompareShadow compares only observable facts and policy outcome. It never
// changes ledger state; the result is stored as rollout evidence.
func CompareShadow(extraction Extraction, policy PolicyResult, baseline ShadowBaseline) (map[string]bool, bool) {
	merchant := value(extraction.Merchant)
	fields := map[string]bool{
		"amount":         value(extraction.AmountIDR) == baseline.Amount,
		"direction":      value(extraction.Direction) == baseline.Direction,
		"channel":        value(extraction.Channel) == baseline.Channel,
		"transaction_at": extraction.TransactionAt != nil && extraction.TransactionAt.Equal(baseline.TransactionAt),
		"merchant":       strings.EqualFold(strings.TrimSpace(merchant), strings.TrimSpace(baseline.Merchant)),
		"policy_result":  policy.Type == baseline.Policy,
	}
	agreement := true
	for _, equal := range fields {
		agreement = agreement && equal
	}
	return fields, agreement
}
