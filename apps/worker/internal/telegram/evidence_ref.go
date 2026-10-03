package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// CEU reference lifetime classes (ADR-050). They are distinct types so the
// compiler, not convention, keeps them apart:
//
//   - turn-local refs (analytics "category.3") live in one model turn and are
//     never stored or replayed; they have no type here because nothing in the CEU
//     path may carry one;
//   - evidenceRef is cross-turn and bounded: opaque, server-issued, household +
//     Telegram user + chat scoped, expiring, resolved only by Go;
//   - canonicalDocumentID is a database identity. It is server-only: it is never
//     placed in a model-visible payload and never accepted as a model argument.
//
// Only resolveEvidenceRef turns an evidenceRef into a canonicalDocumentID.
type evidenceRef string

type canonicalDocumentID string

const (
	// evidenceRefTTL matches the transaction ref lifetime.
	evidenceRefTTL = 60 * time.Minute
	// maxEvidenceItems bounds how much evidence one turn's context carries.
	maxEvidenceItems = 5
	// evidenceRefPhase is the phase slot evidence refs use; the "_ev" suffix keeps
	// the key space disjoint from transaction refs ("_tx").
	evidenceRefPhase = "p0r0"
	// evidenceLinkedTxPhase scopes the transaction refs issued for evidence-linked
	// transactions. recentAgentTransactions already owns p0r0_tx<n> for the same
	// source event; sharing it would let a later turn silently re-point an earlier
	// ref. Read phases are bounded by MaxModelPhases, so p9 can never collide.
	evidenceLinkedTxPhase = "p9r0"
)

var evidenceRefPattern = regexp.MustCompile(`^a[0-9a-f]{8}_p[0-9]+r[0-9]+_ev[0-9]+$`)

// ceuOutcome is a bounded, allow-listed telemetry action. It never carries text,
// values, or identifiers.
type ceuOutcome string

const (
	ceuExactReplyBinding     ceuOutcome = "EXACT_REPLY_BINDING"
	ceuActiveReviewBinding   ceuOutcome = "ACTIVE_REVIEW_BINDING"
	ceuOpaqueRefBinding      ceuOutcome = "OPAQUE_REF_BINDING"
	ceuRecentContextBinding  ceuOutcome = "RECENT_CONTEXT_BINDING"
	ceuSemanticDisambiguated ceuOutcome = "SEMANTIC_DISAMBIGUATION"
	ceuAmbiguousContext      ceuOutcome = "AMBIGUOUS_CONTEXT"
	ceuReferenceExpired      ceuOutcome = "REFERENCE_EXPIRED"
	ceuReferenceInvalid      ceuOutcome = "REFERENCE_INVALID"
	ceuEvidenceNotFound      ceuOutcome = "EVIDENCE_NOT_FOUND"
	ceuEvidenceStale         ceuOutcome = "EVIDENCE_STALE"
	// ceuResolved is a successful resolution. It is not a telemetry action: the
	// binding level that used it is what gets counted.
	ceuResolved ceuOutcome = "RESOLVED"
)

var ceuTelemetryOutcomes = map[ceuOutcome]bool{
	ceuExactReplyBinding: true, ceuActiveReviewBinding: true, ceuOpaqueRefBinding: true,
	ceuRecentContextBinding: true, ceuSemanticDisambiguated: true, ceuAmbiguousContext: true,
	ceuReferenceExpired: true, ceuReferenceInvalid: true, ceuEvidenceNotFound: true, ceuEvidenceStale: true,
}

