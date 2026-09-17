package document

import (
	"fmt"
	"strings"
	"time"
)

// ValidationIssue is one structured, field-scoped validator problem. Codes are
// deterministic and stable; the model never decides their meaning.
type ValidationIssue struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

func (i ValidationIssue) String() string { return i.Field + ":" + i.Code }

type validationIssues []ValidationIssue

func (list validationIssues) has(field, code string) bool {
	for _, issue := range list {
		if issue.Field == field && issue.Code == code {
			return true
		}
	}
	return false
}

func (list validationIssues) String() string {
	parts := make([]string, 0, len(list))
	for _, issue := range list {
		parts = append(parts, issue.String())
	}
	return strings.Join(parts, ",")
}

// validateReceiptIssues re-derives the existing receipt rules as structured
// field-level issues instead of one package error. Codes are validator-owned.
func validateReceiptIssues(value receiptExtraction, receivedAt time.Time) (receiptValidation, validationIssues) {
	validation, legacyErr := validateReceipt(value, receivedAt)
	if legacyErr == nil {
		return validation, nil
	}
	issues := validationIssues{}
	if value.Currency != "IDR" {
		issues = append(issues, ValidationIssue{"currency", "INVALID_CURRENCY"})
	}
	if value.Confidence < 0 || value.Confidence > 1 {
		issues = append(issues, ValidationIssue{"confidence", "INVALID_CONFIDENCE"})
	}
	if value.CategoryConfidence < 0 || value.CategoryConfidence > 1 {
		issues = append(issues, ValidationIssue{"category_confidence", "INVALID_CONFIDENCE"})
	}
	if _, ok := wholeMoney(value.Total, true); !ok {
		issues = append(issues, ValidationIssue{"total", "INVALID_AMOUNT"})
	}
	if len([]rune(strings.TrimSpace(value.Merchant))) > 160 {
		issues = append(issues, ValidationIssue{"merchant", "TEXT_TOO_LONG"})
	}
	if len([]rune(strings.TrimSpace(value.PaymentMethodHint))) > 160 {
		issues = append(issues, ValidationIssue{"payment_method_hint", "TEXT_TOO_LONG"})
	}
	for index, item := range value.Items {
		field := fmt.Sprintf("items[%d]", index)
		if strings.TrimSpace(item.Name) == "" || len([]rune(item.Name)) > 300 {
			issues = append(issues, ValidationIssue{field + ".name", "INVALID_TEXT"})
		}
		if _, ok := wholeMoney(item.Amount, false); !ok {
			issues = append(issues, ValidationIssue{field + ".amount", "INVALID_AMOUNT"})
		}
	}
	for index, part := range []*string{value.Subtotal, value.Tax, value.ServiceCharge, value.Discount} {
		if part == nil {
			continue
		}
		if _, ok := wholeMoney(*part, false); !ok {
			field := "subtotal"
			switch index {
			case 1:
				field = "tax"
			case 2:
				field = "service_charge"
			case 3:
				field = "discount"
			}
			issues = append(issues, ValidationIssue{field, "INVALID_AMOUNT"})
		}
	}
	if value.TransactionAt != nil {
		if _, err := time.Parse(time.RFC3339, *value.TransactionAt); err != nil {
			issues = append(issues, ValidationIssue{"transaction_at", "INVALID_FORMAT"})
		} else if parsed, err := time.Parse(time.RFC3339, *value.TransactionAt); err == nil {
			t := parsed.In(jakarta())
			if t.Before(receivedAt.AddDate(-2, 0, 0)) || t.After(receivedAt.Add(24*time.Hour)) {
				issues = append(issues, ValidationIssue{"transaction_at", "IMPLAUSIBLE_TIME"})
			}
		}
	}
	if len(issues) == 0 {
		// Rule violation not yet mapped field-locally; keep the package-level
		// failure deterministic instead of guessing a field.
		issues = append(issues, ValidationIssue{"receipt", "UNMAPPED_VALIDATION_FAILURE"})
	}
	return validation, issues
}

func payslipValidationIssues(value payslipExtraction) validationIssues {
	_, _, err := validatePayslip(value)
	if err == nil {
		return nil
	}
	issues := validationIssues{}
	if value.Currency != "IDR" {
		issues = append(issues, ValidationIssue{"currency", "INVALID_CURRENCY"})
	}
	if value.Confidence < 0 || value.Confidence > 1 {
		issues = append(issues, ValidationIssue{"confidence", "INVALID_CONFIDENCE"})
	}
	if _, ok := wholeMoney(value.NetPay, true); !ok {
		issues = append(issues, ValidationIssue{"net_pay", "INVALID_AMOUNT"})
	}
	gross, grossOK := wholeMoney(value.GrossPay, true)
	net, netOK := wholeMoney(value.NetPay, true)
	if !grossOK || (grossOK && netOK && gross.Cmp(net) < 0) {
		issues = append(issues, ValidationIssue{"gross_pay", "INVALID_AMOUNT_OR_ORDER"})
	}
	if _, periodErr := parsePayslipPeriod(value.Period); periodErr != nil {
		issues = append(issues, ValidationIssue{"period", "INVALID_PERIOD"})
	}
	for i, line := range value.Allowances {
		if _, ok := wholeMoney(line.Amount, false); !ok {
			issues = append(issues, ValidationIssue{fmt.Sprintf("allowances[%d].amount", i), "INVALID_AMOUNT"})
		}
	}
	for i, line := range value.Deductions {
		if _, ok := wholeMoney(line.Amount, false); !ok {
			issues = append(issues, ValidationIssue{fmt.Sprintf("deductions[%d].amount", i), "INVALID_AMOUNT"})
		}
	}
	if len(issues) == 0 {
		issues = append(issues, ValidationIssue{"payslip", "UNMAPPED_VALIDATION_FAILURE"})
	}
	return issues
}

// screenshotValidationIssues maps screenshot rule failures to the top-level
// field the model may re-answer. Row-local problems stay on the rows field so a
// repair cannot silently restructure the row set.
func screenshotValidationIssues(value screenshotExtraction, documentType string) validationIssues {
	if value.Confidence < 0 || value.Confidence > 1 {
		return validationIssues{{Field: "confidence", Code: "INVALID_CONFIDENCE"}}
	}
	if len([]rune(value.AccountHint)) > 160 {
		return validationIssues{{Field: "account_hint", Code: "TEXT_TOO_LONG"}}
	}
	if documentType == "BILL_OR_INVOICE" && value.PaymentStatus != "PAID" {
		return validationIssues{{Field: "payment_status", Code: "PAYMENT_NOT_CONFIRMED"}}
	}
	if len(value.Transactions) == 0 || len(value.Transactions) > 50 {
		return validationIssues{{Field: "transactions", Code: "INVALID_ROW_COUNT"}}
	}
	return validationIssues{{Field: "transactions", Code: "UNMAPPED_VALIDATION_FAILURE"}}
}
