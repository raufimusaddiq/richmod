package reviewdomain

import "github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"

// TelegramCompleteActions lists ReviewDecision ordinary actions with a Telegram
// terminal or continuation, derived from the review kind registry (ADR-054) so
// Admin metrics and the worker contract gate share one list.
func TelegramCompleteActions() []string {
	return reviewdec.TelegramCompleteActions()
}
