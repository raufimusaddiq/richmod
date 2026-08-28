package gmail

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestDecodeHistoryPayload(t *testing.T) {
	payload, err := DecodeHistoryPayload(json.RawMessage(`{"source_event_id":"source-1","history_id":"123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if payload.SourceEventID != "source-1" || payload.HistoryID != "123" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestDecodeHistoryPayloadRejectsMissingFields(t *testing.T) {
	if _, err := DecodeHistoryPayload(json.RawMessage(`{"history_id":"123"}`)); err == nil {
		t.Fatal("expected invalid payload error")
	}
}

func TestParseMessageExtractsAuthenticatedHTML(t *testing.T) {
	body := `<div>Jumlah</div><div>Rp55.199</div>`
	message := gmailMessage{ID: "message-1"}
	message.Payload.MimeType = "multipart/alternative"
	message.Payload.Headers = append(message.Payload.Headers,
		struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{Name: "From", Value: "Bank Jago <noreply@jago.com>"},
		struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{Name: "Subject", Value: "Kamu telah membayar ke PAMELLA DUA"},
		struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{Name: "Authentication-Results", Value: "dkim=pass; dmarc=pass"},
	)
	message.Payload.Parts = []messagePart{{MimeType: "text/html", Body: messageBody{Data: base64.RawURLEncoding.EncodeToString([]byte(body))}}}

	parsed, err := parseMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.fromDomain != "jago.com" || parsed.subject == "" || parsed.html != body || parsed.body != body {
		t.Fatalf("unexpected parsed message: %#v", parsed)
	}
}

func TestParseMessageFallsBackToPlainTextBody(t *testing.T) {
	body := "Thank you for using d-Card Jenius.\nMerchant: TOKOPEDIA\nTotal: IDR 378,075.00"
	message := gmailMessage{ID: "message-jenius"}
	message.Payload.MimeType = "multipart/alternative"
	message.Payload.Headers = append(message.Payload.Headers,
		struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{Name: "From", Value: "Jenius <jenius_noreply@smbci.com>"},
		struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{Name: "Subject", Value: "d-Card Credit Card Transaction"},
	)
	message.Payload.Parts = []messagePart{{MimeType: "text/plain", Body: messageBody{Data: base64.RawURLEncoding.EncodeToString([]byte(body))}}}

	parsed, err := parseMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.fromDomain != "smbci.com" || parsed.html != "" || parsed.body != body {
		t.Fatalf("unexpected parsed plain-text message: %#v", parsed)
	}
}

func TestSenderDomain(t *testing.T) {
	if got := senderDomain("NoReply@Jago.com"); got != "jago.com" {
		t.Fatalf("senderDomain = %q", got)
	}
}
