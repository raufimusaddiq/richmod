package push

import "github.com/raufimusaddiq/richmod/apps/reviewdomain/pushsupport"

// PublicKey delegates to the shared VAPID parser. The handler itself uses the
// Parse predicate so a missing or invalid subject disables push.
func PublicKey(privateKey string) string { return pushsupport.PublicKey(privateKey) }
