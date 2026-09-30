package analytics

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// These tests are intentionally non-parallel: slog's default is process-wide.
func captureProductEvents(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output
}

func assertProductEvents(t *testing.T, output *bytes.Buffer, event string, count int, keys ...string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if count == 0 && output.Len() == 0 {
		return
	}
	if len(lines) != count {
		t.Fatalf("event count=%d want=%d", len(lines), count)
	}
	allowed := map[string]bool{"time": true, "level": true, "msg": true}
	for _, key := range keys {
		allowed[key] = true
	}
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["msg"] != event {
			t.Fatalf("unexpected event %v", record["msg"])
		}
		for key := range record {
			if !allowed[key] {
				t.Fatalf("unbounded telemetry field %q", key)
			}
		}
	}
}
