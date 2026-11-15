package telegram

import (
	"context"
	"sync"
	"time"
)

// The progress notice tells the household a long turn's answer is still coming, naming what the turn is doing
// (agentActivity) and where the answer will appear.
const (
	progressNoticeMessage   = "Masih kuproses ya, analisis seperti ini butuh waktu lebih lama. Jawabannya menyusul di sini."
	progressNoticeRecording = "Masih kucatat ya, sebentar lagi selesai. Hasilnya menyusul di sini."
	progressNoticeThinking  = "Masih kuproses ya, jawabannya menyusul di sini."
)

// progressNoticeTexts are every interim notice; a recovery check must not mistake one for the turn's answer.
var progressNoticeTexts = []string{progressNoticeMessage, progressNoticeRecording, progressNoticeThinking, gatewayDeferredMessage}

type agentActivity int32

const (
	activityThinking  agentActivity = iota // waiting for the model's first decision
	activityAnalyzing                      // READ tools ran: the turn is gathering facts
	activityRecording                      // a side effect is being applied
)

func progressNoticeFor(activity agentActivity) string {
	switch activity {
	case activityAnalyzing:
		return progressNoticeMessage
	case activityRecording:
		return progressNoticeRecording
	}
	return progressNoticeThinking
}

// startProgressNotice calls send once if the turn still runs after delay; after stop returns send is never
// called again, so a notice cannot follow the answer.
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

// progressNoticeLookback bounds the existence check to this turn and its retry; the job table has no index on
// the reply target, so an unbounded check would scan the chat's history.
const progressNoticeLookback = 10 * time.Minute

// enqueueProgressNotice queues the notice as a reply unless one was queued within the lookback, so it is
// idempotent across retries and never lands after the answer. The final answer is queued unconditionally.
func (p *Processor) enqueueProgressNotice(ctx context.Context, update telegramUpdate, text string) error {
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
		    AND created_at > now() - make_interval(secs => $4::double precision)
		    AND (payload_json->>'chat_id')::bigint=$1::bigint
		    AND (payload_json->>'reply_to_message_id')::bigint=$2::bigint)`,
		update.Message.Chat.ID, update.Message.MessageID, text, progressNoticeLookback.Seconds())
	return err
}
