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
