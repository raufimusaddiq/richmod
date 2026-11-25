package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

const (
	cycleReviewType       = "CYCLE_RESIDUAL_ALLOCATION"
	cycleAskFallback      = "Sisa siklus belum dialokasikan. Mau dialokasikan ke mana?"
	noCycleAccountsHint   = "Belum ada akun tabungan atau investasi yang aktif. Tambahkan dulu di halaman Kekayaan, atau pilih Biarkan saja."
	cycleInvalidAck       = "Pilihan itu tidak berlaku untuk sisa ini."
	cycleChangedAck       = "Sisa siklus berubah. Pilih lagi."
	cycleClosedAck        = "Sisa siklus sudah tidak ada lagi. Tinjauan ditutup."
	cycleMissingAck       = "📝 Ketik transaksi yang belum tercatat, misalnya 'makan 25rb kemarin'. Sisa siklus akan dihitung ulang."
	splitAmountHint       = "Pilih Semua sisa, atau ketik jumlahnya, misalnya 500rb atau 1,5jt."
	splitAmountBadAck     = "Jumlahnya belum terbaca."
	splitNoAccountsHint   = "Tidak ada akun lain yang tersisa. Pilih Batal untuk mulai lagi."
	cycleResidualAuditSQL = `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1::uuid,'TELEGRAM',$2::uuid,$3::text,'cycle_residual_case',$4::uuid,jsonb_build_object('residualIdr',$5::text))`
)

type cycleSubject struct {
	Start, End time.Time
	Residual   string
}

type cycleSplitAllocation struct {
	AccountID string `json:"account_id"`
	Amount    string `json:"amount_idr"`
}

type cycleSplit struct {
	Allocations []cycleSplitAllocation `json:"allocations"`
	Pending     string                 `json:"pending"`
}

func cycleFact(fact factKind) bool {
	return fact == factCycle || fact == factSplitAcct || fact == factSplitAmount
}

func deriveCycleStep(step Step, subject reviewSubject) Step {
	step.Kind, step.Fact, step.State = stepChoice, factCycle, "AWAITING_DETAIL"
	step.Ask = cycleAskFallback
	if cycle := subject.Cycle; cycle != nil {
		step.Ask = reviewdomain.CycleResidualQuestion(cycle.Start, cycle.End, cycle.Residual)
	}
	step.Hint = reviewdomain.CycleResidualCardInstruction
	return step
}

func cycleExecutableRows(rows [][]InlineKeyboardButton, actions []string) [][]InlineKeyboardButton {
	var kept [][]InlineKeyboardButton
	for _, row := range rows {
		var keptRow []InlineKeyboardButton
		for _, button := range row {
			allowed := true
			switch {
			case button.CallbackData == callbackLeave:
				allowed = contains(actions, "LEAVE_UNALLOCATED")
			case button.CallbackData == callbackCycleMissing:
				allowed = contains(actions, "TRANSACTION_MISSING")
			case button.CallbackData == callbackCycleSplit || button.CallbackData == callbackCycleAll || strings.HasPrefix(button.CallbackData, callbackCycle):
				allowed = contains(actions, "ALLOCATE_RETAINED_BALANCE")
			}
			if allowed {
				keptRow = append(keptRow, button)
			}
		}
		if len(keptRow) > 0 {
			kept = append(kept, keptRow)
		}
	}
	return kept
}

func cycleAccountCandidates(ctx context.Context, tx pgx.Tx, householdID string, exclude []string) ([]accountChoice, error) {
	if exclude == nil {
		exclude = []string{}
	}
	rows, err := tx.Query(ctx, `SELECT id::text,name FROM wealth_account WHERE household_id=$1 AND active AND side='ASSET' AND usage_role IN ('SAVINGS','INVESTMENT') AND NOT (id::text=ANY($2::text[])) ORDER BY name,id`, householdID, exclude)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (accountChoice, error) {
		var choice accountChoice
		err := row.Scan(&choice.ID, &choice.Name)
		return choice, err
	})
}

