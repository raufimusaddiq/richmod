package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

const positionLineRoom = 24

func positionLine(live, queued int, cardLive bool) string {
	if !cardLive || live != 1 || queued == 0 {
		return ""
	}
	return fmt.Sprintf("1 dari %d", live+queued)
}

func withPositionLine(text, line string) string {
	if line == "" {
		return text
	}
	if runes := []rune(text); len(runes) > 4000-positionLineRoom {
		text = string(runes[:4000-positionLineRoom])
	}
	return text + "\n\n" + line
}

func (p *Processor) chatCardCounts(ctx context.Context, chatID int64, reviewRequestID string) (live, queued int, cardLive bool, err error) {
	err = p.pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id
			 WHERE rr.telegram_chat_id=$1 AND rr.delivery_status IN ('PENDING','SENT') AND r.status IN ('PENDING_SEND','OPEN'))::int,
			(SELECT count(*) FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id JOIN review_item ri ON ri.id=r.review_item_id
			 WHERE rr.telegram_chat_id=$1 AND rr.delivery_status='QUEUED' AND rr.queued_payload IS NOT NULL
			   AND r.status IN ('PENDING_SEND','OPEN','EXPIRED') AND ri.status IN ('PENDING_SEND','OPEN'))::int,
			EXISTS(SELECT 1 FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id
			 WHERE rr.telegram_chat_id=$1 AND rr.review_request_id=$2::uuid AND rr.delivery_status IN ('PENDING','SENT') AND r.status IN ('PENDING_SEND','OPEN'))`,
		chatID, reviewRequestID).Scan(&live, &queued, &cardLive)
	return live, queued, cardLive, err
}

func (p *Processor) cardPositionLine(ctx context.Context, chatID int64, reviewRequestID string) (string, error) {
	if reviewRequestID == "" {
		return "", nil
	}
	live, queued, cardLive, err := p.chatCardCounts(ctx, chatID, reviewRequestID)
	if err != nil {
		return "", fmt.Errorf("count chat review cards: %w", err)
	}
	return positionLine(live, queued, cardLive), nil
}

func (p *Processor) recordReviewCardMarkup(ctx context.Context, chatID, messageID int64, markup *InlineKeyboardMarkup) {
	var encoded []byte
	if markup != nil && len(markup.InlineKeyboard) > 0 {
		var err error
		if encoded, err = json.Marshal(markup); err != nil {
			return
		}
	}
	if _, err := p.pool.Exec(ctx, `UPDATE review_request_message SET delivered_markup=$3::jsonb WHERE telegram_chat_id=$1 AND telegram_message_id=$2`, chatID, messageID, nullableJSON(encoded)); err != nil {
		slog.Default().Warn("record Telegram review card markup failed", "error", err)
	}
}

func nullableJSON(encoded []byte) any {
	if len(encoded) == 0 {
		return nil
	}
	return string(encoded)
}

func (p *Processor) refreshCardPosition(ctx context.Context, bot *Bot, payload EditPayload) error {
	var text string
	var rawMarkup []byte
	var settled bool
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(m.delivered_text,''),m.delivered_markup,
			COALESCE(c.context_json->'card_outcomes' ? ($2::bigint::text||':'||$3::bigint::text),false)
		FROM review_request_message m
		JOIN review_request r ON r.id=m.review_request_id
		JOIN review_request_recipient rr ON rr.id=m.recipient_id AND rr.telegram_message_id=m.telegram_message_id AND rr.delivery_status='SENT'
		LEFT JOIN review_conversation c ON c.review_request_id=r.id
		WHERE m.review_request_id=$1 AND m.telegram_chat_id=$2 AND m.telegram_message_id=$3 AND m.superseded_at IS NULL
		  AND r.status IN ('PENDING_SEND','OPEN')`, payload.ReviewRequestID, payload.ChatID, payload.MessageID).Scan(&text, &rawMarkup, &settled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load Telegram review card for position refresh: %w", err)
	}
	if text == "" || settled {
		return nil
	}
	line, err := p.cardPositionLine(ctx, payload.ChatID, payload.ReviewRequestID)
	if err != nil {
		return err
	}
	edit := EditPayload{ChatID: payload.ChatID, MessageID: payload.MessageID, Text: withPositionLine(text, line)}
	if len(rawMarkup) > 0 {
		var markup InlineKeyboardMarkup
		if err := json.Unmarshal(rawMarkup, &markup); err == nil {
			edit.ReplyMarkup = &markup
		}
	}
	switch err := bot.Edit(ctx, edit); {
	case err == nil, editNotModified(err), editFinished(err):
		return nil
	case editUnreachable(err):
		return nil
	case editRejected(err):
		return TerminalError(err)
	default:
		return err
	}
}

func (p *Processor) refreshPositionIfStale(ctx context.Context, chatID int64, reviewRequestID, sentLine string) error {
	line, err := p.cardPositionLine(ctx, chatID, reviewRequestID)
	if err != nil || line == sentLine {
		return err
	}
	if _, err := p.pool.Exec(ctx, `SELECT enqueue_review_position_refresh($1)`, chatID); err != nil {
		return fmt.Errorf("refresh Telegram review card position: %w", err)
	}
	return nil
}
