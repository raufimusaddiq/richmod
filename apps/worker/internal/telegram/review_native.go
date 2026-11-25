package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

type confirmedReview struct {
	HouseholdID, UserID, ReviewID, TransactionID, CategoryID, SourceEventID, MerchantID string
	Confidence                                                                          float64
	OfferLearning                                                                       bool
}

func merchantLearningEligible(ctx context.Context, tx pgx.Tx, householdID, merchantID, categoryID, transactionID string) (bool, error) {
	var eligible bool
	err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM merchant_alias ma WHERE ma.household_id=$1 AND ma.normalized_merchant_id=$2 AND ma.default_category_id=$3 AND ma.auto_apply AND ma.created_from_user_confirmation)
		AND NOT EXISTS(SELECT 1 FROM transaction h WHERE h.household_id=$1 AND h.merchant_id=$2 AND h.id<>$4 AND h.status='CONFIRMED' AND h.category_id IS NOT NULL AND h.category_id<>$3
			AND (h.auto_confirmed_at IS NULL OR EXISTS(SELECT 1 FROM product_telemetry_event e WHERE e.household_id=$1 AND e.transaction_id=h.id AND e.event_type='AUTO_CONFIRM_CORRECTION' AND 'category'=ANY(e.changed_fields))))`, householdID, merchantID, categoryID, transactionID).Scan(&eligible)
	return eligible, err
}

func finishConfirmedReview(ctx context.Context, tx pgx.Tx, in confirmedReview) (bool, error) {
	if err := reviewdomain.RefreshTransactionLifecycle(ctx, tx, in.HouseholdID, in.TransactionID); err != nil {
		return false, err
	}
	askRemember := false
	if in.OfferLearning && in.MerchantID != "" && in.CategoryID != "" {
		var err error
		if askRemember, err = merchantLearningEligible(ctx, tx, in.HouseholdID, in.MerchantID, in.CategoryID, in.TransactionID); err != nil {
			return false, err
		}
	}
	if err := resolveCanonicalReviewItem(ctx, tx, in.ReviewID, in.UserID, "TELEGRAM_CONFIRMED"); err != nil {
		return false, err
	}
	state := "RESOLVED"
	if askRemember {
		state = "AWAITING_MERCHANT_DECISION"
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state=$3,context_json=context_json||jsonb_build_object('category_id',NULLIF($2,'')::uuid),last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, in.ReviewID, in.CategoryID, state); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, in.SourceEventID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence (transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES ($2,$1,'TELEGRAM_REVIEW_REPLY',$3,jsonb_build_object('review_request_id',$4::uuid,'classification','CONFIRM')) ON CONFLICT DO NOTHING`, in.SourceEventID, in.TransactionID, in.Confidence, in.ReviewID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES ($1,'TELEGRAM',$2,'RESOLVE_REVIEW','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'category_id',NULLIF($5,'')::uuid,'merchant_id',(SELECT merchant_id FROM transaction WHERE id=$3)))`, in.HouseholdID, in.UserID, in.TransactionID, in.ReviewID, in.CategoryID); err != nil {
		return false, err
	}
	return askRemember, nil
}

type cardOutcome struct {
	Category string
	Merchant string
	Facts    string
}

func (o cardOutcome) text(note string) string {
	title := "✅ Dicatat"
	if o.Category != "" {
		title += ": " + o.Category
	}
	return reviewdomain.TelegramCardText(title, o.Facts, "", note)
}

func confirmedCardOutcome(ctx context.Context, tx pgx.Tx, householdID, transactionID string) (cardOutcome, error) {
	var outcome cardOutcome
	if err := tx.QueryRow(ctx, `SELECT COALESCE(c.name,''),COALESCE(m.normalized_name,'') FROM transaction t LEFT JOIN category c ON c.id=t.category_id AND c.household_id=t.household_id LEFT JOIN merchant m ON m.id=t.merchant_id AND m.household_id=t.household_id WHERE t.id=$1 AND t.household_id=$2`, transactionID, householdID).Scan(&outcome.Category, &outcome.Merchant); err != nil {
		return outcome, err
	}
	facts, err := transactionReviewSummary(ctx, tx, householdID, transactionID)
	if err != nil {
		return outcome, err
	}
	outcome.Facts = facts
	return outcome, nil
}

