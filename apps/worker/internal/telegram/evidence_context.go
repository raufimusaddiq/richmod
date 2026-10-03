package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	evidenceCaptionChars  = 200
	evidenceMerchantChars = 80
	evidenceDescChars     = 120
)

type evidenceFacts struct {
	DocumentType, DocumentStatus, SourceType, Caption string
	ReceivedAt                                        time.Time

	HasProposal                                bool
	ProposalAmount                             *string
	ProposalMerchant, ProposalDesc             string
	ProposalAt                                 time.Time
	ProposalStatus                             string
	LinkedTransactionID                        string
	LinkedAmount, LinkedCategory, LinkedStatus string
	ReviewType                                 string
	Missing, AllowedActions                    []string
}

// loadEvidenceContexts issues opaque refs for the given documents and returns
// their Evidence Context Packages, in input order. It is the single entry point
// for conversational evidence context: nothing else builds one.
//
// The package is the smallest model-safe view of evidence (ADR-050):
//
//	observed  what the extractor produced; unverified
//	canonical what Go stores; present only when a transaction is linked
//	workflow  what the server will accept (open review, missing facts, actions)
//
// It carries no canonical ids, no storage refs, no OCR text, and no confidence.
// Every evidence-derived string is wrapped as untrusted data and clipped.
func (p *Processor) loadEvidenceContexts(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, documents []canonicalDocumentID) ([]map[string]any, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	if len(documents) > maxEvidenceItems {
		documents = documents[:maxEvidenceItems]
	}
	refs, err := p.issueEvidenceRefs(ctx, householdID, sourceEventID, update, documents)
	if err != nil {
		return nil, fmt.Errorf("issue evidence references: %w", err)
	}
	facts := make([]evidenceFacts, len(documents))
	var transactionIDs []string
	seen := map[string]int{}
	for index, document := range documents {
		facts[index], err = p.loadEvidenceFacts(ctx, householdID, document)
		if err != nil {
			return nil, err
		}
		if id := facts[index].LinkedTransactionID; id != "" {
			if _, ok := seen[id]; !ok {
				seen[id] = len(transactionIDs)
				transactionIDs = append(transactionIDs, id)
			}
		}
	}
	// One batch for every linked transaction in this turn: refs are keyed by
	// position inside a single TOOL turn, so two evidence items cannot overwrite
	// each other's transaction ref.
	var transactionRefs []agentPublicRef
	if len(transactionIDs) > 0 {
		transactionRefs, err = p.persistAgentTransactionReferences(ctx, householdID, sourceEventID, update, evidenceLinkedTxPhase, transactionIDs)
		if err != nil {
			return nil, fmt.Errorf("issue evidence transaction references: %w", err)
		}
	}
	out := make([]map[string]any, len(documents))
	for index := range documents {
		txRef := ""
		if id := facts[index].LinkedTransactionID; id != "" {
			txRef = transactionRefs[seen[id]].Ref
		}
		out[index] = facts[index].public(refs[index], txRef)
	}
	return out, nil
}

