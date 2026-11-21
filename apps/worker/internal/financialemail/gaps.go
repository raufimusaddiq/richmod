package financialemail

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
