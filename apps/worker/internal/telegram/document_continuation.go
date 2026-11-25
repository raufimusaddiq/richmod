package telegram

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type reprocessingCard struct {
	RequestID string
	ItemID    string
}

func markReprocessContinued(ctx context.Context, tx pgx.Tx, requestID string) error {
	_, err := tx.Exec(ctx, `UPDATE review_conversation SET context_json=jsonb_set(context_json,'{reprocess,continued}',to_jsonb(now())),updated_at=now() WHERE review_request_id=$1`, requestID)
	return err
}

func ContinueDocumentCard(ctx context.Context, tx pgx.Tx, documentID, text string) (bool, error) {
	var card reprocessingCard
	var continued, sameRun bool
	err := tx.QueryRow(ctx, `SELECT r.id::text,r.review_item_id::text,c.context_json->'reprocess'->>'continued' IS NOT NULL,
			COALESCE((c.context_json->'reprocess'->>'continued')::timestamptz=now(),false)
		FROM review_conversation c JOIN review_request r ON r.id=c.review_request_id
		WHERE c.context_json ? 'reprocess' AND c.context_json->'reprocess'->>'document_id'=$1
		ORDER BY (c.context_json->'reprocess'->>'continued' IS NULL) DESC,c.updated_at DESC LIMIT 1 FOR UPDATE OF c`, documentID).Scan(&card.RequestID, &card.ItemID, &continued, &sameRun)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if continued {
		return sameRun, nil
	}
	_, _, copies, err := loadCardCopies(ctx, tx, card.ItemID)
	if err != nil || len(copies) == 0 {
		return false, err
	}
	for _, copy := range copies {
		if err := settleReviewCardInPlace(ctx, tx, card.RequestID, copy.ChatID, copy.MessageID, text, nil); err != nil {
			return false, err
		}
	}
	return true, markReprocessContinued(ctx, tx, card.RequestID)
}

func continueReprocessedCard(ctx context.Context, tx pgx.Tx, householdID, newRequestID, itemID, cardText string, markup *InlineKeyboardMarkup) (bool, error) {
	var card reprocessingCard
	err := tx.QueryRow(ctx, `SELECT r.id::text,r.review_item_id::text
		FROM review_conversation c JOIN review_request r ON r.id=c.review_request_id
		JOIN document d ON d.id=NULLIF(c.context_json->'reprocess'->>'document_id','')::uuid AND d.household_id=r.household_id
		WHERE r.household_id=$1 AND r.id<>$2 AND c.context_json ? 'reprocess' AND c.context_json->'reprocess'->>'continued' IS NULL
		  AND (d.id=(SELECT ri.document_id FROM review_item ri WHERE ri.id=$3)
		    OR d.source_event_id IN (
		      SELECT ri.source_event_id FROM review_item ri WHERE ri.id=$3 AND ri.source_event_id IS NOT NULL
		      UNION SELECT tp.source_event_id FROM review_item ri JOIN transaction_proposal tp ON tp.id=ri.proposal_id WHERE ri.id=$3
		      UNION SELECT te.source_event_id FROM review_item ri JOIN transaction_evidence te ON te.transaction_id=ri.transaction_id WHERE ri.id=$3))
		ORDER BY c.updated_at DESC LIMIT 1 FOR UPDATE OF c`, householdID, newRequestID, itemID).Scan(&card.RequestID, &card.ItemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, _, copies, err := loadCardCopies(ctx, tx, card.ItemID)
	if err != nil || len(copies) == 0 {
		return false, err
	}
	if err := markReprocessContinued(ctx, tx, card.RequestID); err != nil {
		return false, err
	}
	return true, moveCardsToRequest(ctx, tx, card.RequestID, newRequestID, copies, cardText, markup)
}
