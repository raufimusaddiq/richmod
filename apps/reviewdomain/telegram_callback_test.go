package reviewdomain

import "testing"

func TestPendingConfirmationCallbackRevisionGrammar(t *testing.T) {
	for _, prefix := range []string{"pending:action:yes", "pending:action:no", "pending:batch:yes", "pending:batch:no"} {
		if !ValidTelegramCallback(prefix) {
			t.Errorf("legacy callback must reach the worker's stale-action response: %s", prefix)
		}
		for _, test := range []struct {
			token string
			valid bool
		}{
			{"8a97e069-0278-4f49-9195-fbbfe81fdfd5", true},
			{"", false}, {"1", false}, {"../../admin", false},
			{"8a97e069-0278-4f49-9195-fbbfe81fdfdg", false},
		} {
			data := prefix + ":" + test.token
			if got := ValidTelegramCallback(data); got != test.valid {
				t.Errorf("callback %q accepted=%v, want %v", data, got, test.valid)
			}
		}
	}
}

func TestEverySampleIsValidAndUnsafeCallbacksAreRejected(t *testing.T) {
	for _, sample := range TelegramCallbackSamples() {
		if !ValidTelegramCallback(sample) {
			t.Errorf("sample %q was rejected", sample)
		}
	}
	for _, data := range []string{
		"", "review:", "pending:", "review:cat:", "review:cat:../../admin", "review:catpage:-1", "review:catpage:99999",
		"review:fepage:", "review:fepage:1x", "review:dup:merge:", "review:dup:merge:-1", "review:dup:merge:one",
		"review:fe:account:", "review:fe:wealth:a/b", "review:invest:", "review:bank:id with space",
		"review:salary:", "review:salary:other", "review:quality:", "review:dup:", "review:dup:other",
		"pending:action", "pending:action:maybe", "pending:batch:yes:1", "admin:delete", "REVIEW:IGNORE",
	} {
		if ValidTelegramCallback(data) {
			t.Errorf("unsafe callback %q was accepted", data)
		}
	}
	long := "review:cat:" + string(make([]byte, 0))
	for len(long) <= 64 {
		long += "a"
	}
	if ValidTelegramCallback(long) {
		t.Errorf("callback over Telegram's 64-byte limit was accepted: %d bytes", len(long))
	}
}
