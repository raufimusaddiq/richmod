package document

import "slices"

// screenshotTimeConflict marks a screenshot row that cannot be the same
// event as the candidate transaction. A QR screenshot reprints the original
// purchase time, so a same-merchant hit an hour or more away is a separate
// purchase, not a re-sent screenshot. Different merchants are handled by the
// shared score: without a merchant match they can never reach the strong
// 0.90 link threshold.
func screenshotTimeConflict(candidate matchCandidate, merchant string) bool {
	return sameMerchant(candidate.Merchant, merchant) && candidate.Hours >= 1
}

func filterScreenshotTimeConflicts(candidates []matchCandidate, merchant string) []matchCandidate {
	return slices.DeleteFunc(candidates, func(candidate matchCandidate) bool {
		return screenshotTimeConflict(candidate, merchant)
	})
}
