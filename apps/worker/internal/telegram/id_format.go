package telegram

import "time"

// Go's time.Format only knows English month names; user-facing dates use Indonesian via these helpers.
var indonesianMonths = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

func formatIDDate(value time.Time) string {
	local := value.In(jakartaLocation())
	return local.Format("02 ") + indonesianMonths[local.Month()-1] + local.Format(" 2006")
}

func formatIDDateTime(value time.Time) string {
	return formatIDDate(value) + value.In(jakartaLocation()).Format(" 15:04")
}
