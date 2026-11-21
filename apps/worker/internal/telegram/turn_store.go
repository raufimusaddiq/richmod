package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type PublicTurn struct {
	Role      string         `json:"role"`
	Text      string         `json:"text,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
	At        string         `json:"at,omitempty"`
	Failure   string         `json:"failure,omitempty"`
	Compacted bool           `json:"compacted,omitempty"`
}

const (
	conversationWindow     = 24 * time.Hour
	conversationScanRows   = 40
	verbatimExchanges      = 3
	olderUserChars         = 200
	olderAssistantChars    = 300
	conversationCharBudget = 6000
)

var agentBookkeepingTools = []string{"agent_transaction_refs", gatewayDeferredTool}

func (p *Processor) persistTurn(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, role, text, tool string, public map[string]any) error {
	if p.pool == nil {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := insertTurnTx(ctx, tx, householdID, sourceEventID, update, role, text, tool, public); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertTurnTx(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, update telegramUpdate, role, text, tool string, public map[string]any) error {
	encoded, _ := json.Marshal(public)
	if sourceEventID != "" && (role == "USER" || role == "ASSISTANT") {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, "telegram_turn:"+sourceEventID+":"+role); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO telegram_conversation_turn(household_id,telegram_user_id,telegram_chat_id,source_event_id,role,message_text,tool_name,public_context_json,telegram_message_id)
		SELECT $1::uuid,$2::bigint,$3::bigint,NULLIF($4::text,'')::uuid,$5::text,NULLIF($6::text,''),NULLIF($7::text,''),$8::jsonb,$9::bigint
		WHERE $5::text NOT IN ('USER','ASSISTANT') OR NULLIF($4::text,'') IS NULL
		   OR NOT EXISTS (SELECT 1 FROM telegram_conversation_turn WHERE source_event_id=NULLIF($4::text,'')::uuid AND role=$5::text)`,
		householdID, update.Message.From.ID, update.Message.Chat.ID, sourceEventID, role, text, tool, encoded, update.Message.MessageID)
	return err
}

func (p *Processor) recentConversation(ctx context.Context, householdID string, chatID int64, currentSourceID string) ([]PublicTurn, error) {
	rows, err := p.pool.Query(ctx, `SELECT role,COALESCE(message_text,''),COALESCE(tool_name,''),COALESCE(public_context_json,'{}'::jsonb)
		FROM telegram_conversation_turn
		WHERE household_id=$1 AND telegram_chat_id=$2 AND source_event_id IS DISTINCT FROM NULLIF($3::text,'')::uuid
		  AND created_at >= now()-make_interval(secs => $4::double precision)
		  AND (tool_name IS NULL OR NOT tool_name=ANY($6::text[]))
		ORDER BY created_at DESC,id DESC LIMIT $5::int`, householdID, chatID, currentSourceID, conversationWindow.Seconds(), conversationScanRows, agentBookkeepingTools)
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
		if turn.Role != "TOOL" {
			turn.At, _ = turn.Context["current_jakarta_datetime"].(string)
			turn.Failure, _ = turn.Context["failure"].(string)
			turn.Context = nil
		}
		dropExpiredAnalyticsRefs(turn.Context)
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

var expiredAnalyticsRef = regexp.MustCompile(`^category\.[0-9]+(\.[a-z_]+\.[0-9]+)*$`)

func dropExpiredAnalyticsRefs(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if text, ok := item.(string); ok && expiredAnalyticsRef.MatchString(text) {
				delete(v, key)
				continue
			}
			v[key] = dropExpiredAnalyticsRefs(item)
		}
		return v
	case []any:
		kept := v[:0]
		for _, item := range v {
			if text, ok := item.(string); ok && expiredAnalyticsRef.MatchString(text) {
				continue
			}
			kept = append(kept, dropExpiredAnalyticsRefs(item))
		}
		return kept
	}
	return value
}

func compactConversation(turns []PublicTurn) []PublicTurn {
	cut := 0
	seen := 0
	for index := len(turns) - 1; index >= 0; index-- {
		if turns[index].Role == "USER" {
			seen++
			if seen == verbatimExchanges {
				cut = index
				break
			}
		}
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
		older = append(older, PublicTurn{Role: turn.Role, Text: clipRunes(turn.Text, limit), At: turn.At, Failure: turn.Failure, Compacted: true})
	}
	verbatim := append([]PublicTurn(nil), turns[cut:]...)
	newestExchange := 0
	for index := len(verbatim) - 1; index >= 0; index-- {
		if verbatim[index].Role == "USER" {
			newestExchange = index
			break
		}
	}
	total := 0
	for _, turn := range verbatim {
		total += turnMemorySize(turn)
	}
	for _, turn := range older {
		total += turnMemorySize(turn)
	}
	if total > conversationCharBudget {
		kept := make([]PublicTurn, 0, len(verbatim))
		for index, turn := range verbatim {
			if total > conversationCharBudget && turn.Role == "TOOL" && index < newestExchange {
				total -= turnMemorySize(turn)
				continue
			}
			kept = append(kept, turn)
		}
		verbatim = kept
	}
	for len(older) > 0 && total > conversationCharBudget {
		total -= turnMemorySize(older[0])
		older = older[1:]
	}
	return append(older, verbatim...)
}

func turnMemorySize(turn PublicTurn) int {
	size := utf8.RuneCountInString(turn.Text)
	if len(turn.Context) > 0 {
		encoded, _ := json.Marshal(turn.Context)
		size += utf8.RuneCount(encoded)
	}
	return size
}

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
