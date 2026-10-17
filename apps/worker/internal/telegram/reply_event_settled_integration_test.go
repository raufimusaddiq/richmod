package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A re-ask ("reply again with the amount and time") answers a message, not a
// review. The reply event is handled, so it must end PROCESSED; the review it
// concerns keeps its own OPEN item. Left NEEDS_REVIEW the event can never get an
// item and the ledger shows it unfinished forever (#359).
func TestBankFactsReAskSettlesTheReplyEventAndKeepsTheReviewOpen(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for name, text := range map[string]string{
		"missing amount and time": "halo",
		"time without a zone":     "54000 2026-09-23 13:45",
	} {
		t.Run(name, func(t *testing.T) {
			must := func(err error) {
				if err != nil {
					t.Fatal(err)
				}
			}
			stamp := time.Now().UnixNano()
			household, user, chat, _, _, _, request, item := seedDuplicateReview(t, pool, stamp)
			var bankSource string
			must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("reask-%d", stamp), []byte(fmt.Sprintf("reask-%d", stamp))).Scan(&bankSource))
			_, err := pool.Exec(ctx, `UPDATE review_item SET transaction_id=NULL,source_event_id=$2,review_type='UNKNOWN_BANK_TEMPLATE',decision='{"version":1,"reasonCode":"UNKNOWN_BANK_TEMPLATE","allowedActions":["COMPLETE_BANK_FACTS","IGNORE"]}'::jsonb WHERE id=$1`, item, bankSource)
			must(err)
			_, err = pool.Exec(ctx, `UPDATE review_request SET transaction_id=NULL,review_type='UNKNOWN_BANK_TEMPLATE' WHERE id=$1`, request)
			must(err)

			messageID := 1000 + stamp%1000000000
			replySource := seedTelegramReply(t, pool, household, chat, messageID, 769, text)
			var payload string
			must(pool.QueryRow(ctx, `SELECT payload_json::text FROM source_event_payload WHERE source_event_id=$1`, replySource).Scan(&payload))
			var update telegramUpdate
			must(json.Unmarshal([]byte(payload), &update))

			processor := NewProcessor(pool, nil)
			must(processor.completeBankFactsReply(ctx, replySource, household, item, user, bankSource, update))

			var replyStatus, itemStatus, requestStatus, bankStatus string
			var replies int
			must(pool.QueryRow(ctx, `SELECT s.processing_status,ri.status,rr.status,b.processing_status,
				(SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'reply_to_message_id'=$5::text)
				FROM source_event s, review_item ri, review_request rr, source_event b
				WHERE s.id=$1 AND ri.id=$2 AND rr.id=$3 AND b.id=$4`, replySource, item, request, bankSource, fmt.Sprint(messageID)).Scan(&replyStatus, &itemStatus, &requestStatus, &bankStatus, &replies))
			if replyStatus != "PROCESSED" {
				t.Fatalf("the re-ask reply event must be PROCESSED, got %s", replyStatus)
			}
			if itemStatus != "OPEN" || requestStatus != "OPEN" || bankStatus != "NEEDS_REVIEW" {
				t.Fatalf("the review must stay untouched: item=%s request=%s bank source=%s", itemStatus, requestStatus, bankStatus)
			}
			if replies != 1 {
				t.Fatalf("the household must still be re-asked once, got %d reply jobs", replies)
			}
		})
	}
}

// No production path may park a chat reply or button tap as NEEDS_REVIEW: the
// event is not a review subject, so it can never be resolved. This covers the
// call sites that need a Telegram round trip to exercise (no active account,
// unavailable transfer choice) as well.
func TestNoTelegramReplyEventIsParkedAsNeedsReview(t *testing.T) {
	calls := regexp.MustCompile(`finishWithoutTransaction\([^)]*"NEEDS_REVIEW"`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if loc := calls.FindIndex(raw); loc != nil {
			line := 1 + strings.Count(string(raw[:loc[0]]), "\n")
			t.Errorf("%s:%d parks a Telegram reply event as NEEDS_REVIEW; settle it PROCESSED and leave the review on its own item", file, line)
		}
	}
}
