package telegram

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (p *Processor) recordTransfer(ctx context.Context, sourceID, householdID string, update telegramUpdate, args map[string]any) error {
	amount, _ := args["amount_idr"].(string)
	source, _ := args["source_account_id"].(string)
	dest, _ := args["destination_wealth_account_id"].(string)
	purpose, _ := args["purpose"].(string)
	n, ok := new(big.Int).SetString(amount, 10)
	if !ok || n.Sign() <= 0 || n.String() != amount {
		return p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Nominal transfer harus berupa IDR bulat positif.")
	}
	dateRef, _ := args["date_reference"].(string)
	at := p.now().In(jakartaLocation())
	if dateRef == "YESTERDAY" {
		at = at.AddDate(0, 0, -1)
	}
	if dateRef != "TODAY" && dateRef != "YESTERDAY" && dateRef != "EXPLICIT" {
		return p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Tanggal transfer tidak valid.")
	}
	if dateRef == "EXPLICIT" {
		d, err := time.ParseInLocation("2006-01-02", fmt.Sprint(args["explicit_date"]), jakartaLocation())
		if err != nil || d.Format("2006-01-02") != fmt.Sprint(args["explicit_date"]) {
			return p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Tanggal transfer tidak valid. Gunakan format YYYY-MM-DD.")
		}
		at = time.Date(d.Year(), d.Month(), d.Day(), at.Hour(), at.Minute(), 0, 0, jakartaLocation())
	}
	desc, _ := args["description"].(string)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return err
	}
	var accountID, wealthID string
	if err = tx.QueryRow(ctx, `SELECT id FROM account WHERE id=$1 AND household_id=$2 AND active`, source, householdID).Scan(&accountID); err != nil {
		return p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Rekening sumber tidak ditemukan secara unik dalam household.")
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM wealth_account WHERE id=$1 AND household_id=$2 AND active`, dest, householdID).Scan(&wealthID); err != nil {
		return p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Wealth Account tujuan tidak ditemukan secara unik dalam household.")
	}
	var existing string
	if err = tx.QueryRow(ctx, `SELECT t.id FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1 AND t.household_id=$2 LIMIT 1`, sourceID, householdID).Scan(&existing); err == nil {
		return p.finishWithoutTransaction(ctx, sourceID, "PROCESSED", update, "Transfer ini sudah tercatat; tidak ada duplikasi.")
	}
	if err != pgx.ErrNoRows {
		return err
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,description,counterparty_name,created_by_user_id,purpose,related_wealth_account_id,confirmed_at) VALUES($1,$2,'TRANSFER','CONFIRMED',$3,'IDR',$4,NULLIF($5,''),NULL,$6,$7,$8,now()) RETURNING id`, householdID, accountID, amount, at, strings.TrimSpace(desc), userID, purpose, wealthID).Scan(&id)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,'TELEGRAM_TEXT',1) ON CONFLICT DO NOTHING`, id, sourceID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-transfer',parser_version='1' WHERE id=$1`, sourceID); err != nil {
		return err
	}
	if err = enqueueReply(ctx, tx, update, "✅ Transfer tercatat\nRp"+FormatIDR(amount)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
