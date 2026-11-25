package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

const (
	salaryAsk         = "Slip gaji ini dicatat sebagai apa?"
	payDateAsk        = "Tanggal pembayarannya kapan?"
	salaryChoiceHint  = "Pilih di bawah."
	dateHint          = "Pilih di bawah, atau ketik tanggalnya."
	dateOtherHint     = "Ketik tanggalnya, misalnya 3 Okt atau 25/9."
	futureDateAck     = "Tanggal itu belum terjadi."
	salaryInvalidAck  = "Pilihan itu tidak berlaku untuk slip ini."
	salaryPolicyAck   = "✅ Jenis gaji disimpan."
	salaryEvidenceKey = "salary_classification"
	dateEvidenceKey   = "transaction_at"
)

type salaryOption struct {
	Action string
	Label  string
	Data   string
}

var salaryOptions = []salaryOption{
	{"HOUSEHOLD_POLICY", "Catat sebagai gaji", callbackSalaryHousehold},
	{"PRIMARY_SALARY", "Gaji utama", callbackSalaryPrimary},
	{"ORDINARY_INCOME", "Pemasukan biasa", callbackSalaryOrdinary},
}

func payslipReviewType(reviewType string) bool {
	return reviewType == "PAYSLIP_CONFIRMATION" || reviewType == "MISSING_PAY_DATE"
}

func engineOwnsPayslip(decision reviewdec.Decision) bool {
	if len(decision.MissingFacts) == 0 || (decision.Provenance != nil && decision.Provenance["evidence_link_dispute"] == true) {
		return false
	}
	for _, fact := range decision.MissingFacts {
		if fact != "transaction_at" && fact != "salary_classification" {
			return false
		}
	}
	return true
}

func derivePayslipStep(step Step, decision reviewdec.Decision, reviewType string) Step {
	actions := cardActions(decision, reviewType)
	if contains(decision.MissingFacts, "salary_classification") {
		step.Kind, step.Fact, step.State = stepChoice, factSalary, "AWAITING_DETAIL"
		step.Ask, step.Hint = salaryAsk, salaryChoiceHint
		step.Rows = salaryChoiceRows(actions)
		return step
	}
	return dateStepFrom(step, payDateAsk, actions)
}

func salaryChoiceRows(actions []string) [][]InlineKeyboardButton {
	var buttons []InlineKeyboardButton
	for _, option := range salaryOptions {
		if contains(actions, option.Action) {
			buttons = append(buttons, InlineKeyboardButton{Text: option.Label, CallbackData: option.Data})
		}
	}
	if contains(actions, "IGNORE") {
		buttons = append(buttons, InlineKeyboardButton{Text: "Abaikan", CallbackData: callbackIgnore})
	}
	return pairRows(buttons)
}

func pairRows(buttons []InlineKeyboardButton) [][]InlineKeyboardButton {
	var rows [][]InlineKeyboardButton
	for len(buttons) > 0 {
		take := min(2, len(buttons))
		rows = append(rows, buttons[:take])
		buttons = buttons[take:]
	}
	return rows
}

func dateStepFrom(base Step, ask string, actions []string, extra ...InlineKeyboardButton) Step {
	base.Kind, base.Fact, base.State, base.Back = stepFact, factDate, "AWAITING_DATE", false
	base.Ask, base.Hint = ask, dateHint
	buttons := []InlineKeyboardButton{{Text: "Hari ini", CallbackData: callbackDateToday}, {Text: "Kemarin", CallbackData: callbackDateYesterday}}
	buttons = append(buttons, extra...)
	buttons = append(buttons, InlineKeyboardButton{Text: "Tanggal lain", CallbackData: callbackDateOther})
	if contains(actions, "IGNORE") {
		buttons = append(buttons, InlineKeyboardButton{Text: "Abaikan", CallbackData: callbackIgnore})
	}
	base.Rows = pairRows(buttons)
	return base
}

func subjectStepForBinding(card agentReviewBinding) (Step, bool) {
	switch {
	case card.Kind == cardKindProposal && payslipReviewType(card.ReviewType):
		if card.ConversationState == "AWAITING_DATE" {
			return Step{Kind: stepFact, Fact: factDate}, true
		}
		return Step{Kind: stepChoice, Fact: factSalary}, true
	case card.Kind == "CYCLE_RESIDUAL" && card.ReviewType == cycleReviewType:
		switch card.ConversationState {
		case "AWAITING_CYCLE_ACCOUNT":
			return Step{Kind: stepChoice, Fact: factSplitAcct}, true
		case "AWAITING_CYCLE_AMOUNT":
			return Step{Kind: stepFact, Fact: factSplitAmount}, true
		}
		return Step{Kind: stepChoice, Fact: factCycle}, true
	}
	return Step{}, false
}

