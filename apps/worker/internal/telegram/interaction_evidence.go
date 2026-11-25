package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

type situation int

const (
	sitNone situation = iota
	sitDuplicate
	sitDispute
	sitEmailEntity
	sitUnsupported
	sitWealth
	sitDocument
	sitQuality
	sitCorrection
)

const (
	duplicateStampKey = "duplicate"
	emailAccountKey   = "fe_account"
	emailWealthKey    = "fe_wealth"
	wealthStampKey    = "wealth_obs"
)

const (
	duplicateAsk          = "Ini sama dengan catatan yang sudah ada?"
	duplicateHint         = "Pilih di bawah."
	duplicateNoneHint     = "Aku tidak menemukan catatan yang mirip lagi. Catat sebagai transaksi baru, atau abaikan."
	duplicateStaleAck     = "Catatan itu sudah berubah, jadi belum ada yang digabung."
	disputeAsk            = "Screenshot ini transaksi yang berbeda?"
	disputeHint           = "Pilih Ya hanya kalau ini memang transaksi lain. Catatan lama tidak diubah."
	disputeMergeActiveAck = "Transaksi ini menjadi tujuan penggabungan yang masih aktif, jadi belum bisa dibatalkan. Pisahkan penggabungannya dulu di halaman Transaksi."
	disputeStaleAck       = "Koreksi ini tidak bisa dilanjutkan: tautan screenshotnya sudah berubah. Cek di Kotak Tinjauan."
	emailAccountAsk       = "Rekening mana yang dipakai di email ini?"
	emailWealthAsk        = "Email ini untuk akun kekayaan yang mana?"
	emailGoneAck          = "Bukti email ini sudah berubah, jadi belum ada yang disimpan."
	noEmailAccountHint    = "Belum ada rekening yang aktif. Tambahkan dulu di aplikasi, lalu pilih lagi."
	noWealthHint          = "Belum ada akun kekayaan yang aktif. Tambahkan dulu di halaman Kekayaan, lalu pilih lagi."
	unsupportedAsk        = "Bukti email ini belum bisa dicatat otomatis."
	unsupportedHint       = "Belum ada transaksi yang dicatat. Abaikan kalau tidak perlu, atau cek di aplikasi."
	conflictAsk           = "Dua bukti tidak sepakat."
	conflictHint          = "Aku tidak menebak mana yang benar, jadi belum ada yang dicatat. Cek di aplikasi, atau abaikan kalau tidak perlu."
	appAsk                = "Ini perlu dicek di aplikasi."
	wealthAsk             = "Saldo ini milik akun yang mana?"
	wealthConfirmAsk      = "Catat saldo ini ke akun yang sudah dikenali?"
	wealthGoneAck         = "Saldo ini sudah berubah, jadi belum ada yang dicatat."
	documentReadAsk       = "Dokumen ini belum terbaca jelas. Proses ulang?"
	documentKindAsk       = "Jenis dokumen ini belum dikenali. Proses ulang?"
	documentHint          = "Proses ulang membaca dokumen ini sekali lagi. Abaikan kalau tidak perlu dicatat."
	documentGoneAck       = "Dokumen ini sudah berubah, jadi belum ada yang diproses."
	qualityAsk            = "Rincian struk tidak cocok dengan total yang tercetak."
	qualityHint           = "Cek totalnya dulu. Pilih Terima total untuk mencatat total yang tercetak."
	correctionAsk         = "Transaksi ini perlu dicek sebelum dicatat."
	correctionHint        = "Pilih Benar kalau datanya sudah betul, atau batalkan transaksinya."
	categoryNeededAck     = "Transaksi ini belum punya kategori."
)

func documentReviewType(reviewType string) bool {
	return reviewType == "DOCUMENT_EXTRACTION_LOW_CONFIDENCE" || reviewType == "DOCUMENT_CLASSIFICATION"
}

func evidenceReviewType(reviewType string) bool {
	switch reviewType {
	case "WEALTH_OBSERVATION_CONFIRMATION", "FINANCIAL_EMAIL_RESOLUTION", "FINANCIAL_EMAIL_FACTS", "CONFLICTING_EVIDENCE", "RECEIPT_MISMATCH", "MANUAL_CORRECTION", "POSSIBLE_DUPLICATE":
		return true
	}
	return documentReviewType(reviewType)
}

