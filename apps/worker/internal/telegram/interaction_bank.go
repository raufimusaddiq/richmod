package telegram

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

const (
	bankReviewType     = "UNKNOWN_BANK_TEMPLATE"
	bankTimeAsk        = "Transaksinya terjadi kapan?"
	bankConfirmAsk     = "Catat transaksi ini?"
	bankConfirmHint    = "Nominal dan waktunya sudah terbaca dari email. Pilih di bawah."
	bankAccountAsk     = "Email ini untuk rekening yang mana?"
	bankAccountHint    = "Pilih di bawah, atau ketik nama rekeningnya."
	bankNoAccountsHint = "Email bank ini belum terhubung ke rekening, dan belum ada rekening yang aktif. Tambahkan dulu di aplikasi."
	bankWhy            = "Aku belum bisa membaca isi email bank ini."
	bankClosedAck      = "Kartu ini sudah berubah, jadi belum ada yang dicatat. Lanjutkan di " + reviewInboxName + " di aplikasi."
	bankAccountMissAck = "Aku belum menemukan satu rekening yang cocok."
)

type bankSubject struct {
	SourceEventID string
	Subject       string
	Sender        string
	AmountIDR     string
	TransactionAt string
	EmailAt       time.Time
	Unlinked      bool
	Queued        bool
}

func tidyEmailText(value string, limit int) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
	return clean(strings.Join(strings.Fields(cleaned), " "), limit)
}

func loadBankSubject(ctx context.Context, tx pgx.Tx, reviewID string) (*bankSubject, error) {
	var b bankSubject
	var emailDate string
	var received time.Time
	err := tx.QueryRow(ctx, `SELECT s.id::text,COALESCE(m.subject,''),COALESCE(l.sender_address,''),COALESCE(m.email_date,''),s.received_at,
			COALESCE(e.output_json->>'amount_idr',''),COALESCE(e.output_json->>'transaction_at',''),COALESCE(l.account_id::text,'')='',
			COALESCE(c.context_json->'bank_pending'->>'queued','')='true'
		FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		JOIN source_event s ON s.id=ri.source_event_id AND s.household_id=ri.household_id
		JOIN bank_email_event m ON m.source_event_id=s.id
		JOIN bank_email_extraction e ON e.source_event_id=s.id
		JOIN bank_email_listener l ON l.id=e.listener_id AND l.household_id=s.household_id
		LEFT JOIN review_conversation c ON c.review_request_id=r.id
		WHERE r.id=$1 AND ri.review_type='UNKNOWN_BANK_TEMPLATE' AND ri.transaction_id IS NULL`, reviewID).
		Scan(&b.SourceEventID, &b.Subject, &b.Sender, &emailDate, &received, &b.AmountIDR, &b.TransactionAt, &b.Unlinked, &b.Queued)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.Subject, b.Sender = tidyEmailText(b.Subject, 120), tidyEmailText(b.Sender, 80)
	b.EmailAt = received
	if parsed, parseErr := mail.ParseDate(strings.TrimSpace(emailDate)); parseErr == nil && !parsed.After(received.Add(24*time.Hour)) {
		b.EmailAt = parsed
	}
	return &b, nil
}

func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func bankMomentLabel(value string) string {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	return bankInstantLabel(at)
}

func bankFacts(b bankSubject, pending bankPending) string {
	var lines []string
	if b.Subject != "" {
		lines = append(lines, "Email: "+b.Subject)
	}
	if b.Sender != "" {
		lines = append(lines, "Dari: "+b.Sender)
	}
	if amount := firstText(b.AmountIDR, pending.AmountIDR); amount != "" {
		lines = appendFact(lines, "Nominal", IDR(amount))
	}
	if label := bankMomentLabel(firstText(b.TransactionAt, pending.TransactionAt)); label != "" {
		lines = append(lines, "Waktu: "+label)
	}
	return strings.Join(lines, "\n")
}

