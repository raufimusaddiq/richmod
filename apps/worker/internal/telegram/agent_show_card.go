package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/plainlabel"
)

const (
	toolShowReviewCard = "show_review_card"
	showCardRepeatSecs = 180
	maxShowableCards   = 9
)

var cardRefPattern = regexp.MustCompile(`^k[1-9]$`)

type showCardArgs struct {
	CardRef string  `json:"card_ref"`
	Step    *string `json:"step"`
}

func showReviewCardTool() gateway.ToolDefinition {
	return gateway.ToolDefinition{
		Name:        toolShowReviewCard,
		Description: "Bring one waiting card from turn_context.open_review_cards into the chat so the user can finish it with a tap. Use it when the user talks about a waiting card, instead of answering the card yourself. card_ref is the card's card_ref. step is optional and only moves the card to a screen the user named: asset (pick the asset account), invest (pick the investment account) or category (pick the category); leave it null otherwise. Nothing is saved by this tool; the user confirms with a button. The result says whether the card was shown, already shown, already finished or not found.",
		Parameters: objectSchema(map[string]any{
			"card_ref": map[string]any{"type": "string", "description": "The card_ref of the waiting card, like k1."},
			"step":     map[string]any{"type": []string{"string", "null"}, "enum": []any{"asset", "invest", "category", nil}},
		}, []string{"card_ref", "step"}),
	}
}

func validateShowCardArgs(call gateway.ToolCall) (map[string]any, error) {
	args, err := decodeAgentArgsRaw[showCardArgs](call)
	if err != nil {
		return nil, err
	}
	if !cardRefPattern.MatchString(args.CardRef) {
		return nil, fmt.Errorf("card_ref must be a card_ref copied from open_review_cards, like k1")
	}
	if args.Step != nil && *args.Step != "asset" && *args.Step != "invest" && *args.Step != "category" {
		return nil, fmt.Errorf("step must be asset, invest, category or null")
	}
	return remarshal(args), nil
}

func showableCardRefs(cards []agentReviewBinding) (map[string]string, []map[string]any) {
	refs := map[string]string{}
	public := make([]map[string]any, 0, len(cards))
	for index := range cards {
		card := agentReviewBindingPublic(&cards[index])
		if _, ok := stepForBinding(cards[index]); ok && len(refs) < maxShowableCards {
			ref := fmt.Sprintf("k%d", len(refs)+1)
			refs[ref] = cards[index].ReviewRequestID
			card["card_ref"] = ref
		}
		public = append(public, card)
	}
	return refs, public
}

func showCardResult(result agentToolResult, outcome string, facts map[string]any) agentToolResult {
	if facts == nil {
		facts = map[string]any{}
	}
	facts["outcome"] = outcome
	result.Facts = facts
	return result
}

func (p *Processor) agentShowReviewCard(ctx context.Context, state *agentState, args map[string]any, result agentToolResult) (agentToolResult, error) {
	ref, _ := args["card_ref"].(string)
	hint, _ := args["step"].(string)
	requestID, known := state.CardRefs[ref]
	if !known {
		return showCardResult(result, "not_found", nil), nil
	}
	message := state.Update.Message
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	review, err := loadEngineReview(ctx, tx, state.HouseholdID, requestID, message.From.ID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return showCardResult(result, "not_found", nil), nil
	}
	if err != nil {
		return result, err
	}
	review.ChatID = message.Chat.ID
	var recipientID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM review_request_recipient WHERE review_request_id=$1 AND telegram_chat_id=$2 AND telegram_message_id IS NOT NULL AND delivery_status IN ('SENT','EXPIRED')`, requestID, message.Chat.ID).Scan(&recipientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return showCardResult(result, "not_found", nil), nil
	}
	if err != nil {
		return result, err
	}
	if itemOpen := review.ItemStatus == "OPEN" || review.ItemStatus == "PENDING_SEND"; itemOpen && (review.RequestStatus == "EXPIRED" || (review.RequestStatus == "OPEN" && !review.Unexpired)) {
		if err := renewShownProjection(ctx, tx, requestID, recipientID, message.Chat.ID); err != nil {
			return result, err
		}
		if review, err = loadEngineReview(ctx, tx, state.HouseholdID, requestID, message.From.ID, true); err != nil {
			return result, err
		}
		review.ChatID = message.Chat.ID
	}
	topic := plainlabel.Humanize(review.ReviewType)
	if label, ok := plainlabel.Label(review.ReviewType); ok {
		topic = label
	}
	if (review.RequestStatus != "OPEN" && review.RequestStatus != "PENDING_SEND" && review.RequestStatus != "EXPIRED") || (review.ItemStatus != "OPEN" && review.ItemStatus != "PENDING_SEND") {
		return showCardResult(result, "already_finished", map[string]any{"topic": topic}), nil
	}
	step, owned, err := p.currentStep(ctx, tx, state.HouseholdID, review)
	if err != nil {
		return result, err
	}
	if !owned {
		return showCardResult(result, "not_showable", map[string]any{"topic": topic}), nil
	}
	var recent bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM review_request_message m JOIN review_request_recipient rr ON rr.id=m.recipient_id AND rr.telegram_message_id=m.telegram_message_id
			WHERE m.review_request_id=$1 AND m.telegram_chat_id=$2 AND m.assistant_shown AND m.superseded_at IS NULL AND m.created_at>now()-make_interval(secs => $3::int))
		OR EXISTS(SELECT 1 FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND status IN ('PENDING','RUNNING')
			AND payload_json->>'review_request_id'=$1::text AND payload_json->>'chat_id'=$2::text AND payload_json->>'assistant_shown'='true'
			AND created_at>now()-make_interval(secs => $3::int))`, requestID, message.Chat.ID, showCardRepeatSecs).Scan(&recent); err != nil {
		return result, err
	}
	if recent {
		return showCardResult(result, "already_shown_recently", map[string]any{"topic": topic, "question": step.Ask}), nil
	}
	hintOutcome := ""
	if hint != "" {
		moved, ok, err := p.navigateToHint(ctx, tx, state.HouseholdID, review, step, hint)
		if err != nil {
			return result, err
		}
		hintOutcome = "hint_not_applied"
		if ok {
			step, hintOutcome = moved, "hint_applied"
			if err := refreshSiblingCards(ctx, tx, requestID, message.Chat.ID, step); err != nil {
				return result, err
			}
		}
	}
	if err := enqueueAssistantCard(ctx, tx, requestID, message.Chat.ID, message.MessageID, step); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	facts := map[string]any{"topic": topic, "question": step.Ask}
	if hintOutcome != "" {
		facts["step_hint"] = hintOutcome
	}
	return showCardResult(result, "card_shown", facts), nil
}

