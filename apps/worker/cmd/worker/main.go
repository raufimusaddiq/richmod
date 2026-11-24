package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/bankemail"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	workerDocument "github.com/raufimusaddiq/richmod/apps/worker/internal/document"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/financialemail"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	workerInsight "github.com/raufimusaddiq/richmod/apps/worker/internal/insight"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/push"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/queue"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/residual"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) > 1 && os.Args[1] == "thumbnails" {
		if err := backfillThumbnails(logger, os.Args[2:]); err != nil {
			logger.Error("worker stopped", "error", err)
			os.Exit(1)
		}
		return
	}
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
	if !strings.Contains(databaseURL, "pool_max_conns") && poolConfig.MaxConns < minWorkerPoolConns {
		poolConfig.MaxConns = minWorkerPoolConns
	}
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
	recordLLMCall := func(callCtx context.Context, metric gateway.CallMetric) {
		metricCtx, metricCancel := context.WithTimeout(context.WithoutCancel(callCtx), 2*time.Second)
		defer metricCancel()
		var cost any
		if metric.Cost != "" {
			if _, err := strconv.ParseFloat(metric.Cost, 64); err == nil {
				cost = metric.Cost
			}
		}
		householdID := telegram.TurnHouseholdID(callCtx)
		if _, err := pool.Exec(metricCtx, `INSERT INTO llm_call(household_id,task,protocol,model,status,error_class,duration_ms,input_tokens,output_tokens,cost,attempt,call_kind,tool_name) VALUES(NULLIF($1,'')::uuid,$2,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,$8,$9,$10::numeric,1,$11,NULLIF($12,''))`, householdID, metric.Task, metric.Protocol, metric.Model, metric.Status, metric.ErrorClass, metric.DurationMs, metric.InputTokens, metric.OutputTokens, cost, metric.CallKind, metric.ToolName); err != nil {
			logger.Warn("LLM metric write failed", "task", metric.Task, "error", err)
			return
		}
		if metric.CallKind != "DECISION" {
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
	processor.SetPostGenerativeAutoConfirm(envEnabled("RICHMOD_AUTOCONFIRM_TELEGRAM"))
	documentLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), os.Getenv("LLM_MODEL_DOCUMENT_VISION")).WithRecorder("DOCUMENT_EXTRACTION", recordLLMCall)
	documentStorage, err := blob.NewFromEnv(os.Getenv("DOCUMENT_STORAGE_PATH"))
	if err != nil {
		return fmt.Errorf("configure document storage: %w", err)
	}
	documentProcessor := workerDocument.NewProcessorWithStorage(pool, documentLLM, documentStorage)
	documentProcessor.SetLogger(logger)
	documentProcessor.SetRowAutoConfirm(envEnabled("RICHMOD_AUTOCONFIRM_SCREENSHOT"))
	documentProcessor.SetReceiptAutoConfirm(envEnabled("RICHMOD_AUTOCONFIRM_RECEIPT"))
	insightLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), os.Getenv("LLM_MODEL_INSIGHTS")).WithRecorder("GENERATE_INSIGHT", recordLLMCall)
	insightProcessor := workerInsight.NewProcessor(pool, insightLLM)
	residualProcessor := residual.New(pool)
	bankModel := os.Getenv("LLM_MODEL_BANK_EXTRACT")
	if bankModel == "" {
		bankModel = os.Getenv("LLM_MODEL_TELEGRAM_EXTRACT")
	}
	bankLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), bankModel).WithRecorder("BANK_EXTRACTION", recordLLMCall)
	bankProcessor := bankemail.NewProcessor(pool, bankemail.NewExtractor(bankLLM))
	bankProcessor.SetCategoryAutoConfirm(envEnabled("RICHMOD_AUTOCONFIRM_BANK_CATEGORY"))
	financialModel := os.Getenv("LLM_MODEL_FINANCIAL_EMAIL")
	if financialModel == "" {
		financialModel = bankModel
	}
	financialLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), financialModel).WithRecorder("FINANCIAL_EMAIL_EXTRACTION", recordLLMCall)
	financialProcessor := financialemail.NewProcessor(pool, financialLLM)
	imageProcessor := telegram.NewImageProcessorWithStorage(pool, bot, documentStorage)
	jobs := queue.New(pool)
	hostname, _ := os.Hostname()
	workerID := hostname + ":" + strconv.Itoa(os.Getpid())

	logger.Info("worker started", "worker_id", workerID)
	var running sync.WaitGroup
	pushSender, pushErr := push.NewSender(pool, os.Getenv("VAPID_PRIVATE_KEY"), os.Getenv("VAPID_SUBJECT"), logger)
	if pushErr != nil {
		logger.Warn("push notifications disabled", "reason", pushErr.Error())
	} else {
		running.Go(func() { pushSender.Run(ctx) })
	}
	catchUpResidualReviews(ctx, logger, pool, 100)
	sweepOrphanedInsights(ctx, logger, pool)
	if err := documentProcessor.EvictTerminalCaches(ctx); err != nil {
		logger.Warn("attachment cache eviction failed", "error", err)
	}
	running.Go(func() { maintainHeartbeat(ctx, logger, pool, workerID) })
	for _, lane := range []struct {
		name     string
		interval time.Duration
		workers  int
	}{{"INTERACTIVE", 200 * time.Millisecond, 1}, {"DETERMINISTIC", 500 * time.Millisecond, deterministicLaneWorkers}, {"DEFAULT", time.Second, defaultLaneWorkers}, {"BACKGROUND", time.Second, 1}} {
		for i := 0; i < lane.workers; i++ {
			laneID := fmt.Sprintf("%s:%s:%d", workerID, strings.ToLower(lane.name), i+1)
			running.Go(func() {
				runLaneLoop(ctx, logger, jobs, processor, imageProcessor, bankProcessor, financialProcessor, documentProcessor, insightProcessor, residualProcessor, bot, laneID, lane.name, lane.interval)
			})
		}
	}
	maintenanceTicker := time.NewTicker(time.Minute)
	defer maintenanceTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			if !waitForLanes(&running, laneShutdownTimeout) {
				logger.Warn("worker loops did not stop before the shutdown deadline", "timeout", laneShutdownTimeout.String())
			}
			return nil
		case <-maintenanceTicker.C:
			if _, err := push.Expire(ctx, pool, logger, push.DefaultExpiry); err != nil {
				logger.Warn("push outbox expiry failed", "error", err)
			}
			if _, err := push.Purge(ctx, pool, logger, push.DefaultRetention); err != nil {
				logger.Warn("push retention purge failed", "error", err)
			}
			catchUpResidualReviews(ctx, logger, pool, 25)
			sweepOrphanedInsights(ctx, logger, pool)
			if expired, released, err := processor.SweepReviewProjections(ctx, 100); err != nil {
				logger.Warn("review projection sweep failed", "error", err)
			} else if expired > 0 || released > 0 {
				logger.Info("review projection sweep", "expired", expired, "released", released)
			}
			if created, err := reviewdomain.SweepFailedSources(ctx, pool, 50); err != nil {
				logger.Warn("failed source sweep failed", "error", err)
			} else if created > 0 {
				logger.Info("failed source sweep", "created", created)
			}
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
	if !reviewdomain.IsSourceEventID(metric.SourceEventID) {
		metric.SourceEventID = ""
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

func sweepOrphanedInsights(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool) {
	failed, err := workerInsight.SweepOrphanedPending(ctx, pool)
	if err != nil {
		if ctx.Err() == nil {
			logger.Warn("orphaned insight sweep failed", "error", err)
		}
		return
	}
	if failed > 0 {
		logger.Info("orphaned insight sweep", "failed", failed)
	}
}

func catchUpResidualReviews(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, limit int) {
	_, err := pool.Exec(ctx, residualCatchUpSQL, limit)
	if err != nil && ctx.Err() == nil {
		logger.Warn("residual catch-up failed", "error", err)
	}
}

const residualCatchUpSQL = `
WITH eligible AS (
	SELECT se.id,se.household_id,se.pay_date,
	       row_number() OVER (PARTITION BY se.household_id ORDER BY se.pay_date DESC,se.id) AS household_rank
	FROM salary_event se
	JOIN salary_source ss ON ss.id=se.salary_source_id AND ss.household_id=se.household_id
	WHERE se.status='CONFIRMED' AND ss.active AND ss.is_primary
	  AND EXISTS(SELECT 1 FROM salary_event prior JOIN salary_source ps ON ps.id=prior.salary_source_id AND ps.household_id=prior.household_id WHERE prior.household_id=se.household_id AND prior.status='CONFIRMED' AND ps.active AND ps.is_primary AND prior.pay_date<se.pay_date)
	  AND NOT EXISTS(SELECT 1 FROM cycle_residual_case c WHERE c.household_id=se.household_id AND c.end_salary_event_id=se.id)
	  AND NOT EXISTS(SELECT 1 FROM job j WHERE j.type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND j.status IN('PENDING','RUNNING','SUCCEEDED','FAILED') AND j.payload_json->>'end_salary_event_id'=se.id::text)
)
INSERT INTO job(type,household_id,payload_json,max_attempts)
SELECT 'GENERATE_CYCLE_RESIDUAL_REVIEW',e.household_id,jsonb_build_object('household_id',e.household_id,'end_salary_event_id',e.id),5
FROM eligible e
ORDER BY e.household_rank,e.pay_date DESC,e.id
LIMIT $1
ON CONFLICT DO NOTHING`

func envEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "0", "false", "off", "no", "disabled":
		return false
	default:
		return true
	}
}

