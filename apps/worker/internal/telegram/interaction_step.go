package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

type stepKind string

const (
	stepChoice  stepKind = "CHOICE"
	stepFact    stepKind = "FACT"
	stepConfirm stepKind = "CONFIRM"
	stepDone    stepKind = "DONE"
)

type factKind string

const (
	factMerchant factKind = "merchant"
	factCategory factKind = "category"
	factRemember factKind = "remember"
	factTransfer factKind = "transfer"
	factAsset    factKind = "asset"
	factInvest   factKind = "invest"
	factRefresh  factKind = "refresh"
	factIncome   factKind = "income"

	factSalary      factKind = "salary"
	factDate        factKind = "date"
	factCycle       factKind = "cycle"
	factSplitAcct   factKind = "splitaccount"
	factSplitAmount factKind = "splitamount"
)

const transferReviewType = "TRANSFER_CLASSIFICATION"

const categoryHint = "Pilih di bawah, atau ketik nama kategorinya."

const (
	callbackIgnore     = "review:ignore"
	callbackAsset      = "review:asset"
	callbackRemember   = "review:remember"
	callbackOnce       = "review:once"
	callbackCategory   = "review:cat:"
	callbackCategoryPg = "review:catpage:"
	callbackExpense    = "review:expense"
	callbackOwn        = "review:own"
	callbackHousehold  = "review:household"
	callbackInvestment = "review:investment"
	callbackIncome     = "review:income"
	callbackBack       = "review:back"
	callbackConfirm    = "review:confirm"
	callbackChange     = "review:change"
	callbackAccount    = "review:acct:"
	callbackAccountPg  = "review:acctpage:"
	callbackInvest     = "review:invest:"
	callbackInvestPg   = "review:investpage:"
	callbackMerge      = "review:xfer:merge:"
	callbackNewXfer    = "review:xfer:new"

	callbackSalaryHousehold = "review:salary:household"
	callbackSalaryPrimary   = "review:salary:primary"
	callbackSalaryOrdinary  = "review:salary:ordinary"
	callbackDateToday       = "review:date:today"
	callbackDateYesterday   = "review:date:yesterday"
	callbackDateOther       = "review:date:other"
	callbackLeave           = "review:leave"
	callbackCycleMissing    = "review:cycmissing"
	callbackCycleSplit      = "review:cycsplit"
	callbackCycleAll        = "review:cycall"
	callbackCancel          = "review:cancel"
	callbackCycle           = "review:cyc:"
	callbackCyclePg         = "review:cycpage:"
)

type Step struct {
	Kind     stepKind
	Fact     factKind
	State    string
	Ack      string
	Ask      string
	Hint     string
	Facts    string
	Why      string
	Page     int
	Back     bool
	Raw      string
	ReadOnly bool
	Rows     [][]InlineKeyboardButton
}

func (s Step) Title() string {
	switch {
	case s.Kind == stepDone:
		return s.Ack
	case s.Ack != "":
		return s.Ack + " " + s.Ask
	}
	return "🟡 " + s.Ask
}

func (s Step) Text() string {
	if s.Raw != "" {
		return s.Raw
	}
	return reviewdomain.TelegramCardText(s.Title(), s.Facts, s.Why, s.Hint)
}

func (s Step) Markup() *InlineKeyboardMarkup {
	if len(s.Rows) == 0 {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: s.Rows}
}

func (s Step) AnswerKind() cardAnswerKind {
	switch s.Fact {
	case factMerchant:
		return cardAnswerDetail
	case factCategory:
		return cardAnswerCategory
	case factRemember:
		return cardAnswerConfirm
	case factAsset, factInvest, factCycle, factSplitAcct:
		return cardAnswerAccount
	case factDate:
		return cardAnswerDate
	case factSplitAmount:
		return cardAnswerAmount
	}
	return cardAnswerNone
}

func (s Step) WithAck(ack string) Step {
	s.Ack = ack
	return s
}

func (s Step) buttons() []InlineKeyboardButton {
	var out []InlineKeyboardButton
	for _, row := range s.Rows {
		out = append(out, row...)
	}
	return out
}

func engineReviewType(reviewType string) bool {
	return reviewType == "UNKNOWN_MERCHANT" || reviewType == "AMBIGUOUS_CATEGORY"
}

