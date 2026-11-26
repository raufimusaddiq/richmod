package telegram

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

const (
	dateForwardDays     = 31
	payDateMaxAgeDays   = reviewdomain.MaxCorrectionAgeDays
	dateHint            = "Pilih di bawah, atau ketik tanggalnya."
	dateOtherHint       = "Ketik tanggalnya, misalnya 3 Okt atau 25/9."
	futureDateAck       = "Tanggal itu belum terjadi."
	staleDateAck        = "Tanggal itu terlalu lama untuk slip ini."
	staleDateGenericAck = "Tanggal itu terlalu lama."
	choiceStaleAck      = "Pilihan itu tidak berlaku untuk kartu ini."
	transactionDateAsk  = "Transaksi ini terjadi tanggal berapa?"
)

func dateStepFrom(base Step, ask string, actions []string, extra ...InlineKeyboardButton) Step {
	base.Kind, base.Fact, base.State, base.Back = stepFact, factDate, "AWAITING_DATE", false
	base.Ask, base.Hint = ask, dateHint
	buttons := []InlineKeyboardButton{{Text: "Hari ini", CallbackData: callbackDateToday}, {Text: "Kemarin", CallbackData: callbackDateYesterday}}
	buttons = append(buttons, extra...)
	buttons = append(buttons, InlineKeyboardButton{Text: "Tanggal lain", CallbackData: callbackDateOther})
	if contains(actions, "IGNORE") {
		buttons = append(buttons, InlineKeyboardButton{Text: "Abaikan", CallbackData: callbackIgnore})
	}
	base.Rows = pairRows(buttons)
	return base
}

func jakartaMidnight(at time.Time) time.Time {
	local := at.In(jakartaLocation())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, jakartaLocation())
}

func exactMomentReview(review engineReview) bool {
	return review.ReviewType == bankReviewType
}

func (p *Processor) applyDateTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	if !stepOffers(step, data) {
		return reask(step, choiceStaleAck), nil
	}
	now := p.now().In(jakartaLocation())
	moment := func(offset int) time.Time {
		if exactMomentReview(review) {
			return now.AddDate(0, 0, offset)
		}
		return jakartaMidnight(now).AddDate(0, 0, offset)
	}
	switch data {
	case callbackDateToday:
		return p.finishDate(ctx, tx, householdID, sourceEventID, review, step, moment(0))
	case callbackDateYesterday:
		return p.finishDate(ctx, tx, householdID, sourceEventID, review, step, moment(-1))
	case callbackDateEmail:
		if review.Subject.Bank == nil || review.Subject.Bank.EmailAt.IsZero() {
			return reask(step, choiceStaleAck), nil
		}
		return p.finishDate(ctx, tx, householdID, sourceEventID, review, step, review.Subject.Bank.EmailAt)
	case callbackDateOther:
		step.Hint = dateOtherHint
		return stepOutcome{Step: step, Changed: true}, nil
	}
	return reask(step, choiceStaleAck), nil
}

func splitClock(text string) (hour, minute int, rest string, found bool) {
	fields := strings.Fields(text)
	kept := fields[:0:0]
	for _, field := range fields {
		token := strings.ToLower(strings.Trim(field, ","))
		if !found && clockToken.MatchString(token) {
			parts := strings.FieldsFunc(token, func(r rune) bool { return r == ':' || r == '.' })
			hour, _ = strconv.Atoi(parts[0])
			minute, _ = strconv.Atoi(parts[1])
			found = true
			continue
		}
		kept = append(kept, field)
	}
	return hour, minute, strings.Join(kept, " "), found
}

func (p *Processor) applyDateText(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, now time.Time, review engineReview, step Step, text string) (stepOutcome, error) {
	hour, minute, clockFree, hasClock := 0, 0, text, false
	if exactMomentReview(review) {
		hour, minute, clockFree, hasClock = splitClock(text)
	}
	date, ok := parseDateValue(clockFree, now, dateForwardDays)
	if !ok {
		return reask(step, unusableAnswerAck), nil
	}
	if date.After(jakartaMidnight(now)) {
		return stepOutcome{Step: step.WithAck(futureDateAck)}, nil
	}
	if date.Before(jakartaMidnight(now).AddDate(0, 0, -payDateMaxAgeDays)) {
		ack := staleDateGenericAck
		if payslipReviewType(review.ReviewType) {
			ack = staleDateAck
		}
		return stepOutcome{Step: step.WithAck(ack)}, nil
	}
	if hasClock {
		date = time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, jakartaLocation())
		if date.After(now) {
			return stepOutcome{Step: step.WithAck(futureDateAck)}, nil
		}
	}
	return p.finishDate(ctx, tx, householdID, sourceEventID, review, step, date)
}

func (p *Processor) finishDate(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, at time.Time) (stepOutcome, error) {
	switch {
	case payslipReviewType(review.ReviewType):
		return p.finishPayDate(ctx, tx, householdID, sourceEventID, review, step, at)
	case review.ReviewType == bankReviewType:
		return p.finishBankTime(ctx, tx, householdID, sourceEventID, review, step, at)
	}
	return p.finishTransactionDate(ctx, tx, householdID, sourceEventID, review, step, at)
}
