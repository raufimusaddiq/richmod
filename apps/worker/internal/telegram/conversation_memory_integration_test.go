package telegram

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func (f *terminalFixture) turnCount(where string, args ...any) int {
	var count int
	query := `SELECT count(*) FROM telegram_conversation_turn WHERE household_id=$1 AND ` + where
	if err := f.pool.QueryRow(f.ctx, query, append([]any{f.householdID}, args...)...).Scan(&count); err != nil {
		f.t.Fatal(err)
	}
	return count
}

func (f *terminalFixture) updateFor(messageID int64) telegramUpdate {
	var u telegramUpdate
	u.Message.MessageID, u.Message.Chat.ID, u.Message.From.ID = messageID, f.chatID, f.chatID
	return u
}

// A retried message runs the turn again. The household's message and the
// assistant's answer are saved once per source event; tool calls and rows
// without a source event are not deduplicated.
func TestRetriedTurnSavesTheUserAndAssistantRowsOnce(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	event := f.event(61)
	update := f.updateFor(61)
	save := func(role, text, tool string) {
		if err := processor.persistTurn(f.ctx, f.householdID, event, update, role, text, tool, map[string]any{"k": "v"}); err != nil {
			t.Fatal(err)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		save("USER", "makan di luar lumayan gede juga?", "")
		save("TOOL", "", "get_cycle_changes")
		save("ASSISTANT", "Makan di Luar Rp413.392.", "")
	}
	if got := f.turnCount(`source_event_id=$2::uuid AND role='USER'`, event); got != 1 {
		t.Fatalf("a retried message must be saved once, got %d USER rows", got)
	}
	if got := f.turnCount(`source_event_id=$2::uuid AND role='ASSISTANT'`, event); got != 1 {
		t.Fatalf("a retried answer must be saved once, got %d ASSISTANT rows", got)
	}
	if got := f.turnCount(`source_event_id=$2::uuid AND role='TOOL'`, event); got != 2 {
		t.Fatalf("each tool call is its own row, got %d", got)
	}

	// Without a source event there is nothing to deduplicate against.
	for i := 0; i < 2; i++ {
		if err := processor.persistTurn(f.ctx, f.householdID, "", update, "USER", "tanpa sumber", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.turnCount(`source_event_id IS NULL AND role='USER'`); got != 2 {
		t.Fatalf("rows without a source event are always inserted, got %d", got)
	}
}

// The memory window reaches back a day, returns oldest first, keeps the newest
// exchange whole, and compacts what is older.
func TestRecentConversationReachesBackADayAndCompactsOlderTurns(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	insert := func(event, role, text, tool string, minutesAgo int, context string) {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO telegram_conversation_turn(household_id,telegram_user_id,telegram_chat_id,source_event_id,role,message_text,tool_name,public_context_json,created_at)
			VALUES($1,$2,$2,$3::uuid,$4,NULLIF($5,''),NULLIF($6,''),$7::jsonb,now()-make_interval(mins => $8))`,
			f.householdID, f.chatID, event, role, text, tool, context, minutesAgo); err != nil {
			t.Fatal(err)
		}
	}
	tooOld, earlier, midA, midB, latest := f.event(71), f.event(72), f.event(74), f.event(75), f.event(73)
	insert(tooOld, "USER", "terlalu lama", "", 30*60, `{}`)
	insert(earlier, "USER", "tadi pagi "+strings.Repeat("a", 400), "", 10*60, `{}`)
	insert(earlier, "ASSISTANT", "jawaban pagi "+strings.Repeat("b", 600), "", 10*60-1, `{}`)
	insert(midA, "USER", "pertanyaan tengah satu "+strings.Repeat("c", 400), "", 40, `{}`)
	insert(midA, "ASSISTANT", "jawaban tengah satu", "", 39, `{}`)
	insert(midB, "USER", "pertanyaan tengah dua", "", 20, `{}`)
	insert(midB, "ASSISTANT", "jawaban tengah dua", "", 19, `{}`)
	insert(latest, "USER", "kenapa siklus ini 6juta?", "", 6, `{}`)
	insert(latest, "TOOL", "", "get_cycle_changes", 5, `{"rows":"data penting untuk follow-up"}`)
	insert(latest, "ASSISTANT", "Pengeluaran Rp6.312.512.", "", 4, `{}`)

	got, err := processor.recentConversation(f.ctx, f.householdID, f.chatID, "")
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, turn := range got {
		texts = append(texts, turn.Role+":"+turn.Text)
		if strings.Contains(turn.Text, "terlalu lama") {
			t.Fatalf("a turn older than the window must not be loaded: %+v", turn)
		}
	}
	// 9 rows are inside the window (the 30-hour-old one is not).
	if len(got) != 9 {
		t.Fatalf("expected the 9 turns inside the window, got %d: %v", len(got), texts)
	}
	// Oldest first.
	if !strings.HasPrefix(got[0].Text, "tadi pagi") || got[len(got)-1].Text != "Pengeluaran Rp6.312.512." {
		t.Fatalf("turns must be oldest first, got %v", texts)
	}
	// All but the newest verbatimTurns rows are compacted and clipped.
	cut := len(got) - verbatimTurns
	for i, turn := range got {
		if i < cut {
			limit := olderUserChars
			if turn.Role == "ASSISTANT" {
				limit = olderAssistantChars
			}
			if !turn.Compacted || len([]rune(turn.Text)) > limit {
				t.Fatalf("older turn %d must be compacted and clipped: %+v", i, turn)
			}
		} else if turn.Compacted {
			t.Fatalf("turn %d is among the newest and must stay whole: %+v", i, turn)
		}
	}
	// The newest exchange keeps its tool result.
	var toolContext any
	for _, turn := range got {
		if turn.Role == "TOOL" {
			toolContext = turn.Context["rows"]
		}
	}
	if toolContext != "data penting untuk follow-up" {
		t.Fatalf("the newest exchange must keep its tool result, got %v", toolContext)
	}
}

// Reading is bounded no matter how long the chat has been.
func TestRecentConversationIsBoundedForALongChat(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	event := f.event(81)
	for i := 0; i < conversationScanRows*3; i++ {
		role := "USER"
		if i%2 == 1 {
			role = "ASSISTANT"
		}
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO telegram_conversation_turn(household_id,telegram_user_id,telegram_chat_id,source_event_id,role,message_text,created_at)
			VALUES($1,$2,$2,$3::uuid,$4,$5,now()-make_interval(secs => $6::double precision))`, f.householdID, f.chatID, event, role, fmt.Sprintf("turn %03d", i), float64((conversationScanRows*3-i)*30)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := processor.recentConversation(f.ctx, f.householdID, f.chatID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || len(got) > conversationScanRows {
		t.Fatalf("a long chat must load at most %d rows, got %d", conversationScanRows, len(got))
	}
	if got[len(got)-1].Text != fmt.Sprintf("turn %03d", conversationScanRows*3-1) {
		t.Fatalf("the newest turn must be present and last, got %q", got[len(got)-1].Text)
	}
}

// Two workers saving the same turn at the same moment (a stale lock reclaimed
// while the first attempt is still finishing) must still leave one USER row and
// one ASSISTANT row.
func TestConcurrentSavesOfTheSameTurnLeaveOneRow(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	event := f.event(91)
	update := f.updateFor(91)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 16; i++ {
		for _, role := range []string{"USER", "ASSISTANT"} {
			wg.Add(1)
			go func(role string) {
				defer wg.Done()
				errs <- processor.persistTurn(f.ctx, f.householdID, event, update, role, "teks "+role, "", nil)
			}(role)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"USER", "ASSISTANT"} {
		if got := f.turnCount(`source_event_id=$2::uuid AND role=$3`, event, role); got != 1 {
			t.Fatalf("concurrent saves must leave one %s row, got %d", role, got)
		}
	}
}

// A replayed tool result keeps its names and figures but not the analytics refs
// that were valid only in the turn that issued them.
func TestReplayedToolResultsCarryNoExpiredCategoryRefs(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	event := f.event(95)
	update := f.updateFor(95)
	context := map[string]any{"categories": []any{map[string]any{"ref": "category.5", "name": "Makan di Luar", "amount": "413392"}}, "transactions": []any{map[string]any{"ref": "tx_2"}}}
	if err := processor.persistTurn(f.ctx, f.householdID, event, update, "TOOL", "", "get_cycle_changes", context); err != nil {
		t.Fatal(err)
	}
	got, err := processor.recentConversation(f.ctx, f.householdID, f.chatID, "")
	if err != nil {
		t.Fatal(err)
	}
	var encoded string
	for _, turn := range got {
		if turn.Role == "TOOL" {
			encoded = fmt.Sprint(turn.Context)
		}
	}
	if encoded == "" {
		t.Fatal("the tool turn must be replayed")
	}
	if strings.Contains(encoded, "category.5") {
		t.Fatalf("an expired analytics ref must not be replayed: %s", encoded)
	}
	if !strings.Contains(encoded, "Makan di Luar") || !strings.Contains(encoded, "413392") || !strings.Contains(encoded, "tx_2") {
		t.Fatalf("names, figures, and tx refs must survive: %s", encoded)
	}
}
