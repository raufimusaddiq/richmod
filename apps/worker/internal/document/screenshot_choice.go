package document

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// jeverifier is the seam onto the bounded judgment plane, expressed in the terms
// this package needs so document intake never imports Telegram policy.
type jeverifier interface {
	Evaluate(context.Context, string, judgment.Request) (judgment.Result, error)
}

// ScreenshotRowCategoryPolicyVersion marks the thresholds behind a row-level
// category ruling, so a stored decision stays reproducible (PRD §21).
const ScreenshotRowCategoryPolicyVersion = "2026-09-screenshot-category2"

// rowCategoryPolicy is the same decisive-answer bar the bank-email category and
// Telegram category policies use (MinTop .85 / MinMargin .20): a confident,
// well-separated top category, otherwise the row asks one question instead of
// guessing. Bump ScreenshotRowCategoryPolicyVersion when the bar moves.
var rowCategoryPolicy = judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60}

// rowChoiceProvenance records what one batched bounded request decided, so the
// canonical mutation keeps its decision provenance (PRD §21, ADR-038).
type rowChoiceProvenance struct {
	Model         string
	PolicyVersion string
	Questions     int
	Decided       int
	QuestionKeys  []string
}

func rowQuestionKey(index int) string { return fmt.Sprintf("row_%03d", index) }

func rowIndexFromKey(key string) (int, bool) {
	var index int
	if _, err := fmt.Sscanf(key, "row_%d", &index); err != nil {
		return 0, false
	}
	return index, true
}

// resolveRowCategories rules on the category of every unmatched OUT row in one
// bounded request (PRD §11.3: one decision set per image, not one call per row).
// Only a decisive, well-separated answer is returned; anything undecided stays
// out of the map so the row keeps a minimal review. A provider failure is
// returned as an error, never as an implicit approval.
func (p *Processor) resolveRowCategories(ctx context.Context, sourceEventID string, rows []validatedScreenshotRow, categories []categoryOption) (map[int]string, rowChoiceProvenance, error) {
	provenance := rowChoiceProvenance{PolicyVersion: ScreenshotRowCategoryPolicyVersion}
	if p.verifier == nil || len(categories) < 2 {
		return nil, provenance, nil
	}
	bySlug := make(map[string]string, len(categories))
	slugs := make([]string, 0, len(categories))
	for _, category := range categories {
		bySlug[category.Slug] = category.ID
		slugs = append(slugs, category.Slug)
	}
	criteria := judgment.CategoryCriteria(slugs)
	state := map[string]any{}
	questions := map[string]judgment.Question{}
	for index, row := range rows {
		// Vision already had the first chance to choose from this exact bounded
		// category set. Only unresolved rows enter the Jev rescue lane.
		if row.Matched != nil || row.Type != "EXPENSE" || row.CategoryDecided || row.CategoryID != nil {
			continue
		}
		key := rowQuestionKey(index)
		state[key] = map[string]any{
			// Image-derived text is untrusted: delimit it so a crafted merchant cannot
			// steer the ruling that authorises a canonical write.
			"merchant":       "<untrusted_row_merchant>" + strings.TrimSpace(row.Value.Merchant) + "</untrusted_row_merchant>",
			"description":    "<untrusted_row_description>" + strings.TrimSpace(row.Value.Description) + "</untrusted_row_description>",
			"amount_idr":     row.Value.Amount,
			"transaction_at": row.Value.TransactionAt,
		}
		provenance.QuestionKeys = append(provenance.QuestionKeys, key)
		questions[key] = judgment.Question{Type: "choice", Instructions: "Which single household category best describes this expense row of a transaction screenshot? Answer with the closest supplied category slug, or OTHER_OR_UNCLEAR when no category is safe.", Criteria: criteria}
	}
	if len(questions) == 0 {
		return nil, provenance, nil
	}
	result, err := p.verifier.Evaluate(ctx, sourceEventID+"-screenshot-category", judgment.Request{State: state, Questions: questions})
	if err != nil {
		return nil, provenance, err
	}
	provenance.Model, provenance.Questions = result.Model, len(questions)
	decided := map[int]string{}
	for key, answer := range result.Answers {
		index, ok := rowIndexFromKey(key)
		if !ok || !judgment.AcceptChoice(answer, criteria, rowCategoryPolicy) {
			continue
		}
		if id, found := bySlug[answer.Choice]; found {
			decided[index] = id
		}
	}
	provenance.Decided = len(decided)
	return decided, provenance, nil
}