func engineOwnedType(reviewType string) bool {
	return engineReviewType(reviewType) || transferReview(reviewType) || payslipReviewType(reviewType) || reviewType == cycleReviewType
}

func transferReview(reviewType string) bool {
	return reviewType == transferReviewType || reviewType == "POSSIBLE_DUPLICATE"
}

func engineOwnsTransfer(decision reviewdec.Decision, subject reviewSubject) bool {
	if decision.ReasonCode == "MISSING_AMOUNT" || (decision.Provenance != nil && decision.Provenance["evidence_link_dispute"] == true) {
		return false
	}
	if !contains(decision.MissingFacts, "transfer_relationship") && !contains(decision.MissingFacts, "wealth_account") {
		return false
	}
	if subject.Reconciliation {
		return contains(decision.MissingFacts, "transfer_relationship")
	}
	switch subject.transactionType(decision) {
	case "EXPENSE", "INCOME", "TRANSFER", "UNCLASSIFIED":
		return true
	}
	return false
}

func engineOwnsDecision(decision reviewdec.Decision, reviewType string, subject reviewSubject) bool {
	if payslipReviewType(reviewType) {
		return engineOwnsPayslip(decision)
	}
	if reviewType == cycleReviewType {
		return true
	}
	if transferReview(reviewType) {
		return engineOwnsTransfer(decision, subject)
	}
	if !engineReviewType(reviewType) || len(decision.MissingFacts) == 0 {
		return false
	}
	switch subject.transactionType(decision) {
	case "INCOME":
		return isCategoryOnly(decision)
	case "TRANSFER", "UNCLASSIFIED":
		return false
	}
	if decision.InteractionMode == reviewdec.ModeConflictResolution || (decision.Provenance != nil && decision.Provenance["evidence_link_dispute"] == true) {
		return false
	}
	for _, fact := range decision.MissingFacts {
		if fact != "merchant" && fact != "category" {
			return false
		}
	}
	return true
}

func deriveStep(decision reviewdec.Decision, reviewType, facts string, subject reviewSubject) (Step, bool) {
	if !engineOwnsDecision(decision, reviewType, subject) {
		return Step{}, false
	}
	step := Step{Facts: strings.TrimSpace(facts), Why: reviewdec.WhyLine(decision)}
	if payslipReviewType(reviewType) {
		return derivePayslipStep(step, decision, reviewType), true
	}
	if reviewType == cycleReviewType {
		return deriveCycleStep(step, subject), true
	}
	if transferReview(reviewType) {
		return deriveTransferStep(step, decision, subject), true
	}
	if subject.transactionType(decision) == "INCOME" {
		return incomeConfirmStep(step), true
	}
	if contains(decision.MissingFacts, "merchant") {
		step.Kind, step.Fact, step.State = stepFact, factMerchant, "AWAITING_MERCHANT"
		step.Ask, step.Hint = "Ini bayar ke mana?", "Ketik nama toko atau merchant-nya."
		row := []InlineKeyboardButton{{Text: "Abaikan", CallbackData: callbackIgnore}}
		if merchantAssetOption(decision, reviewType) {
			row = []InlineKeyboardButton{{Text: "Beli aset", CallbackData: callbackAsset}, row[0]}
		}
		step.Rows = [][]InlineKeyboardButton{row}
		return step, true
	}
	step.Kind, step.Fact, step.State = stepChoice, factCategory, "AWAITING_CATEGORY"
	step.Ask, step.Hint = "Masuk kategori apa?", categoryHint
	return step, true
}

func stepForBinding(card agentReviewBinding) (Step, bool) {
	if step, ok := subjectStepForBinding(card); ok {
		return step, true
	}
	if card.Kind != "TRANSACTION" || !engineOwnedType(card.ReviewType) {
		return Step{}, false
	}
	if card.ConversationState == "AWAITING_ASSET_WEALTH" {
		return Step{Kind: stepChoice, Fact: factAsset}, true
	}
	if transferReview(card.ReviewType) {
		return transferStepForState(card.ConversationState), true
	}
	switch {
	case card.ConversationState == "AWAITING_MERCHANT_DECISION":
		return Step{Kind: stepConfirm, Fact: factRemember}, true
	case card.ConversationState == "AWAITING_MERCHANT", card.ReviewType == "UNKNOWN_MERCHANT" && card.MerchantID == "":
		return Step{Kind: stepFact, Fact: factMerchant}, true
	case card.ConversationState == "AWAITING_CATEGORY":
		return Step{Kind: stepChoice, Fact: factCategory}, true
	}
	return Step{}, false
}

