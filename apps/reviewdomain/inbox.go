package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

const (
	SubjectTransaction       = "transaction"
	SubjectFinancialEmail    = "financial_email_observation"
	SubjectProposal          = "proposal"
	SubjectSourceEvent       = "source_event"
	SubjectDocument          = "document"
	SubjectWealthObservation = "wealth_observation"
	SubjectCycleResidual     = "cycle_residual_case"
)

const (
	PresentationDecision = "DECISION"
	PresentationNotice   = "NOTICE"
)

type Binding struct {
	Subject                     string
	TransactionStatus           string
	SourceType                  string
	HasDocument                 bool
	HasTransferCase             bool
	TransferCandidates          int
	MergeableTransferCandidates int
	HasWealthObservation        bool
	FinancialTransferCase       bool
}

var documentKinds = map[string]bool{"DOCUMENT_CLASSIFICATION": true, "DOCUMENT_EXTRACTION_LOW_CONFIDENCE": true}
var bankFactKinds = map[string]bool{"UNKNOWN_BANK_TEMPLATE": true, "DOCUMENT_EXTRACTION_LOW_CONFIDENCE": true}
var payslipKinds = map[string]bool{"PAYSLIP_CONFIRMATION": true, "MISSING_PAY_DATE": true}

func Executable(kind, action string, b Binding) bool {
	openTransaction := b.Subject == SubjectTransaction && b.TransactionStatus == "NEEDS_REVIEW"
	switch action {
	case "IGNORE":
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
				return b.MergeableTransferCandidates > 0
			}
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
		return openTransaction
	case "SET_AMOUNT":
		return kind == "MISSING_AMOUNT" && b.Subject == SubjectProposal
	default:
		return openTransaction
	}
}

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
		out = append(out, "IGNORE")
	}
	return out
}

func PresentationFor(actions []string) string {
	if len(actions) == 1 && actions[0] == "IGNORE" {
		return PresentationNotice
	}
	return PresentationDecision
}

func LoadBinding(ctx context.Context, tx pgx.Tx, householdID, reviewItemID string) (string, []string, Binding, error) {
	var kind, caseID string
	var decision []byte
	var b Binding
	err := tx.QueryRow(ctx, `SELECT ri.review_type,COALESCE(ri.decision,'{}'::jsonb),
		CASE WHEN ri.transaction_id IS NOT NULL THEN 'transaction' WHEN ri.financial_email_observation_id IS NOT NULL THEN 'financial_email_observation' WHEN ri.proposal_id IS NOT NULL THEN 'proposal' WHEN ri.source_event_id IS NOT NULL THEN 'source_event' WHEN ri.document_id IS NOT NULL THEN 'document' WHEN ri.wealth_observation_id IS NOT NULL THEN 'wealth_observation' ELSE 'cycle_residual_case' END,
		COALESCE(t.status,''),
		COALESCE(s.source_type,fs.source_type,''),
		ri.document_id IS NOT NULL,
		trc.id IS NOT NULL,COALESCE(trc.id::text,''),
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
		WHERE ri.id=$1 AND ri.household_id=$2`, reviewItemID, householdID).Scan(&kind, &decision, &b.Subject, &b.TransactionStatus, &b.SourceType, &b.HasDocument, &b.HasTransferCase, &caseID, &b.TransferCandidates, &b.FinancialTransferCase, &b.HasWealthObservation)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, b, ErrAlreadyResolved
	}
	if err != nil {
		return "", nil, b, err
	}
	if b.HasTransferCase {
		set, err := LoadTransferMergeSet(ctx, tx, householdID, caseID)
		if err != nil {
			return "", nil, b, err
		}
		b.MergeableTransferCandidates = set.Mergeable()
	}
	var contract struct {
		AllowedActions []string `json:"allowedActions"`
	}
	_ = json.Unmarshal(decision, &contract)
	return kind, contract.AllowedActions, b, nil
}
