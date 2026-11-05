package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func (p *Processor) extractReview(ctx context.Context, sourceEventID, text string, categories []categoryChoice) (reviewExtraction, error) {
	if p.gateway == nil {
		return reviewExtraction{}, fmt.Errorf("review classifier unavailable")
	}
	slugs := make([]string, 0, len(categories))
	for _, category := range categories {
		slugs = append(slugs, category.Slug)
	}
	call, metadata, err := p.gateway.NativeToolCall(ctx, sourceEventID, reviewPrompt,
		map[string]any{"reply": untrustedUser(text), "allowed_category_slugs": slugs},
		[]gateway.ToolDefinition{{Name: "resolve_review", Description: "Resolve one already-bound finance review using bounded values.", Parameters: reviewSchema(slugs, len(categories) > 0)}}, gateway.NativeToolOptions{Required: true})
	if err != nil {
		return reviewExtraction{}, err
	}
	result, err := gateway.DecodeToolArguments[reviewExtraction](call, "resolve_review")
	if err != nil {
		return reviewExtraction{}, err
	}
	_ = metadata
	if result.Confidence < 0 || result.Confidence > 1 {
		return reviewExtraction{}, fmt.Errorf("review confidence outside range")
	}
	result.Description = clean(result.Description, 500)
	result.Note = clean(result.Note, 1000)
	return result, nil
}

type residualAllocation struct {
	WealthAccountID string `json:"wealth_account_id"`
	AmountIDR       string `json:"amount_idr"`
	Note            string `json:"note"`
}

// requiredNativeReviewDetail names the one free-form fact a bound transaction
// review still needs and the value the model proposed for it. For a date review
// the value is the proposed date, whichever date argument carried it.
func requiredNativeReviewDetail(reviewType, state, merchantID, merchant, description, date string) (field, value string, required bool) {
	if state == "AWAITING_MERCHANT" || (reviewType == "UNKNOWN_MERCHANT" && merchantID == "" && strings.TrimSpace(merchant) != "") {
		return "merchant", clean(strings.TrimSpace(merchant), 500), true
	}
	if state == "AWAITING_DATE" {
		return "transaction_at", strings.TrimSpace(date), true
	}
	if reviewType == "UNKNOWN_PURPOSE" || state == "AWAITING_DETAIL" {
		return "description", clean(strings.TrimSpace(description), 500), true
	}
	return "", "", false
}

func validReviewDate(value string) bool {
	_, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	return err == nil
}

func validReviewTimestamp(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	return err == nil && !parsed.IsZero() && parsed.Format(time.RFC3339) != ""
}

