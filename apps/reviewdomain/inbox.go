// Inbox action contract. The stored ReviewDecision says what a review asks;
// the binding says which of those answers the canonical resolvers can actually
// execute for this item. Every surface offers the intersection, so no surface
// renders a button that can only fail (ADR-054 amendment, 2026-11).
package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

// Subject discriminators of review_item's exactly-one-subject binding.
const (
	SubjectTransaction       = "transaction"
	SubjectFinancialEmail    = "financial_email_observation"
	SubjectProposal          = "proposal"
	SubjectSourceEvent       = "source_event"
	SubjectDocument          = "document"
	SubjectWealthObservation = "wealth_observation"
	SubjectCycleResidual     = "cycle_residual_case"
)

// Presentation values. A NOTICE asks nothing the household can decide: the only
// executable answer is acknowledging it, so surfaces render it as information
// ("Mengerti") and keep it out of the needs-your-decision count.
const (
	PresentationDecision = "DECISION"
	PresentationNotice   = "NOTICE"
)

// Binding is the server-side shape of one review item that decides which
// actions a resolver can execute. It never reaches a model.
type Binding struct {
	Subject string
	// TransactionStatus is the bound transaction's status ('' when unbound).
	TransactionStatus string
	// SourceType is the evidence source's type (BANK_EMAIL, FINANCIAL_EMAIL, ...).
	SourceType string
	// HasDocument reports a bound document (document-bound reviews only).
	HasDocument bool
	// HasTransferCase reports an open transfer_reconciliation_case for the item.
	HasTransferCase bool
	// TransferCandidates is the case's full candidate count before ranking.
	TransferCandidates int
	// HasWealthObservation reports a bound wealth observation. A provider-email
	// wealth review binds both the observation and the email observation
	// (migration 00052), so the subject discriminator alone is not enough.
	HasWealthObservation bool
	// FinancialTransferCase reports that the open case is bound to a provider
	// email observation, which is the resolver's overflow predicate.
	FinancialTransferCase bool
}

var documentKinds = map[string]bool{"DOCUMENT_CLASSIFICATION": true, "DOCUMENT_EXTRACTION_LOW_CONFIDENCE": true}
var bankFactKinds = map[string]bool{"UNKNOWN_BANK_TEMPLATE": true, "DOCUMENT_EXTRACTION_LOW_CONFIDENCE": true}
var payslipKinds = map[string]bool{"PAYSLIP_CONFIRMATION": true, "MISSING_PAY_DATE": true}

// Executable reports whether a canonical resolver exists for action on this
// binding. It is the single rule the Web dispatcher and the list share.
func Executable(kind, action string, b Binding) bool {
	openTransaction := b.Subject == SubjectTransaction && b.TransactionStatus == "NEEDS_REVIEW"
	switch action {
	case "IGNORE":
		// A residual allocation is a policy choice with its own "leave
		// unallocated" answer; it is never silently dismissed.
		return b.Subject != SubjectCycleResidual
	case "REPROCESS_DOCUMENT":
		return documentKinds[kind] && b.Subject == SubjectDocument && b.HasDocument
	case "COMPLETE_BANK_FACTS":
		return bankFactKinds[kind] && b.Subject == SubjectSourceEvent && b.SourceType == "BANK_EMAIL"
	case "SET_WEALTH_ACCOUNT":
		return kind == "WEALTH_OBSERVATION_CONFIRMATION" && (b.Subject == SubjectWealthObservation || b.HasWealthObservation)
	case "SET_FINANCIAL_EMAIL_ENTITIES":
		return kind == "FINANCIAL_EMAIL_RESOLUTION" && b.Subject == SubjectFinancialEmail
	case "MERGE_EXISTING", "CONFIRM_NEW_TRANSFER":
		if kind == "TRANSFER_CLASSIFICATION" && b.Subject != SubjectTransaction {
			if !b.HasTransferCase {
				return false
			}
			if action == "MERGE_EXISTING" {
				return b.TransferCandidates > 0
			}
			// Same predicate as ReconcileTransfer: the case's observation binding.
			return !(b.FinancialTransferCase && b.TransferCandidates > TransferCandidateLimit)
		}
		return openTransaction
	case "ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED", "TRANSACTION_MISSING":
		return kind == "CYCLE_RESIDUAL_ALLOCATION" && b.Subject == SubjectCycleResidual
	case "PRIMARY_SALARY", "ORDINARY_INCOME":
		return payslipKinds[kind] && b.Subject == SubjectProposal
	case "SET_PAY_DATE":
		if payslipKinds[kind] {
			return b.Subject == SubjectProposal
		}
		// Legacy transaction-date decisions stored SET_PAY_DATE; it executes as
		// the transaction-date answer (SET_TRANSACTION_DATE).
		return openTransaction
	case "SET_AMOUNT":
		return kind == "MISSING_AMOUNT" && b.Subject == SubjectProposal
	default:
		// CONFIRM_REVIEW, SET_TRANSACTION_DATE, CLASSIFY_TRANSFER and the transfer
		// classification vocabulary answer an open transaction only.
		return openTransaction
	}
}

