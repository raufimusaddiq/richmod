package telegram

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type assistantRange struct{ From, To time.Time }

func (r assistantRange) label() string {
	from := formatIDDate(r.From)
	to := formatIDDate(r.To.In(jakartaLocation()).AddDate(0, 0, -1))
	if from == to {
		return from
	}
	return from + "–" + to
}

func (p *Processor) resolveSalaryCycleRange(ctx context.Context, householdID string, now time.Time, previous bool) (assistantRange, error) {
	var current, next, prior *time.Time
	err := p.pool.QueryRow(ctx, `WITH anchors AS (SELECT se.pay_date FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id AND ss.household_id=se.household_id WHERE se.household_id=$1 AND ss.active AND ss.is_primary AND se.status='CONFIRMED') SELECT (SELECT max(pay_date)::timestamp AT TIME ZONE 'Asia/Jakarta' FROM anchors WHERE pay_date <= $2::date),(SELECT min(pay_date)::timestamp AT TIME ZONE 'Asia/Jakarta' FROM anchors WHERE pay_date > $2::date),(SELECT max(pay_date)::timestamp AT TIME ZONE 'Asia/Jakarta' FROM anchors WHERE pay_date < (SELECT max(pay_date) FROM anchors WHERE pay_date <= $2::date))`, householdID, now.In(jakartaLocation()).Format("2006-01-02")).Scan(&current, &next, &prior)
	if err != nil || current == nil {
		return assistantRange{}, errors.New("salary cycle unavailable")
	}
	if previous {
		if prior == nil {
			return assistantRange{}, errors.New("previous salary cycle unavailable")
		}
		return assistantRange{From: *prior, To: *current}, nil
	}
	if next == nil {
		nextValue := now.In(jakartaLocation())
		nextValue = time.Date(nextValue.Year(), nextValue.Month(), nextValue.Day()+1, 0, 0, 0, 0, jakartaLocation())
		next = &nextValue
	}
	return assistantRange{From: *current, To: *next}, nil
}

func resolveAssistantRange(now time.Time, period, fromDate, toDate *string) (assistantRange, error) {
	local := now.In(jakartaLocation())
	startDay := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, local.Location())
	}
	periodValue := pointerValue(period)
	if periodValue == "" {
		periodValue = "THIS_MONTH"
	}
	var from, to time.Time
	switch periodValue {
	case "TODAY":
		from = startDay(local)
		to = from.AddDate(0, 0, 1)
	case "THIS_WEEK":
		from = startDay(local).AddDate(0, 0, -((int(local.Weekday()) + 6) % 7))
		to = from.AddDate(0, 0, 7)
	case "LAST_WEEK":
		to = startDay(local).AddDate(0, 0, -((int(local.Weekday()) + 6) % 7))
		from = to.AddDate(0, 0, -7)
	case "THIS_MONTH":
		from = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, local.Location())
		to = from.AddDate(0, 1, 0)
	case "LAST_MONTH":
		to = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, local.Location())
		from = to.AddDate(0, -1, 0)
	case "CUSTOM":
		if fromDate == nil || toDate == nil {
			return assistantRange{}, errors.New("custom range missing")
		}
		var err error
		from, err = time.ParseInLocation("2006-01-02", *fromDate, local.Location())
		if err != nil {
			return assistantRange{}, err
		}
		to, err = time.ParseInLocation("2006-01-02", *toDate, local.Location())
		if err != nil {
			return assistantRange{}, err
		}
		to = to.AddDate(0, 0, 1)
	default:
		return assistantRange{}, errors.New("invalid period")
	}
	if !to.After(from) || to.Sub(from) > 366*24*time.Hour || from.After(local.AddDate(0, 0, 1)) {
		return assistantRange{}, errors.New("range outside bounds")
	}
	return assistantRange{From: from, To: to}, nil
}

