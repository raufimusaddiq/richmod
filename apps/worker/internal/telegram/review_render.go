package telegram

// review_render.go renders the stored ReviewDecision for Telegram, following missing_facts/allowed_actions.
// One card shape (ADR-046): title, facts, one Indonesian reason line, and an instruction matching buttons.

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

type reviewCard struct {
	State       string
	Mode        string
	Title       string
	Facts       string
	Why         string
	Instruction string
}

func (c reviewCard) Text() string {
	return reviewdomain.TelegramCardText(c.Title, c.Facts, c.Why, c.Instruction)
}

type reviewSubject struct {
	TransactionType string
	// Reconciliation reports a source-bound transfer with an open case, whose chooser is built from its candidates.
	Reconciliation bool
}

func (s reviewSubject) transactionType(decision reviewdec.Decision) string {
	if s.TransactionType != "" {
		return s.TransactionType
	}
	if kind, ok := decision.KnownFacts["type"].(string); ok {
		return kind
	}
	return ""
}

func loadReviewSubject(ctx context.Context, tx pgx.Tx, reviewID string) (reviewSubject, error) {
	var subject reviewSubject
	err := tx.QueryRow(ctx, `SELECT COALESCE(t.type,''),
		ri.transaction_id IS NULL AND EXISTS(SELECT 1 FROM transfer_reconciliation_case trc WHERE trc.household_id=ri.household_id AND trc.status='OPEN' AND ri.financial_email_observation_id IS NULL AND trc.source_event_id=ri.source_event_id AND trc.financial_email_observation_id IS NULL)
		FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id
		LEFT JOIN transaction t ON t.id=COALESCE(r.transaction_id,ri.transaction_id) AND t.household_id=r.household_id
		WHERE r.id=$1`, reviewID).Scan(&subject.TransactionType, &subject.Reconciliation)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewSubject{}, nil
	}
	return subject, err
}

// Re-renders a live card after a partial answer with the first send's builder, so every kind keeps its
// remaining actions (including Abaikan).
func refreshReviewFactsProjection(ctx context.Context, tx pgx.Tx, household, request string, update telegramUpdate) error {
	var raw []byte
	var reason, itemID string
	if err := tx.QueryRow(ctx, `SELECT ri.decision,ri.review_type,ri.id::text FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1 AND r.household_id=$2 AND ri.status IN ('OPEN','PENDING_SEND')`, request, household).Scan(&raw, &reason, &itemID); err != nil {
		return err
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return err
	}
	summary, err := reviewItemSummary(ctx, tx, household, itemID)
	if err != nil {
		return err
	}
	subject, err := loadReviewSubject(ctx, tx, request)
	if err != nil {
		return err
	}
	card := buildReviewCard(decision, reason, summary, subject)
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state=$2,last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, request, card.State); err != nil {
		return err
	}
	markup, err := reviewCardMarkup(ctx, tx, request, reason, decision, card, subject)
	if err != nil {
		return err
	}
	if markup == nil {
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
	}
	return enqueueReviewUpdateWithMarkup(ctx, tx, request, update, card.Text(), markup)
}

// unknownReviewPrompt is the honest fallback: with no decision contract Go asks for detail instead of
// inventing a category prompt.
const unknownReviewPrompt = "🟡 Perlu detail transaksi"

func renderReviewPresentation(decision reviewdec.Decision, reviewType, context string) (state, reviewMessage, markupMode string) {
	card := buildReviewCard(decision, reviewType, context, reviewSubject{})
	return card.State, card.Text(), card.Mode
}

