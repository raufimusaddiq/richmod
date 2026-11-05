package reviewdomain

import (
	"strings"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

// Telegram review card text (ADR-046 amendment). A card has exactly one title,
// the facts of its subject, one short reason line, and one instruction that
// matches the buttons the card carries. The worker renders every card through
// TelegramCardText; the canonical operations that edit a live card (a refreshed
// cycle residual) use the same helpers so an edit keeps the card's facts.

// TelegramCardText joins the card parts, skipping empty ones.
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

// CycleResidualCardTitle is the cycle residual card's one title.
const CycleResidualCardTitle = "🧾 Sisa salary cycle belum dialokasikan"

// CycleResidualCardInstruction matches the cycle card's buttons: allocation and
// a missing transaction are typed replies; leaving it unallocated is a button.
const CycleResidualCardInstruction = "Balas pesan ini (Reply) untuk mengalokasikan sisa ke akun kekayaan, atau sebutkan transaksi yang belum tercatat. Pilih Biarkan saja jika sisa ini memang tidak dialokasikan."

// CycleResidualCardFacts names the cycle window (end exclusive) and residual.
func CycleResidualCardFacts(cycleStart, cycleEnd time.Time, residual string) string {
	last := cycleEnd.AddDate(0, 0, -1)
	return "Periode: " + FormatIndonesianDate(cycleStart) + " – " + FormatIndonesianDate(last) + "\nSisa: Rp" + FormatRupiah(residual)
}

// CycleResidualCardText is the complete cycle residual card.
func CycleResidualCardText(cycleStart, cycleEnd time.Time, residual string) string {
	return TelegramCardText(CycleResidualCardTitle, CycleResidualCardFacts(cycleStart, cycleEnd, residual), reviewdec.WhyLine(reviewdec.Decision{ReasonCode: "CYCLE_RESIDUAL_ALLOCATION"}), CycleResidualCardInstruction)
}

var indonesianMonths = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// FormatIndonesianDate renders a calendar date as "25 Okt 2026".
func FormatIndonesianDate(value time.Time) string {
	return value.Format("2") + " " + indonesianMonths[value.Month()-1] + " " + value.Format("2006")
}

// FormatRupiah groups a whole-rupiah decimal string with dots: 1250000 -> 1.250.000.
func FormatRupiah(value string) string {
	value = strings.TrimSpace(value)
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign, value = "-", strings.TrimPrefix(value, "-")
	}
	if whole, _, found := strings.Cut(value, "."); found {
		value = whole
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
