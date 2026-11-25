package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

var localTimePattern = regexp.MustCompile(`^([01]?[0-9]|2[0-3])[:.]([0-5][0-9])$`)

func parseLocalTime(value string) (hour, minute int, ok bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if approximate, found := approximateLocalTimes[value]; found {
		return approximate.hour, approximate.minute, true
	}
	match := localTimePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, 0, false
	}
	_, _ = fmt.Sscanf(match[1], "%d", &hour)
	_, _ = fmt.Sscanf(match[2], "%d", &minute)
	return hour, minute, true
}

var approximateLocalTimes = map[string]struct {
	hour, minute int
	period       string
}{
	"pagi":      {hour: 9, period: "PAGI"},
	"morning":   {hour: 9, period: "PAGI"},
	"siang":     {hour: 13, period: "SIANG"},
	"afternoon": {hour: 13, period: "SIANG"},
	"sore":      {hour: 17, period: "SORE"},
	"evening":   {hour: 17, period: "SORE"},
	"malam":     {hour: 20, period: "MALAM"},
	"night":     {hour: 20, period: "MALAM"},
}

type Gateway interface {
	NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error)
}

type Processor struct {
	pool                         *pgxpool.Pool
	gateway                      Gateway
	postGenerativeAutoConfirmOff bool
	now                          func() time.Time
	bot                          *Bot
}

type telegramUpdate struct {
	Message struct {
		MessageID      int64  `json:"message_id"`
		Date           int64  `json:"date"`
		Text           string `json:"text"`
		ReplyToMessage *struct {
			MessageID int64 `json:"message_id"`
		} `json:"reply_to_message"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
	CallbackQuery *telegramCallbackQuery `json:"callback_query"`
}

type telegramCallbackQuery struct {
	ID   string `json:"id"`
	Data string `json:"data"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Message telegramCallbackMessage `json:"message"`
}

type telegramCallbackMessage struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}

func (p *Processor) turnTime(update telegramUpdate) time.Time {
	if update.Message.Date > 0 {
		return time.Unix(update.Message.Date, 0).In(jakartaLocation())
	}
	return p.now().In(jakartaLocation())
}

func NewProcessor(pool *pgxpool.Pool, llm Gateway) *Processor {
	return &Processor{pool: pool, gateway: llm, now: time.Now}
}

func (p *Processor) SetBot(bot *Bot) { p.bot = bot }

func (p *Processor) SetPostGenerativeAutoConfirm(enabled bool) {
	p.postGenerativeAutoConfirmOff = !enabled
}

func (p *Processor) EnsureSourceEventFinal(ctx context.Context, sourceEventID string) error {
	var status string
	if err := p.pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, sourceEventID).Scan(&status); err != nil {
		return fmt.Errorf("verify Telegram source event: %w", err)
	}
	if status == "RECEIVED" || status == "PROCESSING" {
		return fmt.Errorf("Telegram source event %s remained %s after processing", sourceEventID, status)
	}
	return nil
}