// recordCEUOutcome stores one bounded counter row. It is best-effort and never
// fails a turn: telemetry must not change financial behavior.
func (p *Processor) recordCEUOutcome(ctx context.Context, householdID, sourceEventID string, outcome ceuOutcome) {
	if p.pool == nil || !ceuTelemetryOutcomes[outcome] {
		return
	}
	_, _ = p.pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,source_event_id,event_type,action)
		VALUES($1,NULLIF($2,'')::uuid,'CEU_BINDING',$3)`, householdID, sourceEventID, string(outcome))
}

// issueEvidenceRefs persists opaque refs for the given documents and returns them
// in input order. It is idempotent: a retried or duplicate-delivered turn reuses
// the same TOOL turn and re-upserts the same keys, so it never multiplies rows or
// re-points a ref. Refs are keyed by the source event of the issuing turn and by
// the document's position, so the same inputs always produce the same refs.
func (p *Processor) issueEvidenceRefs(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, documents []canonicalDocumentID) ([]evidenceRef, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	if len(documents) > maxEvidenceItems {
		documents = documents[:maxEvidenceItems]
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	refs, err := issueEvidenceRefsTx(ctx, tx, householdID, sourceEventID, update, documents)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return refs, nil
}

func issueEvidenceRefsTx(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, update telegramUpdate, documents []canonicalDocumentID) ([]evidenceRef, error) {
	prefix := agentScopedRefPrefix(sourceEventID, evidenceRefPhase)
	refs := make([]evidenceRef, len(documents))
	keys := make([]string, len(documents))
	for index := range documents {
		keys[index] = fmt.Sprintf("%s_ev%d", prefix, index+1)
		refs[index] = evidenceRef(keys[index])
	}
	encoded, _ := json.Marshal(keys)
	// Serialize concurrent issuers for the same source event, then reuse its one
	// evidence TOOL turn.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, "telegram_evidence_refs:"+sourceEventID); err != nil {
		return nil, err
	}
	var turnID string
	err := tx.QueryRow(ctx, `SELECT id FROM telegram_conversation_turn WHERE source_event_id=$1::uuid AND role='TOOL' AND tool_name='agent_evidence_refs' ORDER BY created_at LIMIT 1`, sourceEventID).Scan(&turnID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO telegram_conversation_turn(household_id,telegram_user_id,telegram_chat_id,source_event_id,role,message_text,tool_name,public_context_json,telegram_message_id) VALUES($1,$2,$3,$4::uuid,'TOOL','Agent evidence references.','agent_evidence_refs',jsonb_build_object('evidence_refs',$5::jsonb),$6) RETURNING id`,
			householdID, update.Message.From.ID, update.Message.Chat.ID, sourceEventID, string(encoded), update.Message.MessageID).Scan(&turnID)
	}
	if err != nil {
		return nil, err
	}
	for index, document := range documents {
		if _, err := tx.Exec(ctx, `INSERT INTO telegram_turn_reference(turn_id,ref_key,entity_type,entity_id,household_id,telegram_user_id,telegram_chat_id,expires_at) VALUES($1,$2,'EVIDENCE',$3::uuid,$4,$5,$6,now()+$7::interval) ON CONFLICT(turn_id,ref_key) DO UPDATE SET entity_id=excluded.entity_id,expires_at=excluded.expires_at`,
			turnID, keys[index], string(document), householdID, update.Message.From.ID, update.Message.Chat.ID, fmt.Sprintf("%d seconds", int(evidenceRefTTL.Seconds()))); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

// resolveEvidenceRef is the only place an evidenceRef becomes a canonical id. It
// re-checks household, Telegram user, chat, expiry, and the current state of the
// document, and returns a bounded outcome instead of ever falling back to "the
// most recent evidence". Non-resolved outcomes are counted.
func (p *Processor) resolveEvidenceRef(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, ref evidenceRef) (canonicalDocumentID, ceuOutcome, error) {
	outcome := func(value ceuOutcome) (canonicalDocumentID, ceuOutcome, error) {
		p.recordCEUOutcome(ctx, householdID, sourceEventID, value)
		return "", value, nil
	}
	if len(ref) > 64 || !evidenceRefPattern.MatchString(string(ref)) {
		return outcome(ceuReferenceInvalid)
	}
	var documentID string
	var live bool
	err := p.pool.QueryRow(ctx, `SELECT r.entity_id::text,r.expires_at>now()
		FROM telegram_turn_reference r JOIN telegram_conversation_turn t ON t.id=r.turn_id
		WHERE r.household_id=$1 AND r.telegram_user_id=$2 AND r.telegram_chat_id=$3
		  AND r.ref_key=$4 AND r.entity_type='EVIDENCE'
		ORDER BY t.created_at DESC LIMIT 1`,
		householdID, update.Message.From.ID, update.Message.Chat.ID, string(ref)).Scan(&documentID, &live)
	if errors.Is(err, pgx.ErrNoRows) {
		// Unknown, or issued to another household, user or chat: indistinguishable
		// on purpose, so a probe learns nothing about other scopes.
		return outcome(ceuReferenceInvalid)
	}
	if err != nil {
		return "", "", fmt.Errorf("resolve evidence reference: %w", err)
	}
	if !live {
		return outcome(ceuReferenceExpired)
	}
	var status string
	err = p.pool.QueryRow(ctx, `SELECT status FROM document WHERE id=$1 AND household_id=$2`, documentID, householdID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return outcome(ceuEvidenceNotFound)
	}
	if err != nil {
		return "", "", fmt.Errorf("check evidence state: %w", err)
	}
	// A document whose processing failed has nothing for a conversation to act on.
	if status == "FAILED" {
		return outcome(ceuEvidenceStale)
	}
	return canonicalDocumentID(documentID), ceuResolved, nil
}
