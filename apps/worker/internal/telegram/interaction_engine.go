package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

const (
	engineParser         = "telegram-step-engine"
	CardAnswerJobBudget  = 45 * time.Second
	unusableAnswerAck    = "Maaf, aku belum menangkap itu."
	categoryInactiveAck  = "Kategori itu sudah tidak aktif."
	mergeTargetActiveAck = "Transaksi ini masih jadi tujuan penggabungan, jadi belum bisa diabaikan."
)

type engineInput struct {
	Tap          string
	Text         string
	CategorySlug string
	Targeted     bool
}

type engineReview struct {
	RequestID     string
	ItemID        string
	TransactionID string
	ReviewType    string
	State         string
	RequestStatus string
	ItemStatus    string
	TxStatus      string
	TxType        string
	MerchantID    string
	UserID        string
	Unexpired     bool
	Decision      reviewdec.Decision
	Subject       reviewSubject
}

type stepOutcome struct {
	Step     Step
	Settled  bool
	Changed  bool
	Declined bool
	Silent   bool
}

func (e engineReview) live() bool {
	open := e.RequestStatus == "OPEN" && e.Unexpired && (e.ItemStatus == "OPEN" || e.ItemStatus == "PENDING_SEND")
	return open && (e.TxStatus == "NEEDS_REVIEW" || (e.TransactionID == "" && e.Subject.Reconciliation))
}

func (e engineReview) rememberPending() bool {
	return e.State == "AWAITING_MERCHANT_DECISION" && e.TxStatus == "CONFIRMED" && engineReviewType(e.ReviewType)
}

