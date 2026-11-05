package telegram

// review_card_actions.go executes the card buttons whose kinds had no Telegram
// lane: IGNORE on a review that is not bound to a transaction, the income
// Benar/Ubah pair, leaving a cycle residual unallocated, and the source-bound
// transfer reconciliation chooser. Each binds the exact card by its stored
// Telegram message, re-checks the household member, and calls the canonical
// reviewdomain operation; none carries a canonical id in its callback data.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// cardActionCallback reports a callback this file owns.
func cardActionCallback(data string) bool {
	return data == "review:confirm" || data == "review:change" || data == "review:leave" || data == "review:xfer:new" || strings.HasPrefix(data, "review:xfer:merge:")
}

func (p *Processor) processCardActionCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) error {
	switch {
	case data == "review:confirm" || data == "review:change":
		return p.processIncomeConfirmCallback(ctx, sourceEventID, householdID, update, data == "review:confirm")
	case data == "review:leave":
		return p.processCycleLeaveCallback(ctx, sourceEventID, householdID, update)
	default:
		return p.processTransferReconciliationCallback(ctx, sourceEventID, householdID, update, data)
	}
}

// boundCardItem is the exact open review a tapped card belongs to.
type boundCardItem struct {
	RequestID, ItemID, UserID, ReviewType string
	Decision                              []byte
}

// bindOpenCardItem locks the open review item bound to the tapped card for the
// authorized household member who tapped it.
func bindOpenCardItem(ctx context.Context, tx pgx.Tx, householdID string, update telegramUpdate) (boundCardItem, error) {
	var item boundCardItem
	err := tx.QueryRow(ctx, `SELECT r.id::text,ri.id::text,ti.user_id::text,ri.review_type,COALESCE(ri.decision,'{}'::jsonb)
		FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN review_item ri ON ri.id=r.review_item_id
		JOIN telegram_identity ti ON ti.telegram_user_id=$4 AND ti.household_id=r.household_id AND ti.active
		JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND r.status='OPEN' AND r.expires_at>now() AND ri.status IN ('OPEN','PENDING_SEND')
		FOR UPDATE OF ri`, householdID, update.Message.Chat.ID, update.Message.MessageID, update.Message.From.ID).
		Scan(&item.RequestID, &item.ItemID, &item.UserID, &item.ReviewType, &item.Decision)
	return item, err
}

func markCallbackProcessed(ctx context.Context, tx pgx.Tx, sourceEventID string) error {
	_, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID)
	return err
}

