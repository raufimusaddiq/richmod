package telegram

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func turnsOf(n int) []PublicTurn {
	var turns []PublicTurn
	for i := 0; i < n; i++ {
		turns = append(turns,
			PublicTurn{Role: "USER", Text: fmt.Sprintf("pertanyaan %d %s", i, strings.Repeat("x", 400))},
			PublicTurn{Role: "TOOL", Tool: "get_cycle_changes", Context: map[string]any{"rows": strings.Repeat("d", 5000)}},
			PublicTurn{Role: "ASSISTANT", Text: fmt.Sprintf("jawaban %d %s", i, strings.Repeat("y", 600))},
		)
	}
	return turns
}

// verbatimStart is where the newest verbatimExchanges user exchanges begin.
func verbatimStart(turns []PublicTurn) int {
	seen := 0
	for index := len(turns) - 1; index >= 0; index-- {
		if turns[index].Role == "USER" {
			seen++
			if seen == verbatimExchanges {
				return index
			}
		}
	}
	return 0
}

func TestCompactionKeepsTheNewestExchangesWholeAndShrinksTheRest(t *testing.T) {
	input := turnsOf(6) // 6 exchanges, 18 rows, tool rows of 5,000 characters each
	got := compactConversation(input)

	// The newest exchange is untouched, tool result included.
	tail := got[len(got)-3:]
	for i, turn := range tail {
		want := input[len(input)-3+i]
		if turn.Role != want.Role || turn.Text != want.Text || turn.Compacted {
			t.Fatalf("newest exchange row %d was changed: %+v", i, turn)
		}
		if want.Role == "TOOL" && turn.Context == nil {
			t.Fatal("the newest exchange must keep its tool result for follow-ups")
		}
	}
	// The verbatim window counts exchanges, not rows: all three newest user
	// messages are present whole.
	whole := 0
	for _, turn := range got {
		if turn.Role == "USER" && !turn.Compacted {
			whole++
		}
	}
	if whole != verbatimExchanges {
		t.Fatalf("want the %d newest user messages whole, got %d", verbatimExchanges, whole)
	}
	// Older rows are clipped, marked, stripped of tool data, and never TOOL rows.
	for _, turn := range got {
		if !turn.Compacted {
			continue
		}
		if turn.Context != nil || turn.Role == "TOOL" {
			t.Fatalf("older turn was not compacted: %+v", turn)
		}
		limit := olderUserChars
		if turn.Role == "ASSISTANT" {
			limit = olderAssistantChars
		}
		if len([]rune(turn.Text)) > limit {
			t.Fatalf("older %s text exceeds %d: %d", turn.Role, limit, len([]rune(turn.Text)))
		}
	}
}

// A1: tool JSON counts against the budget. The tool results of older verbatim
// exchanges are dropped before the newest exchange is touched.
func TestCompactionCountsToolJSONAgainstTheBudget(t *testing.T) {
	got := compactConversation(turnsOf(4))
	total := 0
	tools := 0
	for _, turn := range got {
		total += turnMemorySize(turn)
		if turn.Role == "TOOL" {
			tools++
		}
	}
	if tools != 1 {
		t.Fatalf("only the newest exchange's 5,000-character tool result fits the budget, kept %d tool rows", tools)
	}
	if got[len(got)-2].Role != "TOOL" {
		t.Fatalf("the kept tool result must be the newest one: %+v", got[len(got)-2])
	}
	if total > conversationCharBudget+6000 {
		t.Fatalf("memory %d is far over the budget %d", total, conversationCharBudget)
	}
}

func TestCompactionKeepsChronologicalOrder(t *testing.T) {
	got := compactConversation(turnsOf(5))
	var order []string
	for _, turn := range got {
		if turn.Role == "USER" {
			order = append(order, turn.Text[:12])
		}
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] >= order[i] {
			t.Fatalf("turns must stay oldest first: %v", order)
		}
	}
}

func TestCompactionBudgetDropsTheOldestCompactedTurnsFirstAndNeverTheNewest(t *testing.T) {
	input := turnsOf(40) // many exchanges: older text alone exceeds the budget
	got := compactConversation(input)
	last := got[len(got)-1]
	if last.Role != "ASSISTANT" || !strings.HasPrefix(last.Text, "jawaban 39") {
		t.Fatalf("the newest turn must survive the budget, got %+v", last)
	}
	// Whatever older turns remain are the most recent of the older ones.
	var older []PublicTurn
	for _, turn := range got {
		if turn.Compacted {
			older = append(older, turn)
		}
	}
	if len(older) > 0 && !strings.HasPrefix(older[len(older)-1].Text, "jawaban 36") {
		t.Fatalf("budget must drop the oldest first, kept %q last", older[len(older)-1].Text)
	}
	if len(older) > 0 && strings.HasPrefix(older[0].Text, "pertanyaan 0 ") {
		t.Fatal("the oldest turn should have been dropped first")
	}
}

