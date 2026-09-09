package financialperiod

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type fakeQuery struct {
	configured bool
	start, end *string
}

func (f fakeQuery) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRow(f)
}

type fakeRow fakeQuery

func (f fakeRow) Scan(dest ...any) error {
	*dest[0].(*bool) = f.configured
	*dest[1].(**string) = f.start
	*dest[2].(**string) = f.end
	return nil
}

func text(value string) *string { return &value }

func TestCurrentPeriodUsesJakartaMidnightBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 6, 18, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	open, err := Current(context.Background(), fakeQuery{configured: true, start: text("2026-08-25")}, "household", now)
	if err != nil || open.Kind != "CURRENT_CYCLE" || open.Start.Format(time.RFC3339) != "2026-08-25T00:00:00+07:00" || open.End.Format(time.RFC3339) != "2026-09-07T00:00:00+07:00" {
		t.Fatalf("open=%+v err=%v", open, err)
	}
	calendar, err := Current(context.Background(), fakeQuery{}, "household", now)
	if err != nil || calendar.Kind != "CALENDAR_MONTH" || calendar.Start.Format(time.RFC3339) != "2026-09-01T00:00:00+07:00" || calendar.End.Format(time.RFC3339) != "2026-10-01T00:00:00+07:00" {
		t.Fatalf("calendar=%+v err=%v", calendar, err)
	}
}
