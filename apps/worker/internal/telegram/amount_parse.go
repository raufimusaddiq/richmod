package telegram

import (
	"math/big"
	"regexp"
	"strings"
)

const maxAnswerAmount = 100_000_000

var (
	amountToken     = regexp.MustCompile(`^(?:rp\.?)?(\d+(?:[.,]\d+)*)(rb|ribu|k|jt|juta)?$`)
	amountThousands = regexp.MustCompile(`^\d{1,3}([.,]\d{3})+$`)
	amountUnits     = map[string]string{"rb": "rb", "ribu": "rb", "k": "rb", "jt": "jt", "juta": "jt"}
)

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
		if strings.ContainsAny(digits, ".,") && !amountThousands.MatchString(digits) {
			return "", false
		}
		plain := strings.NewReplacer(".", "", ",", "").Replace(digits)
		value, _ = new(big.Rat).SetString(plain)
		if value == nil || value.Cmp(big.NewRat(500, 1)) < 0 {
			return "", false
		}
	default:
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
