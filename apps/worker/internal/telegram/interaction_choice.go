package telegram

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const (
	callbackInteractionAnswer = "interaction:answer"
	callbackInteractionNew    = "interaction:new"
	closedCardReply           = "Kartu ini sudah selesai, jadi balasan tadi tidak mengubah apa pun."
	answerOrNewQuestion       = "Jawaban atau catatan baru? Ada kartu yang masih menunggu jawaban, dan pesanmu memuat nominal."
	interactionChoiceStale    = "Pilihan ini sudah tidak berlaku. Kirim ulang pesannya kalau masih perlu, ya."
)

func interactionOverride(parserName string) string {
	switch parserName {
	case interactionParserAnswer, interactionParserNew:
		return parserName
	}
	return ""
}

func syntheticTap(update telegramUpdate, card interactionCard, data string) telegramUpdate {
	tap := update
	tap.CallbackQuery = &telegramCallbackQuery{Data: data}
	tap.CallbackQuery.From.ID = update.Message.From.ID
	tap.CallbackQuery.Message = telegramCallbackMessage{MessageID: card.MessageID, Text: card.DeliveredText}
	tap.CallbackQuery.Message.Chat.ID = update.Message.Chat.ID
	tap.Message.MessageID = card.MessageID
	tap.Message.ReplyToMessage = replyToMessage(card.MessageID)
	tap.Message.Text = callbackText(data)
	return tap
}

func (p *Processor) askAnswerOrNew(ctx context.Context, householdID, sourceEventID string, update telegramUpdate) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name=$2,parser_version='1' WHERE id=$1`, sourceEventID, interactionParserChoice); err != nil {
		return err
	}
	markup := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "Jawaban untuk kartu tadi", CallbackData: callbackInteractionAnswer}},
		{{Text: "Catatan baru", CallbackData: callbackInteractionNew}},
	}}
	if err := enqueueReplyMarkup(ctx, tx, update, answerOrNewQuestion, markup); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) processInteractionChoiceCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) error {
	parser := interactionParserNew
	if data == callbackInteractionAnswer {
		parser = interactionParserAnswer
	}
	chatID := update.CallbackQuery.Message.Chat.ID
	original, err := p.replyThreadParent(ctx, householdID, chatID, update.CallbackQuery.Message.MessageID)
	if err != nil {
		return err
	}
	if original == 0 {
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, interactionChoiceStale)
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var originalEventID string
	err = tx.QueryRow(ctx, `SELECT s.id::text FROM source_event s JOIN source_event_payload pl ON pl.source_event_id=s.id
		WHERE s.household_id=$1 AND s.source_type='TELEGRAM_TEXT' AND s.parser_name=$5 AND s.processing_status='PROCESSED'
		  AND pl.payload_json#>>'{message,chat,id}'=$2::bigint::text AND pl.payload_json#>>'{message,message_id}'=$3::bigint::text AND pl.payload_json#>>'{message,from,id}'=$4::bigint::text
		ORDER BY s.received_at DESC LIMIT 1 FOR UPDATE OF s`, householdID, chatID, original, update.Message.From.ID, interactionParserChoice).Scan(&originalEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, interactionChoiceStale)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='RECEIVED',parser_name=$2 WHERE id=$1`, originalEventID, parser); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('PROCESS_TELEGRAM_TEXT',jsonb_build_object('source_event_id',$1::uuid))`, originalEventID); err != nil {
		return err
	}
	if err := markCallbackProcessed(ctx, tx, sourceEventID); err != nil {
		return err
	}
	if err := enqueueRetireButtons(ctx, tx, update, "Sudah dipilih."); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
