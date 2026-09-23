package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type agentContextState struct {
	Conversation        []PublicTurn
	HasPendingAction    bool
	PendingAction       map[string]any
	HasPendingBatch     bool
	PendingBatch        []map[string]any
	HasSalaryChoice     bool
	HasMerchantLearning bool
	ActiveReview        any
	ActiveReviewCount   int
	ReviewType          string
	ReviewMode          string
	// RecentTransactions gives the model a structured view of what it just
	// recorded, so a follow-up ("ibu kantin") can bind to the pending row and a
	// re-sent line can be recognized as the same event instead of a second row.
	RecentTransactions []map[string]any
}

func (p *Processor) loadAgentContextState(ctx context.Context, householdID, sourceEventID string, update telegramUpdate) (agentContextState, error) {
	var state agentContextState
	conversation, err := p.recentConversation(ctx, householdID, update.Message.Chat.ID, sourceEventID)
	if err != nil {
		return state, err
	}
	state.Conversation = conversation

	pendingAction, err := p.loadAgentPendingAction(ctx, householdID, update)
	if err != nil {
		return state, err
	}
	if pendingAction != nil {
		state.HasPendingAction = true
		state.PendingAction = pendingAction
	}

	pendingBatch, err := p.loadAgentPendingBatch(ctx, householdID, update)
	if err != nil {
		return state, err
	}
	if pendingBatch != nil {
		state.HasPendingBatch = true
		state.PendingBatch = pendingBatch
	}

	state.HasSalaryChoice, err = p.hasPendingSalaryChoice(ctx, householdID, update)
	if err != nil {
		return state, err
	}
	state.RecentTransactions, err = p.recentAgentTransactions(ctx, householdID, sourceEventID, update)
	if err != nil {
		return state, err
	}
	return state, nil
}

