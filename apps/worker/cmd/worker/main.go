package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/bankemail"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	workerDocument "github.com/raufimusaddiq/richmod/apps/worker/internal/document"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/financialemail"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	workerInsight "github.com/raufimusaddiq/richmod/apps/worker/internal/insight"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment/systemone"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/queue"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/residual"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	poolConfig.ConnConfig.RuntimeParams["timezone"] = "Asia/Jakarta"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("open database pool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	recordPhase := func(callCtx context.Context, metric gateway.CallMetric) {
		phaseCtx, phaseCancel := context.WithTimeout(context.WithoutCancel(callCtx), 2*time.Second)
		defer phaseCancel()
		phase := phaseMetric(metric, telegram.TurnSourceEventID(callCtx))
		var householdID any = telegram.TurnHouseholdID(callCtx)
		if phase.SourceEventID != "" {
			householdID = nil
		}
		if _, err := pool.Exec(phaseCtx, `INSERT INTO intelligence_phase_telemetry(household_id,source_event_id,capability,purpose,semantic_dimensions,answered_dimensions,residual_dimensions,policy_version,model,latency_ms,outcome,accepted_dimensions_at_entry) VALUES(COALESCE(NULLIF($1::text,'')::uuid,(SELECT household_id FROM source_event WHERE id=NULLIF($2::text,'')::uuid)),NULLIF($2::text,'')::uuid,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12)`, householdID, nullUUID(phase.SourceEventID), phase.Capability, phase.Purpose, phase.Dimensions, phase.AnsweredDimensions, phase.ResidualDimensions, phase.PolicyVersion, phase.Model, phase.DurationMs, phaseOutcome(phase.Status), phase.AcceptedDimensions); err != nil {
			logger.Warn("intelligence phase write failed", "task", phase.Purpose, "error", err)
		}
	}
	recordIntelligencePhase := func(callCtx context.Context, metric systemone.Metric) {
		recordPhase(callCtx, gateway.CallMetric{Capability: "JEV", Purpose: metric.Purpose, PolicyVersion: metric.PolicyVersion, Dimensions: metric.Dimensions, AnsweredDimensions: metric.AnsweredDimensions, AcceptedDimensions: metric.AcceptedDimensions, SourceEventID: metric.SourceEventID, Model: metric.Model, Status: metric.Status, ErrorClass: metric.ErrorClass, DurationMs: metric.DurationMs})
	}
	recordLLMCall := func(callCtx context.Context, metric gateway.CallMetric) {
		metricCtx, metricCancel := context.WithTimeout(context.WithoutCancel(callCtx), 2*time.Second)
		defer metricCancel()
		var cost any
		if metric.Cost != "" {
			if _, err := strconv.ParseFloat(metric.Cost, 64); err == nil {
				cost = metric.Cost
			}
		}
		// The turn context carries the household, so a bounded-call row is
		// attributable and the per-household value scoreboard can read it.
		householdID := telegram.TurnHouseholdID(callCtx)
		if _, err := pool.Exec(metricCtx, `INSERT INTO llm_call(household_id,task,protocol,model,status,error_class,duration_ms,input_tokens,output_tokens,cost,attempt,call_kind,tool_name) VALUES(NULLIF($1,'')::uuid,$2,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,$8,$9,$10::numeric,1,$11,NULLIF($12,''))`, householdID, metric.Task, metric.Protocol, metric.Model, metric.Status, metric.ErrorClass, metric.DurationMs, metric.InputTokens, metric.OutputTokens, cost, metric.CallKind, metric.ToolName); err != nil {
			logger.Warn("LLM metric write failed", "task", metric.Task, "error", err)
			return
		}
		if metric.CallKind != "JUDGMENT" && metric.CallKind != "DECISION" {
			if metric.Capability == "" {
				metric.Capability = "GENERATIVE"
			}
			recordPhase(callCtx, metric)
		}
	}
	llm := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), os.Getenv("LLM_MODEL_TELEGRAM_EXTRACT")).WithRecorder("TELEGRAM_NATIVE", recordLLMCall)
	bot := telegram.NewBot(os.Getenv("TELEGRAM_BOT_TOKEN"))
	processor := telegram.NewProcessor(pool, llm)
	processor.SetBot(bot)
	func() {
		commandsCtx, cancelCommands := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCommands()
		if err := bot.SetCommands(commandsCtx); err != nil {
			logger.Warn("telegram command menu not registered", "error", err)
		}
	}()
	// The judgment plane owns bounded semantic mutation authority (ADR-038), so
	// production must not be able to disable it by leaving the model unset.
	// Non-production environments opt out explicitly with JUDGMENT_MODE=disabled-dev.
	judgmentMode := strings.ToLower(strings.TrimSpace(os.Getenv("JUDGMENT_MODE")))
	judgmentModel := strings.TrimSpace(os.Getenv("JUDGMENT_MODEL"))
	var judgmentEngine systemone.InstrumentedEngine
	if judgmentMode != "disabled-dev" {
		if err := requireJudgmentModel(judgmentMode, judgmentModel); err != nil {
			return err
		}
		judgmentEngine = systemone.InstrumentedEngine{Engine: systemone.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), judgmentModel, envDuration("JUDGMENT_TIMEOUT_MS", 3*time.Second, 30*time.Second)), Record: recordIntelligencePhase}
		processor.SetJudgment(judgmentEngine)
		// Task-attributed bounded telemetry: the client records the transport call,
		// and the decision counter records what policy did with the answer, which
		// is what makes review rate per decision task measurable.
		processor.SetJudgmentMetrics(telegram.JudgmentMetricsFor(recordLLMCall))
		// Turn-level value: records which lane resolved each turn and how many
		// bounded generative decisions the judgment plane replaced.
		processor.SetTurnTelemetry(true)
	}
	processor.SetPostGenerativeAutoConfirm(envEnabled("RICHMOD_AUTOCONFIRM_TELEGRAM"))
	documentLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), os.Getenv("LLM_MODEL_DOCUMENT_VISION")).WithRecorder("DOCUMENT_EXTRACTION", recordLLMCall)
	documentStorage, err := blob.NewFromEnv(os.Getenv("DOCUMENT_STORAGE_PATH"))
	if err != nil {
		return fmt.Errorf("configure document storage: %w", err)
	}
	documentProcessor := workerDocument.NewProcessorWithStorage(pool, documentLLM, documentStorage)
	// Operational kill-switch: an operator must be able to park screenshot
	// rows in review without a deploy. Unset keeps auto-confirm on.
	documentProcessor.SetRowAutoConfirm(envEnabled("RICHMOD_AUTOCONFIRM_SCREENSHOT"))
	documentProcessor.SetReceiptAutoConfirm(envEnabled("RICHMOD_AUTOCONFIRM_RECEIPT"))
	if judgmentEngine.Record != nil {
		// Row-level category rulings let a clear screenshot row reach the ledger
		// without a review; the bounded plane, not generative confidence, is what
		// authorises that write.
		documentProcessor.SetVerifier(judgmentEngine)
	}
	insightLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), os.Getenv("LLM_MODEL_INSIGHTS")).WithRecorder("GENERATE_INSIGHT", recordLLMCall)
	insightProcessor := workerInsight.NewProcessor(pool, insightLLM)
	residualProcessor := residual.New(pool)
	bankModel := os.Getenv("LLM_MODEL_BANK_EXTRACT")
	if bankModel == "" {
		bankModel = os.Getenv("LLM_MODEL_TELEGRAM_EXTRACT")
	}
	bankLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), bankModel).WithRecorder("BANK_EXTRACTION", recordLLMCall)
	bankProcessor := bankemail.NewProcessor(pool, bankemail.NewExtractor(bankLLM))
	// Each auto-confirm source has its own operational kill-switch. The
	// default is on; an operator disables one source without touching the others.
	bankProcessor.SetCategoryAutoConfirm(envEnabled("RICHMOD_AUTOCONFIRM_BANK_CATEGORY"))
	// Evidence-channel semantic verification: the bounded plane rules on claims Go
	// already holds, so neither email channel trusts generative self-confidence as
	// its semantic gate (ADR-038).
	if judgmentEngine.Record != nil {
		bankProcessor.SetVerifier(judgmentEngine)
	}
	financialModel := os.Getenv("LLM_MODEL_FINANCIAL_EMAIL")
	if financialModel == "" {
		financialModel = bankModel
	}
	financialLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), financialModel).WithRecorder("FINANCIAL_EMAIL_EXTRACTION", recordLLMCall)
	financialProcessor := financialemail.NewProcessor(pool, financialLLM)
	if judgmentEngine.Record != nil {
		financialProcessor.SetVerifier(judgmentEngine)
	}
	imageProcessor := telegram.NewImageProcessorWithStorage(pool, bot, documentStorage)
	jobs := queue.New(pool)
	hostname, _ := os.Hostname()
	workerID := hostname + ":" + strconv.Itoa(os.Getpid())

	logger.Info("worker started", "worker_id", workerID)
	catchUpResidualReviews(ctx, logger, pool, 100)
	if err := documentProcessor.EvictTerminalCaches(ctx); err != nil {
		logger.Warn("attachment cache eviction failed", "error", err)
	}
	go maintainHeartbeat(ctx, logger, pool, workerID)
	// Keep callback and other interactive work on a reserved execution loop so
	// long-running jobs cannot delay Telegram button handling. Free-text Telegram
	// turns run on the single DEFAULT loop, which keeps one person's messages in
	// order (the CHAT lane of ADR-032 is retired).
	for _, lane := range []struct {
		name     string
		interval time.Duration
		workers  int
	}{{"INTERACTIVE", 200 * time.Millisecond, 1}, {"DEFAULT", time.Second, 1}, {"BACKGROUND", time.Second, 1}} {
		for i := 0; i < lane.workers; i++ {
			go runLaneLoop(ctx, logger, jobs, processor, imageProcessor, bankProcessor, financialProcessor, documentProcessor, insightProcessor, residualProcessor, bot, fmt.Sprintf("%s:%s:%d", workerID, strings.ToLower(lane.name), i+1), lane.name, lane.interval)
		}
	}
	maintenanceTicker := time.NewTicker(time.Minute)
	defer maintenanceTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-maintenanceTicker.C:
			catchUpResidualReviews(ctx, logger, pool, 25)
			if err := documentProcessor.EvictTerminalCaches(ctx); err != nil {
				logger.Warn("attachment cache eviction failed", "error", err)
			}
			if deleted, err := pruneTerminalJobs(ctx, pool, 500); err != nil {
				logger.Warn("job retention cleanup failed", "error", err)
			} else if deleted > 0 {
				logger.Info("job retention cleanup", "deleted", deleted)
			}
		}
	}
}

