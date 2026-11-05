package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
			// The message is out but could not be remembered: a retry would send
			// a second card. Strip this one's buttons (best effort) and stop.
			if payload.ReplyMarkup != nil {
				if stripErr := bot.EditReplyMarkup(ctx, payload.ChatID, messageID); stripErr != nil {
					slog.Default().Warn("strip unrecorded Telegram message failed", "job_id", jobID, "error", stripErr)
				}
			}
			return TerminalError(err)
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

// recordDelivery remembers the message Telegram accepted. It is retried a few
// times in place because a lost record is the one gap left between Telegram
// and the database: without it a job retry would send the card again.
func (p *Processor) recordDelivery(ctx context.Context, jobID, householdID string, chatID, messageID int64) error {
	if jobID == "" {
		return nil
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("record Telegram delivery: %w", ctx.Err())
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}
		if _, err = p.pool.Exec(ctx, `INSERT INTO telegram_outbound_delivery(job_id,household_id,telegram_chat_id,telegram_message_id) VALUES($1,NULLIF($2,'')::uuid,$3,$4) ON CONFLICT (job_id) DO NOTHING`, jobID, householdID, chatID, messageID); err == nil {
			return nil
		}
	}
	return fmt.Errorf("record Telegram delivery: %w", err)
}

// reviewCopyEditable reports whether a queued edit of a review message may
// still be applied: the request must be active, and the message's copy must
// not be EXPIRED nor the message superseded by a newer one. Otherwise the
// message belongs to retirement, and an edit carrying its old keyboard must
// not bring the buttons back. A message that is not a recorded review message
// follows the request alone.
func (p *Processor) reviewCopyEditable(ctx context.Context, reviewRequestID string, chatID, messageID int64) (bool, error) {
	var editable bool
	err := p.pool.QueryRow(ctx, `SELECT r.status IN ('PENDING_SEND','OPEN')
		  AND NOT EXISTS(SELECT 1 FROM review_request_recipient rr WHERE rr.review_request_id=r.id AND rr.telegram_chat_id=$2 AND rr.delivery_status='EXPIRED')
		  AND NOT EXISTS(SELECT 1 FROM review_request_message m WHERE m.review_request_id=r.id AND m.telegram_chat_id=$2 AND m.telegram_message_id=$3 AND m.superseded_at IS NOT NULL)
		FROM review_request r WHERE r.id=$1`, reviewRequestID, chatID, messageID).Scan(&editable)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load edited Telegram review card: %w", err)
	}
	return editable, nil
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
	// The chat's pacing lock comes before any recipient row (migration 00094).
	if _, err := tx.Exec(ctx, `SELECT lock_review_card_chat($1)`, payload.ChatID); err != nil {
		return err
	}
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
// pacing slot is released. A card Telegram did deliver (its message id is
// recorded for the job) but that could not be bound is not a failed delivery:
// it is bound here from the recorded message, keeps its slot, and is retired
// with the rest of the review. The canonical review_item is untouched either
// way: it stays actionable in the web Review Inbox. A failed follow-up of a
// delivered card changes nothing, because that recipient already has its card.
func ReviewDeliveryFailedTx(ctx context.Context, tx pgx.Tx, jobID string, payload SendPayload, cause error) error {
	if payload.ReviewRequestID == "" || payload.ChatID == 0 {
		return nil
	}
	reason := "send failed"
	if cause != nil {
		reason = clean(cause.Error(), 500)
	}
	if _, err := tx.Exec(ctx, `SELECT lock_review_card_chat($1)`, payload.ChatID); err != nil {
		return err
	}
	var recipientID, householdID string
	var messageID int64
	err := tx.QueryRow(ctx, `SELECT rr.id::text,r.household_id::text,d.telegram_message_id
		FROM telegram_outbound_delivery d
		JOIN review_request_recipient rr ON rr.review_request_id=$2 AND rr.telegram_chat_id=d.telegram_chat_id
		JOIN review_request r ON r.id=rr.review_request_id
		WHERE d.job_id=NULLIF($1,'')::uuid AND d.telegram_chat_id=$3 AND rr.delivery_status='PENDING' AND rr.telegram_message_id IS NULL`,
		jobID, payload.ReviewRequestID, payload.ChatID).Scan(&recipientID, &householdID, &messageID)
	switch {
	case err == nil:
		if _, err := tx.Exec(ctx, `UPDATE review_request_recipient SET telegram_message_id=$2,delivered_text=NULLIF($3,''),delivery_status='SENT',
			card_expires_at=COALESCE(card_expires_at,now()+interval '7 days'),delivery_error=$4 WHERE id=$1`, recipientID, messageID, clean(payload.Text, 4000), "bind failed: "+reason); err != nil {
			return fmt.Errorf("bind delivered Telegram review card: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO review_request_message(household_id,review_request_id,recipient_id,telegram_chat_id,telegram_message_id,delivered_text)
			VALUES($1,$2,$3,$4,$5,NULLIF($6,'')) ON CONFLICT (telegram_chat_id,telegram_message_id) DO NOTHING`,
			householdID, payload.ReviewRequestID, recipientID, payload.ChatID, messageID, clean(payload.Text, 4000)); err != nil {
			return fmt.Errorf("record delivered Telegram review card: %w", err)
		}
		// A card that arrived after its request closed must still lose its buttons.
		if _, err := tx.Exec(ctx, `SELECT enqueue_review_card_retirement($1::uuid,$2::uuid)`, payload.ReviewRequestID, recipientID); err != nil {
			return fmt.Errorf("retire delivered Telegram review card: %w", err)
		}
		return nil
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("load delivered Telegram review card: %w", err)
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

// repairOrphanedReviewCards closes PENDING copies whose send job no longer
// exists (pruned, or a final failure step that could not run): such a copy
// would hold a pacing slot forever. It becomes FAILED and the chat is released.
// Admission and release insert the job in the same transaction that marks the
// copy PENDING, so a PENDING copy without a live or finished-unbound job is
// never mid-admission.
func (p *Processor) repairOrphanedReviewCards(ctx context.Context, limit int) (int, error) {
	rows, err := p.pool.Query(ctx, `SELECT rr.id::text,rr.telegram_chat_id FROM review_request_recipient rr
		WHERE rr.delivery_status='PENDING' AND rr.telegram_message_id IS NULL
		  AND NOT EXISTS(SELECT 1 FROM job j WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.status IN ('PENDING','RUNNING')
		                 AND j.payload_json->>'review_request_id'=rr.review_request_id::text AND j.payload_json->>'chat_id'=rr.telegram_chat_id::text)
		LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	type orphan struct {
		id   string
		chat int64
	}
	var orphans []orphan
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.id, &o.chat); err != nil {
			rows.Close()
			return 0, err
		}
		orphans = append(orphans, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	repaired := 0
	for _, o := range orphans {
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return repaired, err
		}
		_, err = tx.Exec(ctx, `SELECT lock_review_card_chat($1)`, o.chat)
		var tag pgconn.CommandTag
		if err == nil {
			tag, err = tx.Exec(ctx, `UPDATE review_request_recipient rr SET delivery_status='FAILED',delivery_error='send job missing',delivery_failed_at=now()
				WHERE rr.id=$1 AND rr.delivery_status='PENDING' AND rr.telegram_message_id IS NULL
				  AND NOT EXISTS(SELECT 1 FROM job j WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.status IN ('PENDING','RUNNING')
				                 AND j.payload_json->>'review_request_id'=rr.review_request_id::text AND j.payload_json->>'chat_id'=rr.telegram_chat_id::text)`, o.id)
		}
		if err == nil && tag.RowsAffected() == 1 {
			_, err = tx.Exec(ctx, `SELECT release_review_cards($1)`, o.chat)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		tx.Rollback(ctx)
		if err != nil {
			return repaired, fmt.Errorf("repair orphaned Telegram review card: %w", err)
		}
		repaired += int(tag.RowsAffected())
	}
	return repaired, nil
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
	// A retry after the card was already re-sent only redoes that binding, and
	// does it before any guard can drop the job: the re-sent message must be
	// bound (and retired by the binding if its copy or request closed since).
	if sent, err := p.recordedDelivery(ctx, jobID); err != nil {
		return err
	} else if sent != 0 {
		return p.DeliverSend(ctx, bot, jobID, householdID, SendPayload{ChatID: payload.ChatID, Text: payload.Text, ReplyMarkup: payload.ReplyMarkup, ReviewRequestID: payload.ReviewRequestID})
	}
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
		live, err := p.reviewCopyEditable(ctx, payload.ReviewRequestID, payload.ChatID, payload.MessageID)
		if err != nil {
			return err
		}
		if !live {
			return nil
		}
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

// SweepReviewProjections expires review card copies past their own seven-day
// window and keeps paced cards moving. It is idempotent and safe to run on
// every maintenance tick. Expiry is per copy: an expired copy loses its buttons
// (a note says the review is still in the Review Inbox) and frees its slot,
// while other members' copies keep their own windows and a copy still waiting
// in the pacing queue keeps waiting. The request closes as EXPIRED only when no
// copy is live or waiting; a request that never had a Telegram copy expires at
// its own expires_at. The canonical review_item stays open on the web either
// way. Finally every chat with a releasable queued card is released, which
// repairs a release that a crash, a skipped lock, or a renewal missed.
func (p *Processor) SweepReviewProjections(ctx context.Context, limit int) (expired, released int, err error) {
	due, err := p.collectIDs(ctx, `SELECT rr.id::text FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id
		WHERE rr.delivery_status='SENT' AND r.status IN ('PENDING_SEND','OPEN') AND COALESCE(rr.card_expires_at,r.expires_at)<=now()
		ORDER BY COALESCE(rr.card_expires_at,r.expires_at),rr.id LIMIT $1`, limit)
	if err != nil {
		return 0, 0, fmt.Errorf("list expired Telegram review cards: %w", err)
	}
	for _, recipientID := range due {
		var done bool
		if err := p.pool.QueryRow(ctx, `SELECT expire_review_card_recipient($1::uuid)`, recipientID).Scan(&done); err != nil {
			return expired, 0, fmt.Errorf("expire Telegram review card: %w", err)
		}
		if done {
			expired++
		}
	}
	tag, err := p.pool.Exec(ctx, `UPDATE review_request r SET status='EXPIRED'
		WHERE r.id IN (
			SELECT x.id FROM review_request x
			WHERE x.status IN ('PENDING_SEND','OPEN') AND x.expires_at<=now()
			  AND NOT EXISTS(SELECT 1 FROM review_request_recipient q WHERE q.review_request_id=x.id AND q.delivery_status IN ('QUEUED','PENDING','SENT'))
			ORDER BY x.expires_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED)
		AND r.status IN ('PENDING_SEND','OPEN')`, limit)
	if err != nil {
		return expired, 0, fmt.Errorf("expire Telegram review projections: %w", err)
	}
	expired += int(tag.RowsAffected())
	if _, err := p.repairOrphanedReviewCards(ctx, limit); err != nil {
		return expired, 0, err
	}
	chats, err := p.collectIDs(ctx, `SELECT DISTINCT rr.telegram_chat_id::text FROM review_request_recipient rr
		JOIN review_request r ON r.id=rr.review_request_id JOIN review_item ri ON ri.id=r.review_item_id
		WHERE rr.delivery_status='QUEUED' AND rr.queued_payload IS NOT NULL
		  AND r.status IN ('PENDING_SEND','OPEN','EXPIRED') AND ri.status IN ('PENDING_SEND','OPEN') LIMIT $1`, limit)
	if err != nil {
		return expired, 0, fmt.Errorf("list paced Telegram review chats: %w", err)
	}
	for _, chat := range chats {
		var count int
		if err := p.pool.QueryRow(ctx, `SELECT release_review_cards($1::bigint)`, chat).Scan(&count); err != nil {
			return expired, released, fmt.Errorf("release paced Telegram review cards: %w", err)
		}
		released += count
	}
	return expired, released, nil
}

func (p *Processor) collectIDs(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
