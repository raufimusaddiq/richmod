package plainlabel

import (
	"encoding/json"
	"regexp"
	"strings"
)

var labels = map[string]string{
	"EXPENSE":                            "pengeluaran",
	"INCOME":                             "pemasukan",
	"TRANSFER":                           "transfer",
	"REFUND":                             "pengembalian dana",
	"SAVINGS_TRANSFER":                   "transfer ke tabungan",
	"INVESTMENT_CONTRIBUTION":            "setoran investasi",
	"ASSET_PURCHASE":                     "pembelian aset",
	"DEBT_PRINCIPAL_PAYMENT":             "pembayaran pokok utang",
	"INTERNAL_TRANSFER":                  "pindah antar rekening sendiri",
	"OWN_ACCOUNT":                        "rekening sendiri",
	"HOUSEHOLD_ACCOUNT":                  "rekening keluarga",
	"INVESTMENT_ACCOUNT":                 "rekening investasi",
	"CYCLE_RESIDUAL_ALLOCATION":          "sisa gaji siklus ini yang belum jelas ke mana",
	"WEALTH_OBSERVATION_CONFIRMATION":    "saldo kekayaan yang perlu dikonfirmasi",
	"PAYSLIP_CONFIRMATION":               "slip gaji yang perlu dikonfirmasi",
	"MISSING_PAY_DATE":                   "tanggal gajian yang belum jelas",
	"MISSING_TRANSACTION_DATE":           "tanggal transaksi yang belum jelas",
	"TRANSACTION_FACTS_MISSING":          "detail transaksi yang belum lengkap",
	"TRANSFER_CLASSIFICATION":            "jenis transfer yang belum jelas",
	"UNKNOWN_MERCHANT":                   "merchant yang belum dikenal",
	"UNKNOWN_PURPOSE":                    "keperluan yang belum jelas",
	"AMBIGUOUS_CATEGORY":                 "kategori yang belum pasti",
	"POSSIBLE_DUPLICATE":                 "transaksi yang mungkin dobel",
	"CONFLICTING_EVIDENCE":               "bukti yang saling berbeda",
	"RECEIPT_MISMATCH":                   "struk yang tidak cocok",
	"UNKNOWN_EMAIL_TEMPLATE":             "email yang formatnya belum dikenal",
	"UNKNOWN_BANK_TEMPLATE":              "email bank yang formatnya belum dikenal",
	"DOCUMENT_EXTRACTION_LOW_CONFIDENCE": "dokumen yang kurang terbaca",
	"DOCUMENT_CLASSIFICATION":            "dokumen yang jenisnya belum jelas",
	"FINANCIAL_EMAIL_RESOLUTION":         "email keuangan yang perlu dicek",
	"MANUAL_CORRECTION":                  "koreksi manual",
	"SALARY_CYCLE":                       "siklus gaji",
	"CALENDAR_MONTH":                     "bulan kalender",
	"CURRENT_CYCLE":                      "siklus gaji ini",
	"PREVIOUS_CYCLE":                     "siklus gaji sebelumnya",
	"PREVIOUS_FULL":                      "siklus penuh sebelumnya",
	"FULL_CYCLE":                         "satu siklus penuh",
	"ELAPSED_DAYS":                       "jumlah hari yang sama",
	"SAME_DAY":                           "hari yang sama",
	"MISSING_SALARY_ANCHOR":              "tanggal gajian belum diketahui",
	"UNCATEGORIZED_EXPENSE":              "pengeluaran tanpa kategori",
	"OPEN_REVIEWS":                       "tinjauan yang belum selesai",
	"PROCESSING_INCOMPLETE":              "ada data yang masih diproses",
	"CASHFLOW_PARTIAL":                   "arus kas belum lengkap",
	"COMPARISON_UNAVAILABLE":             "perbandingan belum bisa dibuat",
	"WEALTH_RECONCILIATION":              "pencocokan kekayaan",
	"WEALTH_ACCOUNT_SET_CHANGED":         "daftar akun kekayaan berubah",
	"WEALTH_SNAPSHOT_BEFORE_CYCLE":       "saldo kekayaan sebelum siklus belum ada",
	"MISSING_WEALTH_SNAPSHOT":            "saldo kekayaan belum dicatat",
	"MISSING_PREVIOUS_WEALTH_SNAPSHOT":   "saldo kekayaan sebelumnya belum dicatat",
	"PRIMARY_SALARY":                     "gaji utama",
	"ORDINARY_INCOME":                    "pemasukan biasa",
	"CASHFLOW":                           "arus kas",
	"PROCESSING":                         "pemrosesan data",
	"MUTUAL_FUND":                        "reksa dana",
	"EWALLET":                            "e-wallet",
	"BROKERAGE":                          "rekening efek",
	"LIABILITY":                          "utang",
	"MERCHANT":                           "merchant",
	"COUNTERPARTY":                       "pihak lawan transaksi",
	"MEMBER":                             "anggota keluarga",
	"UNKNOWN":                            "belum diketahui",
	"NEEDS_REVIEW":                       "menunggu tinjauan",
	"CONFIRMED":                          "sudah dikonfirmasi",
	"VOIDED":                             "dibatalkan",
	"PENDING":                            "menunggu",
	"OPEN":                               "terbuka",
	"RESOLVED":                           "selesai",
}

var UpperCodePattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+\b`)

var codeValuePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)*$`)

func Label(code string) (string, bool) {
	label, ok := labels[code]
	return label, ok
}

func Humanize(code string) string {
	return strings.ToLower(strings.ReplaceAll(code, "_", " "))
}

func Value(value any) any {
	switch typed := value.(type) {
	case string:
		if !codeValuePattern.MatchString(typed) || len(typed) < 3 {
			return typed
		}
		if label, ok := Label(typed); ok {
			return label
		}
		if strings.Contains(typed, "_") {
			return Humanize(typed)
		}
		return typed
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = Value(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = Value(item)
		}
		return out
	}
	return value
}

func Facts(facts map[string]any) map[string]any {
	if facts == nil {
		return nil
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return facts
	}
	var generic map[string]any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		return facts
	}
	return Value(generic).(map[string]any)
}
