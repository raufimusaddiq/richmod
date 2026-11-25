package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

const (
	ubahWindow     = 10 * time.Minute
	ubahExpiredAck = "Sudah lewat 10 menit. Ubah di aplikasi."
	ubahGoneAck    = "Transaksi ini sudah tidak bisa diubah di sini. Ubah di aplikasi."

	callbackUbah         = "review:ubah"
	callbackUbahCategory = "review:ubah:cat"
	callbackUbahDate     = "review:ubah:date"
	callbackUbahRemember = "review:ubah:remember"
	callbackUbahBack     = "review:ubah:back"
	callbackUbahDone     = "review:ubah:done"

	interactionUbahReopened = "UBAH_REOPENED"
)

var ubahReviewTypes = map[string]bool{
	"UNKNOWN_MERCHANT":          true,
	"AMBIGUOUS_CATEGORY":        true,
	"UNKNOWN_PURPOSE":           true,
	"MISSING_TRANSACTION_DATE":  true,
	"TRANSACTION_FACTS_MISSING": true,
	transferReviewType:          true,
}

type ubahState struct {
	ChatID    int64     `json:"chat_id"`
	MessageID int64     `json:"message_id"`
	Until     time.Time `json:"until"`
	UserID    string    `json:"user_id"`
	Mode      string    `json:"mode"`
	Closed    bool      `json:"closed,omitempty"`
}

type ubahOffer struct {
	TransactionID string
	Category      bool
	Date          bool
	Remember      bool
}

func ubahDoneMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ubah", CallbackData: callbackUbah}}}}
}

