package telegram

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// persistAgentTransactionReferencesTx persists model-safe transaction refs in
// the same database transaction as the canonical financial mutation. This
// removes the post-commit gap where the ledger could be committed but the
// conversational reference was not durable yet.
func persistAgentTransactionReferencesTx(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, update telegramUpdate, prefix string, ids []string) ([]agentPublicRef, error) {
	if len(ids) == 0 {
		return nil, nil
	}
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
	return public, nil
}
