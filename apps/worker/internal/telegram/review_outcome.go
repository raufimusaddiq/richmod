package telegram

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// reviewOutcome describes, from canonical state only, how a closed review
// ended: "dicatat sebagai Makan oleh Rina", "diabaikan oleh Budi", "ditutup".
// It is shown on every member's retired card and to the member whose tap or
// reply lost the race, so it never names an amount the household did not see
// on the card and never exposes an identifier. An open review has no outcome.
func reviewOutcome(ctx context.Context, db authorizationQuerier, reviewRequestID string) (string, error) {
	var itemStatus, action, resolver, category, transactionStatus string
	err := db.QueryRow(ctx, `SELECT ri.status,COALESCE(ri.resolution_action,''),COALESCE(u.display_name,''),
			COALESCE(c.name,''),COALESCE(t.status,'')
		FROM review_request r
		JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		LEFT JOIN "user" u ON u.id=ri.resolved_by_user_id
		LEFT JOIN transaction t ON t.id=ri.transaction_id AND t.household_id=ri.household_id
		LEFT JOIN category c ON c.id=t.category_id AND c.household_id=t.household_id
		WHERE r.id=$1`, reviewRequestID).Scan(&itemStatus, &action, &resolver, &category, &transactionStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return describeReviewOutcome(itemStatus, action, resolver, category, transactionStatus), nil
}

// describeReviewOutcome is the deterministic wording behind reviewOutcome.
func describeReviewOutcome(itemStatus, action, resolver, category, transactionStatus string) string {
	var phrase string
	switch {
	case itemStatus == "OPEN" || itemStatus == "PENDING_SEND":
		return ""
	case itemStatus == "CANCELLED" || itemStatus == "EXPIRED":
		phrase = "ditutup"
	case strings.Contains(action, "IGNORE") || strings.Contains(action, "REJECT") || transactionStatus == "IGNORED" || transactionStatus == "VOIDED":
		phrase = "diabaikan"
	case action == "NO_LONGER_APPLICABLE" || action == "LEGACY_TRANSACTION_RESOLVED" || action == "RECONCILED_TERMINAL_TRANSACTION" || action == "EMAIL_RECEIVED_AT_FALLBACK":
		return "ditutup otomatis karena tidak lagi berlaku"
	case transactionStatus == "CONFIRMED" && category != "":
		phrase = "dicatat sebagai " + clean(category, 80)
	case transactionStatus == "CONFIRMED":
		phrase = "dicatat"
	default:
		phrase = "diselesaikan"
	}
	if resolver = clean(resolver, 80); resolver != "" {
		phrase += " oleh " + resolver
	}
	return phrase
}

// boundReviewForMessage finds the review a Telegram message belongs to in a
// chat, whether it is the current card or an older message a follow-up
// replaced. It returns the request id and whether that message is the current
// card, or "" when the message is not a review message of this household.
func boundReviewForMessage(ctx context.Context, db authorizationQuerier, householdID string, chatID, messageID int64) (string, bool, error) {
	if chatID == 0 || messageID == 0 {
		return "", false, nil
	}
	var requestID string
	var current bool
	err := db.QueryRow(ctx, `SELECT r.id::text, rr.telegram_message_id IS NOT DISTINCT FROM $3::bigint
		FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2
		  AND (rr.telegram_message_id=$3 OR EXISTS(SELECT 1 FROM review_request_message m WHERE m.recipient_id=rr.id AND m.telegram_chat_id=$2 AND m.telegram_message_id=$3))
		ORDER BY 2 DESC LIMIT 1`, householdID, chatID, messageID).Scan(&requestID, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return requestID, current, err
}

// answeredReviewReply is the deterministic answer for a tap or reply that
// reached a review message after the review was answered elsewhere (by another
// member, on the web, or by the same member's other action): it names the
// outcome. A message replaced by a newer card of a review that is still open
// points to that card. Anything else returns "" and the caller keeps its own
// stale copy.
func answeredReviewReply(ctx context.Context, db authorizationQuerier, householdID string, chatID, messageID int64) (string, error) {
	requestID, current, err := boundReviewForMessage(ctx, db, householdID, chatID, messageID)
	if err != nil || requestID == "" {
		return "", err
	}
	outcome, err := reviewOutcome(ctx, db, requestID)
	if err != nil {
		return "", err
	}
	if outcome != "" {
		return "Tinjauan ini sudah dijawab: " + outcome + ".", nil
	}
	if !current {
		return supersededReviewReply, nil
	}
	return "", nil
}

// supersededReviewReply answers a reply to an older message of a review that is
// still open in a newer message.
const supersededReviewReply = "Tinjauan ini masih terbuka di pesan yang lebih baru. Balas pesan terbaru untuk melanjutkan."

// staleReviewReply returns the outcome-aware answer for a stale review action
// in update's chat, or fallback when no closed review is bound to the message.
func staleReviewReply(ctx context.Context, db authorizationQuerier, householdID string, update telegramUpdate, fallback string) string {
	messageID := int64(0)
	if update.CallbackQuery != nil {
		messageID = update.CallbackQuery.Message.MessageID
	} else if update.Message.ReplyToMessage != nil {
		messageID = update.Message.ReplyToMessage.MessageID
	}
	reply, err := answeredReviewReply(ctx, db, householdID, update.Message.Chat.ID, messageID)
	if err != nil || reply == "" {
		return fallback
	}
	return reply
}

// staleReviewReplyForSource is staleReviewReply for callers that hold only the
// source event: the household is the event's own.
func staleReviewReplyForSource(ctx context.Context, db authorizationQuerier, sourceEventID string, update telegramUpdate, fallback string) string {
	var householdID string
	if err := db.QueryRow(ctx, `SELECT household_id::text FROM source_event WHERE id=$1`, sourceEventID).Scan(&householdID); err != nil {
		return fallback
	}
	return staleReviewReply(ctx, db, householdID, update, fallback)
}
