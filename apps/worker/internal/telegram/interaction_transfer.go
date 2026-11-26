package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

const (
	accountStampBase = 1000
	accountStampKey  = "account"
)

const (
	transferAsk            = "Ini untuk apa?"
	accountHint            = "Pilih di bawah, atau ketik nama akunnya."
	noAssetAccountsHint    = "Belum ada akun aset yang aktif. Tambahkan dulu di halaman Kekayaan, lalu pilih lagi."
	noInvestAccountsHint   = "Belum ada akun investasi yang aktif. Tambahkan dulu di halaman Kekayaan, lalu pilih lagi."
	accountStaleAck        = "Pilihan itu sudah tidak berlaku."
	accountIncompatibleAck = "Akun itu tidak cocok untuk pilihan ini."
	transferInvalidAck     = "Pilihan itu tidak berlaku untuk transaksi ini."
	ignoreNotAllowedAck    = "Kartu ini tidak bisa diabaikan."
	relinkElsewhereHint    = "Akun transfer ini diganti di " + reviewInboxName + " di aplikasi."
	reconciliationHint     = "Pilih transaksi yang sama untuk digabung, atau Catat transfer baru."
)

type transferOption struct {
	Action string
	Label  string
	Data   string
}

var transferOptions = []transferOption{
	{"", "Pemasukan", callbackIncome},
	{"EXPENSE", "Pengeluaran", callbackExpense},
	{"ASSET_PURCHASE", "Beli aset", callbackAsset},
	{"OWN_ACCOUNT", "Ke rekening sendiri", callbackOwn},
	{"HOUSEHOLD_ACCOUNT", "Rumah tangga", callbackHousehold},
	{"INVESTMENT_ACCOUNT", "Investasi", callbackInvestment},
}

func transferOptionApplies(kind string, option transferOption) bool {
	switch kind {
	case "INCOME":
		return option.Data == callbackIncome || option.Data == callbackOwn || option.Data == callbackHousehold
	case "EXPENSE":
		return option.Data == callbackExpense || option.Data == callbackAsset
	}
	return option.Data != callbackIncome
}

func classificationActions(actions []string) []string {
	for _, option := range transferOptions {
		if option.Action != "" && contains(actions, option.Action) {
			return actions
		}
	}
	return nil
}

func transferChoiceRows(kind string, actions []string) [][]InlineKeyboardButton {
	var buttons []InlineKeyboardButton
	for _, option := range transferOptions {
		if transferOptionApplies(kind, option) && (option.Action == "" || actions == nil || contains(actions, option.Action)) {
			buttons = append(buttons, InlineKeyboardButton{Text: option.Label, CallbackData: option.Data})
		}
	}
	buttons = append(buttons, InlineKeyboardButton{Text: "Abaikan", CallbackData: callbackIgnore})
	var rows [][]InlineKeyboardButton
	for len(buttons) > 0 {
		take := min(2, len(buttons))
		rows = append(rows, buttons[:take])
		buttons = buttons[take:]
	}
	return rows
}

func deriveTransferStep(step Step, decision reviewdec.Decision, subject reviewSubject) Step {
	step.Kind, step.Fact, step.State, step.Ask = stepChoice, factTransfer, "AWAITING_DETAIL", transferAsk
	if subject.Reconciliation {
		step.Hint = reconciliationHint
		return step
	}
	kind := subject.transactionType(decision)
	if kind == "TRANSFER" {
		switch subject.Purpose {
		case "INVESTMENT_CONTRIBUTION":
			return accountStepFrom(step, factInvest, false)
		case "ASSET_PURCHASE":
			return accountStepFrom(step, factAsset, false)
		}
		step.Hint = relinkElsewhereHint
		step.Rows = [][]InlineKeyboardButton{{{Text: "Abaikan", CallbackData: callbackIgnore}}}
		return step
	}
	step.Hint = "Pilih di bawah."
	step.Rows = transferChoiceRows(kind, classificationActions(cardActions(decision, transferReviewType)))
	return step
}

func incomeConfirmStep(step Step) Step {
	step.Kind, step.Fact, step.State = stepChoice, factIncome, "AWAITING_CATEGORY"
	step.Ask, step.Hint = "Pemasukan ini perlu dicek.", "Pilih Benar jika ini penghasilan, atau Ubah jika ini transfer."
	step.Rows = [][]InlineKeyboardButton{{{Text: "Benar", CallbackData: callbackConfirm}, {Text: "Ubah", CallbackData: callbackChange}}, {{Text: "Abaikan", CallbackData: callbackIgnore}}}
	return step
}

