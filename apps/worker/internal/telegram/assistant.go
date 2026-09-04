package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type assistantRange struct{ From, To time.Time }

func (r assistantRange) label() string {
	from := r.From.In(jakartaLocation()).Format("02 Jan 2006")
	to := r.To.In(jakartaLocation()).AddDate(0, 0, -1).Format("02 Jan 2006")
	if from == to {
		return from
	}
	return from + "–" + to
}

func (p *Processor) processAssistantIntent(ctx context.Context, sourceID, householdID string, update telegramUpdate, value extraction, now time.Time) error {
	if value.Ambiguous {
		return p.finishAssistant(ctx, sourceID, update, "Permintaannya belum cukup jelas. Sebutkan periode atau transaksi yang dimaksud.", nil)
	}
	var rangeValue assistantRange
	var err error
	if pointerValue(value.Period) == "CURRENT_CYCLE" || pointerValue(value.Period) == "PREVIOUS_CYCLE" {
		rangeValue, err = p.resolveSalaryCycleRange(ctx, householdID, now, pointerValue(value.Period) == "PREVIOUS_CYCLE")
	} else {
		rangeValue, err = resolveAssistantRange(now, value.Period, value.FromDate, value.ToDate)
	}
	if err != nil {
		return p.finishAssistant(ctx, sourceID, update, "Periodenya belum jelas. Contoh: minggu ini, bulan lalu, atau 1–15 Agustus 2026.", nil)
	}
	switch value.Intent {
	case "GET_SPENDING":
		return p.replySpending(ctx, sourceID, householdID, update, rangeValue)
	case "GET_CASHFLOW":
		return p.replyCashflow(ctx, sourceID, householdID, update, rangeValue)
	case "GET_INSIGHTS":
		return p.replyCycleInsight(ctx, sourceID, householdID, update, rangeValue)
	case "SEARCH_TRANSACTIONS":
		return p.replySearch(ctx, sourceID, householdID, update, rangeValue, clean(pointerValue(value.SearchText), 120))
	case "CORRECT_TRANSACTION":
		return p.correctTransaction(ctx, sourceID, householdID, update, rangeValue, value)
	case "GET_REVIEW_ITEMS":
		return p.replyReviews(ctx, sourceID, householdID, update)
	case "UPLOAD_FINANCIAL_DOCUMENT":
		return p.finishAssistant(ctx, sourceID, update, "Kirim foto atau dokumen ke chat ini. Richmod akan memprosesnya lewat pipeline dokumen dan meminta review jika buktinya ambigu.", nil)
	default:
		return p.finishAssistant(ctx, sourceID, update, "Saya hanya membantu pencatatan, pencarian, koreksi, arus kas, dan review keuangan keluarga.", nil)
	}
}

func (p *Processor) replyCycleInsight(ctx context.Context, sourceID, householdID string, update telegramUpdate, r assistantRange) error {
	var income, expense, net, reviews, topName, topAmount string
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text,COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)::text,(COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)-COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0))::text,(SELECT count(*)::text FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at >= $2 AND transaction_at < $3`, householdID, r.From, r.To).Scan(&income, &expense, &net, &reviews)
	if err != nil {
		return err
	}
	_ = p.pool.QueryRow(ctx, `SELECT COALESCE(counterparty_name,description,'Tidak diketahui'),sum(amount)::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND type='EXPENSE' AND transaction_at >= $2 AND transaction_at < $3 GROUP BY 1 ORDER BY sum(amount) DESC LIMIT 1`, householdID, r.From, r.To).Scan(&topName, &topAmount)
	// Interactive insight never creates a second LLM contract. Canonical insight
	// generation remains background-only; Telegram always has this deterministic
	// aggregate fallback while a matching persisted insight is unavailable.
	message := "📊 Ringkasan keuangan\nPeriode: " + r.label() + "\n\nPemasukan: Rp" + FormatIDR(income) + "\nPengeluaran: Rp" + FormatIDR(expense) + "\nArus kas bersih: Rp" + FormatIDR(net) + "\nReview terbuka: " + reviews
	if topName != "" {
		message += "\nPengeluaran terbesar: " + topName + " (Rp" + FormatIDR(topAmount) + ")"
	}
	return p.finishAssistant(ctx, sourceID, update, message, nil)
}

func (p *Processor) resolveSalaryCycleRange(ctx context.Context, householdID string, now time.Time, previous bool) (assistantRange, error) {
	var current, next, prior *time.Time
	err := p.pool.QueryRow(ctx, `WITH anchors AS (SELECT se.pay_date FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id WHERE se.household_id=$1 AND ss.active AND ss.is_primary AND se.status='CONFIRMED') SELECT (SELECT max(pay_date) FROM anchors WHERE pay_date <= $2::date),(SELECT min(pay_date) FROM anchors WHERE pay_date > $2::date),(SELECT max(pay_date) FROM anchors WHERE pay_date < (SELECT max(pay_date) FROM anchors WHERE pay_date <= $2::date))`, householdID, now.In(jakartaLocation()).Format("2006-01-02")).Scan(&current, &next, &prior)
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

func (p *Processor) replyCashflow(ctx context.Context, sourceID, householdID string, update telegramUpdate, r assistantRange) error {
	var income, expense, net string
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text,COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)::text,(COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)-COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0))::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at >= $2 AND transaction_at < $3`, householdID, r.From, r.To).Scan(&income, &expense, &net)
	if err != nil {
		return err
	}
	return p.finishAssistant(ctx, sourceID, update, "💰 Arus kas\nPeriode: "+r.label()+"\n\nPemasukan: Rp"+FormatIDR(income)+"\nPengeluaran: Rp"+FormatIDR(expense)+"\nArus kas bersih: Rp"+FormatIDR(net), nil)
}

