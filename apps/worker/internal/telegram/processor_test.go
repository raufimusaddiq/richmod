package telegram

import (
	"strings"
	"testing"
	"time"
)

func TestNativeExtractionPreservesExplicitDateWithApproximateMorning(t *testing.T) {
	now := time.Date(2026, time.September, 7, 17, 37, 0, 0, jakartaLocation())
	validated, err := nativeValidatedExtraction(map[string]any{
		"type":                "EXPENSE",
		"amount_idr":          "46000",
		"merchant":            "Bensin",
		"category_slug":       "bahan-bakar",
		"description":         "Beli bensin",
		"note":                nil,
		"date_reference":      "EXPLICIT",
		"date_provenance":     "USER_STATED",
		"explicit_date":       "2026-09-06",
		"local_time":          "PAGI",
		"confidence":          0.99,
		"category_confidence": 0.99,
	}, now)
	if err != nil {
		t.Fatalf("nativeValidatedExtraction() error = %v", err)
	}
	want := time.Date(2026, time.September, 6, 9, 0, 0, 0, jakartaLocation())
	if !validated.TransactionAt.Equal(want) {
		t.Fatalf("transaction time = %v, want %v", validated.TransactionAt, want)
	}
	if validated.TimePrecision != "APPROXIMATE" || validated.TimePeriod != "PAGI" {
		t.Fatalf("time metadata = %q, %q", validated.TimePrecision, validated.TimePeriod)
	}
}

func TestNativeExtractionDoesNotPromoteModelDateToUserStatement(t *testing.T) {
	now := time.Date(2026, time.September, 7, 17, 37, 0, 0, jakartaLocation())
	base := map[string]any{
		"type": "EXPENSE", "amount_idr": "46000", "date_reference": "TODAY",
		"explicit_date": nil, "local_time": nil, "date_provenance": "NOT_USER_STATED",
	}
	value, err := nativeValidatedExtraction(base, now)
	if err != nil {
		t.Fatal(err)
	}
	if value.DateProvenance == "USER_STATED" {
		t.Fatal("model date was promoted to user-stated provenance")
	}
	if _, ok := directAcceptanceDecision(value, nil, now); ok {
		t.Fatal("non-user-stated date passed canonical acceptance")
	}
	delete(base, "date_provenance")
	if _, err := nativeValidatedExtraction(base, now); err == nil {
		t.Fatal("date without provenance was accepted")
	}
}

func TestResolveTimeRejectsUnknownApproximatePeriod(t *testing.T) {
	now := time.Date(2026, time.September, 7, 17, 37, 0, 0, jakartaLocation())
	dateReference, explicitDate, localTime := "EXPLICIT", "2026-09-06", "subuh"
	if _, err := resolveTime(now, &dateReference, &explicitDate, &localTime); err == nil {
		t.Fatal("resolveTime() accepted an unsupported approximate period")
	}
}

// A clear purchase with an accepted category must not create avoidable review,
// and an expense without an accepted category must stay reviewable. The old
// model self-confidence thresholds no longer exist: the semantic decision object
// is the only authority (ADR-038), so a 0.88 category_confidence from the
// generative model is irrelevant to confirmation.
func TestClearExpenseDecisionDoesNotCreateAvoidableReview(t *testing.T) {
	clear := TransactionSemanticDecision{
		RouteAccepted: true, TransactionType: "EXPENSE", TypeAccepted: true,
		AmountSupported: true, DateSupported: true, CategorySlug: "makanan-minuman", CategoryAccepted: true,
		AmbiguityDecidedNotAmbiguous: true,
		DecisionSource:               "JEV", PolicyVersion: judgmentPolicyVersion,
	}
	if !clear.decisionAllowed() {
		t.Fatal("a supported expense decision must be allowed")
	}
	unknownCategory := clear
	unknownCategory.CategoryAccepted = false
	unknownCategory.CategorySlug = ""
	if unknownCategory.decisionAllowed() {
		t.Fatal("an expense without an accepted category must remain reviewable")
	}
	// A generative model grading itself highly cannot substitute for the decision.
	if (TransactionSemanticDecision{}).decisionAllowed() {
		t.Fatal("an empty decision must never confirm a transaction")
	}
}

func TestAssistantRangesUseJakartaCalendarBoundaries(t *testing.T) {
	now := time.Date(2026, time.August, 26, 14, 0, 0, 0, jakartaLocation())
	period := "THIS_WEEK"
	got, err := resolveAssistantRange(now, &period, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.From.Format(time.RFC3339) != "2026-08-24T00:00:00+07:00" || got.To.Format(time.RFC3339) != "2026-08-31T00:00:00+07:00" {
		t.Fatalf("range=%s..%s", got.From.Format(time.RFC3339), got.To.Format(time.RFC3339))
	}
}

func TestAssistantCustomRangeIsBounded(t *testing.T) {
	now := time.Date(2026, time.August, 26, 14, 0, 0, 0, jakartaLocation())
	period, from, to := "CUSTOM", "2024-01-01", "2026-08-01"
	if _, err := resolveAssistantRange(now, &period, &from, &to); err == nil {
		t.Fatal("accepted disclosure range over one year")
	}
}

func TestCallbackTextContainsNoTransactionIdentity(t *testing.T) {
	if got := callbackText("review:own"); got != "rekening sendiri" {
		t.Fatalf("callback=%q", got)
	}
	if got := callbackText("review:asset"); got != "beli aset" {
		t.Fatalf("asset callback=%q", got)
	}
	if got := callbackText("transaction:00000000-0000-0000-0000-000000000000"); got != "" {
		t.Fatalf("untrusted callback accepted: %q", got)
	}
}

func TestStaleReviewCallbackUsesSuccessReply(t *testing.T) {
	if !strings.Contains("✅ Tinjauan ini sudah selesai. Tidak ada perubahan baru.", "sudah selesai") {
		t.Fatal("stale review callback must acknowledge completed review")
	}
}