// ignoreBoundReviewItem is Abaikan for every review that is not bound to a
// transaction and has no dedicated lane (a missing-amount screenshot row, a
// wealth observation, a source-bound transfer, ...). The canonical
// reviewdomain.IgnoreReview owns the transition and refuses a kind whose stored
// contract does not offer IGNORE.
func (p *Processor) ignoreBoundReviewItem(ctx context.Context, sourceEventID, householdID string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	item, err := bindOpenCardItem(ctx, tx, householdID, update)
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

// processIncomeConfirmCallback answers the income card. Benar confirms the
// incoming transaction as income through the canonical confirm; Ubah keeps the
// review open and offers the income/transfer classification on the same card.
func (p *Processor) processIncomeConfirmCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, confirm bool) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var reviewID, transactionID string
	err = tx.QueryRow(ctx, `SELECT r.id::text,t.id::text FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN transaction t ON t.id=r.transaction_id AND t.household_id=r.household_id
		JOIN review_item ri ON ri.id=r.review_item_id
		JOIN telegram_identity ti ON ti.telegram_user_id=$4 AND ti.household_id=r.household_id AND ti.active
		JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND r.status='OPEN' AND r.expires_at>now()
		AND ri.status IN ('OPEN','PENDING_SEND') AND t.status='NEEDS_REVIEW' AND t.type='INCOME'`, householdID, update.Message.Chat.ID, update.Message.MessageID, update.Message.From.ID).Scan(&reviewID, &transactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	if confirm {
		if err := tx.Rollback(ctx); err != nil {
			return err
		}
		return p.confirmIncomeTransferReview(ctx, sourceEventID, householdID, reviewID, transactionID, update)
	}
	facts, err := transactionReviewSummary(ctx, tx, householdID, transactionID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_DETAIL',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, reviewID); err != nil {
		return err
	}
	if err := markCallbackProcessed(ctx, tx, sourceEventID); err != nil {
		return err
	}
	text := reviewdomain.TelegramCardText("🟡 Pemasukan atau transfer?", facts, "", "Pilih Penghasilan jika ini pemasukan, atau Rekening sendiri/Household jika ini dana milik sendiri.")
	if err := enqueueReviewUpdateWithMarkup(ctx, tx, reviewID, update, text, incomeClassificationMarkup()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// processCycleLeaveCallback is the cycle card's one-tap LEAVE_UNALLOCATED. The
// shared cycle operation revalidates the basis first, so a stale card refreshes
// instead of closing on an old residual.
func (p *Processor) processCycleLeaveCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	item, err := bindOpenCardItem(ctx, tx, householdID, update)
	if errors.Is(err, pgx.ErrNoRows) {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	var caseID string
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(cycle_residual_case_id::text,''),COALESCE(decision->'allowedActions','[]'::jsonb) ? 'LEAVE_UNALLOCATED' FROM review_item WHERE id=$1 AND household_id=$2`, item.ItemID, householdID).Scan(&caseID, &allowed); err != nil {
		return err
	}
	if caseID == "" || !allowed {
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

// processTransferReconciliationCallback answers the source-bound transfer
// chooser: merge into the n-th stored candidate or record a new transfer. The
// candidate list was stored with the card when it was rendered, and the shared
// reconciliation revalidates the chosen candidate against the open case.
func (p *Processor) processTransferReconciliationCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	item, err := bindOpenCardItem(ctx, tx, householdID, update)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && item.ReviewType != "TRANSFER_CLASSIFICATION") {
		return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return err
	}
	command := reviewdomain.TransferReconciliationCommand{HouseholdID: householdID, ActorUserID: item.UserID, ReviewItemID: item.ItemID, Action: "CONFIRM_NEW_TRANSFER", ActorType: "TELEGRAM"}
	message := "✅ Transfer baru sudah dicatat."
	if strings.HasPrefix(data, "review:xfer:merge:") {
		index, parseErr := strconv.Atoi(strings.TrimPrefix(data, "review:xfer:merge:"))
		if parseErr != nil || index < 0 || index > 9 {
			return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
		}
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT COALESCE(context_json->'transfer_candidates','[]'::jsonb) FROM review_conversation WHERE review_request_id=$1`, item.RequestID).Scan(&raw); err != nil {
			return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
		}
		var candidates []string
		if json.Unmarshal(raw, &candidates) != nil || index >= len(candidates) {
			return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
		}
		command.Action, command.CandidateID = "MERGE_EXISTING", candidates[index]
		message = "✅ Transfer sudah digabung dengan catatan yang ada."
	}
	if _, err := reviewdomain.ReconcileTransfer(ctx, tx, command); err != nil {
		if errors.Is(err, reviewdomain.ErrTransferCaseUnavailable) {
			return finishStaleReviewCallback(ctx, tx, sourceEventID, update)
		}
		return err
	}
	if err := markCallbackProcessed(ctx, tx, sourceEventID); err != nil {
		return err
	}
	if err := enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ProjectReviewPayload is one PROJECT_TELEGRAM_REVIEW job: a review item a
// surface without a Telegram renderer (the API, for a reopened review) asks the
// worker to project through the shared projection path.
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

// ProjectReviewJob projects one review item. It is idempotent: a closed item
// or an already rendered projection sends nothing.
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