// offerCategoryChooser renders the household category chooser for a review whose only
// unresolved fact is the category, so a merchant-less bank transaction is completable
// inside Telegram. It reuses the same paged markup the initial send uses.
func (p *Processor) offerCategoryChooser(ctx context.Context, sourceEventID, householdID, reviewID, transactionID, reviewType string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// The category chooser itself, never the review's own kind keyboard: a
	// transfer card's Pengeluaran choice must lead to categories, not back to the
	// transfer choices.
	markup := categoryChooserMarkup(ctx, tx, reviewID, 0)
	if markup == nil {
		if err = enqueueReply(ctx, tx, update, "Belum ada kategori aktif yang bisa dipilih."); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	text := categoryPageText(ctx, tx, householdID, reviewID, 0)
	if update.CallbackQuery != nil {
		// The chooser is already on screen, so edit it in place.
		err = enqueueReviewUpdateWithMarkup(ctx, tx, reviewID, update, text, markup)
	} else {
		err = enqueueReviewMessageWithMarkup(ctx, tx, reviewID, update.Message.Chat.ID, update.Message.MessageID, text, markup)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) continueReview(ctx context.Context, sourceEventID, reviewID, transactionID string, update telegramUpdate, message string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence (transaction_id,source_event_id,evidence_type,metadata_json) VALUES ($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid)) ON CONFLICT DO NOTHING`, transactionID, sourceEventID, reviewID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_CATEGORY',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID); err != nil {
		return err
	}
	if err := enqueueReviewMessage(ctx, tx, reviewID, update.Message.Chat.ID, update.Message.MessageID, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) resolveReview(ctx context.Context, sourceEventID, householdID, reviewID, transactionID, categoryID string, update telegramUpdate, value reviewExtraction) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err := tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return err
	}
	if err := p.resolveReviewTx(ctx, tx, sourceEventID, householdID, reviewID, transactionID, categoryID, update, value, userID, true); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) resolveReviewTx(ctx context.Context, tx pgx.Tx, sourceEventID, householdID, reviewID, transactionID, categoryID string, update telegramUpdate, value reviewExtraction, userID string, offerMerchantLearning bool) error {
	var err error
	var merchantID *string
	// A legacy card or a client that omits a field must not confirm while
	// the stored ReviewDecision still reports a canonical-required residual fact.
	// The date check uses the parsed pay date only; a fallback timestamp never
	// satisfies it.
	var storedDecision []byte
	if err := tx.QueryRow(ctx, `SELECT COALESCE(ri.decision,'{}'::jsonb) FROM review_request rr JOIN review_item ri ON ri.id=rr.review_item_id WHERE rr.id=$1 AND ri.household_id=$2 AND ri.status IN ('PENDING_SEND','OPEN') FOR UPDATE OF ri`, reviewID, householdID).Scan(&storedDecision); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	payDate, payDateErr := parseSuppliedReviewDate(value.PayDate)
	if payDateErr != nil {
		return payDateErr
	}
	if blocked := residualConfirmationBlockers(storedDecision, payDate != nil, categoryID != "", false); len(blocked) > 0 {
		return enqueueReply(ctx, tx, update, reviewNeedsFactsMessage(blocked))
	}
	var transactionAt *time.Time
	if payDate != nil {
		parsed, err := time.ParseInLocation("2006-01-02", *payDate, jakartaLocation())
		if err != nil {
			return err
		}
		transactionAt = &parsed
	}
	// ADR-046: the transaction mutation and candidate revalidation live in the
	// shared canonical resolver. Telegram keeps only its own delivery/evidence and
	// payslip side effects, and defers terminal completion while it asks whether to
	// remember the merchant.
	var requestID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM review_request WHERE id=$1 AND household_id=$2`, reviewID, householdID).Scan(&requestID); err != nil {
		return err
	}
	confirmResult, err := reviewdomain.ConfirmTransactionReview(ctx, tx, reviewdomain.ConfirmCommand{
		HouseholdID: householdID, ActorUserID: userID, TransactionID: transactionID,
		ReviewItemID: pendingReviewItemID(ctx, tx, reviewID), RequestID: requestID,
		Action: "TELEGRAM_CONFIRMED", CategorySupplied: categoryID != "", CategoryID: categoryID,
		Description: value.Description, Note: value.Note, TransactionAt: transactionAt,
		ResolveReview: false,
	})
	if err != nil {
		var missing *reviewdomain.ErrMissingFacts
		if errors.As(err, &missing) {
			if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id) VALUES($1,'TELEGRAM',$2,'UPDATE_REVIEW_FACTS','transaction',$3)`, householdID, userID, transactionID); err != nil {
				return err
			}
			return refreshReviewFactsProjection(ctx, tx, householdID, reviewID, update)
		}
		if errors.Is(err, reviewdomain.ErrInvalidCandidate) {
			return enqueueReply(ctx, tx, update, "Pilih kategori rumah tangga yang masih aktif.")
		}
		return err
	}
	merchantID = &confirmResult.MerchantID
	if confirmResult.MerchantID == "" {
		merchantID = nil
	}
	// A confirmed review also completes the document workflow (ADR-046: shared op).
	if err := reviewdomain.PromoteEvidenceDocuments(ctx, tx, householdID, transactionID); err != nil {
		return err
	}
	if value.PayDate != "" {
		// A user-supplied pay date completes only a payslip-backed income review.
		// Evidence and period come from the shared payslip reader; no expected
		// payday is ever inferred.
		facts, ok, err := reviewdomain.LoadPayslipFacts(ctx, tx, householdID, transactionID)
		if err != nil {
			return err
		}
		if ok && reviewPayrollPeriodPattern.MatchString(facts.Period) {
			if _, err := reviewdomain.RecordSalaryEvent(ctx, tx, reviewdomain.SalaryCommand{
				HouseholdID: householdID, Employer: facts.Employer, Period: facts.Period,
				PayDate: value.PayDate, NetPay: facts.NetPay,
				Transaction: transactionID, SourceEvent: sourceEventID,
			}); err != nil {
				return err
			}
		}
	}
	if err := reviewdomain.RefreshTransactionLifecycle(ctx, tx, householdID, transactionID); err != nil {
		return err
	}
	askRemember := offerMerchantLearning && merchantID != nil && categoryID != ""
	if askRemember {
		// Complete the review item now so a merchant-learning question the user
		// never answers cannot leave the review open. The pending question
		// is tracked by conversation state, not by review_request.status.
		if err := resolveCanonicalReviewItem(ctx, tx, reviewID, userID, "TELEGRAM_CONFIRMED"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_MERCHANT_DECISION',context_json=context_json||jsonb_build_object('category_id',NULLIF($2,'')::uuid),last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID, categoryID); err != nil {
			return err
		}
	} else {
		if err := resolveCanonicalReviewItem(ctx, tx, reviewID, userID, "TELEGRAM_CONFIRMED"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',context_json=context_json||jsonb_build_object('category_id',NULLIF($2,'')::uuid),last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID, categoryID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence (transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES ($2,$1,'TELEGRAM_REVIEW_REPLY',$3,jsonb_build_object('review_request_id',$4::uuid,'classification','CONFIRM')) ON CONFLICT DO NOTHING`, sourceEventID, transactionID, value.Confidence, reviewID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES ($1,'TELEGRAM',$2,'RESOLVE_REVIEW','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'category_id',NULLIF($5,'')::uuid,'merchant_id',(SELECT merchant_id FROM transaction WHERE id=$3)))`, householdID, userID, transactionID, reviewID, categoryID); err != nil {
		return err
	}
	learningMarkup := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ingat kategori", CallbackData: "review:remember"}, {Text: "Hanya kali ini", CallbackData: "review:once"}}}}
	if update.CallbackQuery != nil {
		// A tap answers on the card itself: the card becomes the result, and the
		// optional remember choice is asked inline on it. No further message is
		// sent and no button is left behind (ADR-046 amendment).
		outcome, err := confirmedCardOutcome(ctx, tx, householdID, transactionID)
		if err != nil {
			return err
		}
		if askRemember {
			if _, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json||jsonb_build_object('learning_card',jsonb_build_object('chat_id',$2::bigint,'message_id',$3::bigint)) WHERE review_request_id=$1`, reviewID, update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID); err != nil {
				return err
			}
			return settleReviewCardInPlace(ctx, tx, reviewID, update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID, outcome.withQuestion(), learningMarkup)
		}
		return settleReviewCardInPlace(ctx, tx, reviewID, update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID, outcome.text(""), nil)
	}
	if askRemember {
		// A typed confirmation has no tapped card to edit, so the optional
		// learning question is its own message bound to the request.
		if err := enqueueMerchantLearningQuestion(ctx, tx, reviewID, update, "✅ Sudah dicatat. Pakai kategori ini juga untuk transaksi berikutnya dari merchant yang sama?", learningMarkup); err != nil {
			return err
		}
	} else if err := enqueueReply(ctx, tx, update, "✅ Sudah dicatat. Kotak Tinjauan juga sudah diperbarui."); err != nil {
		return err
	}
	return nil
}

// cardOutcome is the result a confirmed expense card shows in place of its
// question: what was recorded, and the subject facts that stay on screen.
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

func (o cardOutcome) withQuestion() string {
	return o.text("Pakai " + o.Category + " juga untuk transaksi berikutnya dari " + o.Merchant + "?")
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

// settleReviewCardInPlace turns one delivered card into its outcome with a
// single edit. The request was resolved in this transaction, so the retirement
// trigger (migration 00079) already queued a RETIRE_TELEGRAM_REVIEW_CARD job
// for this card; that job would strip the inline remember choice in an
// unordered race, so this card's pending retirement is marked superseded and the
// outcome edit retires it instead. Other recipients' cards still retire
// normally. The edit carries no review_request_id: the request is terminal and
// this edit is the retirement.
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
	_, err = tx.Exec(ctx, `INSERT INTO job(type,lane,payload_json) VALUES('EDIT_TELEGRAM_MESSAGE','INTERACTIVE',jsonb_build_object('chat_id',$1::bigint,'message_id',$2::bigint,'text',$3::text,'reply_markup',$4::jsonb))`, chatID, messageID, clean(text, 4000), string(encoded))
	return err
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
	if remember {
		var merchantID, categoryID string
		if err = tx.QueryRow(ctx, `SELECT merchant_id,category_id FROM transaction WHERE id=$1 AND household_id=$2 AND status='CONFIRMED' AND merchant_id IS NOT NULL AND category_id IS NOT NULL`, transactionID, householdID).Scan(&merchantID, &categoryID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) SELECT $1,normalized_name,id,$3,true,true FROM merchant WHERE id=$2 AND household_id=$1 ON CONFLICT(household_id,lower(regexp_replace(btrim(raw_name), '[[:space:]]+', ' ', 'g'))) DO UPDATE SET raw_name=excluded.raw_name,default_category_id=excluded.default_category_id,auto_apply=true,created_from_user_confirmation=true`, householdID, merchantID, categoryID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'REMEMBER_MERCHANT','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'merchant_id',$5::uuid,'category_id',$6::uuid))`, householdID, userID, transactionID, reviewID, merchantID, categoryID); err != nil {
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
	message := "Kategori ini hanya dipakai untuk transaksi tadi, tidak disimpan sebagai aturan merchant."
	if remember {
		message = "✅ Kategori merchant sudah disimpan untuk transaksi berikutnya. Aturannya bisa dinonaktifkan di Settings."
	}
	// The card that asked the question inline settles to its final result, so its
	// remember buttons never outlive the answer, however the answer arrived.
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
		// The tapped message is the question (the card itself, or a legacy separate
		// question); it becomes the final result and no reply is sent.
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

func reviewSchema(slugs []string, categoriesRequired ...bool) map[string]any {
	sort.Strings(slugs)
	properties := map[string]any{
		"description": map[string]any{"type": "string"},
		"note":        map[string]any{"type": "string"},
		"confidence":  map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		"ambiguous":   map[string]any{"type": "boolean"},
		"pay_date":    map[string]any{"type": "string"},
	}
	required := []string{"description", "note", "confidence", "ambiguous", "pay_date"}
	if len(categoriesRequired) == 0 || categoriesRequired[0] {
		properties["category_slug"] = map[string]any{"type": "string", "enum": slugs}
		required = append(required, "category_slug")
	}
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": properties,
		"required":   required,
	}
}

// ReviewQuestion is the subject summary a producer passes for a new expense
// review: the amount and, when known, the merchant. The card adds its own title,
// reason and instruction around it (review_render.go), so the summary carries no
// prompt of its own and never a placeholder merchant.
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

// resolveNativeMerchantLearning handles explicit native confirmation without
// parsing free-form text. It reuses the audited transactional executor.
func (p *Processor) resolveNativeMerchantLearning(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, args map[string]any) error {
	remember, _ := args["remember"].(bool)
	var reviewID, transactionID string
	err := p.pool.QueryRow(ctx, `SELECT r.id,r.transaction_id FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id JOIN transaction t ON t.id=r.transaction_id JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.household_id=$1 AND c.state='AWAITING_MERCHANT_DECISION' AND t.status='CONFIRMED' AND rr.telegram_chat_id=$2 ORDER BY r.created_at DESC LIMIT 1`, householdID, update.Message.Chat.ID).Scan(&reviewID, &transactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Tidak ada pilihan kategori merchant yang sedang menunggu konfirmasi.")
	}
	if err != nil {
		return err
	}
	return p.applyMerchantLearningChoice(ctx, sourceEventID, householdID, reviewID, transactionID, update, remember)
}
