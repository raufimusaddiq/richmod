package document

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	workerTelegram "github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

const ScreenshotRowCategoryPolicyVersion = "2026-11-screenshot-generative1"

func confirmScreenshotRow(ctx context.Context, tx pgx.Tx, householdID, sourceID, documentID, proposalKey string, index int, row validatedScreenshotRow) error {
	merchantID, err := canonicalMerchantID(ctx, tx, householdID, row.Value.Merchant, row.MerchantID)
	if err != nil {
		return err
	}
	var proposalID string
	if err := tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,description,confidence,proposal_status,metadata_json) VALUES($1,$2,$3,'EXPENSE',$4,'IDR',$5,NULLIF($6,''),$7,NULLIF($8,''),$9,'ACCEPTED',jsonb_build_object('document_id',$10::uuid,'row_index',$11::integer,'direction',$12::text,'date_known',$13::boolean,'auto_confirm',true,'category_policy_version',$14::text) || $15::jsonb) RETURNING id`, householdID, sourceID, proposalKey, row.Value.Amount, row.TransactionAt, row.Value.Merchant, row.CategoryID, row.Value.Description, row.Value.Confidence, documentID, index, row.Value.Direction, row.DateKnown, ScreenshotRowCategoryPolicyVersion, screenshotIdentityMetadata(row)).Scan(&proposalID); err != nil {
		return err
	}
	var transactionID string
	if err := tx.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,category_id,description,source_confidence,classification_confidence,confirmed_at,auto_confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',$2,'IDR',$3,$4,$5,NULLIF($6,''),$7,$8,now(),now()) RETURNING id`, householdID, row.Value.Amount, row.TransactionAt, merchantID, row.CategoryID, row.Value.Description, row.Value.Confidence, row.classificationConfidence()).Scan(&transactionID); err != nil {
		return err
	}
	if err := reviewdomain.RefreshOpenCycleResiduals(ctx, tx, householdID, row.TransactionAt, ""); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TRANSACTION_SCREENSHOT',$3,jsonb_build_object('proposal_id',$4::uuid,'document_id',$5::uuid,'row_index',$6::integer))`, transactionID, sourceID, row.Value.Confidence, proposalID, documentID, index); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'WORKER','AUTO_CONFIRM_SCREENSHOT_ROW','transaction',$2,jsonb_build_object('document_id',$3::uuid,'row_index',$4::integer,'direction',$5::text,'category_policy_version',$6::text))`, householdID, transactionID, documentID, index, row.Value.Direction, ScreenshotRowCategoryPolicyVersion)
	return err
}