// buildReviewCard never introduces a fact the decision did not name: the missing fact decides the question,
// the allowed actions the buttons.
func buildReviewCard(decision reviewdec.Decision, reviewType, facts string, subject reviewSubject) reviewCard {
	card := reviewCard{State: "AWAITING_DETAIL", Facts: strings.TrimSpace(facts), Why: reviewdec.WhyLine(decision)}
	kind := subject.transactionType(decision)
	switch {
	case decision.ReasonCode == "MISSING_AMOUNT":
		card.Mode, card.Title = "reply", "🟡 Nominal transaksi belum terbaca"
		card.Instruction = "Balas pesan ini (Reply) dengan nominal dalam rupiah, tanpa pemisah. Contoh: 75000."
		if contains(decision.MissingFacts, "transfer_relationship") {
			card.Instruction += " Setelah itu, konfirmasi apakah ini penghasilan atau transfer sendiri."
		}
	case decision.ReasonCode == "RECEIPT_MISMATCH" && decision.Consequence == reviewdec.QualitySignal && len(decision.MissingFacts) == 0:
		card.Mode, card.Title = "receipt_quality", "🧾 Rincian struk berbeda dari total yang tercetak"
		card.Instruction = "Cek totalnya dulu. Pilih Terima total untuk mencatat total yang tercetak."
	case decision.ReasonCode == "FINANCIAL_EMAIL_FACTS":
		// No transaction was written; the only bounded action is to acknowledge, so no fact is needed.
		card.Mode, card.Title = "financial_email_facts", "🟡 Bukti email belum pasti"
		card.Instruction = "Bukti email ini belum didukung. Pilih Abaikan atau buka Kotak Tinjauan untuk memeriksanya."
	case contains(decision.AllowedActions, "REPROCESS_DOCUMENT"):
		card.Mode, card.Title = "document", "🟡 Dokumen belum terbaca jelas"
		if decision.ReasonCode == "DOCUMENT_CLASSIFICATION" {
			card.Title = "🟡 Jenis dokumen belum dikenali"
		}
		card.Instruction = "Pilih Proses ulang untuk membaca dokumen ini lagi."
	case contains(decision.AllowedActions, "SET_FINANCIAL_EMAIL_ENTITIES"):
		card.Mode, card.Title = "financial_email", promptTitle(decision)
		card.Instruction = "Pilih rekening untuk bukti email ini."
	case contains(decision.MissingFacts, "salary_classification") && contains(decision.AllowedActions, "PRIMARY_SALARY") && contains(decision.AllowedActions, "ORDINARY_INCOME"):
		card.Mode, card.Title = "salary", "🧾 Ini gaji utama atau pemasukan biasa?"
		card.Instruction = "Pilih jenis pemasukan di bawah."
	case contains(decision.MissingFacts, "wealth_account"):
		card.Mode, card.Title = "transfer", "🟡 Akun kekayaan perlu diganti"
		card.Instruction = "Pilih kembali akun kekayaan aktif untuk jenis transfer yang sudah dipilih."
	case contains(decision.MissingFacts, "transaction_at"):
		card.State, card.Mode, card.Title = "AWAITING_DATE", "reply", promptTitle(decision)
		card.Instruction = dateInstruction(decision)
	case isCategoryOnly(decision) && kind == "INCOME":
		// An income has no expense category; the bounded question is whether it is income at all (Benar/Ubah).
		card.State, card.Mode, card.Title = "AWAITING_CATEGORY", "income", "🟡 Pemasukan perlu dicek"
		card.Instruction = "Pilih Benar jika ini penghasilan, atau Ubah jika ini transfer."
	case isCategoryOnly(decision):
		// Category is the single unresolved fact with bounded values, so the chooser is the whole interaction.
		card.State, card.Mode, card.Title = "AWAITING_CATEGORY", "category", "🟡 Kategori belum pasti"
		card.Instruction = "Pilih kategori di bawah, atau balas pesan ini (Reply) dengan tujuan pengeluaran."
	case contains(decision.MissingFacts, "transfer_relationship"):
		card.Mode, card.Title = "transfer", "🟡 Jenis transfer belum pasti"
		card.Instruction = transferInstruction(kind, subject.Reconciliation)
	case decision.InteractionMode == reviewdec.ModeConflictResolution || contains(decision.MissingFacts, "duplicate_relationship"):
		card.Mode, card.Title = "duplicate", promptTitle(decision)
		card.Instruction = replyInstruction(decision)
	case contains(decision.MissingFacts, "merchant"):
		card.State, card.Mode, card.Title = "AWAITING_MERCHANT", "reply", "🟡 Nama merchant belum ada"
		card.Instruction = "Balas pesan ini (Reply) dengan nama merchant."
		if merchantAssetOption(decision, reviewType) {
			card.Instruction += " Jika ini pembelian aset, pilih Beli aset."
		}
	case decision.ReasonCode == "CYCLE_RESIDUAL_ALLOCATION":
		card.Mode, card.Title = "cycle", reviewdomain.CycleResidualCardTitle
		card.Instruction = reviewdomain.CycleResidualCardReplyInstruction
		if contains(cardActions(decision, reviewType), "LEAVE_UNALLOCATED") {
			card.Instruction = reviewdomain.CycleResidualCardInstruction
		}
	case contains(decision.MissingFacts, "wealth_snapshot_confirmation"):
		card.Mode, card.Title = "reply", "🟡 Akun kekayaan belum pasti"
		card.Instruction = "Balas pesan ini (Reply) dengan nama akun kekayaan yang benar."
	case decision.ReasonCode == "UNKNOWN_BANK_TEMPLATE":
		card.Mode, card.Title = "reply", "🟡 Detail transaksi bank belum terbaca"
		card.Instruction = "Balas pesan ini (Reply) dengan nominal dan waktu transaksi. Contoh: 54000 2026-09-23T13:45:00+07:00."
	case requiresBoundReply(decision):
		card.Mode, card.Title = "reply", promptTitle(decision)
		card.Instruction = replyInstruction(decision)
	default:
		card.Mode, card.Title = "reply", unknownReviewPrompt
		card.Instruction = "Balas pesan ini (Reply) dengan keterangan atau tujuan transaksi."
	}
	return card
}

