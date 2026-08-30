package admin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type pageCursor struct {
	Time time.Time
	ID   string
}

func parseCursor(value string) (pageCursor, bool) {
	if value == "" {
		return pageCursor{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return pageCursor{}, false
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return pageCursor{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || parts[1] == "" {
		return pageCursor{}, false
	}
	return pageCursor{Time: t, ID: parts[1]}, true
}

func makeCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func pageLimit(r *http.Request, fallback, maximum int) int {
	value := fallback
	if parsed := 0; r.URL.Query().Get("limit") != "" {
		if _, err := fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &parsed); err == nil {
			value = parsed
		}
	}
	if value < 1 {
		value = fallback
	}
	if value > maximum {
		value = maximum
	}
	return value
}

func dateBound(value string, end bool) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		if end {
			return t.AddDate(0, 0, 1), true
		}
		return t, true
	}
	return time.Time{}, false
}

func safeJobRefs(raw json.RawMessage) map[string]string {
	var payload map[string]any
	out := map[string]string{}
	if json.Unmarshal(raw, &payload) != nil {
		return out
	}
	for _, key := range []string{"source_event_id", "document_id", "insight_id", "review_item_id"} {
		if value, ok := payload[key].(string); ok && value != "" {
			out[key] = value
		}
	}
	return out
}
