package financialemail

// resolutionGaps names exactly the entities the evidence could not bind, so the
// stored ReviewDecision asks the household for the unresolved dimension only and
// never re-requests an entity Go already resolved (PRD 12, 13.4).
func resolutionGaps(account, wealth string) []string {
	missing := make([]string, 0, 2)
	if account == "" {
		missing = append(missing, "funding_account")
	}
	if wealth == "" {
		missing = append(missing, "wealth_account")
	}
	return missing
}
