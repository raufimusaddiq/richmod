package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// Typed-message failure classes. Each has its own terminal copy, because the
// household acts differently on each: a slow assistant is worth a retry soon, an
// unreachable one is not (and simple expenses still record deterministically), and
// a system fault is never the household's wording. Every copy says nothing was
// recorded: the terminal step only runs for an event no mutation finalized.
type textFailureClass string

const (
	textFailureTimeout     textFailureClass = "TIMEOUT"
	textFailureUnavailable textFailureClass = "UNAVAILABLE"
	textFailureError       textFailureClass = "ERROR"
)

const (
	terminalTextFailureMessage     = "Richmod lagi lambat menjawab, jadi pesanmu belum terjawab dan belum ada yang dicatat. Coba kirim ulang sebentar lagi, ya."
	terminalTextUnavailableMessage = "Asisten AI Richmod sedang tidak bisa dihubungi, jadi pesanmu belum kuproses dan belum ada yang dicatat. Coba kirim ulang beberapa menit lagi, ya. Pengeluaran sederhana seperti “kopi 25rb” tetap bisa kucatat dulu untuk ditinjau."
	terminalTextErrorMessage       = "Ada kendala di sistem Richmod, jadi pesanmu belum kuproses dan belum ada yang dicatat. Coba kirim ulang sebentar lagi, ya."
)

func terminalTextFailureCopy(class textFailureClass) (message, reason string) {
	switch class {
	case textFailureTimeout:
		return terminalTextFailureMessage, "TIMEOUT"
	case textFailureUnavailable:
		return terminalTextUnavailableMessage, "UNAVAILABLE"
	}
	return terminalTextErrorMessage, "ERROR"
}

// errModelPhase marks a failure of the conversational model call. runAgentLoop
// wraps it, so the retry rule keys on this value and not on message text.
var errModelPhase = errors.New("conversational model phase")

// errConversationalGatewayMissing is a processor wired without a conversational
// gateway; it is unavailability, not a bug in the household's message.
var errConversationalGatewayMissing = &gateway.UnavailableError{Err: errors.New("conversational gateway unavailable")}

// IsModelTimeout reports whether a typed-message failure was the conversational
// model call running out of time.
func IsModelTimeout(err error) bool {
	if err == nil || !errors.Is(err, errModelPhase) {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded")
}

// IsGatewayUnavailable reports whether a typed-message failure was the AI
// gateway being unreachable, unconfigured, or refusing work (HTTP 408/429/5xx).
func IsGatewayUnavailable(err error) bool {
	return err != nil && gateway.IsUnavailable(err)
}

func classifyTextFailure(err error) textFailureClass {
	switch {
	case IsModelTimeout(err):
		return textFailureTimeout
	case IsGatewayUnavailable(err):
		return textFailureUnavailable
	}
	return textFailureError
}

// MaxGatewayUnavailableAttempts bounds retries while the gateway is
// unreachable: one quick retry covers a blip, a second would only make the
// household wait for the same failure.
const MaxGatewayUnavailableAttempts = 2

// TextFailurePolicy is the retry taxonomy for one failed PROCESS_TELEGRAM_TEXT
// attempt. It returns err unchanged when the queue may retry it, or wrapped as
// terminal when it must not:
//
//   - a model timeout is retried once (MaxModelTimeoutAttempts);
//   - an unreachable gateway is retried once (MaxGatewayUnavailableAttempts);
//   - a gateway rejection (HTTP 4xx) and a permanent validation failure are
//     never retried: the same request fails the same way;
//   - anything else (database, network to Postgres) is transient and keeps the
//     job's normal attempt budget.
//
// Model-recoverable failures never reach here: they are returned to the model
// as tool results inside the turn (agentRecoverable).
func TextFailurePolicy(err error, attempts int) error {
	if err == nil {
		return nil
	}
	switch {
	case IsModelTimeout(err) && attempts >= MaxModelTimeoutAttempts,
		IsGatewayUnavailable(err) && attempts >= MaxGatewayUnavailableAttempts,
		gateway.IsRejected(err):
		return TerminalError(err)
	}
	return err
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
func (p *Processor) TerminalTextFailureTx(ctx context.Context, tx pgx.Tx, sourceEventID string, cause error) error {
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
	message, reason := terminalTextFailureCopy(classifyTextFailure(cause))
	if err := enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	// The failure is part of the conversation: without it the next turn would see
	// an unanswered question and might answer or act on it again.
	if err := insertTurnTx(ctx, tx, householdID, sourceEventID, update, "ASSISTANT", message, "", map[string]any{
		"failure": reason, "nothing_recorded": true,
	}); err != nil {
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
