package reviewdomain

import "strings"

func NormalizedEmployer(employer string) string {
	return strings.ToLower(cleanText(employer, 160))
}