func loadCycleSplit(ctx context.Context, tx pgx.Tx, requestID string) (cycleSplit, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT COALESCE(context_json->'cycle_split','{}'::jsonb) FROM review_conversation WHERE review_request_id=$1`, requestID).Scan(&raw); err != nil {
		return cycleSplit{}, ignoreNoRows(err)
	}
	var split cycleSplit
	if err := json.Unmarshal(raw, &split); err != nil {
		return cycleSplit{}, err
	}
	return split, nil
}

func (split cycleSplit) usedIDs() []string {
	ids := make([]string, 0, len(split.Allocations))
	for _, allocation := range split.Allocations {
		ids = append(ids, allocation.AccountID)
	}
	return ids
}

func (split cycleSplit) allocated() *big.Int {
	total := new(big.Int)
	for _, allocation := range split.Allocations {
		if amount, ok := new(big.Int).SetString(allocation.Amount, 10); ok {
			total.Add(total, amount)
		}
	}
	return total
}

func (split cycleSplit) remaining(residual string) *big.Int {
	whole, ok := new(big.Int).SetString(residual, 10)
	if !ok {
		return new(big.Int)
	}
	return whole.Sub(whole, split.allocated())
}

func saveCycleSplit(ctx context.Context, tx pgx.Tx, requestID, state string, split *cycleSplit) error {
	if split == nil {
		_, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json-'cycle_split',state=$2,last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, requestID, state)
		return err
	}
	encoded, err := json.Marshal(split)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json||jsonb_build_object('cycle_split',$3::jsonb),state=$2,last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, requestID, state, string(encoded))
	return err
}

func accountNames(ctx context.Context, tx pgx.Tx, householdID string, ids []string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT id::text,name FROM wealth_account WHERE household_id=$1 AND id::text=ANY($2::text[])`, householdID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}

func rupiah(value *big.Int) string { return "Rp" + FormatIDR(value.String()) }

func (p *Processor) cycleSplitStep(ctx context.Context, tx pgx.Tx, householdID string, e engineReview, base Step) (Step, error) {
	if e.Subject.Cycle == nil {
		return base, nil
	}
	split, err := loadCycleSplit(ctx, tx, e.RequestID)
	if err != nil {
		return Step{}, err
	}
	ids := append(split.usedIDs(), split.Pending)
	names, err := accountNames(ctx, tx, householdID, ids)
	if err != nil {
		return Step{}, err
	}
	remaining := split.remaining(e.Subject.Cycle.Residual)
	if remaining.Sign() <= 0 {
		if !e.ReadOnly {
			if err := saveCycleSplit(ctx, tx, e.RequestID, "AWAITING_DETAIL", nil); err != nil {
				return Step{}, err
			}
		}
		return base.WithAck(cycleChangedAck), nil
	}
	var lines []string
	for _, allocation := range split.Allocations {
		amount, _ := new(big.Int).SetString(allocation.Amount, 10)
		if amount != nil {
			lines = append(lines, "• "+clean(names[allocation.AccountID], 60)+": "+rupiah(amount))
		}
	}
	base.Facts = strings.Join(lines, "\n")
	base.Back = true
	if e.State == "AWAITING_CYCLE_AMOUNT" && split.Pending != "" {
		base.Kind, base.Fact, base.State = stepFact, factSplitAmount, "AWAITING_CYCLE_AMOUNT"
		base.Ask = "Berapa ke " + clean(names[split.Pending], 60) + "? Sisa " + rupiah(remaining)
		base.Hint = splitAmountHint
		return base, nil
	}
	base.Kind, base.Fact, base.State = stepChoice, factSplitAcct, "AWAITING_CYCLE_ACCOUNT"
	if len(split.Allocations) == 0 {
		base.Ask = "Bagi sisa " + rupiah(remaining) + ". Pilih akun pertama."
	} else {
		base.Ask = "Sisa " + rupiah(remaining) + " belum dibagi. Pilih akun berikutnya."
	}
	return base, nil
}

