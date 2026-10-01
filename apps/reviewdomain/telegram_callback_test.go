package reviewdomain

import "testing"

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
