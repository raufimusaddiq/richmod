package telegram

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	progressNoticeMessage   = "Masih kuproses ya, analisis seperti ini butuh waktu lebih lama. Jawabannya menyusul di sini."
	progressNoticeRecording = "Masih kucatat ya, sebentar lagi selesai. Hasilnya menyusul di sini."
	progressNoticeThinking  = "Masih kuproses ya, jawabannya menyusul di sini."
)

type agentActivity int32

const (
	activityThinking agentActivity = iota
	activityAnalyzing
	activityRecording
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
		if err := send(ctx); err != nil {
			slog.Default().Warn("telegram progress notice failed", "error", err)
		}
	})
	return func() {
		mu.Lock()
		finished = true
		mu.Unlock()
		timer.Stop()
	}
}

const progressNoticeLookback = 10 * time.Minute

func (p *Processor) enqueueProgressNotice(ctx context.Context, update telegramUpdate, text string) error {
	if p.pool == nil || update.Message.Chat.ID == 0 {
		return nil
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO job(type,payload_json)
		SELECT 'SEND_TELEGRAM_MESSAGE',
		       jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',$2::bigint,'text',$3::text,'notice',true)
		WHERE NOT EXISTS (
		  SELECT 1 FROM job
		  WHERE type='SEND_TELEGRAM_MESSAGE'
		    AND created_at > now() - make_interval(secs => $4::double precision)
		    AND (payload_json->>'chat_id')::bigint=$1::bigint
		    AND (payload_json->>'reply_to_message_id')::bigint=$2::bigint)`,
		update.Message.Chat.ID, update.Message.MessageID, text, progressNoticeLookback.Seconds())
	return err
}