func (p *Processor) replySpending(ctx context.Context, sourceID, householdID string, update telegramUpdate, r assistantRange) error {
	var total, topName, topAmount string
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at >= $2 AND transaction_at < $3`, householdID, r.From, r.To).Scan(&total)
	if err != nil {
		return err
	}
	err = p.pool.QueryRow(ctx, `SELECT COALESCE(c.name,'Tanpa kategori'),sum(CASE WHEN t.type='EXPENSE' THEN t.amount ELSE -t.amount END)::text FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type IN('EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3 GROUP BY COALESCE(c.name,'Tanpa kategori') HAVING sum(CASE WHEN t.type='EXPENSE' THEN t.amount ELSE -t.amount END)>0 ORDER BY sum(CASE WHEN t.type='EXPENSE' THEN t.amount ELSE -t.amount END) DESC LIMIT 1`, householdID, r.From, r.To).Scan(&topName, &topAmount)
	message := "💸 Pengeluaran\nPeriode: " + r.label() + "\n\nTotal: Rp" + FormatIDR(total)
	if err == nil {
		message += "\nTerbesar: " + topName + " (Rp" + FormatIDR(topAmount) + ")"
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return p.finishAssistant(ctx, sourceID, update, message, nil)
}

func (p *Processor) replySavings(ctx context.Context, sourceID, householdID string, update telegramUpdate, r assistantRange) error {
	var total string
	if err := p.pool.QueryRow(ctx, `SELECT COALESCE(sum(amount),0)::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND type='TRANSFER' AND purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE') AND transaction_at >= $2 AND transaction_at < $3`, householdID, r.From, r.To).Scan(&total); err != nil {
		return err
	}
	return p.finishAssistant(ctx, sourceID, update, "💾 Tabungan\nPeriode: "+r.label()+"\n\nTotal: Rp"+FormatIDR(total), nil)
}

func (p *Processor) replyWealth(ctx context.Context, sourceID, householdID string, update telegramUpdate) error {
	var observed time.Time
	var value string
	if err := p.pool.QueryRow(ctx, `SELECT s.observed_at,COALESCE(sum(CASE WHEN w.side='LIABILITY' THEN -i.value_idr ELSE i.value_idr END),0)::text FROM wealth_snapshot s JOIN wealth_snapshot_item i ON i.snapshot_id=s.id JOIN wealth_account w ON w.id=i.wealth_account_id WHERE s.household_id=$1 AND s.id=(SELECT id FROM wealth_snapshot WHERE household_id=$1 ORDER BY observed_at DESC,id DESC LIMIT 1) GROUP BY s.id,s.observed_at`, householdID).Scan(&observed, &value); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return p.finishAssistant(ctx, sourceID, update, "Belum ada Wealth Snapshot.", nil)
		}
		return err
	}
	return p.finishAssistant(ctx, sourceID, update, "💎 Net worth sekarang\nRp"+FormatIDR(value)+"\nDiamati: "+observed.In(jakartaLocation()).Format(time.RFC3339), nil)
}

func (p *Processor) replyCashflow(ctx context.Context, sourceID, householdID string, update telegramUpdate, r assistantRange) error {
	var income, expense, net string
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text,COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)::text,(COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)-COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0))::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at >= $2 AND transaction_at < $3`, householdID, r.From, r.To).Scan(&income, &expense, &net)
	if err != nil {
		return err
	}
	return p.finishAssistant(ctx, sourceID, update, "💰 Arus kas\nPeriode: "+r.label()+"\n\nPemasukan: Rp"+FormatIDR(income)+"\nPengeluaran: Rp"+FormatIDR(expense)+"\nArus kas bersih: Rp"+FormatIDR(net), nil)
}

func (p *Processor) replyReviews(ctx context.Context, sourceID, householdID string, update telegramUpdate) error {
	rows, err := p.pool.Query(ctx, `SELECT r.review_type,t.amount::text,COALESCE(t.counterparty_name,t.description,'Transaksi') FROM review_request r JOIN transaction t ON t.id=r.transaction_id WHERE r.household_id=$1 AND r.status IN('PENDING_SEND','OPEN') ORDER BY r.created_at DESC LIMIT 5`, householdID)
	if err != nil {
		return err
	}
	defer rows.Close()
	lines := []string{"🟡 Review terbuka"}
	for rows.Next() {
		var kind, amount, label string
		if err = rows.Scan(&kind, &amount, &label); err != nil {
			return err
		}
		lines = append(lines, "• "+label+" · Rp"+FormatIDR(amount)+" · "+kind)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(lines) == 1 {
		lines = []string{"✅ Tidak ada review yang terbuka."}
	}
	return p.finishAssistant(ctx, sourceID, update, strings.Join(lines, "\n"), nil)
}

func (p *Processor) finishAssistant(ctx context.Context, sourceID string, update telegramUpdate, message string, markup *InlineKeyboardMarkup) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-assistant',parser_version='1' WHERE id=$1`, sourceID); err != nil {
		return err
	}
	if markup == nil {
		if err = enqueueReply(ctx, tx, update, message); err != nil {
			return err
		}
	} else {
		if err = enqueueReplyMarkup(ctx, tx, update, message, markup); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func callbackText(data string) string {
	switch data {
	case "review:expense":
		return "pengeluaran"
	case "review:own":
		return "rekening sendiri"
	case "review:household":
		return "rekening household"
	case "review:asset":
		return "beli aset"
	case "review:investment":
		return "investasi"
	case "review:confirm":
		return "konfirmasi"
	case "review:change":
		return "ubah"
	case "review:remember":
		return "ingat merchant"
	case "review:once":
		return "tidak"
	}
	if strings.HasPrefix(data, "review:category:") {
		return strings.TrimPrefix(data, "review:category:")
	}
	return ""
}