func screenshotRowDecision(household, sourceEventID, transactionID, reviewType string, index int, row validatedScreenshotRow) reviewdec.Decision {
	known := map[string]any{"direction": row.Value.Direction}
	if row.Value.Amount != nil {
		known["amount_idr"] = *row.Value.Amount
	}
	if row.DateKnown {
		known["transaction_at"] = row.TransactionAt.Format(time.RFC3339)
	} else {
		known["transaction_time_source"] = "RECEIVED_AT_FALLBACK"
	}
	if merchant := strings.TrimSpace(row.Value.Merchant); merchant != "" {
		known["merchant"] = merchant
	}
	if len(row.PriorMatchIDs) > 0 {
		known["prior_match_transaction_ids"] = row.PriorMatchIDs
	}
	categoryKnown := row.CategoryDecided && row.CategoryID != nil
	if categoryKnown && row.CategorySlug != "" {
		known["category"] = row.CategorySlug
	}
	decision := reviewdec.Decision{
		Version:         reviewdec.Version,
		Subject:         reviewdec.Subject{Type: "transaction", ID: transactionID},
		SourceEventID:   sourceEventID,
		ReasonCode:      reviewType,
		KnownFacts:      known,
		Provenance:      map[string]any{"pipeline": "transaction-screenshot", "document_row": index},
		EvidenceRefs:    []reviewdec.EvidenceRef{{Kind: "source_event", ID: sourceEventID}},
		DecisionSource:  reviewdec.SourceGenerativeExtraction,
		PolicyVersion:   ScreenshotRowCategoryPolicyVersion,
		InteractionMode: reviewdec.ModeBoundedChoice,
	}
	if shared, ok := reviewdec.Preset(reviewType, "transaction", transactionID); ok {
		decision.DecisionClass = shared.DecisionClass
		decision.MissingFacts = shared.MissingFacts
		decision.AllowedActions = shared.AllowedActions
		decision.InteractionMode = shared.InteractionMode
		decision.WhyNotAuto = shared.WhyNotAuto
	}
	switch reviewType {
	case "MISSING_AMOUNT":
		decision.Subject.Type = "proposal"
		decision.DecisionClass, decision.InteractionMode = reviewdec.ClassEvidenceGap, reviewdec.ModeSingleField
		decision.MissingFacts = []string{"amount"}
		if row.Type == "EXPENSE" && !categoryKnown {
			decision.MissingFacts = append(decision.MissingFacts, "category")
		}
		if !row.DateKnown {
			decision.MissingFacts = append(decision.MissingFacts, "transaction_at")
		}
		decision.AllowedActions = []string{"SET_AMOUNT", "IGNORE"}
		if row.Type == "INCOME" {
			decision.MissingFacts = append(decision.MissingFacts, "transfer_relationship")
		}
		decision.WhyNotAuto = "the screenshot does not show an amount"
	case "TRANSFER_CLASSIFICATION":
		decision.DecisionClass, decision.InteractionMode = reviewdec.ClassHumanPolicyChoice, reviewdec.ModePolicyChoice
		decision.MissingFacts = []string{"transfer_relationship"}
		decision.WhyNotAuto = "evidence cannot separate income from an own-account or household transfer on a screenshot row"
	case "POSSIBLE_DUPLICATE":
	default:
		decision.MissingFacts = screenshotMissingFacts(categoryKnown, row.DateKnown)
	}
	if reviewType == "TRANSFER_CLASSIFICATION" || reviewType == "POSSIBLE_DUPLICATE" {
		if row.Type == "EXPENSE" && !categoryKnown {
			decision.MissingFacts = append(decision.MissingFacts, "category")
		}
		if !row.DateKnown {
			decision.MissingFacts = append(decision.MissingFacts, "transaction_at")
		}
		if reviewType == "POSSIBLE_DUPLICATE" && row.Type == "INCOME" {
			decision.MissingFacts = append(decision.MissingFacts, "transfer_relationship")
		}
	}
	return decision
}

func screenshotMissingFacts(categoryKnown, dateKnown bool) []string {
	missing := make([]string, 0, 2)
	if !categoryKnown {
		missing = append(missing, "category")
	}
	if !dateKnown {
		missing = append(missing, "transaction_at")
	}
	return missing
}

func screenshotReviewReason(row validatedScreenshotRow, possibleDuplicate bool) string {
	if possibleDuplicate {
		return "POSSIBLE_DUPLICATE"
	}
	if row.Type == "INCOME" {
		return "TRANSFER_CLASSIFICATION"
	}
	if row.MerchantReview {
		return "UNKNOWN_MERCHANT"
	}
	categoryKnown := row.CategoryDecided && row.CategoryID != nil
	switch {
	case !categoryKnown && !row.DateKnown:
		return "TRANSACTION_FACTS_MISSING"
	case !categoryKnown:
		return "AMBIGUOUS_CATEGORY"
	case !row.DateKnown:
		return "MISSING_TRANSACTION_DATE"
	default:
		return "AMBIGUOUS_CATEGORY"
	}
}

func screenshotSummary(found, recorded, linked, pending int) string {
	lines := []string{fmt.Sprintf("%d transaksi ditemukan.", found)}
	if recorded > 0 {
		lines = append(lines, fmt.Sprintf("✅ %d tersimpan", recorded))
	}
	if linked > 0 {
		lines = append(lines, fmt.Sprintf("✅ %d cocok dengan transaksi yang sudah ada", linked))
	}
	if pending > 0 {
		lines = append(lines, fmt.Sprintf("⏳ %d menunggu jawabanmu", pending))
	}
	return strings.Join(lines, "\n")
}

func enqueueScreenshotSummary(ctx context.Context, tx pgx.Tx, chatID int64, documentID, summary string) error {
	if continued, err := workerTelegram.ContinueDocumentCard(ctx, tx, documentID, summary); err != nil || continued {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'text',$2::text,'bind_document_id',$3::text))`, chatID, summary, documentID)
	return err
}
