package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// defaultLease is longer than an HTTP send, so a claimed row is not stolen
	// while its push service is slow (even a user with many devices). A crash
	// after a successful send and before the result is recorded can re-send
	// once; that window is unavoidable without a push-service idempotency key.
	defaultLease = 5 * time.Minute
	// defaultMaxAttempts caps retries per device before its delivery is FAILED.
	defaultMaxAttempts = 5
	// DefaultExpiry bounds how old a notification may be before it is dropped.
	// A review push is useful while the inbox item is fresh; replaying a month
	// of history to a newly subscribed device is noise, not notification.
	DefaultExpiry = time.Hour
	// DefaultRetention bounds how long a terminal row is kept for diagnosis.
	// Delivery outcomes are only useful while chasing a user's "I did not get
	// that notification" report, which resolves within days; a week leaves a
	// full week of history on hand without letting either table grow forever.
	DefaultRetention = 7 * 24 * time.Hour
	// purgeBatch caps one sweep so the first purge after a long uptime deletes
	// in bounded chunks instead of holding locks over an arbitrarily large set.
	purgeBatch = 1000
)

// Sender claims pending outbox rows and delivers them to live subscriptions.
type Sender struct {
	pool        *pgxpool.Pool
	vapid       *VAPID
	client      *http.Client
	logger      *slog.Logger
	lease       time.Duration
	maxAttempts int
	expiry      time.Duration
	backoff     func(attempt int) time.Duration
	batch       int
}

type outboxRow struct {
	id          int64
	householdID string
	userID      string
	kind        string
	attempts    int
}

type groupKey struct {
	householdID string
	userID      string
}

type pendingDelivery struct {
	deliveryID   int64
	outboxID     int64
	subscription Subscription
}

type notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Count int    `json:"count"`
}

// secureClient refuses redirects and validates every dial target. Proxy is
// disabled so an environment proxy cannot become an SSRF bypass.
func secureClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         guardedDialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("push redirect refused")
		},
	}
}

// NewSender returns a disabled error when VAPID configuration is absent. The
// caller must keep starting normally in that case.
func NewSender(pool *pgxpool.Pool, privateKey, subject string, logger *slog.Logger) (*Sender, error) {
	vapid, err := NewVAPID(privateKey, subject)
	if err != nil {
		return nil, err
	}
	return &Sender{
		pool:        pool,
		vapid:       vapid,
		client:      secureClient(),
		logger:      logger,
		lease:       defaultLease,
		maxAttempts: defaultMaxAttempts,
		expiry:      DefaultExpiry,
		backoff: func(attempt int) time.Duration {
			duration := time.Minute << (attempt - 1)
			if duration > time.Hour {
				return time.Hour
			}
			return duration
		},
		batch: 50,
	}, nil
}

// Run polls until the worker context is cancelled.
func (s *Sender) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			s.logger.Warn("push outbox claim failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce expires stale rows, claims one batch, and delivers one notification
// per user/household. It returns the number of groups processed.
func (s *Sender) RunOnce(ctx context.Context) (int, error) {
	if err := s.expire(ctx); err != nil {
		return 0, err
	}
	rows, err := s.claim(ctx)
	if err != nil {
		return 0, err
	}
	groups := make(map[groupKey][]outboxRow)
	order := make([]groupKey, 0)
	for _, row := range rows {
		key := groupKey{householdID: row.householdID, userID: row.userID}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], row)
	}
	processed := 0
	batchIDs := rowIDs(rows)
	for _, key := range order {
		if err := s.deliver(ctx, key, groups[key], batchIDs); err != nil {
			s.logger.Warn("push delivery bookkeeping failed", "error", err)
			continue
		}
		processed++
	}
	return processed, nil
}

func (s *Sender) expire(ctx context.Context) error {
	_, err := Expire(ctx, s.pool, s.logger, s.expiry)
	return err
}

