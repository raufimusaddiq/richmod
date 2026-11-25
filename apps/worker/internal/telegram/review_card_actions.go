package telegram

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

func markCallbackProcessed(ctx context.Context, tx pgx.Tx, sourceEventID string) error {
	_, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID)
	return err
}

type ProjectReviewPayload struct {
	HouseholdID  string `json:"household_id"`
	ReviewItemID string `json:"review_item_id"`
}

func DecodeProjectReviewPayload(raw json.RawMessage) (ProjectReviewPayload, error) {
	var payload ProjectReviewPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ProjectReviewPayload{}, err
	}
	if payload.HouseholdID == "" || payload.ReviewItemID == "" {
		return ProjectReviewPayload{}, errors.New("invalid review projection payload")
	}
	return payload, nil
}

func (p *Processor) ProjectReviewJob(ctx context.Context, payload ProjectReviewPayload) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := ProjectReviewItem(ctx, tx, payload.HouseholdID, payload.ReviewItemID, 0, "", 0); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	return tx.Commit(ctx)
}