func incomeChoiceStep(base Step) Step {
	base.Kind, base.Fact, base.State, base.Ask, base.Hint = stepChoice, factTransfer, "AWAITING_DETAIL", transferAsk, "Pilih di bawah."
	base.Rows = transferChoiceRows("INCOME", nil)
	return base
}

func transferStepForState(state string) Step {
	switch state {
	case "AWAITING_ASSET_WEALTH":
		return Step{Kind: stepChoice, Fact: factAsset}
	case "AWAITING_INVESTMENT_ACCOUNT":
		return Step{Kind: stepChoice, Fact: factInvest}
	case "AWAITING_CATEGORY":
		return Step{Kind: stepChoice, Fact: factCategory}
	}
	return Step{Kind: stepChoice, Fact: factTransfer}
}

func accountStepFrom(base Step, fact factKind, back bool) Step {
	base.Kind, base.Fact, base.Back, base.Rows, base.Hint = stepChoice, fact, back, nil, accountHint
	if fact == factInvest {
		base.State, base.Ask = "AWAITING_INVESTMENT_ACCOUNT", "Investasi ke akun mana?"
	} else {
		base.State, base.Ask = "AWAITING_ASSET_WEALTH", "Beli aset ke akun mana?"
	}
	return base
}

func categoryStepFrom(base Step, back bool) Step {
	base.Kind, base.Fact, base.State, base.Back, base.Rows = stepChoice, factCategory, "AWAITING_CATEGORY", back, nil
	base.Ask, base.Hint = "Masuk kategori apa?", categoryHint
	return base
}

func (s *Step) loadRows(ctx context.Context, tx pgx.Tx, reviewID string, page int) error {
	switch s.Fact {
	case factCategory:
		return s.loadCategoryRows(ctx, tx, reviewID, page)
	case factAsset, factInvest:
		return s.loadAccountRows(ctx, tx, reviewID, page)
	case factTransfer:
		if len(s.Rows) == 0 {
			return s.loadReconciliationRows(ctx, tx, reviewID)
		}
	case factCycle, factSplitAcct, factSplitAmount:
		return s.loadCycleRows(ctx, tx, reviewID, page)
	case factDuplicate:
		return s.loadDuplicateRows(ctx, tx, reviewID, page)
	case factEmailAccount, factEmailWealth, factWealthObs:
		return s.loadEntityRows(ctx, tx, reviewID, page)
	case factWealthConfirm:
		return s.loadWealthConfirmRows(ctx, tx, reviewID)
	case factBankAccount:
		return s.loadBankAccountRows(ctx, tx, reviewID, page)
	}
	return nil
}

func (s *Step) loadReconciliationRows(ctx context.Context, tx pgx.Tx, reviewID string) error {
	markup, err := transferReconciliationMarkup(ctx, tx, reviewID)
	if err != nil {
		return err
	}
	if markup != nil {
		s.Rows = markup.InlineKeyboard
	}
	return nil
}

type accountChoice struct {
	ID   string
	Name string
}

func accountCandidates(ctx context.Context, tx pgx.Tx, householdID string, fact factKind) ([]accountChoice, error) {
	role := ""
	if fact == factInvest {
		role = "INVESTMENT"
	}
	rows, err := tx.Query(ctx, `SELECT id::text,name FROM wealth_account WHERE household_id=$1 AND active AND side='ASSET' AND ($2='' OR usage_role=$2) ORDER BY (wealth_type IN ('BANK','CASH','EWALLET')),name,id`, householdID, role)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (accountChoice, error) {
		var choice accountChoice
		err := row.Scan(&choice.ID, &choice.Name)
		return choice, err
	})
}