func (p *Processor) Process(ctx context.Context, sourceEventID string) (err error) {
	var householdID, payloadText, processingStatus, sourceType string
	err = p.pool.QueryRow(ctx, `
		SELECT s.household_id,p.payload_json::text,s.processing_status,s.source_type
		FROM source_event s JOIN source_event_payload p ON p.source_event_id=s.id
		WHERE s.id=$1 AND s.source_type IN ('TELEGRAM_TEXT','TELEGRAM_CALLBACK')`, sourceEventID).Scan(&householdID, &payloadText, &processingStatus, &sourceType)
	if err != nil {
		return fmt.Errorf("load Telegram source event: %w", err)
	}
	if processingStatus == "PROCESSED" || processingStatus == "IGNORED" || processingStatus == "NEEDS_REVIEW" {
		return nil
	}
	var update telegramUpdate
	if err := json.Unmarshal([]byte(payloadText), &update); err != nil {
		return fmt.Errorf("decode Telegram source evidence: %w", err)
	}
	if update.CallbackQuery != nil {
		defer func() {
			if errors.Is(err, reviewdomain.ErrEvidenceDisputeConfirmationRequired) {
				err = p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, disputeButtonsOnlyMessage)
			}
		}()
		update.Message.MessageID = update.CallbackQuery.Message.MessageID
		update.Message.Chat.ID = update.CallbackQuery.Message.Chat.ID
		update.Message.From.ID = update.CallbackQuery.From.ID
		update.Message.ReplyToMessage = &struct {
			MessageID int64 `json:"message_id"`
		}{MessageID: update.CallbackQuery.Message.MessageID}
		update.Message.Text = callbackText(update.CallbackQuery.Data)
	}
	if authorized, err := telegramMemberAuthorized(ctx, p.pool, update.Message.From.ID, householdID); err != nil {
		return fmt.Errorf("re-check Telegram sender: %w", err)
	} else if !authorized {
		return ignoreUnauthorizedSender(ctx, p.pool, sourceEventID)
	}
	if update.CallbackQuery != nil {
		if tapped, err := p.resolveInteraction(ctx, householdID, sourceEventID, update, ""); err != nil {
			return err
		} else if tapped.Kind == interactionCardStep {
			if !navigationCallback(update.CallbackQuery.Data) && !legacyDetailNavigation(update.CallbackQuery.Data) {
				p.recordInteraction(ctx, householdID, sourceEventID, tapped.Card.ReviewItemID, interactionAnswered, sourceTap)
			}
			if handled, err := p.runStepEngine(ctx, householdID, sourceEventID, update, tapped.Card, engineInput{Tap: update.CallbackQuery.Data, Targeted: true}); handled || err != nil {
				return err
			}
		}
		if handled, err := p.dispatchCallback(ctx, sourceEventID, householdID, update); handled {
			return err
		}
	}
	if sourceType == "TELEGRAM_CALLBACK" {
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, staleActionMessage)
	}
	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Pesan kosong diabaikan.")
	}
	if isHelpCommand(text) {
		return p.finishWithoutTransaction(ctx, sourceEventID, "PROCESSED", update, helpMessage)
	}
	return p.ProcessAgent(ctx, sourceEventID)
}

func (p *Processor) dispatchCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate) (handledResult bool, err error) {
	defer func() {
		if errors.Is(err, reviewdomain.ErrEvidenceDisputeConfirmationRequired) {
			handledResult, err = true, p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, disputeButtonsOnlyMessage)
		}
	}()
	data := update.CallbackQuery.Data
	if data == callbackInteractionAnswer || data == callbackInteractionNew {
		return true, p.processInteractionChoiceCallback(ctx, sourceEventID, householdID, update, data)
	}
	if handled, err := p.processCardChoiceCallback(ctx, sourceEventID, householdID, update, data); handled {
		return true, err
	}
	if handled, err := p.processPendingConfirmCallback(ctx, sourceEventID, householdID, update, data); handled {
		return true, err
	}
	return p.processMerchantLearningTap(ctx, sourceEventID, householdID, update, data)
}

const disputeButtonsOnlyMessage = "Koreksi screenshot hanya bisa diselesaikan lewat tombol di kartunya."

func strPtr(value string) *string { return &value }

