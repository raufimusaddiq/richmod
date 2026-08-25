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
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
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
	bot := telegram.NewBot(os.Getenv("TELEGRAM_BOT_TOKEN"))
	jobs := queue.New(pool)
	hostname, _ := os.Hostname()
	workerID := hostname + ":" + strconv.Itoa(os.Getpid())

	logger.Info("worker started", "worker_id", workerID)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := processAvailable(ctx, logger, jobs, processor, bot, workerID); err != nil {
			logger.Error("job polling failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func processAvailable(ctx context.Context, logger *slog.Logger, jobs *queue.Queue, processor *telegram.Processor, bot *telegram.Bot, workerID string) error {
	for {
		job, found, err := jobs.Claim(ctx, workerID)
		if err != nil || !found {
			return err
		}
		err = processJob(ctx, processor, bot, job)
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

func processJob(ctx context.Context, processor *telegram.Processor, bot *telegram.Bot, job queue.Job) error {
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
		return bot.Send(ctx, payload)
	default:
		return fmt.Errorf("unsupported job type %q", job.Type)
	}
}