// confirmScreenshotRow writes one auto-confirmed expense row: an accepted
// proposal, the confirmed transaction, its evidence link, and an audit entry
// naming the policy that authorised the write without a human (PRD §17, §21).
func confirmScreenshotRow(ctx context.Context, tx pgx.Tx, householdID, sourceID, documentID, proposalKey string, index int, row validatedScreenshotRow, provenance rowChoiceProvenance) error {
	var merchantID *string
	if merchant := strings.TrimSpace(row.Value.Merchant); merchant != "" {
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,regexp_replace(trim($2), '[[:space:]]+', ' ', 'g')) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET updated_at=now() RETURNING id`, householdID, merchant).Scan(&id); err != nil {
			return err
		}
		merchantID = &id
	}
	var proposalID string
	if err := tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,description,confidence,proposal_status,metadata_json) VALUES($1,$2,$3,'EXPENSE',$4,'IDR',$5,NULLIF($6,''),$7,NULLIF($8,''),$9,'ACCEPTED',jsonb_build_object('document_id',$10::uuid,'row_index',$11::integer,'direction',$12::text,'date_known',$13::boolean,'auto_confirm',true,'category_policy_version',$14::text)) RETURNING id`, householdID, sourceID, proposalKey, row.Value.Amount, row.TransactionAt, row.Value.Merchant, row.CategoryID, row.Value.Description, row.Value.Confidence, documentID, index, row.Value.Direction, row.DateKnown, provenance.PolicyVersion).Scan(&proposalID); err != nil {
		return err
	}
	var transactionID string
	if err := tx.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,category_id,description,source_confidence,classification_confidence,confirmed_at,auto_confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',$2,'IDR',$3,$4,$5,NULLIF($6,''),$7,$8,now(),now()) RETURNING id`, householdID, row.Value.Amount, row.TransactionAt, merchantID, row.CategoryID, row.Value.Description, row.Value.Confidence, row.Value.Confidence).Scan(&transactionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TRANSACTION_SCREENSHOT',$3,jsonb_build_object('proposal_id',$4::uuid,'document_id',$5::uuid,'row_index',$6::integer))`, transactionID, sourceID, row.Value.Confidence, proposalID, documentID, index); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'WORKER','AUTO_CONFIRM_SCREENSHOT_ROW','transaction',$2,jsonb_build_object('document_id',$3::uuid,'row_index',$4::integer,'direction',$5::text,'category_policy_version',$6::text))`, householdID, transactionID, documentID, index, row.Value.Direction, provenance.PolicyVersion)
	return err
}

// screenshotRowDecision builds the PRD §7 contract for one unresolved row, so the
// Inbox asks only the dimension that is genuinely missing instead of reshowing
// amount, direction, and time that Go already holds.
func screenshotRowDecision(household, sourceEventID, transactionID, reviewType string, index int, row validatedScreenshotRow) reviewdec.Decision {
	known := map[string]any{"amount_idr": row.Value.Amount, "direction": row.Value.Direction}
	if row.DateKnown {
		known["transaction_at"] = row.TransactionAt.Format(time.RFC3339)
	} else {
		// PRD §18.3: a fallback time keeps its provenance instead of pretending
		// the source printed it.
		known["transaction_time_source"] = "RECEIVED_AT_FALLBACK"
	}
	if merchant := strings.TrimSpace(row.Value.Merchant); merchant != "" {
		known["merchant"] = merchant
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
	if row.CategoryDecisionSource == reviewdec.SourceJev {
		decision.DecisionSource = reviewdec.SourceGenerativePlusJev
	}
	// PRD §37: one reason code resolves to exactly one contract, so a screenshot
	// row stores the same decision the bank-email and Telegram paths store. Jev is
	// a rescue for unresolved categories, never a mandatory second opinion after
	// a decisive vision result.
	if shared, ok := reviewdec.Preset(reviewType, "transaction", transactionID); ok {
		decision.DecisionClass = shared.DecisionClass
		decision.MissingFacts = shared.MissingFacts
		decision.AllowedActions = shared.AllowedActions
		decision.InteractionMode = shared.InteractionMode
		decision.WhyNotAuto = shared.WhyNotAuto
	}
	switch reviewType {
	case "TRANSFER_CLASSIFICATION":
		decision.DecisionClass, decision.InteractionMode = reviewdec.ClassHumanPolicyChoice, reviewdec.ModePolicyChoice
		decision.MissingFacts = []string{"transfer_relationship"}
		decision.WhyNotAuto = "evidence cannot separate income from an own-account or household transfer on a screenshot row"
	default:
		if row.CategoryConflict {
			decision.DecisionClass, decision.InteractionMode = reviewdec.ClassEvidenceConflict, reviewdec.ModeConflictResolution
			decision.WhyNotAuto = "the image and the bounded plane named different categories"
		}
	}
	return decision
}

// screenshotSummary is the PRD §11.4 batch summary.
func screenshotSummary(found, recorded, linked, pending int) string {
	lines := []string{fmt.Sprintf("%d transaksi ditemukan.", found)}
	if recorded > 0 {
		lines = append(lines, fmt.Sprintf("✓ %d berhasil dicatat", recorded))
	}
	if linked > 0 {
		lines = append(lines, fmt.Sprintf("✓ %d cocok dengan transaksi yang sudah ada", linked))
	}
	if pending > 0 {
		lines = append(lines, fmt.Sprintf("! %d butuh keputusan", pending))
	}
	return strings.Join(lines, "\n")
}

func enqueueScreenshotSummary(ctx context.Context, tx pgx.Tx, chatID int64, summary string) error {
	_, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'text',$2::text))`, chatID, summary)
	return err
}