func transferInstruction(transactionType string, reconciliation bool) string {
	switch {
	case reconciliation:
		return "Pilih catatan yang sama untuk digabung, atau Catat transfer baru."
	case transactionType == "INCOME":
		return "Pilih Penghasilan jika ini pemasukan, atau Rekening sendiri/Household jika ini dana milik sendiri."
	case transactionType == "EXPENSE":
		return "Pilih Pengeluaran atau Beli aset."
	case transactionType == "TRANSFER":
		return "Pilih akun tujuan di bawah."
	}
	return "Pilih jenis transfer di bawah, atau balas pesan ini (Reply) dengan tujuan transfer."
}

// Only a transaction-bound unknown-merchant review can be reclassified as an asset purchase.
func merchantAssetOption(decision reviewdec.Decision, reviewType string) bool {
	return reviewType == "UNKNOWN_MERCHANT" && decision.Subject.Type == "transaction"
}

// cardActions returns the stored allowed actions, or the registry's Telegram actions for a legacy item.
func cardActions(decision reviewdec.Decision, reviewType string) []string {
	if len(decision.AllowedActions) > 0 {
		return decision.AllowedActions
	}
	return reviewdec.TelegramActions(reviewType)
}

// Abaikan appears only when the stored contract allows IGNORE, so a button never answers stale.
func reviewCardMarkup(ctx context.Context, tx pgx.Tx, reviewID, reviewType string, decision reviewdec.Decision, card reviewCard, subject reviewSubject) (*InlineKeyboardMarkup, error) {
	actions := cardActions(decision, reviewType)
	ignore := []InlineKeyboardButton{{Text: "Abaikan", CallbackData: "review:ignore"}}
	var markup *InlineKeyboardMarkup
	var err error
	switch card.Mode {
	case "category":
		markup = reviewActionMarkupPage(ctx, tx, reviewID, reviewType, 0)
	case "income":
		markup = incomeConfirmMarkup()
	case "transfer":
		if subject.Reconciliation {
			markup, err = transferReconciliationMarkup(ctx, tx, reviewID)
			break
		}
		// The transfer chooser follows the transaction type whatever the review kind (an incoming duplicate
		// asks Penghasilan / Rekening sendiri / Household, never Benar/Ubah).
		markup = reviewActionMarkupPage(ctx, tx, reviewID, "TRANSFER_CLASSIFICATION", 0)
	case "reply":
		row := ignore
		if card.State == "AWAITING_MERCHANT" && merchantAssetOption(decision, reviewType) {
			row = []InlineKeyboardButton{{Text: "Beli aset", CallbackData: "review:asset"}, ignore[0]}
		}
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{row}}
	case "cycle":
		if contains(actions, "LEAVE_UNALLOCATED") {
			markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Biarkan saja", CallbackData: "review:leave"}}}}
		}
	case "duplicate":
		if decision.Provenance != nil && decision.Provenance["evidence_link_dispute"] == true {
			markup = evidenceDisputeMarkup()
			break
		}
		markup, err = projectDuplicateChoices(ctx, tx, reviewID)
	case "salary":
		markup = salaryPolicyMarkup(actions)
	case "document":
		markup = documentReviewMarkup()
	case "financial_email":
		markup, err = financialEmailEntityMarkup(ctx, tx, reviewID, decision, 0)
	case "receipt_quality":
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Terima total", CallbackData: "review:quality:confirm"}, ignore[0]}}}
	case "financial_email_facts":
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{ignore}}
	default:
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ubah detail", CallbackData: "review:edit"}, ignore[0]}}}
	}
	if err != nil {
		return nil, err
	}
	if markup == nil && card.Mode != "cycle" {
		// No chooser could be built (e.g. a transfer with no subject); the card still offers dismissal when allowed.
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{ignore}}
	}
	return keepExecutableButtons(markup, actions), nil
}

