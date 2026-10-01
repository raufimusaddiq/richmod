package telegram

import (
	"testing"
	"time"
)

func TestIndonesianDateFormatting(t *testing.T) {
	at := time.Date(2026, time.May, 3, 21, 5, 0, 0, time.UTC) // 04 May 04:05 Jakarta
	cases := map[string]string{
		formatIDDate(at):     "04 Mei 2026",
		formatIDDayMonth(at): "04 Mei",
		formatIDDateTime(at): "04 Mei 2026 04:05",
		formatIDDate(time.Date(2026, time.August, 1, 0, 0, 0, 0, jakartaLocation())):     "01 Agu 2026",
		formatIDDate(time.Date(2026, time.October, 9, 12, 0, 0, 0, jakartaLocation())):   "09 Okt 2026",
		formatIDDate(time.Date(2026, time.December, 31, 12, 0, 0, 0, jakartaLocation())): "31 Des 2026",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestPendingMarkupFollowsLatestSideEffect(t *testing.T) {
	batch := agentToolResult{Class: agentToolSideEffect, Status: "PENDING_CONFIRMATION", Mutation: map[string]any{"action": "BATCH_STAGED"}}
	correction := agentToolResult{Class: agentToolSideEffect, Status: "PENDING_CONFIRMATION", Mutation: map[string]any{"action": "CORRECTION_STAGED"}}
	edit := agentToolResult{Class: agentToolSideEffect, Status: "EDIT_CONFIRMATION_REQUIRED"}
	read := agentToolResult{Class: agentToolRead, Status: "OK"}
	done := agentToolResult{Class: agentToolSideEffect, Status: "COMMITTED"}

	if m := agentPendingMarkup([]agentToolResult{batch, read}); m == nil || m.InlineKeyboard[0][0].CallbackData != callbackPendingBatchYes {
		t.Fatalf("batch staging must offer batch buttons, got %+v", m)
	}
	if m := agentPendingMarkup([]agentToolResult{correction}); m == nil || m.InlineKeyboard[0][0].CallbackData != callbackPendingActionYes {
		t.Fatalf("correction staging must offer action buttons, got %+v", m)
	}
	if m := agentPendingMarkup([]agentToolResult{edit}); m == nil || m.InlineKeyboard[0][1].CallbackData != callbackPendingActionNo {
		t.Fatalf("edit confirmation must offer action buttons, got %+v", m)
	}
	if m := agentPendingMarkup([]agentToolResult{batch, done}); m != nil {
		t.Fatalf("a later committed side effect must clear stale buttons, got %+v", m)
	}
	if m := agentPendingMarkup(nil); m != nil {
		t.Fatalf("no history must offer no buttons")
	}
}
