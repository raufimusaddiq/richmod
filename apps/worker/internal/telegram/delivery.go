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

func (p *Processor) DeliverSend(ctx context.Context, bot *Bot, jobID, householdID string, payload SendPayload) error {
	messageID, err := p.recordedDelivery(ctx, jobID)
	if err != nil {
		return err
	}
	if messageID == 0 {
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

func (p *Processor) skipReviewDelivery(ctx context.Context, payload SendPayload) error {
	if payload.ReviewRequestID == "" {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
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

func (p *Processor) DeliverEdit(ctx context.Context, bot *Bot, jobID, householdID string, payload EditPayload) error {
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
	if payload.OutcomeRequestID != "" {
		return p.ApplyCardOutcomeEdit(ctx, bot, payload)
	}
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
