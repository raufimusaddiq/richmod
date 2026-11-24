package reviewdomain

import (
	"regexp"
	"strings"
	"time"
)

var emailNumberPattern = regexp.MustCompile(`[0-9]+(?:[.,][0-9]+)*`)

func EmailAmountSupports(amount string, texts ...string) bool {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return false
	}
	for _, text := range texts {
		for _, candidate := range emailNumberPattern.FindAllString(text, -1) {
			if normalizeIndonesianNumber(candidate) == amount {
				return true
			}
		}
	}
	return false
}

func normalizeIndonesianNumber(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, suffix := range []string{",00", ",0", ".00"} {
		if strings.HasSuffix(raw, suffix) {
			raw = strings.TrimSuffix(raw, suffix)
			break
		}
	}
	var out strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func EmailTimePlausible(at, receivedAt time.Time) bool {
	return !at.After(receivedAt.Add(10 * time.Minute))
}
