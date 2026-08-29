package bankemail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
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
	SourceEventID string  `json:"source_event_id"`
	Shadow        bool    `json:"shadow"`
	ReviewID      string  `json:"review_id,omitempty"`
	AmountIDR     *string `json:"amount_idr,omitempty"`
	TransactionAt *string `json:"transaction_at,omitempty"`
}

// Complete resumes a source-bound review with only the facts a person entered.
// The resulting extraction still flows through the ordinary Go policy/persist
// path; it never fabricates a ledger row merely to close a review.
func (p *Processor) Complete(ctx context.Context, payload Payload) error {
	if payload.ReviewID == "" {
		return fmt.Errorf("bank review id is required")
	}
	var household, listenerID, bank, sender, accountID string
	var raw []byte
	if err := p.pool.QueryRow(ctx, `SELECT s.household_id,l.id,l.bank_name,l.sender_address,COALESCE(l.account_id::text,''),e.output_json FROM review_item ri JOIN source_event s ON s.id=ri.source_event_id JOIN bank_email_extraction e ON e.source_event_id=s.id JOIN bank_email_listener l ON l.id=e.listener_id WHERE ri.id=$1 AND ri.source_event_id=$2 AND ri.status IN ('OPEN','PENDING_SEND') FOR UPDATE`, payload.ReviewID, payload.SourceEventID).Scan(&household, &listenerID, &bank, &sender, &accountID, &raw); err != nil {
		return err
	}
	var extraction Extraction
	if err := json.Unmarshal(raw, &extraction); err != nil {
		return fmt.Errorf("load reviewed extraction: %w", err)
	}
	if extraction.AmountIDR == nil && payload.AmountIDR != nil {
		call := gateway.ToolCall{Name: "emit_bank_transaction", Arguments: []byte(`{"kind":"TRANSACTION","direction":"UNKNOWN","channel":"UNKNOWN","amount_idr":` + strconv.Quote(*payload.AmountIDR) + `,"transaction_at":null,"merchant":null,"counterparty":null,"reference":null,"description":null,"missing_fields":["transaction_at"],"confidence":0.8}`)}
		checked, err := ValidateEmitBankTransaction(call)
		if err != nil {
			return err
		}
		extraction.AmountIDR = checked.AmountIDR
	}
	if extraction.TransactionAt == nil && payload.TransactionAt != nil {
		at, err := parseStrictBankRFC3339(*payload.TransactionAt)
		if err != nil {
			return err
		}
		extraction.TransactionAt = &at
	}
	if extraction.AmountIDR == nil || extraction.TransactionAt == nil {
		return fmt.Errorf("bank amount and RFC3339 timestamp are required")
	}
	extraction.MissingFields = removeMissing(extraction.MissingFields, "amount_idr", "transaction_at")
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: bank, SenderAddress: sender, AccountID: accountID, TrackingPolicy: "SPENDING_ONLY", Active: true}
	var already bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM transaction_proposal WHERE source_event_id=$1)`, payload.SourceEventID).Scan(&already); err != nil {
		return err
	}
	if already {
		_, err := p.pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='COMPLETE_BANK_FACTS',updated_at=now() WHERE id=$1 AND status IN ('OPEN','PENDING_SEND')`, payload.ReviewID)
		return err
	}
	known, err := p.loadKnownAccounts(ctx, household)
	if err != nil {
		return err
	}
	memory, err := loadMerchantMemory(ctx, p.pool, household, value(extraction.Merchant))
	if err != nil {
		return err
	}
	result := EvaluateBankEmail(listener, extraction, known, memory)
	if err := p.persist(ctx, listener, payload.SourceEventID, extraction, result); err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `UPDATE bank_email_extraction SET output_json=$2::jsonb,validation_status='VALID',policy_result=$3 WHERE source_event_id=$1; UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='COMPLETE_BANK_FACTS',resolution_values=jsonb_build_object('amount_idr',$4::text,'transaction_at',$5::timestamptz),updated_at=now() WHERE id=$6 AND status IN ('OPEN','PENDING_SEND')`, payload.SourceEventID, mustJSON(extraction), result.Status, value(extraction.AmountIDR), extraction.TransactionAt, payload.ReviewID)
	return err
}
func removeMissing(values []string, names ...string) []string {
	blocked := map[string]bool{}
	for _, name := range names {
		blocked[name] = true
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if !blocked[v] {
			out = append(out, v)
		}
	}
	return out
}
func mustJSON(v any) string { raw, _ := json.Marshal(v); return string(raw) }
func (p *Processor) loadKnownAccounts(ctx context.Context, household string) ([]KnownAccount, error) {
	rows, err := p.pool.Query(ctx, `SELECT match_hint,relationship FROM known_account WHERE household_id=$1 AND active`, household)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KnownAccount{}
	for rows.Next() {
		var v KnownAccount
		if err := rows.Scan(&v.MatchHint, &v.Relationship); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
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
		var schemaErr SchemaError
		if errors.As(err, &schemaErr) {
			_, persistErr := p.pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status,policy_result) VALUES($1,$2,'native_tool',$3,$4,'{}','INVALID','NEEDS_REVIEW') ON CONFLICT(source_event_id) DO UPDATE SET gateway_model=excluded.gateway_model,validation_status='INVALID',policy_result='NEEDS_REVIEW'; UPDATE source_event SET processing_status='NEEDS_REVIEW',parser_name='bank-email-generic',parser_version=$4 WHERE id=$1`, payload.SourceEventID, listenerID, meta.Model, ToolSchemaVersion)
			return persistErr
		}
		_, _ = p.pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status,policy_result) VALUES($1,$2,'native_tool',$3,$4,'{}','TRANSPORT_FAILED','RETRY') ON CONFLICT(source_event_id) DO UPDATE SET gateway_model=excluded.gateway_model,validation_status='TRANSPORT_FAILED',policy_result='RETRY'; UPDATE source_event SET processing_status='FAILED',parser_name='bank-email-generic',parser_version=$4 WHERE id=$1`, payload.SourceEventID, listenerID, meta.Model, ToolSchemaVersion)
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
	memory, err := loadMerchantMemory(ctx, p.pool, household, value(extraction.Merchant))
	if err != nil {
		return err
	}
	result := EvaluateBankEmail(listener, extraction, knownAccounts, memory)
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
		_, err = p.pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status,policy_result) VALUES($1,$2,'native_tool',$3,$4,$5::jsonb,'VALID','IGNORED') ON CONFLICT(source_event_id) DO UPDATE SET output_json=excluded.output_json,gateway_model=excluded.gateway_model,validation_status='VALID',policy_result='IGNORED'; UPDATE source_event SET processing_status='IGNORED',parser_name='bank-email-generic',parser_version=$4 WHERE id=$1`, payload.SourceEventID, listenerID, meta.Model, ToolSchemaVersion, string(output))
		return err
	}
	if _, err = p.pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status,policy_result) VALUES($1,$2,'native_tool',$3,$4,$5::jsonb,'VALID',$6) ON CONFLICT(source_event_id) DO UPDATE SET output_json=excluded.output_json,gateway_model=excluded.gateway_model,validation_status=excluded.validation_status,policy_result=excluded.policy_result`, payload.SourceEventID, listenerID, meta.Model, ToolSchemaVersion, string(output), status); err != nil {
		return err
	}
	if missing(extraction, "amount_idr") || missing(extraction, "transaction_at") || extraction.Confidence < 0.80 {
		_, err = p.pool.Exec(ctx, `UPDATE source_event SET processing_status='NEEDS_REVIEW',parser_name='bank-email-generic',parser_version=$2 WHERE id=$1; INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($3,$1,'UNKNOWN_BANK_TEMPLATE','OPEN') ON CONFLICT DO NOTHING`, payload.SourceEventID, ToolSchemaVersion, household)
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
	categoryID := result.CategoryID
	merchantID, err := resolveMerchantID(ctx, tx, listener.HouseholdID, value(extraction.Merchant))
	if err != nil {
		return err
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
	err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,merchant_id,category_id,description,counterparty_name,external_reference,source_confidence,classification_confidence,confirmed_at) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,'IDR',$6,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),$12,$13,CASE WHEN $4='CONFIRMED' THEN now() END) RETURNING id`, listener.HouseholdID, listener.AccountID, transactionType, transactionStatus, amount, *at, merchantID, categoryID, description, counterparty, reference, extraction.Confidence, extraction.Confidence).Scan(&transactionID)
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
			message := "🏦 Transaksi bank perlu ditinjau\n\nNominal: Rp" + workerTelegram.FormatIDR(amount) + "\nKeterangan: " + description + "\n\nPilih kategori atau lengkapi detail transaksi."
			if err = workerTelegram.EnqueueReviewRequest(ctx, tx, transactionID, result.ReviewType, chatID, 0, message); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'WORKER','CREATE_FROM_BANK_EMAIL','transaction',$2,jsonb_build_object('source_event_id',$3::uuid,'listener_id',$4::uuid,'proposal_id',$5::uuid,'policy_result',$6::text,'auto_confirm',$7::boolean,'tool_schema_version',$8::text))`, listener.HouseholdID, transactionID, sourceID, listener.ID, proposalID, result.Status, result.AutoConfirm, ToolSchemaVersion); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadMerchantMemory(ctx context.Context, q rowQuerier, household, raw string) (MerchantMemory, error) {
	if strings.TrimSpace(raw) == "" {
		return MerchantMemory{}, nil
	}
	var m MerchantMemory
	err := q.QueryRow(ctx, `SELECT ma.normalized_merchant_id::text,ma.default_category_id::text,ma.auto_apply FROM merchant_alias ma JOIN category c ON c.id=ma.default_category_id WHERE ma.household_id=$1 AND lower(ma.raw_name)=lower($2) AND ma.auto_apply AND ma.created_from_user_confirmation AND ma.default_category_id IS NOT NULL AND c.household_id=$1 AND c.active LIMIT 1`, household, raw).Scan(&m.MerchantID, &m.CategoryID, &m.AutoApply)
	if errors.Is(err, pgx.ErrNoRows) {
		return MerchantMemory{}, nil
	}
	return m, err
}
func resolveMerchantID(ctx context.Context, tx pgx.Tx, household, raw string) (string, error) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", nil
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT normalized_merchant_id::text FROM merchant_alias WHERE household_id=$1 AND lower(raw_name)=lower($2) LIMIT 1`, household, raw).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	normalized := strings.ToUpper(raw)
	if len([]rune(normalized)) > 160 {
		normalized = string([]rune(normalized)[:160])
	}
	err = tx.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) ON CONFLICT(household_id,normalized_name) DO UPDATE SET updated_at=merchant.updated_at RETURNING id`, household, normalized).Scan(&id)
	return id, err
}
