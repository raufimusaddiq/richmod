package reviewdomain

import (
	"os"
	"strings"
	"testing"
)

// The worker autonomous path and the household-resolved path must mint
// canonical salary state through one operation, and a duplicate period/employer
// salary may only link when amount and pay date also agree.
func TestPayslipPathsShareFinalizer(t *testing.T) {
	for _, path := range []string{"../worker/internal/document/payslip.go", "payslip.go"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if body := string(source); strings.Contains(body, "INSERT INTO salary_event") || strings.Contains(body, "INSERT INTO salary_source") {
			t.Fatalf("%s still owns salary mutation SQL", path)
		}
	}
	finalizer, err := os.ReadFile("payslip.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"func FinalizePayslip(", "RecordSalaryEvent(ctx, tx", "sameFacts"} {
		if !strings.Contains(string(finalizer), want) {
			t.Fatalf("finalizer missing %q", want)
		}
	}
	worker, err := os.ReadFile("../worker/internal/document/payslip.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(worker), "reviewdomain.FinalizePayslip(") {
		t.Fatal("worker autonomous payslip must finalize through the shared salary finalizer")
	}
}
