package telegram

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

const (
	toolProposeAssetPurchase  = "propose_asset_purchase"
	assetPurchaseWindow       = "30 minutes"
	assetPurchaseStagedAction = "ASSET_PURCHASE_STAGED"
	assetPurchaseStaleMessage = "Usulan pembelian aset ini sudah tidak berlaku, jadi belum ada yang dicatat."
)

type assetPurchaseArgs struct {
	SourceAccountHint string  `json:"source_account_hint"`
	WealthAccountHint *string `json:"wealth_account_hint"`
	DateReference     string  `json:"date_reference"`
	ExplicitDate      *string `json:"explicit_date"`
	LocalTime         *string `json:"local_time"`
}

func assetPurchaseOffered(binding *agentReviewBinding) bool {
	return binding != nil && binding.Kind == wealthObservationBindingKind && binding.TargetID != "" && binding.ReviewRequestID != ""
}

func proposeAssetPurchaseTool() gateway.ToolDefinition {
	nullString := map[string]any{"type": []string{"string", "null"}}
	return gateway.ToolDefinition{
		Name:        toolProposeAssetPurchase,
		Description: "Propose that the balance screenshot the user is replying to was really an asset purchase. Call it only when the user says so and names the bank or cash account the money left and when it was paid. Go checks the accounts and stages the proposal; nothing is recorded until the user taps Ya on the confirmation message. wealth_account_hint is null when the screenshot already names the account.",
		Parameters: objectSchema(map[string]any{
			"source_account_hint": map[string]any{"type": "string", "description": "Name of the account the money was paid from."},
			"wealth_account_hint": nullString,
			"date_reference":      map[string]any{"type": "string", "enum": []string{"TODAY", "YESTERDAY", "EXPLICIT"}},
			"explicit_date":       explicitDateSchema(),
			"local_time":          map[string]any{"type": []string{"string", "null"}, "description": "Exact local HH:MM (24-hour). Null when the user gave no time."},
		}, []string{"source_account_hint", "wealth_account_hint", "date_reference", "explicit_date", "local_time"}),
	}
}

func validateProposeAssetPurchase(call gateway.ToolCall) (map[string]any, error) {
	args, err := decodeAgentArgsRaw[assetPurchaseArgs](call)
	if err != nil {
		return nil, err
	}
	if hint := strings.TrimSpace(args.SourceAccountHint); hint == "" || len(hint) > 120 {
		return nil, fmt.Errorf("source_account_hint must name the account the money was paid from")
	}
	if args.DateReference != "TODAY" && args.DateReference != "YESTERDAY" && args.DateReference != "EXPLICIT" {
		return nil, fmt.Errorf("date_reference must be TODAY, YESTERDAY or EXPLICIT")
	}
	if err := validateDateFields(args.DateReference, args.ExplicitDate, args.LocalTime); err != nil {
		return nil, err
	}
	return remarshal(args), nil
}