// Drops Abaikan when the contract disallows IGNORE, then empty rows; a keyboard with no button is nil.
func keepExecutableButtons(markup *InlineKeyboardMarkup, actions []string) *InlineKeyboardMarkup {
	if markup == nil {
		return nil
	}
	ignoreAllowed := contains(actions, "IGNORE")
	var keyboard [][]InlineKeyboardButton
	for _, row := range markup.InlineKeyboard {
		var kept []InlineKeyboardButton
		for _, button := range row {
			if button.CallbackData == "review:ignore" && !ignoreAllowed {
				continue
			}
			kept = append(kept, button)
		}
		if len(kept) > 0 {
			keyboard = append(keyboard, kept)
		}
	}
	if len(keyboard) == 0 {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

// incomeConfirmMarkup answers "is this income?": Benar confirms, Ubah offers income/transfer classification.
func incomeConfirmMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Benar", CallbackData: "review:confirm"}, {Text: "Ubah", CallbackData: "review:change"}}, {{Text: "Abaikan", CallbackData: "review:ignore"}}}}
}

func incomeClassificationMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Penghasilan", CallbackData: "review:income"}}, {{Text: "Rekening sendiri", CallbackData: "review:own"}, {Text: "Household", CallbackData: "review:household"}}, {{Text: "Abaikan", CallbackData: "review:ignore"}}}}
}

