package telegram

import "time"

// Go's time.Format only knows English month names. The household reads
// Indonesian, so every user-facing date goes through these helpers.
var indonesianMonths = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// formatIDDate renders "02 Agu 2026" in Asia/Jakarta.
func formatIDDate(value time.Time) string {
	local := value.In(jakartaLocation())
	return local.Format("02 ") + indonesianMonths[local.Month()-1] + local.Format(" 2006")
}

// formatIDDateTime renders "02 Agu 2026 15:04" in Asia/Jakarta.
func formatIDDateTime(value time.Time) string {
	return formatIDDate(value) + value.In(jakartaLocation()).Format(" 15:04")
}
