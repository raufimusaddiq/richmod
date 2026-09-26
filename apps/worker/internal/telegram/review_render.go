package telegram

// review_render.go turns the stored ReviewDecision into the Telegram
// presentation: which conversation state the reply binds to, the prompt text,
// and the markup mode. Rendering follows missing_facts/allowed_actions instead
// of a review_type default, so a date, policy, or duplicate review can never be
// mis-rendered as a category chooser (PRD §7.8, UIR-03).

import (
	"strings"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// unknownReviewPrompt is the honest fallback: when a review has no decision
// contract, Go asks the user for detail instead of inventing a category prompt.
const unknownReviewPrompt = "🟡 Perlu detail transaksi"

// renderReviewPresentation maps a ReviewDecision plus its subject summary to the
// Telegram prompt. It never introduces a fact the decision did not name: the
// missing fact decides the question, the allowed actions decide the buttons.
func renderReviewPresentation(decision reviewdec.Decision, reviewType, context string) (state, reviewMessage, markupMode string) {
	// A producer may supply no subject summary. The card body still has to be
	// non-empty or Telegram rejects the send, so fall back to the decision's own
	// prompt for the category/transfer modes that otherwise pass context verbatim.
	if strings.TrimSpace(context) == "" && (isCategoryOnly(decision) || contains(decision.MissingFacts, "transfer_relationship") || contains(decision.MissingFacts, "salary_classification") || contains(decision.MissingFacts, "transaction_at")) {
		context = promptTitle(decision)
	}
	switch {
	case contains(decision.AllowedActions, "REPROCESS_DOCUMENT"):
		// A document review's only bounded action is to retry the shared document
		// pipeline; the summary is the document's own extraction context.
		return "AWAITING_DETAIL", context, "document"
	case isCategoryOnly(decision):
		// Category is the single unresolved fact and it has bounded values, so the
		// chooser is the whole interaction; merchant enrichment is optional.
		return "AWAITING_CATEGORY", context, "category"
	case contains(decision.MissingFacts, "transfer_relationship"):
		return "AWAITING_DETAIL", context, "transfer"
	case decision.InteractionMode == reviewdec.ModeConflictResolution || contains(decision.MissingFacts, "duplicate_relationship"):
		return "AWAITING_DETAIL", reviewDetailMessage(promptTitle(decision), context, replyInstruction(decision)), "duplicate"
	case contains(decision.MissingFacts, "salary_classification") && contains(decision.AllowedActions, "PRIMARY_SALARY") && contains(decision.AllowedActions, "ORDINARY_INCOME"):
		return "AWAITING_DETAIL", reviewDetailMessage("🧾 Pilih kebijakan gaji", context, "Pilih gaji utama atau pemasukan biasa."), "salary"
	// A date fact is collected first because the reply lane binds exactly one
	// value. A compound category+date decision therefore starts with the date
	// prompt and advances to the category chooser afterward, so no missing fact is
	// dropped.
	case contains(decision.MissingFacts, "transaction_at"):
		return "AWAITING_DATE", reviewDetailMessage(promptTitle(decision), context, dateInstruction(decision)), "reply"
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
	// A transfer review also carries CONFIRM_REVIEW, so the decision class must
	// agree that category is the unresolved dimension before using the chooser.
	return decision.DecisionClass == reviewdec.ClassEvidenceGap &&
		(contains(decision.AllowedActions, "CONFIRM_REVIEW") || contains(decision.AllowedActions, "SET_CATEGORY"))
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
		case "salary_classification":
			return "🟡 Pilih klasifikasi gaji"
		case "transaction_at":
			if decision.ReasonCode == "MISSING_PAY_DATE" {
				return "🟡 Tanggal pembayaran belum ada"
			}
			return "🟡 Tanggal transaksi belum ada"
		case "transaction_semantics":
			return "🟡 Perlu detail transaksi"
		case "duplicate_relationship":
			return "🟡 Transaksi ini mungkin duplikat"
		}
	}
	return unknownReviewPrompt
}

// dateInstruction asks for the one fact the date review is missing in the format
// the date resolver parses, instead of the generic description wording.
func dateInstruction(decision reviewdec.Decision) string {
	if decision.ReasonCode == "MISSING_PAY_DATE" {
		return "Balas pesan ini dengan tanggal pembayaran (contoh: 25 September 2026)."
	}
	return "Balas pesan ini dengan tanggal transaksi (YYYY-MM-DD)."
}

func replyInstruction(decision reviewdec.Decision) string {
	if contains(decision.MissingFacts, "duplicate_relationship") {
		return "Balas pesan ini dengan pilihan pada tombol di atas."
	}
	if contains(decision.MissingFacts, "transaction_at") {
		return dateInstruction(decision)
	}
	return "Balas pesan ini dengan keterangan atau tujuan transaksi."
}