func (p *Processor) loadEvidenceFacts(ctx context.Context, householdID string, document canonicalDocumentID) (evidenceFacts, error) {
	var f evidenceFacts
	var sourceEventID string
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(d.document_type,''),d.status,COALESCE(s.source_type,''),s.received_at,d.source_event_id::text,
			COALESCE(pl.payload_json->>'caption','')
		FROM document d JOIN source_event s ON s.id=d.source_event_id
		LEFT JOIN source_event_payload pl ON pl.source_event_id=s.id
		WHERE d.id=$1 AND d.household_id=$2`, string(document), householdID).
		Scan(&f.DocumentType, &f.DocumentStatus, &f.SourceType, &f.ReceivedAt, &sourceEventID, &f.Caption)
	if err != nil {
		return f, fmt.Errorf("load evidence document: %w", err)
	}

	err = p.pool.QueryRow(ctx, `SELECT amount::text,COALESCE(merchant_raw,counterparty_raw,''),COALESCE(description,''),transaction_at,proposal_status
		FROM transaction_proposal WHERE source_event_id=$1::uuid AND household_id=$2`, sourceEventID, householdID).
		Scan(&f.ProposalAmount, &f.ProposalMerchant, &f.ProposalDesc, &f.ProposalAt, &f.ProposalStatus)
	switch {
	case err == nil:
		f.HasProposal = true
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return f, fmt.Errorf("load evidence proposal: %w", err)
	}

	// An album is one document whose later images keep their own source event, so
	// a transaction linked through any member counts as linked.
	err = p.pool.QueryRow(ctx, `SELECT t.id::text,t.amount::text,COALESCE(c.slug,''),t.status
		FROM transaction_evidence te JOIN transaction t ON t.id=te.transaction_id
		LEFT JOIN category c ON c.id=t.category_id
		WHERE t.household_id=$2 AND t.status<>'VOIDED'
		  AND te.source_event_id IN (SELECT $1::uuid UNION SELECT source_event_id FROM document_page WHERE document_id=$3::uuid)
		ORDER BY te.created_at LIMIT 1`, sourceEventID, householdID, string(document)).
		Scan(&f.LinkedTransactionID, &f.LinkedAmount, &f.LinkedCategory, &f.LinkedStatus)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return f, fmt.Errorf("load evidence transaction link: %w", err)
	}

	var missing, actions []byte
	err = p.pool.QueryRow(ctx, `SELECT review_type,COALESCE(decision->'missingFacts','[]'::jsonb),COALESCE(decision->'allowedActions','[]'::jsonb)
		FROM review_item WHERE household_id=$1 AND (document_id=$2::uuid OR source_event_id=$3::uuid)
		  AND status IN ('OPEN','PENDING_SEND') ORDER BY created_at DESC LIMIT 1`, householdID, string(document), sourceEventID).
		Scan(&f.ReviewType, &missing, &actions)
	switch {
	case err == nil:
		_ = json.Unmarshal(missing, &f.Missing)
		_ = json.Unmarshal(actions, &f.AllowedActions)
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return f, fmt.Errorf("load evidence review: %w", err)
	}
	return f, nil
}

// public renders the model-visible package. Provenance is structural: observed,
// canonical and workflow are separate blocks and are never merged.
func (f evidenceFacts) public(ref evidenceRef, transactionRef string) map[string]any {
	out := map[string]any{
		"evidence_ref":    string(ref),
		"source_type":     f.SourceType,
		"document_type":   f.DocumentType,
		"document_status": f.DocumentStatus,
		"received_at":     f.ReceivedAt.In(jakartaLocation()).Format(time.RFC3339),
	}
	if f.Caption != "" {
		out["caption"] = untrustedEvidence(clipRunes(f.Caption, evidenceCaptionChars))
	}
	if f.HasProposal {
		observed := map[string]any{
			"merchant":       untrustedEvidence(clipRunes(f.ProposalMerchant, evidenceMerchantChars)),
			"description":    untrustedEvidence(clipRunes(f.ProposalDesc, evidenceDescChars)),
			"transaction_at": f.ProposalAt.In(jakartaLocation()).Format(time.RFC3339),
			"status":         f.ProposalStatus,
		}
		if f.ProposalAmount != nil {
			observed["amount_idr"] = *f.ProposalAmount
		}
		out["observed"] = observed
	}
	if f.LinkedTransactionID != "" {
		out["canonical"] = map[string]any{
			"transaction_ref": transactionRef,
			"amount_idr":      f.LinkedAmount,
			"category_slug":   f.LinkedCategory,
			"status":          f.LinkedStatus,
		}
	}
	if f.ReviewType != "" {
		out["workflow"] = map[string]any{
			"review_open":     true,
			"review_type":     f.ReviewType,
			"missing":         nonNilStrings(f.Missing),
			"allowed_actions": nonNilStrings(f.AllowedActions),
		}
	}
	return out
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
