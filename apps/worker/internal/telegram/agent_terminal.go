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

// Typed-message failure classes each get their own terminal copy because the household acts differently on each.
// Every copy says nothing was recorded: the terminal step only runs for events no mutation finalized.
type textFailureClass string

const (
	textFailureTimeout     textFailureClass = "TIMEOUT"
	textFailureUnavailable textFailureClass = "UNAVAILABLE"
	textFailureError       textFailureClass = "ERROR"
)

const (
	terminalTextFailureMessage     = "Richmod lagi lambat menjawab, jadi pesanmu belum terjawab dan belum ada yang dicatat. Coba kirim ulang sebentar lagi, ya."
	terminalTextUnavailableMessage = "AI Richmod belum juga bisa dihubungi, jadi pesanmu belum bisa diproses dan belum ada yang dicatat. Silakan kirim ulang, ya."
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

// errModelPhase marks a conversational model call failure; the retry rule keys on it, not on message text.
var errModelPhase = errors.New("conversational model phase")

// errConversationalGatewayMissing marks a processor without a conversational gateway: unavailability, not a
// bug in the message.
var errConversationalGatewayMissing = &gateway.UnavailableError{Err: errors.New("conversational gateway unavailable")}

func IsModelTimeout(err error) bool {
	if err == nil || !errors.Is(err, errModelPhase) {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded")
}

// IsGatewayUnavailable covers an unreachable, unconfigured, or refusing (HTTP 408/429/5xx) AI gateway.
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

// MaxGatewayUnavailableAttempts bounds retries of an undeferred unavailability: one quick retry, then the
// terminal notice.
const MaxGatewayUnavailableAttempts = 2

// TextFailurePolicy classifies a failed PROCESS_TELEGRAM_TEXT attempt: an unreachable gateway before any
// write is deferred (up to a day), else timeouts retry once and permanent failures are terminal.
func TextFailurePolicy(err error, attempts int) error {
	if err == nil || IsGatewayDeferral(err) {
		return err
	}
	switch {
	case IsModelTimeout(err) && attempts >= MaxModelTimeoutAttempts,
		IsGatewayUnavailable(err) && attempts >= MaxGatewayUnavailableAttempts,
		gateway.IsRejected(err):
		return TerminalError(err)
	}
	return err
}

// terminalError marks a failure the queue must not retry (Permanent() == true).
type terminalError struct{ err error }

func (e terminalError) Error() string   { return e.err.Error() }
func (e terminalError) Unwrap() error   { return e.err }
func (e terminalError) Permanent() bool { return true }

func TerminalError(err error) error {
	if err == nil {
		return nil
	}
	return terminalError{err: err}
}

// TerminalTextFailureTx runs in the transaction marking the job FAILED: an unanswered message gets its source
// event FAILED and one plain reply. It only issues statements that cannot abort the transaction.
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
	// Claim the event: only an unanswered message gets the notice, and two workers cannot both send it.
	var claimed string
	err = tx.QueryRow(ctx, `UPDATE source_event SET processing_status='FAILED' WHERE id=$1 AND processing_status IN ('RECEIVED','PROCESSING') RETURNING id::text`, sourceEventID).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// Unconditional on purpose: an earlier progress notice must never suppress the failure notice.
	message, reason := terminalTextFailureCopy(classifyTextFailure(cause))
	if err := enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	// Part of the conversation: otherwise the next turn sees an unanswered question and may act on it again.
	if err := insertTurnTx(ctx, tx, householdID, sourceEventID, update, "ASSISTANT", message, "", map[string]any{
		"failure": reason, "nothing_recorded": true,
	}); err != nil {
		return err
	}
	// Leave a dismissable Inbox record so the failure stays visible after the chat scrolls.
	return reviewdomain.RecordFailedSourceAction(ctx, tx, reviewdomain.FailedSource{
		HouseholdID: householdID, SourceEventID: sourceEventID, SourceType: "TELEGRAM_TEXT", Reason: reason,
	})
}

// terminalCallbackFailureMessage is what the household sees when a button tap could not be processed.
const terminalCallbackFailureMessage = "Tombol ini belum bisa diproses. Coba tekan lagi, ya, atau lanjutkan dari Kotak Tinjauan di web."

// TerminalCallbackFailureTx runs when a button-tap job is marked FAILED: finalizes the tap, notifies the
// household, retires dead buttons and leaves an Inbox item. A tap already final is left alone.
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
	// A callback carries chat and message on the callback query; Process copies them onto the message before replying.
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