func phaseMetric(metric gateway.CallMetric, fallbackSourceEventID string) gateway.CallMetric {
	if metric.Dimensions == nil {
		metric.Dimensions = []string{}
	}
	if metric.AnsweredDimensions == nil {
		metric.AnsweredDimensions = []string{}
	}
	if metric.ResidualDimensions == nil {
		metric.ResidualDimensions = []string{}
	}
	if metric.Capability == "" {
		metric.Capability = "GENERATIVE"
	}
	if metric.SourceEventID == "" {
		metric.SourceEventID = fallbackSourceEventID
	}
	if metric.Purpose == "" {
		metric.Purpose = "OTHER_BOUNDED"
	}
	return metric
}

func nullUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func phaseOutcome(status string) string {
	if status == "SUCCEEDED" {
		return "SUCCEEDED"
	}
	return "FAILED"
}

// requireJudgmentModel enforces the production invariant that bounded mutation
// semantics cannot be silently disabled by omitting the model. Only an explicit
// opt-out outside production is allowed.
func requireJudgmentModel(mode, model string) error {
	if mode == "disabled-dev" || strings.TrimSpace(model) != "" {
		return nil
	}
	return fmt.Errorf("JUDGMENT_MODEL is required for mutation semantics (set JUDGMENT_MODE=disabled-dev to opt out outside production)")
}

