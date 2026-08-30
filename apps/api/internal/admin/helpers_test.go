package admin

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	wantTime := time.Date(2026, 8, 30, 3, 10, 11, 12, time.UTC)
	wantID := "7ab578ff-10ec-4de0-a046-96f13bedb9c8"
	got, ok := parseCursor(makeCursor(wantTime, wantID))
	if !ok || !got.Time.Equal(wantTime) || got.ID != wantID {
		t.Fatalf("cursor mismatch: %#v, %v", got, ok)
	}
	if _, ok := parseCursor("not-a-cursor"); ok {
		t.Fatal("invalid cursor accepted")
	}
}

func TestSafeJobRefsRedactsPayload(t *testing.T) {
	payload := json.RawMessage(`{"source_event_id":"source-1","document_id":"document-1","household_id":"secret-household","message":"raw text"}`)
	got := safeJobRefs(payload)
	if len(got) != 2 || got["source_event_id"] != "source-1" || got["document_id"] != "document-1" {
		t.Fatalf("unexpected safe refs: %#v", got)
	}
	if _, ok := got["message"]; ok {
		t.Fatal("raw message leaked")
	}
}

func TestPageLimitBounded(t *testing.T) {
	r, _ := http.NewRequest("GET", "/?limit=999", nil)
	if got := pageLimit(r, 50, 100); got != 100 {
		t.Fatalf("got %d", got)
	}
}