func pruneTerminalJobs(ctx context.Context, pool *pgxpool.Pool, batch int) (int64, error) {
	var deleted int64
	err := pool.QueryRow(ctx, `WITH doomed AS (
		SELECT id FROM job
		WHERE type<>'GENERATE_CYCLE_RESIDUAL_REVIEW'
		  AND ((status='SUCCEEDED' AND finished_at < now()-interval '30 days')
		    OR (status='FAILED' AND finished_at < now()-interval '90 days'))
		ORDER BY finished_at LIMIT $1 FOR UPDATE SKIP LOCKED
	), deleted AS (
		DELETE FROM job j USING doomed d WHERE j.id=d.id RETURNING j.id
	), logs AS (
		DELETE FROM job_retry_log r USING deleted d WHERE r.job_id=d.id
	), phases AS (
		DELETE FROM intelligence_phase_telemetry WHERE created_at < now()-interval '30 days'
	)
	SELECT count(*) FROM deleted`, batch).Scan(&deleted)
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

const defaultLaneWorkers = 3

const deterministicLaneWorkers = 2

const minWorkerPoolConns = 16

func runLaneLoop(ctx context.Context, logger *slog.Logger, jobs *queue.Queue, processor *telegram.Processor, imageProcessor *telegram.ImageProcessor, bankProcessor *bankemail.Processor, financialProcessor *financialemail.Processor, documentProcessor *workerDocument.Processor, insightProcessor *workerInsight.Processor, residualProcessor *residual.Processor, bot *telegram.Bot, workerID, lane string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
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

func terminalStep(job queue.Job, cause error, processor *telegram.Processor, bankProcessor *bankemail.Processor, financialProcessor *financialemail.Processor, documentProcessor *workerDocument.Processor, insightProcessor *workerInsight.Processor) func(context.Context, pgx.Tx) error {
	switch job.Type {
	case "PROCESS_TELEGRAM_TEXT":
		id := ""
		if payload, err := telegram.DecodeProcessPayload(job.Payload); err == nil {
			id = payload.SourceEventID
		}
		return func(ctx context.Context, tx pgx.Tx) error {
			return processor.TerminalTextFailureTx(ctx, tx, id, cause)
		}
	case "PROCESS_TELEGRAM_CALLBACK":
		id := ""
		if payload, err := telegram.DecodeCallbackPayload(job.Payload); err == nil {
			id = payload.SourceEventID
		}
		return func(ctx context.Context, tx pgx.Tx) error { return processor.TerminalCallbackFailureTx(ctx, tx, id) }
	case "SEND_TELEGRAM_MESSAGE":
		payload, err := telegram.DecodeSendPayload(job.Payload)
		if err != nil {
			return nil
		}
		return func(ctx context.Context, tx pgx.Tx) error {
			return telegram.ReviewDeliveryFailedTx(ctx, tx, job.ID, payload, cause)
		}
	case "PROCESS_BANK_EMAIL":
		id := ""
		if payload, err := bankemail.DecodePayload(job.Payload); err == nil {
			id = payload.SourceEventID
		}
		return func(ctx context.Context, tx pgx.Tx) error { return bankProcessor.TerminalFailureTx(ctx, tx, id) }
	case "PROCESS_FINANCIAL_EMAIL":
		id := ""
		if payload, err := financialemail.DecodePayload(job.Payload); err == nil {
			id = payload.SourceEventID
		}
		return func(ctx context.Context, tx pgx.Tx) error { return financialProcessor.TerminalFailureTx(ctx, tx, id) }
	case "PROCESS_FINANCIAL_EMAIL_PREVIEW":
		id := ""
		if payload, err := financialemail.DecodePreviewPayload(job.Payload); err == nil {
			id = payload.PreviewID
		}
		return func(ctx context.Context, tx pgx.Tx) error {
			return financialProcessor.TerminalPreviewFailureTx(ctx, tx, id, cause)
		}
	case "PROCESS_DOCUMENT":
		id := ""
		if payload, err := workerDocument.DecodePayload(job.Payload); err == nil {
			id = payload.DocumentID
		}
		return func(ctx context.Context, tx pgx.Tx) error {
			return documentProcessor.TerminalClassificationFailureTx(ctx, tx, id, cause)
		}
	case "PROCESS_PAYSLIP", "PROCESS_RECEIPT", "PROCESS_TRANSACTION_SCREENSHOT":
		id := ""
		if payload, err := workerDocument.DecodePayload(job.Payload); err == nil {
			id = payload.DocumentID
		}
		return func(ctx context.Context, tx pgx.Tx) error {
			return documentProcessor.TerminalDownstreamFailureTx(ctx, tx, id)
		}
	case "GENERATE_INSIGHT":
		id := ""
		if payload, err := workerInsight.DecodePayload(job.Payload); err == nil {
			id = payload.InsightID
		}
		return func(ctx context.Context, tx pgx.Tx) error { return insightProcessor.TerminalFailureTx(ctx, tx, id) }
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
		if queue.Exhausted(job) {
			err = queue.LeaseExpiredError{Attempts: job.Attempts, MaxAttempts: job.MaxAttempts}
		} else {
			stopLease := jobs.KeepLease(ctx, job)
			err = processJob(ctx, processor, imageProcessor, bankProcessor, financialProcessor, documentProcessor, insightProcessor, residualProcessor, bot, job)
			stopLease()
		}
		if err == nil {
			if finishErr := jobs.Succeed(ctx, job); finishErr != nil {
				return fmt.Errorf("complete job: %w", finishErr)
			}
		} else {
			action, effective := settle(ctx.Err() != nil, job, err)
			switch action {
			case settleRelease:
				logger.Info("job released for shutdown", "job_id", job.ID, "type", job.Type)
				if releaseErr := jobs.Release(ctx, job); releaseErr != nil {
					return fmt.Errorf("release job: %w", releaseErr)
				}
			case settleDefer:
				logger.Warn("job deferred: AI gateway unavailable", "job_id", job.ID, "type", job.Type, "deferrals", job.Deferrals)
				if deferErr := jobs.Defer(ctx, job, effective); deferErr != nil {
					return fmt.Errorf("defer job: %w", deferErr)
				}
			default:
				logger.Warn("job attempt failed", "job_id", job.ID, "type", job.Type, "attempt", job.Attempts, "error", effective)
				onFinal := terminalStep(job, effective, processor, bankProcessor, financialProcessor, documentProcessor, insightProcessor)
				if finishErr := jobs.FailWithHook(ctx, job, effective, onFinal); finishErr != nil {
					return fmt.Errorf("reschedule job: %w", finishErr)
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
	ctx = telegram.WithHousehold(ctx, job.HouseholdID)
	switch job.Type {
	case "PROCESS_TELEGRAM_CALLBACK":
		payload, err := telegram.DecodeCallbackPayload(job.Payload)
		if err != nil {
			return err
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
		return processor.DeliverSend(ctx, bot, job.ID, job.HouseholdID, payload)
	case "EDIT_TELEGRAM_MESSAGE":
		payload, err := telegram.DecodeEditPayload(job.Payload)
		if err != nil {
			return err
		}
		return processor.DeliverEdit(ctx, bot, job.ID, job.HouseholdID, payload)
	case "PROJECT_TELEGRAM_REVIEW":
		payload, err := telegram.DecodeProjectReviewPayload(job.Payload)
		if err != nil {
			return err
		}
		return processor.ProjectReviewJob(ctx, payload)
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
