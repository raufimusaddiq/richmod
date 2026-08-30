package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type assistantGateway struct{}

func (assistantGateway) Structured(_ context.Context, _ string, _ string, _ string, content any, _ map[string]any, out any) (gateway.Metadata, error) {
	value := out.(*extraction)
	message := strings.TrimSuffix(strings.TrimPrefix(content.(map[string]any)["untrusted_user_message"].(string), "<untrusted_user_message>"), "</untrusted_user_message>")
	period := "THIS_MONTH"
	value.Period = &period
	value.ResponseMessage = "LLM TOTAL PALSU Rp999.999.999"
	switch {
	case strings.HasPrefix(message, "spending"):
		value.Intent = "GET_SPENDING"
	case strings.HasPrefix(message, "cashflow"):
		value.Intent = "GET_CASHFLOW"
	case strings.HasPrefix(message, "search"):
		value.Intent = "SEARCH_TRANSACTIONS"
		query := "Pamella"
		value.SearchText = &query
	case strings.HasPrefix(message, "correct"):
		value.Intent = "CORRECT_TRANSACTION"
		query, category := "Pamella", "belanja-rumah"
		value.SearchText = &query
		value.CorrectionCategorySlug = &category
	case strings.HasPrefix(message, "reviews"):
		value.Intent = "GET_REVIEW_ITEMS"
	case strings.HasPrefix(message, "ambiguous"):
		value.Intent = "SEARCH_TRANSACTIONS"
		value.Ambiguous = true
	default:
		value.Intent = "UNKNOWN"
	}
	return gateway.Metadata{Model: "test"}, nil
}

func TestTelegramAssistantUsesAuthoritativeHouseholdQueries(t *testing.T) {
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
	var householdID, userID, categoryFoodID, categoryHomeID, transactionID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Assistant %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Assistant Owner','unused') RETURNING id`, fmt.Sprintf("assistant-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id`, householdID).Scan(&categoryFoodID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Belanja Rumah','belanja-rumah') RETURNING id`, householdID).Scan(&categoryHomeID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,category_id,counterparty_name,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',125000,now(),$2,'Pamella',now()) RETURNING id`, householdID, categoryFoodID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,counterparty_name) VALUES($1,'INCOME','CONFIRMED',500000,now(),now(),'Gaji')`, householdID); err != nil {
		t.Fatal(err)
	}
	processor := NewProcessor(pool, assistantGateway{})
	processor.now = func() time.Time { return time.Now().In(jakartaLocation()) }
	run := func(message string, messageID int64) string {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"update_id": stamp + messageID, "message": map[string]any{"message_id": messageID, "text": message, "from": map[string]any{"id": chatID}, "chat": map[string]any{"id": chatID}}})
		var sourceID string
		if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("assistant-%d-%d", stamp, messageID), raw).Scan(&sourceID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2)`, sourceID, raw); err != nil {
			t.Fatal(err)
		}
		if err = processor.Process(ctx, sourceID); err != nil {
			t.Fatal(err)
		}
		var reply string
		if err = pool.QueryRow(ctx, `SELECT payload_json->>'text' FROM job WHERE payload_json->>'reply_to_message_id'=$1 ORDER BY created_at DESC LIMIT 1`, fmt.Sprint(messageID)).Scan(&reply); err != nil {
			t.Fatal(err)
		}
		return reply
	}
	if reply := run("spending", 101); !strings.Contains(reply, "Rp125.000") || strings.Contains(reply, "999.999.999") {
		t.Fatalf("spending reply=%q", reply)
	}
	if reply := run("cashflow", 102); !strings.Contains(reply, "Rp500.000") || !strings.Contains(reply, "Rp125.000") {
		t.Fatalf("cashflow reply=%q", reply)
	}
	if reply := run("search", 103); !strings.Contains(reply, "Pamella") || !strings.Contains(reply, "Rp125.000") {
		t.Fatalf("search reply=%q", reply)
	}
	if reply := run("ambiguous", 104); !strings.Contains(reply, "belum cukup jelas") {
		t.Fatalf("ambiguity reply=%q", reply)
	}
	if reply := run("correct", 105); !strings.Contains(reply, "Transaksi diperbarui") || !strings.Contains(reply, "Rp125.000") {
		t.Fatalf("correction reply=%q", reply)
	}
	var correctedCategory string
	var auditCount int
	if err = pool.QueryRow(ctx, `SELECT category_id::text,(SELECT count(*) FROM audit_log WHERE household_id=$1 AND entity_id=$2 AND action='CORRECT_TRANSACTION') FROM transaction WHERE id=$2`, householdID, transactionID).Scan(&correctedCategory, &auditCount); err != nil {
		t.Fatal(err)
	}
	if correctedCategory != categoryHomeID || auditCount != 1 {
		t.Fatalf("category=%s audit=%d", correctedCategory, auditCount)
	}
	var reviewTransactionID string
	if err = pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',75000,now()) RETURNING id`, householdID).Scan(&reviewTransactionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO review_request(household_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY',$3,'PENDING_SEND')`, householdID, reviewTransactionID, chatID); err != nil {
		t.Fatal(err)
	}
	if reply := run("reviews", 106); !strings.Contains(reply, "Rp75.000") {
		t.Fatalf("review reply=%q", reply)
	}
}
