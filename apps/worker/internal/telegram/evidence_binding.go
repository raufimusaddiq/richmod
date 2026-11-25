package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

const agentWorkflowExactEvidence agentWorkflowScope = "EXACT_EVIDENCE"

const agentWorkflowEvidenceReadOnly agentWorkflowScope = "EVIDENCE_READ_ONLY"

type agentEvidenceBinding struct {
	Document             canonicalDocumentID
	Context              map[string]any
	HasLinkedTransaction bool
}

func (p *Processor) BindEvidenceMessage(ctx context.Context, chatID, messageID int64, documentID string) error {
	if chatID == 0 || messageID == 0 || documentID == "" {
		return nil
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO telegram_message_binding(household_id,telegram_chat_id,telegram_message_id,entity_type,entity_id)
		SELECT d.household_id,$1,$2,'DOCUMENT',d.id FROM document d WHERE d.id=$3::uuid
		ON CONFLICT(telegram_chat_id,telegram_message_id) DO NOTHING`, chatID, messageID, documentID)
	return err
}

func (p *Processor) resolveReplyEvidence(ctx context.Context, householdID string, update telegramUpdate) (canonicalDocumentID, bool, error) {
	if update.Message.ReplyToMessage == nil || update.Message.ReplyToMessage.MessageID == 0 {
		return "", false, nil
	}
	chatID, messageID := update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID
	var documentID string
	err := p.pool.QueryRow(ctx, `SELECT b.entity_id::text FROM telegram_message_binding b
		JOIN document d ON d.id=b.entity_id AND d.household_id=b.household_id
		WHERE b.household_id=$1 AND b.telegram_chat_id=$2 AND b.telegram_message_id=$3 AND b.entity_type='DOCUMENT'`,
		householdID, chatID, messageID).Scan(&documentID)
	if err == nil {
		return canonicalDocumentID(documentID), true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("resolve bound notice: %w", err)
	}
	err = p.pool.QueryRow(ctx, `SELECT document_id FROM (
			SELECT d.id::text AS document_id FROM source_event s JOIN document d ON d.source_event_id=s.id AND d.household_id=s.household_id
			WHERE s.household_id=$1 AND s.telegram_chat_id=$2 AND s.telegram_message_id=$3 AND s.source_type='TELEGRAM_IMAGE'
			UNION ALL
			SELECT dp.document_id::text FROM source_event s JOIN document_page dp ON dp.source_event_id=s.id
			JOIN document d ON d.id=dp.document_id AND d.household_id=s.household_id
			WHERE s.household_id=$1 AND s.telegram_chat_id=$2 AND s.telegram_message_id=$3 AND s.source_type='TELEGRAM_IMAGE'
		) found LIMIT 1`, householdID, chatID, messageID).Scan(&documentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve replied upload: %w", err)
	}
	return canonicalDocumentID(documentID), true, nil
}

func (p *Processor) reviewBindingForDocument(ctx context.Context, householdID string, chatID int64, document canonicalDocumentID) (*agentReviewBinding, error) {
	messageID, err := p.reviewCardMessageForDocument(ctx, householdID, chatID, document)
	if err != nil || messageID == 0 {
		return nil, err
	}
	return p.exactAgentReviewBinding(ctx, householdID, chatID, messageID)
}

func (p *Processor) reviewCardMessageForDocument(ctx context.Context, householdID string, chatID int64, document canonicalDocumentID) (int64, error) {
	var messageID int64
	err := p.pool.QueryRow(ctx, `SELECT rr.telegram_message_id
		FROM review_request r
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		JOIN review_item ri ON ri.id=r.review_item_id
		WHERE r.household_id=$1 AND r.status='OPEN' AND rr.telegram_chat_id=$2 AND rr.telegram_message_id IS NOT NULL
		  AND (ri.document_id=$3::uuid
		       OR ri.source_event_id IN (SELECT source_event_id FROM document WHERE id=$3::uuid UNION SELECT source_event_id FROM document_page WHERE document_id=$3::uuid)
		       OR COALESCE(ri.transaction_id,r.transaction_id) IN (SELECT te.transaction_id FROM transaction_evidence te WHERE COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE' AND te.source_event_id IN (
		            SELECT source_event_id FROM document WHERE id=$3::uuid UNION SELECT source_event_id FROM document_page WHERE document_id=$3::uuid)))
		ORDER BY r.created_at DESC LIMIT 1`, householdID, chatID, string(document)).Scan(&messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find review for evidence: %w", err)
	}
	return messageID, nil
}

func (p *Processor) replyTargetForEvidenceReview(ctx context.Context, householdID string, update telegramUpdate) (telegramUpdate, error) {
	document, found, err := p.resolveReplyEvidence(ctx, householdID, update)
	if err != nil {
		return update, err
	}
	if !found {
		return update, nil
	}
	card, err := p.reviewCardMessageForDocument(ctx, householdID, update.Message.Chat.ID, document)
	if err != nil || card == 0 {
		return update, err
	}
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: card}
	return update, nil
}

func (p *Processor) repliesToProposalReview(ctx context.Context, householdID string, update telegramUpdate) (bool, error) {
	if update.Message.ReplyToMessage == nil || update.Message.ReplyToMessage.MessageID == 0 {
		return false, nil
	}
	var found bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM review_request r
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		JOIN review_item ri ON ri.id=r.review_item_id
		WHERE r.household_id=$1 AND r.status='OPEN' AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3
		  AND ri.proposal_id IS NOT NULL AND ri.transaction_id IS NULL AND r.transaction_id IS NULL)`,
		householdID, update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID).Scan(&found)
	return found, err
}

