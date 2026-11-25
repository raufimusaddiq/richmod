package telegram

import "context"

func (p *Processor) continueProposalAmountReview(ctx context.Context, sourceEventID, householdID, itemID string, update telegramUpdate, message string) error {
	if _, err := p.pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',$2::bigint,'text',$3::text))`, update.Message.Chat.ID, update.Message.MessageID, message)
	return err
}
