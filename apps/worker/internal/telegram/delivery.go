package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// DeliverSend runs one SEND_TELEGRAM_MESSAGE job. Telegram has no idempotency
// key, so the message id Telegram returns is recorded against the job before
// any binding: a retry after a failed bind binds that same message instead of
// sending a second, unbound card. A queued message is dropped unsent when the
// recipient lost access or the review card it carries is no longer open; a card
// recipient dropped that way is marked SKIPPED and frees its pacing slot.
func (p *Processor) DeliverSend(ctx context.Context, bot *Bot, jobID, householdID string, payload SendPayload) error {
	messageID, err := p.recordedDelivery(ctx, jobID)
	if err != nil {
		return err
	}
	if messageID == 0 {
		// The recipient may have been removed from the household (or the user
		// disabled) after this message was queued.
		authorized, err := p.RecipientAuthorized(ctx, payload.ChatID, householdID)
		if err != nil {
			return err
		}
		if !authorized {
			slog.Default().Info("queued Telegram message dropped: recipient no longer authorized", "job_id", jobID)
			if err := p.skipReviewDelivery(ctx, payload); err != nil {
				return err
			}
			return answerLegacyCallback(ctx, bot, payload)
		}
		// A review card resolved (or cancelled/expired) between enqueue and send
		// must not arrive as a fresh live card.
		if payload.ReviewRequestID != "" {
			open, err := p.ReviewProjectionOpen(ctx, payload.ReviewRequestID)
			if err != nil {
				return err
			}
			if !open {
				if err := p.skipReviewDelivery(ctx, payload); err != nil {
					return err
				}
				return answerLegacyCallback(ctx, bot, payload)
			}
		}
		if messageID, err = bot.Send(ctx, payload); err != nil {
			return err
		}
		if err := p.recordDelivery(ctx, jobID, householdID, payload.ChatID, messageID); err != nil {
			return err
		}
	}
	if payload.ReviewRequestID != "" {
		if err := p.BindReviewMessage(ctx, payload.ReviewRequestID, payload.ChatID, messageID, payload.Text); err != nil {
			return err
		}
	}
	if payload.BindMerchantLearningRequestID != "" {
		if err := p.BindMerchantLearningMessage(ctx, payload.BindMerchantLearningRequestID, payload.ChatID, messageID); err != nil {
			return err
		}
	}
	if payload.BindDocumentID != "" {
		if err := p.BindEvidenceMessage(ctx, payload.ChatID, messageID, payload.BindDocumentID); err != nil {
			return err
		}
	}
	return answerLegacyCallback(ctx, bot, payload)
}

// answerLegacyCallback answers a callback id carried by a send payload queued
// before callbacks were acknowledged at the webhook. Nothing enqueues one now.
func answerLegacyCallback(ctx context.Context, bot *Bot, payload SendPayload) error {
	if payload.CallbackQueryID == "" {
		return nil
	}
	if err := bot.AnswerCallback(ctx, payload.CallbackQueryID); err != nil {
		slog.Default().Warn("legacy Telegram callback answer failed", "error", err)
	}
	return nil
}

func (p *Processor) recordedDelivery(ctx context.Context, jobID string) (int64, error) {
	if jobID == "" {
		return 0, nil
	}
	var messageID int64
	err := p.pool.QueryRow(ctx, `SELECT telegram_message_id FROM telegram_outbound_delivery WHERE job_id=$1`, jobID).Scan(&messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load recorded Telegram delivery: %w", err)
	}
	return messageID, nil
}

