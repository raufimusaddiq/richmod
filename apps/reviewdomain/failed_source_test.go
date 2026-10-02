package reviewdomain

import (
	"strings"
	"testing"
)

func TestFailedSourceCopyIsPlainAndSaysWhatToDo(t *testing.T) {
	for _, sourceType := range []string{"TELEGRAM_TEXT", "TELEGRAM_CALLBACK", "BANK_EMAIL", "SOMETHING_ELSE"} {
		for _, reason := range []string{"INVALID", "TRANSPORT_FAILED", "TIMEOUT", "ERROR", ""} {
			title, description := FailedSourceCopy(sourceType, reason)
			if strings.TrimSpace(title) == "" || strings.TrimSpace(description) == "" {
				t.Fatalf("%s/%s needs a title and description", sourceType, reason)
			}
			if !strings.Contains(description, "tutup tindakan ini") {
				t.Errorf("%s/%s must tell the household to close the action: %q", sourceType, reason, description)
			}
			for _, jargon := range []string{"source_event", "SOURCE_", "FAILED", "payload", "job", "LLM", "household"} {
				if strings.Contains(title+description, jargon) {
					t.Errorf("%s/%s leaks %q into user-facing copy: %q", sourceType, reason, jargon, title+" "+description)
				}
			}
		}
	}
}

func TestFailedBankEmailCopyDistinguishesUnreadableFromProviderFailure(t *testing.T) {
	_, unreadable := FailedSourceCopy("BANK_EMAIL", "INVALID")
	_, provider := FailedSourceCopy("BANK_EMAIL", "TRANSPORT_FAILED")
	if unreadable == provider {
		t.Fatal("an unreadable email and a provider outage need different guidance")
	}
	if !strings.Contains(unreadable, "belum ada transaksi yang dicatat") || !strings.Contains(provider, "belum ada transaksi yang dicatat") {
		t.Fatal("both must say plainly that no transaction was recorded")
	}
}

func TestSourceEventIDValidation(t *testing.T) {
	for _, id := range []string{"3e647399-1c2d-4a5b-8c9d-0e1f2a3b4c5d", "3E647399-1C2D-4A5B-8C9D-0E1F2A3B4C5D"} {
		if !IsSourceEventID(id) {
			t.Errorf("%q must be accepted", id)
		}
	}
	for _, id := range []string{"", "not-a-uuid", "3e647399", "3e647399-1c2d-4a5b-8c9d-0e1f2a3b4c5d; DROP TABLE job", " 3e647399-1c2d-4a5b-8c9d-0e1f2a3b4c5d"} {
		if IsSourceEventID(id) {
			t.Errorf("%q must be rejected", id)
		}
	}
}
