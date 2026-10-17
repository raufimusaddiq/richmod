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
	for _, tc := range []struct {
		name, text string
		unlinked   bool // a bank source with no bound account and no active account to offer
	}{
		{"missing amount and time", "halo", false},
		{"time without a zone", "54000 2026-09-23 13:45", false},
		{"unlinked bank source and no active account", "54000 2026-09-23T13:45:00+07:00", true},
	} {
		name, text := tc.name, tc.text
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
			if tc.unlinked {
				var listener string
				must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Fixture bank',$2,$3) RETURNING id`, household, fmt.Sprintf("reask-%d@example.test", stamp), user).Scan(&listener))
				_, err = pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','v1','{}','VALID')`, bankSource, listener)
				must(err)
				_, err = pool.Exec(ctx, `UPDATE account SET active=false WHERE household_id=$1`, household)
				must(err)
			}
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
// event is not a review subject, so it can never be resolved. The guard sees both
// the helper and direct `SET processing_status='NEEDS_REVIEW'` writes, and only
// covers this package (filepath.Glob is not recursive).
//
// The one allowed direct write is agentStageTransferReview, which writes the
// source event's own review item in the same transaction, so the event is a
// real review subject. A new site must do the same or settle PROCESSED.
func TestNoTelegramReplyEventIsParkedAsNeedsReview(t *testing.T) {
	calls := regexp.MustCompile(`finishWithoutTransaction\([^)]*"NEEDS_REVIEW"|SET\s+processing_status\s*=\s*'NEEDS_REVIEW'`)
	allowed := map[string]bool{"agent_specialized_mutations.go": true}
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
		for _, loc := range calls.FindAllIndex(raw, -1) {
			if allowed[file] {
				continue
			}
			line := 1 + strings.Count(string(raw[:loc[0]]), "\n")
			t.Errorf("%s:%d parks a Telegram reply event as NEEDS_REVIEW; settle it PROCESSED and leave the review on its own item", file, line)
		}
	}
}
