package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type evidenceRef string

type canonicalDocumentID string

const (
	evidenceRefTTL        = 60 * time.Minute
	maxEvidenceItems      = 5
	evidenceRefPhase      = "p0r0"
	evidenceLinkedTxPhase = "p9r0"
)

var evidenceRefPattern = regexp.MustCompile(`^a[0-9a-f]{8}_p[0-9]+r[0-9]+_ev[0-9]+$`)

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
	ceuResolved              ceuOutcome = "RESOLVED"
)

var ceuTelemetryOutcomes = map[ceuOutcome]bool{
	ceuExactReplyBinding: true, ceuActiveReviewBinding: true, ceuOpaqueRefBinding: true,
	ceuRecentContextBinding: true, ceuSemanticDisambiguated: true, ceuAmbiguousContext: true,
	ceuReferenceExpired: true, ceuReferenceInvalid: true, ceuEvidenceNotFound: true, ceuEvidenceStale: true,
}

func (p *Processor) recordCEUOutcome(ctx context.Context, householdID, sourceEventID string, outcome ceuOutcome) {
	if p.pool == nil || !ceuTelemetryOutcomes[outcome] {
		return
	}
	_, _ = p.pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,source_event_id,event_type,action)
		VALUES($1,NULLIF($2,'')::uuid,'CEU_BINDING',$3)`, householdID, sourceEventID, string(outcome))
}

func evidenceRefKey(prefix string, document canonicalDocumentID) string {
	sum := sha256.Sum256([]byte(document))
	return prefix + "_ev" + strconv.FormatUint(binary.BigEndian.Uint64(sum[:8]), 10)
}

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
	for index, document := range documents {
		keys[index] = evidenceRefKey(prefix, document)
		refs[index] = evidenceRef(keys[index])
	}
	encoded, _ := json.Marshal(keys)
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
	if status == "FAILED" {
		return outcome(ceuEvidenceStale)
	}
	return canonicalDocumentID(documentID), ceuResolved, nil
}
