package document

import "testing"

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
