package telegram

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

const (
	interactionAnswered       = "ANSWERED"
	interactionUnmatched      = "UNMATCHED"
	interactionAmbiguousEvent = "AMBIGUOUS"
	interactionClosedReply    = "CLOSED_CARD_REPLY"
	interactionRepeated       = "REPEATED_INPUT"
)

const repeatedInputWindow = 3 * time.Minute

func (p *Processor) recordInteraction(ctx context.Context, householdID, sourceEventID, reviewItemID, action string, source interactionSource) {
	if p.pool == nil || householdID == "" {
		return
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,source_event_id,review_item_id,event_type,source_type,action)
		VALUES($1,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,'INTERACTION',NULLIF($4,''),$5) ON CONFLICT DO NOTHING`,
		householdID, sourceEventID, reviewItemID, string(source), action)
	if err != nil {
		slog.Default().Warn("record interaction telemetry failed", "household_id", householdID, "source_event_id", sourceEventID, "action", action, "error", err)
	}
}

func (p *Processor) noteRepeatedInput(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, receivedAt time.Time) {
	normalized := strings.ToLower(strings.Join(strings.Fields(update.Message.Text), " "))
	if normalized == "" || p.pool == nil {
		return
	}
	var repeated bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM source_event s JOIN source_event_payload pl ON pl.source_event_id=s.id
		WHERE s.household_id=$1 AND s.source_type='TELEGRAM_TEXT' AND s.id<>$2::uuid
		  AND s.received_at<=$3 AND s.received_at>$3-make_interval(secs => $4::int)
		  AND pl.payload_json#>>'{message,chat,id}'=$5::bigint::text AND pl.payload_json#>>'{message,from,id}'=$6::bigint::text
		  AND lower(regexp_replace(btrim(COALESCE(pl.payload_json#>>'{message,text}','')),'\s+',' ','g'))=$7)`,
		householdID, sourceEventID, receivedAt, int(repeatedInputWindow.Seconds()), update.Message.Chat.ID, update.Message.From.ID, normalized).Scan(&repeated)
	if err != nil {
		slog.Default().Warn("check repeated input failed", "household_id", householdID, "source_event_id", sourceEventID, "error", err)
		return
	}
	if repeated {
		p.recordInteraction(ctx, householdID, sourceEventID, "", interactionRepeated, "")
	}
}
