package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	workerDocument "github.com/raufimusaddiq/richmod/apps/worker/internal/document"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	workerGmail "github.com/raufimusaddiq/richmod/apps/worker/internal/gmail"
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

	llm := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), os.Getenv("LLM_MODEL_TELEGRAM_EXTRACT"))
	processor := telegram.NewProcessor(pool, llm)
	documentLLM := gateway.New(os.Getenv("LLM_GATEWAY_BASE_URL"), os.Getenv("LLM_GATEWAY_API_KEY"), os.Getenv("LLM_MODEL_DOCUMENT_VISION"))
	documentProcessor, err := workerDocument.NewProcessor(pool, documentLLM, os.Getenv("DOCUMENT_STORAGE_PATH"))
	if err != nil {
		return fmt.Errorf("configure document worker: %w", err)
	}
	gmailProcessor, err := workerGmail.NewProcessor(pool, llm, workerGmail.Config{
		OAuthClientPath: os.Getenv("GMAIL_OAUTH_CLIENT_PATH"),
		TokenKeyHex:     os.Getenv("GMAIL_TOKEN_ENCRYPTION_KEY"),
		Mailbox:         os.Getenv("GMAIL_MAILBOX"),
		TrustedSender:   os.Getenv("GMAIL_TRUSTED_SENDER"),
		PubSubTopic:     os.Getenv("GMAIL_PUBSUB_TOPIC"),
	})
	if err != nil {
		return fmt.Errorf("configure Gmail worker: %w", err)
	}
	bot := telegram.NewBot(os.Getenv("TELEGRAM_BOT_TOKEN"))
	jobs := queue.New(pool)
	hostname, _ := os.Hostname()
	workerID := hostname + ":" + strconv.Itoa(os.Getpid())

	logger.Info("worker started", "worker_id", workerID)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	maintenanceTicker := time.NewTicker(time.Minute)
	defer maintenanceTicker.Stop()
	if gmailProcessor != nil {
		if err := gmailProcessor.SeedRenewalJobs(ctx); err != nil {
			logger.Error("Gmail watch maintenance failed", "error", err)
		}
	}
	for {
		if err := processAvailable(ctx, logger, jobs, processor, gmailProcessor, documentProcessor, bot, workerID); err != nil {
			logger.Error("job polling failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-maintenanceTicker.C:
			if gmailProcessor != nil {
				if err := gmailProcessor.SeedRenewalJobs(ctx); err != nil {
					logger.Error("Gmail watch maintenance failed", "error", err)
				}
			}
		}
	}
}

func processAvailable(ctx context.Context, logger *slog.Logger, jobs *queue.Queue, processor *telegram.Processor, gmailProcessor *workerGmail.Processor, documentProcessor *workerDocument.Processor, bot *telegram.Bot, workerID string) error {
	for {
		job, found, err := jobs.Claim(ctx, workerID)
		if err != nil || !found {
			return err
		}
		err = processJob(ctx, processor, gmailProcessor, documentProcessor, bot, job)
		if err == nil {
			if finishErr := jobs.Succeed(ctx, job.ID); finishErr != nil {
				return fmt.Errorf("complete job: %w", finishErr)
			}
			continue
		}
		logger.Warn("job attempt failed", "job_id", job.ID, "type", job.Type, "attempt", job.Attempts, "error", err)
		if finishErr := jobs.Fail(ctx, job, err); finishErr != nil {
			return fmt.Errorf("reschedule job: %w", finishErr)
		}
	}
}

func processJob(ctx context.Context, processor *telegram.Processor, gmailProcessor *workerGmail.Processor, documentProcessor *workerDocument.Processor, bot *telegram.Bot, job queue.Job) error {
	switch job.Type {
	case "PROCESS_TELEGRAM_TEXT":
		payload, err := telegram.DecodeProcessPayload(job.Payload)
		if err != nil {
			return err
		}
		return processor.Process(ctx, payload.SourceEventID)
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
			return processor.BindReviewMessage(ctx, payload.ReviewRequestID, payload.ChatID, messageID)
		}
		return nil
	case "PROCESS_GMAIL_HISTORY":
		if gmailProcessor == nil {
			return fmt.Errorf("Gmail worker is not configured")
		}
		payload, err := workerGmail.DecodeHistoryPayload(job.Payload)
		if err != nil {
			return err
		}
		return gmailProcessor.ProcessHistory(ctx, payload)
	case "RENEW_GMAIL_WATCH":
		if gmailProcessor == nil {
			return fmt.Errorf("Gmail worker is not configured")
		}
		payload, err := workerGmail.DecodeRenewPayload(job.Payload)
		if err != nil {
			return err
		}
		return gmailProcessor.RenewWatch(ctx, payload.HouseholdID)
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
	default:
		return fmt.Errorf("unsupported job type %q", job.Type)
	}
}
