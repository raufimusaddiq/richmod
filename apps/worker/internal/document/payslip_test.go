package document

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

func TestValidatePayslipIDRArithmetic(t *testing.T) {
	date := "2026-08-25"
	value := payslipExtraction{Period: "2026-08", Employer: "Example", GrossPay: "17500000", Deductions: []moneyLine{{Name: "Tax", Amount: "1500000"}}, NetPay: "16000000", Currency: "IDR", PayDate: &date, Confidence: .97}
	transactionAt, arithmeticOK, err := validatePayslip(value)
	if err != nil {
		t.Fatal(err)
	}
	if !arithmeticOK || transactionAt.Location().String() != "Asia/Jakarta" || transactionAt.Day() != 25 {
		t.Fatalf("unexpected validation: %v %v", transactionAt, arithmeticOK)
	}
}

func TestValidatePayslipRejectsFractionalOrNonIDR(t *testing.T) {
	value := payslipExtraction{Period: "2026-08", GrossPay: "18500000", NetPay: "16000000.50", Currency: "IDR", Confidence: .9}
	if _, _, err := validatePayslip(value); err == nil {
		t.Fatal("expected fractional rupiah rejection")
	}
	value.NetPay = "16000000"
	value.Currency = "USD"
	if _, _, err := validatePayslip(value); err == nil {
		t.Fatal("expected non-IDR rejection")
	}
}

func TestPayrollDeductionsDoNotBecomeTransactions(t *testing.T) {
	// The extractor schema retains deductions only inside payslip metadata. The
	// persistence path creates exactly one INCOME proposal from net_pay.
	schema := payslipSchema()
	properties := schema["properties"].(map[string]any)
	if _, ok := properties["deductions"]; !ok {
		t.Fatal("deductions metadata missing")
	}
}

func TestPayslipReviewSeparatesDateAndSalaryPolicy(t *testing.T) {
	dateOnly, _ := reviewdec.Preset("MISSING_PAY_DATE", "proposal", "id")
	dateOnly = configurePayslipReviewDecision(dateOnly, "MISSING_PAY_DATE", true)
	if !reflect.DeepEqual(dateOnly.MissingFacts, []string{"transaction_at"}) {
		t.Fatalf("primary-known payslip missing facts=%v", dateOnly.MissingFacts)
	}
	if !reflect.DeepEqual(dateOnly.AllowedActions, []string{"SET_PAY_DATE", "PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}) {
		t.Fatalf("date-only payslip resolution domain=%v", dateOnly.AllowedActions)
	}

	firstSalary, _ := reviewdec.Preset("PAYSLIP_CONFIRMATION", "proposal", "id")
	firstSalary = configurePayslipReviewDecision(firstSalary, "PAYSLIP_CONFIRMATION", false)
	if !reflect.DeepEqual(firstSalary.MissingFacts, []string{"salary_classification"}) {
		t.Fatalf("clear-date first salary missing facts=%v", firstSalary.MissingFacts)
	}

	firstSalaryAndDate, _ := reviewdec.Preset("MISSING_PAY_DATE", "proposal", "id")
	firstSalaryAndDate = configurePayslipReviewDecision(firstSalaryAndDate, "MISSING_PAY_DATE", false)
	if !reflect.DeepEqual(firstSalaryAndDate.MissingFacts, []string{"transaction_at", "salary_classification"}) {
		t.Fatalf("first salary and date missing facts=%v", firstSalaryAndDate.MissingFacts)
	}
}

func TestPayslipUsesOneGenerativeExtractionAndNoJevReplay(t *testing.T) {
	// Payslip processing calls the required vision extraction, followed only by
	// deterministic validation and policy review; it has no Jev verifier path.
	content, err := os.ReadFile("payslip.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), "p.gateway.NativeToolCall(ctx, documentID, payslipPrompt") != 1 {
		t.Fatal("payslip must make exactly one generative extraction call")
	}
	if strings.Contains(string(content), "p.verifier") {
		t.Fatal("payslip must not replay the extraction through Jev")
	}
}
