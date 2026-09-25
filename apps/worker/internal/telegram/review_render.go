package telegram

// review_render.go turns the stored ReviewDecision into the Telegram
// presentation: which conversation state the reply binds to, the prompt text,
// and the markup mode. Rendering follows missing_facts/allowed_actions instead
// of a review_type default, so a date, policy, or duplicate review can never be
// mis-rendered as a category chooser (PRD §7.8, UIR-03).

import "github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"

// unknownReviewPrompt is the honest fallback: when a review has no decision
// contract, Go asks the user for detail instead of inventing a category prompt.
const unknownReviewPrompt = "🟡 Perlu detail transaksi"

// renderReviewPresentation maps a ReviewDecision plus its subject summary to the
// Telegram prompt. It never introduces a fact the decision did not name: the
// missing fact decides the question, the allowed actions decide the buttons.
func renderReviewPresentation(decision reviewdec.Decision, reviewType, context string) (state, reviewMessage, markupMode string) {
	switch {
	case isCategoryOnly(decision):
		// Category is the single unresolved fact and it has bounded values, so the
		// chooser is the whole interaction; merchant enrichment is optional.
		return "AWAITING_CATEGORY", context, "category"
	case decision.InteractionMode == reviewdec.ModePolicyChoice && reviewType == "TRANSFER_CLASSIFICATION":
		return "AWAITING_DETAIL", context, "transfer"
	case decision.InteractionMode == reviewdec.ModeConflictResolution || contains(decision.MissingFacts, "duplicate_relationship"):
		return "AWAITING_DETAIL", reviewDetailMessage(promptTitle(decision), context, replyInstruction(decision)), "duplicate"
	case requiresBoundReply(decision):
		title := promptTitle(decision)
		return "AWAITING_DETAIL", reviewDetailMessage(title, context, replyInstruction(decision)), "reply"
	default:
		return "AWAITING_DETAIL", reviewDetailMessage(unknownReviewPrompt, context, "Balas pesan ini dengan keterangan atau tujuan transaksi."), "reply"
	}
}

// isCategoryOnly reports a review whose only unresolved fact is the category. A
// compound residual (category plus date) deliberately does not qualify: it needs
// a bound reply so no dimension is silently dropped.
func isCategoryOnly(decision reviewdec.Decision) bool {
	if len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "category" {
		return false
	}
	return contains(decision.AllowedActions, "CONFIRM_REVIEW") || contains(decision.AllowedActions, "SET_CATEGORY")
}

// requiresBoundReply reports whether the unresolved dimension is a free-form
// value the user must type, which is the case for a single-field or correction
// review that is not a bounded category choice.
func requiresBoundReply(decision reviewdec.Decision) bool {
	if len(decision.MissingFacts) == 0 {
		return false
	}
	for _, fact := range decision.MissingFacts {
		if fact == "category" {
			continue
		}
		return true
	}
	return false
}

func promptTitle(decision reviewdec.Decision) string {
	for _, fact := range decision.MissingFacts {
		switch fact {
		case "transaction_at":
			return "🟡 Tanggal transaksi belum ada"
		case "transaction_semantics":
			return "🟡 Perlu detail transaksi"
		case "duplicate_relationship":
			return "🟡 Transaksi ini mungkin duplikat"
		}
	}
	return unknownReviewPrompt
}

func replyInstruction(decision reviewdec.Decision) string {
	if contains(decision.MissingFacts, "duplicate_relationship") {
		return "Balas pesan ini dengan pilihan pada tombol di atas."
	}
	return "Balas pesan ini dengan keterangan atau tujuan transaksi."
}