func evidenceDisputed(decision reviewdec.Decision) bool {
	return decision.Provenance != nil && decision.Provenance["evidence_link_dispute"] == true
}

func situationOf(decision reviewdec.Decision, reviewType string, subject reviewSubject) situation {
	switch {
	case documentReviewType(reviewType):
		return sitDocument
	case reviewType == "WEALTH_OBSERVATION_CONFIRMATION":
		return sitWealth
	case reviewType == "FINANCIAL_EMAIL_RESOLUTION":
		if contains(decision.MissingFacts, "funding_account") || contains(decision.MissingFacts, "wealth_account") {
			return sitEmailEntity
		}
	case reviewType == "FINANCIAL_EMAIL_FACTS":
		return sitUnsupported
	case reviewType == "RECEIPT_MISMATCH":
		if subject.TransactionType != "" {
			return sitQuality
		}
	case reviewType == "MANUAL_CORRECTION":
		if subject.TransactionType != "" {
			return sitCorrection
		}
	case reviewType == "CONFLICTING_EVIDENCE":
		if subject.TransactionType != "" && contains(decision.MissingFacts, "duplicate_relationship") && contains(cardActions(decision, reviewType), "MERGE_EXISTING") {
			return sitDuplicate
		}
		return sitUnsupported
	case reviewType == "POSSIBLE_DUPLICATE":
		if subject.TransactionType == "" || engineOwnsTransfer(decision, subject) {
			return sitNone
		}
		if evidenceDisputed(decision) {
			return sitDispute
		}
		if contains(decision.MissingFacts, "duplicate_relationship") {
			return sitDuplicate
		}
	}
	return sitNone
}

func (e engineReview) situation() situation {
	return situationOf(e.Decision, e.ReviewType, e.Subject)
}

func ignoreRow(actions []string) [][]InlineKeyboardButton {
	if !contains(actions, "IGNORE") {
		return nil
	}
	return [][]InlineKeyboardButton{{{Text: "Abaikan", CallbackData: callbackIgnore}}}
}

func deriveEvidenceStep(step Step, sit situation, decision reviewdec.Decision, reviewType string) Step {
	actions := cardActions(decision, reviewType)
	step.Kind, step.State, step.Back = stepChoice, "AWAITING_DETAIL", false
	switch sit {
	case sitDuplicate:
		step.Fact, step.Ask, step.Hint = factDuplicate, duplicateAsk, duplicateHint
		step.Rows = duplicateTailRows(actions)
	case sitDispute:
		step.Fact, step.Ask, step.Hint = factDispute, disputeAsk, disputeHint
		step.Rows = [][]InlineKeyboardButton{{{Text: "Ya, transaksi berbeda", CallbackData: callbackDisputeConfirm}}, {{Text: "Batalkan koreksi", CallbackData: callbackDisputeReject}}}
	case sitEmailEntity:
		step.Fact, step.Ask = factEmailAccount, emailAccountAsk
		if !contains(decision.MissingFacts, "funding_account") {
			step.Fact, step.Ask = factEmailWealth, emailWealthAsk
		}
		step.Hint = accountHint
		step.Rows = ignoreRow(actions)
	case sitUnsupported:
		step.Fact, step.Ask, step.Hint = factUnsupported, unsupportedAsk, unsupportedHint
		if reviewType == "CONFLICTING_EVIDENCE" {
			step.Ask, step.Hint = conflictAsk, conflictHint
		}
		step.Rows = ignoreRow(actions)
	case sitWealth:
		step.Fact, step.Ask, step.Hint = factWealthObs, wealthAsk, accountHint
		if known, _ := decision.KnownFacts["wealth_account"].(string); known != "" {
			step.Fact, step.State, step.Ask, step.Hint = factWealthConfirm, "AWAITING_CONFIRMATION", wealthConfirmAsk, "Pilih Ya, catat atau Akun lain."
		}
		step.Rows = ignoreRow(actions)
	case sitDocument:
		step.Fact, step.Ask, step.Hint = factDocument, documentReadAsk, documentHint
		if reviewType == "DOCUMENT_CLASSIFICATION" {
			step.Ask = documentKindAsk
		}
		step.Rows = nil
		if contains(actions, "REPROCESS_DOCUMENT") {
			step.Rows = [][]InlineKeyboardButton{{{Text: "Proses ulang", CallbackData: callbackReprocess}}}
		}
		step.Rows = append(step.Rows, ignoreRow(actions)...)
	case sitQuality:
		step.Fact, step.Ask, step.Hint, step.Defer = factQuality, qualityAsk, qualityHint, true
		var buttons []InlineKeyboardButton
		if contains(actions, "CONFIRM_REVIEW") {
			buttons = append(buttons, InlineKeyboardButton{Text: "Terima total", CallbackData: callbackQuality})
		}
		if contains(actions, "IGNORE") {
			buttons = append(buttons, InlineKeyboardButton{Text: "Abaikan", CallbackData: callbackIgnore})
		}
		step.Rows = pairRows(buttons)
	case sitCorrection:
		step.Fact, step.Ask, step.Hint, step.Defer = factCorrection, correctionAsk, correctionHint, true
		var buttons []InlineKeyboardButton
		if contains(actions, "CONFIRM_REVIEW") {
			buttons = append(buttons, InlineKeyboardButton{Text: "Benar", CallbackData: callbackConfirm})
		}
		if contains(actions, "IGNORE") {
			buttons = append(buttons, InlineKeyboardButton{Text: "Batalkan transaksi", CallbackData: callbackIgnore})
		}
		step.Rows = pairRows(buttons)
	}
	return step
}