func (p *Processor) replySearch(ctx context.Context, sourceID, householdID string, update telegramUpdate, r assistantRange, query string) error {
	if query == "" {
		return p.finishAssistant(ctx, sourceID, update, "Transaksi apa yang ingin dicari? Sebutkan merchant atau keterangannya.", nil)
	}
	rows, err := p.pool.Query(ctx, `SELECT t.id,t.transaction_at,t.type,t.amount::text,COALESCE(t.counterparty_name,t.description,c.name,'Transaksi') FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.household_id=$1 AND t.status<>'VOIDED' AND t.type IN('INCOME','EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3 AND (t.counterparty_name ILIKE '%'||$4||'%' OR t.description ILIKE '%'||$4||'%' OR c.name ILIKE '%'||$4||'%') ORDER BY t.transaction_at DESC LIMIT 6`, householdID, r.From, r.To, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	lines := []string{"🔎 Hasil pencarian\nKata kunci: “" + query + "”\nPeriode: " + r.label()}
	ids := make([]string, 0, 6)
	for rows.Next() {
		var id string
		var at time.Time
		var typ, amount, label string
		if err = rows.Scan(&id, &at, &typ, &amount, &label); err != nil {
			return err
		}
		ids = append(ids, id)
		lines = append(lines, fmt.Sprintf("%d. %s · %s · Rp%s · %s", len(ids), at.In(jakartaLocation()).Format("02 Jan"), typ, FormatIDR(amount), label))
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(lines) == 1 {
		lines = []string{"🔎 Tidak ada hasil\n\nCoba kata kunci atau periode lain."}
	}
	if err := p.finishAssistant(ctx, sourceID, update, strings.Join(lines, "\n"), nil); err != nil { return err }
	return p.persistTransactionReferences(ctx, householdID, sourceID, update, ids)
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

func (p *Processor) correctTransaction(ctx context.Context, sourceID, householdID string, update telegramUpdate, r assistantRange, value extraction) error {
	query := clean(pointerValue(value.SearchText), 120)
	categorySlug := clean(pointerValue(value.CorrectionCategorySlug), 120)
	description := clean(pointerValue(value.CorrectionDescription), 500)
	dateReference := pointerValue(value.CorrectionDateReference)
	explicitDate := pointerValue(value.CorrectionExplicitDate)
	localTime := pointerValue(value.CorrectionLocalTime)
	if query == "" || (categorySlug == "" && description == "" && dateReference == "") {
		return p.finishAssistant(ctx, sourceID, update, "Sebutkan satu transaksi dan koreksinya, misalnya: Pamella tadi ubah kategori menjadi belanja rumah, atau: yang tadi pindahkan ke kemarin sore.", nil)
	}
	var correctedAt *time.Time
	if dateReference != "" {
		at, err := resolveTime(p.now().In(jakartaLocation()), &dateReference, nullablePointer(explicitDate), nullablePointer(localTime))
		if err != nil {
			return p.finishAssistant(ctx, sourceID, update, "Tanggal atau waktu koreksinya belum jelas.", nil)
		}
		correctedAt = &at
	}
	rows, err := p.pool.Query(ctx, `SELECT t.id,COALESCE(t.counterparty_name,t.description,'Transaksi'),t.amount::text FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.household_id=$1 AND t.status<>'VOIDED' AND t.type IN('INCOME','EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3 AND (t.counterparty_name ILIKE '%'||$4||'%' OR t.description ILIKE '%'||$4||'%' OR c.name ILIKE '%'||$4||'%') ORDER BY t.transaction_at DESC LIMIT 3`, householdID, r.From, r.To, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	type candidate struct{ id, label, amount string }
	var found []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.label, &c.amount); err != nil {
			return err
		}
		found = append(found, c)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(found) != 1 {
		return p.finishAssistant(ctx, sourceID, update, fmt.Sprintf("Saya menemukan %d transaksi yang cocok. Tambahkan merchant, nominal, atau tanggal agar hanya satu transaksi yang terpilih.", len(found)), nil)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return err
	}
	var categoryID *string
	if categorySlug != "" {
		var id string
		if err = tx.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, householdID, categorySlug).Scan(&id); err != nil {
			return p.finishAssistant(ctx, sourceID, update, "Kategori koreksi tidak tersedia. Gunakan nama kategori aktif yang lebih jelas.", nil)
		}
		categoryID = &id
	}
	if _, err = tx.Exec(ctx, `UPDATE transaction SET category_id=COALESCE($2,category_id),description=COALESCE(NULLIF($3,''),description),transaction_at=COALESCE($4,transaction_at),updated_at=now() WHERE id=$1 AND household_id=$5`, found[0].id, categoryID, description, correctedAt, householdID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'CORRECT_TRANSACTION','transaction',$3,jsonb_build_object('category_id',$4::uuid,'description',NULLIF($5,''),'source_event_id',$6::uuid))`, householdID, userID, found[0].id, categoryID, description, sourceID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-assistant',parser_version='1' WHERE id=$1`, sourceID); err != nil {
		return err
	}
	if err = enqueueReply(ctx, tx, update, "✅ Transaksi diperbarui\n\n"+found[0].label+" · Rp"+FormatIDR(found[0].amount)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func nullablePointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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