// transferReconciliationMarkup builds the transfer chooser: a merge button per accepted candidate.
// Adds record-as-new when the case proposes a purpose, and dismiss; over ten candidates leaves only dismiss.
func transferReconciliationMarkup(ctx context.Context, tx pgx.Tx, reviewID string) (*InlineKeyboardMarkup, error) {
	set, err := transferReconciliationSet(ctx, tx, reviewID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(set.Candidates))
	var mergeable []reviewdomain.TransferMergeCandidate
	for _, candidate := range set.Candidates {
		if candidate.Mergeable {
			ids = append(ids, candidate.ID)
			mergeable = append(mergeable, candidate)
		}
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	// Merge buttons carry a list position; the server-owned list they resolve against is stored with the card.
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json||jsonb_build_object('transfer_candidates',$2::jsonb),updated_at=now() WHERE review_request_id=$1`, reviewID, string(encoded)); err != nil {
		return nil, err
	}
	ignore := []InlineKeyboardButton{{Text: "Abaikan", CallbackData: "review:ignore"}}
	if set.Total > reviewdomain.TransferCandidateLimit {
		return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{ignore}}, nil
	}
	keyboard := make([][]InlineKeyboardButton, 0, len(mergeable)+2)
	for index, candidate := range mergeable {
		label := "Gabung Rp" + FormatIDR(candidate.Amount) + " · " + reviewdomain.FormatIndonesianDate(candidate.TransactionAt.In(jakartaLocation()))
		keyboard = append(keyboard, []InlineKeyboardButton{{Text: clean(label, 40), CallbackData: fmt.Sprintf("review:xfer:merge:%d", index)}})
	}
	if set.Purpose != "" {
		keyboard = append(keyboard, []InlineKeyboardButton{{Text: "Catat transfer baru", CallbackData: "review:xfer:new"}})
	}
	keyboard = append(keyboard, ignore)
	return &InlineKeyboardMarkup{InlineKeyboard: keyboard}, nil
}

// Reads the open source-bound case behind a request through the shared merge rule, in every surface's ranking order.
func transferReconciliationSet(ctx context.Context, tx pgx.Tx, reviewID string) (reviewdomain.TransferMergeSet, error) {
	var household, caseID string
	err := tx.QueryRow(ctx, `SELECT ri.household_id::text,trc.id::text
		FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id
		JOIN transfer_reconciliation_case trc ON trc.household_id=ri.household_id AND trc.source_event_id=ri.source_event_id AND trc.financial_email_observation_id IS NULL AND trc.status='OPEN'
		WHERE r.id=$1 AND ri.transaction_id IS NULL AND ri.financial_email_observation_id IS NULL`, reviewID).Scan(&household, &caseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewdomain.TransferMergeSet{}, nil
	}
	if err != nil {
		return reviewdomain.TransferMergeSet{}, err
	}
	return reviewdomain.LoadTransferMergeSet(ctx, tx, household, caseID)
}

// reviewItemSummary names the subject of any review item from canonical state, never from model output.
func reviewItemSummary(ctx context.Context, tx pgx.Tx, householdID, itemID string) (string, error) {
	var transactionID, residualCase, wealthObservation, proposalID, sourceID string
	err := tx.QueryRow(ctx, `SELECT COALESCE(transaction_id::text,''),COALESCE(cycle_residual_case_id::text,''),COALESCE(wealth_observation_id::text,''),COALESCE(proposal_id::text,''),COALESCE(source_event_id::text,'')
		FROM review_item WHERE id=$1 AND household_id=$2`, itemID, householdID).Scan(&transactionID, &residualCase, &wealthObservation, &proposalID, &sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	switch {
	case transactionID != "":
		return transactionReviewSummary(ctx, tx, householdID, transactionID)
	case residualCase != "":
		var start, end time.Time
		var residual string
		if err := tx.QueryRow(ctx, `SELECT cycle_start,cycle_end,basis_residual_idr::text FROM cycle_residual_case WHERE id=$1 AND household_id=$2`, residualCase, householdID).Scan(&start, &end, &residual); err != nil {
			return "", ignoreNoRows(err)
		}
		return reviewdomain.CycleResidualCardFacts(start, end, residual), nil
	case wealthObservation != "":
		var institution, hint, value string
		var observed *time.Time
		if err := tx.QueryRow(ctx, `SELECT institution,account_hint,observed_value_idr::text,observed_date FROM wealth_observation WHERE id=$1 AND household_id=$2`, wealthObservation, householdID).Scan(&institution, &hint, &value, &observed); err != nil {
			return "", ignoreNoRows(err)
		}
		lines := []string{"Akun: " + strings.TrimSpace(institution+" "+hint), "Nilai: Rp" + FormatIDR(value)}
		if observed != nil {
			lines = append(lines, "Tanggal: "+reviewdomain.FormatIndonesianDate(*observed))
		}
		return strings.Join(lines, "\n"), nil
	case proposalID != "":
		var merchant string
		var at time.Time
		if err := tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(merchant_raw,''),NULLIF(counterparty_raw,''),NULLIF(description,''),''),transaction_at FROM transaction_proposal WHERE id=$1 AND household_id=$2`, proposalID, householdID).Scan(&merchant, &at); err != nil {
			return "", ignoreNoRows(err)
		}
		lines := []string{}
		if merchant != "" {
			lines = append(lines, "Merchant: "+clean(merchant, 200))
		}
		lines = append(lines, "Tanggal: "+reviewdomain.FormatIndonesianDate(at.In(jakartaLocation())))
		return strings.Join(lines, "\n"), nil
	case sourceID != "":
		var amount, description string
		var at time.Time
		err := tx.QueryRow(ctx, `SELECT amount_idr::text,COALESCE(description,''),transaction_at FROM transfer_reconciliation_case WHERE source_event_id=$1 AND household_id=$2 AND financial_email_observation_id IS NULL ORDER BY created_at DESC LIMIT 1`, sourceID, householdID).Scan(&amount, &description, &at)
		if err != nil {
			return "", ignoreNoRows(err)
		}
		lines := []string{"Nominal: Rp" + FormatIDR(amount), "Tanggal: " + reviewdomain.FormatIndonesianDate(at.In(jakartaLocation()))}
		if description != "" {
			lines = append(lines, "Keterangan: "+clean(description, 200))
		}
		return strings.Join(lines, "\n"), nil
	}
	return "", nil
}

func ignoreNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// A compound residual (category plus date) does not qualify as category-only: it needs a bound reply so no
// dimension is dropped.
func isCategoryOnly(decision reviewdec.Decision) bool {
	if len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "category" {
		return false
	}
	// A transfer review also carries CONFIRM_REVIEW, so the decision class must agree category is unresolved
	// before using the chooser.
	return decision.DecisionClass == reviewdec.ClassEvidenceGap &&
		(contains(decision.AllowedActions, "CONFIRM_REVIEW") || contains(decision.AllowedActions, "SET_CATEGORY"))
}

// requiresBoundReply: the unresolved dimension is a free-form value the user types (single-field or
// correction review, not a bounded category choice).
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
		case "correction_details":
			return "🟡 Transaksi perlu dikoreksi"
		}
	}
	return unknownReviewPrompt
}

// dateInstruction asks for the missing date in the format the date resolver parses.
func dateInstruction(decision reviewdec.Decision) string {
	if decision.ReasonCode == "MISSING_PAY_DATE" {
		return "Balas pesan ini (Reply) dengan tanggal pembayaran. Contoh: 25 September 2026, 25/9, atau kemarin."
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
	if contains(decision.MissingFacts, "correction_details") {
		return "Balas pesan ini (Reply) dengan koreksi untuk transaksi ini."
	}
	return "Balas pesan ini (Reply) dengan keterangan atau tujuan transaksi."
}

// categoryPageText keeps the card's facts on screen while paging, so a chooser edit never replaces what the
// card is about.
func categoryPageText(ctx context.Context, tx pgx.Tx, householdID, reviewID string, page int) string {
	instruction := "Pilih kategori pengeluaran di bawah."
	if page > 0 {
		instruction = fmt.Sprintf("Pilih kategori pengeluaran di bawah (halaman %d).", page+1)
	}
	return reviewFollowUpText(ctx, tx, reviewID, "🟡 Kategori belum pasti", instruction)
}

// reviewFollowUpText wraps a follow-up chooser's own title and instruction around the subject facts from
// canonical state.
func reviewFollowUpText(ctx context.Context, tx pgx.Tx, reviewID, title, instruction string) string {
	var householdID, transactionID string
	_ = tx.QueryRow(ctx, `SELECT household_id::text,COALESCE(transaction_id::text,'') FROM review_request WHERE id=$1`, reviewID).Scan(&householdID, &transactionID)
	facts := ""
	if transactionID != "" {
		facts, _ = transactionReviewSummary(ctx, tx, householdID, transactionID)
	}
	return reviewdomain.TelegramCardText(title, facts, "", instruction)
}
