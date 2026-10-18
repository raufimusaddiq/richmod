package document

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestScreenshotFallbackRecipients(t *testing.T) {
	for _, mode := range []string{"active_member", "inactive_identity", "inactive_member", "no_identities", "source_active_member", "source_inactive_member", "source_inactive_identity", "source_missing_identity", "source_no_eligible", "source_foreign_household"} {
		t.Run(mode, func(t *testing.T) {
			f := seedScreenshotFixture(t, "Screenshot recipient "+mode)
			foreign := seedScreenshotFixture(t, "Foreign screenshot recipient")
			ctx := context.Background()
			var err error
			if strings.HasPrefix(mode, "source_") {
				chat := f.chatID
				if mode == "source_foreign_household" {
					chat = foreign.chatID
				}
				if _, err := f.pool.Exec(ctx, `UPDATE source_event SET telegram_chat_id=$2 WHERE id=$1`, f.sourceID, chat); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "inactive_identity", "source_inactive_identity":
				_, err = f.pool.Exec(ctx, `UPDATE telegram_identity SET active=false WHERE household_id=$1`, f.householdID)
			case "inactive_member", "source_inactive_member", "source_no_eligible":
				_, err = f.pool.Exec(ctx, `UPDATE household_member SET active=false WHERE household_id=$1`, f.householdID)
			case "no_identities", "source_missing_identity":
				_, err = f.pool.Exec(ctx, `DELETE FROM telegram_identity WHERE household_id=$1`, f.householdID)
			}
			if err != nil {
				t.Fatal(err)
			}
			var wantCards, wantSummary []int64
			if mode == "active_member" || mode == "source_active_member" || mode == "source_foreign_household" {
				wantCards, wantSummary = []int64{f.chatID}, []int64{f.chatID}
			}
			if strings.HasPrefix(mode, "source_") && mode != "source_no_eligible" {
				chat := time.Now().UnixNano()
				var user string
				if err := f.pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Fallback','test-only-hash') RETURNING id`, fmt.Sprintf("screenshot-fallback-%d@test.invalid", chat)).Scan(&user); err != nil {
					t.Fatal(err)
				}
				if _, err := f.pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, f.householdID, user); err != nil {
					t.Fatal(err)
				}
				if _, err := f.pool.Exec(ctx, `INSERT INTO telegram_identity(household_id,user_id,telegram_user_id,created_at) VALUES($1,$2,$3,now()-interval '1 day')`, f.householdID, user, chat); err != nil {
					t.Fatal(err)
				}
				wantCards = append(wantCards, chat)
				if mode != "source_active_member" {
					wantSummary = []int64{chat}
				}
			}
			slices.Sort(wantCards)
			row := screenshotDataRow("EXPENSE", "54000", "Warung")
			f.persist(t, rowChoiceProvenance{}, []validatedScreenshotRow{row})
			var reviews, requests int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, f.householdID).Scan(&reviews); err != nil {
				t.Fatal(err)
			}
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM review_request WHERE household_id=$1`, f.householdID).Scan(&requests); err != nil {
				t.Fatal(err)
			}
			wantRequests := 0
			if len(wantCards) > 0 {
				wantRequests = 1
			}
			if reviews != 1 || requests != wantRequests {
				t.Fatalf("reviews=%d requests=%d; want 1/%d", reviews, requests, wantRequests)
			}
			rows, err := f.pool.Query(ctx, `SELECT (payload_json->>'chat_id')::bigint,payload_json ? 'bind_document_id' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'bind_document_id'=$1 OR payload_json->>'review_request_id' IN (SELECT id::text FROM review_request WHERE household_id=$2)) ORDER BY 1`, f.documentID, f.householdID)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var cards, summaries []int64
			for rows.Next() {
				var chat int64
				var summary bool
				if err := rows.Scan(&chat, &summary); err != nil {
					t.Fatal(err)
				}
				if summary {
					summaries = append(summaries, chat)
				} else {
					cards = append(cards, chat)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(cards, wantCards) || !slices.Equal(summaries, wantSummary) {
				t.Fatalf("cards=%v summary=%v; want %v/%v", cards, summaries, wantCards, wantSummary)
			}
		})
	}
}