func (p *Processor) agentProposeAssetPurchase(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	binding := state.ReviewBinding
	if !assetPurchaseOffered(binding) {
		result.Status = "NO_ACTIVE_REVIEW"
		return result, true, nil
	}
	sourceHint, _ := args["source_account_hint"].(string)
	wealthHint, _ := args["wealth_account_hint"].(string)
	reference, _ := args["date_reference"].(string)
	explicit, _ := args["explicit_date"].(string)
	local, _ := args["local_time"].(string)
	at, err := resolveTime(state.Now, &reference, stringPtr(explicit), stringPtr(local))
	if err != nil {
		return result, true, recoverableDate(err)
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	resolvedWealthID, institution, observationHint, _, valid, err := p.loadBoundWealthObservationTx(ctx, tx, state, binding)
	if err != nil {
		return result, true, err
	}
	if !valid {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	accountID, err := resolveUniqueAccountHint(ctx, tx, state.HouseholdID, sourceHint)
	if err != nil {
		result.Status = "ACCOUNT_AMBIGUOUS"
		return result, true, nil
	}
	wealthID := resolvedWealthID
	if strings.TrimSpace(wealthHint) != "" {
		wealthID, err = resolveUniqueWealthHint(ctx, tx, state.HouseholdID, wealthHint)
	} else if wealthID == "" {
		wealthID, err = resolveUniqueWealthHint(ctx, tx, state.HouseholdID, strings.TrimSpace(institution+" "+observationHint))
	}
	if err != nil || wealthID == "" {
		result.Status = "WEALTH_ACCOUNT_AMBIGUOUS"
		return result, true, nil
	}
	var amount string
	if err = tx.QueryRow(ctx, `SELECT observed_value_idr::text FROM wealth_observation WHERE id=$1 AND household_id=$2 AND status='PENDING'`, binding.TargetID, state.HouseholdID).Scan(&amount); err != nil {
		return result, true, err
	}
	if value, ok := new(big.Int).SetString(amount, 10); !ok || value.Sign() <= 0 {
		result.Status = "INVALID_AMOUNT"
		return result, true, nil
	}
	var compatible bool
	if err = tx.QueryRow(ctx, `SELECT transfer_wealth_compatible('ASSET_PURCHASE',$1::uuid,$2)`, wealthID, state.HouseholdID).Scan(&compatible); err != nil {
		return result, true, err
	}
	if !compatible {
		result.Status = "INCOMPATIBLE_WEALTH_ACCOUNT"
		return result, true, nil
	}
	var accountName, wealthName string
	if err = tx.QueryRow(ctx, `SELECT a.name,w.name FROM account a,wealth_account w WHERE a.id=$1 AND a.household_id=$3 AND w.id=$2 AND w.household_id=$3`, accountID, wealthID, state.HouseholdID).Scan(&accountName, &wealthName); err != nil {
		return result, true, err
	}
	var token string
	err = tx.QueryRow(ctx, `UPDATE review_conversation SET context_json=COALESCE(context_json,'{}'::jsonb)||jsonb_build_object('asset_purchase',jsonb_build_object(
			'token',gen_random_uuid()::text,'telegram_user_id',$2::bigint,'telegram_chat_id',$3::bigint,'account_id',$4::text,'wealth_account_id',$5::text,
			'amount_idr',$6::text,'transaction_at',$7::text,'expires_at',(now()+$8::interval)::text)),updated_at=now()
		WHERE review_request_id=$1 RETURNING context_json->'asset_purchase'->>'token'`,
		binding.ReviewRequestID, state.Update.Message.From.ID, state.Update.Message.Chat.ID, accountID, wealthID, amount, at.UTC().Format(time.RFC3339), assetPurchaseWindow).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}
	result.confirmationToken = token
	result.Status = "PENDING_CONFIRMATION"
	result.Mutation = map[string]any{
		"action": assetPurchaseStagedAction, "amount_idr": amount, "account": accountName, "wealth_account": wealthName,
		"transaction_at": at.In(jakartaLocation()).Format(time.RFC3339),
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-asset-purchase-proposal',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if err = p.queueTurnReplyTx(ctx, tx, state, result); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	return result, false, nil
}

func assetPurchaseStagedMessage(mutation map[string]any) string {
	amount, _ := mutation["amount_idr"].(string)
	account, _ := mutation["account"].(string)
	wealth, _ := mutation["wealth_account"].(string)
	at, _ := mutation["transaction_at"].(string)
	when := at
	if parsed, err := time.Parse(time.RFC3339, at); err == nil {
		when = formatIDDateTime(parsed)
	}
	return fmt.Sprintf("Catat Rp%s sebagai pembelian aset ke %s, dibayar dari %s pada %s? Saldo yang terbaca dari gambar tadi diganti dengan transaksi ini. Belum ada yang tersimpan sampai kamu menekan Ya.", FormatIDR(amount), wealth, account, when)
}

func assetPurchaseMarkup(token string) *InlineKeyboardMarkup {
	if uuid.Validate(token) != nil {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ya, catat pembelian", CallbackData: callbackPendingActionYes + ":" + token}, {Text: "Batal", CallbackData: callbackPendingActionNo + ":" + token}}}}
}

type assetPurchaseProposal struct {
	reviewRequestID, observationID, accountID, wealthID, amount string
	at                                                          time.Time
}

func (p *Processor) processPendingAssetPurchase(ctx context.Context, householdID string, update telegramUpdate, sourceEventID string, confirm bool, token string) (bool, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	var proposal assetPurchaseProposal
	var atText string
	err = tx.QueryRow(ctx, `SELECT c.review_request_id::text,ri.wealth_observation_id::text,c.context_json->'asset_purchase'->>'account_id',c.context_json->'asset_purchase'->>'wealth_account_id',
			c.context_json->'asset_purchase'->>'amount_idr',c.context_json->'asset_purchase'->>'transaction_at'
		FROM review_conversation c
		JOIN review_request r ON r.id=c.review_request_id
		JOIN review_item ri ON ri.id=r.review_item_id
		WHERE r.household_id=$1 AND r.status='OPEN' AND ri.wealth_observation_id IS NOT NULL
		  AND c.context_json->'asset_purchase'->>'token'=$2
		  AND (c.context_json->'asset_purchase'->>'telegram_user_id')::bigint=$3
		  AND (c.context_json->'asset_purchase'->>'telegram_chat_id')::bigint=$4
		  AND (c.context_json->'asset_purchase'->>'expires_at')::timestamptz>now()
		FOR UPDATE OF c`, householdID, token, update.Message.From.ID, update.Message.Chat.ID).
		Scan(&proposal.reviewRequestID, &proposal.observationID, &proposal.accountID, &proposal.wealthID, &proposal.amount, &atText)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	message := "Usulan pembelian aset dibatalkan."
	if confirm {
		if proposal.at, err = time.Parse(time.RFC3339, atText); err != nil {
			return true, err
		}
		status, err := p.applyAssetPurchaseTx(ctx, tx, householdID, update, sourceEventID, proposal)
		if err != nil {
			return true, err
		}
		message = assetPurchaseOutcomeMessage(status, proposal)
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET context_json=context_json-'asset_purchase',updated_at=now() WHERE review_request_id=$1`, proposal.reviewRequestID); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-asset-purchase-confirmation',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return true, err
	}
	if err = enqueueReply(ctx, tx, update, message); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

func assetPurchaseOutcomeMessage(status string, proposal assetPurchaseProposal) string {
	switch status {
	case "RESOLVED":
		return "✅ Pembelian aset Rp" + FormatIDR(proposal.amount) + " sudah tercatat."
	case "RESOLVED_EXISTING":
		return "✅ Pembelian aset Rp" + FormatIDR(proposal.amount) + " tercatat dengan memakai transaksi yang sudah ada pada hari itu, tidak ada transaksi baru."
	case "TRANSFER_RECONCILIATION_REQUIRED":
		return "Ada beberapa transaksi mirip pada hari itu, jadi belum ada yang dicatat. Kartu saldonya tetap menunggu: pilih akun atau Abaikan di kartunya, atau rapikan transaksinya di halaman Transaksi lalu minta lagi."
	case "INCOMPATIBLE_WEALTH_ACCOUNT":
		return "Akun kekayaan itu tidak bisa dipakai untuk pembelian aset, jadi belum ada yang dicatat."
	default:
		return assetPurchaseStaleMessage
	}
}

func (p *Processor) applyAssetPurchaseTx(ctx context.Context, tx pgx.Tx, householdID string, update telegramUpdate, sourceEventID string, proposal assetPurchaseProposal) (string, error) {
	state := &agentState{HouseholdID: householdID, Update: update, SourceEventID: sourceEventID}
	binding := &agentReviewBinding{Kind: wealthObservationBindingKind, TargetID: proposal.observationID, ReviewRequestID: proposal.reviewRequestID}
	_, _, _, originalSource, valid, err := p.loadBoundWealthObservationTx(ctx, tx, state, binding)
	if err != nil {
		return "", err
	}
	if !valid {
		return "STALE_REVIEW_BINDING", nil
	}
	var compatible bool
	if err = tx.QueryRow(ctx, `SELECT transfer_wealth_compatible('ASSET_PURCHASE',$1::uuid,$2)`, proposal.wealthID, householdID).Scan(&compatible); err != nil {
		return "", err
	}
	if !compatible {
		return "INCOMPATIBLE_WEALTH_ACCOUNT", nil
	}
	var accountActive bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account WHERE id=$1 AND household_id=$2 AND active)`, proposal.accountID, householdID).Scan(&accountActive); err != nil {
		return "", err
	}
	if !accountActive {
		return "STALE_REVIEW_BINDING", nil
	}
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, update.Message.From.ID, householdID).Scan(&userID); err != nil {
		return "", err
	}
	transactionID, reused, status, err := p.upsertAssetPurchaseTransferTx(ctx, tx, householdID, userID, proposal)
	if err != nil || status != "" {
		return status, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',1,jsonb_build_object('review_request_id',$3::uuid,'agent_sprint',1)) ON CONFLICT DO NOTHING`, transactionID, sourceEventID, proposal.reviewRequestID); err != nil {
		return "", err
	}
	if originalSource != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TELEGRAM_IMAGE',1,jsonb_build_object('reclassified_from','WEALTH_OBSERVATION','observation_id',$3::uuid)) ON CONFLICT DO NOTHING`, transactionID, originalSource, proposal.observationID); err != nil {
			return "", err
		}
	}
	err = reviewdomain.DismissWealthObservation(ctx, tx, reviewdomain.WealthObservationCommand{HouseholdID: householdID, ObservationID: proposal.observationID})
	if errors.Is(err, reviewdomain.ErrWealthObservationNotFound) {
		return "STALE_REVIEW_BINDING", nil
	}
	if err != nil {
		return "", err
	}
	if err = reviewdomain.ReclassifyWealthEvidence(ctx, tx, householdID, proposal.observationID); err != nil {
		return "", err
	}
	if err = resolveAgentBoundReviewRequestTx(ctx, tx, binding, userID, "RECLASSIFIED_ASSET_PURCHASE"); err != nil {
		return "", err
	}
	if originalSource != "" {
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-asset-purchase-confirmation',parser_version='1' WHERE id=$1 AND household_id=$2`, originalSource, householdID); err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'RECLASSIFY_WEALTH_OBSERVATION','wealth_observation',$3,jsonb_build_object('transaction_id',$4::uuid,'purpose','ASSET_PURCHASE','agent_sprint',1))`, householdID, userID, proposal.observationID, transactionID); err != nil {
		return "", err
	}
	if err = reviewdomain.RefreshOpenCycleResiduals(ctx, tx, householdID, proposal.at, userID); err != nil {
		return "", err
	}
	if reused {
		return "RESOLVED_EXISTING", nil
	}
	return "RESOLVED", nil
}