func rememberStep(facts, merchant, category string) Step {
	return Step{
		Kind: stepConfirm, Fact: factRemember, Facts: facts,
		Ack:  "✅ Dicatat: " + category + ".",
		Ask:  "Pakai " + category + " juga untuk transaksi berikutnya dari " + merchant + "?",
		Hint: "Pilih Ya, ingat atau Sekali ini.",
		Rows: [][]InlineKeyboardButton{{{Text: "Ya, ingat", CallbackData: callbackRemember}, {Text: "Sekali ini", CallbackData: callbackOnce}}},
	}
}

func doneStep(merchant, category, amount, note string) Step {
	parts := []string{}
	for _, part := range []string{merchant, category} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	parts = append(parts, "Rp"+FormatIDR(amount))
	return Step{Kind: stepDone, Ack: "✅ Tersimpan: " + strings.Join(parts, " · "), Hint: note}
}

func ignoredStep(merchant, amount string) Step {
	label := "Rp" + FormatIDR(amount)
	if merchant != "" {
		label = merchant + " · " + label
	}
	return Step{Kind: stepDone, Ack: "🗑️ Diabaikan: " + label + ". Tidak dicatat."}
}

func (s *Step) loadCategoryRows(ctx context.Context, tx pgx.Tx, reviewID string, page int) error {
	chooser, err := loadCategoryPage(ctx, tx, reviewID, page)
	if err != nil {
		return err
	}
	if page > 0 && len(chooser.Items) == 0 {
		if chooser, err = loadCategoryPage(ctx, tx, reviewID, 0); err != nil {
			return err
		}
	}
	s.Page = chooser.Page
	s.Hint = categoryHint
	if s.Page > 0 {
		s.Hint = fmt.Sprintf("Pilih di bawah, atau ketik nama kategorinya (halaman %d).", s.Page+1)
	}
	tail := []InlineKeyboardButton{{Text: "Abaikan", CallbackData: callbackIgnore}}
	if chooser.AssetPurchase {
		tail = []InlineKeyboardButton{{Text: "Beli aset", CallbackData: callbackAsset}, tail[0]}
	}
	if s.Back {
		tail = append([]InlineKeyboardButton{{Text: "Kembali", CallbackData: callbackBack}}, tail...)
	}
	s.Rows = append(chooser.keyboard(), tail)
	return nil
}

func (s Step) executable(actions []string) Step {
	if s.Kind == stepDone || s.Fact == factRemember {
		return s
	}
	rows := s.Rows
	if cycleFact(s.Fact) {
		rows = cycleExecutableRows(rows, actions)
	}
	kept := keepExecutableButtons(&InlineKeyboardMarkup{InlineKeyboard: rows}, actions)
	s.Rows = nil
	if kept != nil {
		s.Rows = kept.InlineKeyboard
	}
	return s
}

func stepRenderCard(step Step) reviewCard {
	return reviewCard{State: step.State, Mode: "step", Title: step.Title(), Facts: step.Facts, Why: step.Why, Instruction: step.Hint}
}

func stepMarkup(ctx context.Context, tx pgx.Tx, reviewID, reviewType string, decision reviewdec.Decision, subject reviewSubject) (*InlineKeyboardMarkup, error) {
	step, ok := deriveStep(decision, reviewType, "", subject)
	if !ok {
		return nil, fmt.Errorf("review %s is not a step card", reviewType)
	}
	if err := step.loadRows(ctx, tx, reviewID, 0); err != nil {
		return nil, err
	}
	return step.executable(cardActions(decision, reviewType)).Markup(), nil
}

func loadedStepCard(ctx context.Context, tx pgx.Tx, reviewID, reviewType, facts string, decision reviewdec.Decision, subject reviewSubject, card reviewCard) (reviewCard, error) {
	if card.Mode != "step" {
		return card, nil
	}
	step, ok := deriveStep(decision, reviewType, facts, subject)
	if !ok {
		return card, nil
	}
	if err := step.loadRows(ctx, tx, reviewID, 0); err != nil {
		return card, err
	}
	return stepRenderCard(step.executable(cardActions(decision, reviewType))), nil
}