func fallbackStep(decision reviewdec.Decision, reviewType, facts string) Step {
	step := Step{Kind: stepChoice, Fact: factUnsupported, Fallback: true, Defer: true, State: "AWAITING_DETAIL", Facts: strings.TrimSpace(facts), Why: reviewdec.WhyLine(decision)}
	step.Ask, step.Hint = appAsk, appReviewHint
	step.Rows = ignoreRow(cardActions(decision, reviewType))
	return step
}

func wealthPickStep(base Step) Step {
	base.Kind, base.Fact, base.State, base.Ask, base.Hint = stepChoice, factWealthObs, "AWAITING_DETAIL", wealthAsk, accountHint
	base.Rows = nil
	return base
}

func disputeCategoryStep(base Step) Step {
	base.Kind, base.Fact, base.State, base.Back, base.Dispute, base.Rows = stepChoice, factCategory, "AWAITING_CATEGORY", false, true, nil
	base.Ask, base.Hint = "Masuk kategori apa?", categoryHint
	return base
}

func evidenceStepForBinding(card agentReviewBinding) (Step, bool) {
	switch {
	case card.Kind == "WEALTH_OBSERVATION" && card.ReviewType == "WEALTH_OBSERVATION_CONFIRMATION":
		if card.ConversationState == "AWAITING_CONFIRMATION" {
			return Step{Kind: stepChoice, Fact: factWealthConfirm}, true
		}
		return Step{Kind: stepChoice, Fact: factWealthObs}, true
	case card.Kind == cardKindEvidence:
		switch {
		case documentReviewType(card.ReviewType):
			return Step{Kind: stepChoice, Fact: factDocument}, true
		case card.ReviewType == "FINANCIAL_EMAIL_RESOLUTION":
			return Step{Kind: stepChoice, Fact: factEmailAccount}, true
		}
		return Step{Kind: stepChoice, Fact: factUnsupported}, true
	}
	return Step{}, false
}

type duplicateRecord struct {
	ID, Label, Amount string
	At                time.Time
}

func (r duplicateRecord) line() string {
	return r.Label + " · Rp" + FormatIDR(r.Amount) + " · " + reviewdomain.FormatIndonesianDate(r.At.In(jakartaLocation()))
}

type duplicateView struct {
	HouseholdID string
	Source      duplicateRecord
	Candidates  []duplicateRecord
	Actions     []string
}

