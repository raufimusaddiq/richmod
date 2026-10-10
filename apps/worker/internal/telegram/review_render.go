package telegram

// review_render.go turns the stored ReviewDecision into the Telegram
// presentation: which conversation state the reply binds to, the prompt text,
// and the markup mode. Rendering follows missing_facts/allowed_actions instead
// of a review_type default, so a date, policy, or duplicate review can never be
// mis-rendered as a category chooser.

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
	// A producer may supply no subject summary. The category chooser sends the
	// summary verbatim and Telegram rejects an empty body, so it falls back to the
	// decision's own prompt. Every other card already leads with that prompt as its
	// title, so using it again as the summary would print it twice.
	if strings.TrimSpace(context) == "" && isCategoryOnly(decision) {
		context = promptTitle(decision)
	}
	switch {
	case decision.ReasonCode == "MISSING_AMOUNT":
		instruction := "Balas pesan ini (Reply) dengan nominal dalam rupiah, tanpa pemisah. Contoh: 75000."
		if contains(decision.MissingFacts, "transfer_relationship") {
			instruction += " Setelah itu, konfirmasi apakah ini penghasilan atau transfer sendiri."
		}
		return "AWAITING_DETAIL", reviewDetailMessage("🟡 Nominal transaksi belum terbaca", context, instruction), "reply"
	case decision.ReasonCode == "RECEIPT_MISMATCH" && decision.Consequence == reviewdec.QualitySignal && len(decision.MissingFacts) == 0:
		const prompt = "🧾 Rincian struk berbeda dari total yang tercetak. Cek totalnya dulu sebelum mencatat."
		if strings.TrimSpace(context) == "" {
			return "AWAITING_DETAIL", prompt, "receipt_quality"
		}
		return "AWAITING_DETAIL", prompt + "\n\n" + context, "receipt_quality"
	case decision.ReasonCode == "FINANCIAL_EMAIL_FACTS":
		// The email did not support a required financial fact, so no canonical
		// transaction was written. The only bounded action is to acknowledge it;
		// there is no fact the household must supply here.
		return "AWAITING_DETAIL", reviewDetailMessage("🟡 Bukti email belum pasti", context, "Bukti email ini belum didukung. Pilih Abaikan atau buka Kotak Tinjauan untuk memeriksanya."), "financial_email_facts"
	case contains(decision.AllowedActions, "REPROCESS_DOCUMENT"):
		// A document review's only bounded action is to retry the shared document
		// pipeline; the summary is the document's own extraction context.
		return "AWAITING_DETAIL", context, "document"
	case contains(decision.AllowedActions, "SET_FINANCIAL_EMAIL_ENTITIES"):
		// A financial provider email resolved every entity except one or two, so the
		// card is a bounded chooser over the household's own accounts; the summary
		// names the provider hint that still needs binding.
		return "AWAITING_DETAIL", reviewDetailMessage(promptTitle(decision), context, "Pilih rekening untuk bukti email ini."), "financial_email"
	case isCategoryOnly(decision):
		// Category is the single unresolved fact and it has bounded values, so the
		// chooser is the whole interaction; merchant enrichment is optional.
		return "AWAITING_CATEGORY", context, "category"
	case contains(decision.MissingFacts, "transfer_relationship"):
		return "AWAITING_DETAIL", reviewDetailMessage(promptTitle(decision), context, "Pilih jenis transfer di bawah, atau balas pesan ini (Reply) dengan tujuan transfer."), "transfer"
	case decision.InteractionMode == reviewdec.ModeConflictResolution || contains(decision.MissingFacts, "duplicate_relationship"):
		return "AWAITING_DETAIL", reviewDetailMessage(promptTitle(decision), context, replyInstruction(decision)), "duplicate"
	case contains(decision.MissingFacts, "salary_classification") && contains(decision.AllowedActions, "PRIMARY_SALARY") && contains(decision.AllowedActions, "ORDINARY_INCOME"):
		return "AWAITING_DETAIL", reviewDetailMessage("🧾 Ini gaji utama atau pemasukan biasa?", context, "Pilih jenis pemasukan di bawah."), "salary"
	// A date fact is collected first because the reply lane binds exactly one
	// value. A compound category+date decision therefore starts with the date
	// prompt and advances to the category chooser afterward, so no missing fact is
	// dropped.
	case contains(decision.MissingFacts, "transaction_at"):
		return "AWAITING_DATE", reviewDetailMessage(promptTitle(decision), context, dateInstruction(decision)), "reply"
	case contains(decision.MissingFacts, "merchant"):
		return "AWAITING_MERCHANT", reviewDetailMessage("🟡 Nama merchant belum ada", context, "Balas pesan ini (Reply) dengan nama merchant."), "reply"
	case requiresBoundReply(decision):
		title := promptTitle(decision)
		return "AWAITING_DETAIL", reviewDetailMessage(title, context, replyInstruction(decision)), "reply"
	default:
		return "AWAITING_DETAIL", reviewDetailMessage(unknownReviewPrompt, context, "Balas pesan ini (Reply) dengan keterangan atau tujuan transaksi."), "reply"
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
			return "🟡 Ini termasuk jenis gaji apa?"
		case "transaction_at":
			if decision.ReasonCode == "MISSING_PAY_DATE" {
				return "🟡 Tanggal pembayaran belum ada"
			}
			return "🟡 Tanggal transaksi belum ada"
		case "transaction_semantics":
			return "🟡 Perlu detail transaksi"
		case "duplicate_relationship":
			return "🟡 Transaksi ini mungkin duplikat"
		case "funding_account", "wealth_account":
			return "🟡 Rekening bukti email belum pasti"
		}
	}
	return unknownReviewPrompt
}

// dateInstruction asks for the one fact the date review is missing in the format
// the date resolver parses, instead of the generic description wording.
func dateInstruction(decision reviewdec.Decision) string {
	if decision.ReasonCode == "MISSING_PAY_DATE" {
		return "Balas pesan ini (Reply) dengan tanggal pembayaran. Contoh: 25 September 2026."
	}
	return "Balas pesan ini (Reply) dengan tanggal transaksi (YYYY-MM-DD)."
}

func replyInstruction(decision reviewdec.Decision) string {
	if contains(decision.MissingFacts, "duplicate_relationship") {
		return "Pilih catatan yang ingin digabung, atau pilih Catat sebagai baru di bawah."
	}
	if contains(decision.MissingFacts, "transaction_at") {
		return dateInstruction(decision)
	}
	return "Balas pesan ini (Reply) dengan keterangan atau tujuan transaksi."
}