func (s *Step) loadAccountRows(ctx context.Context, tx pgx.Tx, reviewID string, page int) error {
	var householdID string
	if err := tx.QueryRow(ctx, `SELECT household_id::text FROM review_request WHERE id=$1`, reviewID).Scan(&householdID); err != nil {
		return err
	}
	choices, err := accountCandidates(ctx, tx, householdID, s.Fact)
	if err != nil {
		return err
	}
	prefix, pagePrefix := callbackAccount, callbackAccountPg
	empty := noAssetAccountsHint
	if s.Fact == factInvest {
		prefix, pagePrefix, empty = callbackInvest, callbackInvestPg, noInvestAccountsHint
	}
	if err := s.loadStampedAccounts(ctx, tx, reviewID, accountStampKey, choices, page, prefix, pagePrefix, empty); err != nil {
		return err
	}
	tail := []InlineKeyboardButton{{Text: "Abaikan", CallbackData: callbackIgnore}}
	if s.Back {
		tail = append([]InlineKeyboardButton{{Text: "Kembali", CallbackData: callbackBack}}, tail...)
	}
	s.Rows = append(s.Rows, tail)
	return nil
}

func stampKeys(key string) (candidates, version string) {
	return key + "_candidates", key + "_version"
}

func readStamp(ctx context.Context, tx pgx.Tx, reviewID, key string) ([]string, int, error) {
	candidatesKey, versionKey := stampKeys(key)
	var raw []byte
	var version int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(context_json->$2,'[]'::jsonb),COALESCE((context_json->>$3)::int,0) FROM review_conversation WHERE review_request_id=$1`, reviewID, candidatesKey, versionKey).Scan(&raw, &version); err != nil {
		return nil, 0, err
	}
	var stored []string
	if json.Unmarshal(raw, &stored) != nil {
		stored = nil
	}
	return stored, version, nil
}

func stampList(ctx context.Context, tx pgx.Tx, reviewID, key string, ids []string, readOnly bool) (int, error) {
	stored, version, err := readStamp(ctx, tx, reviewID, key)
	if err != nil {
		return 0, err
	}
	if slices.Equal(stored, ids) {
		return version, nil
	}
	version++
	if readOnly {
		return version, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return 0, err
	}
	candidatesKey, versionKey := stampKeys(key)
	_, err = tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json||jsonb_build_object($2::text,$3::jsonb,$4::text,$5::int),updated_at=now() WHERE review_request_id=$1`, reviewID, candidatesKey, string(encoded), versionKey, version)
	return version, err
}

func resolveStamp(ctx context.Context, tx pgx.Tx, reviewID, key, data, prefix string) (string, bool, error) {
	number, err := strconv.Atoi(strings.TrimPrefix(data, prefix))
	if !strings.HasPrefix(data, prefix) || err != nil || number < 0 {
		return "", false, nil
	}
	index, stamp := number%accountStampBase, number/accountStampBase
	stored, version, err := readStamp(ctx, tx, reviewID, key)
	if err != nil {
		return "", false, err
	}
	if stamp != version || index >= len(stored) {
		return "", false, nil
	}
	return stored[index], true, nil
}

func stampedData(prefix string, version, index int) string {
	return fmt.Sprintf("%s%d", prefix, version*accountStampBase+index)
}

func (s *Step) loadStampedAccounts(ctx context.Context, tx pgx.Tx, reviewID, key string, choices []accountChoice, page int, prefix, pagePrefix, empty string) error {
	ids := make([]string, 0, len(choices))
	for _, choice := range choices {
		ids = append(ids, choice.ID)
	}
	version, err := stampList(ctx, tx, reviewID, key, ids, s.ReadOnly)
	if err != nil {
		return err
	}
	s.Page = clampChooserPage(page, len(choices), chooserPageSize)
	s.Hint = accountHint
	if len(choices) == 0 {
		s.Hint = empty
	} else if s.Page > 0 {
		s.Hint = fmt.Sprintf("Pilih di bawah, atau ketik nama akunnya (halaman %d).", s.Page+1)
	}
	start := s.Page * chooserPageSize
	end := min(start+chooserPageSize, len(choices))
	s.Rows = nil
	for index := start; index < end; index++ {
		s.Rows = append(s.Rows, []InlineKeyboardButton{{Text: clean(choices[index].Name, 28), CallbackData: stampedData(prefix, version, index)}})
	}
	if pager := pagerRow(s.Page, len(choices), chooserPageSize, pagePrefix); len(pager) > 0 {
		s.Rows = append(s.Rows, pager)
	}
	return nil
}

func resolveStampedAccount(ctx context.Context, tx pgx.Tx, reviewID, key, data, prefix string, current []accountChoice) (string, bool, error) {
	id, ok, err := resolveStamp(ctx, tx, reviewID, key, data, prefix)
	if err != nil || !ok || !accountListed(current, id) {
		return "", false, err
	}
	return id, true, nil
}

