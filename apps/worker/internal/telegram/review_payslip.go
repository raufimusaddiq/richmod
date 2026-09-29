package telegram

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

func (p *Processor) continueProposalDateReview(ctx context.Context, sourceEventID, householdID, itemID string, update telegramUpdate) error {
	if _, err := p.pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',$2::bigint,'text',$3::text))`, update.Message.Chat.ID, update.Message.MessageID, "Balas dengan tanggal pembayaran, misalnya: 25 September 2026.")
	return err
}

// continueProposalAmountReview re-asks the one missing fact and leaves the
// proposal and review open, so an unusable amount never resolves the review.
func (p *Processor) continueProposalAmountReview(ctx context.Context, sourceEventID, householdID, itemID string, update telegramUpdate, message string) error {
	if _, err := p.pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'reply_to_message_id',$2::bigint,'text',$3::text))`, update.Message.Chat.ID, update.Message.MessageID, message)
	return err
}

func (p *Processor) processPayslipPolicyCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) (bool, error) {
	choice := map[string]string{"review:salary:primary": "PRIMARY_SALARY", "review:salary:ordinary": "ORDINARY_INCOME"}[data]
	if choice == "" {
		return false, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var itemID, proposalID, sourceID, documentID, userID, requestID string
	err = tx.QueryRow(ctx, `SELECT ri.id::text,ri.proposal_id::text,COALESCE(ri.source_event_id,p.source_event_id)::text,COALESCE(ri.document_id,NULLIF(p.metadata_json->>'document_id','')::uuid)::text,ti.user_id::text,r.id::text FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id JOIN review_request_recipient rr ON rr.review_request_id=r.id JOIN review_item ri ON ri.id=r.review_item_id JOIN transaction_proposal p ON p.id=ri.proposal_id JOIN telegram_identity ti ON ti.telegram_user_id=$2 AND ti.household_id=r.household_id AND ti.active JOIN household_member hm ON hm.household_id=r.household_id AND hm.user_id=ti.user_id AND hm.active WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 AND r.status='OPEN' AND r.expires_at>now() AND ri.review_type IN ('PAYSLIP_CONFIRMATION','MISSING_PAY_DATE') AND ri.status IN ('OPEN','PENDING_SEND') AND c.state='AWAITING_DETAIL'`, householdID, update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID).Scan(&itemID, &proposalID, &sourceID, &documentID, &userID, &requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if err != nil {
		return true, err
	}
	var decision []byte
	if err = tx.QueryRow(ctx, `SELECT decision FROM review_item WHERE id=$1 FOR UPDATE`, itemID).Scan(&decision); err != nil {
		return true, err
	}
	var contract struct {
		MissingFacts   []string `json:"missingFacts"`
		AllowedActions []string `json:"allowedActions"`
	}
	if json.Unmarshal(decision, &contract) != nil || !contains(contract.AllowedActions, choice) {
		return true, finishStaleReviewCallback(ctx, tx, sourceEventID, update)
	}
	if contains(contract.MissingFacts, "transaction_at") {
		err = reviewdomain.SetPayslipPolicy(ctx, tx, reviewdomain.PayslipPolicyCommand{HouseholdID: householdID, UserID: userID, ReviewItemID: itemID, ProposalID: proposalID, SourceEventID: sourceID, DocumentID: documentID, ReplySourceEventID: sourceEventID, Choice: choice})
		if err != nil {
			if errors.Is(err, reviewdomain.ErrPayslipReviewInvalid) {
				return true, finishStaleReviewCallback(ctx, tx, sourceEventID, update)
			}
			return true, err
		}
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
			return true, err
		}
		if err = enqueueReply(ctx, tx, update, "Kategori gaji disimpan. Balas pesan ini dengan tanggal pembayaran."); err != nil {
			return true, err
		}
		return true, tx.Commit(ctx)
	}
	result, err := reviewdomain.ResolvePayslipProposal(ctx, tx, reviewdomain.PayslipCommand{HouseholdID: householdID, UserID: userID, ReviewItemID: itemID, ProposalID: proposalID, SourceEventID: sourceID, DocumentID: documentID, ActorType: "TELEGRAM", Action: choice, Choice: choice})
	if err != nil {
		if errors.Is(err, reviewdomain.ErrPayslipReviewInvalid) {
			return true, finishStaleReviewCallback(ctx, tx, sourceEventID, update)
		}
		return true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'classification',$4::text)) ON CONFLICT DO NOTHING`, result.TransactionID, sourceEventID, requestID, choice); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-review',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return true, err
	}
	if err = enqueueReply(ctx, tx, update, "Slip gaji dikonfirmasi."); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}
