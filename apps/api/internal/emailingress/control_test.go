package emailingress

import (
	"net/url"
	"strings"
	"testing"
)

func TestGmailForwardingConfirmationDetection(t *testing.T) {
	parsed := ParsedEmail{
		Sender:   "forwarding-noreply@google.com",
		Subject:  "(Gmail Forwarding Confirmation - Receive Mail from owner@example.com",
		HTMLBody: `<a href="https://mail-settings.google.com/mail/vf-abc123">Confirm</a><p>Confirmation code: 123456789</p>`,
	}
	action, ok := detectControlAction(parsed, [32]byte{1})
	if !ok || action.ActionType != "VERIFY_FORWARDING" || action.ActionURL == "" || action.ActionCode != "123456789" {
		t.Fatalf("action = %#v, recognized = %v", action, ok)
	}
}

func TestGmailForwardingConfirmationRequiresSignal(t *testing.T) {
	parsed := ParsedEmail{Sender: "alerts@google.com", Subject: "Gmail Forwarding Confirmation - Receive Mail from owner@example.com", TextBody: "https://mail-settings.google.com/mail/vf-abc"}
	if _, ok := detectControlAction(parsed, [32]byte{}); ok {
		t.Fatal("unrelated Google sender recognized as setup action")
	}
}

func TestAllowedGmailVerificationURL(t *testing.T) {
	valid := []string{"https://mail-settings.google.com/mail/vf-abc", "https://mail.google.com/mail/vf-abc"}
	for _, raw := range valid {
		parsed, _ := url.Parse(raw)
		if !isAllowedGmailVerificationURL(*parsed) {
			t.Fatalf("valid URL rejected: %s", raw)
		}
	}
	invalid := []string{"http://mail-settings.google.com/mail/vf-abc", "javascript:alert(1)", "https://attacker.example/mail/vf-abc", "https://google.com.attacker.example/mail/vf-abc", "https://attacker-google.com/mail/vf-abc", "https://mail-settings.google.com.evil/mail/vf-abc", "https://mail-settings.google.com/other"}
	for _, raw := range invalid {
		parsed, err := url.Parse(raw)
		if err == nil && isAllowedGmailVerificationURL(*parsed) {
			t.Fatalf("deceptive URL accepted: %s", raw)
		}
	}
}

func TestGmailConfirmationIgnoresUntrustedLinks(t *testing.T) {
	parsed := ParsedEmail{Sender: "forwarding-noreply@google.com", Subject: "Gmail Forwarding Confirmation - Receive Mail from owner@example.com", TextBody: strings.Join([]string{"https://attacker.example/confirm", "Confirmation code: 123456"}, "\n")}
	action, ok := detectControlAction(parsed, [32]byte{2})
	if !ok || action.ActionURL != "" || action.ActionCode != "123456" {
		t.Fatalf("action = %#v, recognized = %v", action, ok)
	}
}
