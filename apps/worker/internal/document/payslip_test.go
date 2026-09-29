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
	gross := "17500000"
	value := payslipExtraction{Period: "2026-08", Employer: "Example", GrossPay: &gross, Deductions: []moneyLine{{Name: "Tax", Amount: "1500000"}}, NetPay: "16000000", Currency: "IDR", PayDate: &date, Confidence: .97}
	transactionAt, arithmeticOK, err := validatePayslip(value)
	if err != nil {
		t.Fatal(err)
	}
	if !arithmeticOK || transactionAt.Location().String() != "Asia/Jakarta" || transactionAt.Day() != 25 {
		t.Fatalf("unexpected validation: %v %v", transactionAt, arithmeticOK)
	}
}

func TestValidatePayslipRejectsFractionalOrNonIDR(t *testing.T) {
	gross := "18500000"
	value := payslipExtraction{Period: "2026-08", GrossPay: &gross, NetPay: "16000000.50", Currency: "IDR", Confidence: .9}
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

// SAVR-03B: real payroll forms carry a display period, may omit gross pay, and
// may contain component lines the net/gross/deduction formula cannot explain.
// Those are representable without fabricating a gross or a deduction.
func TestPayslipPeriodRequiresCanonicalMonth(t *testing.T) {
	period, err := parsePayslipPeriod("2026-09")
	if err != nil || period.Format("2006-01") != "2026-09" {
		t.Fatalf("canonical month rejected: %v %v", period, err)
	}
	if _, err := parsePayslipPeriod("September 2026 (01/09/26 - 30/09/26)"); err == nil {
		t.Fatal("Go must not parse localized payroll periods")
	}
	if _, err := parsePayslipPeriod("2026-13"); err == nil {
		t.Fatal("an impossible month must be rejected")
	}
	date := "2026-09-25"
	missingGross := payslipExtraction{Period: "2026-09", Employer: "Example", NetPay: "16000000", OtherComponents: []moneyLine{{Name: "Potongan lain", Amount: "-250000"}}, Currency: "IDR", PayDate: &date, Confidence: .96}
	transactionAt, arithmeticOK, err := validatePayslip(missingGross)
	if err != nil || transactionAt.IsZero() || transactionAt.Month() != 9 {
		t.Fatalf("a real payroll range must survive validation: %v %v", transactionAt, err)
	}
	if arithmeticOK {
		t.Fatal("an unproven breakdown is a quality signal, not a proven reconciliation")
	}
	gross := "15500000"
	missingGross.GrossPay, missingGross.OtherComponents = &gross, nil
	if _, arithmeticOK, err := validatePayslip(missingGross); err != nil || arithmeticOK {
		t.Fatalf("a printed net exceeding a labeled gross is a quality signal, not an invalid net: arithmetic=%t err=%v", arithmeticOK, err)
	}
	if _, _, err := validatePayslip(payslipExtraction{Period: "2026-09", Employer: "Example", NetPay: "16000000", OtherComponents: []moneyLine{{Name: "Potongan lain", Amount: "250.000"}}, Currency: "IDR", Confidence: .96}); err == nil {
		t.Fatal("a component amount that is not whole rupiah must be rejected")
	}
}

func TestPayslipPayDateRequiresCanonicalISO(t *testing.T) {
	for _, date := range []string{"2026-09-28", "gajian tanggal dua puluh delapan september", "salary was paid last Friday"} {
		value := payslipExtraction{Period: "2026-09", Employer: "Example", NetPay: "1", Currency: "IDR", Confidence: .2, PayDate: &date}
		_, _, err := validatePayslip(value)
		if date == "2026-09-28" && err != nil {
			t.Fatalf("canonical date rejected: %v", err)
		}
		if date != "2026-09-28" && err == nil {
			t.Fatalf("raw-language date accepted: %q", date)
		}
	}
}

func TestPayslipReviewSeparatesDateAndSalaryPolicy(t *testing.T) {
	dateOnly, _ := reviewdec.Preset("MISSING_PAY_DATE", "proposal", "id")
	dateOnly = configurePayslipReviewDecision(dateOnly, "MISSING_PAY_DATE", true)
	if !reflect.DeepEqual(dateOnly.MissingFacts, []string{"transaction_at"}) {
		t.Fatalf("primary-known payslip missing facts=%v", dateOnly.MissingFacts)
	}
	if !reflect.DeepEqual(dateOnly.AllowedActions, []string{"SET_PAY_DATE", "IGNORE"}) {
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
	if !reflect.DeepEqual(firstSalaryAndDate.AllowedActions, []string{"SET_PAY_DATE", "PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}) {
		t.Fatalf("first-salary policy choices=%v", firstSalaryAndDate.AllowedActions)
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
