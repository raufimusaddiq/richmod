package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Recency windows for evidence a message does not reply to (ADR-050 binding levels 4-6); tune from the
// binding-level counters.
const (
	immediateEvidenceWindow = 10 * time.Minute
	recentEvidenceWindow    = 60 * time.Minute
	maxRecentEvidence       = 3

	evidenceBindingImmediate = "IMMEDIATE"
	evidenceBindingRecent    = "RECENT"
)

type recentEvidenceCandidate struct {
	Document canonicalDocumentID
	Age      time.Duration
}

// Scoped to household, chat and sending user; older rows without a sender fall back to the chat id.
// Reads one row past the carry limit to tell "more than" from "exactly".
func (p *Processor) recentEvidenceCandidates(ctx context.Context, householdID string, chatID, userID int64) ([]recentEvidenceCandidate, error) {
	rows, err := p.pool.Query(ctx, `SELECT d.id::text,extract(epoch FROM now()-d.created_at)::float8
		FROM document d JOIN source_event s ON s.id=d.source_event_id AND s.household_id=d.household_id
		LEFT JOIN source_event_payload pl ON pl.source_event_id=s.id
		WHERE d.household_id=$1 AND s.telegram_chat_id=$2 AND s.source_type='TELEGRAM_IMAGE'
		  AND COALESCE((pl.payload_json->>'telegram_user_id')::bigint,s.telegram_chat_id)=$5
		  AND d.status<>'FAILED' AND d.created_at > now()-make_interval(secs => $3::double precision)
		ORDER BY d.created_at DESC LIMIT $4::int`,
		householdID, chatID, recentEvidenceWindow.Seconds(), maxRecentEvidence+1, userID)
	if err != nil {
		return nil, fmt.Errorf("load recent evidence: %w", err)
	}
	defer rows.Close()
	var out []recentEvidenceCandidate
	for rows.Next() {
		var id string
		var seconds float64
		if err := rows.Scan(&id, &seconds); err != nil {
			return nil, err
		}
		out = append(out, recentEvidenceCandidate{Document: canonicalDocumentID(id), Age: time.Duration(seconds * float64(time.Second))})
	}
	return out, rows.Err()
}

// Context is optional: any failure yields none rather than aborting the household's message.
func (p *Processor) recentEvidenceContext(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, candidates []recentEvidenceCandidate) (*agentEvidenceBinding, []map[string]any) {
	evidence, ambiguous, err := p.bindRecentEvidenceFrom(ctx, householdID, sourceEventID, update, candidates)
	if err != nil {
		return nil, nil
	}
	return evidence, ambiguous
}

func (p *Processor) bindRecentEvidenceFrom(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, candidates []recentEvidenceCandidate) (*agentEvidenceBinding, []map[string]any, error) {
	if len(candidates) == 0 {
		return nil, nil, nil
	}
	immediate := 0
	for _, candidate := range candidates {
		if candidate.Age <= immediateEvidenceWindow {
			immediate++
		}
	}
	var bound canonicalDocumentID
	var kind string
	switch {
	case immediate == 1:
		bound, kind = candidates[0].Document, evidenceBindingImmediate
	case immediate == 0 && len(candidates) == 1:
		bound, kind = candidates[0].Document, evidenceBindingRecent
	}
	if bound != "" {
		evidence, err := p.newEvidenceBinding(ctx, householdID, sourceEventID, update, bound)
		if err != nil || evidence == nil {
			return nil, nil, err
		}
		evidence.Context["binding"] = kind
		p.recordCEUOutcome(ctx, householdID, sourceEventID, ceuRecentContextBinding)
		return evidence, nil, nil
	}
	if len(candidates) > maxRecentEvidence {
		candidates = candidates[:maxRecentEvidence]
	}
	documents := make([]canonicalDocumentID, len(candidates))
	for index, candidate := range candidates {
		documents[index] = candidate.Document
	}
	contexts, err := p.loadEvidenceContexts(ctx, householdID, sourceEventID, update, documents)
	if err != nil {
		return nil, nil, err
	}
	p.recordCEUOutcome(ctx, householdID, sourceEventID, ceuAmbiguousContext)
	return nil, contexts, nil
}

// Decides whether a bare amount may be harvested as a new transaction (older evidence must not slow an
// ordinary expense) and feeds the ledger guard.
func freshEvidenceDocuments(candidates []recentEvidenceCandidate) []string {
	var documents []string
	for _, candidate := range candidates {
		if candidate.Age <= immediateEvidenceWindow {
			documents = append(documents, string(candidate.Document))
		}
	}
	return documents
}

// Backstop against a second transaction for one document: same type and amount, merchant absent or equal.
// A different merchant is allowed so a genuine second purchase is not blocked; it fails open.
func (p *Processor) evidenceAlreadyRecorded(ctx context.Context, householdID string, freshDocuments []string, transactionType, amount, merchant string) bool {
	if len(freshDocuments) == 0 {
		return false
	}
	rows, err := p.pool.Query(ctx, `SELECT COALESCE(m.normalized_name,''),COALESCE(t.counterparty_name,'')
		FROM transaction_evidence te JOIN transaction t ON t.id=te.transaction_id AND t.household_id=$1
		LEFT JOIN merchant m ON m.id=t.merchant_id
		WHERE t.status<>'VOIDED' AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE' AND t.type=$2 AND t.amount=$3::numeric
		  AND te.source_event_id IN (
		       SELECT source_event_id FROM document WHERE id=ANY($4::uuid[])
		       UNION SELECT source_event_id FROM document_page WHERE document_id=ANY($4::uuid[]))`,
		householdID, transactionType, amount, freshDocuments)
	if err != nil {
		return false
	}
	defer rows.Close()
	proposed := strings.Fields(strings.ToLower(merchant))
	for rows.Next() {
		var recordedMerchant, recordedName string
		if err := rows.Scan(&recordedMerchant, &recordedName); err != nil {
			return false
		}
		if len(proposed) == 0 {
			return true
		}
		for _, recorded := range []string{recordedMerchant, recordedName} {
			if sameMerchantTokens(proposed, strings.Fields(strings.ToLower(recorded))) {
				return true
			}
		}
	}
	return false
}

// Identical names match; otherwise each word of the shorter must be a whole word in the longer, and it needs
// a distinctive word of 5+ letters ("mirota" matches "mirota swalayan", "toko" does not match "toko jaya").
func sameMerchantTokens(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	inLong := map[string]bool{}
	for _, word := range long {
		inLong[word] = true
	}
	distinctive := false
	for _, word := range short {
		if !inLong[word] {
			return false
		}
		if len([]rune(word)) >= 5 {
			distinctive = true
		}
	}
	return len(short) == len(long) || distinctive
}