func (p *Processor) recordDelivery(ctx context.Context, jobID, householdID string, chatID, messageID int64) error {
	if jobID == "" {
		return nil
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO telegram_outbound_delivery(job_id,household_id,telegram_chat_id,telegram_message_id) VALUES($1,NULLIF($2,'')::uuid,$3,$4) ON CONFLICT (job_id) DO NOTHING`, jobID, householdID, chatID, messageID); err != nil {
		return fmt.Errorf("record Telegram delivery: %w", err)
	}
	return nil
}

// skipReviewDelivery marks an undelivered card recipient SKIPPED and releases
// the next paced card for that chat.
func (p *Processor) skipReviewDelivery(ctx context.Context, payload SendPayload) error {
	if payload.ReviewRequestID == "" {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE review_request_recipient SET delivery_status='SKIPPED' WHERE review_request_id=$1 AND telegram_chat_id=$2 AND delivery_status='PENDING' AND telegram_message_id IS NULL`, payload.ReviewRequestID, payload.ChatID); err != nil {
		return fmt.Errorf("skip Telegram review card: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT release_review_cards($1)`, payload.ChatID); err != nil {
		return fmt.Errorf("release paced Telegram review cards: %w", err)
	}
	return tx.Commit(ctx)
}

// ReviewDeliveryFailedTx runs in the transaction that marks a send job FAILED
// for good. A review card that never reached its recipient becomes FAILED with
// the redacted Telegram error, so the household diagnostics show it, and its
// pacing slot is released. The canonical review_item is untouched: it stays
// actionable in the web Review Inbox. A failed follow-up of a delivered card
// changes nothing, because that recipient already has its card.
func ReviewDeliveryFailedTx(ctx context.Context, tx pgx.Tx, payload SendPayload, cause error) error {
	if payload.ReviewRequestID == "" || payload.ChatID == 0 {
		return nil
	}
	reason := "send failed"
	if cause != nil {
		reason = clean(cause.Error(), 500)
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request_recipient SET delivery_status='FAILED',delivery_error=$3,delivery_failed_at=now()
		WHERE review_request_id=$1 AND telegram_chat_id=$2 AND delivery_status='PENDING' AND telegram_message_id IS NULL`, payload.ReviewRequestID, payload.ChatID, reason); err != nil {
		return fmt.Errorf("record failed Telegram review card: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT release_review_cards($1)`, payload.ChatID); err != nil {
		return fmt.Errorf("release paced Telegram review cards: %w", err)
	}
	return nil
}

// DeliverEdit runs one EDIT_TELEGRAM_MESSAGE job. "Message is not modified" (a
// double tap, a repeated edit) is a success. A chat the bot cannot reach is
// finished. Rate limits, server errors and transport failures are returned for
// the queue to retry. When Telegram can no longer edit a live review card, the
// card is sent again with its buttons and bound like any card, replacing the
// old message; a plain edit (button retirement of a confirmation) whose message
// is gone has nothing left to repair. Nothing is ever resent without its
// buttons or binding.
func (p *Processor) DeliverEdit(ctx context.Context, bot *Bot, jobID, householdID string, payload EditPayload) error {
	authorized, err := p.RecipientAuthorized(ctx, payload.ChatID, householdID)
	if err != nil {
		return err
	}
	if !authorized {
		slog.Default().Info("queued Telegram edit dropped: recipient no longer authorized", "job_id", jobID)
		return nil
	}
	// An edit of a review card queued while it was open must not restore its
	// buttons after the request closed; RETIRE_TELEGRAM_REVIEW_CARD owns it now.
	if payload.ReviewRequestID != "" {
		live, err := p.ReviewCardLive(ctx, payload.ReviewRequestID)
		if err != nil {
			return err
		}
		if !live {
			return nil
		}
	}
	// A retry after the card was already re-sent only redoes that binding.
	if sent, err := p.recordedDelivery(ctx, jobID); err != nil {
		return err
	} else if sent != 0 {
		return p.DeliverSend(ctx, bot, jobID, householdID, SendPayload{ChatID: payload.ChatID, Text: payload.Text, ReplyMarkup: payload.ReplyMarkup, ReviewRequestID: payload.ReviewRequestID})
	}
	err = bot.Edit(ctx, payload)
	switch {
	case err == nil:
		// The edit is delivered; a failure to remember its text only costs a
		// later retirement its closure note, so it must not retry the edit.
		if err := p.RecordReviewCardText(ctx, payload.ChatID, payload.MessageID, payload.Text); err != nil {
			slog.Default().Warn("record edited Telegram review card text failed", "job_id", jobID, "error", err)
		}
		return nil
	case editNotModified(err):
		return nil
	case editUnreachable(err):
		slog.Default().Info("Telegram edit dropped: chat unreachable", "job_id", jobID, "error", err)
		return nil
	case !editRejected(err):
		return err
	case payload.ReviewRequestID == "":
		if editFinished(err) {
			return nil
		}
		return TerminalError(err)
	}
	slog.Default().Info("Telegram review card could not be edited; sending it again", "job_id", jobID, "error", err)
	return p.DeliverSend(ctx, bot, jobID, householdID, SendPayload{ChatID: payload.ChatID, Text: payload.Text, ReplyMarkup: payload.ReplyMarkup, ReviewRequestID: payload.ReviewRequestID})
}

// SweepReviewProjections expires Telegram projections past their window and
// keeps paced cards moving. It is idempotent and safe to run on every
// maintenance tick. Expiring a request only closes the Telegram projection: the
// review_request trigger queues retirement of every card (buttons removed, a
// note that the review is still in the Review Inbox) and releases the chat's
// next queued card; the canonical review_item stays open on the web. A request
// whose only cards are still waiting in the pacing queue is not expired: its
// window starts when a card is shown. A copy still queued when the others
// expire stays queued, and its release renews the projection. Finally every chat with a queued card and
// a free slot is released, which repairs a release a crash or renewal missed.
func (p *Processor) SweepReviewProjections(ctx context.Context, limit int) (expired, released int, err error) {
	tag, err := p.pool.Exec(ctx, `UPDATE review_request r SET status='EXPIRED'
		WHERE r.id IN (
			SELECT x.id FROM review_request x
			WHERE x.status IN ('PENDING_SEND','OPEN') AND x.expires_at<=now()
			  AND NOT (EXISTS(SELECT 1 FROM review_request_recipient q WHERE q.review_request_id=x.id AND q.delivery_status='QUEUED')
			           AND NOT EXISTS(SELECT 1 FROM review_request_recipient s WHERE s.review_request_id=x.id AND s.delivery_status IN ('PENDING','SENT')))
			ORDER BY x.expires_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED)
		AND r.status IN ('PENDING_SEND','OPEN')`, limit)
	if err != nil {
		return 0, 0, fmt.Errorf("expire Telegram review projections: %w", err)
	}
	expired = int(tag.RowsAffected())
	rows, err := p.pool.Query(ctx, `SELECT DISTINCT rr.telegram_chat_id FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id
		WHERE rr.delivery_status='QUEUED' AND r.status IN ('PENDING_SEND','OPEN') LIMIT $1`, limit)
	if err != nil {
		return expired, 0, fmt.Errorf("list paced Telegram review chats: %w", err)
	}
	var chats []int64
	for rows.Next() {
		var chat int64
		if err := rows.Scan(&chat); err != nil {
			rows.Close()
			return expired, 0, err
		}
		chats = append(chats, chat)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return expired, 0, err
	}
	for _, chat := range chats {
		var count int
		if err := p.pool.QueryRow(ctx, `SELECT release_review_cards($1)`, chat).Scan(&count); err != nil {
			return expired, released, fmt.Errorf("release paced Telegram review cards: %w", err)
		}
		released += count
	}
	return expired, released, nil
}