func loadDuplicateView(ctx context.Context, tx pgx.Tx, reviewID string) (duplicateView, error) {
	var view duplicateView
	var transactionID, reviewType string
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT r.household_id::text,COALESCE(r.transaction_id::text,''),ri.review_type,COALESCE(ri.decision,'{}'::jsonb)
		FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1`, reviewID).Scan(&view.HouseholdID, &transactionID, &reviewType, &raw); err != nil {
		return view, err
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return view, err
	}
	view.Actions = cardActions(decision, reviewType)
	if transactionID == "" {
		return view, nil
	}
	var kind, currency, amount string
	view.Source.ID = transactionID
	if err := tx.QueryRow(ctx, `SELECT t.type::text,t.currency,t.amount::text,round(t.amount)::text,COALESCE(NULLIF(m.normalized_name,''),NULLIF(t.counterparty_name,''),NULLIF(t.description,''),'Transaksi'),t.transaction_at
		FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id AND m.household_id=t.household_id
		WHERE t.id=$1 AND t.household_id=$2`, transactionID, view.HouseholdID).Scan(&kind, &currency, &amount, &view.Source.Amount, &view.Source.Label, &view.Source.At); err != nil {
		return view, err
	}
	rows, err := tx.Query(ctx, `WITH prior AS (SELECT jsonb_array_elements_text(ri.decision->'knownFacts'->'prior_match_transaction_ids') AS id FROM review_item ri WHERE ri.household_id=$1 AND ri.transaction_id=$2 AND jsonb_typeof(ri.decision->'knownFacts'->'prior_match_transaction_ids')='array')
		SELECT t.id::text,COALESCE(NULLIF(m.normalized_name,''),NULLIF(t.counterparty_name,''),NULLIF(t.description,''),'Transaksi'),round(t.amount)::text,t.transaction_at
		FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id AND m.household_id=t.household_id
		WHERE t.household_id=$1 AND t.id<>$2 AND (t.status='CONFIRMED' OR (t.status='NEEDS_REVIEW' AND t.id::text IN (SELECT id FROM prior)))
		  AND ((t.type=$3 AND t.currency=$4 AND t.amount=$5::numeric AND t.transaction_at BETWEEN $6::timestamptz-interval '72 hours' AND $6::timestamptz+interval '72 hours') OR (t.type=$3 AND t.currency=$4 AND t.id::text IN (SELECT id FROM prior)))
		ORDER BY (t.id::text IN (SELECT id FROM prior)) DESC,abs(extract(epoch FROM (t.transaction_at-$6::timestamptz))),t.id`, view.HouseholdID, transactionID, kind, currency, amount, view.Source.At)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		var candidate duplicateRecord
		if err := rows.Scan(&candidate.ID, &candidate.Label, &candidate.Amount, &candidate.At); err != nil {
			return view, err
		}
		view.Candidates = append(view.Candidates, candidate)
	}
	return view, rows.Err()
}

func (v duplicateView) choices() []accountChoice {
	out := make([]accountChoice, 0, len(v.Candidates))
	for _, candidate := range v.Candidates {
		out = append(out, accountChoice{ID: candidate.ID, Name: candidate.Label})
	}
	return out
}

func (v duplicateView) mergeAllowed() bool { return contains(v.Actions, "MERGE_EXISTING") }

func duplicateTailRows(actions []string) [][]InlineKeyboardButton {
	var buttons []InlineKeyboardButton
	if contains(actions, "CONFIRM_REVIEW") || contains(actions, "CONFIRM_NEW_TRANSFER") {
		buttons = append(buttons, InlineKeyboardButton{Text: "Beda, catat baru", CallbackData: callbackDupNew})
	}
	if contains(actions, "IGNORE") {
		buttons = append(buttons, InlineKeyboardButton{Text: "Abaikan", CallbackData: callbackIgnore})
	}
	return pairRows(buttons)
}

func (s *Step) loadDuplicateRows(ctx context.Context, tx pgx.Tx, reviewID string, page int) error {
	view, err := loadDuplicateView(ctx, tx, reviewID)
	if err != nil {
		return err
	}
	candidates := view.Candidates
	if !view.mergeAllowed() {
		candidates = nil
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	version, err := stampList(ctx, tx, reviewID, duplicateStampKey, ids, s.ReadOnly)
	if err != nil {
		return err
	}
	s.Page = clampChooserPage(page, len(candidates), chooserPageSize)
	start := s.Page * chooserPageSize
	end := min(start+chooserPageSize, len(candidates))
	lines := []string{"Baru: " + view.Source.line()}
	s.Rows = nil
	for index := start; index < end; index++ {
		label, prefix := "Sama, gabungkan", "Sudah ada: "
		if len(candidates) > 1 {
			label = fmt.Sprintf("Sama, gabungkan %d", index+1)
			prefix = fmt.Sprintf("Sudah ada (%d): ", index+1)
		}
		lines = append(lines, prefix+candidates[index].line())
		s.Rows = append(s.Rows, []InlineKeyboardButton{{Text: label, CallbackData: stampedData(callbackDupMerge, version, index)}})
	}
	if pager := pagerRow(s.Page, len(candidates), chooserPageSize, callbackDupPg); len(pager) > 0 {
		s.Rows = append(s.Rows, pager)
	}
	s.Rows = append(s.Rows, duplicateTailRows(view.Actions)...)
	s.Facts = strings.Join(lines, "\n")
	s.Hint = duplicateHint
	switch {
	case len(candidates) == 0:
		s.Hint = duplicateNoneHint
	case s.Page > 0:
		s.Hint = fmt.Sprintf("Pilih di bawah (halaman %d).", s.Page+1)
	}
	return nil
}

func emailChoices(ctx context.Context, tx pgx.Tx, householdID string, fact factKind) ([]accountChoice, error) {
	table := "account"
	if fact == factEmailWealth || fact == factWealthObs {
		table = "wealth_account"
	}
	rows, err := tx.Query(ctx, `SELECT id::text,name FROM `+table+` WHERE household_id=$1 AND active ORDER BY name,id`, householdID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (accountChoice, error) {
		var choice accountChoice
		err := row.Scan(&choice.ID, &choice.Name)
		return choice, err
	})
}

type stampSpec struct {
	Key, Prefix, PagePrefix, Empty string
}

func stampSpecFor(fact factKind) stampSpec {
	switch fact {
	case factEmailWealth:
		return stampSpec{emailWealthKey, callbackEmailWealth, callbackEmailPg, noWealthHint}
	case factWealthObs:
		return stampSpec{wealthStampKey, callbackWealthAccount, callbackWealthPg, noWealthHint}
	}
	return stampSpec{emailAccountKey, callbackEmailAccount, callbackEmailPg, noEmailAccountHint}
}

func (s *Step) loadEntityRows(ctx context.Context, tx pgx.Tx, reviewID string, page int) error {
	var householdID string
	if err := tx.QueryRow(ctx, `SELECT household_id::text FROM review_request WHERE id=$1`, reviewID).Scan(&householdID); err != nil {
		return err
	}
	choices, err := emailChoices(ctx, tx, householdID, s.Fact)
	if err != nil {
		return err
	}
	spec := stampSpecFor(s.Fact)
	if err := s.loadStampedAccounts(ctx, tx, reviewID, spec.Key, choices, page, spec.Prefix, spec.PagePrefix, spec.Empty); err != nil {
		return err
	}
	var raw []byte
	var reviewType string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(ri.decision,'{}'::jsonb),ri.review_type FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1`, reviewID).Scan(&raw, &reviewType); err != nil {
		return err
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return err
	}
	s.Rows = append(s.Rows, ignoreRow(cardActions(decision, reviewType))...)
	return nil
}

