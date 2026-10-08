package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/queue"
)

// The progress notice is a reply to the user's message. It must be sent once
// per message, never after an answer already exists, and never for another
// message's turn.
func TestProgressNoticeIsIdempotentAndNeverFollowsAnAnswer(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)

	update := func(messageID int64) telegramUpdate {
		var u telegramUpdate
		u.Message.MessageID, u.Message.Chat.ID, u.Message.From.ID = messageID, f.chatID, f.chatID
		return u
	}
	notices := func(messageID int64) int {
		var count int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'reply_to_message_id'=$2 AND payload_json->>'text'=$3`,
			fmt.Sprint(f.chatID), fmt.Sprint(messageID), progressNoticeMessage).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	anyReply := func(messageID int64) int {
		var count int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'reply_to_message_id'=$2`,
			fmt.Sprint(f.chatID), fmt.Sprint(messageID)).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	// First call queues the notice; a retry of the same turn does not repeat it.
	if err := processor.enqueueProgressNotice(f.ctx, update(11)); err != nil {
		t.Fatal(err)
	}
	if err := processor.enqueueProgressNotice(f.ctx, update(11)); err != nil {
		t.Fatal(err)
	}
	if got := notices(11); got != 1 {
		t.Fatalf("the notice must be queued once per message, got %d", got)
	}

	// A different message gets its own notice.
	if err := processor.enqueueProgressNotice(f.ctx, update(12)); err != nil {
		t.Fatal(err)
	}
	if got := notices(12); got != 1 {
		t.Fatalf("each message gets its own notice, got %d", got)
	}

	// If the answer is already queued, the notice must not land after it.
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',13::bigint,'text','jawaban'))`, f.chatID); err != nil {
		t.Fatal(err)
	}
	if err := processor.enqueueProgressNotice(f.ctx, update(13)); err != nil {
		t.Fatal(err)
	}
	if got := notices(13); got != 0 {
		t.Fatalf("a notice must not follow an existing answer, got %d", got)
	}
	if got := anyReply(13); got != 1 {
		t.Fatalf("only the answer should exist for that message, got %d replies", got)
	}

	// Nothing to reply to: a no-op, not an error.
	var empty telegramUpdate
	if err := processor.enqueueProgressNotice(f.ctx, empty); err != nil {
		t.Fatalf("a message without a chat must be a no-op, got %v", err)
	}
}

// A progress notice that was already sent must never swallow the terminal
// failure notice: the household would be left with "still working" and then
// nothing.
func TestProgressNoticeDoesNotSuppressTheTerminalFailureNotice(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	jobs := queue.New(f.pool)

	event := f.event(21)
	var update telegramUpdate
	update.Message.MessageID, update.Message.Chat.ID, update.Message.From.ID = 21, f.chatID, f.chatID
	if err := processor.enqueueProgressNotice(f.ctx, update); err != nil {
		t.Fatal(err)
	}
	var notices int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'text'=$2`,
		fmt.Sprint(f.chatID), progressNoticeMessage).Scan(&notices); err != nil || notices != 1 {
		t.Fatalf("setup: the progress notice must exist, got %d (%v)", notices, err)
	}

	job := f.job(event, 2)
	hook := func(ctx context.Context, tx pgx.Tx) error {
		return processor.TerminalTextFailureTx(ctx, tx, event, true)
	}
	if err := jobs.FailWithHook(f.ctx, job, timeoutError(), hook); err != nil {
		t.Fatal(err)
	}
	if f.jobStatus(job.ID) != "FAILED" || f.eventStatus(event) != "FAILED" {
		t.Fatalf("job=%s event=%s", f.jobStatus(job.ID), f.eventStatus(event))
	}
	if got := f.replies(); got != 1 {
		t.Fatalf("the terminal notice must still be queued after a progress notice, got %d", got)
	}
}

// The notice's existence check is bounded: a reply to the same message that is
// older than the lookback does not suppress it.
func TestProgressNoticeIgnoresRepliesOlderThanTheLookback(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO job(type,payload_json,created_at) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',31::bigint,'text','lama'),now()-interval '1 hour')`, f.chatID); err != nil {
		t.Fatal(err)
	}
	var update telegramUpdate
	update.Message.MessageID, update.Message.Chat.ID, update.Message.From.ID = 31, f.chatID, f.chatID
	if err := processor.enqueueProgressNotice(f.ctx, update); err != nil {
		t.Fatal(err)
	}
	var notices int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'reply_to_message_id'='31' AND payload_json->>'text'=$2`,
		fmt.Sprint(f.chatID), progressNoticeMessage).Scan(&notices); err != nil {
		t.Fatal(err)
	}
	if notices != 1 {
		t.Fatalf("a reply older than the lookback must not suppress the notice, got %d", notices)
	}
}

func (f *terminalFixture) callbackEvent(messageID int64, withText bool) string {
	message := map[string]any{"message_id": messageID, "chat": map[string]any{"id": f.chatID}}
	if withText {
		message["text"] = "Pilih kategori untuk transaksi ini"
	}
	return seedTelegramRaw(f.t, f.pool, f.householdID, "TELEGRAM_CALLBACK", map[string]any{
		"update_id": f.stamp + messageID,
		"callback_query": map[string]any{
			"id": fmt.Sprintf("cb-%d-%d", f.stamp, messageID), "data": "review:ignore",
			"from": map[string]any{"id": f.chatID}, "message": message,
		},
	})
}

func (f *terminalFixture) failedSourceItems(eventID string) (count int, sourceType string) {
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*),COALESCE(max(metadata_json->>'source_type'),'') FROM integration_action WHERE household_id=$1 AND integration_type='SOURCE_PROCESSING' AND action_type='SOURCE_FAILED' AND status='OPEN' AND dedupe_key=$2`,
		f.householdID, eventID).Scan(&count, &sourceType); err != nil {
		f.t.Fatal(err)
	}
	return count, sourceType
}

