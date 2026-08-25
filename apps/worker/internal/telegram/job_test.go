package telegram

import (
	"encoding/json"
	"testing"
)

func TestDecodeProcessPayloadRequiresSourceEvent(t *testing.T) {
	payload, err := DecodeProcessPayload(json.RawMessage(`{"source_event_id":"event-1"}`))
	if err != nil || payload.SourceEventID != "event-1" {
		t.Fatalf("payload = %#v, error = %v", payload, err)
	}
	if _, err := DecodeProcessPayload(json.RawMessage(`{}`)); err == nil {
		t.Fatal("empty source event ID was accepted")
	}
}