var shortMonths = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

func payslipPeriodLabel(period string) string {
	parsed, err := time.Parse("2006-01", period)
	if err != nil {
		return ""
	}
	return shortMonths[parsed.Month()-1] + " " + parsed.Format("2006")
}

func payslipSummary(ctx context.Context, tx pgx.Tx, householdID, proposalID string, dateKnown bool) (string, error) {
	var employer, amount, period string
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT COALESCE(counterparty_raw,''),round(amount)::text,COALESCE(metadata_json->>'period',''),transaction_at FROM transaction_proposal WHERE id=$1 AND household_id=$2`, proposalID, householdID).Scan(&employer, &amount, &period, &at)
	if err != nil {
		return "", ignoreNoRows(err)
	}
	var lines []string
	if employer != "" {
		lines = append(lines, "Pemberi kerja: "+clean(employer, 200))
	}
	lines = append(lines, "Gaji bersih: Rp"+FormatIDR(amount))
	if label := payslipPeriodLabel(period); label != "" {
		lines = append(lines, "Periode: "+label)
	}
	if dateKnown {
		lines = append(lines, "Tanggal bayar: "+reviewdomain.FormatIndonesianDate(at.In(jakartaLocation())))
	}
	return strings.Join(lines, "\n"), nil
}

type payslipRefs struct {
	ProposalID, SourceEventID, DocumentID string
}

func loadPayslipRefs(ctx context.Context, tx pgx.Tx, householdID, itemID string) (payslipRefs, error) {
	var refs payslipRefs
	err := tx.QueryRow(ctx, `SELECT ri.proposal_id::text,COALESCE(ri.source_event_id,p.source_event_id)::text,COALESCE(COALESCE(ri.document_id,NULLIF(p.metadata_json->>'document_id','')::uuid)::text,'')
		FROM review_item ri JOIN transaction_proposal p ON p.id=ri.proposal_id AND p.household_id=ri.household_id
		WHERE ri.id=$1 AND ri.household_id=$2`, itemID, householdID).Scan(&refs.ProposalID, &refs.SourceEventID, &refs.DocumentID)
	return refs, err
}

func choiceForSalaryData(data string) string {
	for _, option := range salaryOptions {
		if option.Data == data {
			return option.Action
		}
	}
	return ""
}

func labelForSalaryAction(action string) string {
	for _, option := range salaryOptions {
		if option.Action == action {
			return option.Label
		}
	}
	return ""
}

func knownSalaryChoice(decision reviewdec.Decision) string {
	if choice, ok := decision.KnownFacts["salary_classification"].(string); ok {
		return choice
	}
	return ""
}

func (p *Processor) applySalaryTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	if !stepOffers(step, data) {
		return reask(step, salaryInvalidAck), nil
	}
	switch {
	case data == callbackIgnore:
		return p.ignorePayslip(ctx, tx, householdID, review, step)
	case step.Fact == factSalary:
		return p.chooseSalary(ctx, tx, householdID, sourceEventID, review, step, choiceForSalaryData(data))
	case step.Fact == factDate:
		return p.applyDateTap(ctx, tx, householdID, sourceEventID, review, step, data)
	}
	return reask(step, salaryInvalidAck), nil
}

func (p *Processor) applySalaryText(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, update telegramUpdate, review engineReview, step Step, text string) (stepOutcome, error) {
	if step.Fact != factDate {
		return reask(step, unusableAnswerAck), nil
	}
	return p.applyDateText(ctx, tx, householdID, sourceEventID, p.turnTime(update), review, step, text)
}

func (p *Processor) chooseSalary(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, choice string) (stepOutcome, error) {
	refs, err := loadPayslipRefs(ctx, tx, householdID, review.ItemID)
	if err != nil {
		return stepOutcome{}, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	if review.ReviewType == "MISSING_PAY_DATE" && contains(review.Decision.MissingFacts, "transaction_at") {
		err = reviewdomain.SetPayslipPolicy(ctx, sp, reviewdomain.PayslipPolicyCommand{
			HouseholdID: householdID, UserID: review.UserID, ActorType: "TELEGRAM", ReviewItemID: review.ItemID,
			ProposalID: refs.ProposalID, SourceEventID: refs.SourceEventID, DocumentID: refs.DocumentID,
			ReplySourceEventID: sourceEventID, Choice: choice,
		})
		if errors.Is(err, reviewdomain.ErrPayslipReviewInvalid) {
			_ = sp.Rollback(ctx)
			return reask(step, salaryInvalidAck), nil
		}
		if err != nil {
			_ = sp.Rollback(ctx)
			return stepOutcome{}, err
		}
		if err := sp.Commit(ctx); err != nil {
			return stepOutcome{}, err
		}
		outcome, err := p.reloadedStep(ctx, tx, householdID, review)
		if err != nil {
			return stepOutcome{}, err
		}
		outcome.Step = outcome.Step.WithAck(salaryPolicyAck)
		return outcome, nil
	}
	result, err := reviewdomain.ResolvePayslipProposal(ctx, sp, reviewdomain.PayslipCommand{
		HouseholdID: householdID, UserID: review.UserID, ReviewItemID: review.ItemID, ProposalID: refs.ProposalID,
		SourceEventID: refs.SourceEventID, DocumentID: refs.DocumentID, ActorType: "TELEGRAM", Action: choice, Choice: choice,
	})
	return p.finishPayslip(ctx, tx, sp, householdID, sourceEventID, review, step, result, err, salaryEvidenceKey)
}

func (p *Processor) finishPayslip(ctx context.Context, tx, sp pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, result reviewdomain.PayslipResult, resolveErr error, field string) (stepOutcome, error) {
	if errors.Is(resolveErr, reviewdomain.ErrPayslipReviewInvalid) {
		_ = sp.Rollback(ctx)
		return reask(step, salaryInvalidAck), nil
	}
	if resolveErr != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, resolveErr
	}
	classification := ""
	if field == salaryEvidenceKey {
		classification = result.Choice
	}
	if _, err := sp.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_strip_nulls(jsonb_build_object('review_request_id',$3::uuid,'field',$4::text,'classification',NULLIF($5::text,'')))) ON CONFLICT DO NOTHING`, result.TransactionID, sourceEventID, review.RequestID, field, classification); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	var amount string
	var at time.Time
	if err := tx.QueryRow(ctx, `SELECT round(amount)::text,transaction_at FROM transaction WHERE id=$1`, result.TransactionID).Scan(&amount, &at); err != nil {
		return stepOutcome{}, err
	}
	startsCycle := false
	if result.Choice != "ORDINARY_INCOME" {
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id WHERE se.transaction_id=$1 AND se.household_id=$2 AND se.status='CONFIRMED' AND ss.active AND ss.is_primary)`, result.TransactionID, householdID).Scan(&startsCycle); err != nil {
			return stepOutcome{}, err
		}
	}
	return stepOutcome{Step: payslipDoneStep(result.Choice, amount, at, startsCycle), Settled: true, Changed: true}, nil
}

func payslipDoneStep(choice, amount string, at time.Time, startsCycle bool) Step {
	if choice == "ORDINARY_INCOME" {
		return Step{Kind: stepDone, Ack: "✅ Pemasukan tercatat · Rp" + FormatIDR(amount)}
	}
	if !startsCycle {
		return Step{Kind: stepDone, Ack: "✅ Gaji tercatat · Rp" + FormatIDR(amount) + "."}
	}
	local := at.In(jakartaLocation())
	return Step{Kind: stepDone, Ack: "✅ Gaji tercatat · Rp" + FormatIDR(amount) + ". Siklus baru mulai " + reviewdomain.FormatIndonesianShortDate(local) + "."}
}

func (p *Processor) ignorePayslip(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step) (stepOutcome, error) {
	refs, err := loadPayslipRefs(ctx, tx, householdID, review.ItemID)
	if err != nil {
		return stepOutcome{}, err
	}
	var amount, employer string
	if err := tx.QueryRow(ctx, `SELECT round(amount)::text,COALESCE(counterparty_raw,'') FROM transaction_proposal WHERE id=$1 AND household_id=$2`, refs.ProposalID, householdID).Scan(&amount, &employer); err != nil {
		return stepOutcome{}, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	_, err = reviewdomain.ResolvePayslipProposal(ctx, sp, reviewdomain.PayslipCommand{
		HouseholdID: householdID, UserID: review.UserID, ReviewItemID: review.ItemID, ProposalID: refs.ProposalID,
		SourceEventID: refs.SourceEventID, DocumentID: refs.DocumentID, ActorType: "TELEGRAM", Action: "IGNORE",
	})
	if errors.Is(err, reviewdomain.ErrPayslipReviewInvalid) {
		_ = sp.Rollback(ctx)
		return reask(step, salaryInvalidAck), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: ignoredStep(clean(employer, 60), amount), Settled: true, Changed: true}, nil
}

func jakartaMidnight(at time.Time) time.Time {
	local := at.In(jakartaLocation())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, jakartaLocation())
}

func (p *Processor) applyDateTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	today := jakartaMidnight(p.now())
	switch data {
	case callbackDateToday:
		return p.finishPayDate(ctx, tx, householdID, sourceEventID, review, step, today)
	case callbackDateYesterday:
		return p.finishPayDate(ctx, tx, householdID, sourceEventID, review, step, today.AddDate(0, 0, -1))
	case callbackDateOther:
		step.Hint = dateOtherHint
		return stepOutcome{Step: step, Changed: true}, nil
	}
	return reask(step, salaryInvalidAck), nil
}

func (p *Processor) applyDateText(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, now time.Time, review engineReview, step Step, text string) (stepOutcome, error) {
	date, ok := parseDateValue(text, now, 0)
	if !ok {
		return reask(step, unusableAnswerAck), nil
	}
	if date.After(jakartaMidnight(now)) {
		return stepOutcome{Step: step.WithAck(futureDateAck)}, nil
	}
	return p.finishPayDate(ctx, tx, householdID, sourceEventID, review, step, date)
}

func (p *Processor) finishPayDate(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, date time.Time) (stepOutcome, error) {
	refs, err := loadPayslipRefs(ctx, tx, householdID, review.ItemID)
	if err != nil {
		return stepOutcome{}, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	result, resolveErr := reviewdomain.ResolvePayslipProposal(ctx, sp, reviewdomain.PayslipCommand{
		HouseholdID: householdID, UserID: review.UserID, ReviewItemID: review.ItemID, ProposalID: refs.ProposalID,
		SourceEventID: refs.SourceEventID, DocumentID: refs.DocumentID, ActorType: "TELEGRAM", Action: "SET_PAY_DATE",
		Choice: knownSalaryChoice(review.Decision), PayDate: &date,
	})
	return p.finishPayslip(ctx, tx, sp, householdID, sourceEventID, review, step, result, resolveErr, dateEvidenceKey)
}

func payslipItemSummary(ctx context.Context, tx pgx.Tx, householdID, itemID, proposalID string) (string, bool, error) {
	var reviewType string
	var missing []byte
	if err := tx.QueryRow(ctx, `SELECT review_type,COALESCE(decision->'missingFacts','[]'::jsonb) FROM review_item WHERE id=$1 AND household_id=$2`, itemID, householdID).Scan(&reviewType, &missing); err != nil {
		return "", false, ignoreNoRows(err)
	}
	if !payslipReviewType(reviewType) {
		return "", false, nil
	}
	var facts []string
	if err := json.Unmarshal(missing, &facts); err != nil {
		return "", false, err
	}
	summary, err := payslipSummary(ctx, tx, householdID, proposalID, !contains(facts, "transaction_at"))
	return summary, true, err
}

func (p *Processor) answerRedirectedPayslip(ctx context.Context, householdID, sourceEventID string, target telegramUpdate, text string) (bool, error) {
	redirected, err := p.resolveReply(ctx, householdID, target)
	if err != nil || redirected.Kind != interactionCardStep || !payslipReviewType(redirected.Card.ReviewType) {
		return false, err
	}
	input := engineInput{Text: text, Targeted: true}
	if redirected.ButtonData != "" {
		input = engineInput{Tap: redirected.ButtonData, Targeted: true}
	}
	handled, err := p.runStepEngine(ctx, householdID, sourceEventID, redirected.Update, redirected.Card, input)
	if handled {
		p.recordInteraction(ctx, householdID, sourceEventID, redirected.Card.ReviewItemID, interactionAnswered, redirected.Source)
	}
	return handled, err
}