func accountFromText(ctx context.Context, tx pgx.Tx, householdID, hint string, targeted bool, choices []accountChoice) (string, bool, error) {
	if targeted {
		id, err := resolveUniqueWealthHint(ctx, tx, householdID, hint)
		return id, err == nil && accountListed(choices, id), nil
	}
	exact, ok := exactAccountMatch(hint, choices)
	return exact.ID, ok, nil
}

func (p *Processor) baseStep(ctx context.Context, tx pgx.Tx, householdID string, review engineReview) (Step, error) {
	facts, err := reviewItemSummary(ctx, tx, householdID, review.ItemID)
	if err != nil {
		return Step{}, err
	}
	step, ok := deriveStep(review.Decision, review.ReviewType, facts, review.Subject)
	if !ok {
		return Step{}, fmt.Errorf("review %s has no first step", review.RequestID)
	}
	return step, nil
}

func (p *Processor) moveToStep(ctx context.Context, tx pgx.Tx, review engineReview, step Step) (stepOutcome, error) {
	if err := step.loadRows(ctx, tx, review.RequestID, 0); err != nil {
		return stepOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state=$2,last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.RequestID, step.State); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: step.executable(cardActions(review.Decision, review.ReviewType)), Changed: true}, nil
}

func (p *Processor) enterAccountStep(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, fact factKind) (stepOutcome, error) {
	base, err := p.baseStep(ctx, tx, householdID, review)
	if err != nil {
		return stepOutcome{}, err
	}
	return p.moveToStep(ctx, tx, review, accountStepFrom(base, fact, base.Fact != factAsset && base.Fact != factInvest))
}

func (p *Processor) enterCategoryStep(ctx context.Context, tx pgx.Tx, householdID string, review engineReview) (stepOutcome, error) {
	base, err := p.baseStep(ctx, tx, householdID, review)
	if err != nil {
		return stepOutcome{}, err
	}
	return p.moveToStep(ctx, tx, review, categoryStepFrom(base, base.Fact == factTransfer || base.Fact == factQuality || base.Fact == factCorrection))
}

func (p *Processor) backToFirstStep(ctx context.Context, tx pgx.Tx, householdID string, review engineReview) (stepOutcome, error) {
	base, err := p.baseStep(ctx, tx, householdID, review)
	if err != nil {
		return stepOutcome{}, err
	}
	return p.moveToStep(ctx, tx, review, base)
}

func assetEntryAllowed(review engineReview, step Step) bool {
	switch step.Fact {
	case factMerchant, factCategory, factTransfer:
		return review.TxType == "EXPENSE" || review.TxType == "UNCLASSIFIED"
	}
	return false
}

func (p *Processor) applyTransferTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, bool, error) {
	var outcome stepOutcome
	var err error
	switch {
	case data == callbackBack && step.Back && stepOffers(step, data):
		outcome, err = p.backToFirstStep(ctx, tx, householdID, review)
	case data == callbackAsset && assetEntryAllowed(review, step) && stepOffers(step, data):
		outcome, err = p.enterAccountStep(ctx, tx, householdID, review, factAsset)
	case data == callbackBack || data == callbackAsset:
		outcome = reask(step, transferInvalidAck)
	case step.Fact == factIncome:
		outcome, err = p.applyIncomeConfirm(ctx, tx, householdID, sourceEventID, review, step, data)
	case step.Fact == factTransfer:
		outcome, err = p.applyTransferChoice(ctx, tx, householdID, sourceEventID, review, step, data)
	case step.Fact == factAsset || step.Fact == factInvest:
		outcome, err = p.applyAccountTap(ctx, tx, householdID, sourceEventID, review, step, data)
	default:
		return stepOutcome{}, false, nil
	}
	return outcome, true, err
}

func (p *Processor) applyIncomeConfirm(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	switch data {
	case callbackConfirm:
		return p.applyIncome(ctx, tx, householdID, sourceEventID, review, step)
	case callbackChange:
		return p.moveToStep(ctx, tx, review, incomeChoiceStep(step))
	}
	return reask(step, transferInvalidAck), nil
}

func stepOffers(step Step, data string) bool {
	for _, button := range step.buttons() {
		if button.CallbackData == data {
			return true
		}
	}
	return false
}

