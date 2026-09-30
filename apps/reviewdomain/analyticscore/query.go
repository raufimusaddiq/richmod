package analyticscore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/clock"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/financialmath"
)

func reviewPeriods(ctx context.Context, tx pgx.Tx, household string, now time.Time) ([]reviewPeriod, bool, error) {
	var configured bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM salary_source WHERE household_id=$1 AND active AND is_primary)`, household).Scan(&configured); err != nil {
		return nil, false, err
	}
	rows, err := tx.Query(ctx, `WITH anchors AS (
		SELECT DISTINCT se.pay_date FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id AND ss.household_id=se.household_id
		WHERE se.household_id=$1 AND ss.active AND ss.is_primary AND se.status='CONFIRMED' AND se.pay_date<=$2::date
	), cycles AS (SELECT pay_date,lead(pay_date) OVER(ORDER BY pay_date) AS end_date FROM anchors)
	SELECT pay_date::text,end_date::text FROM cycles ORDER BY pay_date DESC`, household, now.Format("2006-01-02"))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []reviewPeriod{}
	for rows.Next() {
		p := reviewPeriod{Kind: "SALARY_CYCLE", State: "ACTIVE", Configured: configured, MeasuredUntil: now.AddDate(0, 0, 1).Format("2006-01-02")}
		if err := rows.Scan(&p.Start, &p.End); err != nil {
			return nil, false, err
		}
		if p.End != nil {
			p.State = "CLOSED"
			p.MeasuredUntil = *p.End
		}
		out = append(out, p)
	}
	return out, configured, rows.Err()
}

func loadReviewMeasures(ctx context.Context, tx pgx.Tx, household string, measures []cycleMeasure, facts *Facts) error {
	starts, ends := []string{}, []string{}
	for _, m := range measures {
		starts = append(starts, m.period.Start)
		ends = append(ends, m.period.MeasuredUntil)
	}
	rows, err := tx.Query(ctx, `WITH periods AS (
		SELECT ordinality-1 AS idx,s::date AS start_date,e::date AS end_date FROM unnest($2::text[],$3::text[]) WITH ORDINALITY AS p(s,e,ordinality)
	)
	SELECT p.idx,to_char(t.transaction_at AT TIME ZONE 'Asia/Jakarta','YYYY-MM-DD'),t.type,t.purpose,
		COALESCE(c.id::text,''),COALESCE(c.name,'Belum dikategorikan'),COALESCE(m.id::text,''),COALESCE(m.normalized_name,NULLIF(t.counterparty_name,''),'Merchant tidak diketahui'),
		COALESCE(u.id::text,''),COALESCE(u.display_name,'Bersama / otomatis / belum diatribusikan'),COALESCE(a.id::text,''),COALESCE(a.name,'Belum ditautkan'),sum(t.amount)::text,count(*)::int
	FROM periods p JOIN transaction t ON t.household_id=$1 AND t.status='CONFIRMED'
		AND t.transaction_at >= (p.start_date::timestamp AT TIME ZONE 'Asia/Jakarta') AND t.transaction_at < (p.end_date::timestamp AT TIME ZONE 'Asia/Jakarta')
	LEFT JOIN category c ON c.id=t.category_id AND c.household_id=t.household_id
	LEFT JOIN merchant m ON m.id=t.merchant_id AND m.household_id=t.household_id
	LEFT JOIN household_member hm ON hm.user_id=t.created_by_user_id AND hm.household_id=t.household_id
	LEFT JOIN "user" u ON u.id=hm.user_id
	LEFT JOIN wealth_account a ON a.id=t.related_wealth_account_id AND a.household_id=t.household_id
	WHERE t.type IN ('INCOME','EXPENSE','REFUND','TRANSFER')
	GROUP BY p.idx,2,t.type,t.purpose,c.id,c.name,m.id,8,u.id,u.display_name,a.id,a.name
	ORDER BY p.idx,2`, household, starts, ends)
	if err != nil {
		return err
	}
	defer rows.Close()
	daily := map[string]map[string]string{}
	members, destinations := map[string]reviewValue{}, map[string]reviewValue{}
	for rows.Next() {
		var idx, count int
		var day, typ, purpose, cid, cname, mid, mname, uid, uname, aid, aname, amount string
		if err := rows.Scan(&idx, &day, &typ, &purpose, &cid, &cname, &mid, &mname, &uid, &uname, &aid, &aname, &amount, &count); err != nil {
			return err
		}
		m := &measures[idx]
		switch typ {
		case "INCOME":
			m.cash.Income = financialmath.Add(m.cash.Income, amount)
		case "EXPENSE":
			m.cash.GrossExpense = financialmath.Add(m.cash.GrossExpense, amount)
		case "REFUND":
			m.cash.Refund = financialmath.Add(m.cash.Refund, amount)
		case "TRANSFER":
			if purpose == "SAVINGS_TRANSFER" || purpose == "INVESTMENT_CONTRIBUTION" || purpose == "ASSET_PURCHASE" {
				m.cash.Allocated = financialmath.Add(m.cash.Allocated, amount)
				if idx == 0 {
					accumulate(destinations, aid, aid, aname, amount, count)
				}
			}
		}
		if idx == 0 && typ != "TRANSFER" {
			if daily[day] == nil {
				daily[day] = map[string]string{"period": day, "income": "0", "grossExpense": "0", "refund": "0"}
			}
			key := "income"
			if typ == "EXPENSE" {
				key = "grossExpense"
			}
			if typ == "REFUND" {
				key = "refund"
			}
			daily[day][key] = financialmath.Add(daily[day][key], amount)
		}
		if typ == "EXPENSE" || typ == "REFUND" {
			if typ == "REFUND" {
				amount = financialmath.Subtract("0", amount)
			}
			accumulate(m.categories, cid, cid, cname, amount, count)
			merchantKey := mid + ":" + mname
			accumulate(m.merchants, merchantKey, mid, mname, amount, count)
			if m.categoryMerchants[cid] == nil {
				m.categoryMerchants[cid] = map[string]reviewValue{}
			}
			accumulate(m.categoryMerchants[cid], merchantKey, mid, mname, amount, count)
			if idx == 0 {
				accumulate(members, uid, uid, uname, amount, count)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	facts.Members = orderedValues(members)
	facts.Destinations = orderedValues(destinations)
	start, _ := time.ParseInLocation("2006-01-02", facts.Period.Start, clock.HouseholdLocation())
	end, _ := time.ParseInLocation("2006-01-02", facts.Period.MeasuredUntil, clock.HouseholdLocation())
	cumulative, total := "0", "0"
	facts.SpendingShape.PeakExpense = "0"
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		day := d.Format("2006-01-02")
		v := daily[day]
		if v == nil {
			v = map[string]string{"period": day, "income": "0", "grossExpense": "0", "refund": "0"}
		}
		cash, err := financialmath.CalculateCashflow(v["income"], v["grossExpense"], v["refund"])
		if err != nil {
			return err
		}
		v["expense"], v["netCashflow"] = cash.NetExpense, cash.Surplus
		cumulative = financialmath.Add(cumulative, cash.NetExpense)
		v["cumulativeExpense"] = cumulative
		total = financialmath.Add(total, cash.NetExpense)
		if cash.NetExpense == "0" {
			facts.SpendingShape.ZeroDays++
		}
		if compareAmounts(cash.NetExpense, facts.SpendingShape.PeakExpense) > 0 {
			facts.SpendingShape.PeakExpense = cash.NetExpense
			facts.SpendingShape.PeakDay = valuePointer(day)
		}
		facts.Daily = append(facts.Daily, v)
	}
	facts.SpendingShape.Days = len(facts.Daily)
	facts.SpendingShape.Average = amountPerDay(total, len(facts.Daily))
	facts.SpendingShape.PeakShare = positiveRatio(facts.SpendingShape.PeakExpense, total)
	return nil
}

func accumulate(values map[string]reviewValue, key, id, name, amount string, count int) {
	v, ok := values[key]
	if !ok {
		v = reviewValue{ID: id, Name: name, Amount: "0"}
	}
	v.Amount = financialmath.Add(v.Amount, amount)
	v.Count += count
	values[key] = v
}

func loadReviewTransactions(ctx context.Context, tx pgx.Tx, household string, facts *Facts) error {
	rows, err := tx.Query(ctx, `WITH ranked AS (
		SELECT t.id,t.transaction_at,t.type,t.amount,t.category_id,COALESCE(m.normalized_name,NULLIF(t.counterparty_name,''),'Merchant tidak diketahui') merchant,
		row_number() OVER(PARTITION BY t.category_id ORDER BY t.amount DESC,t.transaction_at DESC,t.id) AS rank
		FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id AND m.household_id=t.household_id
		WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type IN ('EXPENSE','REFUND')
		AND t.transaction_at>=($2::date::timestamp AT TIME ZONE 'Asia/Jakarta') AND t.transaction_at<($3::date::timestamp AT TIME ZONE 'Asia/Jakarta')
	) SELECT id::text,transaction_at,type,amount::text,COALESCE(category_id::text,''),merchant FROM ranked WHERE rank<=10 ORDER BY category_id,rank`, household, facts.Period.Start, facts.Period.MeasuredUntil)
	if err != nil {
		return err
	}
	defer rows.Close()
	indices := map[string]int{}
	for i, c := range facts.Categories {
		indices[c.ID] = i
	}
	for rows.Next() {
		var v reviewTransaction
		var cid string
		if err := rows.Scan(&v.ID, &v.At, &v.Type, &v.Amount, &cid, &v.Merchant); err != nil {
			return err
		}
		if i, ok := indices[cid]; ok {
			facts.Categories[i].Transactions = append(facts.Categories[i].Transactions, v)
		}
	}
	return rows.Err()
}

func loadReviewQuality(ctx context.Context, tx pgx.Tx, household string, facts *Facts) error {
	var open, uncategorized, processing int
	var amount string
	err := tx.QueryRow(ctx, `WITH bounds AS (SELECT ($2::date::timestamp AT TIME ZONE 'Asia/Jakarta') AS lo,($3::date::timestamp AT TIME ZONE 'Asia/Jakarta') AS hi), unresolved AS (
		SELECT 't:'||t.id::text AS ref FROM transaction t,bounds b WHERE t.household_id=$1 AND t.status IN ('NEEDS_REVIEW','PENDING') AND t.transaction_at>=b.lo AND t.transaction_at<b.hi
		UNION SELECT CASE WHEN ri.transaction_id IS NOT NULL THEN 't:'||ri.transaction_id::text ELSE 'r:'||ri.id::text END FROM review_item ri
		LEFT JOIN transaction t ON t.id=ri.transaction_id AND t.household_id=ri.household_id
		LEFT JOIN transaction_proposal p ON p.id=ri.proposal_id AND p.household_id=ri.household_id
		LEFT JOIN source_event s ON s.id=ri.source_event_id AND s.household_id=ri.household_id
		LEFT JOIN cycle_residual_case c ON c.id=ri.cycle_residual_case_id AND c.household_id=ri.household_id,bounds b
		WHERE ri.household_id=$1 AND ri.status IN ('OPEN','PENDING_SEND') AND
		COALESCE(t.transaction_at,p.transaction_at,(c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta'),s.received_at,ri.created_at)>=b.lo AND
		COALESCE(t.transaction_at,p.transaction_at,(c.cycle_start::timestamp AT TIME ZONE 'Asia/Jakarta'),s.received_at,ri.created_at)<b.hi
	)
	SELECT (SELECT count(*)::int FROM unresolved),
		count(*) FILTER(WHERE t.type='EXPENSE' AND t.category_id IS NULL)::int,
		COALESCE(sum(t.amount) FILTER(WHERE t.type='EXPENSE' AND t.category_id IS NULL),0)::text,
		(SELECT count(*)::int FROM source_event s,bounds b WHERE s.household_id=$1 AND s.processing_status IN ('RECEIVED','PROCESSING','FAILED') AND s.received_at>=b.lo AND s.received_at<b.hi)
	FROM transaction t,bounds b WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.transaction_at>=b.lo AND t.transaction_at<b.hi`, household, facts.Period.Start, facts.Period.MeasuredUntil).Scan(&open, &uncategorized, &amount, &processing)
	if err != nil {
		return err
	}
	facts.block("OPEN_REVIEWS", open, nil, "/inbox")
	facts.block("UNCATEGORIZED_EXPENSE", uncategorized, &amount, "/transactions")
	facts.block("PROCESSING_INCOMPLETE", processing, nil, "/inbox")
	return nil
}

func loadReviewWealth(ctx context.Context, tx pgx.Tx, household string, now time.Time, facts *Facts) error {
	// Snapshot reconciliation is for the actual observation interval, not an
	// invented cycle-end balance. Future observations never enter a past review.
	end, _ := time.ParseInLocation("2006-01-02", facts.Period.MeasuredUntil, clock.HouseholdLocation())
	if now.Before(end) {
		end = now
	}
	rows, err := tx.Query(ctx, `WITH selected AS (
		(SELECT 'current' AS kind,id,observed_at FROM wealth_snapshot WHERE household_id=$1 AND observed_at<$3 ORDER BY observed_at DESC,id LIMIT 1)
		UNION ALL (SELECT 'previous' AS kind,id,observed_at FROM wealth_snapshot WHERE household_id=$1 AND observed_at<($2::date::timestamp AT TIME ZONE 'Asia/Jakarta') ORDER BY observed_at DESC,id LIMIT 1)
	) SELECT s.kind,s.id::text,s.observed_at,COALESCE(sum(CASE WHEN a.side='ASSET' THEN i.value_idr ELSE -i.value_idr END),0)::text,
		COALESCE(array_agg(a.id::text ORDER BY a.id) FILTER(WHERE a.id IS NOT NULL),ARRAY[]::text[])
	FROM selected s LEFT JOIN wealth_snapshot_item i ON i.snapshot_id=s.id LEFT JOIN wealth_account a ON a.id=i.wealth_account_id AND a.household_id=$1
	GROUP BY s.kind,s.id,s.observed_at`, household, facts.Period.Start, end)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		v := &reviewSnapshot{}
		var kind string
		if err := rows.Scan(&kind, &v.ID, &v.At, &v.NetWorth, &v.accounts); err != nil {
			return err
		}
		v.AgeDays = daysBetween(v.At.In(clock.HouseholdLocation()).Format("2006-01-02"), end.In(clock.HouseholdLocation()).Format("2006-01-02"))
		if kind == "current" {
			facts.Wealth.Current = v
		} else {
			facts.Wealth.Previous = v
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if facts.Wealth.Current == nil {
		facts.block("MISSING_WEALTH_SNAPSHOT", 1, nil, "/wealth")
		return nil
	}
	if facts.Wealth.Current.At.In(clock.HouseholdLocation()).Format("2006-01-02") < facts.Period.Start {
		facts.block("WEALTH_SNAPSHOT_BEFORE_CYCLE", 1, nil, "/wealth")
	}
	if facts.Wealth.Previous == nil {
		facts.block("MISSING_PREVIOUS_WEALTH_SNAPSHOT", 1, nil, "/wealth")
		return nil
	}
	current, previous := facts.Wealth.Current, facts.Wealth.Previous
	if !sameAccounts(current.accounts, previous.accounts) {
		facts.block("WEALTH_ACCOUNT_SET_CHANGED", 1, nil, "/wealth")
		return nil
	}
	if current.ID == previous.ID {
		facts.block("WEALTH_SNAPSHOT_UNCHANGED", 1, nil, "/wealth")
		return nil
	}
	var income, expense, refund string
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='EXPENSE'),0)::text,COALESCE(sum(amount) FILTER(WHERE type='REFUND'),0)::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at>$2 AND transaction_at<=$3`, household, previous.At, current.At).Scan(&income, &expense, &refund)
	if err != nil {
		return err
	}
	cash, err := financialmath.CalculateCashflow(income, expense, refund)
	if err != nil {
		return err
	}
	facts.Wealth.Change = valuePointer(financialmath.Subtract(current.NetWorth, previous.NetWorth))
	facts.Wealth.Cashflow = &cash.Surplus
	facts.Wealth.Other = valuePointer(financialmath.Subtract(*facts.Wealth.Change, cash.Surplus))
	return nil
}

func sameAccounts(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
