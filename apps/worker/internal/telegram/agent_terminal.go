package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// terminalTextFailureMessage is what the household sees when a typed message
// could not be answered because the model was too slow; terminalTextErrorMessage
// when it failed for another reason. Each says what happened and what to do, so a
// failure is never silence, and a failure that is not slowness is not blamed on it.
const (
	terminalTextFailureMessage = "Richmod lagi lambat menjawab, jadi pertanyaanmu belum terjawab. Coba kirim ulang sebentar lagi, ya."
	terminalTextErrorMessage   = "Aku belum bisa menjawab pertanyaan ini. Coba tulis dengan kata lain, ya."
)

func terminalTextFailureCopy(timedOut bool) (message, reason string) {
	if timedOut {
		return terminalTextFailureMessage, "TIMEOUT"
	}
	return terminalTextErrorMessage, "ERROR"
}

// errModelPhase marks a failure of the conversational model call. runAgentLoop
// wraps it, so the retry rule keys on this value and not on message text.
var errModelPhase = errors.New("conversational model phase")

// IsModelTimeout reports whether a typed-message failure was the conversational
// model call running out of time.
func IsModelTimeout(err error) bool {
	if err == nil || !errors.Is(err, errModelPhase) {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded")
}

// terminalError marks a failure the queue must not retry. The queue treats any
// error with Permanent() == true as final.
type terminalError struct{ err error }

func (e terminalError) Error() string   { return e.err.Error() }
func (e terminalError) Unwrap() error   { return e.err }
func (e terminalError) Permanent() bool { return true }

// TerminalError wraps err so the queue stops retrying it.
func TerminalError(err error) error {
	if err == nil {
		return nil
	}
	return terminalError{err: err}
}

// TerminalTextFailureTx runs inside the transaction that marks a typed-message
// job FAILED, so the household cannot be left without an answer: either both
// happen or neither does. If the message is still unanswered it marks the
// source event FAILED and queues one plain reply; if the event already reached a
// final state (answered, ignored, sent to review) it does nothing.
//
// It only issues statements that cannot abort the transaction: a malformed ID or
// a missing event is a no-op, never an SQL error.
func (p *Processor) TerminalTextFailureTx(ctx context.Context, tx pgx.Tx, sourceEventID string, timedOut bool) error {
	if !reviewdomain.IsSourceEventID(sourceEventID) {
		return nil
	}
	var householdID, payloadText string
	err := tx.QueryRow(ctx, `
		SELECT s.household_id::text,p.payload_json::text
		FROM source_event s JOIN source_event_payload p ON p.source_event_id=s.id
		WHERE s.id=$1 AND s.source_type='TELEGRAM_TEXT'`, sourceEventID).Scan(&householdID, &payloadText)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load unanswered Telegram message: %w", err)
	}
	var update telegramUpdate
	if err := json.Unmarshal([]byte(payloadText), &update); err != nil || update.Message.Chat.ID == 0 {
		return nil
	}
	// Claim the event: only a message nobody has answered gets the notice, and
	// two workers cannot both send it.
	var claimed string
	err = tx.QueryRow(ctx, `UPDATE source_event SET processing_status='FAILED' WHERE id=$1 AND processing_status IN ('RECEIVED','PROCESSING') RETURNING id::text`, sourceEventID).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// Unconditional on purpose: an earlier progress notice must never suppress the
	// failure notice (the household would be left with "still working" and then
	// nothing). Only the progress notice checks for existing replies.
	message, reason := terminalTextFailureCopy(timedOut)
	if err := enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	// Leave a dismissable record in the Inbox, so the failure is visible after
	// the chat scrolls away and analytics has somewhere honest to point.
	return reviewdomain.RecordFailedSourceAction(ctx, tx, reviewdomain.FailedSource{
		HouseholdID: householdID, SourceEventID: sourceEventID, SourceType: "TELEGRAM_TEXT", Reason: reason,
	})
}

// terminalCallbackFailureMessage is what the household sees when a button tap
// could not be processed. It says what happened and what to do.
const terminalCallbackFailureMessage = "Tombol ini belum bisa diproses. Coba tekan lagi, ya, atau lanjutkan dari Kotak Tinjauan di web."

// TerminalCallbackFailureTx runs inside the transaction that marks a button-tap
// job FAILED. A tap that is still unfinished is finalized as FAILED, the
// household is told, the dead buttons are retired, and a dismissable item is left
// in the Inbox; a tap that already reached a final state is left alone.
func (p *Processor) TerminalCallbackFailureTx(ctx context.Context, tx pgx.Tx, sourceEventID string) error {
	if !reviewdomain.IsSourceEventID(sourceEventID) {
		return nil
	}
	var householdID, payloadText string
	err := tx.QueryRow(ctx, `
		SELECT s.household_id::text,p.payload_json::text
		FROM source_event s JOIN source_event_payload p ON p.source_event_id=s.id
		WHERE s.id=$1 AND s.source_type='TELEGRAM_CALLBACK'`, sourceEventID).Scan(&householdID, &payloadText)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load failed Telegram button tap: %w", err)
	}
	var update telegramUpdate
	if err := json.Unmarshal([]byte(payloadText), &update); err != nil || update.CallbackQuery == nil || update.CallbackQuery.Message.Chat.ID == 0 {
		return nil
	}
	// A callback carries its chat and message on the callback query; Process
	// copies them onto the message the same way before replying.
	update.Message.MessageID = update.CallbackQuery.Message.MessageID
	update.Message.Chat.ID = update.CallbackQuery.Message.Chat.ID
	update.Message.From.ID = update.CallbackQuery.From.ID
	var claimed string
	err = tx.QueryRow(ctx, `UPDATE source_event SET processing_status='FAILED' WHERE id=$1 AND processing_status IN ('RECEIVED','PROCESSING') RETURNING id::text`, sourceEventID).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := enqueueReply(ctx, tx, update, terminalCallbackFailureMessage); err != nil {
		return err
	}
	if err := enqueueRetireButtons(withCallbackQuestion(ctx, payloadText), tx, update, "Tidak berhasil diproses."); err != nil {
		return err
	}
	return reviewdomain.RecordFailedSourceAction(ctx, tx, reviewdomain.FailedSource{
		HouseholdID: householdID, SourceEventID: sourceEventID, SourceType: "TELEGRAM_CALLBACK", Reason: "ERROR",
	})
}
