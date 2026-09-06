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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/bankemail"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	workerDocument "github.com/raufimusaddiq/richmod/apps/worker/internal/document"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	workerInsight "github.com/raufimusaddiq/richmod/apps/worker/internal/insight"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/queue"
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

	recordLLMCall := func(callCtx context.Context, metric gateway.CallMetric) {
		metricCtx, metricCancel := context.WithTimeout(context.WithoutCancel(callCtx), 2*time.Second)
		defer metricCancel()
		var cost any
		if metric.Cost != "" {
			if _, err := strconv.ParseFloat(metric.Cost, 64); err == nil {
				cost = metric.Cost
			}
		}
		if _, err := pool.Exec(metricCtx, `INSERT INTO llm_call(task,protocol,model,status,error_class,duration_ms,input_tokens,output_tokens,cost,attempt,call_kind,tool_name) VALUES($1,$2,NULLIF($3,''),$4,NULLIF($5,''),$6,$7,$8,$9::numeric,1,$10,NULLIF($11,''))`, metric.Task, metric.Protocol, metric.Model, metric.Status, metric.ErrorClass, metric.DurationMs, metric.InputTokens, metric.OutputTokens, cost, metric.CallKind, metric.ToolName); err != nil {
			logger.Warn("LLM metric write failed", "task", metric.Task, "error", err)
		}
	}
	llm := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), os.Getenv("LLM_MODEL_TELEGRAM_EXTRACT")).WithRecorder("TELEGRAM_NATIVE", recordLLMCall)
	processor := telegram.NewProcessor(pool, llm)
	documentLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), os.Getenv("LLM_MODEL_DOCUMENT_VISION")).WithRecorder("DOCUMENT_EXTRACTION", recordLLMCall)
	documentStorage, err := blob.NewFromEnv(os.Getenv("DOCUMENT_STORAGE_PATH"))
	if err != nil {
		return fmt.Errorf("configure document storage: %w", err)
	}
	documentProcessor := workerDocument.NewProcessorWithStorage(pool, documentLLM, documentStorage)
	insightLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), os.Getenv("LLM_MODEL_INSIGHTS")).WithRecorder("GENERATE_INSIGHT", recordLLMCall)
	insightProcessor := workerInsight.NewProcessor(pool, insightLLM)
	bankModel := os.Getenv("LLM_MODEL_BANK_EXTRACT")
	if bankModel == "" {
		bankModel = os.Getenv("LLM_MODEL_TELEGRAM_EXTRACT")
	}
	bankLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), bankModel).WithRecorder("BANK_EXTRACTION", recordLLMCall)
	bankProcessor := bankemail.NewProcessor(pool, bankemail.NewExtractor(bankLLM))
	bot := telegram.NewBot(os.Getenv("TELEGRAM_BOT_TOKEN"))
	imageProcessor := telegram.NewImageProcessorWithStorage(pool, bot, documentStorage)
	jobs := queue.New(pool)
	hostname, _ := os.Hostname()
	workerID := hostname + ":" + strconv.Itoa(os.Getpid())

	logger.Info("worker started", "worker_id", workerID)
	if err := documentProcessor.EvictTerminalCaches(ctx); err != nil {
		logger.Warn("attachment cache eviction failed", "error", err)
	}
	go maintainHeartbeat(ctx, logger, pool, workerID)
	// Keep callback and other interactive work on a reserved execution loop so
	// Long-running background jobs cannot delay Telegram button handling.
	chatWorkers := envPositiveInt("WORKER_CHAT_CONCURRENCY", 2, 4)
	for _, lane := range []struct {
		name     string
		interval time.Duration
		workers  int
	}{{"INTERACTIVE", 200 * time.Millisecond, 1}, {"CHAT", 200 * time.Millisecond, chatWorkers}, {"DEFAULT", time.Second, 1}, {"BACKGROUND", time.Second, 1}} {
		for i := 0; i < lane.workers; i++ {
			go runLaneLoop(ctx, logger, jobs, processor, imageProcessor, bankProcessor, documentProcessor, insightProcessor, bot, fmt.Sprintf("%s:%s:%d", workerID, strings.ToLower(lane.name), i+1), lane.name, lane.interval)
		}
	}
	maintenanceTicker := time.NewTicker(time.Minute)
	defer maintenanceTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-maintenanceTicker.C:
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