func (p *Processor) documentForReviewBinding(ctx context.Context, householdID string, binding *agentReviewBinding) (canonicalDocumentID, bool, error) {
	if binding == nil || binding.ReviewRequestID == "" {
		return "", false, nil
	}
	var documentID string
	err := p.pool.QueryRow(ctx, `SELECT d.id::text FROM review_request r
		JOIN review_item ri ON ri.id=r.review_item_id
		JOIN document d ON d.household_id=r.household_id AND (
		     d.id=ri.document_id OR d.source_event_id=ri.source_event_id
		     OR d.source_event_id IN (SELECT te.source_event_id FROM transaction_evidence te WHERE COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE' AND te.transaction_id=COALESCE(ri.transaction_id,r.transaction_id))
		     OR d.id IN (SELECT dp.document_id FROM document_page dp WHERE dp.source_event_id IN (
		          SELECT te.source_event_id FROM transaction_evidence te WHERE COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE' AND te.transaction_id=COALESCE(ri.transaction_id,r.transaction_id))))
		WHERE r.id=$1::uuid AND r.household_id=$2 ORDER BY d.created_at DESC LIMIT 1`, binding.ReviewRequestID, householdID).Scan(&documentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find evidence for review: %w", err)
	}
	return canonicalDocumentID(documentID), true, nil
}

func (p *Processor) newEvidenceBinding(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, document canonicalDocumentID) (*agentEvidenceBinding, error) {
	contexts, err := p.loadEvidenceContexts(ctx, householdID, sourceEventID, update, []canonicalDocumentID{document})
	if err != nil {
		return nil, err
	}
	if len(contexts) != 1 {
		return nil, nil
	}
	_, linked := contexts[0]["canonical"]
	return &agentEvidenceBinding{Document: document, Context: contexts[0], HasLinkedTransaction: linked}, nil
}

func (p *Processor) bindTurnEvidence(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, reviewBinding *agentReviewBinding, explicitReply bool) (*agentEvidenceBinding, *agentReviewBinding, error) {
	if reviewBinding != nil {
		document, found, err := p.documentForReviewBinding(ctx, householdID, reviewBinding)
		if err != nil || !found {
			return nil, reviewBinding, err
		}
		evidence, err := p.newEvidenceBinding(ctx, householdID, sourceEventID, update, document)
		if err != nil {
			return nil, reviewBinding, err
		}
		outcome := ceuActiveReviewBinding
		if explicitReply {
			outcome = ceuExactReplyBinding
		}
		p.recordCEUOutcome(ctx, householdID, sourceEventID, outcome)
		return evidence, reviewBinding, nil
	}
	if !explicitReply {
		return nil, nil, nil
	}
	document, found, err := p.resolveReplyEvidence(ctx, householdID, update)
	if err != nil || !found {
		return nil, nil, err
	}
	evidence, err := p.newEvidenceBinding(ctx, householdID, sourceEventID, update, document)
	if err != nil {
		return nil, nil, err
	}
	review, err := p.reviewBindingForDocument(ctx, householdID, update.Message.Chat.ID, document)
	if err != nil {
		return evidence, nil, err
	}
	p.recordCEUOutcome(ctx, householdID, sourceEventID, ceuExactReplyBinding)
	return evidence, review, nil
}

func applyEvidenceToolPolicy(general, filtered []gateway.ToolDefinition, scope agentWorkflowScope, evidence *agentEvidenceBinding) ([]gateway.ToolDefinition, agentWorkflowScope) {
	if evidence == nil || scope != agentWorkflowExplicitUnbound {
		return filtered, scope
	}
	if evidence.HasLinkedTransaction {
		for _, tool := range general {
			if tool.Name == "propose_transaction_correction" {
				return append(append([]gateway.ToolDefinition{}, filtered...), tool, evidenceDisputeTool()), agentWorkflowExactEvidence
			}
		}
	}
	return filtered, agentWorkflowEvidenceReadOnly
}
