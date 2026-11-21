package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	workerDocument "github.com/raufimusaddiq/richmod/apps/worker/internal/document"
)

func backfillThumbnails(logger *slog.Logger, args []string) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	householdID := ""
	if len(args) > 0 {
		householdID = args[0]
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open database pool: %w", err)
	}
	defer pool.Close()
	storage, err := blob.NewFromEnv(os.Getenv("DOCUMENT_STORAGE_PATH"))
	if err != nil {
		return fmt.Errorf("configure document storage: %w", err)
	}
	processor := workerDocument.NewProcessorWithStorage(pool, nil, storage)
	processor.SetLogger(logger)
	if err := processor.BackfillThumbnails(ctx, householdID); err != nil {
		return fmt.Errorf("backfill document thumbnails: %w", err)
	}
	logger.Info("document thumbnails backfilled", "household", householdID)
	return nil
}