func loadEngineReview(ctx context.Context, tx pgx.Tx, householdID, reviewRequestID string, telegramUserID int64, lock bool) (engineReview, error) {
	lockClause := ""
	if lock {
		lockClause = "FOR UPDATE OF r,ri"
	}
	var e engineReview
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT r.id::text,ri.id::text,COALESCE(r.transaction_id::text,''),r.review_type,COALESCE(c.state,''),r.status,ri.status,
			COALESCE(t.status,''),COALESCE(t.type,''),COALESCE(t.merchant_id::text,''),COALESCE(ri.decision,'{}'::jsonb),ti.user_id::text,r.expires_at>now()
		FROM review_request r
		JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		JOIN telegram_identity ti ON ti.telegram_user_id=$3 AND ti.household_id=r.household_id AND ti.active
		LEFT JOIN transaction t ON t.id=r.transaction_id AND t.household_id=r.household_id
		LEFT JOIN review_conversation c ON c.review_request_id=r.id
		WHERE r.id=$1 AND r.household_id=$2
		`+lockClause, reviewRequestID, householdID, telegramUserID).
		Scan(&e.RequestID, &e.ItemID, &e.TransactionID, &e.ReviewType, &e.State, &e.RequestStatus, &e.ItemStatus, &e.TxStatus, &e.TxType, &e.MerchantID, &raw, &e.UserID, &e.Unexpired)
	if err != nil {
		return e, err
	}
	if err := json.Unmarshal(raw, &e.Decision); err != nil {
		return e, err
	}
	if e.Subject, err = loadReviewSubject(ctx, tx, e.RequestID); err != nil {
		return e, err
	}
	return e, nil
}

func (p *Processor) currentStep(ctx context.Context, tx pgx.Tx, householdID string, e engineReview) (Step, bool, error) {
	switch {
	case e.rememberPending():
		outcome, err := confirmedCardOutcome(ctx, tx, householdID, e.TransactionID)
		if err != nil {
			return Step{}, false, err
		}
		return rememberStep(outcome.Facts, outcome.Merchant, outcome.Category), true, nil
	case !e.live():
		return Step{}, false, nil
	}
	facts, err := reviewItemSummary(ctx, tx, householdID, e.ItemID)
	if err != nil {
		return Step{}, false, err
	}
	step, ok := deriveStep(e.Decision, e.ReviewType, facts, e.Subject)
	if !ok {
		return Step{}, false, nil
	}
	switch e.State {
	case "AWAITING_DETAIL":
		if step.Fact == factIncome {
			step = incomeChoiceStep(step)
		}
	case "AWAITING_ASSET_WEALTH":
		step = accountStepFrom(step, factAsset, step.Fact != factAsset && step.Fact != factInvest)
	case "AWAITING_INVESTMENT_ACCOUNT":
		if transferReview(e.ReviewType) {
			step = accountStepFrom(step, factInvest, step.Fact != factAsset && step.Fact != factInvest)
		}
	case "AWAITING_CATEGORY":
		if transferReview(e.ReviewType) {
			step = categoryStepFrom(step, step.Fact == factTransfer)
		}
	}
	if err := step.loadRows(ctx, tx, e.RequestID, 0); err != nil {
		return Step{}, false, err
	}
	return step.executable(cardActions(e.Decision, e.ReviewType)), true, nil
}

func engineNavigation(data string) bool {
	for _, prefix := range []string{callbackCategoryPg, callbackAccountPg, callbackInvestPg} {
		if strings.HasPrefix(data, prefix) {
			return true
		}
	}
	return false
}

func (p *Processor) runStepEngine(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, card interactionCard, in engineInput) (bool, error) {
	if card.ReviewRequestID == "" || !engineOwnedType(card.ReviewType) || (navigationCallback(in.Tap) && !engineNavigation(in.Tap)) {
		return false, nil
	}
	peeked, owned, err := p.peekStep(ctx, householdID, card.ReviewRequestID, update.Message.From.ID)
	if err != nil || !owned {
		return false, err
	}
	picked := ""
	switch {
	case in.CategorySlug != "":
		if err := p.pool.QueryRow(ctx, `SELECT COALESCE((SELECT id::text FROM category WHERE household_id=$1 AND slug=$2 AND active),'')`, householdID, in.CategorySlug).Scan(&picked); err != nil {
			return false, err
		}
	case in.Tap == "" && peeked.Fact == factCategory && !strings.Contains(in.Text, "?"):
		picked = p.preselectCategory(ctx, householdID, sourceEventID, in.Text)
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	review, err := loadEngineReview(ctx, tx, householdID, card.ReviewRequestID, update.Message.From.ID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	step, owned, err := p.currentStep(ctx, tx, householdID, review)
	if err != nil || !owned {
		return false, err
	}
	outcome, err := p.applyStep(ctx, tx, householdID, sourceEventID, update, review, step, in, picked)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name=$2,parser_version='1' WHERE id=$1`, sourceEventID, engineParser); err != nil {
		return false, err
	}
	if outcome.Declined && in.Tap == "" && in.CategorySlug == "" && (!in.Targeted || strings.Contains(in.Text, "?")) {
		return false, nil
	}
	if !outcome.Silent {
		if err := p.remember(ctx, tx, householdID, sourceEventID, update, outcome); err != nil {
			return false, err
		}
		if err := p.renderStep(ctx, tx, update, review.RequestID, outcome); err != nil {
			return false, err
		}
	}
	if outcome.Changed && !outcome.Settled {
		if err := refreshSiblingCards(ctx, tx, review.RequestID, update.Message.Chat.ID, outcome.Step); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

func (p *Processor) remember(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, update telegramUpdate, outcome stepOutcome) error {
	if text := strings.TrimSpace(update.Message.Text); text != "" {
		if err := insertTurnTx(ctx, tx, householdID, sourceEventID, update, "USER", text, "", map[string]any{"step_engine": true}); err != nil {
			return err
		}
	}
	return insertTurnTx(ctx, tx, householdID, sourceEventID, update, "ASSISTANT", outcome.Step.Text(), "", map[string]any{"step_engine": true})
}

func (p *Processor) applyStep(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, update telegramUpdate, review engineReview, step Step, in engineInput, picked string) (stepOutcome, error) {
	if in.Tap != "" && in.CategorySlug == "" {
		return p.applyTap(ctx, tx, householdID, sourceEventID, review, step, in.Tap)
	}
	switch step.Fact {
	case factMerchant:
		return p.applyMerchant(ctx, tx, householdID, sourceEventID, review, step, in.Text)
	case factCategory:
		if picked == "" {
			return reask(step, unusableAnswerAck), nil
		}
		return p.applyCategory(ctx, tx, householdID, sourceEventID, review, step, picked)
	case factAsset, factInvest:
		return p.applyAccountText(ctx, tx, householdID, sourceEventID, review, step, in.Text)
	case factRemember:
		remember, ok := rememberAnswer(in.Text)
		if !ok {
			return reask(step, unusableAnswerAck), nil
		}
		return p.applyRemember(ctx, tx, householdID, sourceEventID, review, remember)
	}
	return reask(step, unusableAnswerAck), nil
}

func (p *Processor) applyTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	if data != callbackIgnore {
		if outcome, handled, err := p.applyTransferTap(ctx, tx, householdID, sourceEventID, review, step, data); handled || err != nil {
			return outcome, err
		}
	}
	switch {
	case data == callbackIgnore && step.Fact != factRemember:
		return p.applyIgnore(ctx, tx, householdID, sourceEventID, review, step)
	case step.Fact == factCategory && strings.HasPrefix(data, callbackCategoryPg):
		page, ok := parsePageCallback(data, callbackCategoryPg)
		if !ok {
			return staleTap(step), nil
		}
		return p.categoryPage(ctx, tx, review, step, page)
	case step.Fact == factCategory && strings.HasPrefix(data, callbackCategory):
		return p.applyCategory(ctx, tx, householdID, sourceEventID, review, step, strings.TrimPrefix(data, callbackCategory))
	case step.Fact == factRemember && (data == callbackRemember || data == callbackOnce):
		return p.applyRemember(ctx, tx, householdID, sourceEventID, review, data == callbackRemember)
	}
	return staleTap(step), nil
}

