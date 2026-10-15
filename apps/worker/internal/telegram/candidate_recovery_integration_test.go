package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

func TestTelegramDateRecoversDeactivatedPartialClassificationCandidate(t *testing.T) {
	for _, choice := range []struct{ classification, role, fact string }{
		{"ASSET_PURCHASE", "OTHER", "wealth_account"},
		{"INVESTMENT_ACCOUNT", "INVESTMENT", "wealth_account"},
		{"EXPENSE", "", "category"},
	} {
		t.Run(choice.classification, func(t *testing.T) {
			ctx := context.Background()
			f, transaction, proposal, request := seedAttributionReview(t, "UNCLASSIFIED", "TRANSFER_CLASSIFICATION", false)
			_, err := f.pool.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(decision,'{missingFacts}','["transfer_relationship","transaction_at"]') WHERE transaction_id=$1`, transaction)
			mustAgentTest(t, err)
			var account, category string
			if choice.role != "" {
				mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Synthetic old asset','ASSET','GOLD',$2) RETURNING id`, f.householdID, choice.role).Scan(&account))
			} else {
				mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Synthetic old category','old') RETURNING id`, f.householdID).Scan(&category))
			}
			tx, err := f.pool.Begin(ctx)
			mustAgentTest(t, err)
			_, err = reviewdomain.ClassifyTransferReview(ctx, tx, reviewdomain.TransferCommand{HouseholdID: f.householdID, ActorUserID: f.userID, TransactionID: transaction, Classification: choice.classification, CategoryID: category, WealthAccountID: account})
			var missing *reviewdomain.ErrMissingFacts
			if !errors.As(err, &missing) {
				_ = tx.Rollback(ctx)
				t.Fatalf("partial classification: %v", err)
			}
			card := f.update
			card.Message.MessageID = 99
			mustAgentTest(t, refreshReviewFactsProjection(ctx, tx, f.householdID, request, card))
			mustAgentTest(t, tx.Commit(ctx))
			if account != "" {
				_, err = f.pool.Exec(ctx, `UPDATE wealth_account SET active=false WHERE id=$1`, account)
			} else {
				_, err = f.pool.Exec(ctx, `UPDATE category SET active=false WHERE id=$1`, category)
			}
			mustAgentTest(t, err)
			p := NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"CONFIRM","transaction_at":"2026-10-03"}`})
			sendTypedReviewReply(t, ctx, f, p, request, "2026-10-03", true)
			var pending bool
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT decision->'missingFacts' ? $2 FROM review_item WHERE transaction_id=$1`, transaction, choice.fact).Scan(&pending))
			if !pending {
				t.Fatal("inactive candidate was not re-asked")
			}
			var replacement string
			var callback string
			if account != "" {
				mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Synthetic replacement asset','ASSET','GOLD',$2) RETURNING id`, f.householdID, choice.role).Scan(&replacement))
				button := "review:investment"
				if choice.classification == "ASSET_PURCHASE" {
					button = "review:asset"
				}
				raw, err := json.Marshal(callbackUpdate(f.chatID, 99, button))
				mustAgentTest(t, err)
				mustAgentTest(t, p.Process(ctx, f.seedSourceEvent(ctx, t, "reselect-account", raw)))
				if choice.classification == "ASSET_PURCHASE" {
					f.update.Message.Text = "Synthetic replacement asset"
					f.update.Message.ReplyToMessage = &struct {
						MessageID int64 `json:"message_id"`
					}{99}
					raw, err := json.Marshal(f.update)
					mustAgentTest(t, err)
					p = NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"ASSET_PURCHASE","wealth_account_hint":"Synthetic replacement asset"}`})
					source := f.seedSourceEvent(ctx, t, "replacement-asset", raw)
					_, err = f.pool.Exec(ctx, `UPDATE source_event SET source_type='TELEGRAM_TEXT' WHERE id=$1`, source)
					mustAgentTest(t, err)
					mustAgentTest(t, p.ProcessAgent(ctx, source))
				} else {
					var offered bool
					mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM job WHERE payload_json::text LIKE '%'||$1||'%')`, "review:invest:"+replacement).Scan(&offered))
					if !offered {
						t.Fatal("replacement account was not offered")
					}
					callback = "review:invest:" + replacement
				}
			} else {
				mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Synthetic replacement category','replacement') RETURNING id`, f.householdID).Scan(&replacement))
				callback = "review:cat:" + replacement
			}
			if callback != "" {
				raw, err := json.Marshal(callbackUpdate(f.chatID, 99, callback))
				mustAgentTest(t, err)
				mustAgentTest(t, p.Process(ctx, f.seedSourceEvent(ctx, t, "replacement-choice", raw)))
			}
			var status, selected, itemStatus, requestStatus, proposalStatus, date string
			mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,COALESCE(t.related_wealth_account_id,t.category_id)::text,ri.status,r.status,p.proposal_status,to_char(t.transaction_at AT TIME ZONE 'Asia/Jakarta','YYYY-MM-DD') FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id JOIN review_request r ON r.review_item_id=ri.id JOIN transaction_proposal p ON p.id=$2 WHERE t.id=$1`, transaction, proposal).Scan(&status, &selected, &itemStatus, &requestStatus, &proposalStatus, &date))
			if status != "CONFIRMED" || selected != replacement || itemStatus != "RESOLVED" || requestStatus != "RESOLVED" || proposalStatus != "ACCEPTED" || date != "2026-10-03" {
				t.Fatalf("recovery: status=%s replacementMatches=%v review=%s/%s proposal=%s date=%s", status, selected == replacement, itemStatus, requestStatus, proposalStatus, date)
			}
		})
	}
}

func TestTypedAssetClassificationRetainsDateResidual(t *testing.T) {
	ctx := context.Background()
	f, transaction, _, request := seedAttributionReview(t, "UNCLASSIFIED", "TRANSFER_CLASSIFICATION", false)
	var account string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Synthetic asset','ASSET','GOLD','OTHER') RETURNING id`, f.householdID).Scan(&account))
	p := NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"ASSET_PURCHASE","wealth_account_hint":"Synthetic asset"}`})
	sendTypedReviewReply(t, ctx, f, p, request, "Synthetic asset", true)
	var status, kind, relationship string
	var missingDate bool
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,t.type,ri.decision->'knownFacts'->>'relationship_choice',ri.decision->'missingFacts' ? 'transaction_at' FROM transaction t JOIN review_item ri ON ri.transaction_id=t.id WHERE t.id=$1`, transaction).Scan(&status, &kind, &relationship, &missingDate))
	if status != "NEEDS_REVIEW" || kind != "TRANSFER" || relationship != "ASSET_PURCHASE" || !missingDate {
		t.Fatalf("typed partial: status=%s type=%s relationship=%s missingDate=%v", status, kind, relationship, missingDate)
	}
	f.update.Message.Text = "2026-10-03"
	f.update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{99}
	raw, err := json.Marshal(f.update)
	mustAgentTest(t, err)
	source := f.seedSourceEvent(ctx, t, "typed-asset-date", raw)
	_, err = f.pool.Exec(ctx, `UPDATE source_event SET source_type='TELEGRAM_TEXT' WHERE id=$1`, source)
	mustAgentTest(t, err)
	p = NewProcessor(f.pool, &typedReplyGateway{arguments: `{"action":"CONFIRM","transaction_at":"2026-10-03"}`})
	mustAgentTest(t, p.ProcessAgent(ctx, source))
	var selected string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status,related_wealth_account_id::text FROM transaction WHERE id=$1`, transaction).Scan(&status, &selected))
	if status != "CONFIRMED" || selected != account {
		t.Fatalf("typed completion: status=%s accountMatches=%v", status, selected == account)
	}
}
