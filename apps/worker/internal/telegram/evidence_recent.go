package telegram

import (
	"context"
	"fmt"
	"time"
)

// Recency policy for evidence a message is not explicitly replying to (ADR-050
// binding levels 4-6). The windows live here and nowhere else; CEU-07 tunes them
// from the binding-level counters.
const (
	// immediateEvidenceWindow is how recent evidence must be to count as "the one
	// just sent".
	immediateEvidenceWindow = 10 * time.Minute
	// recentEvidenceWindow bounds how far back evidence stays in recent context.
	recentEvidenceWindow = 60 * time.Minute
	// maxRecentEvidence is how many candidates a turn can carry.
	maxRecentEvidence = 3

	evidenceBindingImmediate = "IMMEDIATE"
	evidenceBindingRecent    = "RECENT"
)

type recentEvidenceCandidate struct {
	Document canonicalDocumentID
	Age      time.Duration
}

// recentEvidenceCandidates lists this chat's newest actionable Telegram evidence
// inside the recent window, newest first. It reads one more row than the carry
// limit so "more than the limit" is distinguishable from "exactly the limit".
// Ages come from the database clock, never from model text.
//
// Evidence is scoped to the household, the chat and the Telegram user who sent it
// (ADR-050: "the newest from this user in this chat"). Ingress only accepts
// private chats, where the chat id equals the user id, so for older rows that lack
// the recorded sender the chat id stands in for the user; that fallback is
// explicit here rather than an invariant this query silently relies on.
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

// recentEvidenceContext is the turn-facing wrapper around bindRecentEvidence. The
// context is optional, so any failure yields no context rather than an error: a
// broken lookup (a bad row, a transient database error) must never abort the
// household's message.
func (p *Processor) recentEvidenceContext(ctx context.Context, householdID, sourceEventID string, update telegramUpdate) (*agentEvidenceBinding, []map[string]any) {
	evidence, candidates, err := p.bindRecentEvidence(ctx, householdID, sourceEventID, update)
	if err != nil {
		return nil, nil
	}
	return evidence, candidates
}

// bindRecentEvidence is the no-reply, no-workflow binding. It never chooses among
// evidence: it binds only when exactly one candidate qualifies, and otherwise it
// hands the model the bounded candidate set with an explicit ambiguity flag so the
// answer is one clarification question, not a guess.
//
//	exactly one candidate in the immediate window            -> bound IMMEDIATE
//	none in the immediate window, exactly one in the hour    -> bound RECENT
//	two or more candidates and no single qualifying one      -> ambiguous
//
// Binding here is context, not authority: it does not change the tool catalog.
// Only an exact reply narrows tools (CEU-02), because an inferred binding must
// not own a turn the route says is something else.
func (p *Processor) bindRecentEvidence(ctx context.Context, householdID, sourceEventID string, update telegramUpdate) (*agentEvidenceBinding, []map[string]any, error) {
	candidates, err := p.recentEvidenceCandidates(ctx, householdID, update.Message.Chat.ID, update.Message.From.ID)
	if err != nil || len(candidates) == 0 {
		return nil, nil, err
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
