package telegram

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// An ambiguous investment classification must stay inside Telegram: the card
// offers real household investment accounts and a selected ID completes the same
// shared transfer classification without opening Settings or the Review Inbox.
func TestInvestmentTransferAmbiguityOffersChooser(t *testing.T) {
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
	chat := stamp
	var household, user, account, later, review, item string
	var eligibleIDs []string
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Investment %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("investment-%d@example.test", stamp)).Scan(&user))
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chat, household, user); err != nil {
		t.Fatal(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Rekening','BANK','FULL_LEDGER') RETURNING id`, household).Scan(&account))
	at := time.Now().Add(-time.Hour)
	var transactionID string
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,counterparty_name) VALUES($1,$2,'UNCLASSIFIED','NEEDS_REVIEW',3000000,'IDR',$3,'RDN investasi') RETURNING id`, household, account, at).Scan(&transactionID))
	for _, name := range []string{fmt.Sprintf("Investasi %d-A", stamp), fmt.Sprintf("Investasi %d-B", stamp)} {
		var wealthID string
		must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,$2,'Bibit','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, household, name).Scan(&wealthID))
		eligibleIDs = append(eligibleIDs, wealthID)
		if later == "" {
			later = wealthID
		}
	}
	decision := `{"version":1,"reasonCode":"TRANSFER_CLASSIFICATION","decisionClass":"EVIDENCE_GAP","missingFacts":["transfer_relationship"],"boundedChoices":[],"allowedActions":["EXPENSE","OWN_ACCOUNT_TRANSFER","HOUSEHOLD_TRANSFER","INVESTMENT_TRANSFER","ASSET_PURCHASE","IGNORE"]}`
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSFER_CLASSIFICATION','OPEN',$3::jsonb) RETURNING id`, household, transactionID, decision).Scan(&item))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'TRANSFER_CLASSIFICATION',$4,'OPEN') RETURNING id`, household, item, transactionID, chat).Scan(&review))
	if _, err = pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,81)`, review, chat); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, review); err != nil {
		t.Fatal(err)
	}
	// No deterministic Known Account mapping: the bounded chooser still offers
	// both compatible household investment targets.
	p := NewProcessor(pool, boundReviewGateway{})
	callback := callbackUpdate(chat, 81, "review:investment")
	callbackSource := seedTelegramRaw(t, pool, household, "TELEGRAM_CALLBACK", map[string]any{"callback_query": callback.CallbackQuery})
	if err = p.Process(ctx, callbackSource); err != nil {
		t.Fatal(err)
	}
	var markup string
	must(pool.QueryRow(ctx, `SELECT COALESCE(payload_json->>'reply_markup','') FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND created_at > now() - interval '1 minute' ORDER BY created_at DESC LIMIT 1`).Scan(&markup))
	if !strings.Contains(markup, "review:invest:"+later) {
		t.Fatalf("chooser markup=%s, want account %s", markup, later)
	}
	for i, hint := range []string{"investasi", "rdn investasi"} {
		if _, err = pool.Exec(ctx, `INSERT INTO known_account(household_id,user_id,institution,display_name,match_hint,relationship,wealth_account_id) VALUES($1,$2,'Bibit',$3,$4,'INVESTMENT_ACCOUNT',$5)`, household, user, fmt.Sprintf("Rekening investasi %d", i), hint, eligibleIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	// Multiple matching Known Accounts must also show the same chooser instead
	// of picking one of the two investment Wealth Accounts.
	second := callbackUpdate(chat, 81, "review:investment")
	secondSource := seedTelegramRaw(t, pool, household, "TELEGRAM_CALLBACK", map[string]any{"callback_query": second.CallbackQuery})
	if err = p.Process(ctx, secondSource); err != nil {
		t.Fatal(err)
	}
	var choosers int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND payload_json->>'text'='Pilih Wealth Account investasi tujuan:' AND payload_json->>'chat_id'=$1`, fmt.Sprint(chat)).Scan(&choosers))
	if choosers != 2 {
		t.Fatalf("zero and multiple mapping produced %d choosers, want 2", choosers)
	}
	var foreignHousehold, foreignAccount string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Foreign investment %d", stamp)).Scan(&foreignHousehold))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'Foreign RDN','Bibit','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, foreignHousehold).Scan(&foreignAccount))
	foreignCallback := callbackUpdate(chat, 81, "review:invest:"+foreignAccount)
	foreignSource := seedTelegramRaw(t, pool, household, "TELEGRAM_CALLBACK", map[string]any{"callback_query": foreignCallback.CallbackQuery})
	if err = p.Process(ctx, foreignSource); err != nil {
		t.Fatal(err)
	}
	var stillOpen string
	must(pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, item).Scan(&stillOpen))
	if stillOpen != "OPEN" {
		t.Fatalf("foreign candidate closed the review: %s", stillOpen)
	}
	if _, err = pool.Exec(ctx, `UPDATE wealth_account SET active=false WHERE id=$1`, later); err != nil {
		t.Fatal(err)
	}
	staleCallback := callbackUpdate(chat, 81, "review:invest:"+later)
	staleSource := seedTelegramRaw(t, pool, household, "TELEGRAM_CALLBACK", map[string]any{"callback_query": staleCallback.CallbackQuery})
	if err = p.Process(ctx, staleSource); err != nil {
		t.Fatal(err)
	}
	must(pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, item).Scan(&stillOpen))
	if stillOpen != "OPEN" {
		t.Fatalf("inactive candidate closed the review: %s", stillOpen)
	}
	if _, err = pool.Exec(ctx, `UPDATE wealth_account SET active=true WHERE id=$1`, later); err != nil {
		t.Fatal(err)
	}
	selectCallback := callbackUpdate(chat, 81, "review:invest:"+later)
	selectSource := seedTelegramRaw(t, pool, household, "TELEGRAM_CALLBACK", map[string]any{"callback_query": selectCallback.CallbackQuery})
	if err = p.Process(ctx, selectSource); err != nil {
		t.Fatal(err)
	}
	var typ, purpose, status, related string
	must(pool.QueryRow(ctx, `SELECT type,purpose,status,COALESCE(related_wealth_account_id::text,'') FROM transaction WHERE id=$1`, transactionID).Scan(&typ, &purpose, &status, &related))
	if typ != "TRANSFER" || purpose != "INVESTMENT_CONTRIBUTION" || status != "CONFIRMED" || related != later {
		t.Fatalf("transfer=%s/%s/%s related=%s, want the selected investment account", typ, purpose, status, related)
	}
	var itemStatus string
	must(pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, item).Scan(&itemStatus))
	if itemStatus != "RESOLVED" {
		t.Fatalf("review status=%s, want RESOLVED", itemStatus)
	}
}