func catchUpResidualReviews(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, limit int) {
	_, err := pool.Exec(ctx, `INSERT INTO job(type,payload_json,max_attempts) SELECT 'GENERATE_CYCLE_RESIDUAL_REVIEW',jsonb_build_object('household_id',se.household_id,'end_salary_event_id',se.id),5 FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id WHERE se.status='CONFIRMED' AND ss.active AND ss.is_primary AND EXISTS(SELECT 1 FROM salary_event prior JOIN salary_source ps ON ps.id=prior.salary_source_id WHERE prior.household_id=se.household_id AND prior.status='CONFIRMED' AND ps.active AND ps.is_primary AND prior.pay_date<se.pay_date) AND NOT EXISTS(SELECT 1 FROM cycle_residual_case c WHERE c.household_id=se.household_id AND c.end_salary_event_id=se.id) AND NOT EXISTS(SELECT 1 FROM job j WHERE j.type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND j.status IN('PENDING','RUNNING') AND j.payload_json->>'end_salary_event_id'=se.id::text) ORDER BY se.pay_date DESC LIMIT $1`, limit)
	if err != nil && ctx.Err() == nil {
		logger.Warn("residual catch-up failed", "error", err)
	}
}

// envEnabled reads a kill-switch. Any value other than an explicit negative
// keeps the switch enabled, so an unset or mistyped variable never silently
// changes auto-confirm behavior.
func envEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "0", "false", "off", "no", "disabled":
		return false
	default:
		return true
	}
}

