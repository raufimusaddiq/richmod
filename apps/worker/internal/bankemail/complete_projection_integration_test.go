package bankemail

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Completing bank facts resolves the Telegram projection with the item, so the
// delivered cards are queued for retirement and the conversation stops waiting.
func TestCompleteBankReviewResolvesItsTelegramProjection(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Now().UnixNano()
	var householdID, userID, accountID, listenerID, sourceEventID, itemID, requestID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank projection %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-projection-%d@example.test", stamp)).Scan(&userID))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening','BANK','FULL_LEDGER') RETURNING id`, householdID).Scan(&accountID))
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,account_id,created_by_user_id) VALUES($1,'Bank','notifikasi@bank.test',$2,$3) RETURNING id`, householdID, accountID, userID).Scan(&listenerID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("bank-projection-%d", stamp), []byte("bank")).Scan(&sourceEventID))
	extraction := `{"kind":"TRANSACTION","direction":"OUT","channel":"CARD","amount_idr":null,"transaction_at":null,"merchant":"Indomaret","counterparty":null,"reference":null,"description":null,"missing_fields":["amount_idr","transaction_at"],"confidence":0.9}`
	_, err = pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'IMAP','v1',$3::jsonb,'INVALID')`, sourceEventID, listenerID, extraction)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN','{"version":1,"reasonCode":"UNKNOWN_BANK_TEMPLATE","allowedActions":["COMPLETE_BANK_FACTS","IGNORE"]}'::jsonb) RETURNING id`, householdID, sourceEventID).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN') RETURNING id`, itemID, householdID).Scan(&requestID))
	_, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_CONFIRMATION')`, requestID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,51),($1,$3,52)`, requestID, stamp, stamp+1)
	must(err)

	amount, at := "54000", "2026-09-23T13:45:00+07:00"
	processor := &Processor{pool: pool}
	must(processor.Complete(ctx, Payload{SourceEventID: sourceEventID, ReviewID: itemID, AmountIDR: &amount, TransactionAt: &at, TelegramChatID: stamp}))

	var itemStatus, requestStatus, conversationState string
	var retirements, notices int
	must(pool.QueryRow(ctx, `SELECT ri.status,r.status,c.state FROM review_item ri JOIN review_request r ON r.review_item_id=ri.id JOIN review_conversation c ON c.review_request_id=r.id WHERE ri.id=$1`, itemID).Scan(&itemStatus, &requestStatus, &conversationState))
	must(pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND payload_json->>'review_request_id'=$1`, requestID).Scan(&retirements))
	must(pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'chat_id'=$1 AND NOT payload_json ? 'review_request_id'`, fmt.Sprint(stamp)).Scan(&notices))
	if itemStatus != "RESOLVED" || requestStatus != "RESOLVED" || conversationState != "RESOLVED" {
		t.Fatalf("item=%s request=%s conversation=%s, want all RESOLVED", itemStatus, requestStatus, conversationState)
	}
	if retirements != 2 || notices != 1 {
		t.Fatalf("retirements=%d notices=%d, want one retirement per delivered card and one outcome notice", retirements, notices)
	}
	// The person who completed the facts gets the credit, not a system
	// fallback: the item records the completion and the same transaction writes
	// a TELEGRAM audit row (the surface admin attribution reads).
	var resolution string
	var telegramAudits int
	must(pool.QueryRow(ctx, `SELECT ri.resolution_action,(SELECT count(*) FROM audit_log a WHERE a.household_id=ri.household_id AND a.created_at=ri.resolved_at AND a.actor_type='TELEGRAM') FROM review_item ri WHERE ri.id=$1`, itemID).Scan(&resolution, &telegramAudits))
	if resolution != "COMPLETE_BANK_FACTS" || telegramAudits != 1 {
		t.Fatalf("resolution=%s telegramAudits=%d, want COMPLETE_BANK_FACTS credited to TELEGRAM", resolution, telegramAudits)
	}

	// A replay finds the item closed and changes nothing.
	must(processor.Complete(ctx, Payload{SourceEventID: sourceEventID, ReviewID: itemID, AmountIDR: &amount, TransactionAt: &at, TelegramChatID: stamp}))
	must(pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND payload_json->>'review_request_id'=$1`, requestID).Scan(&retirements))
	if retirements != 2 {
		t.Fatalf("replay queued more retirements: %d", retirements)
	}
}
