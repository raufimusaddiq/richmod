package telegram

import (
	"errors"
	"fmt"
)

type agentRecoverable struct {
	Status   string
	Reason   string
	NextStep string
	UserCopy string
}

func (e *agentRecoverable) Error() string { return e.Status + ": " + e.Reason }

func (e *agentRecoverable) toolOutput(tool string) map[string]any {
	return map[string]any{"status": e.Status, "tool": tool, "reason": e.Reason, "next_step": e.NextStep, "executed": false}
}

const maxRecoverableResults = 2

var (
	errSalaryCycleUnavailable   = errors.New("salary cycle unavailable")
	errPreviousCycleUnavailable = errors.New("previous salary cycle unavailable")
	errRangeOutOfBounds         = errors.New("range outside bounds")
	errInvalidRange             = errors.New("invalid range")
)

func recoverableRange(err error) (*agentRecoverable, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, errSalaryCycleUnavailable):
		return &agentRecoverable{Status: "CYCLE_UNAVAILABLE", Reason: "the household has no confirmed primary salary yet, so salary-cycle periods do not exist",
			NextStep: "use a calendar period (THIS_MONTH, LAST_MONTH or CUSTOM) and tell the user cycle figures start once a primary salary is confirmed",
			UserCopy: "Siklus gaji belum ada karena gaji utama belum dikonfirmasi. Coba tanyakan per bulan, misalnya “pengeluaran bulan ini”."}, true
	case errors.Is(err, errPreviousCycleUnavailable):
		return &agentRecoverable{Status: "PREVIOUS_CYCLE_UNAVAILABLE", Reason: "only the current salary cycle exists",
			NextStep: "use CURRENT_CYCLE or a calendar period and say there is no earlier cycle yet",
			UserCopy: "Baru ada satu siklus gaji, jadi belum ada siklus sebelumnya untuk dibandingkan."}, true
	case errors.Is(err, errRangeOutOfBounds):
		return &agentRecoverable{Status: "RANGE_OUT_OF_BOUNDS", Reason: "a period must end after it starts, span at most 366 days, and not start in the future",
			NextStep: "split the question into periods of at most a year, or ask the user which year they mean",
			UserCopy: "Rentang waktunya terlalu panjang. Aku bisa menghitung paling lama satu tahun sekaligus; sebutkan tahun atau bulannya, ya."}, true
	case errors.Is(err, errInvalidRange):
		return &agentRecoverable{Status: "INVALID_RANGE", Reason: "CUSTOM needs from_date and to_date as YYYY-MM-DD",
			NextStep: "call again with valid from_date and to_date, or a named period",
			UserCopy: "Tanggalnya belum terbaca. Sebutkan rentangnya, misalnya 1–15 Oktober."}, true
	}
	return nil, false
}

func recoverableDate(err error) *agentRecoverable {
	return &agentRecoverable{Status: "INVALID_DATE", Reason: fmt.Sprintf("Go could not read the date/time: %v", err),
		NextStep: "use date_reference TODAY/YESTERDAY/EXPLICIT with explicit_date YYYY-MM-DD and local_time HH:MM, or ask the user for the date",
		UserCopy: "Tanggal atau jamnya belum terbaca, jadi belum ada yang diubah. Tulis tanggalnya, misalnya 3 Okt jam 15.00."}
}