func (s *Step) loadWealthConfirmRows(ctx context.Context, tx pgx.Tx, reviewID string) error {
	var name string
	err := tx.QueryRow(ctx, `SELECT w.name FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id
		JOIN wealth_account w ON w.id=NULLIF(ri.decision->'knownFacts'->>'wealth_account','')::uuid AND w.household_id=r.household_id AND w.active
		WHERE r.id=$1`, reviewID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		*s = wealthPickStep(*s)
		return s.loadEntityRows(ctx, tx, reviewID, 0)
	}
	if err != nil {
		return err
	}
	line := "Akun yang dikenali: " + clean(name, 60)
	if !strings.Contains(s.Facts, line) {
		s.Facts = strings.TrimSpace(s.Facts + "\n" + line)
	}
	rows := [][]InlineKeyboardButton{{{Text: "Ya, catat", CallbackData: callbackConfirm}, {Text: "Akun lain", CallbackData: callbackChange}}}
	s.Rows = append(rows, s.Rows...)
	return nil
}

func documentTypeLabel(documentType string) string {
	switch documentType {
	case "RECEIPT":
		return "Struk"
	case "PAYSLIP":
		return "Slip gaji"
	case "BANK_TRANSACTION_SCREENSHOT":
		return "Screenshot transaksi bank"
	case "TRANSFER_PROOF":
		return "Bukti transfer"
	case "EWALLET_SCREENSHOT":
		return "Screenshot e-wallet"
	case "BILL_OR_INVOICE":
		return "Tagihan atau invoice"
	case "TRANSACTION_HISTORY_SCREENSHOT":
		return "Riwayat transaksi"
	case "WEALTH_OBSERVATION":
		return "Saldo atau nilai aset"
	case "OTHER_FINANCIAL_DOCUMENT":
		return "Dokumen keuangan lain"
	}
	return ""
}