func (s *Step) loadCycleRows(ctx context.Context, tx pgx.Tx, reviewID string, page int) error {
	if s.Fact == factSplitAmount {
		s.Rows = [][]InlineKeyboardButton{{{Text: "Semua sisa", CallbackData: callbackCycleAll}}, {{Text: "Kembali", CallbackData: callbackBack}, {Text: "Batal", CallbackData: callbackCancel}}}
		return nil
	}
	var householdID string
	if err := tx.QueryRow(ctx, `SELECT household_id::text FROM review_request WHERE id=$1`, reviewID).Scan(&householdID); err != nil {
		return err
	}
	var exclude []string
	if s.Fact == factSplitAcct {
		split, err := loadCycleSplit(ctx, tx, reviewID)
		if err != nil {
			return err
		}
		exclude = split.usedIDs()
	}
	choices, err := cycleAccountCandidates(ctx, tx, householdID, exclude)
	if err != nil {
		return err
	}
	empty := noCycleAccountsHint
	if s.Fact == factSplitAcct {
		empty = splitNoAccountsHint
	}
	if err := s.loadStampedAccounts(ctx, tx, reviewID, choices, page, callbackCycle, callbackCyclePg, empty); err != nil {
		return err
	}
	if s.Fact == factSplitAcct {
		s.Rows = append(s.Rows, []InlineKeyboardButton{{Text: "Kembali", CallbackData: callbackBack}, {Text: "Batal", CallbackData: callbackCancel}})
		return nil
	}
	if len(choices) >= 2 {
		s.Rows = append(s.Rows, []InlineKeyboardButton{{Text: "Bagi ke beberapa akun", CallbackData: callbackCycleSplit}})
	}
	s.Rows = append(s.Rows, []InlineKeyboardButton{{Text: "Biarkan saja", CallbackData: callbackLeave}}, []InlineKeyboardButton{{Text: "Ada transaksi belum tercatat", CallbackData: callbackCycleMissing}})
	return nil
}

func (p *Processor) cycleCandidatesFor(ctx context.Context, tx pgx.Tx, householdID, requestID string, fact factKind) ([]accountChoice, error) {
	var exclude []string
	if fact == factSplitAcct {
		split, err := loadCycleSplit(ctx, tx, requestID)
		if err != nil {
			return nil, err
		}
		exclude = split.usedIDs()
	}
	return cycleAccountCandidates(ctx, tx, householdID, exclude)
}

func (p *Processor) applyCycleTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	if page, ok := parsePageCallback(data, callbackCyclePg); ok {
		if step.Fact != factCycle && step.Fact != factSplitAcct {
			return reask(step, cycleInvalidAck), nil
		}
		if err := step.loadRows(ctx, tx, review.RequestID, page); err != nil {
			return stepOutcome{}, err
		}
		return stepOutcome{Step: step.executable(cardActions(review.Decision, review.ReviewType))}, nil
	}
	if strings.HasPrefix(data, callbackCycle) && (step.Fact == factCycle || step.Fact == factSplitAcct) {
		current, err := p.cycleCandidatesFor(ctx, tx, householdID, review.RequestID, step.Fact)
		if err != nil {
			return stepOutcome{}, err
		}
		id, ok, err := resolveStampedAccount(ctx, tx, review.RequestID, data, callbackCycle, current)
		if err != nil {
			return stepOutcome{}, err
		}
		if !ok {
			return reask(step, accountStaleAck), nil
		}
		return p.chooseCycleAccount(ctx, tx, householdID, sourceEventID, review, step, id)
	}
	if !stepOffers(step, data) {
		return reask(step, cycleInvalidAck), nil
	}
	switch {
	case data == callbackCycleSplit:
		return p.cycleGo(ctx, tx, householdID, review, "AWAITING_CYCLE_ACCOUNT", &cycleSplit{}, "")
	case data == callbackLeave:
		return p.cycleSimpleAction(ctx, tx, householdID, review, step, "LEAVE_UNALLOCATED")
	case data == callbackCycleMissing:
		return p.cycleSimpleAction(ctx, tx, householdID, review, step, "TRANSACTION_MISSING")
	case data == callbackCycleAll && step.Fact == factSplitAmount:
		return p.commitSplitAmount(ctx, tx, householdID, sourceEventID, review, step, nil)
	case data == callbackBack:
		return p.cycleBack(ctx, tx, householdID, review, step)
	case data == callbackCancel:
		return p.cycleGo(ctx, tx, householdID, review, "AWAITING_DETAIL", nil, "")
	}
	return reask(step, cycleInvalidAck), nil
}

