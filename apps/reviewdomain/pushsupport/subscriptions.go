package pushsupport

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UpsertSubscription registers or refreshes a caller-owned device.
//
// A refresh of an already-live subscription keeps its active period: a browser
// refreshing inside the 30-second batching window must not move active_since
// past an event that is already pending. Only a disabled subscription starts a
// new active period. disabled_at is always cleared; failure_count is reset on
// every refresh because it is diagnostic and the client just proved the
// endpoint is current. Reactivating a disabled subscription also retires that
// subscription's PENDING delivery rows in the same transaction, so a retry from
// the period the user opted out of cannot be sent.
//
// There is no subscription-level last_error: transient text stays on the
// event-scoped push_outbox_delivery row, so a stale error cannot read as current
// device health.
func UpsertSubscription(ctx context.Context, pool *pgxpool.Pool, userID, householdID, endpoint, p256dh, auth, userAgent string) (string, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var id, oldHouseholdID string
	var wasDisabled bool
	err = tx.QueryRow(ctx, `SELECT id,household_id::text,disabled_at IS NOT NULL FROM push_subscriptions WHERE endpoint=$1 FOR UPDATE`, endpoint).Scan(&id, &oldHouseholdID, &wasDisabled)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		wasDisabled = false
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO push_subscriptions(user_id,household_id,endpoint,p256dh,auth,user_agent,last_seen_at)
		SELECT $1,$2,$3,$4,$5,NULLIF($6,''),now()
		WHERE EXISTS (SELECT 1 FROM household_member hm WHERE hm.user_id=$1 AND hm.household_id=$2 AND hm.active)
		ON CONFLICT (endpoint) DO UPDATE
		SET household_id=EXCLUDED.household_id,
		    p256dh=EXCLUDED.p256dh,auth=EXCLUDED.auth,user_agent=EXCLUDED.user_agent,
		    last_seen_at=now(),disabled_at=NULL,failure_count=0,
		    active_since=CASE WHEN push_subscriptions.disabled_at IS NOT NULL THEN now() ELSE push_subscriptions.active_since END
		WHERE push_subscriptions.user_id=EXCLUDED.user_id
		RETURNING id`,
		userID, householdID, endpoint, p256dh, auth, userAgent).Scan(&id)
	if err != nil {
		return "", err
	}
	if wasDisabled || (oldHouseholdID != "" && oldHouseholdID != householdID) {
		if _, err := tx.Exec(ctx, `UPDATE push_outbox_delivery SET status='DEAD',updated_at=now() WHERE subscription_id=$1 AND status='PENDING'`, id); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}
