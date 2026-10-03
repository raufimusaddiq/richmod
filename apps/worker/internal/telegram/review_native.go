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

func requiredNativeReviewDetail(reviewType, state, merchantID, merchant, description string) (field, value string, required bool) {
	if state == "AWAITING_MERCHANT" || (reviewType == "UNKNOWN_MERCHANT" && merchantID == "" && strings.TrimSpace(merchant) != "") {
		return "merchant", clean(strings.TrimSpace(merchant), 500), true
	}
	if state == "AWAITING_DATE" {
		return "transaction_at", clean(strings.TrimSpace(description), 500), true
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

// missingFactsAreCategoryOnly reports a stored ReviewDecision whose only unresolved
// fact is the category. That is the whole interaction, so the Telegram chooser can
// complete it instead of deferring to the Review Inbox. A legacy review without a
// stored contract keeps its previous behavior and does not qualify.
func missingFactsAreCategoryOnly(raw *string) bool {
	if raw == nil || strings.TrimSpace(*raw) == "null" {
		return false
	}
	var facts []string
	if json.Unmarshal([]byte(*raw), &facts) != nil {
		return false
	}
	return len(facts) == 1 && facts[0] == "category"
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
	markup := reviewActionMarkupPage(ctx, tx, reviewID, reviewType, 0)
	if markup == nil {
		if err = enqueueReply(ctx, tx, update, "Tidak ada kategori aktif untuk dipilih."); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if update.CallbackQuery != nil {
		// The chooser is already on screen, so edit it in place.
		err = enqueueReviewUpdateWithMarkup(ctx, tx, reviewID, update, "Pilih kategori pengeluaran:", markup)
	} else {
		err = enqueueReviewMessageWithMarkup(ctx, tx, reviewID, update.Message.Chat.ID, update.Message.MessageID, "Pilih kategori pengeluaran:", markup)
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
		return err
	}
	merchantID = &confirmResult.MerchantID
	if confirmResult.MerchantID == "" {
		merchantID = nil
	}
	// A confirmed review also completes the document workflow (ADR-046: shared op).
	if err := reviewdomain.PromoteEvidenceDocuments(ctx, tx, transactionID); err != nil {
		return err
	}
	if value.PayDate != "" {
		// A user-supplied pay date completes only a payslip-backed income review.
		// Evidence and period come from the shared payslip reader; no expected
		// payday is ever inferred.
		facts, ok, err := reviewdomain.LoadPayslipFacts(ctx, tx, transactionID)
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
	if _, err := tx.Exec(ctx, `UPDATE source_event s SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM transaction_evidence te JOIN transaction other_t ON other_t.id=te.transaction_id WHERE te.source_event_id=s.id AND other_t.status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' ELSE 'PROCESSED' END WHERE s.id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)`, transactionID); err != nil {
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
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES ($1,'TELEGRAM',$2,'RESOLVE_REVIEW','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'category_id',NULLIF($5,'')::uuid))`, householdID, userID, transactionID, reviewID, categoryID); err != nil {
		return err
	}
	if askRemember {
		markup := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ingat merchant", CallbackData: "review:remember"}, {Text: "Sekali ini", CallbackData: "review:once"}}}}
		if err := enqueueReviewUpdateWithMarkup(ctx, tx, reviewID, update, "Tercatat. Ingat kategori ini untuk merchant tersebut?", markup); err != nil {
			return err
		}
	} else if err := enqueueReply(ctx, tx, update, "Tercatat dan Kotak Tinjauan sudah diperbarui."); err != nil {
		return err
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
	message := "Kategori merchant tidak disimpan sebagai aturan."
	if remember {
		message = "Kategori merchant disimpan dan dapat dinonaktifkan di Settings."
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

func ReviewQuestion(amount, merchant string) string {
	if strings.TrimSpace(merchant) == "" {
		merchant = "transaksi ini"
	}
	return "🟡 Perlu ditinjau\n\n" +
		"Nominal: Rp" + FormatIDR(amount) + "\n" +
		"Merchant: " + merchant + "\n\n" +
		"Balas pesan ini dengan tujuan pengeluaran, atau pilih kategori di bawah."
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
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Tidak ada konfirmasi merchant yang aktif.")
	}
	if err != nil {
		return err
	}
	return p.applyMerchantLearningChoice(ctx, sourceEventID, householdID, reviewID, transactionID, update, remember)
}
