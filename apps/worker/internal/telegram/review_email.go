package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

func (p *Processor) processFinancialEmailPager(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, page int) (bool, error) {
	var itemID string
	err := p.pool.QueryRow(ctx, `SELECT ri.id::text FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN review_item ri ON ri.id=r.review_item_id WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND') AND ri.review_type='FINANCIAL_EMAIL_RESOLUTION'`, householdID, update.Message.Chat.ID, update.Message.MessageID).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, p.askFinancialEmailEntityPage(ctx, sourceEventID, itemID, householdID, update, page)
}

// processFinancialEmailCallback resolves a FINANCIAL_EMAIL_RESOLUTION review
// from the entity chooser. Each button names one dimension (funding account or
// wealth account) and one household-scoped id, so it delegates straight to the
// shared resolver and then enqueues the provider-email replay, exactly as the
// Review Inbox does. Bounded buttons never enter the conversational pipeline.
func (p *Processor) processFinancialEmailCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) (bool, error) {
	dimension, entityID := financialEmailDimension(data)
	if dimension == "" {
		if page := financialEmailPage(data); page >= 0 {
			return p.processFinancialEmailPager(ctx, sourceEventID, householdID, update, page)
		}
		return false, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var itemID, observationID, source, userID string
	err = tx.QueryRow(ctx, `SELECT ri.id::text,ri.financial_email_observation_id::text,fo.source_event_id::text,ti.user_id::text
		FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN review_item ri ON ri.id=r.review_item_id JOIN financial_email_observation fo ON fo.id=ri.financial_email_observation_id
		JOIN telegram_identity ti ON ti.telegram_user_id=$2 AND ti.household_id=r.household_id AND ti.active JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND') AND ri.review_type='FINANCIAL_EMAIL_RESOLUTION' FOR UPDATE OF ri`, householdID, update.Message.Chat.ID, update.Message.MessageID).Scan(&itemID, &observationID, &source, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	cmd := reviewdomain.FinancialEmailCommand{HouseholdID: householdID, ObservationID: observationID, ReviewItemID: itemID, ActorUserID: userID, ActorType: "TELEGRAM", Ignore: dimension == "ignore"}
	if dimension == "account" {
		cmd.AccountID = entityID
	} else if dimension == "wealth" {
		cmd.WealthAccountID = entityID
	}
	result, err := reviewdomain.ResolveFinancialEmailReview(ctx, tx, cmd)
	if errors.Is(err, reviewdomain.ErrAccountInvalid) || errors.Is(err, reviewdomain.ErrWealthAccountInvalid) {
		if err := tx.Rollback(ctx); err != nil {
			return true, err
		}
		return true, p.askFinancialEmailEntity(ctx, sourceEventID, itemID, householdID, update)
	}
	if errors.Is(err, reviewdomain.ErrFinancialObservationUnavailable) {
		return true, finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return true, err
	}
	if !result.Complete {
		if err := tx.Commit(ctx); err != nil {
			return true, err
		}
		return true, p.askFinancialEmailEntity(ctx, sourceEventID, itemID, householdID, update)
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return true, err
	}
	message := "Rekening bukti email disimpan."
	if cmd.Ignore {
		message = "Bukti email finansial diabaikan."
	}
	if err = enqueueReply(ctx, tx, update, message); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

// processDocumentReviewCallback resolves a document-bound review (classification
// or extraction failure) from its Telegram card. The buttons carry bounded
// intents, so it never enters the conversational pipeline: it binds the card by
// its delivered message id, then delegates to the shared document resolver.
func (p *Processor) processDocumentReviewCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) (bool, error) {
	action := map[string]string{"review:reprocess": "REPROCESS_DOCUMENT", "review:ignore": "IGNORE"}[data]
	if action == "" {
		return false, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var itemID, documentID, userID string
	err = tx.QueryRow(ctx, `SELECT ri.id::text,ri.document_id::text,ti.user_id::text
		FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN review_item ri ON ri.id=r.review_item_id JOIN document d ON d.id=ri.document_id
		JOIN telegram_identity ti ON ti.telegram_user_id=$2 AND ti.household_id=r.household_id AND ti.active JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND') AND ri.review_type IN ('DOCUMENT_CLASSIFICATION','DOCUMENT_EXTRACTION_LOW_CONFIDENCE')`, householdID, update.Message.Chat.ID, update.Message.MessageID).Scan(&itemID, &documentID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	_, err = reviewdomain.ResolveDocumentReview(ctx, tx, reviewdomain.DocumentReviewCommand{HouseholdID: householdID, UserID: userID, ReviewItemID: itemID, DocumentID: documentID, ActorType: "TELEGRAM", Action: action})
	if err != nil {
		if errors.Is(err, reviewdomain.ErrDocumentReviewInvalid) {
			return true, finishStaleReviewCallback(ctx, tx, sourceEventID, update)
		}
		return true, err
	}
	if action == "REPROCESS_DOCUMENT" {
		if _, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json) SELECT 'PROCESS_DOCUMENT',jsonb_build_object('document_id',$1::uuid) WHERE NOT EXISTS(SELECT 1 FROM job WHERE type='PROCESS_DOCUMENT' AND payload_json->>'document_id'=$1::text AND status IN ('PENDING','RUNNING'))`, documentID); err != nil {
			return true, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return true, err
	}
	message := "Dokumen akan diproses ulang."
	if action == "IGNORE" {
		message = "Dokumen diabaikan."
	}
	if err = enqueueReply(ctx, tx, update, message); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

func (p *Processor) ignoreFinancialEmailFacts(ctx context.Context, sourceEventID, householdID string, update telegramUpdate) (bool, error) {
	replyID := int64(0)
	if update.Message.ReplyToMessage != nil {
		replyID = update.Message.ReplyToMessage.MessageID
	} else if update.CallbackQuery != nil {
		replyID = update.Message.MessageID
	}
	if replyID == 0 {
		return false, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var itemID, observationID, observationSource string
	// Bind inside the transaction with the open-liveness guards its siblings use
	// (unexpired request, FOR UPDATE) so an expired card cannot still resolve and
	// two concurrent taps cannot both commit.
	// Select the observation's own source_event_id: `sourceEventID` is the
	// Telegram callback event, whose id never matches the observation's
	// source_event_id, so settling it left the provider email stuck at
	// NEEDS_REVIEW.
	err = tx.QueryRow(ctx, `SELECT ri.id::text,ri.financial_email_observation_id::text,fo.source_event_id::text FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN review_item ri ON ri.id=r.review_item_id JOIN financial_email_observation fo ON fo.id=ri.financial_email_observation_id WHERE r.household_id=$1 AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND') AND ri.review_type='FINANCIAL_EMAIL_FACTS' AND ri.financial_email_observation_id IS NOT NULL AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 FOR UPDATE OF ri`, householdID, update.Message.Chat.ID, replyID).Scan(&itemID, &observationID, &observationSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	var userID string
	// Require an active household membership, not just an active Telegram
	// identity, exactly like the sibling resolvers.
	if err = tx.QueryRow(ctx, `SELECT ti.user_id FROM telegram_identity ti JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active WHERE ti.telegram_user_id=$1 AND ti.household_id=$2 AND ti.active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE id=$1 AND household_id=$2 AND status='REVIEW'`, observationID, householdID); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$3,resolution_action='IGNORE',updated_at=now() WHERE id=$1 AND household_id=$2 AND status IN ('OPEN','PENDING_SEND')`, itemID, householdID, userID); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status='OPEN'`, itemID); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1)`, itemID); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'IGNORE_FINANCIAL_EMAIL_FACTS','financial_email_observation',$3,jsonb_build_object('review_item_id',$4::uuid))`, householdID, userID, observationID, itemID); err != nil {
		return true, err
	}
	// Three-branch CASE shared with reviewdomain.ResolveFinancialEmailReview and
	// financialemail's own projection: an email with one APPLIED observation is
	// PROCESSED even after another observation is ignored, so this lane must not
	// stamp IGNORED over canonical state already written.
	// It settles the provider email's own event, not `sourceEventID` (the
	// Telegram callback event); the callback event is settled below like every
	// sibling lane, or EnsureSourceEventFinal fails on a still-RECEIVED callback.
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM financial_email_observation WHERE source_event_id=$1 AND status IN ('PENDING','REVIEW')) THEN 'NEEDS_REVIEW' WHEN EXISTS(SELECT 1 FROM financial_email_observation WHERE source_event_id=$1 AND status='APPLIED') THEN 'PROCESSED' ELSE 'IGNORED' END WHERE id=$1`, observationSource); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return true, err
	}
	if err = enqueueReply(ctx, tx, update, "Bukti email finansial diabaikan."); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

func documentReviewMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "Proses ulang", CallbackData: "review:reprocess"}},
		{{Text: "Abaikan", CallbackData: "review:ignore"}},
	}}
}

// financialEmailEntityMarkup offers the still-unresolved entity dimensions as
// paged choosers over the household's own accounts and wealth accounts. Only the
// dimension the decision names as missing is offered, so a review that already
// knows the funding account asks for the wealth account alone. Callback data is
// prefixed with the dimension so the resolver sets the right field.
// askFinancialEmailEntity keeps an unfinished entity review open and re-emits the
// chooser for the dimension(s) still missing, so a two-dimension resolution can
// finish in two taps without leaving Telegram.
func (p *Processor) askFinancialEmailEntity(ctx context.Context, sourceEventID, itemID, householdID string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var decisionJSON []byte
	var requestID string
	var status string
	if err := tx.QueryRow(ctx, `SELECT decision,status FROM review_item WHERE id=$1 AND household_id=$2 FOR UPDATE`, itemID, householdID).Scan(&decisionJSON, &status); err != nil {
		return err
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM review_request WHERE review_item_id=$1 AND status IN ('OPEN','PENDING_SEND') ORDER BY created_at LIMIT 1`, itemID).Scan(&requestID); err != nil {
		return err
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(decisionJSON, &decision); err != nil {
		return err
	}
	// A dimension resolved on an earlier turn must not be offered again: rebuild
	// the missing set from the observation's persisted columns.
	var resolvedAccount, resolvedWealth string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(fo.resolved_account_id::text,''),COALESCE(fo.resolved_wealth_account_id::text,'') FROM review_item ri JOIN financial_email_observation fo ON fo.id=ri.financial_email_observation_id WHERE ri.id=$1`, itemID).Scan(&resolvedAccount, &resolvedWealth); err != nil {
		return err
	}
	remaining := decision.MissingFacts[:0:0]
	for _, fact := range decision.MissingFacts {
		if (fact == "funding_account" && resolvedAccount != "") || (fact == "wealth_account" && resolvedWealth != "") {
			continue
		}
		remaining = append(remaining, fact)
	}
	decision.MissingFacts = remaining
	if len(remaining) == 0 {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	markup, err := financialEmailEntityMarkup(ctx, tx, requestID, decision, 0)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if err := enqueueReplyMarkup(ctx, tx, update, "Masih ada rekening yang perlu dipilih.", markup); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// financialEmailEntityMarkup builds the entity chooser for the still-missing
// dimensions. page selects the chunk of each dimension, so a household with more
// accounts than fit one Telegram keyboard can still bind the missing entity.
func financialEmailEntityMarkup(ctx context.Context, tx pgx.Tx, reviewID string, decision reviewdec.Decision, page int) (*InlineKeyboardMarkup, error) {
	hasAccount := contains(decision.MissingFacts, "funding_account")
	hasWealth := contains(decision.MissingFacts, "wealth_account")
	// A legacy decision with neither dimension named means both are open.
	if !hasAccount && !hasWealth {
		hasAccount, hasWealth = true, true
	}
	var householdID string
	if err := tx.QueryRow(ctx, `SELECT household_id FROM review_request WHERE id=$1`, reviewID).Scan(&householdID); err != nil {
		return nil, err
	}
	var buttons [][]InlineKeyboardButton
	total, cap := 0, 8
	if hasAccount {
		rows, err := tx.Query(ctx, `SELECT count(*) FROM account WHERE household_id=$1 AND active`, householdID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err := rows.Scan(&total); err != nil {
				rows.Close()
				return nil, err
			}
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT id,name FROM account WHERE household_id=$1 AND active ORDER BY name,id LIMIT $2 OFFSET $3`, householdID, cap, page*cap)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			buttons = append(buttons, []InlineKeyboardButton{{Text: clean(name, 28), CallbackData: "review:fe:account:" + id}})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	wealthTotal, wealthCap := 0, 16
	if hasWealth {
		rows, err := tx.Query(ctx, `SELECT count(*) FROM wealth_account WHERE household_id=$1 AND active`, householdID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err := rows.Scan(&wealthTotal); err != nil {
				rows.Close()
				return nil, err
			}
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT id,name FROM wealth_account WHERE household_id=$1 AND active ORDER BY name,id LIMIT $2 OFFSET $3`, householdID, wealthCap, page*wealthCap)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			buttons = append(buttons, []InlineKeyboardButton{{Text: clean(name, 28), CallbackData: "review:fe:wealth:" + id}})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	lastPage := (total - 1) / cap
	if wealthPage := (wealthTotal - 1) / wealthCap; wealthPage > lastPage {
		lastPage = wealthPage
	}
	if lastPage > 0 {
		var pager []InlineKeyboardButton
		if page > 0 {
			pager = append(pager, InlineKeyboardButton{Text: "‹", CallbackData: fmt.Sprintf("review:fepage:%d", page-1)})
		}
		if page < lastPage {
			pager = append(pager, InlineKeyboardButton{Text: "›", CallbackData: fmt.Sprintf("review:fepage:%d", page+1)})
		}
		if len(pager) > 0 {
			buttons = append(buttons, pager)
		}
	}
	buttons = append(buttons, []InlineKeyboardButton{{Text: "Abaikan", CallbackData: "review:ignore"}})
	return &InlineKeyboardMarkup{InlineKeyboard: buttons}, nil
}

// askFinancialEmailEntityPage re-renders the chooser on a later page without
// touching the observation.
func (p *Processor) askFinancialEmailEntityPage(ctx context.Context, sourceEventID, itemID, householdID string, update telegramUpdate, page int) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var decisionJSON []byte
	var status, reviewID string
	if err := tx.QueryRow(ctx, `SELECT ri.decision,ri.status,rr.id::text FROM review_item ri JOIN review_request rr ON rr.review_item_id=ri.id AND rr.status IN ('OPEN','PENDING_SEND') WHERE ri.id=$1 AND ri.household_id=$2 ORDER BY rr.created_at LIMIT 1 FOR UPDATE OF ri`, itemID, householdID).Scan(&decisionJSON, &status, &reviewID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
		}
		return err
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(decisionJSON, &decision); err != nil {
		return err
	}
	markup, err := financialEmailEntityMarkup(ctx, tx, reviewID, decision, page)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if err := enqueueReplyMarkup(ctx, tx, update, "Masih ada rekening yang perlu dipilih.", markup); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// financialEmailDimension returns the household-scoped entity dimensions a
// financial-email chooser callback refers to: an entity id, or "ignore".
func financialEmailDimension(data string) (string, string) {
	switch {
	case strings.HasPrefix(data, "review:fe:account:"):
		return "account", strings.TrimPrefix(data, "review:fe:account:")
	case strings.HasPrefix(data, "review:fe:wealth:"):
		return "wealth", strings.TrimPrefix(data, "review:fe:wealth:")
	case data == "review:ignore":
		return "ignore", ""
	default:
		return "", ""
	}
}

// financialEmailPage parses review:fepage:<n>; the negative sentinel means the
// callback is not a pager.
func financialEmailPage(data string) int {
	if !strings.HasPrefix(data, "review:fepage:") {
		return -1
	}
	page, err := strconv.Atoi(strings.TrimPrefix(data, "review:fepage:"))
	if err != nil || page < 0 {
		return -1
	}
	return page
}