func (p *Processor) applyCycleText(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, text string, targeted bool) (stepOutcome, error) {
	if isYesNoAnswer(text) || !contains(cardActions(review.Decision, review.ReviewType), "ALLOCATE_RETAINED_BALANCE") {
		return reask(step, unusableAnswerAck), nil
	}
	if step.Fact == factSplitAmount {
		amount, ok := parseSplitAmount(text)
		if !ok {
			return reask(step, splitAmountBadAck), nil
		}
		return p.commitSplitAmount(ctx, tx, householdID, sourceEventID, review, step, amount)
	}
	hint := strings.TrimSpace(text)
	if hint == "" || len([]rune(hint)) > 80 || strings.Contains(hint, "?") {
		return reask(step, unusableAnswerAck), nil
	}
	choices, err := p.cycleCandidatesFor(ctx, tx, householdID, review.RequestID, step.Fact)
	if err != nil {
		return stepOutcome{}, err
	}
	id, ok, err := accountFromText(ctx, tx, householdID, hint, targeted, choices)
	if err != nil {
		return stepOutcome{}, err
	}
	if !ok {
		return reask(step, "Aku belum menemukan satu akun yang cocok dengan “"+clean(hint, 40)+"”."), nil
	}
	return p.chooseCycleAccount(ctx, tx, householdID, sourceEventID, review, step, id)
}

func parseSplitAmount(text string) (*big.Int, bool) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	if len(fields) > 1 && (fields[0] == "rp" || fields[0] == "rp.") {
		fields = fields[1:]
	}
	var value string
	var ok bool
	switch len(fields) {
	case 1:
		value, ok = parseAmountToken(fields[0], "")
	case 2:
		unit, known := amountUnits[fields[1]]
		if !known {
			return nil, false
		}
		value, ok = parseAmountToken(fields[0], unit)
	}
	if !ok {
		return nil, false
	}
	amount, parsed := new(big.Int).SetString(value, 10)
	return amount, parsed && amount.Sign() > 0
}

func (p *Processor) chooseCycleAccount(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, accountID string) (stepOutcome, error) {
	if step.Fact == factCycle {
		if review.Subject.Cycle == nil {
			return reask(step, cycleInvalidAck), nil
		}
		return p.applyCycleAllocations(ctx, tx, householdID, review, step, []reviewdomain.CycleAllocation{{WealthAccountID: accountID, AmountIDR: review.Subject.Cycle.Residual}})
	}
	split, err := loadCycleSplit(ctx, tx, review.RequestID)
	if err != nil {
		return stepOutcome{}, err
	}
	split.Pending = accountID
	return p.cycleGo(ctx, tx, householdID, review, "AWAITING_CYCLE_AMOUNT", &split, "")
}

func (p *Processor) cycleGo(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, state string, split *cycleSplit, ack string) (stepOutcome, error) {
	if err := saveCycleSplit(ctx, tx, review.RequestID, state, split); err != nil {
		return stepOutcome{}, err
	}
	review.State = state
	step, ok, err := p.currentStep(ctx, tx, householdID, review)
	if err != nil {
		return stepOutcome{}, err
	}
	if !ok {
		return stepOutcome{}, fmt.Errorf("review %s has no cycle step", review.RequestID)
	}
	return stepOutcome{Step: step.WithAck(ack), Changed: true}, nil
}