func settleReviewCardInPlace(ctx context.Context, tx pgx.Tx, reviewID string, chatID, messageID int64, text string, markup *InlineKeyboardMarkup) error {
	if chatID == 0 || messageID == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE job SET status='SUCCEEDED',finished_at=now(),updated_at=now(),payload_json=payload_json||'{"superseded_by":"card_outcome"}'::jsonb WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND status='PENDING' AND payload_json->>'review_request_id'=$1 AND (payload_json->>'chat_id')::bigint=$2 AND (payload_json->>'message_id')::bigint=$3`, reviewID, chatID, messageID); err != nil {
		return err
	}
	if markup == nil {
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
	}
	encoded, err := json.Marshal(markup)
	if err != nil {
		return err
	}
	var revision int
	if err := tx.QueryRow(ctx, `UPDATE review_conversation SET context_json=jsonb_set(context_json,'{card_outcomes}',COALESCE(context_json->'card_outcomes','{}'::jsonb)||jsonb_build_object($2::text,COALESCE((context_json->'card_outcomes'->>$2)::int,0)+1)),updated_at=now() WHERE review_request_id=$1 RETURNING (context_json->'card_outcomes'->>$2)::int`, reviewID, cardOutcomeKey(chatID, messageID)).Scan(&revision); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO job(type,lane,payload_json) VALUES('EDIT_TELEGRAM_MESSAGE','INTERACTIVE',jsonb_build_object('chat_id',$1::bigint,'message_id',$2::bigint,'text',$3::text,'reply_markup',$4::jsonb,'outcome_request_id',$5::text,'outcome_revision',$6::int))`, chatID, messageID, clean(text, 4000), string(encoded), reviewID, revision)
	return err
}

func cardOutcomeKey(chatID, messageID int64) string {
	return fmt.Sprintf("%d:%d", chatID, messageID)
}

