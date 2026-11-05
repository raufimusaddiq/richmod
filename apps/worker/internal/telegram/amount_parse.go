package telegram

import (
	"math/big"
	"regexp"
	"strings"
)

// Amounts as people type them in Indonesian chat (25rb, 25 ribu, 5k, 50.000,
// Rp 25.000, 1,5jt, 25000), read deterministically for typed card answers.

const maxAnswerAmount = 100_000_000

var (
	amountToken     = regexp.MustCompile(`^(?:rp\.?)?(\d+(?:[.,]\d+)*)(rb|ribu|k|jt|juta)?$`)
	amountThousands = regexp.MustCompile(`^\d{1,3}([.,]\d{3})+$`)
	amountUnits     = map[string]string{"rb": "rb", "ribu": "rb", "k": "rb", "jt": "jt", "juta": "jt"}
)

// parseAmountToken turns one amount token into whole rupiah. Dots and commas
// are thousands separators unless a unit follows (1,5jt, 2.5rb). A bare number
// under 500 is refused: "25" most likely means 25 ribu, and Go never guesses.
func parseAmountToken(token, unit string) (string, bool) {
	match := amountToken.FindStringSubmatch(token)
	if match == nil {
		return "", false
	}
	digits := match[1]
	if match[2] != "" {
		if unit != "" {
			return "", false
		}
		unit = amountUnits[match[2]]
	}
	var value *big.Rat
	switch unit {
	case "":
		// Only well-formed thousands groups: 50.000, 1.250.000 or 25000.
		if strings.ContainsAny(digits, ".,") && !amountThousands.MatchString(digits) {
			return "", false
		}
		plain := strings.NewReplacer(".", "", ",", "").Replace(digits)
		value, _ = new(big.Rat).SetString(plain)
		if value == nil || value.Cmp(big.NewRat(500, 1)) < 0 {
			return "", false
		}
	default:
		// With a unit, one decimal separator is a decimal point: 1,5jt, 2.5rb.
		if strings.Count(digits, ".")+strings.Count(digits, ",") > 1 {
			return "", false
		}
		decimal := strings.Replace(digits, ",", ".", 1)
		number, ok := new(big.Rat).SetString(decimal)
		if !ok {
			return "", false
		}
		multiplier := int64(1_000)
		if unit == "jt" {
			multiplier = 1_000_000
		}
		value = number.Mul(number, big.NewRat(multiplier, 1))
	}
	if !value.IsInt() || value.Sign() <= 0 || value.Cmp(big.NewRat(maxAnswerAmount, 1)) > 0 {
		return "", false
	}
	return value.Num().String(), true
}