func bankStepFor(base Step, b bankSubject, pending bankPending, actions []string) Step {
	base.Facts, base.Why = bankFacts(b, pending), bankWhy
	amount, at := firstText(b.AmountIDR, pending.AmountIDR), firstText(b.TransactionAt, pending.TransactionAt)
	switch {
	case amount == "":
		return amountStepFrom(base, actions)
	case at == "":
		var extra []InlineKeyboardButton
		if !b.EmailAt.IsZero() {
			extra = append(extra, InlineKeyboardButton{Text: "Waktu email", CallbackData: callbackDateEmail})
		}
		return dateStepFrom(base, bankTimeAsk, actions, extra...)
	}
	base.Back = false
	base.Rows = nil
	if b.Unlinked {
		base.Kind, base.Fact, base.State = stepChoice, factBankAccount, "AWAITING_BANK_ACCOUNT"
		base.Ask, base.Hint = bankAccountAsk, bankAccountHint
		if contains(actions, "IGNORE") {
			base.Rows = [][]InlineKeyboardButton{{{Text: "Abaikan", CallbackData: callbackIgnore}}}
		}
		return base
	}
	base.Kind, base.Fact, base.State = stepChoice, factBankConfirm, "AWAITING_CONFIRMATION"
	base.Ask, base.Hint = bankConfirmAsk, bankConfirmHint
	buttons := []InlineKeyboardButton{{Text: "Catat", CallbackData: callbackConfirm}}
	if contains(actions, "IGNORE") {
		buttons = append(buttons, InlineKeyboardButton{Text: "Abaikan", CallbackData: callbackIgnore})
	}
	base.Rows = pairRows(buttons)
	return base
}

func deriveBankStep(step Step, decision reviewdec.Decision, subject reviewSubject) Step {
	return bankStepFor(step, *subject.Bank, bankPending{}, cardActions(decision, bankReviewType))
}

func (p *Processor) bankStep(ctx context.Context, tx pgx.Tx, e engineReview, base Step) (Step, error) {
	pending, err := loadBankPending(ctx, tx, e.RequestID)
	if err != nil {
		return Step{}, err
	}
	return bankStepFor(base, *e.Subject.Bank, pending, cardActions(e.Decision, e.ReviewType)), nil
}

func (s *Step) loadBankAccountRows(ctx context.Context, tx pgx.Tx, reviewID string, page int) error {
	choices, err := bankAccountChoices(ctx, tx, reviewID)
	if err != nil {
		return err
	}
	s.Page = clampChooserPage(page, len(choices), chooserPageSize)
	s.Hint = bankAccountHint
	if len(choices) == 0 {
		s.Hint = bankNoAccountsHint
	}
	s.Rows = nil
	if len(choices) > 0 {
		s.Rows = bankAccountChoicesMarkup(choices, s.Page).InlineKeyboard
	}
	s.Rows = append(s.Rows, []InlineKeyboardButton{{Text: "Abaikan", CallbackData: callbackIgnore}})
	return nil
}

func bankAccountChoices(ctx context.Context, tx pgx.Tx, reviewID string) ([]reviewdomain.BankAccountChoice, error) {
	var householdID, itemID, sourceID string
	if err := tx.QueryRow(ctx, `SELECT r.household_id::text,ri.id::text,COALESCE(ri.source_event_id::text,'') FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1`, reviewID).Scan(&householdID, &itemID, &sourceID); err != nil {
		return nil, err
	}
	choices, err := reviewdomain.ListBankSourceAccountChoices(ctx, tx, householdID, itemID, sourceID, 0)
	if errors.Is(err, reviewdomain.ErrBankReviewUnavailable) {
		return nil, nil
	}
	return choices, err
}

func bankAccountChoicesMarkup(accounts []reviewdomain.BankAccountChoice, page int) *InlineKeyboardMarkup {
	page = clampChooserPage(page, len(accounts), chooserPageSize)
	start := page * chooserPageSize
	end := min(start+chooserPageSize, len(accounts))
	buttons := make([][]InlineKeyboardButton, 0, end-start+1)
	for _, account := range accounts[start:end] {
		buttons = append(buttons, []InlineKeyboardButton{{Text: clean(account.Name, 28), CallbackData: callbackBankAccount + account.ID}})
	}
	if pager := pagerRow(page, len(accounts), chooserPageSize, callbackBankPg); len(pager) > 0 {
		buttons = append(buttons, pager)
	}
	return &InlineKeyboardMarkup{InlineKeyboard: buttons}
}

