package telegram

import "strings"

const (
	outcomeSavedMessage     = "✅ Sudah tersimpan."
	outcomeIgnoredMessage   = "Oke, kuabaikan. Tidak ada yang dicatat."
	outcomeUnchangedMessage = "Tidak ada yang berubah."
	outcomeFailedMessage    = "Belum berhasil, dan belum ada yang berubah. Coba lagi sebentar lagi, ya."
)

func mutationOutcomeCopy(result agentToolResult) (string, bool) {
	status := strings.ToUpper(result.Status)
	switch {
	case result.Mutation != nil:
		return outcomeSavedMessage, true
	case status == "IGNORED" || status == "SKIPPED":
		return outcomeIgnoredMessage, true
	case status == "FAILED" || status == "ERROR":
		return outcomeFailedMessage, true
	case len(result.Facts) == 0 && len(result.Review) == 0:
		return outcomeUnchangedMessage, true
	}
	return "", false
}