func TestCompactionNeverDropsVerbatimTurnsEvenOverBudget(t *testing.T) {
	huge := []PublicTurn{{Role: "USER", Text: strings.Repeat("z", conversationCharBudget*3)}}
	got := compactConversation(huge)
	if len(got) != 1 || got[0].Text != huge[0].Text || got[0].Compacted {
		t.Fatalf("a recent turn must survive even when it alone exceeds the budget: %+v", got)
	}
}

func TestCompactionHandlesShortAndEmptyConversations(t *testing.T) {
	if got := compactConversation(nil); len(got) != 0 {
		t.Fatalf("nil must stay empty, got %v", got)
	}
	short := turnsOf(1)
	got := compactConversation(short)
	if len(got) != len(short) {
		t.Fatalf("a short conversation is kept whole, got %d of %d rows", len(got), len(short))
	}
	for _, turn := range got {
		if turn.Compacted {
			t.Fatal("nothing in a short conversation should be marked compacted")
		}
	}
}

func TestClipRunesDoesNotSplitMultibyteCharacters(t *testing.T) {
	text := strings.Repeat("é", 50)
	clipped := clipRunes(text, 10)
	if len([]rune(clipped)) != 10 || !strings.HasSuffix(clipped, "…") {
		t.Fatalf("clip must keep 10 characters ending in an ellipsis, got %q", clipped)
	}
	if clipRunes("pendek", 10) != "pendek" {
		t.Fatal("text under the limit must be unchanged")
	}
}

func TestMemoryWindowIsLongerThanAnHourButBounded(t *testing.T) {
	// The old window was 60 minutes and 20 rows. A follow-up the next morning must
	// still work; the scan and the budget keep the prompt from growing with it.
	if conversationWindow <= time.Hour {
		t.Fatalf("window %s is not longer than the old one hour", conversationWindow)
	}
	if conversationScanRows < 20 || verbatimExchanges < 2 || conversationCharBudget < 2000 {
		t.Fatal("memory limits are too small to carry a follow-up")
	}
}

// The budget is in characters, so text with multi-byte characters is not counted
// as if each were several characters.
func TestCompactionBudgetCountsCharactersNotBytes(t *testing.T) {
	// 40 older turns of 150 two-byte characters: 6,000 characters in total fit the
	// budget, but the same text measured in bytes (12,000) would not.
	var turns []PublicTurn
	for i := 0; i < 40; i++ {
		turns = append(turns, PublicTurn{Role: "USER", Text: strings.Repeat("é", 150)})
	}
	turns = append(turns, PublicTurn{Role: "USER"}, PublicTurn{Role: "USER"}, PublicTurn{Role: "USER"})
	kept := 0
	for _, turn := range compactConversation(turns) {
		if turn.Compacted {
			kept++
		}
	}
	if kept < 30 {
		t.Fatalf("multi-byte text must not be dropped as if it were twice as long, kept %d of 40", kept)
	}
}

func TestExpiredAnalyticsRefsAreDroppedButFiguresAndOtherRefsStay(t *testing.T) {
	toolContext := map[string]any{
		"categories": []any{
			map[string]any{"ref": "category.1", "name": "Orang Tua", "amount": "2500000"},
			map[string]any{"ref": "category.5", "name": "Makan di Luar", "amount": "413392", "merchants": []any{
				map[string]any{"ref": "category.5.merchant.2", "name": "Warung"},
			}},
		},
		"refs":         []any{"category.1", "category.5", "tx_3"},
		"transactions": []any{map[string]any{"ref": "tx_7", "amount": "50000"}, map[string]any{"ref": "review_2"}},
		"period":       map[string]any{"start": "2026-09-25"},
	}
	dropExpiredAnalyticsRefs(toolContext)

	encoded := fmt.Sprint(toolContext)
	for _, expired := range []string{"category.1", "category.5", "category.5.merchant.2"} {
		if strings.Contains(encoded, expired) {
			t.Fatalf("expired ref %q must be removed from a replayed turn: %s", expired, encoded)
		}
	}
	for _, kept := range []string{"Orang Tua", "2500000", "Makan di Luar", "413392", "Warung", "2026-09-25", "tx_3", "tx_7", "review_2"} {
		if !strings.Contains(encoded, kept) {
			t.Fatalf("%q must survive: names and figures stay so a follow-up can refer to them, and tx_/review_ refs are valid across turns: %s", kept, encoded)
		}
	}
}

func TestDroppingExpiredRefsToleratesEmptyAndOddInput(t *testing.T) {
	dropExpiredAnalyticsRefs(nil)
	dropExpiredAnalyticsRefs(map[string]any{})
	dropExpiredAnalyticsRefs(map[string]any{"ref": 42, "list": []any{nil, 1, "category.x", "category.1x"}})
	if got := dropExpiredAnalyticsRefs("category.2"); got != "category.2" {
		t.Fatal("a bare string is not a container and must be returned as is")
	}
	// Only well-formed refs are expired; text that merely resembles one is not.
	kept := map[string]any{"note": "category.x", "name": "category.1x"}
	dropExpiredAnalyticsRefs(kept)
	if len(kept) != 2 {
		t.Fatalf("malformed look-alikes must be left alone, got %v", kept)
	}
}
