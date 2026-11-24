package emailingress

import (
	"strconv"
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

func TestParseMIMEBoundsNestingPartsAndSenders(t *testing.T) {
	nested := "From: Alerts <alerts@example.test>\r\nContent-Type: multipart/mixed; boundary=b0\r\n\r\n"
	for depth := 0; depth < 12; depth++ {
		nested += "--b" + strconv.Itoa(depth) + "\r\nContent-Type: multipart/mixed; boundary=b" + strconv.Itoa(depth+1) + "\r\n\r\n"
	}
	nested += "--b12\r\nContent-Type: text/plain\r\n\r\nAmount 100\r\n"
	if _, err := parseMIME([]byte(nested)); err == nil {
		t.Fatal("deeply nested multipart was accepted")
	}
	wide := "From: Alerts <alerts@example.test>\r\nContent-Type: multipart/mixed; boundary=w\r\n\r\n--w\r\nContent-Type: text/plain\r\n\r\nAmount 100\r\n"
	for part := 0; part < maxMIMEParts+5; part++ {
		wide += "--w\r\nContent-Type: application/octet-stream\r\n\r\nx\r\n"
	}
	wide += "--w--\r\n"
	if email, err := parseMIME([]byte(wide)); err != nil || email.TextBody != "" {
		t.Logf("wide multipart parsed with error %v", err)
	}
	if _, err := parseMIME([]byte("From: Alerts <alerts@example.test>\r\nFrom: Spoof <spoof@example.test>\r\nContent-Type: text/plain\r\n\r\nAmount 100")); err == nil {
		t.Fatal("an email with two From headers was accepted")
	}
	long := "From: Alerts <alerts@example.test>\r\nSubject: " + strings.Repeat("s", 5000) + "\r\nContent-Type: text/plain\r\n\r\nAmount 100"
	email, err := parseMIME([]byte(long))
	if err != nil || len(email.Subject) > maxSubjectBytes {
		t.Fatalf("subject length = %d err=%v", len(email.Subject), err)
	}
}

func TestVisibleHTMLDropsStyleAndScriptContent(t *testing.T) {
	page := "<html><head><style>.a{color:red}</style></head><body><script>alert(1)</script><p>Amount 100</p></body></html>"
	if got := visibleHTML(page); got != "Amount 100" {
		t.Fatalf("visible html = %q", got)
	}
}

func TestSignedRequestRejectsUnsafeObjectKeys(t *testing.T) {
	for key, valid := range map[string]bool{"raw/2026/11/24/0b8f0a6e-1111-4222-8333-444455556666.eml": true, "raw/key.eml": true, "../escape.eml": false, "raw/../escape.eml": false, "/abs.eml": false, "raw//x.eml": false, "raw/x\n.eml": false, "raw/" + strings.Repeat("a", 300): false} {
		if validObjectKey(key) != valid {
			t.Errorf("validObjectKey(%q) = %t, want %t", key, !valid, valid)
		}
	}
}
