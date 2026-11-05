package telegram

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Deterministic parsing of an explicit date typed as a review reply. A reply
// that states the date plainly ("25 Okt", "25/10", "kemarin", "2026-10-25")
// is read here in Go; only a reply this parser cannot read goes to the model.
// The household timezone is Asia/Jakarta.

var indonesianMonthNames = map[string]time.Month{
	"jan": time.January, "januari": time.January, "january": time.January,
	"feb": time.February, "februari": time.February, "february": time.February, "peb": time.February, "pebruari": time.February,
	"mar": time.March, "maret": time.March, "march": time.March,
	"apr": time.April, "april": time.April,
	"mei": time.May, "may": time.May,
	"jun": time.June, "juni": time.June, "june": time.June,
	"jul": time.July, "juli": time.July, "july": time.July,
	"agu": time.August, "agt": time.August, "ags": time.August, "agus": time.August, "agustus": time.August, "aug": time.August, "august": time.August,
	"sep": time.September, "sept": time.September, "september": time.September,
	"okt": time.October, "oktober": time.October, "oct": time.October, "october": time.October,
	"nov": time.November, "nopember": time.November, "november": time.November,
	"des": time.December, "desember": time.December, "dec": time.December, "december": time.December,
}

var (
	isoDatePattern     = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})$`)
	numericDatePattern = regexp.MustCompile(`^(\d{1,2})[/.-](\d{1,2})(?:[/.-](\d{2}|\d{4}))?$`)
	dateReplyPrefix    = regexp.MustCompile(`^(?:tanggal|tgl\.?)\s*`)
	namedDatePattern   = regexp.MustCompile(`^(\d{1,2})\s+([a-z]+)\.?(?:\s+(\d{4}))?$`)
)

// parseReplyDate reads one explicit calendar date from a short reply. It
// returns false for anything that is not a single unambiguous date, so a
// sentence is never half-read. A date without a year takes the current year,
// or the previous year when that would put it more than 31 days in the future.
func parseReplyDate(text string, now time.Time) (time.Time, bool) {
	location := jakartaLocation()
	now = now.In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	value := strings.ToLower(strings.Join(strings.Fields(text), " "))
	value = dateReplyPrefix.ReplaceAllString(value, "")
	value = strings.Trim(value, " .,!?")
	switch value {
	case "hari ini", "today":
		return today, true
	case "kemarin", "kmrn", "yesterday":
		return today.AddDate(0, 0, -1), true
	case "kemarin lusa", "lusa kemarin":
		return today.AddDate(0, 0, -2), true
	}
	var day, month, year int
	var yearKnown bool
	switch {
	case isoDatePattern.MatchString(value):
		parts := isoDatePattern.FindStringSubmatch(value)
		year, _ = strconv.Atoi(parts[1])
		month, _ = strconv.Atoi(parts[2])
		day, _ = strconv.Atoi(parts[3])
		yearKnown = true
	case numericDatePattern.MatchString(value):
		// Indonesian order is day/month[/year].
		parts := numericDatePattern.FindStringSubmatch(value)
		day, _ = strconv.Atoi(parts[1])
		month, _ = strconv.Atoi(parts[2])
		if parts[3] != "" {
			year, _ = strconv.Atoi(parts[3])
			if year < 100 {
				year += 2000
			}
			yearKnown = true
		}
	case namedDatePattern.MatchString(value):
		parts := namedDatePattern.FindStringSubmatch(value)
		day, _ = strconv.Atoi(parts[1])
		named, ok := indonesianMonthNames[parts[2]]
		if !ok {
			return time.Time{}, false
		}
		month = int(named)
		if parts[3] != "" {
			year, _ = strconv.Atoi(parts[3])
			yearKnown = true
		}
	default:
		return time.Time{}, false
	}
	if !yearKnown {
		year = today.Year()
	}
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, location)
	// time.Date normalizes 31/02 into March; such a reply is not a real date.
	if month < 1 || month > 12 || date.Day() != day || int(date.Month()) != month {
		return time.Time{}, false
	}
	if !yearKnown && date.After(today.AddDate(0, 0, 31)) {
		date = date.AddDate(-1, 0, 0)
		if date.Day() != day {
			return time.Time{}, false
		}
	}
	return date, true
}
