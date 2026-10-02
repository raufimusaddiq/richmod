package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type PublicTurn struct {
	Role    string         `json:"role"`
	Text    string         `json:"text,omitempty"`
	Tool    string         `json:"tool,omitempty"`
	Context map[string]any `json:"context,omitempty"`
	// Compacted marks an older turn whose text was shortened and whose tool data
	// was dropped, so the model does not treat it as the full original.
	Compacted bool `json:"compacted,omitempty"`
}

// Conversation memory. Each message is its own turn; what the agent remembers is
// the stored turns of the same chat inside a window, trimmed by compactConversation
// so a longer window does not mean a heavier prompt. The trimming is
// deterministic on purpose: a model-written summary would put untrusted numbers
// into later prompts, add a model call, and add one more thing that can time out.
const (
	// conversationWindow is how far back a follow-up can reach.
	conversationWindow = 24 * time.Hour
	// conversationScanRows bounds how many stored rows are read for one turn.
	conversationScanRows = 40
	// verbatimTurns is how many of the newest rows are kept whole, with their tool
	// results, because a follow-up refers to the last exchange most.
	verbatimTurns = 6
	// olderUserChars and olderAssistantChars clip the text of older turns.
	olderUserChars      = 200
	olderAssistantChars = 300
	// conversationCharBudget caps the text of all turns; the oldest compacted
	// turns are dropped first, the verbatim ones never are.
	conversationCharBudget = 6000
)

// persistTurn stores one conversation turn. A retried message runs the turn
// again, so the USER and ASSISTANT rows are saved once per source event; TOOL
// rows (one per call) and rows without a source event are always inserted.
func (p *Processor) persistTurn(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, role, text, tool string, public map[string]any) error {
	if p.pool == nil {
		return nil
	}
	encoded, _ := json.Marshal(public)
	_, err := p.pool.Exec(ctx, `INSERT INTO telegram_conversation_turn(household_id,telegram_user_id,telegram_chat_id,source_event_id,role,message_text,tool_name,public_context_json,telegram_message_id)
		SELECT $1::uuid,$2::bigint,$3::bigint,NULLIF($4::text,'')::uuid,$5::text,NULLIF($6::text,''),NULLIF($7::text,''),$8::jsonb,$9::bigint
		WHERE $5::text NOT IN ('USER','ASSISTANT') OR NULLIF($4::text,'') IS NULL
		   OR NOT EXISTS (SELECT 1 FROM telegram_conversation_turn WHERE source_event_id=NULLIF($4::text,'')::uuid AND role=$5::text)`,
		householdID, update.Message.From.ID, update.Message.Chat.ID, sourceEventID, role, text, tool, encoded, update.Message.MessageID)
	return err
}

// recentConversation returns the chat's recent turns, oldest first, compacted.
func (p *Processor) recentConversation(ctx context.Context, householdID string, chatID int64, currentSourceID string) ([]PublicTurn, error) {
	rows, err := p.pool.Query(ctx, `SELECT role,COALESCE(message_text,''),COALESCE(tool_name,''),COALESCE(public_context_json,'{}'::jsonb)
		FROM telegram_conversation_turn
		WHERE household_id=$1 AND telegram_chat_id=$2 AND source_event_id IS DISTINCT FROM NULLIF($3,'')::uuid
		  AND created_at >= now()-make_interval(secs => $4::double precision)
		ORDER BY created_at DESC LIMIT $5::int`, householdID, chatID, currentSourceID, conversationWindow.Seconds(), conversationScanRows)
	if err != nil {
		return nil, fmt.Errorf("load Telegram conversation context: %w", err)
	}
	defer rows.Close()
	var newestFirst []PublicTurn
	for rows.Next() {
		var turn PublicTurn
		var raw []byte
		if err := rows.Scan(&turn.Role, &turn.Text, &turn.Tool, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &turn.Context)
		newestFirst = append(newestFirst, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	chronological := make([]PublicTurn, len(newestFirst))
	for i, turn := range newestFirst {
		chronological[len(newestFirst)-1-i] = turn
	}
	return compactConversation(chronological), nil
}

// compactConversation takes turns oldest first. The newest verbatimTurns stay
// whole. Older turns keep only what a follow-up needs: user and assistant text,
// clipped, with tool results dropped and the turn marked Compacted. If the text
// still exceeds conversationCharBudget, the oldest compacted turns go first.
func compactConversation(turns []PublicTurn) []PublicTurn {
	cut := len(turns) - verbatimTurns
	if cut < 0 {
		cut = 0
	}
	older := make([]PublicTurn, 0, cut)
	for _, turn := range turns[:cut] {
		if turn.Role == "TOOL" {
			continue
		}
		limit := olderUserChars
		if turn.Role == "ASSISTANT" {
			limit = olderAssistantChars
		}
		older = append(older, PublicTurn{Role: turn.Role, Text: clipRunes(turn.Text, limit), Compacted: true})
	}
	verbatim := turns[cut:]
	total := 0
	for _, turn := range verbatim {
		total += len(turn.Text)
	}
	for _, turn := range older {
		total += len(turn.Text)
	}
	for len(older) > 0 && total > conversationCharBudget {
		total -= len(older[0].Text)
		older = older[1:]
	}
	return append(older, verbatim...)
}

// clipRunes shortens text to at most limit characters, adding an ellipsis when it
// cuts, without splitting a multi-byte character.
func clipRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

func (p *Processor) hasPendingSalaryChoice(ctx context.Context, householdID string, update telegramUpdate) (bool, error) {
	var found bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM salary_pending_choice WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now())`, householdID, update.Message.From.ID, update.Message.Chat.ID).Scan(&found)
	return found, err
}

func (p *Processor) resolveTransactionReference(ctx context.Context, householdID string, update telegramUpdate, ref string) (string, error) {
	var transactionID string
	err := p.pool.QueryRow(ctx, `SELECT r.entity_id FROM telegram_turn_reference r JOIN telegram_conversation_turn t ON t.id=r.turn_id JOIN transaction x ON x.id=r.entity_id WHERE r.household_id=$1 AND r.telegram_user_id=$2 AND r.telegram_chat_id=$3 AND r.ref_key=$4 AND r.entity_type='TRANSACTION' AND r.expires_at>now() AND x.household_id=$1 AND x.status<>'VOIDED' ORDER BY t.created_at DESC LIMIT 1`, householdID, update.Message.From.ID, update.Message.Chat.ID, ref).Scan(&transactionID)
	return transactionID, err
}