func documentSummary(ctx context.Context, tx pgx.Tx, householdID, documentID string) (string, error) {
	var documentType string
	var pages int
	err := tx.QueryRow(ctx, `SELECT d.document_type,(SELECT count(*) FROM document_page dp WHERE dp.document_id=d.id) FROM document d WHERE d.id=$1 AND d.household_id=$2`, documentID, householdID).Scan(&documentType, &pages)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var lines []string
	if label := documentTypeLabel(documentType); label != "" {
		lines = append(lines, "Dokumen: "+label)
	}
	if pages > 1 {
		lines = append(lines, fmt.Sprintf("Halaman: %d", pages))
	}
	return strings.Join(lines, "\n"), nil
}

func financialEmailSummary(ctx context.Context, tx pgx.Tx, householdID, observationID string) (string, error) {
	var amount, funding, provider, occurred string
	err := tx.QueryRow(ctx, `SELECT COALESCE(facts_json->>'amount_idr',''),COALESCE(facts_json->>'funding_account_hint',''),COALESCE(facts_json->>'provider_account_hint',''),COALESCE(facts_json->>'occurred_at','')
		FROM financial_email_observation WHERE id=$1 AND household_id=$2`, observationID, householdID).Scan(&amount, &funding, &provider, &occurred)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var lines []string
	if digitsOnly(amount) {
		lines = append(lines, "Nominal: Rp"+FormatIDR(amount))
	}
	if at, err := time.Parse(time.RFC3339, occurred); err == nil {
		lines = append(lines, "Tanggal: "+reviewdomain.FormatIndonesianDate(at.In(jakartaLocation())))
	}
	if funding = strings.TrimSpace(funding); funding != "" {
		lines = append(lines, "Rekening di email: "+clean(funding, 60))
	}
	if provider = strings.TrimSpace(provider); provider != "" {
		lines = append(lines, "Akun kekayaan di email: "+clean(provider, 60))
	}
	return strings.Join(lines, "\n"), nil
}

func digitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func entityFromText(ctx context.Context, tx pgx.Tx, householdID, hint string, targeted bool, choices []accountChoice, resolve func(context.Context, pgx.Tx, string, string) (string, error)) (string, bool, error) {
	if targeted {
		id, err := resolve(ctx, tx, householdID, hint)
		if err != nil {
			return "", false, err
		}
		return id, id != "" && accountListed(choices, id), nil
	}
	exact, ok := exactAccountMatch(hint, choices)
	return exact.ID, ok, nil
}

func (p *Processor) entityCandidatesFor(ctx context.Context, tx pgx.Tx, householdID string, card agentReviewBinding, fact factKind) ([]accountChoice, error) {
	if fact == factEmailAccount || fact == factEmailWealth {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT COALESCE(decision->'missingFacts','[]'::jsonb) FROM review_item WHERE id=$1 AND household_id=$2`, card.ReviewItemID, householdID).Scan(&raw); err != nil {
			return nil, err
		}
		var missing []string
		if err := json.Unmarshal(raw, &missing); err != nil {
			return nil, err
		}
		fact = factEmailAccount
		if !contains(missing, "funding_account") {
			fact = factEmailWealth
		}
	}
	return emailChoices(ctx, tx, householdID, fact)
}
