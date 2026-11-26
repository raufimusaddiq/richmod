package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

const (
	missingAmountReviewType = "MISSING_AMOUNT"
	amountAsk               = "Berapa nominalnya?"
	amountHint              = "Ketik nominalnya, misalnya 54rb, 54.000 atau 1,5jt."
	amountBadAck            = "Nominalnya belum terbaca."
	amountClosedAck         = "Kartu ini sudah berubah, jadi belum ada yang dicatat. Lanjutkan di " + reviewInboxName + " di aplikasi."
	proposalIncomeHint      = "Pilih di bawah."
	minAnswerAmount         = 500
	maxStepAmount           = 1_000_000_000_000
)

var amountFillerWords = map[string]bool{
	"nominal": true, "nominalnya": true, "jumlah": true, "jumlahnya": true, "sebesar": true, "sekitar": true, "idr": true,
}

func parseAmountText(text string) (*big.Int, bool) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	if len(fields) > 1 && (fields[0] == "rp" || fields[0] == "rp.") {
		fields = fields[1:]
	}
	if len(fields) == 0 || len(fields) > 2 {
		return nil, false
	}
	match := amountToken.FindStringSubmatch(fields[0])
	if match == nil {
		return nil, false
	}
	digits, unit := match[1], amountUnits[match[2]]
	if len(fields) == 2 {
		external, known := amountUnits[fields[1]]
		if !known || unit != "" {
			return nil, false
		}
		unit = external
	}
	multiplier := int64(1)
	switch unit {
	case "":
		if strings.ContainsAny(digits, ".,") && !amountThousands.MatchString(digits) {
			return nil, false
		}
		digits = strings.NewReplacer(".", "", ",", "").Replace(digits)
	case "rb":
		multiplier = 1_000
	case "jt":
		multiplier = 1_000_000
	}
	if unit != "" {
		if strings.Count(digits, ".")+strings.Count(digits, ",") > 1 {
			return nil, false
		}
		digits = strings.Replace(digits, ",", ".", 1)
	}
	number, ok := new(big.Rat).SetString(digits)
	if !ok {
		return nil, false
	}
	number.Mul(number, big.NewRat(multiplier, 1))
	if !number.IsInt() || number.Sign() <= 0 {
		return nil, false
	}
	return new(big.Int).Set(number.Num()), true
}

func parseAnswerAmount(text string) (*big.Int, bool) {
	fields := strings.Fields(strings.ToLower(text))
	kept := fields[:0:0]
	for _, field := range fields {
		if !amountFillerWords[strings.Trim(field, ".,:")] {
			kept = append(kept, field)
		}
	}
	value, ok := parseAmountText(strings.Join(kept, " "))
	if !ok {
		digits, lenient := parseBankFactsAnswer(text)
		if !lenient {
			return nil, false
		}
		value, ok = new(big.Int).SetString(digits, 10)
		if !ok {
			return nil, false
		}
	}
	if value.Cmp(big.NewInt(minAnswerAmount)) < 0 || value.Cmp(big.NewInt(maxStepAmount)) > 0 {
		return nil, false
	}
	return value, true
}

func amountStepFrom(base Step, actions []string) Step {
	base.Kind, base.Fact, base.State, base.Back = stepFact, factAmount, "AWAITING_AMOUNT", false
	base.Ask, base.Hint = amountAsk, amountHint
	base.Rows = nil
	if contains(actions, "IGNORE") {
		base.Rows = [][]InlineKeyboardButton{{{Text: "Abaikan", CallbackData: callbackIgnore}}}
	}
	return base
}

func proposalIncomeStep(base Step, amount string, actions []string) Step {
	base.Kind, base.Fact, base.State, base.Back = stepChoice, factProposalIncome, "AWAITING_CONFIRMATION", false
	base.Ask, base.Hint = IDR(amount)+" ini dicatat sebagai apa?", proposalIncomeHint
	var buttons []InlineKeyboardButton
	if contains(actions, "SET_AMOUNT") {
		buttons = append(buttons, InlineKeyboardButton{Text: "Penghasilan", CallbackData: callbackIncome})
	}
	if contains(actions, "IGNORE") {
		buttons = append(buttons, InlineKeyboardButton{Text: "Transfer sendiri", CallbackData: callbackOwn}, InlineKeyboardButton{Text: "Abaikan", CallbackData: callbackIgnore})
	}
	base.Rows = pairRows(buttons)
	return base
}

func deriveMissingAmountStep(step Step, decision reviewdec.Decision) Step {
	return amountStepFrom(step, cardActions(decision, missingAmountReviewType))
}

func loadAmountPending(ctx context.Context, tx pgx.Tx, requestID string) (string, error) {
	var amount string
	err := tx.QueryRow(ctx, `SELECT COALESCE(context_json->>'amount_pending',context_json->>'amount_idr','') FROM review_conversation WHERE review_request_id=$1`, requestID).Scan(&amount)
	return amount, ignoreNoRows(err)
}

func (p *Processor) proposalAmountStep(ctx context.Context, tx pgx.Tx, e engineReview, base Step) (Step, error) {
	if e.State != "AWAITING_CONFIRMATION" || e.Subject.ProposalType != "INCOME" {
		return base, nil
	}
	amount, err := loadAmountPending(ctx, tx, e.RequestID)
	if err != nil || amount == "" {
		return base, err
	}
	return proposalIncomeStep(base, amount, cardActions(e.Decision, e.ReviewType)), nil
}

