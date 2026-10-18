package bankemail

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBankEmailPersistRecipients(t *testing.T) {
	for _, mode := range []string{"all_active", "inactive_identity", "inactive_member", "foreign_household", "no_identities", "retry_after_pruning", "concurrent_retry", "rollback_after_notice", "review_inactive_member", "review_active_members", "completion_no_notice", "source_household_mismatch"} {
		t.Run(mode, func(t *testing.T) {
			if os.Getenv("TEST_DATABASE_URL") == "" {
				t.Skip("TEST_DATABASE_URL is not configured")
			}
			ctx := context.Background()
			pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
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
			var household, foreign, owner, source, category string
			listener := Listener{TrackingPolicy: "SPENDING_ONLY", Active: true}
			must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Notice fixture') RETURNING id`).Scan(&household))
			must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Foreign notice fixture') RETURNING id`).Scan(&foreign))
			must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-notice-%d@test.invalid", stamp)).Scan(&owner))
			must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Bank','bank@test.invalid',$2) RETURNING id`, household, owner).Scan(&listener.ID))
			listener.HouseholdID = household
			must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food','food') RETURNING id`, household).Scan(&category))
			must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&source))
			_, err = pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{}','VALID')`, source, listener.ID)
			must(err)
			addIdentity := func(householdID string, identityActive, memberActive bool, offset int64) int64 {
				t.Helper()
				var user string
				must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Recipient','unused') RETURNING id`, fmt.Sprintf("bank-recipient-%d-%d@test.invalid", stamp, offset)).Scan(&user))
				_, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role,active) VALUES($1,$2,'MEMBER',$3)`, householdID, user, memberActive)
				must(err)
				_, err = pool.Exec(ctx, `INSERT INTO telegram_identity(household_id,user_id,telegram_user_id,active) VALUES($1,$2,$3,$4)`, householdID, user, stamp+offset, identityActive)
				must(err)
				return stamp + offset
			}
			var want []int64
			switch mode {
			case "all_active", "retry_after_pruning", "concurrent_retry", "rollback_after_notice", "review_active_members", "completion_no_notice", "source_household_mismatch":
				want = []int64{addIdentity(household, true, true, 1), addIdentity(household, true, true, 2)}
				addIdentity(household, false, true, 3)
				addIdentity(household, true, false, 4)
				addIdentity(foreign, true, true, 5)
			case "inactive_identity":
				addIdentity(household, false, true, 1)
			case "inactive_member", "review_inactive_member":
				addIdentity(household, true, false, 1)
			case "foreign_household":
				addIdentity(foreign, true, true, 1)
			}
			amount, merchant, description := "54000", "Warung", fmt.Sprintf("Makan fixture %d", stamp)
			at := time.Date(2026, 10, 19, 1, 30, 0, 0, time.UTC)
			extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Merchant: &merchant, Description: &description, TransactionAt: &at, Confidence: .95}
			result := PolicyResult{Type: "EXPENSE", Status: "CONFIRMED", AutoConfirm: true, CategoryID: category}
			if strings.HasPrefix(mode, "review_") {
				result.Status, result.AutoConfirm, result.ReviewType = "NEEDS_REVIEW", false, "AMBIGUOUS_CATEGORY"
			}
			completing := ""
			if mode == "completion_no_notice" {
				completing, want = source, nil
			}
			if mode == "source_household_mismatch" {
				listener.HouseholdID = foreign
				want = nil
			}
			processor := &Processor{pool: pool}
			if mode == "rollback_after_notice" {
				validID := listener.ID
				listener.ID = "invalid-uuid"
				if err := processor.persist(ctx, listener, source, extraction, result, ""); err == nil {
					t.Fatal("invalid listener must fail after notice enqueue")
				}
				var marked bool
				var sends, transactions int
				must(pool.QueryRow(ctx, `SELECT notice_at IS NOT NULL FROM bank_email_extraction WHERE source_event_id=$1`, source).Scan(&marked))
				must(pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE payload_json->>'text' LIKE $1`, "%"+description+"%").Scan(&sends))
				must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, household).Scan(&transactions))
				if marked || sends != 0 || transactions != 0 {
					t.Fatalf("rollback left marker=%v jobs=%d transactions=%d", marked, sends, transactions)
				}
				listener.ID = validID
			}
			if mode == "concurrent_retry" {
				outcomes := make(chan error, 2)
				for range 2 {
					go func() { outcomes <- processor.persist(ctx, listener, source, extraction, result, "") }()
				}
				err = <-outcomes
				must(<-outcomes)
			} else {
				err = processor.persist(ctx, listener, source, extraction, result, completing)
			}
			if mode == "source_household_mismatch" {
				if err == nil {
					t.Fatal("foreign caller household accepted")
				}
			} else {
				must(err)
			}
			if mode == "retry_after_pruning" {
				must(processor.persist(ctx, listener, source, extraction, result, ""))
			}
			var got []int64
			rows, err := pool.Query(ctx, `SELECT (payload_json->>'chat_id')::bigint,payload_json->>'text',payload_json ? 'reply_to_message_id' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'text' LIKE $1 ORDER BY 1`, "%"+description+"%")
			must(err)
			for rows.Next() {
				var chat int64
				var text string
				var reply bool
				must(rows.Scan(&chat, &text, &reply))
				got = append(got, chat)
				wantNotice := "Transaksi bank sudah tercatat.\n\nNominal: Rp54.000\nWaktu: 19/10/2026 08:30 WIB\nKeterangan: " + description
				if result.AutoConfirm && (reply || text != wantNotice) {
					t.Fatalf("invalid recorded notice: reply=%v text=%q", reply, text)
				}
			}
			must(rows.Err())
			rows.Close()
			if !slices.Equal(got, want) {
				t.Fatalf("recipients=%v; want %v", got, want)
			}
			var transactions, reviews int
			must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction_evidence WHERE source_event_id=$1`, source).Scan(&transactions))
			if mode == "source_household_mismatch" {
				if transactions != 0 {
					t.Fatal("household mismatch wrote ledger evidence")
				}
				return
			}
			if transactions != 1 {
				t.Fatalf("transaction evidence=%d; want 1", transactions)
			}
			must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, household).Scan(&transactions))
			if transactions != 1 {
				t.Fatalf("ledger transactions=%d; want 1", transactions)
			}
			var marked bool
			must(pool.QueryRow(ctx, `SELECT notice_at IS NOT NULL FROM bank_email_extraction WHERE source_event_id=$1`, source).Scan(&marked))
			if marked != (result.AutoConfirm && completing == "") {
				t.Fatalf("notice marker=%v", marked)
			}
			if strings.HasPrefix(mode, "review_") {
				must(pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE transaction_id IN (SELECT transaction_id FROM transaction_evidence WHERE source_event_id=$1) AND status IN ('OPEN','PENDING_SEND')`, source).Scan(&reviews))
				if reviews != 1 {
					t.Fatalf("canonical reviews=%d; want 1", reviews)
				}
				if mode == "review_inactive_member" {
					var requests int
					must(pool.QueryRow(ctx, `SELECT count(*) FROM review_request WHERE household_id=$1`, household).Scan(&requests))
					if requests != 0 {
						t.Fatalf("inactive member created Telegram projection: %d", requests)
					}
				}
			}
			if mode == "retry_after_pruning" {
				_, err = pool.Exec(ctx, `DELETE FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'text' LIKE $1`, "%"+description+"%")
				must(err)
				must(processor.persist(ctx, listener, source, extraction, result, ""))
				var sends int
				must(pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'text' LIKE $1`, "%"+description+"%").Scan(&sends))
				if sends != 0 {
					t.Fatalf("pruned notice requeued: %d", sends)
				}
				must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, household).Scan(&transactions))
				if transactions != 1 {
					t.Fatalf("ledger transactions after pruning=%d; want 1", transactions)
				}
			}
		})
	}
}
