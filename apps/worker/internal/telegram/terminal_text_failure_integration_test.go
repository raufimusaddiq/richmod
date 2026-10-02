package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A typed message that will not be retried again must not end in silence: an
// unanswered one is marked FAILED and answered once; an answered one is left
// alone.
func TestHandleTerminalTextFailureAnswersOnlyAnUnansweredMessageOnce(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	stamp := time.Now().UnixNano()
	var householdID string
	if err := pool.QueryRow(ctx, "INSERT INTO household(name) VALUES($1) RETURNING id", fmt.Sprintf("Terminal text %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	chatID := stamp%1_000_000_000 + 7_000_000_000
	message := func(id int64) map[string]any {
		return map[string]any{
			"update_id": stamp + id,
			"message": map[string]any{
				"message_id": id, "text": "jelaskan analisis bulan ini",
				"from": map[string]any{"id": chatID}, "chat": map[string]any{"id": chatID},
			},
		}
	}
	replies := func() int {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND payload_json->>'text'=$2`,
			fmt.Sprint(chatID), terminalTextFailureMessage).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	status := func(id string) string {
		var value string
		if err := pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, id).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	processor := NewProcessor(pool, nil)

	unanswered := seedTelegramRaw(t, pool, householdID, "TELEGRAM_TEXT", message(1))
	if err := processor.HandleTerminalTextFailure(ctx, unanswered); err != nil {
		t.Fatal(err)
	}
	if got := status(unanswered); got != "FAILED" {
		t.Fatalf("an unanswered message must be marked FAILED, got %s", got)
	}
	if got := replies(); got != 1 {
		t.Fatalf("an unanswered message must get exactly one reply, got %d", got)
	}

	// A second worker (or a replayed failure) must not send it again.
	if err := processor.HandleTerminalTextFailure(ctx, unanswered); err != nil {
		t.Fatal(err)
	}
	if got := replies(); got != 1 {
		t.Fatalf("the apology must be sent once, got %d", got)
	}

	// A message that was already answered must not be contradicted.
	answered := seedTelegramRaw(t, pool, householdID, "TELEGRAM_TEXT", message(2))
	if _, err := pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1`, answered); err != nil {
		t.Fatal(err)
	}
	if err := processor.HandleTerminalTextFailure(ctx, answered); err != nil {
		t.Fatal(err)
	}
	if got := status(answered); got != "PROCESSED" {
		t.Fatalf("an answered message must stay PROCESSED, got %s", got)
	}
	if got := replies(); got != 1 {
		t.Fatalf("an answered message must not get the failure notice, got %d replies", got)
	}

}
