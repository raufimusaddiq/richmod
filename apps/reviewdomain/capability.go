package reviewdomain

import "github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"

// TelegramCompleteActions lists ReviewDecision ordinary actions with a Telegram
// terminal or continuation. Admin metrics and the worker contract gate share it.
// It is derived from the review kind registry (ADR-054), so a kind that gains a
// Telegram lane there is counted here without a second hand-kept list.
func TelegramCompleteActions() []string {
	return reviewdec.TelegramCompleteActions()
}
