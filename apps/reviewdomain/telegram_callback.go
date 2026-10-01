package reviewdomain

import "strings"

// Telegram inline-button callback grammar, shared by the API webhook (which
// admits only this grammar before persisting anything) and the worker (which
// produces and consumes it). Every callback is a bounded literal or a literal
// prefix followed by an opaque token or a small page/index number; none carries
// a canonical identifier that authorizes a mutation on its own. The worker
// re-derives household, user, chat, and pending state for every callback.
//
// Telegram rejects callback_data longer than 64 bytes.
const telegramCallbackMaxBytes = 64

var telegramCallbackExact = map[string]struct{}{
	"review:expense": {}, "review:asset": {}, "review:own": {}, "review:household": {},
	"review:investment": {}, "review:confirm": {}, "review:change": {}, "review:remember": {},
	"review:once": {}, "review:edit": {}, "review:merchant": {}, "review:description": {},
	"review:category": {}, "review:ignore": {}, "review:reprocess": {},
	"review:quality:confirm": {}, "review:salary:primary": {}, "review:salary:ordinary": {},
	"review:dup:new": {},
	"pending:action:yes": {}, "pending:action:no": {}, "pending:batch:yes": {}, "pending:batch:no": {},
}

// Opaque-token callbacks: the token is a server-issued identifier of a choice
// the worker re-validates against the household.
var telegramCallbackTokenPrefixes = []string{
	"review:cat:", "review:category:", "review:fe:account:", "review:fe:wealth:",
	"review:invest:", "review:bank:",
}

// Numeric callbacks: a page number or a list position resolved server-side.
var telegramCallbackNumberPrefixes = []string{
	"review:catpage:", "review:fepage:", "review:dup:merge:",
}

// ValidTelegramCallback reports whether data is a callback the system issues.
func ValidTelegramCallback(data string) bool {
	if data == "" || len(data) > telegramCallbackMaxBytes {
		return false
	}
	if _, ok := telegramCallbackExact[data]; ok {
		return true
	}
	for _, prefix := range telegramCallbackTokenPrefixes {
		if strings.HasPrefix(data, prefix) {
			return validCallbackToken(strings.TrimPrefix(data, prefix))
		}
	}
	for _, prefix := range telegramCallbackNumberPrefixes {
		if strings.HasPrefix(data, prefix) {
			return validCallbackNumber(strings.TrimPrefix(data, prefix))
		}
	}
	return false
}

// TelegramCallbackSamples returns one valid example of every callback form. It
// exists so the worker can prove that everything it emits is admitted.
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