func stringPtr(v any) *string {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func (p *Processor) processPendingEdit(ctx context.Context, householdID string, update telegramUpdate, sourceID string, confirm bool, token string) (bool, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var actionID string
	err = tx.QueryRow(ctx, `SELECT id FROM telegram_pending_action WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND confirmation_token::text=$4 AND status='PENDING' AND expires_at>now() FOR UPDATE`, householdID, update.Message.From.ID, update.Message.Chat.ID, token).Scan(&actionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	message := "Perubahan dibatalkan."
	if confirm {
		outcome, err := applyPendingCorrectionTx(ctx, tx, householdID, actionID, update.Message.From.ID)
		if err != nil {
			return true, err
		}
		message = "✅ Perubahan transaksi sudah disimpan."
		if outcome.ProposedAt != nil {
			message = "✅ Tanggal transaksi sudah diubah ke " + formatIDDateTime(*outcome.ProposedAt) + "."
		}
		if outcome.Refused {
			message = sourceEvidenceFixesAmountMessage
		}
		if outcome.Voided {
			message = "Transaksi itu sudah dibatalkan, jadi perubahannya tidak disimpan."
		}
		if outcome.Blocked != "" {
			message, _ = agentMutationReply(agentToolResult{Status: outcome.Blocked})
		}
	} else if _, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status='CANCELLED',resolved_at=now() WHERE id=$1`, actionID); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-edit-confirmation',parser_version='1' WHERE id=$1`, sourceID); err != nil {
		return true, err
	}
	if err = enqueueReply(ctx, tx, update, message); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

func (p *Processor) processPendingBatch(ctx context.Context, householdID string, update telegramUpdate, sourceID string, confirm bool, token string) (bool, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var batchID, raw string
	err = tx.QueryRow(ctx, `SELECT id,items_json::text FROM telegram_pending_batch WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND confirmation_token::text=$4 AND status='PENDING' AND expires_at>now() FOR UPDATE`, householdID, update.Message.From.ID, update.Message.Chat.ID, token).Scan(&batchID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	msg := "Pencatatan daftar transaksi dibatalkan."
	if confirm {
		var items []agentPendingBatchItem
		if err = json.Unmarshal([]byte(raw), &items); err != nil {
			return true, err
		}
		var userID string
		if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
			return true, err
		}
		outcome, err := p.confirmPendingBatchTx(ctx, tx, householdID, batchID, items, userID, update)
		if err != nil {
			return true, err
		}
		msg = batchOutcomeMessage(outcome.Confirmed, outcome.NeedsReview, outcome.Skipped)
	} else if _, err = tx.Exec(ctx, `UPDATE telegram_pending_batch SET status='CANCELLED',resolved_at=now() WHERE id=$1`, batchID); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-batch-confirmation',parser_version='1' WHERE id=$1`, sourceID); err != nil {
		return true, err
	}
	if err = enqueueReply(ctx, tx, update, msg); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

type validatedExtraction struct {
	Type               string
	Amount             string
	TransactionAt      time.Time
	DateProvenance     string
	Merchant           string
	CategorySlug       string
	Description        string
	Note               string
	Confidence         float64
	CategoryConfidence float64
	Ambiguous          bool
	ResponseMessage    string
	TimePrecision      string
	TimePeriod         string
}

type resolvedTransactionTime struct {
	At        time.Time
	Precision string
	Period    string
}

func nativeValidatedExtraction(args map[string]any, now time.Time) (validatedExtraction, error) {
	typ, _ := args["type"].(string)
	amount, _ := args["amount_idr"].(string)
	merchant, _ := args["merchant"].(string)
	category, _ := args["category_slug"].(string)
	description, _ := args["description"].(string)
	note, _ := args["note"].(string)
	dateReference, _ := args["date_reference"].(string)
	dateSource, _ := args["date_provenance"].(string)
	ambiguous, _ := args["ambiguous"].(bool)
	explicitDate, _ := args["explicit_date"].(string)
	localTime, _ := args["local_time"].(string)
	confidence, _ := args["confidence"].(float64)
	categoryConfidence, _ := args["category_confidence"].(float64)
	if typ != "INCOME" && typ != "EXPENSE" {
		return validatedExtraction{}, fmt.Errorf("type")
	}
	if (dateSource != "" && dateSource != "USER_STATED" && dateSource != "NOT_USER_STATED") || (dateSource == "USER_STATED" && dateReference == "") {
		return validatedExtraction{}, fmt.Errorf("date provenance")
	}
	if dateReference != "" && dateSource == "" {
		return validatedExtraction{}, fmt.Errorf("date provenance missing")
	}
	value, ok := new(big.Int).SetString(amount, 10)
	if !ok || value.Sign() <= 0 || value.String() != amount {
		return validatedExtraction{}, fmt.Errorf("amount")
	}
	var dateReferencePtr *string
	if dateReference != "" {
		dateReferencePtr = &dateReference
	}
	resolved, err := resolveTransactionTime(now, dateReferencePtr, stringPtr(explicitDate), stringPtr(localTime))
	if err != nil {
		return validatedExtraction{}, err
	}
	provenance := "MISSING"
	if dateSource == "USER_STATED" {
		provenance = "USER_STATED"
	}
	return validatedExtraction{Type: typ, Amount: amount, TransactionAt: resolved.At, DateProvenance: provenance, Merchant: clean(merchant, 160), CategorySlug: clean(category, 120), Description: clean(description, 500), Note: clean(note, 1000), Confidence: confidence, CategoryConfidence: categoryConfidence, Ambiguous: ambiguous, TimePrecision: resolved.Precision, TimePeriod: resolved.Period}, nil
}

func resolveTime(now time.Time, dateReference, explicitDate, localTime *string) (time.Time, error) {
	resolved, err := resolveTransactionTime(now, dateReference, explicitDate, localTime)
	return resolved.At, err
}

func resolveTransactionTime(now time.Time, dateReference, explicitDate, localTime *string) (resolvedTransactionTime, error) {
	date := now
	if dateReference != nil {
		switch *dateReference {
		case "TODAY":
		case "YESTERDAY":
			date = date.AddDate(0, 0, -1)
		case "EXPLICIT":
			if explicitDate == nil {
				return resolvedTransactionTime{}, fmt.Errorf("explicit date missing")
			}
			parsed, err := time.ParseInLocation("2006-01-02", *explicitDate, now.Location())
			if err != nil {
				return resolvedTransactionTime{}, fmt.Errorf("invalid explicit date")
			}
			date = parsed
		default:
			return resolvedTransactionTime{}, fmt.Errorf("unknown date reference")
		}
	}
	hour, minute := now.Hour(), now.Minute()
	precision, period := "OBSERVED_AT_PROCESSING", ""
	if localTime != nil {
		value := strings.ToLower(strings.TrimSpace(*localTime))
		if approximate, ok := approximateLocalTimes[value]; ok {
			hour, minute = approximate.hour, approximate.minute
			precision, period = "APPROXIMATE", approximate.period
		} else {
			h, m, ok := parseLocalTime(value)
			if !ok {
				return resolvedTransactionTime{}, fmt.Errorf("invalid local time")
			}
			hour, minute = h, m
			precision = "EXACT"
		}
	}
	return resolvedTransactionTime{At: time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, now.Location()), Precision: precision, Period: period}, nil
}

func (p *Processor) categorySlugs(ctx context.Context, householdID string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT slug FROM category WHERE household_id=$1 AND active ORDER BY slug`, householdID)
	if err != nil {
		return nil, fmt.Errorf("load category policies: %w", err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		result = append(result, slug)
	}
	return result, rows.Err()
}

func (p *Processor) finishWithoutTransaction(ctx context.Context, sourceEventID, status string, update telegramUpdate, message string) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status=$2,parser_name=COALESCE(parser_name,'telegram-reply'),parser_version=COALESCE(parser_version,'1') WHERE id=$1`, sourceEventID, status); err != nil {
		return err
	}
	reply := message
	if update.CallbackQuery != nil && message == staleActionMessage {
		reply = staleReviewReplyForSource(ctx, tx, sourceEventID, update, message)
	}
	if err := enqueueReply(ctx, tx, update, reply); err != nil {
		return err
	}
	if update.CallbackQuery != nil && message == staleActionMessage {
		if err := enqueueRetireButtons(ctx, tx, update, "Tidak lagi berlaku."); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func enqueueReply(ctx context.Context, tx pgx.Tx, update telegramUpdate, message string) error {
	_, err := tx.Exec(ctx, `INSERT INTO job (type,payload_json) VALUES ('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',$2::bigint,'text',$3::text))`, update.Message.Chat.ID, update.Message.MessageID, clean(message, 4000))
	return err
}

func enqueueReplyMarkup(ctx context.Context, tx pgx.Tx, update telegramUpdate, message string, markup *InlineKeyboardMarkup) error {
	encoded, err := json.Marshal(markup)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',$2::bigint,'text',$3::text,'reply_markup',$4::jsonb))`, update.Message.Chat.ID, update.Message.MessageID, clean(message, 4000), string(encoded))
	return err
}

func clean(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func jakartaLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		panic(err)
	}
	return location
}
