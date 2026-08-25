package settings

import "strings"

func accountPolicyError(currentName, resultName, resultPolicy string) string {
	currentJago := strings.Contains(strings.ToLower(currentName), "jago")
	resultJago := strings.Contains(strings.ToLower(resultName), "jago")
	if currentJago && currentName != resultName {
		return "Bank Jago account name is fixed"
	}
	if resultJago && resultPolicy != "SPENDING_ONLY" {
		return "Bank Jago must use SPENDING_ONLY"
	}
	return ""
}