func (p *Processor) ubahOffer(ctx context.Context, tx pgx.Tx, householdID, reviewRequestID string) (ubahOffer, bool, error) {
	var offer ubahOffer
	var reviewType, status, kind, categoryID, merchantID string
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT ri.review_type,COALESCE(ri.decision,'{}'::jsonb),t.id::text,t.status,t.type,COALESCE(t.category_id::text,''),COALESCE(t.merchant_id::text,'')
		FROM review_request r
		JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		JOIN transaction t ON t.id=r.transaction_id AND t.household_id=r.household_id
		WHERE r.id=$1 AND r.household_id=$2`, reviewRequestID, householdID).Scan(&reviewType, &raw, &offer.TransactionID, &status, &kind, &categoryID, &merchantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return offer, false, nil
	}
	if err != nil {
		return offer, false, fmt.Errorf("load Ubah offer: %w", err)
	}
	if !ubahReviewTypes[reviewType] || status != "CONFIRMED" || (kind != "EXPENSE" && kind != "INCOME") || categoryID == "" {
		return offer, false, nil
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return offer, false, fmt.Errorf("decode Ubah decision: %w", err)
	}
	offer.Category = true
	offer.Date = (reviewType == "MISSING_TRANSACTION_DATE" || reviewType == "TRANSACTION_FACTS_MISSING") && contains(decision.MissingFacts, "transaction_at")
	if merchantID != "" && kind == "EXPENSE" && reviewType != transferReviewType {
		if offer.Remember, err = merchantLearningEligible(ctx, tx, householdID, merchantID, categoryID, offer.TransactionID); err != nil {
			return offer, false, err
		}
	}
	return offer, true, nil
}

func armUbah(ctx context.Context, tx jobExecer, reviewID string, chatID, messageID int64, userID string, until time.Time) error {
	state, err := json.Marshal(ubahState{ChatID: chatID, MessageID: messageID, Until: until.UTC(), UserID: userID, Mode: "done"})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json||jsonb_build_object('ubah',$2::jsonb),updated_at=now() WHERE review_request_id=$1`, reviewID, string(state)); err != nil {
		return fmt.Errorf("arm Ubah window: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json,run_after) VALUES('EXPIRE_UBAH_BUTTON',
		jsonb_build_object('chat_id',$1::bigint,'message_id',$2::bigint,'review_request_id',$3::text),$4)`, chatID, messageID, reviewID, until.UTC()); err != nil {
		return fmt.Errorf("schedule Ubah expiry: %w", err)
	}
	return nil
}

func (p *Processor) armUbahFromSend(ctx context.Context, payload SendPayload, messageID int64) error {
	until, err := time.Parse(time.RFC3339, payload.UbahUntil)
	if err != nil {
		return nil
	}
	var userID string
	err = p.pool.QueryRow(ctx, `SELECT ti.user_id::text FROM telegram_identity ti JOIN review_request r ON r.household_id=ti.household_id
		WHERE ti.telegram_user_id=$1 AND ti.active AND r.id=$2`, payload.ChatID, payload.ReviewRequestID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load Ubah owner: %w", err)
	}
	return armUbah(ctx, p.pool, payload.ReviewRequestID, payload.ChatID, messageID, userID, until)
}

func (p *Processor) ubahDoneText(ctx context.Context, tx pgx.Tx, householdID, transactionID, note string) (string, error) {
	outcome, err := confirmedCardOutcome(ctx, tx, householdID, transactionID)
	if err != nil {
		return "", err
	}
	amount, err := transactionAmount(ctx, tx, householdID, transactionID)
	if err != nil {
		return "", err
	}
	return doneStep(outcome.Merchant, outcome.Category, amount, note).Text(), nil
}

type UbahExpirePayload struct {
	ReviewRequestID string `json:"review_request_id"`
	ChatID          int64  `json:"chat_id"`
	MessageID       int64  `json:"message_id"`
}

func DecodeUbahExpirePayload(raw json.RawMessage) (UbahExpirePayload, error) {
	var payload UbahExpirePayload
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return UbahExpirePayload{}, fmt.Errorf("decode Ubah expiry payload: %w", err)
	}
	if payload.ReviewRequestID == "" || payload.ChatID == 0 || payload.MessageID == 0 {
		return UbahExpirePayload{}, fmt.Errorf("invalid Ubah expiry payload")
	}
	return payload, nil
}

func (p *Processor) ExpireUbahButton(ctx context.Context, payload UbahExpirePayload) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	var householdID, transactionID string
	err = tx.QueryRow(ctx, `SELECT c.context_json->'ubah',r.household_id::text,COALESCE(r.transaction_id::text,'')
		FROM review_conversation c JOIN review_request r ON r.id=c.review_request_id
		WHERE c.review_request_id=$1 FOR UPDATE OF c`, payload.ReviewRequestID).Scan(&raw, &householdID, &transactionID)
	if errors.Is(err, pgx.ErrNoRows) || len(raw) == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	var state ubahState
	if err := json.Unmarshal(raw, &state); err != nil || state.Closed || state.MessageID != payload.MessageID || state.ChatID != payload.ChatID {
		return nil
	}
	return p.closeUbah(ctx, tx, householdID, payload.ReviewRequestID, transactionID, state, nil)
}

func (p *Processor) closeUbah(ctx context.Context, tx pgx.Tx, householdID, reviewID, transactionID string, state ubahState, then func() error) error {
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=jsonb_set(context_json,'{ubah,closed}','true'::jsonb),updated_at=now() WHERE review_request_id=$1`, reviewID); err != nil {
		return err
	}
	if transactionID != "" {
		text, err := p.ubahDoneText(ctx, tx, householdID, transactionID, "")
		if err == nil {
			if err := settleReviewCardInPlace(ctx, tx, reviewID, state.ChatID, state.MessageID, text, nil); err != nil {
				return err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if then != nil {
		if err := then(); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func ubahChooserMarkup(offer ubahOffer) [][]InlineKeyboardButton {
	buttons := []InlineKeyboardButton{{Text: "Kategori", CallbackData: callbackUbahCategory}}
	if offer.Date {
		buttons = append(buttons, InlineKeyboardButton{Text: "Tanggal", CallbackData: callbackUbahDate})
	}
	if offer.Remember {
		buttons = append(buttons, InlineKeyboardButton{Text: "Ingat merchant", CallbackData: callbackUbahRemember})
	}
	return append(pairRows(buttons), []InlineKeyboardButton{{Text: "Selesai", CallbackData: callbackUbahDone}})
}

func (p *Processor) processUbahTap(ctx context.Context, sourceEventID, householdID string, update telegramUpdate) (bool, error) {
	query := update.CallbackQuery
	if query == nil || query.Message.MessageID == 0 {
		return false, nil
	}
	chatID, messageID, data := query.Message.Chat.ID, query.Message.MessageID, query.Data
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var reviewID, itemID, transactionID string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT r.id::text,ri.id::text,COALESCE(r.transaction_id::text,''),c.context_json->'ubah'
		FROM review_request_message m
		JOIN review_request r ON r.id=m.review_request_id AND r.household_id=$1
		JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		JOIN review_conversation c ON c.review_request_id=r.id
		WHERE m.telegram_chat_id=$2 AND m.telegram_message_id=$3
		  AND c.context_json->'ubah'->>'chat_id'=($2::bigint)::text AND c.context_json->'ubah'->>'message_id'=($3::bigint)::text
		FOR UPDATE OF c`, householdID, chatID, messageID).Scan(&reviewID, &itemID, &transactionID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var state ubahState
	if err := json.Unmarshal(raw, &state); err != nil {
		return false, nil
	}
	if !ubahOwnsTap(data) {
		return false, nil
	}
	finish := func(reply string) (bool, error) {
		if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-ubah',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
			return true, err
		}
		if reply != "" {
			if err := enqueueReply(ctx, tx, update, reply); err != nil {
				return true, err
			}
		}
		return true, tx.Commit(ctx)
	}
	var userID string
	if err := tx.QueryRow(ctx, `SELECT user_id::text FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, query.From.ID, householdID).Scan(&userID); err != nil || userID != state.UserID {
		return finish("")
	}
	if state.Closed || !p.now().Before(state.Until) {
		if state.Closed {
			return finish(ubahExpiredAck)
		}
		return true, p.closeUbah(ctx, tx, householdID, reviewID, transactionID, state, func() error {
			if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-ubah',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
				return err
			}
			return enqueueReply(ctx, tx, update, ubahExpiredAck)
		})
	}
	offer, offered, err := p.ubahOffer(ctx, tx, householdID, reviewID)
	if err != nil {
		return true, err
	}
	if !offered {
		return finish(ubahGoneAck)
	}
	show := func(mode, text string, rows [][]InlineKeyboardButton) (bool, error) {
		if _, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=jsonb_set(context_json,'{ubah,mode}',to_jsonb($2::text)),updated_at=now() WHERE review_request_id=$1`, reviewID, mode); err != nil {
			return true, err
		}
		if err := settleReviewCardInPlace(ctx, tx, reviewID, chatID, messageID, text, &InlineKeyboardMarkup{InlineKeyboard: rows}); err != nil {
			return true, err
		}
		return finish("")
	}
	chooser := func() (bool, error) {
		outcome, err := confirmedCardOutcome(ctx, tx, householdID, offer.TransactionID)
		if err != nil {
			return true, err
		}
		facts := outcome.Facts
		if outcome.Category != "" {
			facts += "\nKategori: " + outcome.Category
		}
		return show("chooser", reviewdomain.TelegramCardText("Mau ubah apa?", facts, "", ""), ubahChooserMarkup(offer))
	}
	doneLine := func(note string) (bool, error) {
		text, err := p.ubahDoneText(ctx, tx, householdID, offer.TransactionID, note)
		if err != nil {
			return true, err
		}
		return show("done", text, ubahDoneMarkup().InlineKeyboard)
	}
	back := []InlineKeyboardButton{{Text: "Kembali", CallbackData: callbackUbahBack}}
	categoryPickerRows := func(page int) (bool, error) {
		chooserPage, err := loadCategoryPage(ctx, tx, reviewID, page)
		if err != nil {
			return true, err
		}
		if page > 0 && len(chooserPage.Items) == 0 {
			if chooserPage, err = loadCategoryPage(ctx, tx, reviewID, 0); err != nil {
				return true, err
			}
		}
		rows := append(chooserPage.keyboard(), back)
		return show("category", reviewdomain.TelegramCardText("Pilih kategori baru.", "", "", categoryHint), rows)
	}
	correct := func(cmd reviewdomain.CorrectionCommand, note string) (bool, error) {
		cmd.HouseholdID, cmd.ActorUserID, cmd.ActorType, cmd.TransactionID, cmd.ReviewRequestID = householdID, userID, "TELEGRAM", offer.TransactionID, reviewID
		sp, err := tx.Begin(ctx)
		if err != nil {
			return true, err
		}
		err = reviewdomain.CorrectConfirmedTransaction(ctx, sp, cmd)
		switch {
		case errors.Is(err, reviewdomain.ErrInvalidCandidate):
			_ = sp.Rollback(ctx)
			return finish(categoryInactiveAck)
		case errors.Is(err, reviewdomain.ErrCorrectionNotAllowed):
			_ = sp.Rollback(ctx)
			return finish(ubahGoneAck)
		case err != nil:
			_ = sp.Rollback(ctx)
			return true, err
		}
		if err := sp.Commit(ctx); err != nil {
			return true, err
		}
		return doneLine(note)
	}
	switch {
	case data == callbackUbah || data == callbackUbahBack:
		if data == callbackUbah && state.Mode == "done" {
			defer p.recordInteraction(ctx, householdID, sourceEventID, itemID, interactionUbahReopened, "")
		}
		return chooser()
	case data == callbackUbahDone:
		return doneLine("")
	case data == callbackUbahCategory && offer.Category:
		return categoryPickerRows(0)
	case strings.HasPrefix(data, callbackCategoryPg) && state.Mode == "category":
		page, ok := parsePageCallback(data, callbackCategoryPg)
		if !ok {
			return finish(choiceStaleAck)
		}
		return categoryPickerRows(page)
	case strings.HasPrefix(data, callbackCategory) && state.Mode == "category":
		return correct(reviewdomain.CorrectionCommand{CategoryID: strings.TrimPrefix(data, callbackCategory)}, "")
	case data == callbackUbahDate && offer.Date:
		return show("date", reviewdomain.TelegramCardText("Transaksi ini terjadi tanggal berapa?", "", "", ""),
			[][]InlineKeyboardButton{{{Text: "Hari ini", CallbackData: callbackDateToday}, {Text: "Kemarin", CallbackData: callbackDateYesterday}}, back})
	case (data == callbackDateToday || data == callbackDateYesterday) && state.Mode == "date" && offer.Date:
		at := jakartaMidnight(p.now())
		if data == callbackDateYesterday {
			at = at.AddDate(0, 0, -1)
		}
		return correct(reviewdomain.CorrectionCommand{TransactionAt: &at}, "")
	case data == callbackUbahRemember && offer.Remember:
		return p.ubahRemember(ctx, tx, householdID, userID, reviewID, offer, doneLine)
	}
	return finish(choiceStaleAck)
}

func (p *Processor) ubahRemember(ctx context.Context, tx pgx.Tx, householdID, userID, reviewID string, offer ubahOffer, done func(note string) (bool, error)) (bool, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return true, err
	}
	if err := rememberMerchantAlias(ctx, sp, householdID, userID, reviewID, offer.TransactionID); err != nil {
		_ = sp.Rollback(ctx)
		return true, err
	}
	if err := sp.Commit(ctx); err != nil {
		return true, err
	}
	outcome, err := confirmedCardOutcome(ctx, tx, householdID, offer.TransactionID)
	if err != nil {
		return true, err
	}
	return done("⭐ Diingat untuk " + outcome.Merchant + ". Bisa dinonaktifkan di Settings.")
}

func ubahOwnsTap(data string) bool {
	return strings.HasPrefix(data, callbackUbah) ||
		strings.HasPrefix(data, callbackCategory) || strings.HasPrefix(data, callbackCategoryPg) ||
		data == callbackDateToday || data == callbackDateYesterday
}
