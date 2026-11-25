package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

func seedTelegramRaw(t *testing.T, pool *pgxpool.Pool, householdID, kind string, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var sourceID string
	if err := pool.QueryRow(context.Background(), "INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,$2,$3,now(),$4,'RECEIVED') RETURNING id", householdID, kind, fmt.Sprintf("%s-%d", kind, testsupport.UniqueStamp()), raw).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), "INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)", sourceID, string(raw)); err != nil {
		t.Fatal(err)
	}
	return sourceID
}

func seedTelegramReply(t *testing.T, pool *pgxpool.Pool, householdID string, chatID, messageID, replyTo int64, text string) string {
	t.Helper()
	return seedTelegramRaw(t, pool, householdID, "TELEGRAM_TEXT", map[string]any{
		"update_id": testsupport.UniqueStamp(),
		"message": map[string]any{
			"message_id":       messageID,
			"text":             text,
			"reply_to_message": map[string]any{"message_id": replyTo},
			"from":             map[string]any{"id": chatID},
			"chat":             map[string]any{"id": chatID},
		},
	})
}
