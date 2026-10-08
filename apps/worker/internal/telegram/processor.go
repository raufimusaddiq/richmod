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
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

var localTimePattern = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)

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
	pool     *pgxpool.Pool
	gateway  Gateway
	judgment judgment.Engine
	// judgmentPlaneConfigured records configuration, not runtime provider
	// health. A configured-but-unreachable engine still reports true so a
	// temporary outage degrades per turn instead of silently shrinking the tool
	// surface and reopening hidden LLM mutation authority.
	judgmentPlaneConfigured bool
	// metrics records bounded-call telemetry and per-task decision outcomes so
	// review/clarification rates stay measurable per decision task.
	// The zero value is a no-op recorder.
	metrics judgmentMetrics
	// turnTelemetryEnabled records one value row per Telegram turn.
	turnTelemetryEnabled         bool
	postGenerativeAutoConfirmOff bool
	now                          func() time.Time
	bot                          *Bot
}

type telegramUpdate struct {
	Message struct {
		MessageID      int64  `json:"message_id"`
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
	CallbackQuery *struct {
		ID   string `json:"id"`
		Data string `json:"data"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Message struct {
			MessageID int64 `json:"message_id"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
}

func NewProcessor(pool *pgxpool.Pool, llm Gateway) *Processor {
	return &Processor{pool: pool, gateway: llm, now: time.Now}
}

func (p *Processor) SetJudgment(engine judgment.Engine) {
	p.judgment = engine
	p.judgmentPlaneConfigured = engine != nil
}

// SetJudgmentMetrics wires bounded-call telemetry and decision outcomes.
func (p *Processor) SetJudgmentMetrics(metrics judgmentMetrics) { p.metrics = metrics }

func (p *Processor) SetBot(bot *Bot) { p.bot = bot }

// SetPostGenerativeAutoConfirm disables direct confirmation after generative extraction.
func (p *Processor) SetPostGenerativeAutoConfirm(enabled bool) {
	p.postGenerativeAutoConfirmOff = !enabled
}

// EnsureSourceEventFinal prevents a queue job from being acknowledged when a
// Telegram event is still unprocessed. This turns orphaned successful jobs
// into retryable failures instead of silently losing a user message.
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

func (p *Processor) Process(ctx context.Context, sourceEventID string) error {
	var householdID, payloadText, processingStatus, sourceType string
	err := p.pool.QueryRow(ctx, `
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
		ctx = withCallbackQuestion(ctx, payloadText)
		update.Message.MessageID = update.CallbackQuery.Message.MessageID
		update.Message.Chat.ID = update.CallbackQuery.Message.Chat.ID
		update.Message.From.ID = update.CallbackQuery.From.ID
		update.Message.ReplyToMessage = &struct {
			MessageID int64 `json:"message_id"`
		}{MessageID: update.CallbackQuery.Message.MessageID}
		update.Message.Text = callbackText(update.CallbackQuery.Data)
	}
	if update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0 {
		if err := p.renewExpiredReviewProjection(ctx, householdID, update); err != nil {
			return err
		}
	}
	if update.CallbackQuery != nil {
		if handled, err := p.processPendingConfirmCallback(ctx, sourceEventID, householdID, update, update.CallbackQuery.Data); handled {
			return err
		}
		if strings.HasPrefix(update.CallbackQuery.Data, "review:cat:") || strings.HasPrefix(update.CallbackQuery.Data, "review:catpage:") {
			return p.processReviewCategoryCallback(ctx, sourceEventID, householdID, update, update.CallbackQuery.Data)
		}
		if update.CallbackQuery.Data == "review:reprocess" || update.CallbackQuery.Data == "review:ignore" {
			if update.CallbackQuery.Data == "review:ignore" {
				if handled, err := p.ignoreBankReview(ctx, sourceEventID, householdID, update); handled {
					return err
				}
			}
			if handled, err := p.processDocumentReviewCallback(ctx, sourceEventID, householdID, update, update.CallbackQuery.Data); handled {
				return err
			}
			if handled, err := p.processFinancialEmailCallback(ctx, sourceEventID, householdID, update, update.CallbackQuery.Data); handled {
				return err
			}
			if handled, err := p.ignoreFinancialEmailFacts(ctx, sourceEventID, householdID, update); handled {
				return err
			}
		}
		if strings.HasPrefix(update.CallbackQuery.Data, "review:fe:") || strings.HasPrefix(update.CallbackQuery.Data, "review:fepage:") {
			if handled, err := p.processFinancialEmailCallback(ctx, sourceEventID, householdID, update, update.CallbackQuery.Data); handled {
				return err
			}
		}
		if strings.HasPrefix(update.CallbackQuery.Data, "review:bank:") {
			return p.processBankAccountCallback(ctx, sourceEventID, householdID, update, update.CallbackQuery.Data)
		}
		if strings.HasPrefix(update.CallbackQuery.Data, "review:invest:") {
			return p.processInvestmentCallback(ctx, sourceEventID, householdID, update, update.CallbackQuery.Data)
		}
		if strings.HasPrefix(update.CallbackQuery.Data, "review:salary:") {
			if handled, err := p.processPayslipPolicyCallback(ctx, sourceEventID, householdID, update, update.CallbackQuery.Data); handled {
				return err
			}
		}
		// A review:salary: callback that the policy lane did not handle (the review
		// is already resolved or the binding changed) is stale; answer it here so it
		// never falls through to the generic lanes as an unrecognized action.
		if strings.HasPrefix(update.CallbackQuery.Data, "review:salary:") {
			return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, staleActionMessage)
		}
		if strings.HasPrefix(update.CallbackQuery.Data, "review:") {
			if handled, err := p.processReviewDetailCallback(ctx, sourceEventID, householdID, update, update.CallbackQuery.Data); handled {
				return err
			}
			// Transfer-classification buttons (review:expense / :own / :household /
			// :asset) carry a bounded intent, not a free-form reply, so they continue
			// into the bound-review lane instead of being rejected as a stale action.
			switch update.CallbackQuery.Data {
			case "review:expense", "review:own", "review:household", "review:asset", "review:investment":
				if handled, err := p.processBoundReview(ctx, sourceEventID, householdID, update); handled {
					return err
				}
			}
		}
	}
	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Pesan kosong diabaikan.")
	}
	if sourceType == "TELEGRAM_CALLBACK" {
		return p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, staleActionMessage)
	}
	if isHelpCommand(text) {
		return p.finishWithoutTransaction(ctx, sourceEventID, "PROCESSED", update, helpMessage)
	}
	// Typed finance messages are answered by the conversational agent. Process
	// owns callbacks; production queues typed text as PROCESS_TELEGRAM_TEXT for
	// ProcessAgent, so a text event that arrives here is handed to it.
	return p.ProcessAgent(ctx, sourceEventID)
}

func strPtr(value string) *string { return &value }

type salaryChoice string

const (
	salaryChoicePrimary  salaryChoice = "PRIMARY"
	salaryChoiceOrdinary salaryChoice = "ORDINARY"
	salaryChoiceIgnore   salaryChoice = "IGNORE"
)

func salaryChoiceFromJudgment(choice string) salaryChoice { return salaryChoice(choice) }

func (p *Processor) executePendingSalaryChoice(ctx context.Context, householdID string, update telegramUpdate, sourceID string, choice salaryChoice) (bool, error) {
	if choice != salaryChoicePrimary && choice != salaryChoiceOrdinary && choice != salaryChoiceIgnore {
		return false, nil
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var id, tid, employer, period, payDate string
	err = tx.QueryRow(ctx, `SELECT id,transaction_id,employer,payroll_period::text,pay_date::text FROM salary_pending_choice WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now() FOR UPDATE`, householdID, update.Message.From.ID, update.Message.Chat.ID).Scan(&id, &tid, &employer, &period, &payDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if choice == salaryChoiceIgnore {
		_, err = tx.Exec(ctx, `UPDATE transaction SET status='VOIDED',updated_at=now() WHERE id=$1 AND household_id=$2`, tid, householdID)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE salary_pending_choice SET status='IGNORED',resolved_at=now() WHERE id=$1`, id)
		}
	} else {
		norm := strings.ToLower(strings.Join(strings.Fields(employer), " "))
		var sid string
		err = tx.QueryRow(ctx, `INSERT INTO salary_source(household_id,employer,normalized_employer,is_primary) VALUES($1,$2,$3,$4) ON CONFLICT(household_id,normalized_employer) WHERE active DO UPDATE SET is_primary=excluded.is_primary RETURNING id`, householdID, employer, norm, choice == salaryChoicePrimary).Scan(&sid)
		if err == nil {
			var salaryEventID string
			// currency must be listed: the SELECT supplies the 'IDR' literal between
			// net_pay and transaction_id, so omitting it misaligns every later value.
			err = tx.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,currency,transaction_id,status,source_event_id) SELECT $1,$2,$3::date,$4::date,t.amount,'IDR',t.id,'CONFIRMED',$5 FROM transaction t WHERE t.id=$6 AND t.household_id=$2 ON CONFLICT (salary_source_id,payroll_period) DO UPDATE SET transaction_id=EXCLUDED.transaction_id RETURNING id`, sid, householdID, period, payDate, sourceID, tid).Scan(&salaryEventID)
			if err == nil && choice == salaryChoicePrimary {
				_, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json,max_attempts) VALUES('GENERATE_CYCLE_RESIDUAL_REVIEW',jsonb_build_object('household_id',$1::uuid,'end_salary_event_id',$2::uuid),5) ON CONFLICT DO NOTHING`, householdID, salaryEventID)
			}
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE salary_pending_choice SET status=$2,resolved_at=now() WHERE id=$1`, id, string(choice))
			}
		}
	}
	if err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-salary-choice',parser_version='1' WHERE id=$1`, sourceID); err != nil {
		return true, err
	}
	msg := "Gaji disimpan sebagai pemasukan biasa."
	if choice == salaryChoicePrimary {
		msg = "Gaji utama disimpan dan menjadi acuan siklus keuangan."
	}
	if choice == salaryChoiceIgnore {
		msg = "Slip gaji diabaikan."
	}
	if err = enqueueReply(ctx, tx, update, msg); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