func reask(step Step, ack string) stepOutcome {
	return stepOutcome{Step: step.WithAck(ack), Declined: true}
}

func staleTap(step Step) stepOutcome {
	return stepOutcome{Step: step, Declined: true, Silent: true}
}

func (p *Processor) categoryPage(ctx context.Context, tx pgx.Tx, review engineReview, step Step, page int) (stepOutcome, error) {
	if err := step.loadCategoryRows(ctx, tx, review.RequestID, page); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: step.executable(cardActions(review.Decision, review.ReviewType))}, nil
}

func (p *Processor) applyIgnore(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step) (stepOutcome, error) {
	if review.TransactionID == "" {
		return p.applyIgnoreReconciliation(ctx, tx, householdID, review, step)
	}
	outcome, err := confirmedCardOutcome(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	amount, err := transactionAmount(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	err = rejectReviewTx(ctx, sp, householdID, review.UserID, sourceEventID, review.RequestID, review.TransactionID, review.ReviewType)
	if errors.Is(err, reviewdomain.ErrMergeTargetActive) {
		_ = sp.Rollback(ctx)
		return reask(step, mergeTargetActiveAck), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: ignoredStep(outcome.Merchant, amount), Settled: true, Changed: true}, nil
}

func (p *Processor) applyMerchant(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, text string) (stepOutcome, error) {
	value, ok := usableMerchantText(text)
	if !ok {
		return reask(step, unusableAnswerAck), nil
	}
	remembered, err := setReviewMerchant(ctx, tx, householdID, review.RequestID, review.TransactionID, value)
	if err != nil {
		return stepOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'field','merchant','value',$4::text)) ON CONFLICT DO NOTHING`, review.TransactionID, sourceEventID, review.RequestID, value); err != nil {
		return stepOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'UPDATE_REVIEW_DETAIL','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'field','merchant','value',$5::text))`, householdID, review.UserID, review.TransactionID, review.RequestID, value); err != nil {
		return stepOutcome{}, err
	}
	if remembered != "" {
		done, err := p.applyCategory(ctx, tx, householdID, sourceEventID, review, step, remembered)
		if err != nil {
			return stepOutcome{}, err
		}
		if !done.Declined {
			if done.Step.Kind == stepDone {
				done.Step.Hint = "Kategorinya aku ambil dari yang sudah kamu simpan untuk merchant ini."
			}
			return done, nil
		}
	}
	review.Decision = withoutMissingFact(review.Decision, "merchant")
	next, ok, err := p.nextAfterMerchant(ctx, tx, householdID, review, value)
	if err != nil || !ok {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: next, Changed: true}, nil
}

