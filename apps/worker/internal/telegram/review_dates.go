package telegram

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var monthNames = map[string]time.Month{
	"jan": 1, "januari": 1, "january": 1, "feb": 2, "februari": 2, "february": 2, "peb": 2, "pebruari": 2,
	"mar": 3, "maret": 3, "march": 3, "apr": 4, "april": 4, "mei": 5, "may": 5, "jun": 6, "juni": 6, "june": 6,
	"jul": 7, "juli": 7, "july": 7, "agu": 8, "agt": 8, "ags": 8, "agus": 8, "agustus": 8, "aug": 8, "august": 8,
	"sep": 9, "sept": 9, "september": 9, "okt": 10, "oktober": 10, "oct": 10, "october": 10,
	"nov": 11, "november": 11, "nop": 11, "nopember": 11, "des": 12, "desember": 12, "dec": 12, "december": 12,
}

var indonesianDays = map[string]int{
	"hari ini": 0, "hariini": 0, "today": 0,
	"kemarin": -1, "kemaren": -1, "kmrn": -1, "yesterday": -1,
	"kemarin lusa": -2, "lusa kemarin": -2,
}

var (
	isoDatePattern     = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})$`)
	numericDatePattern = regexp.MustCompile(`^(\d{1,2})[/.-](\d{1,2})(?:[/.-](\d{2}|\d{4}))?$`)
	dateReplyPrefix    = regexp.MustCompile(`^(?:tanggal|tgl\.?)\s*`)
	namedDatePattern   = regexp.MustCompile(`^(\d{1,2})\s+([a-z]+)\.?(?:\s+(\d{4}))?$`)
)

func parseDateValue(text string, now time.Time, futureDays int) (time.Time, bool) {
	location := jakartaLocation()
	local := now.In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	value := strings.ToLower(strings.Join(strings.Fields(text), " "))
	value = dateReplyPrefix.ReplaceAllString(value, "")
	value = strings.Trim(value, " .,!?")
	if date, ok := parseDateToken(value, today, futureDays); ok {
		return date, true
	}
	if stripped := stripAnswerFiller(text); stripped != "" && stripped != value {
		return parseDateToken(stripped, today, futureDays)
	}
	return time.Time{}, false
}

func parseDateToken(value string, today time.Time, futureDays int) (time.Time, bool) {
	if offset, ok := indonesianDays[value]; ok {
		return today.AddDate(0, 0, offset), true
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
		named, ok := monthNames[parts[2]]
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
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, today.Location())
	if month < 1 || month > 12 || date.Day() != day || int(date.Month()) != month {
		return time.Time{}, false
	}
	if !yearKnown && date.After(today.AddDate(0, 0, futureDays)) {
		date = date.AddDate(-1, 0, 0)
		if date.Day() != day || int(date.Month()) != month {
			return time.Time{}, false
		}
	}
	return date, true
}
