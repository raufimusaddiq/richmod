package emailingress

import "testing"

func TestParseMIMEMultipartAlternative(t *testing.T) {
	raw := []byte("From: Bank Alerts <alerts@example.test>\r\nSubject: =?UTF-8?B?VHJhbnNha3Np?=\r\nMessage-ID: <abc@example.test>\r\nAuthentication-Results: mx.example; dkim=pass; dmarc=pass\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=xyz\r\n\r\n--xyz\r\nContent-Type: text/plain\r\n\r\nAmount 100\r\n--xyz\r\nContent-Type: text/html\r\n\r\n<html><body><b>Amount</b> 100</body></html>\r\n--xyz--\r\n")
	email, err := parseMIME(raw)
	if err != nil {
		t.Fatal(err)
	}
	if email.Sender != "alerts@example.test" || email.MessageID != "<abc@example.test>" {
		t.Fatalf("metadata: %#v", email)
	}
	if email.TextBody == "" || email.HTMLBody == "" {
		t.Fatalf("bodies missing: %#v", email)
	}
	if got := visibleHTML(email.HTMLBody); got != "Amount 100" {
		t.Fatalf("visible html = %q", got)
	}
}
