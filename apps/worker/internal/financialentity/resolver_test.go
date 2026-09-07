package financialentity

import "testing"

func TestNormalizeAndGenericTokenMatch(t *testing.T) {
	if got := Normalize(" JÁGO—Autodebit "); got != "jágo autodebit" {
		t.Fatalf("Normalize() = %q", got)
	}
	if !plausible("Jago Autodebit", "Bank Jago") {
		t.Fatal("generic shared token must be a candidate")
	}
	if plausible("Jago", "Bank BCA") {
		t.Fatal("unrelated entity matched")
	}
}
