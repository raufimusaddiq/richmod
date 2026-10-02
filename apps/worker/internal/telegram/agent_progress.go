package telegram

import (
	"context"
	"sync"
	"time"
)

// progressNoticeMessage tells the household the answer is still coming, so a
// long turn is never silent. It says what is happening and where the answer
// will appear.
const progressNoticeMessage = "Masih kuproses ya, analisis seperti ini butuh waktu lebih lama. Jawabannya menyusul di sini."

// startProgressNotice calls send once if the turn is still running after delay.
// The returned stop function ends the wait; after it returns, send is never
// called again and no call is in flight, so a notice cannot follow the answer.
func startProgressNotice(delay time.Duration, send func(context.Context) error) (stop func()) {
	var mu sync.Mutex
	finished := false
	timer := time.AfterFunc(delay, func() {
		mu.Lock()
		defer mu.Unlock()
		if finished {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = send(ctx) // best effort: the answer, not the notice, is what matters
	})
	return func() {
		mu.Lock()
		finished = true
		mu.Unlock()
		timer.Stop()
	}
}

// enqueueProgressNotice queues the notice as a reply to the user's message. It
// is skipped if any reply to that message already exists, which makes it
// idempotent across retries and keeps it from landing after the answer.
func (p *Processor) enqueueProgressNotice(ctx context.Context, update telegramUpdate) error {
	if p.pool == nil || update.Message.Chat.ID == 0 {
		return nil
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO job(type,payload_json)
		SELECT 'SEND_TELEGRAM_MESSAGE',
		       jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',$2::bigint,'text',$3::text)
		WHERE NOT EXISTS (
		  SELECT 1 FROM job
		  WHERE type='SEND_TELEGRAM_MESSAGE'
		    AND (payload_json->>'chat_id')::bigint=$1::bigint
		    AND (payload_json->>'reply_to_message_id')::bigint=$2::bigint)`,
		update.Message.Chat.ID, update.Message.MessageID, progressNoticeMessage)
	return err
}
