package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

// terminalTextFailureMessage is what the household sees when a typed message
// could not be answered. It says what happened and what to do, so a failure is
// never silence.
const terminalTextFailureMessage = "Richmod lagi lambat menjawab, jadi pertanyaanmu belum terjawab. Coba kirim ulang sebentar lagi."

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

var sourceEventIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// TerminalTextFailureTx runs inside the transaction that marks a typed-message
// job FAILED, so the household cannot be left without an answer: either both
// happen or neither does. If the message is still unanswered it marks the
// source event FAILED and queues one plain reply; if the event already reached a
// final state (answered, ignored, sent to review) it does nothing.
//
// It only issues statements that cannot abort the transaction: a malformed ID or
// a missing event is a no-op, never an SQL error.
func (p *Processor) TerminalTextFailureTx(ctx context.Context, tx pgx.Tx, sourceEventID string) error {
	if !sourceEventIDPattern.MatchString(sourceEventID) {
		return nil
	}
	var payloadText string
	err := tx.QueryRow(ctx, `
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
	return enqueueReply(ctx, tx, update, terminalTextFailureMessage)
}

// HandleTerminalTextFailure applies TerminalTextFailureTx in its own
// transaction, for callers that are not already inside one.
func (p *Processor) HandleTerminalTextFailure(ctx context.Context, sourceEventID string) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := p.TerminalTextFailureTx(ctx, tx, sourceEventID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