func (p *Processor) applyTransferChoice(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	if !stepOffers(step, data) {
		return reask(step, transferInvalidAck), nil
	}
	switch {
	case data == callbackExpense:
		return p.enterCategoryStep(ctx, tx, householdID, review)
	case data == callbackOwn:
		return p.classifyTransfer(ctx, tx, householdID, sourceEventID, review, step, "OWN_ACCOUNT", "", "")
	case data == callbackHousehold:
		return p.classifyTransfer(ctx, tx, householdID, sourceEventID, review, step, "HOUSEHOLD_ACCOUNT", "", "")
	case data == callbackInvestment:
		return p.classifyTransfer(ctx, tx, householdID, sourceEventID, review, step, "INVESTMENT_ACCOUNT", "", "")
	case data == callbackIncome:
		return p.applyIncome(ctx, tx, householdID, sourceEventID, review, step)
	case data == callbackNewXfer || strings.HasPrefix(data, callbackMerge):
		return p.applyReconciliation(ctx, tx, householdID, review, step, data)
	}
	return reask(step, transferInvalidAck), nil
}

func (p *Processor) applyAccountTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	prefix, pagePrefix := callbackAccount, callbackAccountPg
	if step.Fact == factInvest {
		prefix, pagePrefix = callbackInvest, callbackInvestPg
	}
	if page, ok := parsePageCallback(data, pagePrefix); ok {
		if err := step.loadRows(ctx, tx, review.RequestID, page); err != nil {
			return stepOutcome{}, err
		}
		return stepOutcome{Step: step.executable(cardActions(review.Decision, review.ReviewType))}, nil
	}
	current, err := accountCandidates(ctx, tx, householdID, step.Fact)
	if err != nil {
		return stepOutcome{}, err
	}
	id, ok, err := resolveStampedAccount(ctx, tx, review.RequestID, accountStampKey, data, prefix, current)
	if err != nil {
		return stepOutcome{}, err
	}
	if !ok {
		return reask(step, accountStaleAck), nil
	}
	return p.finishAccount(ctx, tx, householdID, sourceEventID, review, step, id)
}

func exactAccountMatch(text string, choices []accountChoice) (accountChoice, bool) {
	value := stripAnswerFiller(text)
	if value == "" || strings.Contains(text, "?") {
		return accountChoice{}, false
	}
	var found accountChoice
	count := 0
	for _, choice := range choices {
		if value == stripAnswerFiller(choice.Name) || value == normalizeAnswer(choice.Name) {
			found = choice
			count++
		}
	}
	return found, count == 1
}

func accountListed(choices []accountChoice, id string) bool {
	for _, choice := range choices {
		if choice.ID == id {
			return true
		}
	}
	return false
}

func (p *Processor) applyAccountText(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, text string, targeted bool) (stepOutcome, error) {
	hint := strings.TrimSpace(text)
	if hint == "" || len([]rune(hint)) > 80 || strings.Contains(hint, "?") {
		return reask(step, unusableAnswerAck), nil
	}
	choices, err := accountCandidates(ctx, tx, householdID, step.Fact)
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
	return p.finishAccount(ctx, tx, householdID, sourceEventID, review, step, id)
}

func (p *Processor) finishAccount(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, accountID string) (stepOutcome, error) {
	classification := "ASSET_PURCHASE"
	if step.Fact == factInvest {
		classification = "INVESTMENT_ACCOUNT"
	}
	return p.classifyTransfer(ctx, tx, householdID, sourceEventID, review, step, classification, accountID, "")
}

var transferLabels = map[string]string{
	"OWN_ACCOUNT":        "Antar rekening sendiri",
	"HOUSEHOLD_ACCOUNT":  "Antar anggota keluarga",
	"ASSET_PURCHASE":     "Beli aset",
	"INVESTMENT_ACCOUNT": "Investasi",
	"INCOME":             "Pemasukan",
}

func transferDoneStep(label, detail, amount string) Step {
	return Step{Kind: stepDone, Ack: savedLine(label, detail, IDR(amount))}
}

