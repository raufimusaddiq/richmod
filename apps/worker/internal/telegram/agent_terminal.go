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

var errModelPhase = errors.New("conversational model phase")

var errConversationalGatewayMissing = &gateway.UnavailableError{Err: errors.New("conversational gateway unavailable")}

func IsModelTimeout(err error) bool {
	if err == nil || !errors.Is(err, errModelPhase) {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded")
}

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

const MaxGatewayUnavailableAttempts = 2

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
	var claimed string
	err = tx.QueryRow(ctx, `UPDATE source_event SET processing_status='FAILED' WHERE id=$1 AND processing_status IN ('RECEIVED','PROCESSING') RETURNING id::text`, sourceEventID).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	message, reason := terminalTextFailureCopy(classifyTextFailure(cause))
	if err := enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	if err := insertTurnTx(ctx, tx, householdID, sourceEventID, update, "ASSISTANT", message, "", map[string]any{
		"failure": reason, "nothing_recorded": true,
	}); err != nil {
		return err
	}
	return reviewdomain.RecordFailedSourceAction(ctx, tx, reviewdomain.FailedSource{
		HouseholdID: householdID, SourceEventID: sourceEventID, SourceType: "TELEGRAM_TEXT", Reason: reason,
	})
}

const terminalCallbackFailureMessage = "Tombol ini belum bisa diproses. Coba tekan lagi, ya, atau lanjutkan dari Kotak Tinjauan di web."

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
