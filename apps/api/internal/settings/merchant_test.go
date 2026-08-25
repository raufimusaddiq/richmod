package settings

import "testing"

func TestAutoApplyRequiresExplicitConfirmation(t *testing.T) {
	if aliasPolicy(true, false) {
		t.Fatal("unconfirmed alias allowed auto-apply")
	}
	if !aliasPolicy(true, true) {
		t.Fatal("confirmed alias denied auto-apply")
	}
	if !aliasPolicy(false, false) {
		t.Fatal("non-auto alias should not require confirmation")
	}
}
