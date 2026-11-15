package admin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type pageCursor struct {
	Time time.Time
	ID   string
}

// errInvalidCursor marks a cursor that was supplied but cannot be decoded to a
// usable keyset position. Callers turn it into 400 instead of page one.
var errInvalidCursor = errors.New("invalid cursor")

// cursorIDKind is the shape the endpoint's SQL casts the cursor id to.
type cursorIDKind int

const (
	cursorUUID cursorIDKind = iota
	cursorNumeric
	cursorText
)

// parseCursor decodes the shared (created_at, id) keyset cursor. An absent
// cursor is (zero, false, nil); a malformed one is an error, never a fallback
// to the first page.
func parseCursor(value string, kind cursorIDKind) (pageCursor, bool, error) {
	if value == "" {
		return pageCursor{}, false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return pageCursor{}, false, errInvalidCursor
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return pageCursor{}, false, errInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return pageCursor{}, false, errInvalidCursor
	}
	switch kind {
	case cursorUUID:
		if _, err := uuid.Parse(id); err != nil {
			return pageCursor{}, false, errInvalidCursor
		}
	case cursorNumeric:
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			return pageCursor{}, false, errInvalidCursor
		}
	}
	return pageCursor{Time: t, ID: id}, true, nil
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
