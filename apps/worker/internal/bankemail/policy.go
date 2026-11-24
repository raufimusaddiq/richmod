package bankemail

import "strings"

type PolicyResult struct {
	Type, Status, ReviewType, Description string
	CategoryID                            string
	AutoConfirm                           bool
}

type MerchantMemory struct {
	MerchantID, CategoryID string
	AutoApply              bool
}

func EvaluateBankEmail(listener Listener, extraction Extraction, knownAccounts []KnownAccount, memory ...MerchantMemory) PolicyResult {
	remembered := MerchantMemory{}
	if len(memory) > 0 {
		remembered = memory[0]
	}
	if extraction.Kind == "NON_TRANSACTION" {
		return PolicyResult{Type: "IGNORE", Status: "IGNORED", Description: "Notifikasi ini bukan transaksi."}
	}
	if extraction.Kind == "UNKNOWN" {
		return review("Jenis notifikasi belum dapat dipastikan.", "UNKNOWN_BANK_TEMPLATE")
	}
	if missing(extraction, "amount_idr") || missing(extraction, "transaction_at") {
		return review("Detail transaksi belum lengkap.", "DOCUMENT_EXTRACTION_LOW_CONFIDENCE")
	}
	if listener.TrackingPolicy == "SPENDING_ONLY" && extraction.Direction != nil && *extraction.Direction == "INCOMING" {
		return PolicyResult{Type: "IGNORE", Status: "IGNORED", Description: "Pemasukan ke rekening pengeluaran tidak dicatat sebagai pendapatan."}
	}
	if extraction.Channel != nil && (*extraction.Channel == "RDN" || *extraction.Channel == "INTERNAL_TRANSFER") {
		return PolicyResult{Type: "IGNORE", Status: "IGNORED", Description: "Pergerakan internal atau investasi tidak termasuk pencatatan pengeluaran."}
	}
	if extraction.Direction != nil && *extraction.Direction == "OUTGOING" && extraction.Channel != nil && *extraction.Channel == "TRANSFER" {
		for _, account := range knownAccounts {
			if (account.Relationship == "OWN_ACCOUNT" || account.Relationship == "HOUSEHOLD_ACCOUNT") && accountMatches(account.MatchHint, extraction) {
				return PolicyResult{Type: "TRANSFER", Status: "CONFIRMED", Description: "Transfer antar rekening yang dikenal.", AutoConfirm: true}
			}
		}
		return review("Tujuan transfer perlu dikonfirmasi.", "TRANSFER_CLASSIFICATION")
	}
	if extraction.Direction != nil && *extraction.Direction == "OUTGOING" && extraction.Channel != nil {
		switch *extraction.Channel {
		case "DEBIT_CARD", "MERCHANT_PAYMENT", "QR", "ATM", "BANK_FEE", "OTHER":
			if extraction.Merchant == nil || strings.TrimSpace(*extraction.Merchant) == "" {
				return PolicyResult{Type: "EXPENSE", Status: "NEEDS_REVIEW", ReviewType: "UNKNOWN_MERCHANT", Description: "Nama merchant belum tersedia."}
			}
			if remembered.AutoApply && remembered.CategoryID != "" {
				return PolicyResult{Type: "EXPENSE", Status: "CONFIRMED", CategoryID: remembered.CategoryID, Description: "Pengeluaran dengan kategori merchant yang telah dikonfirmasi.", AutoConfirm: true}
			}
			return PolicyResult{Type: "EXPENSE", Status: "NEEDS_REVIEW", ReviewType: "AMBIGUOUS_CATEGORY", Description: "Pengeluaran menunggu kategori."}
		}
	}
	return review("Transaksi perlu ditinjau sebelum dicatat.", "UNKNOWN_PURPOSE")
}

func accountMatches(hint string, extraction Extraction) bool {
	digits := func(value string) string {
		var out strings.Builder
		for _, r := range value {
			if r >= '0' && r <= '9' {
				out.WriteRune(r)
			}
		}
		return out.String()
	}
	needle := digits(value(extraction.Counterparty))
	if needle == "" {
		needle = digits(value(extraction.Description))
	}
	return needle != "" && strings.HasSuffix(needle, digits(hint))
}
func value(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func missing(e Extraction, name string) bool {
	for _, v := range e.MissingFields {
		if v == name {
			return true
		}
	}
	switch name {
	case "amount_idr":
		return e.AmountIDR == nil
	case "transaction_at":
		return e.TransactionAt == nil
	}
	return false
}
func review(description, typ string) PolicyResult {
	return PolicyResult{Type: "NEEDS_REVIEW", Status: "NEEDS_REVIEW", ReviewType: typ, Description: description}
}
