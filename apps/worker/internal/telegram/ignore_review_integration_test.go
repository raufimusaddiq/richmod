package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTelegramIgnoreReviewCallback(t *testing.T) {
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
	for _, name := range []string{"bank", "legacy_bank", "transaction", "wrong_message", "unauthorized_actor", "inactive_member", "cross_household", "disallowed_action"} {
		t.Run(name, func(t *testing.T) {
			must := func(err error) {
				if err != nil {
					t.Fatal(err)
				}
			}
			stamp := time.Now().UnixNano()
			household, user, chat, _, transaction, _, request, item := seedDuplicateReview(t, pool, stamp)
			var bankSource string
			must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("ignore-%d", stamp), []byte(fmt.Sprintf("ignore-%d", stamp))).Scan(&bankSource))
			if name != "transaction" {
				_, err = pool.Exec(ctx, `UPDATE review_item SET transaction_id=NULL,source_event_id=$2,review_type='UNKNOWN_BANK_TEMPLATE',decision='{"version":1,"reasonCode":"UNKNOWN_BANK_TEMPLATE","allowedActions":["COMPLETE_BANK_FACTS","IGNORE"]}'::jsonb WHERE id=$1`, item, bankSource)
				must(err)
				_, err = pool.Exec(ctx, `UPDATE review_request SET transaction_id=NULL,review_type='UNKNOWN_BANK_TEMPLATE' WHERE id=$1`, request)
				must(err)
			}
			_, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,769)`, request, chat)
			must(err)
			actor, message, callbackHousehold := chat, int64(769), household
			switch name {
			case "legacy_bank":
				// A historical row created before the ReviewDecision contract.
				// The 00078 trigger refuses erasing a stored decision, so the
				// fixture recreates the legacy shape with triggers skipped for
				// this one statement.
				execWithoutReviewDecisionTrigger(t, pool, `UPDATE review_item SET decision=NULL WHERE id=$1`, item)
			case "wrong_message":
				message++
			case "unauthorized_actor":
				actor++
			case "inactive_member":
				_, err = pool.Exec(ctx, `UPDATE household_member SET active=false WHERE household_id=$1 AND user_id=$2`, household, user)
				must(err)
			case "cross_household":
				must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Other ignore household') RETURNING id`).Scan(&callbackHousehold))
			case "disallowed_action":
				_, err = pool.Exec(ctx, `UPDATE review_item SET decision='{"version":1,"reasonCode":"UNKNOWN_BANK_TEMPLATE","allowedActions":["COMPLETE_BANK_FACTS"]}'::jsonb WHERE id=$1`, item)
				must(err)
			}
			callback := func() string {
				return seedTelegramRaw(t, pool, callbackHousehold, "TELEGRAM_CALLBACK", map[string]any{"callback_query": map[string]any{
					"id": fmt.Sprint(time.Now().UnixNano()), "data": "review:ignore", "from": map[string]any{"id": actor},
					"message": map[string]any{"message_id": message, "chat": map[string]any{"id": chat}},
				}})
			}
			processor := NewProcessor(pool, nil) // Ignore must work without AI.
			for attempt := 0; attempt < 2; attempt++ {
				must(processor.Process(ctx, callback()))
			}
			var itemStatus, sourceStatus, transactionStatus, requestStatus, conversationStatus string
			var audits int
			must(pool.QueryRow(ctx, `SELECT ri.status,s.processing_status,t.status,r.status,c.state,(SELECT count(*) FROM audit_log WHERE entity_id=ri.id AND action='RESOLVE_REVIEW') FROM review_item ri JOIN source_event s ON s.id=$2 JOIN transaction t ON t.id=$3 JOIN review_request r ON r.id=$4 JOIN review_conversation c ON c.review_request_id=r.id WHERE ri.id=$1`, item, bankSource, transaction, request).Scan(&itemStatus, &sourceStatus, &transactionStatus, &requestStatus, &conversationStatus, &audits))
			switch name {
			case "bank", "legacy_bank":
				if itemStatus != "RESOLVED" || sourceStatus != "IGNORED" || requestStatus != "RESOLVED" || conversationStatus != "RESOLVED" || audits != 1 || transactionStatus != "NEEDS_REVIEW" {
					t.Fatalf("item=%s source=%s request=%s conversation=%s audits=%d transaction=%s", itemStatus, sourceStatus, requestStatus, conversationStatus, audits, transactionStatus)
				}
			case "transaction":
				if itemStatus != "RESOLVED" || transactionStatus != "VOIDED" || conversationStatus != "RESOLVED" {
					t.Fatalf("item=%s transaction=%s conversation=%s", itemStatus, transactionStatus, conversationStatus)
				}
			default:
				if itemStatus != "OPEN" || sourceStatus != "NEEDS_REVIEW" || requestStatus != "OPEN" || transactionStatus != "NEEDS_REVIEW" || audits != 0 {
					t.Fatalf("invalid callback mutated item=%s source=%s request=%s transaction=%s audits=%d", itemStatus, sourceStatus, requestStatus, transactionStatus, audits)
				}
			}
		})
	}
}

// execWithoutReviewDecisionTrigger runs one fixture statement that recreates a
// historical review_item with decision IS NULL, which the 00078 trigger refuses
// for every producer. session_replication_role=replica skips triggers for this
// transaction only (the test role owns the database); other fixtures must go
// through the trigger.
func execWithoutReviewDecisionTrigger(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