// Expire abandons rows older than maxAge. It is a free function so the worker
// can run the sweep even when VAPID configuration is absent and no Sender
// exists; otherwise the outbox could grow behind a disabled feature flag.
func Expire(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, maxAge time.Duration) (int, error) {
	rows, err := pool.Query(ctx, `
		UPDATE push_outbox
		SET abandoned_at=now(),last_error=$2
		WHERE sent_at IS NULL AND abandoned_at IS NULL
		  AND created_at < now()-($1*interval '1 second')
		RETURNING id`,
		int(maxAge.Seconds()), "expired: notification older than the maximum delivery age")
	if err != nil {
		return 0, err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if _, err := pool.Exec(ctx, `UPDATE push_outbox_delivery SET status='DEAD',updated_at=now() WHERE outbox_id=ANY($1) AND status='PENDING'`, ids); err != nil {
		return 0, err
	}
	if logger != nil {
		logger.Info("push outbox expired", "rows", len(ids))
	}
	return len(ids), nil
}

// Purge deletes terminal push rows older than retention, in batches of at most
// purgeBatch per table. Expire bounds how long a row stays eligible for
// delivery; without this sweep both tables would grow for the life of the
// deployment. Like Expire it is a free function: the worker runs it on the same
// unconditional tick, so a deployment with no VAPID keys still cleans up.
//
// The outbox -> delivery foreign key has no ON DELETE CASCADE, so nothing is
// taken along implicitly: deliveries are deleted first (oldest first), then
// outbox rows that no longer have any delivery row. Outbox rows whose
// deliveries are newer than retention therefore wait for the next sweeps
// instead of failing the delete. PENDING deliveries are never matched, and an
// outbox row that still has a PENDING delivery keeps its row either way.
//
// It returns the number of outbox rows removed; both counts are logged.
func Purge(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, retention time.Duration) (int, error) {
	deliveries, err := pool.Exec(ctx, `
		WITH doomed AS (
			SELECT id FROM push_outbox_delivery
			WHERE status IN ('SENT','DEAD','FAILED')
			  AND updated_at < now()-($1*interval '1 second')
			ORDER BY updated_at,id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM push_outbox_delivery d USING doomed
		WHERE d.id=doomed.id`,
		int(retention.Seconds()), purgeBatch)
	if err != nil {
		return 0, err
	}
	outbox, err := pool.Exec(ctx, `
		WITH doomed AS (
			SELECT id FROM push_outbox o
			WHERE (o.sent_at IS NOT NULL OR o.abandoned_at IS NOT NULL)
			  AND COALESCE(o.sent_at,o.abandoned_at) < now()-($1*interval '1 second')
			  AND NOT EXISTS (SELECT 1 FROM push_outbox_delivery d WHERE d.outbox_id=o.id)
			ORDER BY COALESCE(o.sent_at,o.abandoned_at),id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM push_outbox o USING doomed
		WHERE o.id=doomed.id`,
		int(retention.Seconds()), purgeBatch)
	if err != nil {
		return 0, err
	}
	removed := int(outbox.RowsAffected())
	if logger != nil && (removed > 0 || deliveries.RowsAffected() > 0) {
		logger.Info("push retention purge", "outbox_rows", removed, "delivery_rows", deliveries.RowsAffected())
	}
	return removed, nil
}

func (s *Sender) claim(ctx context.Context) ([]outboxRow, error) {
	rows, err := s.pool.Query(ctx, `
		WITH due AS (
			SELECT id FROM push_outbox
			WHERE sent_at IS NULL AND abandoned_at IS NULL AND available_at<=now()
			ORDER BY available_at,id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE push_outbox o
		SET attempts=o.attempts+1, available_at=now()+($2*interval '1 second')
		FROM due
		WHERE o.id=due.id
		RETURNING o.id,o.household_id::text,o.user_id::text,o.kind,o.attempts`, s.batch, int(s.lease.Seconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]outboxRow, 0)
	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.id, &row.householdID, &row.userID, &row.kind, &row.attempts); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func (s *Sender) deliver(ctx context.Context, key groupKey, rows []outboxRow, batchIDs []int64) error {
	active, err := s.activeMember(ctx, key.householdID, key.userID)
	if err != nil {
		return err
	}
	if !active {
		// The row still records the skip, so the removed recipient is visible
		// in the outbox instead of disappearing.
		return s.abandonRows(ctx, rowIDs(rows), "recipient is not an active household member")
	}
	ids := rowIDs(rows)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO push_outbox_delivery(outbox_id,subscription_id)
		SELECT o.id,s.id
		FROM push_outbox o
		JOIN push_subscriptions s ON s.user_id=o.user_id AND s.household_id=o.household_id
		  AND s.disabled_at IS NULL AND s.active_since <= o.created_at
		WHERE o.id=ANY($1)
		  AND NOT EXISTS (SELECT 1 FROM push_outbox_delivery d WHERE d.outbox_id=o.id)
		ON CONFLICT DO NOTHING`, ids); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE push_outbox_delivery d
		SET status='DEAD',updated_at=now()
		FROM push_subscriptions s,push_outbox o
		WHERE d.subscription_id=s.id AND d.outbox_id=o.id AND d.outbox_id=ANY($1)
		  AND d.status='PENDING'
		  AND (s.disabled_at IS NOT NULL OR s.active_since > o.created_at OR s.household_id <> o.household_id)`, ids); err != nil {
		return err
	}
	pending, err := s.pendingDeliveries(ctx, ids)
	if err != nil {
		return err
	}
	rowsByID := make(map[int64]outboxRow, len(rows))
	for _, row := range rows {
		rowsByID[row.id] = row
	}
	lastError := "push delivery failed"
	bySubscription := make(map[string][]pendingDelivery)
	subscriptionOrder := make([]string, 0)
	for _, delivery := range pending {
		if _, ok := bySubscription[delivery.subscription.ID]; !ok {
			subscriptionOrder = append(subscriptionOrder, delivery.subscription.ID)
		}
		bySubscription[delivery.subscription.ID] = append(bySubscription[delivery.subscription.ID], delivery)
	}
	for _, subscriptionID := range subscriptionOrder {
		deliveries := bySubscription[subscriptionID]
		deliveryIDs := make([]int64, 0, len(deliveries))
		for _, delivery := range deliveries {
			deliveryIDs = append(deliveryIDs, delivery.deliveryID)
		}
		pendingIDs := make([]int64, 0, len(deliveries))
		for _, delivery := range deliveries {
			pendingIDs = append(pendingIDs, delivery.outboxID)
		}
		payloadRows := make([]outboxRow, 0, len(pendingIDs))
		for _, id := range pendingIDs {
			if row, ok := rowsByID[id]; ok {
				payloadRows = append(payloadRows, row)
			}
		}
		payload, err := json.Marshal(buildNotification(payloadRows))
		if err != nil {
			return err
		}
		// Renew the lease for every row claimed in this batch before each
		// device send, so a slow batch cannot outlive its lease and be
		// claimed by another worker.
		if _, renewErr := s.pool.Exec(ctx, `UPDATE push_outbox SET available_at=now()+($2*interval '1 second') WHERE id=ANY($1) AND sent_at IS NULL AND abandoned_at IS NULL`, batchIDs, int(s.lease.Seconds())); renewErr != nil {
			return renewErr
		}
		err = s.vapid.Send(ctx, s.client, deliveries[0].subscription, payload)
		if err == nil {
			if _, updateErr := s.pool.Exec(ctx, `UPDATE push_outbox_delivery SET status='SENT',attempts=attempts+1,last_error=NULL,updated_at=now() WHERE id=ANY($1) AND status='PENDING'`, deliveryIDs); updateErr != nil {
				s.logger.Warn("push delivery record failed", "error", updateErr)
			}
			if _, updateErr := s.pool.Exec(ctx, `UPDATE push_subscriptions SET failure_count=0 WHERE id=$1`, subscriptionID); updateErr != nil {
				s.logger.Warn("push subscription reset failed", "error", updateErr)
			}
			continue
		}
		dead := false
		var deliveryError *DeliveryError
		if errors.As(err, &deliveryError) {
			lastError = deliveryError.Message
			dead = deliveryError.Dead
		}
		if dead {
			if _, updateErr := s.pool.Exec(ctx, `UPDATE push_outbox_delivery SET status='DEAD',attempts=attempts+1,last_error=$2,updated_at=now() WHERE id=ANY($1) AND status='PENDING'`, deliveryIDs, lastError); updateErr != nil {
				s.logger.Warn("push delivery record failed", "error", updateErr)
			}
			if _, updateErr := s.pool.Exec(ctx, `UPDATE push_subscriptions SET disabled_at=now() WHERE id=$1 AND disabled_at IS NULL`, subscriptionID); updateErr != nil {
				s.logger.Warn("push subscription disable failed", "error", updateErr)
			}
			continue
		}
		if _, updateErr := s.pool.Exec(ctx, `UPDATE push_outbox_delivery SET attempts=attempts+1,status=CASE WHEN attempts+1>=$2 THEN 'FAILED' ELSE 'PENDING' END,last_error=$3,updated_at=now() WHERE id=ANY($1) AND status='PENDING'`, deliveryIDs, s.maxAttempts, lastError); updateErr != nil {
			s.logger.Warn("push delivery record failed", "error", updateErr)
		}
		// failure_count is diagnostic only: a transient push-service outage
		// must never permanently retire a valid device. Only DeliveryError.Dead
		// (404/410) sets disabled_at. The per-event attempts cap already marks
		// this delivery FAILED after bounded retries.
		if _, updateErr := s.pool.Exec(ctx, `UPDATE push_subscriptions SET failure_count=failure_count+1 WHERE id=$1 AND disabled_at IS NULL`, subscriptionID); updateErr != nil {
			s.logger.Warn("push subscription failure count failed", "error", updateErr)
		}
		if _, updateErr := s.pool.Exec(ctx, `
			UPDATE push_outbox_delivery d
			SET status='DEAD',updated_at=now()
			FROM push_subscriptions s
			WHERE d.subscription_id=s.id AND d.subscription_id=$1 AND d.outbox_id=ANY($2)
			  AND d.status='PENDING' AND s.disabled_at IS NOT NULL`, subscriptionID, ids); updateErr != nil {
			s.logger.Warn("push delivery record failed", "error", updateErr)
		}
	}
	return s.finalize(ctx, rows, lastError)
}

