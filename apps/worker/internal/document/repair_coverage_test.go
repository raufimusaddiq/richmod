package document

import (
	"testing"
	"time"
)

// Every structured issue the three validators emit must map to a repairable
// top-level field, or the repair helper fails closed to Review.
func TestEveryValidationIssueMapsToRepairableField(t *testing.T) {
	received := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	_ = received
	receiptIssues := []string{
		"currency", "confidence", "category_confidence", "total", "merchant",
		"payment_method_hint", "items[0].name", "items[0].amount",
		"subtotal", "tax", "service_charge", "discount", "transaction_at",
		"receipt",
	}
	for _, field := range receiptIssues {
		for _, code := range []string{"INVALID_CURRENCY", "INVALID_CONFIDENCE", "INVALID_AMOUNT", "TEXT_TOO_LONG", "INVALID_TEXT", "INVALID_FORMAT", "IMPLAUSIBLE_TIME", "INVALID_AMOUNT_OR_ORDER", "UNMAPPED_VALIDATION_FAILURE"} {
			issue := ValidationIssue{Field: field, Code: code}
			mapped := repairableTopLevelField("RECEIPT", issue.Field)
			if field == "receipt" {
				if mapped != "" {
					t.Fatalf("%s should not map to a repairable field", issue)
				}
				continue
			}
			if mapped == "" {
				t.Errorf("%s does not map to a repairable receipt field", issue)
			}
		}
	}
	payslipIssues := []string{"currency", "confidence", "net_pay", "gross_pay", "period", "allowances[0].amount", "deductions[0].amount", "payslip"}
	for _, field := range payslipIssues {
		for _, code := range []string{"INVALID_CURRENCY", "INVALID_CONFIDENCE", "INVALID_AMOUNT", "INVALID_AMOUNT_OR_ORDER", "INVALID_PERIOD", "UNMAPPED_VALIDATION_FAILURE"} {
			issue := ValidationIssue{Field: field, Code: code}
			mapped := repairableTopLevelField("PAYSLIP", issue.Field)
			if field == "payslip" {
				if mapped != "" {
					t.Fatalf("%s should not map to a repairable field", issue)
				}
				continue
			}
			if mapped == "" {
				t.Errorf("%s does not map to a repairable payslip field", issue)
			}
		}
	}
	screenshotIssues := []string{"confidence", "account_hint", "payment_status", "transactions", "transactions[0]", "transactions[0].amount", "transactions[0].confidence", "transactions[0].category_confidence"}
	for _, docType := range []string{"BANK_TRANSACTION_SCREENSHOT", "TRANSFER_PROOF", "EWALLET_SCREENSHOT", "BILL_OR_INVOICE", "TRANSACTION_HISTORY_SCREENSHOT"} {
		for _, field := range screenshotIssues {
			for _, code := range []string{"INVALID_CONFIDENCE", "TEXT_TOO_LONG", "PAYMENT_NOT_CONFIRMED", "INVALID_ROW_COUNT", "INVALID_DIRECTION_OR_CURRENCY", "INVALID_AMOUNT", "UNMAPPED_VALIDATION_FAILURE"} {
				issue := ValidationIssue{Field: field, Code: code}
				mapped := repairableTopLevelField(docType, issue.Field)
				if mapped == "" {
					t.Errorf("%s does not map to a repairable screenshot field for %s", issue, docType)
				}
			}
		}
	}
}
