package financialemail

// resolutionGaps names the entities the evidence could not bind, so the review asks only for unresolved dimensions.
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