func (s *Sender) activeMember(ctx context.Context, householdID, userID string) (bool, error) {
	var active bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM household_member WHERE household_id=$1 AND user_id=$2 AND active)`, householdID, userID).Scan(&active)
	return active, err
}

func (s *Sender) pendingDeliveries(ctx context.Context, ids []int64) ([]pendingDelivery, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id,d.outbox_id,s.id::text,s.endpoint,s.p256dh,s.auth
		FROM push_outbox_delivery d
		JOIN push_subscriptions s ON s.id=d.subscription_id
		JOIN push_outbox o ON o.id=d.outbox_id
		WHERE d.outbox_id=ANY($1) AND d.status='PENDING' AND s.disabled_at IS NULL
		  AND s.household_id=o.household_id AND s.active_since <= o.created_at
		ORDER BY s.id,d.id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deliveries := make([]pendingDelivery, 0)
	for rows.Next() {
		var delivery pendingDelivery
		if err := rows.Scan(&delivery.deliveryID, &delivery.outboxID, &delivery.subscription.ID, &delivery.subscription.Endpoint, &delivery.subscription.P256DH, &delivery.subscription.Auth); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func (s *Sender) abandonRows(ctx context.Context, ids []int64, reason string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE push_outbox_delivery SET status='DEAD',updated_at=now() WHERE outbox_id=ANY($1) AND status='PENDING'`, ids); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE push_outbox SET abandoned_at=now(),last_error=$2 WHERE id=ANY($1) AND sent_at IS NULL AND abandoned_at IS NULL`, ids, reason)
	return err
}

func (s *Sender) finalize(ctx context.Context, rows []outboxRow, lastError string) error {
	ids := rowIDs(rows)
	if _, err := s.pool.Exec(ctx, `
		UPDATE push_outbox o
		SET sent_at=now(),last_error='no live subscription'
		WHERE o.id=ANY($1) AND o.sent_at IS NULL AND o.abandoned_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM push_outbox_delivery d WHERE d.outbox_id=o.id)`, ids); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE push_outbox o
		SET sent_at=now(),last_error=NULL
		WHERE o.id=ANY($1) AND o.sent_at IS NULL AND o.abandoned_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM push_outbox_delivery d WHERE d.outbox_id=o.id AND d.status='PENDING')
		  AND EXISTS (SELECT 1 FROM push_outbox_delivery d WHERE d.outbox_id=o.id AND d.status='SENT')`, ids); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE push_outbox o
		SET abandoned_at=now(),last_error=COALESCE(NULLIF($2,''),'no device delivered the notification')
		WHERE o.id=ANY($1) AND o.sent_at IS NULL AND o.abandoned_at IS NULL
		  AND EXISTS (SELECT 1 FROM push_outbox_delivery d WHERE d.outbox_id=o.id)
		  AND NOT EXISTS (SELECT 1 FROM push_outbox_delivery d WHERE d.outbox_id=o.id AND d.status='PENDING')
		  AND NOT EXISTS (SELECT 1 FROM push_outbox_delivery d WHERE d.outbox_id=o.id AND d.status='SENT')`, ids, lastError); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := s.pool.Exec(ctx, `
			UPDATE push_outbox
			SET available_at=now()+($2*interval '1 second'),last_error=$3
			WHERE id=$1 AND sent_at IS NULL AND abandoned_at IS NULL
			  AND EXISTS (SELECT 1 FROM push_outbox_delivery d WHERE d.outbox_id=$1 AND d.status='PENDING')`,
			row.id, int(s.backoff(row.attempts).Seconds()), lastError); err != nil {
			return err
		}
	}
	return nil
}

func rowIDs(rows []outboxRow) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.id)
	}
	return ids
}

func buildNotification(rows []outboxRow) notification {
	reviews, actions := 0, 0
	for _, row := range rows {
		if row.kind == "REVIEW" {
			reviews++
		} else {
			actions++
		}
	}
	result := notification{URL: "/inbox", Count: len(rows)}
	switch {
	case reviews > 0 && actions == 0:
		result.Title = "Review baru"
		result.Body = fmt.Sprintf("%d transaksi menunggu tinjauan.", reviews)
	case actions > 0 && reviews == 0:
		result.Title = "Tindakan baru"
		result.Body = fmt.Sprintf("%d tindakan integrasi menunggu.", actions)
	default:
		result.Title = "Inbox Richmod"
		result.Body = fmt.Sprintf("%d hal baru menunggu.", len(rows))
	}
	return result
}