func (p *Processor) applyAmountText(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, text string) (stepOutcome, error) {
	amount, ok := parseAnswerAmount(text)
	if !ok || isYesNoAnswer(text) {
		return reask(step, amountBadAck), nil
	}
	if review.ReviewType == bankReviewType {
		return p.saveBankAmount(ctx, tx, householdID, sourceEventID, review, step, amount.String())
	}
	return p.saveProposalAmount(ctx, tx, householdID, sourceEventID, review, step, amount.String())
}

func (p *Processor) saveProposalAmount(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, amount string) (stepOutcome, error) {
	if review.Subject.ProposalType == "INCOME" {
		if _, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json||jsonb_build_object('amount_pending',$2::text),state='AWAITING_CONFIRMATION',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.RequestID, amount); err != nil {
			return stepOutcome{}, err
		}
		next := proposalIncomeStep(step, amount, cardActions(review.Decision, review.ReviewType))
		return stepOutcome{Step: next.WithAck("✅ Nominal " + IDR(amount) + " diterima."), Changed: true}, nil
	}
	return p.resolveProposalAmount(ctx, tx, householdID, sourceEventID, review, step, amount, false)
}

func (p *Processor) resolveProposalAmount(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, amount string, income bool) (stepOutcome, error) {
	refs, err := loadPayslipRefs(ctx, tx, householdID, review.ItemID)
	if err != nil {
		return stepOutcome{}, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	result, err := reviewdomain.ResolveMissingAmountProposal(ctx, sp, reviewdomain.MissingAmountCommand{
		HouseholdID: householdID, UserID: review.UserID, ReviewItemID: review.ItemID, ProposalID: refs.ProposalID,
		SourceEventID: refs.SourceEventID, ActorType: "TELEGRAM", AmountIDR: &amount, IncomeConfirmed: income,
	})
	if errors.Is(err, reviewdomain.ErrMissingAmountReviewInvalid) {
		_ = sp.Rollback(ctx)
		return stepOutcome{Step: Step{Kind: stepDone, Ack: amountClosedAck}, Settled: true, Changed: true}, nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'field','amount')) ON CONFLICT DO NOTHING`, result.TransactionID, refs.SourceEventID, review.RequestID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `UPDATE review_conversation SET context_json=context_json-'amount_pending' WHERE review_request_id=$1`, review.RequestID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	if income {
		return stepOutcome{Step: Step{Kind: stepDone, Ack: savedLine("Penghasilan", IDR(amount))}, Settled: true, Changed: true}, nil
	}
	outcome, err := confirmedCardOutcome(ctx, tx, householdID, result.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: doneStep(outcome.Merchant, outcome.Category, amount, ""), Settled: true, Changed: true}, nil
}

func (p *Processor) applyProposalAmountTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	if !stepOffers(step, data) {
		return reask(step, choiceStaleAck), nil
	}
	switch {
	case data == callbackIgnore || (data == callbackOwn && step.Fact == factProposalIncome):
		return p.ignoreProposalAmount(ctx, tx, householdID, review, step)
	case data == callbackIncome && step.Fact == factProposalIncome:
		amount, err := loadAmountPending(ctx, tx, review.RequestID)
		if err != nil {
			return stepOutcome{}, err
		}
		if amount == "" {
			return p.backToFirstStep(ctx, tx, householdID, review)
		}
		return p.resolveProposalAmount(ctx, tx, householdID, sourceEventID, review, step, amount, true)
	}
	return reask(step, choiceStaleAck), nil
}

func (p *Processor) applyProposalIncomeText(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, text string) (stepOutcome, error) {
	switch incomeReviewChoice(text) {
	case "CONFIRM":
		return p.applyProposalAmountTap(ctx, tx, householdID, sourceEventID, review, step, callbackIncome)
	case "REJECT":
		return p.applyProposalAmountTap(ctx, tx, householdID, sourceEventID, review, step, callbackOwn)
	}
	return reask(step, unusableAnswerAck), nil
}

func (p *Processor) ignoreProposalAmount(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step) (stepOutcome, error) {
	refs, err := loadPayslipRefs(ctx, tx, householdID, review.ItemID)
	if err != nil {
		return stepOutcome{}, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	err = reviewdomain.IgnoreMissingAmountProposal(ctx, sp, reviewdomain.MissingAmountCommand{
		HouseholdID: householdID, UserID: review.UserID, ReviewItemID: review.ItemID, ProposalID: refs.ProposalID,
		SourceEventID: refs.SourceEventID, ActorType: "TELEGRAM",
	})
	if errors.Is(err, reviewdomain.ErrMissingAmountReviewInvalid) {
		_ = sp.Rollback(ctx)
		return stepOutcome{Step: Step{Kind: stepDone, Ack: amountClosedAck}, Settled: true, Changed: true}, nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `UPDATE review_conversation SET context_json=context_json-'amount_pending' WHERE review_request_id=$1`, review.RequestID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: Step{Kind: stepDone, Ack: ignoredLine}, Settled: true, Changed: true}, nil
}

type bankPending struct {
	AmountIDR     string `json:"amount_idr"`
	TransactionAt string `json:"transaction_at"`
	Queued        bool   `json:"queued"`
}

func loadBankPending(ctx context.Context, tx pgx.Tx, requestID string) (bankPending, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT COALESCE(context_json->'bank_pending','{}'::jsonb) FROM review_conversation WHERE review_request_id=$1`, requestID).Scan(&raw); err != nil {
		return bankPending{}, ignoreNoRows(err)
	}
	var pending bankPending
	if err := json.Unmarshal(raw, &pending); err != nil {
		return bankPending{}, err
	}
	return pending, nil
}

func saveBankPending(ctx context.Context, tx pgx.Tx, requestID string, pending bankPending) error {
	encoded, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json||jsonb_build_object('bank_pending',$2::jsonb),last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, requestID, string(encoded))
	return err
}