func (p *Processor) classifyTransfer(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, classification, wealthAccountID, categoryID string) (stepOutcome, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	result, err := reviewdomain.ClassifyTransferReview(ctx, sp, reviewdomain.TransferCommand{
		HouseholdID: householdID, ActorUserID: review.UserID, TransactionID: review.TransactionID,
		ReviewItemID: review.ItemID, RequestID: review.RequestID,
		Action: "TELEGRAM_TRANSFER_CLASSIFIED", Classification: classification,
		CategoryID: categoryID, WealthAccountID: wealthAccountID,
	})
	var missing *reviewdomain.ErrMissingFacts
	switch {
	case errors.As(err, &missing) && result.Status == "NEEDS_REVIEW":
		if err := sp.Commit(ctx); err != nil {
			return stepOutcome{}, err
		}
		return p.reloadedStep(ctx, tx, householdID, review)
	case errors.Is(err, reviewdomain.ErrInvestmentAccountAmbiguous):
		_ = sp.Rollback(ctx)
		return p.enterAccountStep(ctx, tx, householdID, review, factInvest)
	case errors.Is(err, reviewdomain.ErrWealthAccountIncompatible), errors.Is(err, reviewdomain.ErrWealthAccountRequired):
		_ = sp.Rollback(ctx)
		return reask(step, accountIncompatibleAck), nil
	case errors.Is(err, reviewdomain.ErrMergeTargetActive):
		_ = sp.Rollback(ctx)
		return reask(step, mergeTargetActiveAck), nil
	case errors.Is(err, reviewdomain.ErrTransferNotFound), errors.Is(err, reviewdomain.ErrAlreadyResolved):
		_ = sp.Rollback(ctx)
		return reask(step, transferInvalidAck), nil
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'classification',$4::text)) ON CONFLICT DO NOTHING`, review.TransactionID, sourceEventID, review.RequestID, classification); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'CLASSIFY_TRANSFER','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'classification',$5::text,'type',$6::text,'purpose',$7::text,'related_wealth_account_id',NULLIF($8,'')::text,'status',$9::text))`, householdID, review.UserID, review.TransactionID, review.RequestID, classification, result.Type, result.Purpose, result.WealthAccountID, result.Status); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	amount, err := transactionAmount(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	label, detail := transferLabels[classification], ""
	if classification == "EXPENSE" {
		label = ""
		if err := tx.QueryRow(ctx, `SELECT COALESCE(name,'') FROM category WHERE id=NULLIF($1,'')::uuid`, result.CategoryID).Scan(&detail); err != nil {
			return stepOutcome{}, err
		}
	}
	if result.WealthAccountID != "" {
		if err := tx.QueryRow(ctx, `SELECT name FROM wealth_account WHERE id=$1`, result.WealthAccountID).Scan(&detail); err != nil {
			return stepOutcome{}, err
		}
	}
	return stepOutcome{Step: transferDoneStep(label, detail, amount), Settled: true, Changed: true}, nil
}

func (p *Processor) applyIncome(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step) (stepOutcome, error) {
	if review.TxType != "INCOME" {
		return reask(step, transferInvalidAck), nil
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	remaining, err := reviewdomain.ApplyTransactionReviewFacts(ctx, sp, reviewdomain.ReviewFacts{HouseholdID: householdID, TransactionID: review.TransactionID, Relationship: "INCOME"})
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if len(remaining) > 0 {
		if _, err := sp.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id) VALUES($1,'TELEGRAM',$2,'REVIEW_INCOME_CHOICE','transaction',$3)`, householdID, review.UserID, review.TransactionID); err != nil {
			_ = sp.Rollback(ctx)
			return stepOutcome{}, err
		}
		if err := sp.Commit(ctx); err != nil {
			return stepOutcome{}, err
		}
		return p.reloadedStep(ctx, tx, householdID, review)
	}
	_, err = reviewdomain.ConfirmTransactionReview(ctx, sp, reviewdomain.ConfirmCommand{
		HouseholdID: householdID, ActorUserID: review.UserID, TransactionID: review.TransactionID,
		ReviewItemID: review.ItemID, RequestID: review.RequestID,
		Action: "TELEGRAM_CONFIRMED", ResolveReview: true,
	})
	if errors.Is(err, reviewdomain.ErrAlreadyResolved) {
		_ = sp.Rollback(ctx)
		return staleTap(step), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'classification','INCOME')) ON CONFLICT DO NOTHING`, review.TransactionID, sourceEventID, review.RequestID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	amount, err := transactionAmount(ctx, tx, householdID, review.TransactionID)
	if err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: transferDoneStep(transferLabels["INCOME"], "", amount), Settled: true, Changed: true}, nil
}

