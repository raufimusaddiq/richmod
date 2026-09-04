package telegram

import (
	"context"
	"encoding/json"
	"fmt"
)

type PublicTurn struct {
	Role    string         `json:"role"`
	Text    string         `json:"text,omitempty"`
	Tool    string         `json:"tool,omitempty"`
	Context map[string]any `json:"context,omitempty"`
}

func (p *Processor) persistTurn(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, role, text, tool string, public map[string]any) error {
	if p.pool == nil {
		return nil
	}
	encoded, _ := json.Marshal(public)
	_, err := p.pool.Exec(ctx, `INSERT INTO telegram_conversation_turn(household_id,telegram_user_id,telegram_chat_id,source_event_id,role,message_text,tool_name,public_context_json,telegram_message_id) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,NULLIF($6,''),NULLIF($7,''),$8::jsonb,$9)`, householdID, update.Message.From.ID, update.Message.Chat.ID, sourceEventID, role, text, tool, encoded, update.Message.MessageID)
	return err
}

func (p *Processor) recentConversation(ctx context.Context, householdID string, chatID int64, currentSourceID string) ([]PublicTurn, error) {
	rows, err := p.pool.Query(ctx, `SELECT role,COALESCE(message_text,''),COALESCE(tool_name,''),COALESCE(public_context_json,'{}'::jsonb) FROM telegram_conversation_turn WHERE household_id=$1 AND telegram_chat_id=$2 AND source_event_id IS DISTINCT FROM NULLIF($3,'')::uuid AND created_at >= now()-interval '60 minutes' ORDER BY created_at DESC LIMIT 20`, householdID, chatID, currentSourceID)
	if err != nil {
		return nil, fmt.Errorf("load Telegram conversation context: %w", err)
	}
	defer rows.Close()
	var result []PublicTurn
	for rows.Next() {
		var turn PublicTurn
		var raw []byte
		if err := rows.Scan(&turn.Role, &turn.Text, &turn.Tool, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &turn.Context)
		result = append(result, turn)
	}
	return result, rows.Err()
}

func (p *Processor) hasPendingAction(ctx context.Context, householdID string, update telegramUpdate) (bool, error) {
	var found bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM telegram_pending_action WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now())`, householdID, update.Message.From.ID, update.Message.Chat.ID).Scan(&found)
	return found, err
}
func (p *Processor) hasPendingBatch(ctx context.Context, householdID string, update telegramUpdate) (bool, error) {
	var found bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM telegram_pending_batch WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now())`, householdID, update.Message.From.ID, update.Message.Chat.ID).Scan(&found)
	return found, err
}

func (p *Processor) hasPendingSalaryChoice(ctx context.Context, householdID string, update telegramUpdate) (bool, error) {
	var found bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM salary_pending_choice WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now())`, householdID, update.Message.From.ID, update.Message.Chat.ID).Scan(&found)
	return found, err
}

func (p *Processor) hasMerchantLearning(ctx context.Context, householdID string, update telegramUpdate) (bool, error) {
	var found bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id JOIN transaction t ON t.id=r.transaction_id WHERE r.household_id=$1 AND r.status='OPEN' AND c.state='AWAITING_CONFIRMATION' AND t.status='CONFIRMED' AND EXISTS (SELECT 1 FROM review_request_recipient rr WHERE rr.review_request_id=r.id AND rr.telegram_chat_id=$2))`, householdID, update.Message.Chat.ID).Scan(&found)
	return found, err
}
func (p *Processor) activeReviewBinding(ctx context.Context, householdID string, update telegramUpdate) (any, int, error) {
	if update.Message.ReplyToMessage != nil {
		var typ, amount, merchant string
		err := p.pool.QueryRow(ctx, `SELECT r.review_type,t.amount::text,COALESCE(t.counterparty_name,t.description,'') FROM review_request r JOIN transaction t ON t.id=r.transaction_id JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.household_id=$1 AND r.status='OPEN' AND t.status='NEEDS_REVIEW' AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3`, householdID, update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID).Scan(&typ, &amount, &merchant)
		if err == nil {
			return map[string]any{"binding": "reply_to_message_id", "review_type": typ, "amount_idr": amount, "merchant": merchant}, 1, nil
		}
	}
	rows, err := p.pool.Query(ctx, `SELECT r.review_type,t.amount::text,COALESCE(t.counterparty_name,t.description,'') FROM review_request r JOIN transaction t ON t.id=r.transaction_id LEFT JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.household_id=$1 AND r.status='OPEN' AND t.status='NEEDS_REVIEW' AND (rr.telegram_chat_id=$2 OR rr.telegram_chat_id IS NULL) ORDER BY r.created_at DESC LIMIT 2`, householdID, update.Message.Chat.ID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var candidates []map[string]any
	for rows.Next() {
		var typ, amount, merchant string
		if err := rows.Scan(&typ, &amount, &merchant); err != nil {
			return nil, 0, err
		}
		candidates = append(candidates, map[string]any{"review_type": typ, "amount_idr": amount, "merchant": merchant})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(candidates) == 1 {
		return candidates[0], 1, nil
	}
	return candidates, len(candidates), nil
}

func (p *Processor) resolveTransactionReference(ctx context.Context, householdID string, update telegramUpdate, ref string) (string, error) {
	var transactionID string
	err := p.pool.QueryRow(ctx, `SELECT r.entity_id FROM telegram_turn_reference r JOIN telegram_conversation_turn t ON t.id=r.turn_id JOIN transaction x ON x.id=r.entity_id WHERE r.household_id=$1 AND r.telegram_user_id=$2 AND r.telegram_chat_id=$3 AND r.ref_key=$4 AND r.entity_type='TRANSACTION' AND r.expires_at>now() AND x.household_id=$1 AND x.status<>'VOIDED' ORDER BY t.created_at DESC LIMIT 1`, householdID, update.Message.From.ID, update.Message.Chat.ID, ref).Scan(&transactionID)
	return transactionID, err
}

func (p *Processor) persistTransactionReferences(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var turnID string
	err = tx.QueryRow(ctx, `INSERT INTO telegram_conversation_turn(household_id,telegram_user_id,telegram_chat_id,source_event_id,role,message_text,public_context_json,telegram_message_id) VALUES($1,$2,$3,$4::uuid,'ASSISTANT','Search results sent to user.',jsonb_build_object('transaction_refs',$5::jsonb),$6) RETURNING id`, householdID, update.Message.From.ID, update.Message.Chat.ID, sourceEventID, referencePublicJSON(len(ids)), update.Message.MessageID).Scan(&turnID)
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err = tx.Exec(ctx, `INSERT INTO telegram_turn_reference(turn_id,ref_key,entity_type,entity_id,household_id,telegram_user_id,telegram_chat_id,expires_at) VALUES($1,$2,'TRANSACTION',$3::uuid,$4,$5,$6,now()+interval '60 minutes') ON CONFLICT(turn_id,ref_key) DO UPDATE SET entity_id=excluded.entity_id,expires_at=excluded.expires_at`, turnID, fmt.Sprintf("tx_%d", i+1), id, householdID, update.Message.From.ID, update.Message.Chat.ID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func referencePublicJSON(count int) string {
	refs := make([]string, count)
	for i := range refs {
		refs[i] = fmt.Sprintf("tx_%d", i+1)
	}
	b, _ := json.Marshal(refs)
	return string(b)
}