func renewShownProjection(ctx context.Context, tx pgx.Tx, requestID, recipientID string, chatID int64) error {
	if _, err := tx.Exec(ctx, `SELECT lock_review_card_chat($1)`, chatID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request_recipient rr SET delivery_status='SENT',card_expires_at=now()+interval '7 days'
		FROM review_request r
		WHERE rr.id=$1 AND r.id=rr.review_request_id AND rr.delivery_status IN ('EXPIRED','SENT')
		  AND (rr.delivery_status='EXPIRED' OR r.status='EXPIRED' OR COALESCE(rr.card_expires_at,r.expires_at)<=now())`, recipientID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE review_request SET status='OPEN',expires_at=GREATEST(expires_at,now()+interval '7 days') WHERE id=$1 AND (status='EXPIRED' OR expires_at<=now())`, requestID)
	return err
}

func (p *Processor) navigateToHint(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, hint string) (Step, bool, error) {
	var outcome stepOutcome
	var err error
	switch {
	case hint == "asset" && step.Fact == factAsset, hint == "invest" && step.Fact == factInvest, hint == "category" && step.Fact == factCategory:
		return step, true, nil
	case hint == "asset" && stepOffers(step, callbackAsset) && assetEntryAllowed(review, step):
		outcome, err = p.enterAccountStep(ctx, tx, householdID, review, factAsset)
	case hint == "invest" && transferReview(review.ReviewType) && step.Fact == factTransfer && stepOffers(step, callbackInvestment):
		outcome, err = p.enterAccountStep(ctx, tx, householdID, review, factInvest)
	case hint == "category" && transferReview(review.ReviewType) && step.Fact == factTransfer && stepOffers(step, callbackExpense):
		outcome, err = p.enterCategoryStep(ctx, tx, householdID, review)
	default:
		return step, false, nil
	}
	if err != nil {
		return step, false, err
	}
	return outcome.Step, true, nil
}

func enqueueAssistantCard(ctx context.Context, tx pgx.Tx, reviewID string, chatID, replyTo int64, step Step) error {
	payload := map[string]any{"chat_id": chatID, "reply_to_message_id": replyTo, "text": clean(step.Text(), 4000), "review_request_id": reviewID, "assistant_shown": true}
	if markup := step.Markup(); markup != nil {
		payload["reply_markup"] = markup
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',$1::jsonb)`, string(encoded))
	return err
}

func (p *Processor) answeredOnAssistantCard(ctx context.Context, householdID, sourceEventID, reviewItemID string, source interactionSource) bool {
	if sourceEventID == "" || reviewItemID == "" {
		return false
	}
	var shown bool
	err := p.pool.QueryRow(ctx, `WITH hit AS (
			SELECT COALESCE(NULLIF(pl.payload_json#>>'{callback_query,message,chat,id}','')::bigint,NULLIF(pl.payload_json#>>'{message,chat,id}','')::bigint,0) AS chat_id,
			       COALESCE(NULLIF(pl.payload_json#>>'{callback_query,message,message_id}','')::bigint,NULLIF(pl.payload_json#>>'{message,reply_to_message,message_id}','')::bigint,0) AS message_id
			FROM source_event_payload pl WHERE pl.source_event_id=$1::uuid)
		SELECT EXISTS(SELECT 1 FROM hit
			JOIN review_request r ON r.review_item_id=$2::uuid AND r.household_id=$3
			JOIN review_request_message m ON m.review_request_id=r.id AND m.telegram_chat_id=hit.chat_id
			JOIN review_request_recipient rr ON rr.id=m.recipient_id
			WHERE m.assistant_shown AND (m.telegram_message_id=hit.message_id OR ($4::boolean AND rr.telegram_message_id=m.telegram_message_id)))`,
		sourceEventID, reviewItemID, householdID, source != sourceTap).Scan(&shown)
	return err == nil && shown
}