func (p *Processor) nextAfterMerchant(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, merchant string) (Step, bool, error) {
	facts, err := transactionReviewSummary(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return Step{}, false, err
	}
	if len(review.Decision.MissingFacts) == 0 {
		review.Decision.MissingFacts = []string{"category"}
		if _, err := tx.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(decision,'{missingFacts}','["category"]'::jsonb),updated_at=now() WHERE id=$1`, review.ItemID); err != nil {
			return Step{}, false, err
		}
	}
	step, ok := deriveStep(review.Decision, review.ReviewType, facts, review.Subject)
	if !ok {
		return Step{}, false, fmt.Errorf("review %s has no step after the merchant", review.RequestID)
	}
	if err := step.loadCategoryRows(ctx, tx, review.RequestID, 0); err != nil {
		return Step{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state=$2,last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.RequestID, step.State); err != nil {
		return Step{}, false, err
	}
	step = step.WithAck("✅ " + merchant + " disimpan.")
	return step.executable(cardActions(review.Decision, review.ReviewType)), true, nil
}

func withoutMissingFact(decision reviewdec.Decision, fact string) reviewdec.Decision {
	kept := make([]string, 0, len(decision.MissingFacts))
	for _, value := range decision.MissingFacts {
		if value != fact {
			kept = append(kept, value)
		}
	}
	decision.MissingFacts = kept
	return decision
}

func (p *Processor) applyCategory(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, categoryID string) (stepOutcome, error) {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM category WHERE id::text=$1 AND household_id=$2 AND active)`, categoryID, householdID).Scan(&active); err != nil {
		return stepOutcome{}, err
	}
	if !active {
		if step.Fact == factCategory {
			if err := step.loadCategoryRows(ctx, tx, review.RequestID, 0); err != nil {
				return stepOutcome{}, err
			}
			step = step.executable(cardActions(review.Decision, review.ReviewType))
		}
		return reask(step, categoryInactiveAck), nil
	}
	if transferReview(review.ReviewType) && review.TxType == "UNCLASSIFIED" {
		return p.classifyTransfer(ctx, tx, householdID, sourceEventID, review, step, "EXPENSE", "", categoryID)
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	if transferReview(review.ReviewType) {
		if _, err := reviewdomain.ApplyTransactionReviewFacts(ctx, sp, reviewdomain.ReviewFacts{HouseholdID: householdID, TransactionID: review.TransactionID, CategoryID: categoryID, Relationship: "EXPENSE"}); err != nil {
			_ = sp.Rollback(ctx)
			return stepOutcome{}, err
		}
	}
	confirmed, err := reviewdomain.ConfirmTransactionReview(ctx, sp, reviewdomain.ConfirmCommand{
		HouseholdID: householdID, ActorUserID: review.UserID, TransactionID: review.TransactionID,
		ReviewItemID: review.ItemID, RequestID: review.RequestID,
		Action: "TELEGRAM_CONFIRMED", CategorySupplied: true, CategoryID: categoryID,
	})
	var missing *reviewdomain.ErrMissingFacts
	if errors.As(err, &missing) || errors.Is(err, reviewdomain.ErrInvalidCandidate) || errors.Is(err, reviewdomain.ErrMissingCategory) {
		_ = sp.Rollback(ctx)
		return staleTap(step), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := reviewdomain.PromoteEvidenceDocuments(ctx, sp, householdID, review.TransactionID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	askRemember, err := finishConfirmedReview(ctx, sp, confirmedReview{
		HouseholdID: householdID, UserID: review.UserID, ReviewID: review.RequestID, TransactionID: review.TransactionID,
		CategoryID: categoryID, SourceEventID: sourceEventID, MerchantID: confirmed.MerchantID, Confidence: 1, OfferLearning: !transferReview(review.ReviewType),
	})
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	outcome, err := confirmedCardOutcome(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	if askRemember {
		return stepOutcome{Step: rememberStep(outcome.Facts, outcome.Merchant, outcome.Category), Settled: true, Changed: true}, nil
	}
	amount, err := transactionAmount(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: doneStep(outcome.Merchant, outcome.Category, amount, ""), Settled: true, Changed: true}, nil
}

func (p *Processor) applyRemember(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, remember bool) (stepOutcome, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	if err := commitMerchantLearning(ctx, sp, householdID, review.UserID, review.RequestID, review.TransactionID, sourceEventID, remember); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	outcome, err := confirmedCardOutcome(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	amount, err := transactionAmount(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	note := "Kategori hanya dipakai untuk transaksi ini."
	if remember {
		note = "⭐ Diingat untuk " + outcome.Merchant + ". Bisa dinonaktifkan di Settings."
	}
	return stepOutcome{Step: doneStep(outcome.Merchant, outcome.Category, amount, note), Settled: true, Changed: true}, nil
}

func transactionAmount(ctx context.Context, tx pgx.Tx, householdID, transactionID string) (string, error) {
	var amount string
	err := tx.QueryRow(ctx, `SELECT round(amount)::text FROM transaction WHERE id=$1 AND household_id=$2`, transactionID, householdID).Scan(&amount)
	return amount, err
}

func (p *Processor) renderStep(ctx context.Context, tx pgx.Tx, update telegramUpdate, reviewID string, outcome stepOutcome) error {
	text := outcome.Step.Text()
	markup := outcome.Step.Markup()
	if update.CallbackQuery != nil {
		chatID, messageID := update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID
		if outcome.Settled {
			return settleReviewCardInPlace(ctx, tx, reviewID, chatID, messageID, text, markup)
		}
		if markup == nil {
			markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
		}
		return enqueueReviewUpdateWithMarkup(ctx, tx, reviewID, update, text, markup)
	}
	return enqueueStepCard(ctx, tx, reviewID, update.Message.Chat.ID, update.Message.MessageID, text, markup, outcome.Settled)
}

func enqueueStepCard(ctx context.Context, tx pgx.Tx, reviewID string, chatID, replyTo int64, text string, markup *InlineKeyboardMarkup, settled bool) error {
	payload := map[string]any{"chat_id": chatID, "reply_to_message_id": replyTo, "text": clean(text, 4000), "review_request_id": reviewID}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	if settled {
		payload["settled_card"] = true
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',$1::jsonb)`, string(encoded))
	return err
}

func refreshSiblingCards(ctx context.Context, tx pgx.Tx, reviewID string, exceptChat int64, step Step) error {
	markup := step.Markup()
	if markup == nil {
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
	}
	encoded, err := json.Marshal(markup)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO job(type,lane,payload_json)
		SELECT 'EDIT_TELEGRAM_MESSAGE','INTERACTIVE',jsonb_build_object('chat_id',rr.telegram_chat_id,'message_id',rr.telegram_message_id,'text',$3::text,'reply_markup',$4::jsonb,'review_request_id',rr.review_request_id::text)
		FROM review_request_recipient rr
		WHERE rr.review_request_id=$1::uuid AND rr.telegram_chat_id<>$2 AND rr.telegram_message_id IS NOT NULL AND rr.delivery_status='SENT'`,
		reviewID, exceptChat, clean(step.Text(), 4000), string(encoded))
	return err
}

func usableMerchantText(text string) (string, bool) {
	value := strings.Join(strings.Fields(text), " ")
	length := len([]rune(value))
	if length < 2 || length > 80 || strings.Contains(text, "?") || len(strings.Fields(value)) > 8 {
		return "", false
	}
	hasLetter := false
	for _, r := range value {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	normalized := normalizeAnswer(value)
	if !hasLetter || isYesNoAnswer(normalized) || isHelpCommand(normalized) || strings.HasPrefix(normalized, "/") || mentionsFinanceRequest(normalized) {
		return "", false
	}
	return clean(value, 160), true
}

var rememberDeclines = map[string]bool{
	"tidak": true, "tdk": true, "gak": true, "ga": true, "nggak": true, "ngga": true, "enggak": true, "no": true, "jangan": true,
	"tidak usah": true, "ga usah": true, "gak usah": true, "nggak usah": true,
	"sekali ini": true, "sekali ini saja": true, "sekali ini aja": true,
}

func rememberAnswer(text string) (remember, ok bool) {
	normalized := normalizeAnswer(text)
	if rememberDeclines[normalized] {
		return false, true
	}
	return true, isYesNoAnswer(normalized)
}

func (p *Processor) peekStep(ctx context.Context, householdID, reviewRequestID string, telegramUserID int64) (Step, bool, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Step{}, false, err
	}
	defer tx.Rollback(ctx)
	review, err := loadEngineReview(ctx, tx, householdID, reviewRequestID, telegramUserID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return Step{}, false, nil
	}
	if err != nil {
		return Step{}, false, err
	}
	return p.currentStep(ctx, tx, householdID, review)
}

func (p *Processor) preselectCategory(ctx context.Context, householdID, sourceEventID, text string) string {
	choices, err := p.categoryChoices(ctx, householdID)
	if err != nil || len(choices) == 0 {
		return ""
	}
	names := map[string]string{}
	bySlug := map[string]string{}
	for _, choice := range choices {
		names[choice.Slug] = choice.Name
		bySlug[choice.Slug] = choice.ID
	}
	if slug, ok := parseCategoryAnswer(text, names); ok {
		return bySlug[slug]
	}
	slug := p.pickCategorySlug(ctx, sourceEventID, text, choices)
	return bySlug[slug]
}

func (p *Processor) categoryChoices(ctx context.Context, householdID string) ([]categoryChoice, error) {
	rows, err := p.pool.Query(ctx, `SELECT id::text,name,slug FROM category WHERE household_id=$1 AND active ORDER BY sort_order,name,id`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []categoryChoice
	for rows.Next() {
		var choice categoryChoice
		if err := rows.Scan(&choice.ID, &choice.Name, &choice.Slug); err != nil {
			return nil, err
		}
		out = append(out, choice)
	}
	return out, rows.Err()
}

func (p *Processor) ProcessCardAnswer(ctx context.Context, sourceEventID string) (bool, error) {
	var householdID, payloadText, processingStatus, parserName string
	err := p.pool.QueryRow(ctx, `SELECT s.household_id,p.payload_json::text,s.processing_status,COALESCE(s.parser_name,'')
		FROM source_event s JOIN source_event_payload p ON p.source_event_id=s.id
		WHERE s.id=$1 AND s.source_type='TELEGRAM_TEXT'`, sourceEventID).Scan(&householdID, &payloadText, &processingStatus, &parserName)
	if err != nil {
		return false, fmt.Errorf("load Telegram card answer: %w", err)
	}
	if processingStatus != "RECEIVED" && processingStatus != "PROCESSING" {
		return false, nil
	}
	var update telegramUpdate
	if err := json.Unmarshal([]byte(payloadText), &update); err != nil {
		return false, fmt.Errorf("decode Telegram card answer: %w", err)
	}
	text := strings.TrimSpace(update.Message.Text)
	if authorized, err := telegramMemberAuthorized(ctx, p.pool, update.Message.From.ID, householdID); err != nil {
		return false, fmt.Errorf("re-check Telegram sender: %w", err)
	} else if authorized && text != "" && !isHelpCommand(text) {
		stopTyping := p.startTyping(ctx, update.Message.Chat.ID)
		defer stopTyping()
		resolved, err := p.resolveInteraction(ctx, householdID, sourceEventID, update, interactionOverride(parserName))
		if err != nil {
			return false, err
		}
		if resolved.Kind == interactionCardStep {
			in := engineInput{Text: text, Targeted: resolved.Source != sourcePlain || interactionOverride(parserName) == interactionParserAnswer}
			if resolved.ButtonData != "" {
				in = engineInput{Tap: resolved.ButtonData, Targeted: true}
			}
			handled, err := p.runStepEngine(ctx, householdID, sourceEventID, resolved.Update, resolved.Card, in)
			if err != nil {
				return false, err
			}
			if handled {
				p.recordInteraction(ctx, householdID, sourceEventID, resolved.Card.ReviewItemID, interactionAnswered, resolved.Source)
				return false, nil
			}
		}
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO job(type,payload_json) SELECT 'PROCESS_TELEGRAM_TEXT',jsonb_build_object('source_event_id',$1::uuid)
		WHERE NOT EXISTS(SELECT 1 FROM job WHERE type='PROCESS_TELEGRAM_TEXT' AND payload_json->>'source_event_id'=$1::text)`, sourceEventID); err != nil {
		return false, fmt.Errorf("hand Telegram card answer to the assistant: %w", err)
	}
	slog.Default().Info("card answer handed to the assistant", "source_event_id", sourceEventID)
	return true, nil
}
