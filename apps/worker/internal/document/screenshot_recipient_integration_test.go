package document

import (
	"context"
	"strings"
	"testing"
)

func TestScreenshotFallbackRecipients(t *testing.T) {
	for _, mode := range []string{"active_member", "inactive_identity", "inactive_member", "no_identities", "source_active_member", "source_inactive_member", "source_foreign_household"} {
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
			case "inactive_identity":
				_, err = f.pool.Exec(ctx, `UPDATE telegram_identity SET active=false WHERE household_id=$1`, f.householdID)
			case "inactive_member", "source_inactive_member":
				_, err = f.pool.Exec(ctx, `UPDATE household_member SET active=false WHERE household_id=$1`, f.householdID)
			case "no_identities":
				_, err = f.pool.Exec(ctx, `DELETE FROM telegram_identity WHERE household_id=$1`, f.householdID)
			}
			if err != nil {
				t.Fatal(err)
			}
			row := screenshotDataRow("EXPENSE", "54000", "Warung")
			f.persist(t, rowChoiceProvenance{}, []validatedScreenshotRow{row})
			var reviews, requests int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, f.householdID).Scan(&reviews); err != nil {
				t.Fatal(err)
			}
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM review_request WHERE household_id=$1`, f.householdID).Scan(&requests); err != nil {
				t.Fatal(err)
			}
			wantRequests, wantSends := 0, 0
			eligible := mode == "active_member" || mode == "source_active_member"
			if eligible {
				wantRequests, wantSends = 1, 2
			}
			if reviews != 1 || requests != wantRequests {
				t.Fatalf("reviews=%d requests=%d; want 1/%d", reviews, requests, wantRequests)
			}
			rows, err := f.pool.Query(ctx, `SELECT (payload_json->>'chat_id')::bigint FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'bind_document_id'=$1 OR payload_json->>'review_request_id' IN (SELECT id::text FROM review_request WHERE household_id=$2))`, f.documentID, f.householdID)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			sends := 0
			for rows.Next() {
				var chat int64
				if err := rows.Scan(&chat); err != nil {
					t.Fatal(err)
				}
				if chat != f.chatID || chat == foreign.chatID || !eligible {
					t.Fatalf("ineligible screenshot recipient %d", chat)
				}
				sends++
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if sends != wantSends {
				t.Fatalf("sends=%d; want %d", sends, wantSends)
			}
		})
	}
}
