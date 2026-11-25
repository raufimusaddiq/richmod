package telegram

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

func cardActionCallback(data string) bool {
	return data == "review:leave"
}

func (p *Processor) processCardActionCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) error {
	return p.processCycleLeaveCallback(ctx, sourceEventID, householdID, update)
}

type boundCardItem struct {
	RequestID, ItemID, UserID, ReviewType string
	Decision                              []byte
}

func (item boundCardItem) allows(action string) bool {
	var stored struct {
		AllowedActions []string `json:"allowedActions"`
	}
	_ = json.Unmarshal(item.Decision, &stored)
	return contains(stored.AllowedActions, action)
}

func bindOpenCardItem(ctx context.Context, tx pgx.Tx, householdID string, update telegramUpdate, messageID int64) (boundCardItem, error) {
	var item boundCardItem
	err := tx.QueryRow(ctx, `SELECT r.id::text,ri.id::text,ti.user_id::text,ri.review_type,COALESCE(ri.decision,'{}'::jsonb)
		FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN review_item ri ON ri.id=r.review_item_id
		JOIN telegram_identity ti ON ti.telegram_user_id=$4 AND ti.household_id=r.household_id AND ti.active
		JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND')
		FOR UPDATE OF ri`, householdID, update.Message.Chat.ID, messageID, update.Message.From.ID).
		Scan(&item.RequestID, &item.ItemID, &item.UserID, &item.ReviewType, &item.Decision)
	return item, err
}

func markCallbackProcessed(ctx context.Context, tx pgx.Tx, sourceEventID string) error {
	_, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID)
	return err
}

func (p *Processor) ignoreBoundReviewItem(ctx context.Context, sourceEventID, householdID string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	item, err := bindOpenCardItem(ctx, tx, householdID, update, update.Message.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	err = reviewdomain.IgnoreReview(ctx, tx, reviewdomain.IgnoreCommand{HouseholdID: householdID, ActorUserID: item.UserID, ReviewItemID: item.ItemID, ActorType: "TELEGRAM"})
	switch {
	case errors.Is(err, reviewdomain.ErrAlreadyResolved), errors.Is(err, reviewdomain.ErrIgnoreTransactionBound):
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	case errors.Is(err, reviewdomain.ErrIgnoreNotAllowed):
		if err := markCallbackProcessed(ctx, tx, sourceEventID); err != nil {
			return err
		}
		if err := enqueueReply(ctx, tx, update, "Tinjauan ini tidak bisa diabaikan. Pilih salah satu jawaban di kartunya."); err != nil {
			return err
		}
		return tx.Commit(ctx)
	case err != nil:
		return err
	}
	if err := markCallbackProcessed(ctx, tx, sourceEventID); err != nil {
		return err
	}
	if err := enqueueReply(ctx, tx, update, "Diabaikan. Buktinya tetap tersimpan, tidak ada transaksi yang dicatat."); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) processCycleLeaveCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	item, err := bindOpenCardItem(ctx, tx, householdID, update, update.Message.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	var caseID string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(cycle_residual_case_id::text,'') FROM review_item WHERE id=$1 AND household_id=$2`, item.ItemID, householdID).Scan(&caseID); err != nil {
		return err
	}
	if caseID == "" || !item.allows("LEAVE_UNALLOCATED") {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	outcome, err := reviewdomain.ApplyCycleResidual(ctx, tx, reviewdomain.CycleResidualCommand{
		HouseholdID: householdID, CaseID: caseID, ReviewItemID: item.ItemID, RequestID: item.RequestID,
		Action: "LEAVE_UNALLOCATED", ActorUserID: item.UserID,
	})
	if errors.Is(err, reviewdomain.ErrCycleCaseNotFound) {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'CYCLE_RESIDUAL_LEAVE_UNALLOCATED','cycle_residual_case',$3,jsonb_build_object('residual_idr',$4::text,'outcome',$5::text))`, householdID, item.UserID, caseID, outcome.Residual, string(outcome.Outcome)); err != nil {
		return err
	}
	if err := markCallbackProcessed(ctx, tx, sourceEventID); err != nil {
		return err
	}
	message := "✅ Sisa salary cycle dibiarkan tidak dialokasikan."
	switch outcome.Outcome {
	case reviewdomain.CycleStaleRefreshed:
		message = "Sisa salary cycle berubah menjadi Rp" + FormatIDR(outcome.Residual) + ". Kartunya sudah diperbarui; pilih lagi."
	case reviewdomain.CycleStaleNotApplicable:
		message = "Sisa salary cycle sudah tidak ada lagi. Tinjauan ditutup."
	}
	if err := enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
