package document

import (
	"fmt"
	"math/big"
	"strings"
	"time"
)

type ValidationIssue struct {
	Field  string `json:"field"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

func (i ValidationIssue) String() string {
	if i.Detail != "" {
		return i.Field + ":" + i.Code + " (" + i.Detail + ")"
	}
	return i.Field + ":" + i.Code
}

type validationIssues []ValidationIssue

func (list *validationIssues) append(issue ValidationIssue) {
	*list = append(*list, issue)
}

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

func validateReceiptIssues(value receiptExtraction, receivedAt time.Time) (receiptValidation, validationIssues) {
	issues := validationIssues{}
	if value.Currency != "IDR" {
		issues = append(issues, ValidationIssue{Field: "currency", Code: "INVALID_CURRENCY"})
	}
	if !(value.Confidence >= 0 && value.Confidence <= 1) {
		issues = append(issues, ValidationIssue{Field: "confidence", Code: "INVALID_CONFIDENCE"})
	}
	if !(value.CategoryConfidence >= 0 && value.CategoryConfidence <= 1) {
		issues = append(issues, ValidationIssue{Field: "category_confidence", Code: "INVALID_CONFIDENCE"})
	}
	total, totalValid := wholeMoney(value.Total, true)
	if !totalValid {
		issues = append(issues, ValidationIssue{Field: "total", Code: "INVALID_AMOUNT"})
	}
	if len([]rune(strings.TrimSpace(value.Merchant))) > 160 {
		issues = append(issues, ValidationIssue{Field: "merchant", Code: "TEXT_TOO_LONG"})
	}
	if len([]rune(strings.TrimSpace(value.PaymentMethodHint))) > 160 {
		issues = append(issues, ValidationIssue{Field: "payment_method_hint", Code: "TEXT_TOO_LONG"})
	}
	for index, item := range value.Items {
		field := fmt.Sprintf("items[%d]", index)
		if strings.TrimSpace(item.Name) == "" || len([]rune(item.Name)) > 300 {
			issues = append(issues, ValidationIssue{Field: field + ".name", Code: "INVALID_TEXT"})
		}
		if _, ok := wholeMoney(item.Amount, false); !ok {
			issues = append(issues, ValidationIssue{Field: field + ".amount", Code: "INVALID_AMOUNT"})
		}
	}
	parts := []struct {
		field string
		value *string
	}{
		{field: "subtotal", value: value.Subtotal},
		{field: "tax", value: value.Tax},
		{field: "service_charge", value: value.ServiceCharge},
		{field: "discount", value: value.Discount},
	}
	parsed := make([]*big.Int, len(parts))
	partsValid := true
	for index, part := range parts {
		if part.value == nil {
			parsed[index] = big.NewInt(0)
			continue
		}
		amount, ok := wholeMoney(*part.value, false)
		if !ok {
			partsValid = false
			issues = append(issues, ValidationIssue{Field: part.field, Code: "INVALID_AMOUNT"})
			continue
		}
		parsed[index] = amount
	}
	arithmeticAvailable := value.Subtotal != nil
	arithmeticOK := false
	if arithmeticAvailable && totalValid && partsValid {
		expected := new(big.Int).Add(new(big.Int).Set(parsed[0]), parsed[1])
		expected.Add(expected, parsed[2])
		expected.Sub(expected, parsed[3])
		arithmeticOK = expected.Sign() >= 0 && expected.Cmp(total) == 0
	}
	transactionAt := receivedAt.In(jakarta())
	dateKnown := value.TransactionAt != nil
	if dateKnown {
		parsedTime, err := time.Parse(time.RFC3339, *value.TransactionAt)
		if err != nil {
			issues = append(issues, ValidationIssue{Field: "transaction_at", Code: "INVALID_FORMAT"})
		} else {
			transactionAt = parsedTime.In(jakarta())
			if transactionAt.Before(receivedAt.AddDate(-2, 0, 0)) || transactionAt.After(receivedAt.Add(24*time.Hour)) {
				issues = append(issues, ValidationIssue{Field: "transaction_at", Code: "IMPLAUSIBLE_TIME"})
			}
		}
	}
	if len(issues) > 0 {
		return receiptValidation{}, issues
	}
	return receiptValidation{
		TransactionAt:       transactionAt,
		DateKnown:           dateKnown,
		ArithmeticAvailable: arithmeticAvailable,
		ArithmeticOK:        arithmeticOK,
	}, nil
}

func payslipValidationIssues(value payslipExtraction) validationIssues {
	_, _, _, err := validatePayslip(value)
	if err == nil {
		return nil
	}
	issues := validationIssues{}
	if value.Currency != "IDR" {
		issues = append(issues, ValidationIssue{Field: "currency", Code: "INVALID_CURRENCY"})
	}
	if value.Confidence < 0 || value.Confidence > 1 {
		issues = append(issues, ValidationIssue{Field: "confidence", Code: "INVALID_CONFIDENCE"})
	}
	if _, ok := wholeMoney(value.NetPay, true); !ok {
		issues = append(issues, ValidationIssue{Field: "net_pay", Code: "INVALID_AMOUNT"})
	}
	if value.GrossPay != nil {
		if _, ok := wholeMoney(*value.GrossPay, true); !ok {
			issues = append(issues, ValidationIssue{Field: "gross_pay", Code: "INVALID_AMOUNT"})
		}
	}
	if _, periodErr := parsePayslipPeriod(value.Period); periodErr != nil {
		issues = append(issues, ValidationIssue{Field: "period", Code: "INVALID_PERIOD"})
	}
	for i, line := range value.Allowances {
		if _, ok := wholeMoney(line.Amount, false); !ok {
			issues = append(issues, ValidationIssue{Field: fmt.Sprintf("allowances[%d].amount", i), Code: "INVALID_AMOUNT"})
		}
	}
	for i, line := range value.Deductions {
		if _, ok := wholeMoney(line.Amount, false); !ok {
			issues = append(issues, ValidationIssue{Field: fmt.Sprintf("deductions[%d].amount", i), Code: "INVALID_AMOUNT"})
		}
	}
	for i, line := range value.OtherComponents {
		if !validPayrollComponent(line.Amount) {
			issues = append(issues, ValidationIssue{Field: fmt.Sprintf("other_components[%d].amount", i), Code: "INVALID_AMOUNT"})
		}
	}
	if len(issues) == 0 {
		issues = append(issues, ValidationIssue{Field: "payslip", Code: "UNMAPPED_VALIDATION_FAILURE"})
	}
	return issues
}

func screenshotValidationIssues(value screenshotExtraction, documentType string) validationIssues {
	issues := validationIssues{}
	if value.Confidence < 0 || value.Confidence > 1 {
		issues = append(issues, ValidationIssue{Field: "confidence", Code: "INVALID_CONFIDENCE"})
	}
	if len([]rune(value.AccountHint)) > 160 {
		issues = append(issues, ValidationIssue{Field: "account_hint", Code: "TEXT_TOO_LONG"})
	}
	if documentType == "BILL_OR_INVOICE" && value.PaymentStatus != "PAID" {
		issues = append(issues, ValidationIssue{Field: "payment_status", Code: "PAYMENT_NOT_CONFIRMED"})
	}
	if len(value.Transactions) == 0 || len(value.Transactions) > 50 {
		issues = append(issues, ValidationIssue{Field: "transactions", Code: "INVALID_ROW_COUNT"})
	} else {
		for i, row := range value.Transactions {
			prefix := fmt.Sprintf("transactions[%d]", i)
			if row.Currency != "IDR" || (row.Direction != "OUT" && row.Direction != "IN") {
				issues = append(issues, ValidationIssue{Field: prefix, Code: "INVALID_DIRECTION_OR_CURRENCY"})
			}
			if row.Amount != nil {
				if _, ok := wholeMoney(*row.Amount, true); !ok {
					issues = append(issues, ValidationIssue{Field: prefix + ".amount", Code: "INVALID_AMOUNT"})
				}
			}
			if !validScreenshotIdentity(row) {
				issues = append(issues, ValidationIssue{Field: prefix + ".transaction_identifiers", Code: "INVALID_TRANSACTION_IDENTITY", Detail: screenshotIdentityIssue(row)})
			}
			if row.Confidence < 0 || row.Confidence > 1 {
				issues = append(issues, ValidationIssue{Field: prefix + ".confidence", Code: "INVALID_CONFIDENCE"})
			}
			if row.CategoryConfidence < 0 || row.CategoryConfidence > 1 {
				issues = append(issues, ValidationIssue{Field: prefix + ".category_confidence", Code: "INVALID_CONFIDENCE"})
			}
			if len([]rune(strings.TrimSpace(row.Merchant))) > 160 || len([]rune(strings.TrimSpace(row.Description))) > 500 {
				issues = append(issues, ValidationIssue{Field: prefix, Code: "TEXT_TOO_LONG"})
			}
		}
	}
	if len(issues) == 0 {
		if _, err := validateScreenshot(value, time.Now(), nil, documentType); err != nil {
			return validationIssues{{Field: "transactions", Code: "UNMAPPED_VALIDATION_FAILURE"}}
		}
	}
	return issues
}