// ExecutableActions returns what a surface may offer: the stored decision's
// actions (ADR-054: the stored decision is what users see), falling back to the
// kind's preset contract and then the registry Inbox list only for rows written
// before the decision contract, filtered by Executable. A transfer review with
// an open reconciliation case additionally carries the bounded chooser.
func ExecutableActions(kind string, stored []string, b Binding) []string {
	base := stored
	if len(base) == 0 {
		if preset, ok := reviewdec.Preset(kind, b.Subject, ""); ok {
			base = preset.AllowedActions
		} else {
			base = reviewdec.InboxActions(kind)
		}
	}
	candidates := append([]string(nil), base...)
	if kind == "TRANSFER_CLASSIFICATION" && b.Subject != SubjectTransaction && b.HasTransferCase {
		candidates = append([]string{"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER"}, candidates...)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, action := range candidates {
		if seen[action] || !Executable(kind, action, b) {
			continue
		}
		seen[action] = true
		out = append(out, action)
	}
	if len(out) == 0 && Executable(kind, "IGNORE", b) {
		// Nothing the decision asked can be answered any more on this binding
		// (for example the transaction it asked about is already final). The
		// review is still acknowledged rather than stranded.
		out = append(out, "IGNORE")
	}
	return out
}

// PresentationFor classifies offered actions: acknowledge-only is a notice.
func PresentationFor(actions []string) string {
	if len(actions) == 1 && actions[0] == "IGNORE" {
		return PresentationNotice
	}
	return PresentationDecision
}

// LoadBinding reads the binding of one review item inside the caller's
// transaction. It is used by the dispatcher so "offered" and "accepted" are the
// same rule.
func LoadBinding(ctx context.Context, tx pgx.Tx, householdID, reviewItemID string) (string, []string, Binding, error) {
	var kind string
	var decision []byte
	var b Binding
	err := tx.QueryRow(ctx, `SELECT ri.review_type,COALESCE(ri.decision,'{}'::jsonb),
		CASE WHEN ri.transaction_id IS NOT NULL THEN 'transaction' WHEN ri.financial_email_observation_id IS NOT NULL THEN 'financial_email_observation' WHEN ri.proposal_id IS NOT NULL THEN 'proposal' WHEN ri.source_event_id IS NOT NULL THEN 'source_event' WHEN ri.document_id IS NOT NULL THEN 'document' WHEN ri.wealth_observation_id IS NOT NULL THEN 'wealth_observation' ELSE 'cycle_residual_case' END,
		COALESCE(t.status,''),
		COALESCE(s.source_type,fs.source_type,''),
		ri.document_id IS NOT NULL,
		trc.id IS NOT NULL,
		COALESCE(cardinality(trc.candidate_transaction_ids),0),
		COALESCE(trc.financial_email_observation_id IS NOT NULL,false),
		ri.wealth_observation_id IS NOT NULL
		FROM review_item ri
		LEFT JOIN transaction t ON t.id=ri.transaction_id AND t.household_id=ri.household_id
		LEFT JOIN source_event s ON s.id=ri.source_event_id AND s.household_id=ri.household_id
		LEFT JOIN financial_email_observation fo ON fo.id=ri.financial_email_observation_id AND fo.household_id=ri.household_id
		LEFT JOIN source_event fs ON fs.id=fo.source_event_id AND fs.household_id=ri.household_id
		LEFT JOIN LATERAL (SELECT c.id,c.candidate_transaction_ids,c.financial_email_observation_id FROM transfer_reconciliation_case c WHERE c.household_id=ri.household_id AND c.status='OPEN'
			AND ((ri.financial_email_observation_id IS NOT NULL AND c.financial_email_observation_id=ri.financial_email_observation_id) OR (ri.financial_email_observation_id IS NULL AND ri.source_event_id IS NOT NULL AND c.source_event_id=ri.source_event_id)) LIMIT 1) trc ON true
		WHERE ri.id=$1 AND ri.household_id=$2`, reviewItemID, householdID).Scan(&kind, &decision, &b.Subject, &b.TransactionStatus, &b.SourceType, &b.HasDocument, &b.HasTransferCase, &b.TransferCandidates, &b.FinancialTransferCase, &b.HasWealthObservation)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, b, ErrAlreadyResolved
	}
	if err != nil {
		return "", nil, b, err
	}
	var contract struct {
		AllowedActions []string `json:"allowedActions"`
	}
	_ = json.Unmarshal(decision, &contract)
	return kind, contract.AllowedActions, b, nil
}
