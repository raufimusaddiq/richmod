package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"
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
	if err != nil {
		return assistantRange{}, fmt.Errorf("load salary cycle anchors: %w", err)
	}
	if current == nil {
		return assistantRange{}, errSalaryCycleUnavailable
	}
	if previous {
		if prior == nil {
			return assistantRange{}, errPreviousCycleUnavailable
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
			return assistantRange{}, errInvalidRange
		}
		var err error
		from, err = time.ParseInLocation("2006-01-02", *fromDate, local.Location())
		if err != nil {
			return assistantRange{}, fmt.Errorf("%w: from_date: %v", errInvalidRange, err)
		}
		to, err = time.ParseInLocation("2006-01-02", *toDate, local.Location())
		if err != nil {
			return assistantRange{}, fmt.Errorf("%w: to_date: %v", errInvalidRange, err)
		}
		to = to.AddDate(0, 0, 1)
	default:
		return assistantRange{}, fmt.Errorf("%w: unknown period", errInvalidRange)
	}
	if !to.After(from) || to.Sub(from) > 366*24*time.Hour || from.After(local.AddDate(0, 0, 1)) {
		return assistantRange{}, errRangeOutOfBounds
	}
	return assistantRange{From: from, To: to}, nil
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