// A button tap that dies must say so, retire its dead buttons, and leave a
// dismissable item, all in the transaction that fails the job.
func TestDeadButtonTapIsFinalizedAnnouncedAndRecorded(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	jobs := queue.New(f.pool)
	event := f.callbackEvent(41, true)
	payload, _ := json.Marshal(map[string]string{"source_event_id": event, "callback_id": "cb"})
	job := queue.Job{Type: "PROCESS_TELEGRAM_CALLBACK", Payload: payload, Attempts: 5, MaxAttempts: 5}
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO job(type,payload_json,status,attempts,max_attempts,locked_at,locked_by) VALUES('PROCESS_TELEGRAM_CALLBACK',$1::jsonb,'RUNNING',5,5,now(),'test') RETURNING id::text`, string(payload)).Scan(&job.ID); err != nil {
		t.Fatal(err)
	}
	hook := func(ctx context.Context, tx pgx.Tx) error { return processor.TerminalCallbackFailureTx(ctx, tx, event) }
	if err := jobs.FailWithHook(f.ctx, job, errors.New("ERROR: null value in column"), hook); err != nil {
		t.Fatal(err)
	}
	if f.jobStatus(job.ID) != "FAILED" || f.eventStatus(event) != "FAILED" {
		t.Fatalf("job=%s event=%s", f.jobStatus(job.ID), f.eventStatus(event))
	}
	var replies, edits int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'text'=$2`, fmt.Sprint(f.chatID), terminalCallbackFailureMessage).Scan(&replies); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'message_id'='41'`, fmt.Sprint(f.chatID)).Scan(&edits); err != nil {
		t.Fatal(err)
	}
	if replies != 1 || edits != 1 {
		t.Fatalf("expected one reply and one button retirement, got replies=%d edits=%d", replies, edits)
	}
	if count, kind := f.failedSourceItems(event); count != 1 || kind != "TELEGRAM_CALLBACK" {
		t.Fatalf("expected one dismissable item, got %d (%s)", count, kind)
	}

	// A replay must not announce again, and a tap that finished is left alone.
	if err := replayCallbackFailure(f, processor, event); err != nil {
		t.Fatal(err)
	}
	if count, _ := f.failedSourceItems(event); count != 1 {
		t.Fatalf("a replay must not duplicate the item, got %d", count)
	}
	done := f.callbackEvent(42, true)
	if _, err := f.pool.Exec(f.ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1`, done); err != nil {
		t.Fatal(err)
	}
	if err := replayCallbackFailure(f, processor, done); err != nil {
		t.Fatal(err)
	}
	if f.eventStatus(done) != "PROCESSED" {
		t.Fatalf("a finished tap must stay PROCESSED, got %s", f.eventStatus(done))
	}
	if count, _ := f.failedSourceItems(done); count != 0 {
		t.Fatalf("a finished tap must not be announced, got %d", count)
	}
}

// A dead typed message also leaves a dismissable item, in the same transaction.
func TestDeadTypedMessageLeavesADismissableItem(t *testing.T) {
	f := newTerminalFixture(t)
	processor := NewProcessor(f.pool, nil)
	jobs := queue.New(f.pool)
	event := f.event(51)
	job := f.job(event, 2)
	hook := func(ctx context.Context, tx pgx.Tx) error {
		return processor.TerminalTextFailureTx(ctx, tx, event, true)
	}
	if err := jobs.FailWithHook(f.ctx, job, timeoutError(), hook); err != nil {
		t.Fatal(err)
	}
	if count, kind := f.failedSourceItems(event); count != 1 || kind != "TELEGRAM_TEXT" {
		t.Fatalf("expected one dismissable item for the dead message, got %d (%s)", count, kind)
	}
}

func replayCallbackFailure(f *terminalFixture, processor *Processor, event string) error {
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(f.ctx)
	if err := processor.TerminalCallbackFailureTx(f.ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(f.ctx)
}