func (p *Processor) upsertAssetPurchaseTransferTx(ctx context.Context, tx pgx.Tx, householdID, userID string, proposal assetPurchaseProposal) (string, bool, string, error) {
	localAt := proposal.at.In(jakartaLocation())
	dayStart := time.Date(localAt.Year(), localAt.Month(), localAt.Day(), 0, 0, 0, 0, jakartaLocation()).UTC()
	type candidate struct {
		id, kind, status, purpose, existingWealth string
		at                                        time.Time
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text,type,status,COALESCE(purpose,''),COALESCE(related_wealth_account_id::text,''),transaction_at
		FROM transaction
		WHERE household_id=$1 AND account_id=$2 AND type IN ('TRANSFER','UNCLASSIFIED')
		  AND status<>'VOIDED' AND amount=$3 AND transaction_at >= $4 AND transaction_at < $5
		ORDER BY abs(extract(epoch FROM transaction_at-$6::timestamptz)),id
		LIMIT 2`, householdID, proposal.accountID, proposal.amount, dayStart, dayStart.AddDate(0, 0, 1), proposal.at.UTC())
	if err != nil {
		return "", false, "", err
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.kind, &c.status, &c.purpose, &c.existingWealth, &c.at); err != nil {
			rows.Close()
			return "", false, "", err
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return "", false, "", err
	}
	if len(candidates) > 1 {
		return "", false, "TRANSFER_RECONCILIATION_REQUIRED", nil
	}
	if len(candidates) == 0 {
		var transactionID string
		err = tx.QueryRow(ctx, `
			INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,description,created_by_user_id,purpose,related_wealth_account_id,confirmed_at)
			VALUES($1,$2,'TRANSFER','CONFIRMED',$3,'IDR',$4,'Pembelian investasi dari observasi kekayaan',$5,'ASSET_PURCHASE',$6::uuid,now())
			RETURNING id::text`, householdID, proposal.accountID, proposal.amount, proposal.at, userID, proposal.wealthID).Scan(&transactionID)
		return transactionID, false, "", err
	}
	found := candidates[0]
	if found.at.Sub(proposal.at).Abs() >= time.Minute {
		return "", false, "TRANSFER_RECONCILIATION_REQUIRED", nil
	}
	switch {
	case found.kind == "TRANSFER" && found.status == "CONFIRMED":
		if found.purpose != "ASSET_PURCHASE" || found.existingWealth != proposal.wealthID {
			return "", false, "TRANSFER_RECONCILIATION_REQUIRED", nil
		}
	case found.kind == "UNCLASSIFIED" || found.status == "NEEDS_REVIEW":
		updated, err := tx.Exec(ctx, `
			UPDATE transaction
			SET type='TRANSFER',status='CONFIRMED',category_id=NULL,purpose='ASSET_PURCHASE',
			    related_wealth_account_id=$2::uuid,
			    description=COALESCE(NULLIF(description,''),'Pembelian investasi dari observasi kekayaan'),
			    confirmed_at=COALESCE(confirmed_at,now()),voided_at=NULL,updated_at=now()
			WHERE id=$1 AND household_id=$3 AND status<>'VOIDED'`, found.id, proposal.wealthID, householdID)
		if err != nil {
			return "", false, "", err
		}
		if updated.RowsAffected() != 1 {
			return "", false, "TRANSFER_RECONCILIATION_REQUIRED", nil
		}
		if _, err = tx.Exec(ctx, `UPDATE transaction_proposal SET proposed_type='TRANSFER',proposal_status='ACCEPTED',metadata_json=metadata_json||jsonb_build_object('purpose','ASSET_PURCHASE','agent_sprint',1),updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')`, found.id); err != nil {
			return "", false, "", err
		}
	default:
		return "", false, "TRANSFER_RECONCILIATION_REQUIRED", nil
	}
	return found.id, true, "", nil
}
