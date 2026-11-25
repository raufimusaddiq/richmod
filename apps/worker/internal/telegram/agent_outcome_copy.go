package telegram

import "strings"

const (
	outcomeSavedMessage     = "✅ Sudah tersimpan."
	outcomeIgnoredMessage   = "Oke, kuabaikan. Tidak ada yang dicatat."
	outcomeUnchangedMessage = "Tidak ada yang berubah."
	outcomeFailedMessage    = "Belum berhasil, dan belum ada yang berubah. Coba lagi sebentar lagi, ya."
	outcomeAwaitingMessage  = "Sudah kusiapkan, tinggal menunggu konfirmasimu lewat tombolnya. Belum ada yang berubah."
)

func isAwaitingStatus(status string) bool {
	for _, prefix := range []string{"PENDING", "AWAITING", "STAGED"} {
		if strings.HasPrefix(status, prefix) {
			return true
		}
	}
	return false
}

func mutationOutcomeCopy(result agentToolResult) (string, bool) {
	status := strings.ToUpper(result.Status)
	switch {
	case isAwaitingStatus(status):
		return outcomeAwaitingMessage, true
	case status == "FAILED" || status == "ERROR":
		return outcomeFailedMessage, true
	case status == "IGNORED" || status == "SKIPPED":
		return outcomeIgnoredMessage, true
	case result.Mutation != nil:
		return outcomeSavedMessage, true
	case len(result.Facts) == 0 && len(result.Review) == 0:
		return outcomeUnchangedMessage, true
	}
	return "", false
}
