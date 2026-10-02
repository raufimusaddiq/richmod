package telegram

import (
	"fmt"
	"testing"
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
