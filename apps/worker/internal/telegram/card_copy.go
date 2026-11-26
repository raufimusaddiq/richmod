package telegram

import "strings"

const (
	reviewInboxName     = "Kotak Tinjauan"
	settingsName        = "Pengaturan"
	rememberedOnceNote  = "Kategori hanya dipakai untuk transaksi ini."
	cardFinishedMessage = "Kartu ini sudah selesai."
)

func idr(amount string) string {
	if !strings.ContainsAny(amount, "0123456789") {
		return ""
	}
	return "Rp" + FormatIDR(amount)
}

func joinDot(parts ...string) string {
	var kept []string
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}

func savedLine(parts ...string) string {
	if detail := joinDot(parts...); detail != "" {
		return "✅ Tersimpan: " + detail
	}
	return "✅ Tersimpan."
}

func rememberedNote(merchant string) string {
	if merchant = strings.TrimSpace(merchant); merchant != "" {
		return "⭐ Diingat untuk " + merchant + ". Bisa dimatikan di " + settingsName + "."
	}
	return "⭐ Diingat untuk transaksi berikutnya. Bisa dimatikan di " + settingsName + "."
}

const ignoredLine = "🗑️ Diabaikan. Tidak dicatat."

func appendFact(lines []string, label, value string) []string {
	if value = strings.TrimSpace(value); value == "" {
		return lines
	}
	return append(lines, label+": "+value)
}

func factBlock(pairs ...string) string {
	var lines []string
	for index := 0; index+1 < len(pairs); index += 2 {
		lines = appendFact(lines, pairs[index], pairs[index+1])
	}
	return strings.Join(lines, "\n")
}

func IDR(amount string) string { return idr(amount) }

func BalanceSavedText(account, amount string) string {
	return Step{Kind: stepDone, Ack: "✅ Saldo tersimpan: " + joinDot(account, idr(amount)), Hint: balanceNote}.Text()
}

const balanceNote = "Ini catatan saldo saja, bukan transaksi."

const DocumentUnreadableNote = "Dokumen ini belum bisa dibaca, tapi fotonya tetap tersimpan sebagai bukti. Coba kirim foto yang lebih jelas, ya."
