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
	Purpose         string
	Reconciliation  bool
	Cycle           *cycleSubject
	Bank            *bankSubject
	ProposalType    string
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
	var cycle cycleSubject
	var hasCycle bool
	err := tx.QueryRow(ctx, `SELECT COALESCE(t.type,''),COALESCE(t.purpose,''),
		ri.transaction_id IS NULL AND EXISTS(SELECT 1 FROM transfer_reconciliation_case trc WHERE trc.household_id=ri.household_id AND trc.status='OPEN' AND ri.financial_email_observation_id IS NULL AND trc.source_event_id=ri.source_event_id AND trc.financial_email_observation_id IS NULL),
		crc.id IS NOT NULL,COALESCE(crc.cycle_start,'0001-01-01'),COALESCE(crc.cycle_end,'0001-01-01'),COALESCE(crc.basis_residual_idr::text,''),
		COALESCE(tp.proposed_type,'')
		FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id
		LEFT JOIN transaction t ON t.id=COALESCE(r.transaction_id,ri.transaction_id) AND t.household_id=r.household_id
		LEFT JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id AND crc.household_id=r.household_id
		LEFT JOIN transaction_proposal tp ON tp.id=ri.proposal_id AND tp.household_id=r.household_id
		WHERE r.id=$1`, reviewID).Scan(&subject.TransactionType, &subject.Purpose, &subject.Reconciliation, &hasCycle, &cycle.Start, &cycle.End, &cycle.Residual, &subject.ProposalType)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewSubject{}, nil
	}
	if err == nil {
		if subject.Bank, err = loadBankSubject(ctx, tx, reviewID); err != nil {
			return reviewSubject{}, err
		}
	}
	if hasCycle {
		subject.Cycle = &cycle
	}
	return subject, err
}

const appReviewHint = "Selesaikan di " + reviewInboxName + " di aplikasi."

func buildReviewCard(decision reviewdec.Decision, reviewType, facts string, subject reviewSubject) reviewCard {
	return stepRenderCard(stepOrFallback(decision, reviewType, facts, subject))
}

func stepOrFallback(decision reviewdec.Decision, reviewType, facts string, subject reviewSubject) Step {
	if step, ok := deriveStep(decision, reviewType, facts, subject); ok {
		return step
	}
	return fallbackStep(decision, reviewType, facts)
}

func merchantAssetOption(decision reviewdec.Decision, reviewType string) bool {
	return reviewType == "UNKNOWN_MERCHANT" && decision.Subject.Type == "transaction"
}

func cardActions(decision reviewdec.Decision, reviewType string) []string {
	if len(decision.AllowedActions) > 0 {
		return decision.AllowedActions
	}
	return reviewdec.TelegramActions(reviewType)
}

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
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json||jsonb_build_object('transfer_candidates',$2::jsonb),updated_at=now() WHERE review_request_id=$1`, reviewID, string(encoded)); err != nil {
		return nil, err
	}
	ignore := []InlineKeyboardButton{{Text: "Abaikan", CallbackData: "review:ignore"}}
	if set.Total > reviewdomain.TransferCandidateLimit {
		return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{ignore}}, nil
	}
	keyboard := make([][]InlineKeyboardButton, 0, len(mergeable)+2)
	for index, candidate := range mergeable {
		label := "Gabung " + joinDot(IDR(candidate.Amount), reviewdomain.FormatIndonesianDate(candidate.TransactionAt.In(jakartaLocation())))
		keyboard = append(keyboard, []InlineKeyboardButton{{Text: clean(label, 40), CallbackData: fmt.Sprintf("review:xfer:merge:%d", index)}})
	}
	if set.Purpose != "" {
		keyboard = append(keyboard, []InlineKeyboardButton{{Text: "Catat transfer baru", CallbackData: "review:xfer:new"}})
	}
	keyboard = append(keyboard, ignore)
	return &InlineKeyboardMarkup{InlineKeyboard: keyboard}, nil
}

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

func reviewItemSummary(ctx context.Context, tx pgx.Tx, householdID, itemID string) (string, error) {
	var transactionID, residualCase, wealthObservation, proposalID, sourceID, documentID, observationID string
	err := tx.QueryRow(ctx, `SELECT COALESCE(transaction_id::text,''),COALESCE(cycle_residual_case_id::text,''),COALESCE(wealth_observation_id::text,''),COALESCE(proposal_id::text,''),COALESCE(source_event_id::text,''),COALESCE(document_id::text,''),COALESCE(financial_email_observation_id::text,'')
		FROM review_item WHERE id=$1 AND household_id=$2`, itemID, householdID).Scan(&transactionID, &residualCase, &wealthObservation, &proposalID, &sourceID, &documentID, &observationID)
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
		return "", nil
	case wealthObservation != "":
		var institution, hint, value string
		var observed *time.Time
		if err := tx.QueryRow(ctx, `SELECT institution,account_hint,observed_value_idr::text,observed_date FROM wealth_observation WHERE id=$1 AND household_id=$2`, wealthObservation, householdID).Scan(&institution, &hint, &value, &observed); err != nil {
			return "", ignoreNoRows(err)
		}
		date := ""
		if observed != nil {
			date = reviewdomain.FormatIndonesianDate(*observed)
		}
		return factBlock("Akun", institution+" "+hint, "Nilai", IDR(value), "Tanggal", date), nil
	case proposalID != "":
		if summary, ok, err := payslipItemSummary(ctx, tx, householdID, itemID, proposalID); ok || err != nil {
			return summary, err
		}
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
	case observationID != "":
		return financialEmailSummary(ctx, tx, householdID, observationID)
	case documentID != "":
		return documentSummary(ctx, tx, householdID, documentID)
	case sourceID != "":
		var amount, description string
		var at time.Time
		err := tx.QueryRow(ctx, `SELECT amount_idr::text,COALESCE(description,''),transaction_at FROM transfer_reconciliation_case WHERE source_event_id=$1 AND household_id=$2 AND financial_email_observation_id IS NULL ORDER BY created_at DESC LIMIT 1`, sourceID, householdID).Scan(&amount, &description, &at)
		if err != nil {
			return "", ignoreNoRows(err)
		}
		return factBlock("Nominal", IDR(amount), "Tanggal", reviewdomain.FormatIndonesianDate(at.In(jakartaLocation())), "Keterangan", clean(description, 200)), nil
	}
	return "", nil
}

func ignoreNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func isCategoryOnly(decision reviewdec.Decision) bool {
	if len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "category" {
		return false
	}
	return decision.DecisionClass == reviewdec.ClassEvidenceGap &&
		(contains(decision.AllowedActions, "CONFIRM_REVIEW") || contains(decision.AllowedActions, "SET_CATEGORY"))
}
