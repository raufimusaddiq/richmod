package bankemail

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
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
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: bank, SenderAddress: sender, TrackingPolicy: "SPENDING_ONLY", Active: true}
	extraction, meta, err := p.extractor.Extract(ctx, payload.SourceEventID, listener, TrustedEmail{MessageID: messageID, Subject: subject, Date: date, AuthenticationResults: auth, Body: body})
	if err != nil {
		_, _ = p.pool.Exec(ctx, `UPDATE source_event SET processing_status='FAILED',parser_name='bank-email-generic',parser_version=$2 WHERE id=$1`, payload.SourceEventID, ToolSchemaVersion)
		return err
	}
	output, _ := json.Marshal(extraction)
	result := EvaluateBankEmail(listener, extraction, nil)
	status := result.Status
	if status == "" {
		status = "NEEDS_REVIEW"
	}
	_, err = p.pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status,policy_result) VALUES($1,$2,'native_tool',$3,$4,$5::jsonb,'VALID',$6) ON CONFLICT(source_event_id) DO UPDATE SET output_json=excluded.output_json,gateway_model=excluded.gateway_model,validation_status=excluded.validation_status,policy_result=excluded.policy_result; UPDATE source_event SET processing_status=$6,parser_name='bank-email-generic',parser_version=$4 WHERE id=$1`, payload.SourceEventID, listenerID, meta.Model, ToolSchemaVersion, string(output), status)
	return err
}
