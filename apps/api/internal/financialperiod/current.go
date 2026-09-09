package financialperiod

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type CurrentPeriod struct {
	Kind       string
	Start      time.Time
	End        time.Time
	Configured bool
}

func Current(ctx context.Context, db queryer, household string, now time.Time) (CurrentPeriod, error) {
	local := now.In(clock.HouseholdLocation())
	calendarStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, clock.HouseholdLocation())
	period := CurrentPeriod{Kind: "CALENDAR_MONTH", Start: calendarStart, End: calendarStart.AddDate(0, 1, 0)}
	var configured bool
	var startDate, endDate *string
	if err := db.QueryRow(ctx, `SELECT configured,starts_on::text,ends_on::text FROM salary_cycle_bounds($1,$2::date)`, household, local.Format("2006-01-02")).Scan(&configured, &startDate, &endDate); err != nil {
		return CurrentPeriod{}, err
	}
	if !configured || startDate == nil {
		return period, nil
	}
	start, err := time.ParseInLocation("2006-01-02", *startDate, clock.HouseholdLocation())
	if err != nil {
		return CurrentPeriod{}, err
	}
	period.Kind, period.Start, period.Configured = "CURRENT_CYCLE", start, true
	if endDate != nil {
		end, err := time.ParseInLocation("2006-01-02", *endDate, clock.HouseholdLocation())
		if err != nil {
			return CurrentPeriod{}, err
		}
		period.End = end
	} else {
		period.End = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, clock.HouseholdLocation()).AddDate(0, 0, 1)
	}
	return period, nil
}