func (p *Processor) saveBankAmount(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, amount string) (stepOutcome, error) {
	pending, err := loadBankPending(ctx, tx, review.RequestID)
	if err != nil {
		return stepOutcome{}, err
	}
	pending.AmountIDR = amount
	if err := saveBankPending(ctx, tx, review.RequestID, pending); err != nil {
		return stepOutcome{}, err
	}
	return p.bankAdvance(ctx, tx, householdID, review, step, pending, "✅ Nominal "+IDR(amount)+" diterima.")
}

func (p *Processor) finishBankTime(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, at time.Time) (stepOutcome, error) {
	pending, err := loadBankPending(ctx, tx, review.RequestID)
	if err != nil {
		return stepOutcome{}, err
	}
	pending.TransactionAt = at.In(jakartaLocation()).Format(time.RFC3339)
	if err := saveBankPending(ctx, tx, review.RequestID, pending); err != nil {
		return stepOutcome{}, err
	}
	return p.bankAdvance(ctx, tx, householdID, review, step, pending, "")
}

func (p *Processor) bankAdvance(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, pending bankPending, ack string) (stepOutcome, error) {
	b := review.Subject.Bank
	amount, at := firstText(b.AmountIDR, pending.AmountIDR), firstText(b.TransactionAt, pending.TransactionAt)
	if amount != "" && at != "" {
		outcome, completed, err := p.completeBank(ctx, tx, householdID, review, step, pending, amount, at)
		if err != nil || completed {
			return outcome, err
		}
	}
	next, ok, err := p.currentStep(ctx, tx, householdID, review)
	if err != nil {
		return stepOutcome{}, err
	}
	if !ok {
		return staleTap(step), nil
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state=$2,last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.RequestID, next.State); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: next.WithAck(ack), Changed: true}, nil
}