func stringPtr(v any) *string {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func (p *Processor) offerExistingEdit(ctx context.Context, householdID string, update telegramUpdate, sourceID string, value validatedExtraction, enqueueMessage bool) (bool, error) {
	if value.Merchant == "" {
		return false, nil
	}
	var transactionID, label string
	var existingAt time.Time
	err := p.pool.QueryRow(ctx, `SELECT id,COALESCE(counterparty_name,description,'Transaksi'),transaction_at FROM transaction WHERE household_id=$1 AND status<>'VOIDED' AND type='EXPENSE' AND amount=$2 AND transaction_at>=now()-interval '7 days' AND (counterparty_name ILIKE '%'||$3||'%' OR description ILIKE '%'||$3||'%') ORDER BY transaction_at DESC LIMIT 1`, householdID, value.Amount, value.Merchant).Scan(&transactionID, &label, &existingAt)
	if errors.Is(err, pgx.ErrNoRows) || existingAt.Equal(value.TransactionAt) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO telegram_pending_action(household_id,telegram_user_id,telegram_chat_id,transaction_id,proposed_transaction_at,status) VALUES($1,$2,$3,$4,$5,'PENDING') ON CONFLICT(telegram_user_id,telegram_chat_id) WHERE status='PENDING' DO UPDATE SET transaction_id=excluded.transaction_id,proposed_transaction_at=excluded.proposed_transaction_at,expires_at=now()+interval '5 minutes',created_at=now()`, householdID, update.Message.From.ID, update.Message.Chat.ID, transactionID, value.TransactionAt); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-edit-proposal',parser_version='1' WHERE id=$1`, sourceID); err != nil {
		return true, err
	}
	message := fmt.Sprintf("Saya menemukan %s · Rp%s. Ubah tanggalnya ke %s?", label, FormatIDR(value.Amount), formatIDDateTime(value.TransactionAt))
	if enqueueMessage {
		err = enqueueReplyMarkup(ctx, tx, update, message, pendingActionMarkup())
	}
	if err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

func (p *Processor) processPendingEdit(ctx context.Context, householdID string, update telegramUpdate, sourceID string, confirm bool) (bool, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var actionID, transactionID string
	var proposedAt *time.Time
	var proposedCategoryID *string
	var proposedDescription *string
	err = tx.QueryRow(ctx, `SELECT id,transaction_id,proposed_transaction_at,proposed_category_id,proposed_description FROM telegram_pending_action WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now() FOR UPDATE`, householdID, update.Message.From.ID, update.Message.Chat.ID).Scan(&actionID, &transactionID, &proposedAt, &proposedCategoryID, &proposedDescription)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	status := "CANCELLED"
	message := "Perubahan dibatalkan."
	if confirm {
		status = "CONFIRMED"
		message = "Perubahan transaksi berhasil disimpan."
		if proposedAt != nil {
			message = "Tanggal transaksi berhasil diubah ke " + formatIDDateTime(*proposedAt) + "."
		}
		if _, err = tx.Exec(ctx, `UPDATE transaction SET transaction_at=COALESCE($2,transaction_at),category_id=COALESCE($3,category_id),description=COALESCE(NULLIF($4,''),description),updated_at=now() WHERE id=$1 AND household_id=$5`, transactionID, proposedAt, proposedCategoryID, proposedDescription, householdID); err != nil {
			return true, err
		}
	}
	if confirm {
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) SELECT $1,'TELEGRAM',ti.user_id,'EDIT_TRANSACTION','transaction',$2,jsonb_build_object('transaction_at',$3::timestamptz,'category_id',$4::uuid,'description',$5) FROM telegram_identity ti WHERE ti.telegram_user_id=$6 AND ti.household_id=$1 AND ti.active`, householdID, transactionID, proposedAt, proposedCategoryID, proposedDescription, update.Message.From.ID); err != nil {
			return true, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE telegram_pending_action SET status=$2,resolved_at=now() WHERE id=$1`, actionID, status); err != nil {
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

func (p *Processor) processPendingBatch(ctx context.Context, householdID string, update telegramUpdate, sourceID string, confirm bool) (bool, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var batchID, raw string
	err = tx.QueryRow(ctx, `SELECT id,items_json::text FROM telegram_pending_batch WHERE household_id=$1 AND telegram_user_id=$2 AND telegram_chat_id=$3 AND status='PENDING' AND expires_at>now() FOR UPDATE`, householdID, update.Message.From.ID, update.Message.Chat.ID).Scan(&batchID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	status := "CANCELLED"
	msg := "Pencatatan batch dibatalkan."
	if confirm {
		var items []struct {
			Type, Amount, Merchant, CategorySlug, Description string
			TransactionAt                                     time.Time
		}
		if err = json.Unmarshal([]byte(raw), &items); err != nil {
			return true, err
		}
		var userID string
		if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
			return true, err
		}
		// Explicit human CONFIRM is the authority for the facts already shown to
		// the user. Structural checks and deterministic
		// merchant-category policy still run, and every check happens before any
		// canonical write.
		allowedCategories, err := p.categorySlugs(ctx, householdID)
		if err != nil {
			return true, err
		}
		for _, v := range items {
			if v.Type != "INCOME" && v.Type != "EXPENSE" {
				return true, fmt.Errorf("invalid batch type")
			}
			amount, ok := new(big.Int).SetString(v.Amount, 10)
			if !ok || amount.Sign() <= 0 || amount.String() != v.Amount || v.TransactionAt.IsZero() {
				return true, fmt.Errorf("invalid batch amount or time")
			}
			if (v.Type == "EXPENSE" && !contains(allowedCategories, v.CategorySlug)) || (v.CategorySlug != "" && !contains(allowedCategories, v.CategorySlug)) {
				if _, e := tx.Exec(ctx, `UPDATE telegram_pending_batch SET status='CANCELLED',resolved_at=now() WHERE id=$1`, batchID); e != nil {
					return true, e
				}
				if _, e := tx.Exec(ctx, `UPDATE source_event SET processing_status='NEEDS_REVIEW',parser_name='telegram-batch-confirmation',parser_version='1' WHERE id=$1`, sourceID); e != nil {
					return true, e
				}
				if e := enqueueReply(ctx, tx, update, "Daftar ini belum bisa dicatat karena ada kategori yang tidak ada. Kirim ulang dengan kategori pengeluaran yang tersedia."); e != nil {
					return true, e
				}
				return true, tx.Commit(ctx)
			}
		}
		for i, v := range items {
			var cat *string
			var cid string
			if v.CategorySlug != "" {
				if e := tx.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, householdID, v.CategorySlug).Scan(&cid); e == nil {
					cat = &cid
				}
			}
			prop := fmt.Sprintf("batch-%d", i)
			var pid, tid string
			if err = tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,description,confidence,proposal_status) VALUES($1,(SELECT source_event_id FROM telegram_pending_batch WHERE id=$2),$3,$4,$5,'IDR',$6,NULLIF($7,''),$8,NULLIF($9,''),1,'ACCEPTED') RETURNING id`, householdID, batchID, prop, v.Type, v.Amount, v.TransactionAt, v.Merchant, cat, v.Description).Scan(&pid); err != nil {
				return true, err
			}
			if err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,description,counterparty_name,source_confidence,classification_confidence,created_by_user_id,confirmed_at) VALUES($1,$2,'CONFIRMED',$3,'IDR',$4,$5,NULLIF($6,''),NULLIF($7,''),1,1,$8,now()) RETURNING id`, householdID, v.Type, v.Amount, v.TransactionAt, cat, v.Description, v.Merchant, userID).Scan(&tid); err != nil {
				return true, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'CONFIRM_PENDING_BATCH','transaction',$3,jsonb_build_object('batch_id',$4::uuid,'item_index',$5::integer))`, householdID, userID, tid, batchID, i); err != nil {
				return true, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,(SELECT source_event_id FROM telegram_pending_batch WHERE id=$2),'TELEGRAM_TEXT',1,jsonb_build_object('proposal_id',$3::uuid))`, tid, batchID, pid); err != nil {
				return true, err
			}
		}
		status = "CONFIRMED"
		msg = fmt.Sprintf("Berhasil mencatat %d transaksi.", len(items))
	}
	if _, err = tx.Exec(ctx, `UPDATE telegram_pending_batch SET status=$2,resolved_at=now() WHERE id=$1`, batchID, status); err != nil {
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

func (p *Processor) finishPendingAction(ctx context.Context, householdID string, update telegramUpdate, sourceID string, confirm bool) error {
	_, err := p.processPendingEdit(ctx, householdID, update, sourceID, confirm)
	return err
}

func (p *Processor) finishPendingBatch(ctx context.Context, householdID string, update telegramUpdate, sourceID string, confirm bool) error {
	_, err := p.processPendingBatch(ctx, householdID, update, sourceID, confirm)
	return err
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
			if !localTimePattern.MatchString(value) {
				return resolvedTransactionTime{}, fmt.Errorf("invalid local time")
			}
			_, _ = fmt.Sscanf(value, "%d:%d", &hour, &minute)
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

func (p *Processor) persistTransaction(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, value validatedExtraction, metadata gateway.Metadata, decision TransactionSemanticDecision) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID string
	if err := tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return fmt.Errorf("re-authorize Telegram identity: %w", err)
	}
	var categoryID *string
	if value.CategorySlug != "" {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, householdID, value.CategorySlug).Scan(&id); err == nil {
			categoryID = &id
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("validate category: %w", err)
		}
	}
	autoConfirm := decision.decisionAllowed() && (value.Type == "INCOME" || categoryID != nil)
	proposalStatus, transactionStatus := "NEEDS_REVIEW", "NEEDS_REVIEW"
	if autoConfirm {
		proposalStatus, transactionStatus = "ACCEPTED", "CONFIRMED"
	}
	metadataJSON, _ := json.Marshal(map[string]any{
		"gateway_model": metadata.Model, "input_tokens": metadata.InputTokens,
		"output_tokens": metadata.OutputTokens, "cost": metadata.Cost,
		"category_confidence": value.CategoryConfidence, "time_precision": value.TimePrecision,
		"time_period":     value.TimePeriod,
		"decision_source": decision.DecisionSource, "policy_version": decision.PolicyVersion,
	})
	if err := p.recordJudgmentDecision(ctx, tx, householdID, sourceEventID, decision, autoConfirm); err != nil {
		return err
	}
	var proposalID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO transaction_proposal
		(household_id,source_event_id,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,description,note,confidence,proposal_status,metadata_json)
		VALUES ($1,$2,$3,$4,'IDR',$5,NULLIF($6,''),$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12::jsonb)
		RETURNING id`, householdID, sourceEventID, value.Type, value.Amount, value.TransactionAt, value.Merchant, categoryID, value.Description, value.Note, value.Confidence, proposalStatus, string(metadataJSON)).Scan(&proposalID); err != nil {
		return fmt.Errorf("create transaction proposal: %w", err)
	}
	var transactionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO transaction
		(household_id,type,status,amount,currency,transaction_at,category_id,description,note,counterparty_name,source_confidence,classification_confidence,created_by_user_id,confirmed_at,auto_confirmed_at)
		VALUES ($1,$2,$3,$4,'IDR',$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,$11,$12,CASE WHEN $3='CONFIRMED' THEN now() END,CASE WHEN $13 THEN now() END)
		RETURNING id`, householdID, value.Type, transactionStatus, value.Amount, value.TransactionAt, categoryID, value.Description, value.Note, value.Merchant, value.Confidence, value.CategoryConfidence, userID, autoConfirm).Scan(&transactionID); err != nil {
		return fmt.Errorf("create reviewed transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence (transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES ($1,$2,'TELEGRAM_TEXT',$3,jsonb_build_object('proposal_id',$4::uuid))`, transactionID, sourceEventID, value.Confidence, proposalID); err != nil {
		return fmt.Errorf("attach Telegram evidence: %w", err)
	}
	sourceStatus := "NEEDS_REVIEW"
	if autoConfirm {
		sourceStatus = "PROCESSED"
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status=$2,parser_name='cloud-llm-gateway',parser_version='1' WHERE id=$1`, sourceEventID, sourceStatus); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (household_id,actor_type,action,entity_type,entity_id,after_json) VALUES ($1,'WORKER','CREATE_FROM_TELEGRAM','transaction',$2,jsonb_build_object('status',$3::text,'proposal_id',$4::uuid))`, householdID, transactionID, transactionStatus, proposalID); err != nil {
		return err
	}
	if autoConfirm {
		if err := reviewdomain.RefreshOpenCycleResiduals(ctx, tx, householdID, value.TransactionAt, userID); err != nil {
			return err
		}
	}
	if autoConfirm {
		message := value.ResponseMessage
		if message == "" {
			label := value.Merchant
			if label == "" {
				label = value.Description
			}
			if label == "" {
				label = "Transaksi"
			}
			kind := "Pengeluaran"
			if value.Type == "INCOME" {
				kind = "Pemasukan"
			}
			message = "✅ " + kind + " tercatat\n\n" + label + "\nRp" + FormatIDR(value.Amount)
			if value.TimePrecision == "APPROXIMATE" {
				message += "\n\nWaktu dicatat sekitar " + strings.ToLower(value.TimePeriod) + "."
			}
		}
		if err := enqueueReply(ctx, tx, update, message); err != nil {
			return err
		}
	} else {
		reviewType := "AMBIGUOUS_CATEGORY"
		if value.Merchant == "" {
			reviewType = "UNKNOWN_MERCHANT"
		}
		if err := EnqueueReviewRequest(ctx, tx, transactionID, reviewType, update.Message.Chat.ID, update.Message.MessageID, ReviewQuestion(value.Amount, value.Merchant)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *Processor) finishWithoutTransaction(ctx context.Context, sourceEventID, status string, update telegramUpdate, message string) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status=$2 WHERE id=$1`, sourceEventID, status); err != nil {
		return err
	}
	if err := enqueueReply(ctx, tx, update, message); err != nil {
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
	// Callback queries are acknowledged synchronously by the webhook after
	// durable capture. Do not enqueue a second ACK: Telegram rejects duplicate
	// answerCallbackQuery calls with HTTP 400 once the callback is answered or
	// expires. The reply job only sends the resulting user-facing message.
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
