package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// terminalTextFailureMessage is what the household sees when a typed message
// could not be answered. It says what happened and what to do, so a failure is
// never silence.
const terminalTextFailureMessage = "Richmod lagi lambat menjawab, jadi pertanyaanmu belum terjawab. Coba kirim ulang sebentar lagi."

// IsModelTimeout reports whether a typed-message failure was the conversational
// model call running out of time.
func IsModelTimeout(err error) bool {
	if err == nil {
		return false
	}
	if !strings.Contains(err.Error(), "conversational model phase") {
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

// HandleTerminalTextFailure runs once a typed message will not be retried
// again. If the message is still unanswered it marks the source event FAILED and
// queues a plain reply in the same transaction; if the event already reached a
// final state (answered, ignored, sent to review) it does nothing.
func (p *Processor) HandleTerminalTextFailure(ctx context.Context, sourceEventID string) error {
	var payloadText string
	err := p.pool.QueryRow(ctx, `
		SELECT p.payload_json::text
		FROM source_event s JOIN source_event_payload p ON p.source_event_id=s.id
		WHERE s.id=$1 AND s.source_type='TELEGRAM_TEXT'`, sourceEventID).Scan(&payloadText)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load unanswered Telegram message: %w", err)
	}
	var update telegramUpdate
	if err := json.Unmarshal([]byte(payloadText), &update); err != nil {
		return fmt.Errorf("decode unanswered Telegram message: %w", err)
	}
	if update.Message.Chat.ID == 0 {
		return nil
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Claim the event: only a message nobody has answered gets the apology, and
	// two workers cannot both send it.
	var claimed string
	err = tx.QueryRow(ctx, `UPDATE source_event SET processing_status='FAILED' WHERE id=$1 AND processing_status IN ('RECEIVED','PROCESSING') RETURNING id::text`, sourceEventID).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := enqueueReply(ctx, tx, update, terminalTextFailureMessage); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