func (p *Processor) completeBank(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, pending bankPending, amount, at string) (stepOutcome, bool, error) {
	command := reviewdomain.BankFactCommand{HouseholdID: householdID, UserID: review.UserID, ReviewItemID: review.ItemID, SourceEventID: review.Subject.Bank.SourceEventID, AmountIDR: amount, TransactionAt: at}
	moment, parseErr := time.Parse(time.RFC3339Nano, at)
	if parseErr != nil {
		return stepOutcome{Step: Step{Kind: stepDone, Ack: bankClosedAck}, Settled: true, Changed: true}, true, nil
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, false, err
	}
	err = enqueueBankFactsCompletion(ctx, sp, command, review.ChatID)
	switch {
	case errors.Is(err, reviewdomain.ErrBankSourceUnlinked):
		_ = sp.Rollback(ctx)
		return stepOutcome{}, false, nil
	case errors.Is(err, reviewdomain.ErrBankReviewUnavailable), errors.Is(err, reviewdomain.ErrAlreadyResolved):
		_ = sp.Rollback(ctx)
		return staleTap(step), true, nil
	case errors.Is(err, reviewdomain.ErrBankFactsRequired), errors.Is(err, reviewdomain.ErrAccountInvalid):
		_ = sp.Rollback(ctx)
		return stepOutcome{Step: Step{Kind: stepDone, Ack: bankClosedAck}, Settled: true, Changed: true}, true, nil
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, false, err
	}
	pending.Queued = true
	if err := saveBankPending(ctx, sp, review.RequestID, pending); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, false, err
	}
	if _, err := sp.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'QUEUE_BANK_FACTS','review_item',$3,jsonb_build_object('amount_idr',$4::text,'transaction_at',$5::timestamptz))`, householdID, review.UserID, review.ItemID, amount, moment); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, false, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, false, err
	}
	done := Step{Kind: stepDone, Ack: "✅ Diterima: " + joinDot(IDR(amount), bankMomentLabel(at)) + ". Transaksinya sedang dicatat."}
	if err := refreshSiblingCards(ctx, tx, review.RequestID, review.ChatID, done); err != nil {
		return stepOutcome{}, false, err
	}
	return stepOutcome{Step: done, Settled: true, Changed: true}, true, nil
}

func (p *Processor) applyBankTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	if page, ok := parsePageCallback(data, callbackBankPg); ok {
		if step.Fact != factBankAccount {
			return reask(step, choiceStaleAck), nil
		}
		if err := step.loadBankAccountRows(ctx, tx, review.RequestID, page); err != nil {
			return stepOutcome{}, err
		}
		return stepOutcome{Step: step.executable(cardActions(review.Decision, review.ReviewType))}, nil
	}
	if !stepOffers(step, data) {
		return reask(step, choiceStaleAck), nil
	}
	switch {
	case data == callbackIgnore:
		return p.ignoreBank(ctx, tx, householdID, review, step)
	case step.Fact == factDate:
		return p.applyDateTap(ctx, tx, householdID, sourceEventID, review, step, data)
	case step.Fact == factBankConfirm && data == callbackConfirm:
		pending, err := loadBankPending(ctx, tx, review.RequestID)
		if err != nil {
			return stepOutcome{}, err
		}
		return p.bankAdvance(ctx, tx, householdID, review, step, pending, "")
	case step.Fact == factBankAccount && strings.HasPrefix(data, callbackBankAccount):
		return p.chooseBankAccount(ctx, tx, householdID, review, step, strings.TrimPrefix(data, callbackBankAccount))
	}
	return reask(step, choiceStaleAck), nil
}

func (p *Processor) applyBankAccountText(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, text string, targeted bool) (stepOutcome, error) {
	choices, err := bankAccountChoices(ctx, tx, review.RequestID)
	if err != nil {
		return stepOutcome{}, err
	}
	want := normalizeAnswer(text)
	var found []string
	for _, choice := range choices {
		name := normalizeAnswer(choice.Name)
		if want == "" {
			continue
		}
		if name == want {
			found = []string{choice.ID}
			break
		}
		if targeted && strings.Contains(name, want) {
			found = append(found, choice.ID)
		}
	}
	if len(found) != 1 {
		return reask(step, bankAccountMissAck), nil
	}
	return p.chooseBankAccount(ctx, tx, householdID, review, step, found[0])
}

func (p *Processor) chooseBankAccount(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, accountID string) (stepOutcome, error) {
	choices, err := bankAccountChoices(ctx, tx, review.RequestID)
	if err != nil {
		return stepOutcome{}, err
	}
	listed := false
	for _, choice := range choices {
		listed = listed || choice.ID == accountID
	}
	if !listed {
		return reask(step, choiceStaleAck), nil
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	err = reviewdomain.BindBankSourceAccount(ctx, sp, householdID, review.ItemID, review.Subject.Bank.SourceEventID, accountID)
	if errors.Is(err, reviewdomain.ErrBankReviewUnavailable) || errors.Is(err, reviewdomain.ErrBankAccountInvalid) {
		_ = sp.Rollback(ctx)
		return reask(step, choiceStaleAck), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	pending, err := loadBankPending(ctx, tx, review.RequestID)
	if err != nil {
		return stepOutcome{}, err
	}
	return p.bankAdvance(ctx, tx, householdID, review, step, pending, "")
}

func (p *Processor) ignoreBank(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step) (stepOutcome, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	err = reviewdomain.IgnoreBankReview(ctx, sp, reviewdomain.BankFactCommand{HouseholdID: householdID, UserID: review.UserID, ReviewItemID: review.ItemID}, "TELEGRAM")
	if errors.Is(err, reviewdomain.ErrBankReviewUnavailable) {
		_ = sp.Rollback(ctx)
		return staleTap(step), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: Step{Kind: stepDone, Ack: "🗑️ Diabaikan: email bank. Tidak dicatat."}, Settled: true, Changed: true}, nil
}

func enqueueBankFactsCompletion(ctx context.Context, tx pgx.Tx, command reviewdomain.BankFactCommand, chatID int64) error {
	if err := reviewdomain.ValidateBankSourceAccount(ctx, tx, command); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('COMPLETE_BANK_REVIEW',jsonb_build_object('source_event_id',$1::uuid,'review_id',$2::uuid,'amount_idr',$3::text,'transaction_at',$4::text,'telegram_chat_id',$5::bigint,'user_id',NULLIF($6,'')::uuid))`, command.SourceEventID, command.ReviewItemID, command.AmountIDR, command.TransactionAt, chatID, command.UserID)
	return err
}