func (p *Processor) cycleBack(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step) (stepOutcome, error) {
	split, err := loadCycleSplit(ctx, tx, review.RequestID)
	if err != nil {
		return stepOutcome{}, err
	}
	switch {
	case step.Fact == factSplitAmount:
		split.Pending = ""
		return p.cycleGo(ctx, tx, householdID, review, "AWAITING_CYCLE_ACCOUNT", &split, "")
	case step.Fact == factSplitAcct && len(split.Allocations) > 0:
		last := split.Allocations[len(split.Allocations)-1]
		split.Allocations, split.Pending = split.Allocations[:len(split.Allocations)-1], last.AccountID
		return p.cycleGo(ctx, tx, householdID, review, "AWAITING_CYCLE_AMOUNT", &split, "")
	}
	return p.cycleGo(ctx, tx, householdID, review, "AWAITING_DETAIL", nil, "")
}

func (p *Processor) commitSplitAmount(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, amount *big.Int) (stepOutcome, error) {
	if review.Subject.Cycle == nil || step.Fact != factSplitAmount {
		return reask(step, cycleInvalidAck), nil
	}
	split, err := loadCycleSplit(ctx, tx, review.RequestID)
	if err != nil {
		return stepOutcome{}, err
	}
	remaining := split.remaining(review.Subject.Cycle.Residual)
	if amount == nil {
		amount = remaining
	}
	if split.Pending == "" || amount.Sign() <= 0 || amount.Cmp(remaining) > 0 {
		return stepOutcome{Step: step.WithAck("Jumlahnya harus lebih dari nol dan tidak lebih dari sisa " + rupiah(remaining) + ".")}, nil
	}
	if amount.Cmp(remaining) < 0 {
		others, err := cycleAccountCandidates(ctx, tx, householdID, append(split.usedIDs(), split.Pending))
		if err != nil {
			return stepOutcome{}, err
		}
		if len(others) == 0 {
			return stepOutcome{Step: step.WithAck("Ini akun terakhir, jadi harus menampung seluruh sisa " + rupiah(remaining) + ".")}, nil
		}
	}
	split.Allocations = append(split.Allocations, cycleSplitAllocation{AccountID: split.Pending, Amount: amount.String()})
	split.Pending = ""
	if amount.Cmp(remaining) == 0 {
		allocations := make([]reviewdomain.CycleAllocation, 0, len(split.Allocations))
		for _, allocation := range split.Allocations {
			allocations = append(allocations, reviewdomain.CycleAllocation{WealthAccountID: allocation.AccountID, AmountIDR: allocation.Amount})
		}
		return p.applyCycleAllocations(ctx, tx, householdID, review, step, allocations)
	}
	return p.cycleGo(ctx, tx, householdID, review, "AWAITING_CYCLE_ACCOUNT", &split, "✅ "+rupiah(amount)+" dialokasikan.")
}

func (p *Processor) applyCycleAllocations(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, allocations []reviewdomain.CycleAllocation) (stepOutcome, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	result, err := reviewdomain.ApplyCycleResidual(ctx, sp, reviewdomain.CycleResidualCommand{
		HouseholdID: householdID, CaseID: review.CycleCaseID, ReviewItemID: review.ItemID, RequestID: review.RequestID,
		Action: "ALLOCATE_RETAINED_BALANCE", Allocations: allocations, ActorUserID: review.UserID,
	})
	switch {
	case errors.Is(err, reviewdomain.ErrCycleCaseNotFound):
		_ = sp.Rollback(ctx)
		return staleTap(step), nil
	case errors.Is(err, reviewdomain.ErrCycleAllocationMismatch), errors.Is(err, reviewdomain.ErrCycleAllocationInvalid),
		errors.Is(err, reviewdomain.ErrCycleAllocationDuplicate), errors.Is(err, reviewdomain.ErrCycleWealthAccountInvalid),
		errors.Is(err, reviewdomain.ErrCycleAllocationsRequired):
		_ = sp.Rollback(ctx)
		return p.cycleRestart(ctx, tx, householdID, review, cycleChangedAck)
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if result.Outcome != reviewdomain.CycleResolved {
		if err := sp.Commit(ctx); err != nil {
			return stepOutcome{}, err
		}
		return p.cycleStale(ctx, tx, householdID, review, result)
	}
	if _, err := sp.Exec(ctx, cycleResidualAuditSQL, householdID, review.UserID, "CYCLE_RESIDUAL_ALLOCATE_RETAINED_BALANCE", review.CycleCaseID, result.Residual); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	ids := make([]string, 0, len(allocations))
	for _, allocation := range allocations {
		ids = append(ids, allocation.WealthAccountID)
	}
	names, err := accountNames(ctx, tx, householdID, ids)
	if err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: cycleAllocatedStep(result.Residual, allocations, names), Settled: true, Changed: true}, nil
}

