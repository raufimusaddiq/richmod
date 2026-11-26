package telegram

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	typingInterval      = 4 * time.Second
	typingSendTimeout   = 2 * time.Second
	typingLookupTimeout = time.Second
)

func (b *Bot) typingReady() bool {
	return b != nil && b.token != ""
}

func runTyping(ctx context.Context, send func(context.Context), interval, firstDelay time.Duration) func() {
	stop := make(chan struct{})
	sendOnce := func() {
		sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), typingSendTimeout)
		send(sendCtx)
		cancel()
	}
	if firstDelay <= 0 {
		sendOnce()
	}
	go func() {
		if firstDelay > 0 {
			timer := time.NewTimer(firstDelay)
			select {
			case <-timer.C:
				sendOnce()
			case <-stop:
				timer.Stop()
				return
			}
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendOnce()
			case <-stop:
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

type activeTyping struct {
	holders int
	stop    func()
}

type DocumentTyping struct {
	pool     *pgxpool.Pool
	bot      *Bot
	interval time.Duration
	mu       sync.Mutex
	active   map[int64]*activeTyping
	ended    map[int64]time.Time
}

func NewDocumentTyping(pool *pgxpool.Pool, bot *Bot) *DocumentTyping {
	return &DocumentTyping{pool: pool, bot: bot, interval: typingInterval, active: map[int64]*activeTyping{}, ended: map[int64]time.Time{}}
}

func (d *DocumentTyping) Start(ctx context.Context, jobType, householdID string, payload json.RawMessage) func() {
	if d == nil || d.pool == nil || !d.bot.typingReady() {
		return func() {}
	}
	chatID := d.chatFor(ctx, jobType, householdID, payload)
	if chatID == 0 {
		return func() {}
	}
	return d.hold(ctx, chatID)
}

func (d *DocumentTyping) chatFor(ctx context.Context, jobType, householdID string, payload json.RawMessage) int64 {
	var query, id string
	switch jobType {
	case "FETCH_TELEGRAM_IMAGE":
		decoded, err := DecodeImagePayload(payload)
		if err != nil {
			return 0
		}
		id = decoded.SourceEventID
		query = `SELECT COALESCE(telegram_chat_id,0) FROM source_event WHERE id=$1 AND household_id=$2 AND source_type='TELEGRAM_IMAGE'`
	case "PROCESS_DOCUMENT", "PROCESS_PAYSLIP", "PROCESS_RECEIPT", "PROCESS_TRANSACTION_SCREENSHOT":
		var decoded struct {
			DocumentID string `json:"document_id"`
		}
		if json.Unmarshal(payload, &decoded) != nil {
			return 0
		}
		id = decoded.DocumentID
		query = `SELECT COALESCE(s.telegram_chat_id,0) FROM document d JOIN source_event s ON s.id=d.source_event_id AND s.household_id=d.household_id WHERE d.id=$1 AND d.household_id=$2 AND s.source_type='TELEGRAM_IMAGE'`
	default:
		return 0
	}
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), typingLookupTimeout)
	defer cancel()
	var chatID int64
	if d.pool.QueryRow(lookupCtx, query, id, householdID).Scan(&chatID) != nil {
		return 0
	}
	return chatID
}

func (d *DocumentTyping) hold(ctx context.Context, chatID int64) func() {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry := d.active[chatID]
	if entry == nil {
		delay := time.Duration(0)
		if last, ok := d.ended[chatID]; ok {
			delay = d.interval - time.Since(last)
		}
		send := func(sendCtx context.Context) { _ = d.bot.Typing(sendCtx, chatID) }
		entry = &activeTyping{stop: runTyping(ctx, send, d.interval, delay)}
		d.active[chatID] = entry
	}
	entry.holders++
	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			defer d.mu.Unlock()
			entry.holders--
			if entry.holders == 0 {
				entry.stop()
				delete(d.active, chatID)
				d.ended[chatID] = time.Now()
			}
		})
	}
}

func (p *Processor) StartDocumentTyping(ctx context.Context, jobType, householdID string, payload json.RawMessage) func() {
	if p == nil {
		return func() {}
	}
	return p.documentTyping.Start(ctx, jobType, householdID, payload)
}
