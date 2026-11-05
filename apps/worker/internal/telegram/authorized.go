package telegram

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type authorizationQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// memberAuthorizedSQL is the one definition of a Telegram user Richmod may
// talk to on behalf of a household: an active identity, linked to an active
// user who is an active member of that same household. Private chats are the
// only chats the bot serves (the webhook drops the rest), so a chat ID is the
// user's Telegram ID. An empty household matches the identity's own household.
const memberAuthorizedSQL = `
	SELECT EXISTS(
	  SELECT 1 FROM telegram_identity ti
	  JOIN "user" u ON u.id=ti.user_id AND u.active
	  JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active
	  WHERE ti.telegram_user_id=$1 AND ti.active
	    AND (NULLIF($2,'') IS NULL OR ti.household_id=NULLIF($2,'')::uuid))`

// telegramMemberAuthorized re-checks, at the moment of use, that a Telegram
// user is still an authorized member. Intake authorizes once at capture; a
// member removed while their work waits in the queue must not be served.
func telegramMemberAuthorized(ctx context.Context, db authorizationQuerier, telegramUserID int64, householdID string) (bool, error) {
	if telegramUserID == 0 {
		return false, nil
	}
	var authorized bool
	err := db.QueryRow(ctx, memberAuthorizedSQL, telegramUserID, householdID).Scan(&authorized)
	return authorized, err
}

// RecipientAuthorized reports whether a queued message may still be delivered
// to a chat: the recipient must be an authorized member of the household the
// job belongs to ("" when the job has no household anchor).
func (p *Processor) RecipientAuthorized(ctx context.Context, chatID int64, householdID string) (bool, error) {
	return telegramMemberAuthorized(ctx, p.pool, chatID, householdID)
}

// ignoreUnauthorizedSender settles a queued Telegram event whose sender lost
// access after capture. Nothing is replied: the chat no longer belongs to a
// member, so no household content may be sent to it.
func ignoreUnauthorizedSender(ctx context.Context, db authorizationQuerier, sourceEventID string) error {
	_, err := db.Exec(ctx, `UPDATE source_event SET processing_status='IGNORED',parser_name=COALESCE(parser_name,'telegram-access'),parser_version=COALESCE(parser_version,'1') WHERE id=$1 AND processing_status IN ('RECEIVED','PROCESSING')`, sourceEventID)
	return err
}
