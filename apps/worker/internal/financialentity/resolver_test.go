package financialentity

import "testing"

func TestNormalizeAndGenericTokenMatch(t *testing.T) {
	if got := Normalize(" JÁGO—Autodebit "); got != "jágo autodebit" {
		t.Fatalf("Normalize() = %q", got)
	}
	if got := matchScore("Jago Autodebit", "Bank Jago"); got == 0 {
		t.Fatal("generic shared token must be a candidate")
	}
	if got := matchScore("Jago", "Bank BCA"); got != 0 {
		t.Fatalf("unrelated match = %d", got)
	}
}
