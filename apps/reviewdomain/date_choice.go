package reviewdomain

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// DateChoice is one bounded one-tap answer for a transaction whose source never
// printed a date. Key is what Go maps back to a date; Label is what the user
// reads; Date is the resolved Jakarta date (YYYY-MM-DD) for display.
type DateChoice struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Date  string `json:"date"`
}

// Date choice keys (SET_TRANSACTION_DATE).
const (
	DateChoiceToday       = "TODAY"
	DateChoiceYesterday   = "YESTERDAY"
	DateChoiceMessageDate = "MESSAGE_DATE"
)

// ErrDateChoiceInvalid reports an unknown date choice key.
var ErrDateChoiceInvalid = errors.New("reviewdomain: unknown transaction date choice")

func jakartaLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.FixedZone("WIB", 7*60*60)
	}
	return location
}

func jakartaDay(at time.Time) time.Time {
	local := at.In(jakartaLocation())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, jakartaLocation())
}

// TransactionDateChoices returns the bounded date answers: today, yesterday and,
// when the recorded time is only the message's received time, the message date.
// A date that equals an earlier choice is not repeated.
func TransactionDateChoices(messageAt time.Time, messageIsFallback bool, now time.Time) []DateChoice {
	today := jakartaDay(now)
	choices := []DateChoice{
		{Key: DateChoiceToday, Label: "Hari ini", Date: today.Format("2006-01-02")},
		{Key: DateChoiceYesterday, Label: "Kemarin", Date: today.AddDate(0, 0, -1).Format("2006-01-02")},
	}
	if messageIsFallback && !messageAt.IsZero() {
		message := jakartaDay(messageAt).Format("2006-01-02")
		if message != choices[0].Date && message != choices[1].Date {
			choices = append(choices, DateChoice{Key: DateChoiceMessageDate, Label: "Tanggal pesan", Date: message})
		}
	}
	return choices
}

// ResolveTransactionDateChoice maps a bounded key to a Jakarta date. The message
// date is the received time of the transaction's earliest active evidence, read
// by Go; it is never supplied by a client or a model.
func ResolveTransactionDateChoice(ctx context.Context, tx pgx.Tx, householdID, transactionID, key string, now time.Time) (time.Time, error) {
	today := jakartaDay(now)
	switch key {
	case DateChoiceToday:
		return today, nil
	case DateChoiceYesterday:
		return today.AddDate(0, 0, -1), nil
	case DateChoiceMessageDate:
		var received time.Time
		if err := tx.QueryRow(ctx, `SELECT s.received_at FROM transaction_evidence te JOIN source_event s ON s.id=te.source_event_id JOIN transaction t ON t.id=te.transaction_id WHERE t.id=$1 AND t.household_id=$2 AND s.household_id=$2 AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE' ORDER BY te.created_at LIMIT 1`, transactionID, householdID).Scan(&received); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return time.Time{}, ErrDateChoiceInvalid
			}
			return time.Time{}, err
		}
		return jakartaDay(received), nil
	}
	return time.Time{}, ErrDateChoiceInvalid
}