func (p *Processor) ApplyCardOutcomeEdit(ctx context.Context, bot *Bot, payload EditPayload) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var current int
	err = tx.QueryRow(ctx, `SELECT COALESCE((context_json->'card_outcomes'->>$2)::int,0) FROM review_conversation WHERE review_request_id=$1 FOR UPDATE`, payload.OutcomeRequestID, cardOutcomeKey(payload.ChatID, payload.MessageID)).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if current != payload.OutcomeRevision {
		return nil
	}
	var dead bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_request_recipient rr WHERE rr.review_request_id=$1 AND rr.telegram_chat_id=$2 AND rr.delivery_status='EXPIRED')
		OR EXISTS(SELECT 1 FROM review_request_message m WHERE m.review_request_id=$1 AND m.telegram_chat_id=$2 AND m.telegram_message_id=$3 AND m.superseded_at IS NOT NULL)`, payload.OutcomeRequestID, payload.ChatID, payload.MessageID).Scan(&dead); err != nil {
		return err
	}
	if dead {
		payload.ReplyMarkup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
	}
	switch err := bot.Edit(ctx, payload); {
	case err == nil:
	case editNotModified(err), editFinished(err):
		return tx.Commit(ctx)
	case editUnreachable(err):
		slog.Default().Info("Telegram outcome edit dropped: chat unreachable", "review_request_id", payload.OutcomeRequestID, "error", err)
		return tx.Commit(ctx)
	case editRejected(err):
		return TerminalError(err)
	default:
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := p.RecordReviewCardText(ctx, payload.ChatID, payload.MessageID, payload.Text); err != nil {
		slog.Default().Warn("record settled Telegram review card text failed", "review_request_id", payload.OutcomeRequestID, "error", err)
	}
	return nil
}

func (p *Processor) applyMerchantLearningChoice(ctx context.Context, sourceEventID, householdID, reviewID, transactionID string, update telegramUpdate, remember bool) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return err
	}
	if err = commitMerchantLearning(ctx, tx, householdID, userID, reviewID, transactionID, sourceEventID, remember); err != nil {
		return err
	}
	message := "Kategori ini hanya dipakai untuk transaksi tadi, tidak disimpan sebagai aturan merchant."
	if remember {
		message = "✅ Kategori merchant sudah disimpan untuk transaksi berikutnya. Aturannya bisa dinonaktifkan di Settings."
	}
	outcome, err := confirmedCardOutcome(ctx, tx, householdID, transactionID)
	if err != nil {
		return err
	}
	note := "Kategori hanya dipakai untuk transaksi ini."
	if remember {
		note = "⭐ Diingat untuk " + outcome.Merchant + ". Bisa dinonaktifkan di Settings."
	}
	var cardChat, cardMessage int64
	_ = tx.QueryRow(ctx, `SELECT COALESCE((context_json->'learning_card'->>'chat_id')::bigint,0),COALESCE((context_json->'learning_card'->>'message_id')::bigint,0) FROM review_conversation WHERE review_request_id=$1`, reviewID).Scan(&cardChat, &cardMessage)
	if update.CallbackQuery != nil {
		if err = settleReviewCardInPlace(ctx, tx, reviewID, update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID, outcome.text(note), nil); err != nil {
			return err
		}
		if cardMessage != 0 && (cardChat != update.CallbackQuery.Message.Chat.ID || cardMessage != update.CallbackQuery.Message.MessageID) {
			if err = settleReviewCardInPlace(ctx, tx, reviewID, cardChat, cardMessage, outcome.text(note), nil); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	if cardMessage != 0 {
		if err = settleReviewCardInPlace(ctx, tx, reviewID, cardChat, cardMessage, outcome.text(note), nil); err != nil {
			return err
		}
	}
	if err = enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func commitMerchantLearning(ctx context.Context, tx pgx.Tx, householdID, userID, reviewID, transactionID, sourceEventID string, remember bool) error {
	var err error
	if remember {
		if err = rememberMerchantAlias(ctx, tx, householdID, userID, reviewID, transactionID); err != nil {
			return err
		}
	}
	if err = resolveCanonicalReviewItem(ctx, tx, reviewID, userID, "TELEGRAM_MERCHANT_DECISION"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($3,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$1::uuid,'remember_merchant',$4::boolean)) ON CONFLICT DO NOTHING`, reviewID, sourceEventID, transactionID, remember); err != nil {
		return err
	}
	return nil
}

func ReviewQuestion(amount, merchant string) string {
	facts := "Nominal: Rp" + FormatIDR(amount)
	if merchant = strings.TrimSpace(merchant); merchant != "" {
		facts += "\nMerchant: " + merchant
	}
	return facts
}

func FormatIDR(value string) string {
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign, value = "-", strings.TrimPrefix(value, "-")
	}
	if len(value) <= 3 {
		return sign + value
	}
	first := len(value) % 3
	if first == 0 {
		first = 3
	}
	parts := []string{value[:first]}
	for index := first; index < len(value); index += 3 {
		parts = append(parts, value[index:index+3])
	}
	return sign + strings.Join(parts, ".")
}

func rememberMerchantAlias(ctx context.Context, tx pgx.Tx, householdID, userID, reviewID, transactionID string) error {
	var merchantID, categoryID string
	if err := tx.QueryRow(ctx, `SELECT merchant_id,category_id FROM transaction WHERE id=$1 AND household_id=$2 AND status='CONFIRMED' AND merchant_id IS NOT NULL AND category_id IS NOT NULL`, transactionID, householdID).Scan(&merchantID, &categoryID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) SELECT $1,normalized_name,id,$3,true,true FROM merchant WHERE id=$2 AND household_id=$1 ON CONFLICT(household_id,lower(regexp_replace(btrim(raw_name), '[[:space:]]+', ' ', 'g'))) DO UPDATE SET raw_name=excluded.raw_name,default_category_id=excluded.default_category_id,auto_apply=true,created_from_user_confirmation=true`, householdID, merchantID, categoryID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'REMEMBER_MERCHANT','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'merchant_id',$5::uuid,'category_id',$6::uuid))`, householdID, userID, transactionID, reviewID, merchantID, categoryID)
	return err
}