func envDuration(name string, fallback, maximum time.Duration) time.Duration {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	duration := time.Duration(value) * time.Millisecond
	if duration > maximum {
		return maximum
	}
	return duration
}

func pruneTerminalJobs(ctx context.Context, pool *pgxpool.Pool, batch int) (int64, error) {
	tag, err := pool.Exec(ctx, `WITH doomed AS (SELECT id FROM job WHERE (status='SUCCEEDED' AND finished_at < now()-interval '30 days') OR (status='FAILED' AND finished_at < now()-interval '90 days') ORDER BY finished_at LIMIT $1 FOR UPDATE SKIP LOCKED) DELETE FROM job j USING doomed d WHERE j.id=d.id`, batch)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func runLaneLoop(ctx context.Context, logger *slog.Logger, jobs *queue.Queue, processor *telegram.Processor, imageProcessor *telegram.ImageProcessor, bankProcessor *bankemail.Processor, financialProcessor *financialemail.Processor, documentProcessor *workerDocument.Processor, insightProcessor *workerInsight.Processor, residualProcessor *residual.Processor, bot *telegram.Bot, workerID, lane string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := processAvailable(ctx, logger, jobs, processor, imageProcessor, bankProcessor, financialProcessor, documentProcessor, insightProcessor, residualProcessor, bot, workerID, lane); err != nil && ctx.Err() == nil {
			logger.Error("job polling failed", "lane", lane, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func maintainHeartbeat(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, workerID string) {
	startedAt := time.Now()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	_, _ = pool.Exec(ctx, `DELETE FROM worker_heartbeat WHERE last_seen_at<now()-interval '7 days'`)
	for {
		if _, err := pool.Exec(ctx, `
			INSERT INTO worker_heartbeat(worker_id,started_at,last_seen_at)
			VALUES($1,$2,now())
			ON CONFLICT(worker_id) DO UPDATE SET last_seen_at=now(),updated_at=now()`, workerID, startedAt); err != nil && ctx.Err() == nil {
			logger.Error("worker heartbeat failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// terminalStep returns what must happen in the same transaction that marks a job
// FAILED for its last attempt: finalize the source event, tell the household when
// there is a chat to tell, and leave a dismissable item in the Inbox. Other job
// types have no step. A payload that cannot be decoded yields an empty ID, which
// every step treats as a no-op.
func terminalStep(job queue.Job, cause error, processor *telegram.Processor, bankProcessor *bankemail.Processor) func(context.Context, pgx.Tx) error {
	switch job.Type {
	case "PROCESS_TELEGRAM_TEXT":
		id := ""
		if payload, err := telegram.DecodeProcessPayload(job.Payload); err == nil {
			id = payload.SourceEventID
		}
		return func(ctx context.Context, tx pgx.Tx) error {
			return processor.TerminalTextFailureTx(ctx, tx, id, telegram.IsModelTimeout(cause))
		}
	case "PROCESS_TELEGRAM_CALLBACK":
		id := ""
		if payload, err := telegram.DecodeCallbackPayload(job.Payload); err == nil {
			id = payload.SourceEventID
		}
		return func(ctx context.Context, tx pgx.Tx) error { return processor.TerminalCallbackFailureTx(ctx, tx, id) }
	case "PROCESS_BANK_EMAIL":
		id := ""
		if payload, err := bankemail.DecodePayload(job.Payload); err == nil {
			id = payload.SourceEventID
		}
		return func(ctx context.Context, tx pgx.Tx) error { return bankProcessor.TerminalFailureTx(ctx, tx, id) }
	}
	return nil
}

func processAvailable(ctx context.Context, logger *slog.Logger, jobs *queue.Queue, processor *telegram.Processor, imageProcessor *telegram.ImageProcessor, bankProcessor *bankemail.Processor, financialProcessor *financialemail.Processor, documentProcessor *workerDocument.Processor, insightProcessor *workerInsight.Processor, residualProcessor *residual.Processor, bot *telegram.Bot, workerID, lane string) error {
	processed := 0
	for {
		job, found, err := jobs.Claim(ctx, workerID, lane)
		if err != nil || !found {
			return err
		}
		processed++
		err = processJob(ctx, processor, imageProcessor, bankProcessor, financialProcessor, documentProcessor, insightProcessor, residualProcessor, bot, job)
		if err == nil {
			if finishErr := jobs.Succeed(ctx, job.ID); finishErr != nil {
				return fmt.Errorf("complete job: %w", finishErr)
			}
		} else {
			logger.Warn("job attempt failed", "job_id", job.ID, "type", job.Type, "attempt", job.Attempts, "error", err)
			// A model timeout repeats with the same prompt and cap, so a typed
			// message is retried once and then stopped.
			if job.Type == "PROCESS_TELEGRAM_TEXT" && telegram.IsModelTimeout(err) && job.Attempts >= telegram.MaxModelTimeoutAttempts {
				err = telegram.TerminalError(err)
			}
			// A typed message that will not be retried again must not end in
			// silence, so the notice is queued in the same transaction that marks
			// the job FAILED.
			onFinal := terminalStep(job, err, processor, bankProcessor)
			if finishErr := jobs.FailWithHook(ctx, job, err, onFinal); finishErr != nil {
				return fmt.Errorf("reschedule job: %w", finishErr)
			}
			if job.Type == "PROCESS_DOCUMENT" && job.Attempts >= job.MaxAttempts {
				payload, decodeErr := workerDocument.DecodePayload(job.Payload)
				if decodeErr != nil {
					logger.Error("terminal document failure payload invalid", "job_id", job.ID, "error", decodeErr)
				} else if failureErr := documentProcessor.HandleTerminalFailure(ctx, payload.DocumentID, err); failureErr != nil {
					logger.Error("terminal document failure handling failed", "job_id", job.ID, "document_id", payload.DocumentID, "error", failureErr)
				}
			}
		}
		if processed >= 25 {
			return nil
		}
	}
}

func processJob(ctx context.Context, processor *telegram.Processor, imageProcessor *telegram.ImageProcessor, bankProcessor *bankemail.Processor, financialProcessor *financialemail.Processor, documentProcessor *workerDocument.Processor, insightProcessor *workerInsight.Processor, residualProcessor *residual.Processor, bot *telegram.Bot, job queue.Job) error {
	budget := time.Duration(0)
	switch job.Type {
	case "PROCESS_TELEGRAM_TEXT":
		budget = telegram.TextJobBudget
	case "PROCESS_BANK_EMAIL", "PROCESS_FINANCIAL_EMAIL", "PROCESS_FINANCIAL_EMAIL_PREVIEW":
		budget = 45 * time.Second
	case "PROCESS_DOCUMENT", "PROCESS_PAYSLIP", "PROCESS_RECEIPT", "PROCESS_TRANSACTION_SCREENSHOT", "FETCH_TELEGRAM_IMAGE":
		budget = 60 * time.Second
	case "GENERATE_INSIGHT":
		budget = workerInsight.Timeout + 5*time.Second
	case "GENERATE_CYCLE_RESIDUAL_REVIEW":
		budget = 30 * time.Second
	}
	if budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}
	switch job.Type {
	case "PROCESS_TELEGRAM_CALLBACK":
		payload, err := telegram.DecodeCallbackPayload(job.Payload)
		if err != nil {
			return err
		}
		ackCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		ackErr := bot.AnswerCallback(ackCtx, payload.CallbackQueryID)
		cancel()
		if ackErr != nil {
			slog.Default().Warn("Telegram callback ACK failed", "error", ackErr)
		}
		if err := processor.Process(ctx, payload.SourceEventID); err != nil {
			return err
		}
		return processor.EnsureSourceEventFinal(ctx, payload.SourceEventID)
	case "PROCESS_TELEGRAM_TEXT":
		payload, err := telegram.DecodeProcessPayload(job.Payload)
		if err != nil {
			return err
		}
		if err := processor.ProcessAgent(ctx, payload.SourceEventID); err != nil {
			return err
		}
		return processor.EnsureSourceEventFinal(ctx, payload.SourceEventID)
	case "FETCH_TELEGRAM_IMAGE":
		payload, err := telegram.DecodeImagePayload(job.Payload)
		if err != nil {
			return err
		}
		return imageProcessor.Process(ctx, payload)
	case "SEND_TELEGRAM_MESSAGE":
		payload, err := telegram.DecodeSendPayload(job.Payload)
		if err != nil {
			return err
		}
		// A review card resolved (or cancelled/expired) between enqueue and
		// send must not arrive as a fresh live card; skip the send but still answer a
		// pending callback so the client spinner clears.
		if payload.ReviewRequestID != "" {
			open, err := processor.ReviewProjectionOpen(ctx, payload.ReviewRequestID)
			if err != nil {
				return err
			}
			if !open {
				if payload.CallbackQueryID != "" {
					return bot.AnswerCallback(ctx, payload.CallbackQueryID)
				}
				return nil
			}
		}
		messageID, err := bot.Send(ctx, payload)
		if err != nil {
			return err
		}
		if payload.ReviewRequestID != "" {
			if err := processor.BindReviewMessage(ctx, payload.ReviewRequestID, payload.ChatID, messageID, payload.Text); err != nil {
				return err
			}
		}
		if payload.BindMerchantLearningRequestID != "" {
			if err := processor.BindMerchantLearningMessage(ctx, payload.BindMerchantLearningRequestID, payload.ChatID, messageID); err != nil {
				return err
			}
		}
		if payload.BindDocumentID != "" {
			if err := processor.BindEvidenceMessage(ctx, payload.ChatID, messageID, payload.BindDocumentID); err != nil {
				return err
			}
		}
		if payload.CallbackQueryID != "" {
			return bot.AnswerCallback(ctx, payload.CallbackQueryID)
		}
		return nil
	case "EDIT_TELEGRAM_MESSAGE":
		payload, err := telegram.DecodeEditPayload(job.Payload)
		if err != nil {
			return err
		}
		// An edit of a review card queued while it was open must not restore its
		// buttons after the request closed; RETIRE_TELEGRAM_REVIEW_CARD owns it now.
		if payload.ReviewRequestID != "" {
			live, err := processor.ReviewCardLive(ctx, payload.ReviewRequestID)
			if err != nil {
				return err
			}
			if !live {
				return nil
			}
		}
		if err := bot.Edit(ctx, payload); err != nil {
			// Financial state is already committed; send a repair notification instead
			// of retrying the mutation.
			_, sendErr := bot.Send(ctx, telegram.SendPayload{ChatID: payload.ChatID, Text: payload.Text})
			if sendErr != nil {
				return fmt.Errorf("edit Telegram message: %v; fallback send: %w", err, sendErr)
			}
			return nil
		}
		// The edit is delivered; a failure to remember its text only costs a
		// later retirement its closure note, so it must not retry the edit.
		if err := processor.RecordReviewCardText(ctx, payload.ChatID, payload.MessageID, payload.Text); err != nil {
			slog.Default().Warn("record edited Telegram review card text failed", "job_id", job.ID, "error", err)
		}
		return nil
	case "RETIRE_TELEGRAM_REVIEW_CARD":
		payload, err := telegram.DecodeRetireCardPayload(job.Payload)
		if err != nil {
			return err
		}
		return processor.RetireReviewCard(ctx, bot, payload)
	case "PROCESS_BANK_EMAIL":
		payload, err := bankemail.DecodePayload(job.Payload)
		if err != nil {
			return err
		}
		return bankProcessor.Process(ctx, payload)
	case "PROCESS_FINANCIAL_EMAIL":
		payload, err := financialemail.DecodePayload(job.Payload)
		if err != nil {
			return err
		}
		return financialProcessor.Process(ctx, payload)
	case "PROCESS_FINANCIAL_EMAIL_PREVIEW":
		payload, err := financialemail.DecodePreviewPayload(job.Payload)
		if err != nil {
			return err
		}
		return financialProcessor.ProcessPreview(ctx, payload)
	case "COMPLETE_BANK_REVIEW":
		payload, err := bankemail.DecodePayload(job.Payload)
		if err != nil {
			return err
		}
		return bankProcessor.Complete(ctx, payload)
	case "PROCESS_DOCUMENT":
		payload, err := workerDocument.DecodePayload(job.Payload)
		if err != nil {
			return err
		}
		return documentProcessor.Process(ctx, payload.DocumentID)
	case "PROCESS_PAYSLIP":
		payload, err := workerDocument.DecodePayload(job.Payload)
		if err != nil {
			return err
		}
		return documentProcessor.ProcessPayslip(ctx, payload.DocumentID)
	case "PROCESS_RECEIPT":
		payload, err := workerDocument.DecodePayload(job.Payload)
		if err != nil {
			return err
		}
		return documentProcessor.ProcessReceipt(ctx, payload.DocumentID)
	case "PROCESS_TRANSACTION_SCREENSHOT":
		payload, err := workerDocument.DecodePayload(job.Payload)
		if err != nil {
			return err
		}
		return documentProcessor.ProcessScreenshot(ctx, payload.DocumentID)
	case "GENERATE_INSIGHT":
		payload, err := workerInsight.DecodePayload(job.Payload)
		if err != nil {
			return err
		}
		return insightProcessor.Process(ctx, payload.InsightID, job.Attempts >= job.MaxAttempts)
	case "GENERATE_CYCLE_RESIDUAL_REVIEW":
		payload, err := residual.Decode(job.Payload)
		if err != nil {
			return err
		}
		return residualProcessor.Generate(ctx, payload)
	default:
		return fmt.Errorf("unsupported job type %q", job.Type)
	}
}