func cycleAllocatedStep(residual string, allocations []reviewdomain.CycleAllocation, names map[string]string) Step {
	if len(allocations) == 1 {
		return Step{Kind: stepDone, Ack: "✅ Sisa Rp" + FormatIDR(residual) + " dialokasikan ke " + clean(names[allocations[0].WealthAccountID], 60) + "."}
	}
	parts := make([]string, 0, len(allocations))
	for _, allocation := range allocations {
		parts = append(parts, clean(names[allocation.WealthAccountID], 40)+" Rp"+FormatIDR(allocation.AmountIDR))
	}
	return Step{Kind: stepDone, Ack: fmt.Sprintf("✅ Sisa Rp%s dialokasikan ke %d akun: %s.", FormatIDR(residual), len(allocations), strings.Join(parts, ", "))}
}

func (p *Processor) cycleSimpleAction(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, action string) (stepOutcome, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	command := reviewdomain.CycleResidualCommand{HouseholdID: householdID, CaseID: review.CycleCaseID, ReviewItemID: review.ItemID, RequestID: review.RequestID, Action: action, ActorUserID: review.UserID}
	var result reviewdomain.CycleResult
	if action == "TRANSACTION_MISSING" {
		result, err = reviewdomain.MarkCycleTransactionMissing(ctx, sp, command)
	} else {
		result, err = reviewdomain.ApplyCycleResidual(ctx, sp, command)
	}
	if errors.Is(err, reviewdomain.ErrCycleCaseNotFound) {
		_ = sp.Rollback(ctx)
		return staleTap(step), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if result.Outcome != reviewdomain.CycleResolved {
		if err := sp.Commit(ctx); err != nil {
			return stepOutcome{}, err
		}
		return p.cycleStale(ctx, tx, householdID, review, result)
	}
	if _, err := sp.Exec(ctx, cycleResidualAuditSQL, householdID, review.UserID, "CYCLE_RESIDUAL_"+action, review.CycleCaseID, result.Residual); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	if action == "TRANSACTION_MISSING" {
		return stepOutcome{Step: Step{Kind: stepDone, Ack: cycleMissingAck}, Settled: true, Changed: true}, nil
	}
	return stepOutcome{Step: Step{Kind: stepDone, Ack: "✅ Sisa Rp" + FormatIDR(result.Residual) + " dibiarkan tidak dialokasikan."}, Settled: true, Changed: true}, nil
}

func (p *Processor) cycleStale(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, result reviewdomain.CycleResult) (stepOutcome, error) {
	if result.Outcome == reviewdomain.CycleStaleNotApplicable {
		return stepOutcome{Step: Step{Kind: stepDone, Ack: cycleClosedAck}, Settled: true, Changed: true}, nil
	}
	return p.cycleRestart(ctx, tx, householdID, review, cycleChangedAck)
}

func (p *Processor) cycleRestart(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, ack string) (stepOutcome, error) {
	if _, err := tx.Exec(ctx, `UPDATE job SET status='SUCCEEDED',finished_at=now(),updated_at=now(),payload_json=payload_json||'{"superseded_by":"step_engine"}'::jsonb WHERE type='EDIT_TELEGRAM_MESSAGE' AND status='PENDING' AND payload_json->>'review_request_id'=$1`, review.RequestID); err != nil {
		return stepOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json-'cycle_split',updated_at=now() WHERE review_request_id=$1`, review.RequestID); err != nil {
		return stepOutcome{}, err
	}
	outcome, err := p.reloadedStep(ctx, tx, householdID, review)
	if err != nil {
		return stepOutcome{}, err
	}
	outcome.Step = outcome.Step.WithAck(ack)
	return outcome, nil
}