// recentAgentTransactions lists the household's most recent ledger rows as
// opaque turn refs, mirroring the ref scheme used for freshly recorded
// transactions. Only model-safe fields are exposed (no canonical UUIDs), so the
// model can reference an existing row but never address the database directly.
func (p *Processor) recentAgentTransactions(ctx context.Context, householdID, sourceEventID string, update telegramUpdate) ([]map[string]any, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT t.id,t.type,t.status,t.amount::text,COALESCE(t.counterparty_name,''),
		       COALESCE(c.slug,''),COALESCE(t.description,''),t.transaction_at
		FROM transaction t
		LEFT JOIN category c ON c.id=t.category_id
		WHERE t.household_id=$1 AND t.status<>'VOIDED' AND t.created_at >= now()-interval '60 minutes'
		ORDER BY t.created_at DESC LIMIT 5`, householdID)
	if err != nil {
		return nil, fmt.Errorf("load recent agent transactions: %w", err)
	}
	defer rows.Close()
	type recent struct {
		ID, Type, Status, Amount, Merchant, CategorySlug, Description string
		At                                                           time.Time
	}
	var loaded []recent
	for rows.Next() {
		var r recent
		if err := rows.Scan(&r.ID, &r.Type, &r.Status, &r.Amount, &r.Merchant, &r.CategorySlug, &r.Description, &r.At); err != nil {
			return nil, err
		}
		loaded = append(loaded, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(loaded) == 0 {
		return nil, nil
	}
	ids := make([]string, len(loaded))
	for i, r := range loaded {
		ids[i] = r.ID
	}
	// The ref_key column constrains keys to the tx phase pattern, so recent rows
	// use the phase-0 shape ("p0r0") rather than a descriptive prefix.
	refs, err := p.persistAgentTransactionReferences(ctx, householdID, sourceEventID, update, "p0r0", ids)
	if err != nil {
		return nil, fmt.Errorf("persist recent transaction references: %w", err)
	}
	out := make([]map[string]any, 0, len(loaded))
	for i, r := range loaded {
		out = append(out, map[string]any{
			"ref":            refs[i].Ref,
			"type":           r.Type,
			"status":         r.Status,
			"amount_idr":     r.Amount,
			// Merchant and description are user-controlled ledger text, so they are
			// wrapped in the same untrusted-data boundary as the current message to
			// keep stored free text from reading as agent instructions.
			"merchant":       untrustedField(r.Merchant),
			"category_slug":  r.CategorySlug,
			"description":    untrustedField(r.Description),
			"transaction_at": r.At.In(jakartaLocation()).Format(time.RFC3339),
		})
	}
	return out, nil
}

// untrustedField delimits a user-controlled string so the model treats it as
// data, mirroring the <untrusted_user_message> boundary used for user text.
func untrustedField(value string) string {
	return "<untrusted_ledger_text>" + value + "</untrusted_ledger_text>"
}

func (p *Processor) loadAgentPendingAction(ctx context.Context, householdID string, update telegramUpdate) (map[string]any, error) {
	var label, amount string
	var proposedAt *time.Time
	var categoryName, description *string
	err := p.pool.QueryRow(ctx, `
		SELECT COALESCE(t.counterparty_name,t.description,'Transaksi'),t.amount::text,
		       a.proposed_transaction_at,c.name,a.proposed_description
		FROM telegram_pending_action a
		JOIN transaction t ON t.id=a.transaction_id AND t.household_id=a.household_id
		LEFT JOIN category c ON c.id=a.proposed_category_id
		WHERE a.household_id=$1 AND a.telegram_user_id=$2 AND a.telegram_chat_id=$3
		  AND a.status='PENDING' AND a.expires_at>now()
		LIMIT 1`, householdID, update.Message.From.ID, update.Message.Chat.ID).Scan(&label, &amount, &proposedAt, &categoryName, &description)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load agent pending action: %w", err)
	}
	out := map[string]any{"kind": "TRANSACTION_CORRECTION", "label": label, "amount_idr": amount}
	if proposedAt != nil {
		out["proposed_transaction_at"] = proposedAt.In(jakartaLocation()).Format(time.RFC3339)
	}
	if categoryName != nil && strings.TrimSpace(*categoryName) != "" {
		out["proposed_category"] = *categoryName
	}
	if description != nil && strings.TrimSpace(*description) != "" {
		out["proposed_description"] = *description
	}
	return out, nil
}

func (p *Processor) loadAgentPendingBatch(ctx context.Context, householdID string, update telegramUpdate) ([]map[string]any, error) {
	var raw string
	err := p.pool.QueryRow(ctx, `SELECT items_json::text FROM telegram_pending_batch WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now() LIMIT 1`, householdID, update.Message.From.ID, update.Message.Chat.ID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load agent pending batch: %w", err)
	}
	var items []agentPendingBatchItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("decode pending batch context: %w", err)
	}
	result := make([]map[string]any, 0, len(items))
	for index, item := range items {
		result = append(result, map[string]any{
			"item_ref":       fmt.Sprintf("batch_%d", index+1),
			"type":           item.Type,
			"amount_idr":     item.Amount,
			"merchant":       item.Merchant,
			"category_slug":  item.CategorySlug,
			"description":    item.Description,
			"transaction_at": item.TransactionAt.In(jakartaLocation()).Format(time.RFC3339),
		})
	}
	return result, nil
}

func buildAgentTurnContext(text string, now time.Time, categories []string, contextState agentContextState) map[string]any {
	return map[string]any{
		"current_user_text":        "<untrusted_user_message>" + text + "</untrusted_user_message>",
		"current_jakarta_datetime": now.In(jakartaLocation()).Format(time.RFC3339),
		"recent_turns":             contextState.Conversation,
		"recent_transactions":      contextState.RecentTransactions,
		"allowed_category_slugs":   categories,
		"pending_action":           contextState.PendingAction,
		"pending_batch":            contextState.PendingBatch,
		"active_review_count":      contextState.ActiveReviewCount,
		"active_review":            contextState.ActiveReview,
		"has_salary_choice":        contextState.HasSalaryChoice,
		"has_merchant_learning":    contextState.HasMerchantLearning,
	}
}

func agentScopedRefPrefix(sourceEventID, prefix string) string {
	sum := sha256.Sum256([]byte(sourceEventID))
	return fmt.Sprintf("a%x_%s", sum[:4], prefix)
}

// persistAgentTransactionReferences stores opaque refs without exposing UUIDs
// to the model. The source-event hash scopes refs across user turns while the
// phase/read suffix keeps parallel reads collision-free inside one turn.
func (p *Processor) persistAgentTransactionReferences(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, prefix string, ids []string) ([]agentPublicRef, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	prefix = agentScopedRefPrefix(sourceEventID, prefix)
	refs := make([]string, len(ids))
	for index := range ids {
		refs[index] = fmt.Sprintf("%s_tx%d", prefix, index+1)
	}
	encoded, _ := json.Marshal(refs)
	var turnID string
	if err := tx.QueryRow(ctx, `INSERT INTO telegram_conversation_turn(household_id,telegram_user_id,telegram_chat_id,source_event_id,role,message_text,tool_name,public_context_json,telegram_message_id) VALUES($1,$2,$3,$4::uuid,'TOOL','Agent transaction references.','agent_transaction_refs',jsonb_build_object('transaction_refs',$5::jsonb),$6) RETURNING id`, householdID, update.Message.From.ID, update.Message.Chat.ID, sourceEventID, string(encoded), update.Message.MessageID).Scan(&turnID); err != nil {
		return nil, err
	}
	public := make([]agentPublicRef, 0, len(ids))
	for index, id := range ids {
		ref := refs[index]
		if _, err := tx.Exec(ctx, `INSERT INTO telegram_turn_reference(turn_id,ref_key,entity_type,entity_id,household_id,telegram_user_id,telegram_chat_id,expires_at) VALUES($1,$2,'TRANSACTION',$3::uuid,$4,$5,$6,now()+interval '60 minutes') ON CONFLICT(turn_id,ref_key) DO UPDATE SET entity_id=excluded.entity_id,expires_at=excluded.expires_at`, turnID, ref, id, householdID, update.Message.From.ID, update.Message.Chat.ID); err != nil {
			return nil, err
		}
		public = append(public, agentPublicRef{Ref: ref, Type: "TRANSACTION"})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return public, nil
}

func agentOptionalString(value any) *string {
	var raw string
	switch typed := value.(type) {
	case string:
		raw = typed
	case *string:
		if typed == nil {
			return nil
		}
		raw = *typed
	default:
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return &raw
}