func (p *Processor) reloadedStep(ctx context.Context, tx pgx.Tx, householdID string, review engineReview) (stepOutcome, error) {
	var raw []byte
	var reviewType string
	if err := tx.QueryRow(ctx, `SELECT ri.decision,ri.review_type FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1 AND r.household_id=$2`, review.RequestID, householdID).Scan(&raw, &reviewType); err != nil {
		return stepOutcome{}, err
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return stepOutcome{}, err
	}
	facts, err := reviewItemSummary(ctx, tx, householdID, review.ItemID)
	if err != nil {
		return stepOutcome{}, err
	}
	subject, err := loadReviewSubject(ctx, tx, review.RequestID)
	if err != nil {
		return stepOutcome{}, err
	}
	return p.moveToStep(ctx, tx, review, stepOrFallback(decision, reviewType, facts, subject))
}

func reconciliationAmount(ctx context.Context, tx pgx.Tx, householdID, itemID string) (string, error) {
	var amount string
	err := tx.QueryRow(ctx, `SELECT round(trc.amount_idr)::text FROM review_item ri
		JOIN transfer_reconciliation_case trc ON trc.household_id=ri.household_id AND trc.status='OPEN'
		  AND ri.financial_email_observation_id IS NULL AND trc.financial_email_observation_id IS NULL AND trc.source_event_id=ri.source_event_id
		WHERE ri.id=$1 AND ri.household_id=$2`, itemID, householdID).Scan(&amount)
	return amount, err
}

func (p *Processor) applyReconciliation(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, data string) (stepOutcome, error) {
	if !review.Subject.Reconciliation {
		return reask(step, transferInvalidAck), nil
	}
	command := reviewdomain.TransferReconciliationCommand{HouseholdID: householdID, ActorUserID: review.UserID, ReviewItemID: review.ItemID, Action: "CONFIRM_NEW_TRANSFER", ActorType: "TELEGRAM"}
	label := "Transfer baru"
	if strings.HasPrefix(data, callbackMerge) {
		index, err := strconv.Atoi(strings.TrimPrefix(data, callbackMerge))
		var raw []byte
		if err != nil || index < 0 {
			return staleTap(step), nil
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(context_json->'transfer_candidates','[]'::jsonb) FROM review_conversation WHERE review_request_id=$1`, review.RequestID).Scan(&raw); err != nil {
			return stepOutcome{}, err
		}
		var candidates []string
		if json.Unmarshal(raw, &candidates) != nil || index >= len(candidates) {
			return staleTap(step), nil
		}
		command.Action, command.CandidateID = "MERGE_EXISTING", candidates[index]
		label = "Digabung dengan transaksi yang sudah ada"
	}
	amount, err := reconciliationAmount(ctx, tx, householdID, review.ItemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return staleTap(step), nil
	}
	if err != nil {
		return stepOutcome{}, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	if _, err := reviewdomain.ReconcileTransfer(ctx, sp, command); err != nil {
		_ = sp.Rollback(ctx)
		if errors.Is(err, reviewdomain.ErrTransferCaseUnavailable) {
			return staleTap(step), nil
		}
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: transferDoneStep(label, "", amount), Settled: true, Changed: true}, nil
}

func (p *Processor) applyIgnoreReconciliation(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step) (stepOutcome, error) {
	amount, err := reconciliationAmount(ctx, tx, householdID, review.ItemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return staleTap(step), nil
	}
	if err != nil {
		return stepOutcome{}, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	err = reviewdomain.IgnoreReview(ctx, sp, reviewdomain.IgnoreCommand{HouseholdID: householdID, ActorUserID: review.UserID, ReviewItemID: review.ItemID, ActorType: "TELEGRAM"})
	switch {
	case errors.Is(err, reviewdomain.ErrAlreadyResolved), errors.Is(err, reviewdomain.ErrIgnoreTransactionBound):
		_ = sp.Rollback(ctx)
		return staleTap(step), nil
	case errors.Is(err, reviewdomain.ErrIgnoreNotAllowed):
		_ = sp.Rollback(ctx)
		return reask(step, ignoreNotAllowedAck), nil
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: ignoredStep("", amount), Settled: true, Changed: true}, nil
}
