package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRecordTransferReusesOneCrossChannelCandidateAndRejectsAmbiguity(t *testing.T) {
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
	chatID := stamp
	var householdID, userID, accountID, wealthID string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Transfer dedup %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("transfer-dedup-%d@example.test", stamp)).Scan(&userID))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Jago','BANK','SPENDING_ONLY') RETURNING id`, householdID).Scan(&accountID))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'RDN','BCA','ASSET','BROKERAGE','INVESTMENT') RETURNING id`, householdID).Scan(&wealthID))

	day := time.Date(2026, 9, 3, 12, 0, 0, 0, jakartaLocation())
	var bankSourceID, canonicalID string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,$3,$4,'PROCESSED') RETURNING id`, householdID, fmt.Sprintf("bank-%d", stamp), day, []byte(fmt.Sprintf("bank-%d", stamp))).Scan(&bankSourceID))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,transaction_at,purpose,related_wealth_account_id,confirmed_at) VALUES($1,$2,'TRANSFER','CONFIRMED',3000000,$3,'INVESTMENT_CONTRIBUTION',$4,now()) RETURNING id`, householdID, accountID, day, wealthID).Scan(&canonicalID))
	_, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'BANK_EMAIL')`, canonicalID, bankSourceID)
	must(err)

	newSource := func(suffix string) string {
		var id string
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,$3,$4,'PROCESSING') RETURNING id`, householdID, fmt.Sprintf("telegram-%s-%d", suffix, stamp), day, []byte(fmt.Sprintf("telegram-%s-%d", suffix, stamp))).Scan(&id))
		return id
	}
	update := telegramUpdate{}
	update.Message.From.ID, update.Message.Chat.ID, update.Message.MessageID = chatID, chatID, 1
	processor := NewProcessor(pool, nil)
	processor.now = func() time.Time { return day }
	args := map[string]any{"amount_idr": "3000000", "source_account_hint": "Jago", "destination_wealth_account_hint": "RDN", "purpose": "INVESTMENT_CONTRIBUTION", "date_reference": "TODAY", "description": "top up RDN"}
	reuseSourceID := newSource("reuse")
	must(processor.recordTransfer(ctx, reuseSourceID, householdID, update, args))
	var transactions, evidence int
	must(pool.QueryRow(ctx, `SELECT count(*),(SELECT count(*) FROM transaction_evidence WHERE transaction_id=$2) FROM transaction WHERE household_id=$1`, householdID, canonicalID).Scan(&transactions, &evidence))
	if transactions != 1 || evidence != 2 {
		var status, parser string
		var reply string
		_ = pool.QueryRow(ctx, `SELECT processing_status,COALESCE(parser_name,'') FROM source_event WHERE id=$1`, reuseSourceID).Scan(&status, &parser)
		_ = pool.QueryRow(ctx, `SELECT COALESCE(payload_json->>'text','') FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'reply_to_message_id'='1' ORDER BY id DESC LIMIT 1`).Scan(&reply)
		t.Fatalf("transactions=%d evidence=%d source=%s parser=%s reply=%s", transactions, evidence, status, parser, reply)
	}

	_, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,transaction_at,purpose,related_wealth_account_id,confirmed_at) VALUES($1,$2,'TRANSFER','CONFIRMED',3000000,$3,'INVESTMENT_CONTRIBUTION',$4,now())`, householdID, accountID, day.Add(time.Hour), wealthID)
	must(err)
	must(processor.recordTransfer(ctx, newSource("ambiguous"), householdID, update, args))
	must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, householdID).Scan(&transactions))
	if transactions != 2 {
		t.Fatalf("ambiguous transfer created transaction: %d", transactions)
	}
}