func envPositiveInt(name string, fallback, maximum int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < 1 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func pruneTerminalJobs(ctx context.Context, pool *pgxpool.Pool, batch int) (int64, error) {
	tag, err := pool.Exec(ctx, `WITH doomed AS (SELECT id FROM job WHERE (status='SUCCEEDED' AND finished_at < now()-interval '30 days') OR (status='FAILED' AND finished_at < now()-interval '90 days') ORDER BY finished_at LIMIT $1 FOR UPDATE SKIP LOCKED) DELETE FROM job j USING doomed d WHERE j.id=d.id`, batch)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func runLaneLoop(ctx context.Context, logger *slog.Logger, jobs *queue.Queue, processor *telegram.Processor, imageProcessor *telegram.ImageProcessor, bankProcessor *bankemail.Processor, documentProcessor *workerDocument.Processor, insightProcessor *workerInsight.Processor, bot *telegram.Bot, workerID, lane string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := processAvailable(ctx, logger, jobs, processor, imageProcessor, bankProcessor, documentProcessor, insightProcessor, bot, workerID, lane); err != nil && ctx.Err() == nil {
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

func processAvailable(ctx context.Context, logger *slog.Logger, jobs *queue.Queue, processor *telegram.Processor, imageProcessor *telegram.ImageProcessor, bankProcessor *bankemail.Processor, documentProcessor *workerDocument.Processor, insightProcessor *workerInsight.Processor, bot *telegram.Bot, workerID, lane string) error {
	processed := 0
	for {
		job, found, err := jobs.Claim(ctx, workerID, lane)
		if err != nil || !found {
			return err
		}
		processed++
		err = processJob(ctx, processor, imageProcessor, bankProcessor, documentProcessor, insightProcessor, bot, job)
		if err == nil {
			if finishErr := jobs.Succeed(ctx, job.ID); finishErr != nil {
				return fmt.Errorf("complete job: %w", finishErr)
			}
		} else {
			logger.Warn("job attempt failed", "job_id", job.ID, "type", job.Type, "attempt", job.Attempts, "error", err)
			if finishErr := jobs.Fail(ctx, job, err); finishErr != nil {
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

func processJob(ctx context.Context, processor *telegram.Processor, imageProcessor *telegram.ImageProcessor, bankProcessor *bankemail.Processor, documentProcessor *workerDocument.Processor, insightProcessor *workerInsight.Processor, bot *telegram.Bot, job queue.Job) error {
	budget := time.Duration(0)
	switch job.Type {
	case "PROCESS_TELEGRAM_TEXT":
		budget = 10 * time.Second
	case "PROCESS_BANK_EMAIL":
		budget = 45 * time.Second
	case "PROCESS_DOCUMENT", "PROCESS_PAYSLIP", "PROCESS_RECEIPT", "PROCESS_TRANSACTION_SCREENSHOT", "FETCH_TELEGRAM_IMAGE":
		budget = 60 * time.Second
	case "GENERATE_INSIGHT":
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
		if err := processor.Process(ctx, payload.SourceEventID); err != nil {
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
		messageID, err := bot.Send(ctx, payload)
		if err != nil {
			return err
		}
		if payload.ReviewRequestID != "" {
			if err := processor.BindReviewMessage(ctx, payload.ReviewRequestID, payload.ChatID, messageID); err != nil {
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
		if err := bot.Edit(ctx, payload); err != nil {
			// Financial state is already committed; send a repair notification instead
			// of retrying the mutation.
			_, sendErr := bot.Send(ctx, telegram.SendPayload{ChatID: payload.ChatID, Text: payload.Text})
			if sendErr != nil {
				return fmt.Errorf("edit Telegram message: %v; fallback send: %w", err, sendErr)
			}
		}
		return nil
	case "PROCESS_BANK_EMAIL":
		payload, err := bankemail.DecodePayload(job.Payload)
		if err != nil {
			return err
		}
		return bankProcessor.Process(ctx, payload)
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
		return insightProcessor.Process(ctx, payload.InsightID)
	default:
		return fmt.Errorf("unsupported job type %q", job.Type)
	}
}
