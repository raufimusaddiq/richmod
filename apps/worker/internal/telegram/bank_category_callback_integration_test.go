package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A category-only bank review supplies no date. Its typed-nil *string must not
// clear the existing proposal timestamp when Telegram and Web can both resolve it.
func TestBankCategoryCallbackPreservesProposalDate(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stamp := time.Now().UnixNano()
	chatID := stamp
	var household, user, category, bankSource, proposal, transaction, item, request string
	must := func(err error) { t.Helper(); if err != nil { t.Fatal(err) } }
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank callback %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-callback-%d@example.test", stamp)).Scan(&user))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, household, user)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id`, household).Scan(&category))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprintf("bank-callback-%d", stamp), []byte("bank-callback")).Scan(&bankSource))
	must(pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,transaction_at,confidence,proposal_status) VALUES($1,$2,'EXPENSE',270710,'2026-09-26T13:32:49Z',.98,'NEEDS_REVIEW') RETURNING id`, household, bankSource).Scan(&proposal))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',270710,'2026-09-26T13:32:49Z') RETURNING id`, household).Scan(&transaction))
	_, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'BANK_EMAIL',jsonb_build_object('proposal_id',$3::uuid))`, transaction, bankSource, proposal)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"missingFacts":["category"]}'::jsonb) RETURNING id`, household, transaction).Scan(&item))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY','OPEN',$4) RETURNING id`, household, item, transaction, chatID).Scan(&request))
	_, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,17)`, request, chatID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_CATEGORY')`, request)
	must(err)
	callback := callbackUpdate(chatID, 17, "review:cat:"+category)
	source := seedTelegramRaw(t, pool, household, "TELEGRAM_CALLBACK", map[string]any{"callback_query": callback.CallbackQuery})
	must(NewProcessor(pool, boundReviewGateway{}).Process(ctx, source))
	var status, proposalDate string
	must(pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transaction).Scan(&status))
	must(pool.QueryRow(ctx, `SELECT transaction_at::text FROM transaction_proposal WHERE id=$1`, proposal).Scan(&proposalDate))
	if status != "CONFIRMED" || proposalDate == "" {
		t.Fatalf("transaction=%s proposal date=%q", status, proposalDate)
	}
}
