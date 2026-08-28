package bankemail

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	workerTelegram "github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

type Processor struct {
	pool      *pgxpool.Pool
	extractor *Extractor
}

func NewProcessor(pool *pgxpool.Pool, extractor *Extractor) *Processor {
	return &Processor{pool: pool, extractor: extractor}
}

type Payload struct {
	SourceEventID string `json:"source_event_id"`
	Shadow        bool   `json:"shadow"`
}

func DecodePayload(raw json.RawMessage) (Payload, error) {
	var p Payload
	if json.Unmarshal(raw, &p) != nil || strings.TrimSpace(p.SourceEventID) == "" {
		return p, fmt.Errorf("invalid bank email payload")
	}
	return p, nil
}

// Process persists extraction evidence before any future proposal/ledger step.
// Policy and mutation remain Go-owned; a failed extraction leaves the source
// event recoverable and never creates a guessed financial record.
func (p *Processor) Process(ctx context.Context, payload Payload) error {
	var household, listenerID, bank, sender, subject, date, auth, body, messageID string
	err := p.pool.QueryRow(ctx, `SELECT s.household_id,l.id,l.bank_name,l.sender_address,m.subject,m.email_date,m.authentication_results,m.body,m.gmail_message_id FROM source_event s JOIN bank_email_event m ON m.source_event_id=s.id JOIN bank_email_listener l ON l.id=m.listener_id WHERE s.id=$1 AND l.active`, payload.SourceEventID).Scan(&household, &listenerID, &bank, &sender, &subject, &date, &auth, &body, &messageID)
	if err != nil {
		return fmt.Errorf("load bank email event: %w", err)
	}
	var accountID string
	_ = p.pool.QueryRow(ctx, `SELECT COALESCE(account_id::text,'') FROM bank_email_listener WHERE id=$1`, listenerID).Scan(&accountID)
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: bank, SenderAddress: sender, AccountID: accountID, TrackingPolicy: "SPENDING_ONLY", Active: true}
	extraction, meta, err := p.extractor.Extract(ctx, payload.SourceEventID, listener, TrustedEmail{MessageID: messageID, Subject: subject, Date: date, AuthenticationResults: auth, Body: body})
	if err != nil {
		if payload.Shadow {
			return err
		}
		_, _ = p.pool.Exec(ctx, `UPDATE source_event SET processing_status='FAILED',parser_name='bank-email-generic',parser_version=$2 WHERE id=$1`, payload.SourceEventID, ToolSchemaVersion)
		return err
	}
	output, _ := json.Marshal(extraction)
	knownAccounts := []KnownAccount{}
	rows, queryErr := p.pool.Query(ctx, `SELECT match_hint,relationship FROM known_account WHERE household_id=$1 AND active`, household)
	if queryErr == nil {
		for rows.Next() {
			var item KnownAccount
			if rows.Scan(&item.MatchHint, &item.Relationship) == nil {
				knownAccounts = append(knownAccounts, item)
			}
		}
		rows.Close()
	}
	result := EvaluateBankEmail(listener, extraction, knownAccounts)
	status := result.Status
	if status == "" {
		status = "NEEDS_REVIEW"
	}
	if payload.Shadow {
		baseline, baselineErr := p.shadowBaseline(ctx, payload.SourceEventID)
		shadowOutput := "null"
		agreement := "NO_BASELINE"
		if baselineErr == nil {
			fields, equal := CompareShadow(extraction, result, baseline)
			encoded, _ := json.Marshal(map[string]any{"baseline": baseline, "fields": fields})
			shadowOutput = string(encoded)
			if equal {
				agreement = "AGREE"
			} else {
				agreement = "DISAGREE"
			}
		}
		_, err = p.pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status,policy_result,shadow_output_json,shadow_agreement) VALUES($1,$2,'native_tool',$3,$4,$5::jsonb,'VALID',$6,$7::jsonb,$8) ON CONFLICT(source_event_id) DO UPDATE SET output_json=excluded.output_json,gateway_model=excluded.gateway_model,validation_status=excluded.validation_status,policy_result=excluded.policy_result,shadow_output_json=excluded.shadow_output_json,shadow_agreement=excluded.shadow_agreement`, payload.SourceEventID, listenerID, meta.Model, ToolSchemaVersion, string(output), status, shadowOutput, agreement)
		return err
	}
	if status == "IGNORED" {
		_, err = p.pool.Exec(ctx, `UPDATE source_event SET processing_status='IGNORED',parser_name='bank-email-generic',parser_version=$2 WHERE id=$1`, payload.SourceEventID, ToolSchemaVersion)
		return err
	}
	if _, err = p.pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status,policy_result) VALUES($1,$2,'native_tool',$3,$4,$5::jsonb,'VALID',$6) ON CONFLICT(source_event_id) DO UPDATE SET output_json=excluded.output_json,gateway_model=excluded.gateway_model,validation_status=excluded.validation_status,policy_result=excluded.policy_result`, payload.SourceEventID, listenerID, meta.Model, ToolSchemaVersion, string(output), status); err != nil {
		return err
	}
	if missing(extraction, "amount_idr") || missing(extraction, "transaction_at") {
		_, err = p.pool.Exec(ctx, `UPDATE source_event SET processing_status='NEEDS_REVIEW',parser_name='bank-email-generic',parser_version=$2 WHERE id=$1`, payload.SourceEventID, ToolSchemaVersion)
		return err
	}
	var alreadyPersisted bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM transaction_proposal WHERE source_event_id=$1)`, payload.SourceEventID).Scan(&alreadyPersisted); err == nil && alreadyPersisted {
		return nil
	}
	return p.persist(ctx, listener, payload.SourceEventID, extraction, result)
}

func (p *Processor) shadowBaseline(ctx context.Context, sourceEventID string) (ShadowBaseline, error) {
	var baseline ShadowBaseline
	var channel string
	var status, transactionType string
	if err := p.pool.QueryRow(ctx, `SELECT t.type,t.status,regexp_replace(t.amount::text,'[.]0+$',''),t.transaction_at,COALESCE(m.normalized_name,''),COALESCE(te.metadata_json->>'channel','') FROM transaction t JOIN transaction_evidence te ON te.transaction_id=t.id LEFT JOIN merchant m ON m.id=t.merchant_id WHERE te.source_event_id=$1 ORDER BY t.created_at LIMIT 1`, sourceEventID).Scan(&transactionType, &status, &baseline.Amount, &baseline.TransactionAt, &baseline.Merchant, &channel); err != nil {
		return ShadowBaseline{}, err
	}
	baseline.Direction = "OUTGOING"
	if transactionType == "INCOME" {
		baseline.Direction = "INCOMING"
	}
	switch strings.ToUpper(channel) {
	case "MERCHANT":
		baseline.Channel = "MERCHANT_PAYMENT"
	default:
		baseline.Channel = strings.ToUpper(channel)
	}
	switch {
	case status == "VOIDED":
		baseline.Policy = "IGNORE"
	case transactionType == "TRANSFER":
		baseline.Policy = "TRANSFER"
	case transactionType == "EXPENSE":
		baseline.Policy = "EXPENSE"
	default:
		baseline.Policy = "NEEDS_REVIEW"
	}
	return baseline, nil
}

func (p *Processor) persist(ctx context.Context, listener Listener, sourceID string, extraction Extraction, result PolicyResult) error {
	amount, at := value(extraction.AmountIDR), extraction.TransactionAt
	if amount == "" || at == nil {
		return fmt.Errorf("validated bank extraction lacks ledger facts")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	categoryID := ""
	if extraction.Merchant != nil {
		_ = tx.QueryRow(ctx, `SELECT default_category_id::text FROM merchant_alias WHERE household_id=$1 AND lower(raw_name)=lower($2) AND auto_apply AND default_category_id IS NOT NULL LIMIT 1`, listener.HouseholdID, *extraction.Merchant).Scan(&categoryID)
	}
	transactionType := result.Type
	if transactionType == "NEEDS_REVIEW" {
		transactionType = "UNCLASSIFIED"
	}
	if transactionType == "" {
		transactionType = "UNCLASSIFIED"
	}
	transactionStatus := result.Status
	if transactionStatus == "" {
		transactionStatus = "NEEDS_REVIEW"
	}
	proposalStatus := "NEEDS_REVIEW"
	if result.AutoConfirm {
		proposalStatus = "ACCEPTED"
	}
	description := value(extraction.Description)
	if description == "" {
		description = result.Description
	}
	merchant := value(extraction.Merchant)
	counterparty := value(extraction.Counterparty)
	reference := value(extraction.Reference)
	var proposalID string
	err = tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,merchant_raw,counterparty_raw,category_candidate_id,description,confidence,proposal_status,metadata_json) VALUES($1,$2,$3,$4,'IDR',$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,'')::uuid,$9,$10,$11,$12::jsonb) RETURNING id`, listener.HouseholdID, sourceID, transactionType, amount, *at, merchant, counterparty, categoryID, description, extraction.Confidence, proposalStatus, `{"bank_email_policy":"v4"}`).Scan(&proposalID)
	if err != nil {
		return err
	}
	var transactionID string
	err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,category_id,description,counterparty_name,external_reference,source_confidence,classification_confidence,confirmed_at) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,'IDR',$6,NULLIF($7,'')::uuid,NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),$11,$12,CASE WHEN $4='CONFIRMED' THEN now() END) RETURNING id`, listener.HouseholdID, listener.AccountID, transactionType, transactionStatus, amount, *at, categoryID, description, counterparty, reference, extraction.Confidence, extraction.Confidence).Scan(&transactionID)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'BANK_EMAIL',$3,jsonb_build_object('proposal_id',$4::uuid,'listener_id',$5::uuid))`, transactionID, sourceID, extraction.Confidence, proposalID, listener.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status=$2,parser_name='bank-email-generic',parser_version=$3 WHERE id=$1`, sourceID, func() string {
		if transactionStatus == "CONFIRMED" {
			return "PROCESSED"
		}
		return "NEEDS_REVIEW"
	}(), ToolSchemaVersion); err != nil {
		return err
	}
	if transactionStatus == "NEEDS_REVIEW" {
		var chatID int64
		if e := tx.QueryRow(ctx, `SELECT telegram_user_id FROM telegram_identity WHERE household_id=$1 AND active ORDER BY created_at LIMIT 1`, listener.HouseholdID).Scan(&chatID); e == nil {
			if err = workerTelegram.EnqueueReviewRequest(ctx, tx, transactionID, result.ReviewType, chatID, 0, "Ada transaksi bank yang perlu ditinjau: "+description); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}
