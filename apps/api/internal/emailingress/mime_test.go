package emailingress

import (
	"strings"
	"testing"
)

func TestParseMIMEMultipartAlternative(t *testing.T) {
	raw := []byte("From: Bank Alerts <alerts@example.test>\r\nSubject: =?UTF-8?B?VHJhbnNha3Np?=\r\nMessage-ID: <abc@example.test>\r\nAuthentication-Results: mx.example; dkim=pass; dmarc=pass\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=xyz\r\n\r\n--xyz\r\nContent-Type: text/plain\r\n\r\nAmount 100\r\n--xyz\r\nContent-Type: text/html\r\n\r\n<html><body><b>Amount</b> 100</body></html>\r\n--xyz--\r\n")
	email, err := parseMIME(raw)
	if err != nil {
		t.Fatal(err)
	}
	if email.Sender != "alerts@example.test" || email.MessageID != "<abc@example.test>" {
		t.Fatalf("metadata: %#v", email)
	}
	if email.Subject != "Transaksi" {
		t.Fatalf("decoded subject = %q", email.Subject)
	}
	if email.TextBody == "" || email.HTMLBody == "" {
		t.Fatalf("bodies missing: %#v", email)
	}
	if got := visibleHTML(email.HTMLBody); got != "Amount 100" {
		t.Fatalf("visible html = %q", got)
	}
}

func TestParseMIMEMixedTransferEncodingsAndIgnoresAttachment(t *testing.T) {
	raw := []byte("From: Alerts <alerts@example.test>\r\nSubject: Mixed\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=mix\r\n\r\n--mix\r\nContent-Type: multipart/alternative; boundary=alt\r\n\r\n--alt\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nAmount=20IDR=20100\r\n--alt\r\nContent-Type: text/html\r\nContent-Transfer-Encoding: base64\r\n\r\nPGI+QW1vdW50IElEUiAxMDA8L2I+\r\n--alt--\r\n--mix\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=statement.pdf\r\n\r\nnot parsed\r\n--mix--\r\n")
	email, err := parseMIME(raw)
	if err != nil {
		t.Fatal(err)
	}
	if email.TextBody != "Amount IDR 100" {
		t.Fatalf("quoted printable = %q", email.TextBody)
	}
	if got := visibleHTML(email.HTMLBody); got != "Amount IDR 100" {
		t.Fatalf("base64 HTML = %q", got)
	}
	if strings.Contains(email.TextBody+email.HTMLBody, "not parsed") {
		t.Fatal("attachment entered extraction body")
	}
}
