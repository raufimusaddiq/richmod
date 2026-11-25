package reviewdomain

import (
	"regexp"
	"strings"
)

const telegramCallbackMaxBytes = 64

var pendingCallbackToken = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var telegramCallbackExact = map[string]struct{}{
	"review:expense": {}, "review:asset": {}, "review:own": {}, "review:household": {}, "review:income": {},
	"review:investment": {}, "review:confirm": {}, "review:change": {}, "review:remember": {},
	"review:once": {}, "review:edit": {}, "review:merchant": {}, "review:description": {},
	"review:category": {}, "review:ignore": {}, "review:reprocess": {},
	"review:quality:confirm": {}, "review:salary:primary": {}, "review:salary:household": {}, "review:salary:ordinary": {},
	"review:dup:new": {}, "review:dispute:confirm": {}, "review:dispute:reject": {},
	"review:leave": {}, "review:xfer:new": {},
	"pending:action:yes": {}, "pending:action:no": {}, "pending:batch:yes": {}, "pending:batch:no": {},
}

var telegramCallbackTokenPrefixes = []string{
	"pending:action:yes:", "pending:action:no:", "pending:batch:yes:", "pending:batch:no:", "pending:card:",
	"review:cat:", "review:category:", "review:fe:account:", "review:fe:wealth:",
	"review:invest:", "review:bank:",
}

var telegramCallbackNumberPrefixes = []string{
	"review:catpage:", "review:fepage:", "review:bankpage:", "review:duppage:", "review:investpage:",
	"review:dup:merge:", "review:xfer:merge:",
}

func ValidTelegramCallback(data string) bool {
	if data == "" || len(data) > telegramCallbackMaxBytes {
		return false
	}
	if _, ok := telegramCallbackExact[data]; ok {
		return true
	}
	for _, prefix := range telegramCallbackTokenPrefixes {
		if strings.HasPrefix(data, prefix) {
			token := strings.TrimPrefix(data, prefix)
			if strings.HasPrefix(prefix, "pending:") {
				return pendingCallbackToken.MatchString(token)
			}
			return validCallbackToken(token)
		}
	}
	for _, prefix := range telegramCallbackNumberPrefixes {
		if strings.HasPrefix(data, prefix) {
			return validCallbackNumber(strings.TrimPrefix(data, prefix))
		}
	}
	return false
}

func TelegramCallbackSamples() []string {
	samples := make([]string, 0, len(telegramCallbackExact)+len(telegramCallbackTokenPrefixes)+len(telegramCallbackNumberPrefixes))
	for data := range telegramCallbackExact {
		samples = append(samples, data)
	}
	for _, prefix := range telegramCallbackTokenPrefixes {
		samples = append(samples, prefix+"8a97e069-0278-4f49-9195-fbbfe81fdfd5")
	}
	for _, prefix := range telegramCallbackNumberPrefixes {
		samples = append(samples, prefix+"1")
	}
	return samples
}

func validCallbackToken(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

func validCallbackNumber(value string) bool {
	if value == "" || len(value) > 4 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
