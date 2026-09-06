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
	source, _ := args["source_account_hint"].(string)
	dest, _ := args["destination_wealth_account_hint"].(string)
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
	accountID, err := resolveUniqueAccountHint(ctx, tx, householdID, source)
	if err != nil {
		return p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Rekening sumber tidak ditemukan secara unik dalam household. Sebutkan nama rekening yang persis seperti di Pengaturan.")
	}
	wealthID := ""
	if purpose != "INTERNAL_TRANSFER" {
		wealthID, err = resolveUniqueWealthHint(ctx, tx, householdID, dest)
		if err != nil {
			return p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Wealth Account tujuan tidak ditemukan secara unik dalam household. Sebutkan nama Wealth Account yang persis seperti di Pengaturan.")
		}
	}
	var existing string
	if err = tx.QueryRow(ctx, `SELECT t.id FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1 AND t.household_id=$2 LIMIT 1`, sourceID, householdID).Scan(&existing); err == nil {
		return p.finishWithoutTransaction(ctx, sourceID, "PROCESSED", update, "Transfer ini sudah tercatat; tidak ada duplikasi.")
	}
	if err != pgx.ErrNoRows {
		return err
	}
	dayStart := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, jakartaLocation()).UTC()
	dayEnd := dayStart.AddDate(0, 0, 1)
	type candidate struct{ id, existingPurpose, existingWealth string }
	rows, err := tx.Query(ctx, `SELECT id::text,purpose,COALESCE(related_wealth_account_id::text,'') FROM transaction WHERE household_id=$1 AND account_id=$2 AND type='TRANSFER' AND status='CONFIRMED' AND amount=$3 AND transaction_at >= $4 AND transaction_at < $5 ORDER BY transaction_at,id LIMIT 2`, householdID, accountID, amount, dayStart, dayEnd)
	if err != nil {
		return err
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.existingPurpose, &c.existingWealth); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, c)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(candidates) > 1 {
		return p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Ada lebih dari satu transfer yang cocok. Tambahkan tanggal atau nama rekening tujuan agar tidak salah mencatat.")
	}
	id := ""
	if len(candidates) == 1 {
		candidate := candidates[0]
		if candidate.existingPurpose != purpose || (wealthID != "" && candidate.existingWealth != "" && candidate.existingWealth != wealthID) {
			return p.finishWithoutTransaction(ctx, sourceID, "NEEDS_REVIEW", update, "Transfer yang cocok sudah memiliki tujuan berbeda. Tinjau di Inbox agar bukti tidak salah digabung.")
		}
		if _, err = tx.Exec(ctx, `UPDATE transaction SET purpose=$2,related_wealth_account_id=NULLIF($3,'')::uuid,description=COALESCE(NULLIF(description,''),NULLIF($4,'')),updated_at=now() WHERE id=$1`, candidate.id, purpose, wealthID, strings.TrimSpace(desc)); err != nil {
			return err
		}
		id = candidate.id
	} else {
		err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,description,counterparty_name,created_by_user_id,purpose,related_wealth_account_id,confirmed_at) VALUES($1,$2,'TRANSFER','CONFIRMED',$3,'IDR',$4,NULLIF($5,''),NULL,$6,$7,NULLIF($8,'')::uuid,now()) RETURNING id`, householdID, accountID, amount, at, strings.TrimSpace(desc), userID, purpose, wealthID).Scan(&id)
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,'TELEGRAM_TEXT',1) ON CONFLICT DO NOTHING`, id, sourceID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-transfer',parser_version='1' WHERE id=$1`, sourceID); err != nil {
		return err
	}
	if err = enqueueReply(ctx, tx, update, "✅ Transfer tercatat tanpa duplikasi\nRp"+FormatIDR(amount)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func resolveUniqueAccountHint(ctx context.Context, tx pgx.Tx, householdID, hint string) (string, error) {
	rows, err := tx.Query(ctx, `SELECT id::text FROM account WHERE household_id=$1 AND active AND lower(regexp_replace(btrim(name),'[[:space:]]+',' ','g'))=lower(regexp_replace(btrim($2),'[[:space:]]+',' ','g')) ORDER BY id LIMIT 2`, householdID, hint)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("account hint is not unique")
	}
	return ids[0], nil
}

func resolveUniqueWealthHint(ctx context.Context, tx pgx.Tx, householdID, hint string) (string, error) {
	rows, err := tx.Query(ctx, `SELECT id::text FROM wealth_account WHERE household_id=$1 AND active AND (lower(btrim(name))=lower(btrim($2)) OR lower(btrim(COALESCE(institution,'')))=lower(btrim($2))) ORDER BY id LIMIT 2`, householdID, hint)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("wealth hint is not unique")
	}
	return ids[0], nil
}
