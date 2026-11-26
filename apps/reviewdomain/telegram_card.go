package reviewdomain

import (
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func TelegramCardText(title, facts, why, instruction string) string {
	parts := make([]string, 0, 4)
	for _, part := range []string{title, facts, whyPart(why), instruction} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n\n")
}

func whyPart(why string) string {
	if strings.TrimSpace(why) == "" {
		return ""
	}
	return "Alasan: " + strings.TrimSpace(why)
}

const CycleResidualCardInstruction = "Pilih di bawah, atau ketik nama akunnya."

func CycleResidualQuestion(cycleStart, cycleEnd time.Time, residual string) string {
	last := cycleEnd.AddDate(0, 0, -1)
	period := FormatIndonesianShortDate(cycleStart) + "–" + FormatIndonesianShortDate(last)
	if !strings.ContainsAny(residual, "0123456789") {
		return "Sisa siklus " + period + " belum dialokasikan. Mau dialokasikan ke mana?"
	}
	return "Sisa siklus " + period + ": Rp" + FormatRupiah(residual) + ". Mau dialokasikan ke mana?"
}

func CycleResidualCardText(cycleStart, cycleEnd time.Time, residual string) string {
	return TelegramCardText("🟡 "+CycleResidualQuestion(cycleStart, cycleEnd, residual), "", reviewdec.WhyLine(reviewdec.Decision{ReasonCode: "CYCLE_RESIDUAL_ALLOCATION"}), CycleResidualCardInstruction)
}

var indonesianMonths = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

func FormatIndonesianDate(value time.Time) string {
	return value.Format("2") + " " + indonesianMonths[value.Month()-1] + " " + value.Format("2006")
}

func FormatIndonesianShortDate(value time.Time) string {
	return value.Format("2") + " " + indonesianMonths[value.Month()-1]
}

func FormatRupiah(value string) string {
	value = strings.TrimSpace(value)
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign, value = "-", strings.TrimPrefix(value, "-")
	}
	if whole, fraction, found := strings.Cut(value, "."); found {
		value = whole
		if fraction != "" && fraction[0] >= '5' {
			value = incrementDecimal(whole)
		}
	}
	if len(value) <= 3 {
		return sign + value
	}
	first := len(value) % 3
	if first == 0 {
		first = 3
	}
	parts := []string{value[:first]}
	for index := first; index < len(value); index += 3 {
		parts = append(parts, value[index:index+3])
	}
	return sign + strings.Join(parts, ".")
}

func incrementDecimal(value string) string {
	digits := []byte(value)
	for i := len(digits) - 1; i >= 0; i-- {
		if digits[i] < '9' {
			digits[i]++
			return string(digits)
		}
		digits[i] = '0'
	}
	return "1" + string(digits)
}
