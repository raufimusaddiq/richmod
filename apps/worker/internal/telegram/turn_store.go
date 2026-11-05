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
	Role    string         `json:"role"`
	Text    string         `json:"text,omitempty"`
	Tool    string         `json:"tool,omitempty"`
	Context map[string]any `json:"context,omitempty"`
	// At is when the user sent the message (USER rows), so "yang tadi pagi" can be
	// placed in time.
	At string `json:"at,omitempty"`
	// Failure marks an assistant reply that was a failure notice (TIMEOUT,
	// UNAVAILABLE, ERROR, or an in-turn refusal): the question before it was not
	// answered and nothing was recorded from it.
	Failure string `json:"failure,omitempty"`
	// Compacted marks an older turn whose text was shortened and whose tool data
	// was dropped, so the model does not treat it as the full original.
	Compacted bool `json:"compacted,omitempty"`
}

// Conversation memory. Each message is its own turn; what the agent remembers is
// the stored turns of the same chat inside a window, trimmed by compactConversation
// so a longer window does not mean a heavier prompt. The trimming is
// deterministic on purpose: a model-written summary would put untrusted numbers
// into later prompts, add a model call, and add one more thing that can time out.
//
// Bookkeeping rows (agentBookkeepingTools) are not memory: they exist so opaque
// refs have a durable owner row, and are never replayed to the model.
const (
	// conversationWindow is how far back a follow-up can reach.
	conversationWindow = 24 * time.Hour
	// conversationScanRows bounds how many stored rows are read for one turn.
	conversationScanRows = 40
	// verbatimExchanges is how many of the newest user exchanges (a USER row and
	// every row after it up to the next USER row) are kept whole, with their tool
	// results, because a follow-up refers to the last exchanges most. It counts
	// exchanges, not rows, so tool rows cannot crowd the window down to one.
	verbatimExchanges = 3
	// olderUserChars and olderAssistantChars clip the text of older turns.
	olderUserChars      = 200
	olderAssistantChars = 300
	// conversationCharBudget caps the replayed memory: text plus the JSON of tool
	// results. Tool data of older verbatim exchanges goes first, then the oldest
	// compacted turns; the newest exchange is never trimmed.
	conversationCharBudget = 6000
)

// agentBookkeepingTools name TOOL rows that only anchor server-side refs.
var agentBookkeepingTools = []string{"agent_transaction_refs", gatewayDeferredTool}

// persistTurn stores one conversation turn. A retried message runs the turn
// again, so the USER and ASSISTANT rows are saved once per source event; TOOL
// rows (one per call) and rows without a source event are always inserted.
//
// "Once" holds even if two workers save the same turn at the same time (a stale
// lock reclaimed while the first attempt is still finishing): for those rows the
// insert runs behind a transaction-scoped advisory lock on the event and role, so
// the second saver waits, then sees the first row and skips. A unique index would
// give the same guarantee, but it would need a migration that first deletes the
// duplicate rows retries already left behind.
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

// insertTurnTx stores one turn inside the caller's transaction, with the same
// once-per-event rule as persistTurn. A reply and its turn row then commit
// together: the conversation can never record an answer that was not sent, or
// miss one that was.
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

// recentConversation returns the chat's recent turns, oldest first, compacted.
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
			// USER/ASSISTANT context is bookkeeping (send time, phase counters, a
			// failure marker); only what a follow-up needs is lifted out of it.
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

// expiredAnalyticsRef matches the opaque refs analytics READ tools issue
// ("category.3", "category.3.merchant.1"). They are positions in one turn's fact
// snapshot, valid only in the turn that issued them, so a replayed turn must not
// carry them: the model would reuse one and the read would be rejected, or worse,
// a position could point at a different category after the data changed.
// Transaction and review refs (tx_1, review_2) are bound server-side across turns
// and are left alone.
var expiredAnalyticsRef = regexp.MustCompile(`^category\.[0-9]+(\.[a-z_]+\.[0-9]+)*$`)

// dropExpiredAnalyticsRefs removes analytics refs from a stored tool context in
// place: map entries whose value is such a ref, and list items that are one. The
// names and figures next to them stay, so a follow-up can still refer to them.
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

// compactConversation takes turns oldest first. The newest verbatimExchanges
// user exchanges stay whole. Older turns keep only what a follow-up needs: user
// and assistant text, clipped, with tool results dropped and the turn marked
// Compacted. Memory is then held to conversationCharBudget, counting tool JSON as
// well as text: first the tool results of older verbatim exchanges are dropped,
// oldest first, then the oldest compacted turns. The newest exchange is never
// trimmed.
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
	// Tool results of older verbatim exchanges are the heaviest and least needed.
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

// turnMemorySize is what one replayed turn costs the prompt, in characters:
// its text plus the JSON of any tool result it carries. Characters, not bytes,
// so non-ASCII text is not penalized.
func turnMemorySize(turn PublicTurn) int {
	size := utf8.RuneCountInString(turn.Text)
	if len(turn.Context) > 0 {
		encoded, _ := json.Marshal(turn.Context)
		size += utf8.RuneCount(encoded)
	}
	return size
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
