package bankemail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
	workerTelegram "github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

type Processor struct {
	pool      *pgxpool.Pool
	extractor *Extractor
	// verifier scores the already-extracted facts against the original email
	// through the bounded judgment plane. It is optional: a nil verifier keeps the
	// deterministic structural gate and never silently invents semantic approval
	// (PRD §20).
	verifier jeverifier
}

func NewProcessor(pool *pgxpool.Pool, extractor *Extractor) *Processor {
	return &Processor{pool: pool, extractor: extractor}
}

// SetVerifier wires the bounded evidence verifier. Production sets it from the
// same configured judgment plane the Telegram decision plane uses.
func (p *Processor) SetVerifier(verifier jeverifier) { p.verifier = verifier }

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
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE bank_email_extraction SET output_json=$2::jsonb,validation_status='VALID',policy_result=$3 WHERE source_event_id=$1`, payload.SourceEventID, mustJSON(extraction), result.Status); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='COMPLETE_BANK_FACTS',resolution_values=jsonb_build_object('amount_idr',$2::text,'transaction_at',$3::timestamptz),updated_at=now() WHERE id=$1 AND status IN ('OPEN','PENDING_SEND')`, payload.ReviewID, value(extraction.AmountIDR), extraction.TransactionAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	var receivedAt time.Time
	err := p.pool.QueryRow(ctx, `SELECT s.household_id,s.received_at,l.id,l.bank_name,l.sender_address,m.subject,m.email_date,m.authentication_results,m.body,m.message_id FROM source_event s JOIN bank_email_event m ON m.source_event_id=s.id JOIN bank_email_listener l ON l.id=m.listener_id WHERE s.id=$1 AND l.active`, payload.SourceEventID).Scan(&household, &receivedAt, &listenerID, &bank, &sender, &subject, &date, &auth, &body, &messageID)
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
			return p.persistExtractionFailure(ctx, payload.SourceEventID, listenerID, meta.Model, "INVALID", "NEEDS_REVIEW")
		}
		_ = p.persistExtractionFailure(ctx, payload.SourceEventID, listenerID, meta.Model, "TRANSPORT_FAILED", "RETRY")
		return err
	}
	applyEmailReceivedTimeFallback(&extraction, receivedAt)
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
	if !payload.Shadow {
		result = p.applyCategoryDecision(ctx, payload.SourceEventID, household, extraction, result)
	}
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
		tx, beginErr := p.pool.Begin(ctx)
		if beginErr != nil {
			return beginErr
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status,policy_result) VALUES($1,$2,'native_tool',$3,$4,$5::jsonb,'VALID','IGNORED') ON CONFLICT(source_event_id) DO UPDATE SET output_json=excluded.output_json,gateway_model=excluded.gateway_model,validation_status='VALID',policy_result='IGNORED'`, payload.SourceEventID, listenerID, meta.Model, ToolSchemaVersion, string(output)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='IGNORED',parser_name='bank-email-generic',parser_version=$2 WHERE id=$1`, payload.SourceEventID, ToolSchemaVersion); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = p.pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status,policy_result) VALUES($1,$2,'native_tool',$3,$4,$5::jsonb,'VALID',$6) ON CONFLICT(source_event_id) DO UPDATE SET output_json=excluded.output_json,gateway_model=excluded.gateway_model,validation_status=excluded.validation_status,policy_result=excluded.policy_result`, payload.SourceEventID, listenerID, meta.Model, ToolSchemaVersion, string(output), status); err != nil {
		return err
	}
	// Structural completeness is a deterministic Go check and always applies. The
	// semantic gate then asks the bounded plane whether the email actually supports
	// the extracted facts; the extractor's self-reported confidence is no longer
	// allowed to authorize (or to hide) a semantic claim (ADR-038, PRD §20).
	if missing(extraction, "amount_idr") || missing(extraction, "transaction_at") {
		// List every absent required fact, not just the first: both amount and time
		// can be missing, and the review must request exactly what is unresolved.
		missingFacts := []string{}
		if missing(extraction, "amount_idr") {
			missingFacts = append(missingFacts, "amount")
		}
		if missing(extraction, "transaction_at") {
			missingFacts = append(missingFacts, "transaction_at")
		}
		return p.reviewIncompleteExtraction(ctx, household, payload.SourceEventID, ToolSchemaVersion, "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", partialDecision(household, payload.SourceEventID, extraction, "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", missingFacts, "a required canonical fact was absent from the email extraction"))
	}
	verification, verified, verifyErr := p.verifyEvidence(ctx, payload.SourceEventID, extraction, TrustedEmail{MessageID: messageID, Subject: subject, Date: date, AuthenticationResults: auth, Body: body})
	if verifyErr != nil {
		// Provider failure is infrastructure state, not semantic uncertainty: keep
		// the source event recoverable instead of confirming on extractor confidence.
		_ = p.persistExtractionFailure(ctx, payload.SourceEventID, listenerID, meta.Model, "VERIFICATION_FAILED", "RETRY")
		return fmt.Errorf("bank email evidence verification unavailable: %w", verifyErr)
	}
	if verified && !verification.supported() {
		return p.reviewIncompleteExtraction(ctx, household, payload.SourceEventID, ToolSchemaVersion, "UNKNOWN_BANK_TEMPLATE", partialDecision(household, payload.SourceEventID, extraction, "UNKNOWN_BANK_TEMPLATE", []string{"transaction_semantics"}, "bounded verification could not confirm the email supports the extracted transaction facts"))
	}
	if !verified && extraction.Confidence < 0.80 {
		return p.reviewIncompleteExtraction(ctx, household, payload.SourceEventID, ToolSchemaVersion, "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", partialDecision(household, payload.SourceEventID, extraction, "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", []string{"transaction_semantics"}, "extraction confidence was below the confirmation threshold and semantic verification was unavailable"))
	}
	if verified {
		if persistErr := p.persistEvidenceVerification(ctx, payload.SourceEventID, listenerID, meta.Model, verification); persistErr != nil {
			return persistErr
		}
	}
	var alreadyPersisted bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM transaction_proposal WHERE source_event_id=$1)`, payload.SourceEventID).Scan(&alreadyPersisted); err == nil && alreadyPersisted {
		return nil
	}
	return p.persist(ctx, listener, payload.SourceEventID, extraction, result)
}

// reviewIncompleteExtraction parks a notification whose facts are structurally
// incomplete or semantically unsupported. It never mutates the ledger. The
// decision records why the review exists so the Inbox can request only the
// unresolved fact (PRD §7).
func (p *Processor) reviewIncompleteExtraction(ctx context.Context, household, sourceEventID, schemaVersion, reviewType string, decision reviewdec.Decision) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='NEEDS_REVIEW',parser_name='bank-email-generic',parser_version=$2 WHERE id=$1`, sourceEventID, schemaVersion); err != nil {
		return err
	}
	encoded, encodeErr := decision.JSON()
	if encodeErr != nil {
		return encodeErr
	}
	if _, err = tx.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,$3,'OPEN',$4::jsonb) ON CONFLICT DO NOTHING`, household, sourceEventID, reviewType, string(encoded)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// partialDecision builds the ReviewDecision for a bank email that could not be
// auto-confirmed. Known facts are the extraction fields Go actually validated;
// proposed facts are the policy's own best reading; missing facts are what the
// user must supply. Canonical IDs are never included.
func partialDecision(household, sourceEventID string, extraction Extraction, reviewType string, missingFacts []string, whyNotAuto string) reviewdec.Decision {
	known := map[string]any{}
	if amount := value(extraction.AmountIDR); amount != "" {
		known["amount_idr"] = amount
	}
	if extraction.Direction != nil {
		known["direction"] = *extraction.Direction
	}
	if extraction.Channel != nil {
		known["channel"] = *extraction.Channel
	}
	if merchant := value(extraction.Merchant); merchant != "" {
		known["merchant"] = merchant
	}
	proposed := map[string]any{}
	if extraction.Kind != "" {
		proposed["kind"] = extraction.Kind
	}
	return reviewdec.Decision{
		Version:         reviewdec.Version,
		Subject:         reviewdec.Subject{Type: "source_event", ID: sourceEventID},
		SourceEventID:   sourceEventID,
		ReasonCode:      reviewType,
		DecisionClass:   reviewdec.ClassEvidenceGap,
		KnownFacts:      known,
		ProposedFacts:   proposed,
		MissingFacts:    missingFacts,
		EvidenceRefs:    []reviewdec.EvidenceRef{{Kind: "source_event", ID: sourceEventID}},
		DecisionSource:  reviewdec.SourceGenerativePlusJev,
		PolicyVersion:   ToolSchemaVersion,
		Provenance:      map[string]any{"pipeline": "bank-email-generic"},
		WhyNotAuto:      whyNotAuto,
		AllowedActions:  []string{"COMPLETE_BANK_FACTS", "IGNORE"},
		InteractionMode: reviewdec.ModeSingleField,
	}
}

// persistEvidenceVerification records the bounded ruling next to the extraction
// so an operator can see what the decision plane actually claimed, without
// storing the email body again (PRD §15/§20).
func (p *Processor) persistEvidenceVerification(ctx context.Context, sourceEventID, listenerID, model string, verification EvidenceVerification) error {
	summary, err := json.Marshal(map[string]any{
		"transaction_observed": verification.TransactionObserved,
		"amount_supported":     verification.AmountSupported,
		"direction_supported":  verification.DirectionSupported,
		"channel_supported":    verification.ChannelSupported,
		"material_ambiguity":   verification.MaterialAmbiguity,
		// Without this an operator reading the row cannot tell "ruled not ambiguous"
		// from "the plane could not tell", which is the distinction that decides
		// whether the event may auto-confirm.
		"ambiguity_decided_not_ambiguous": verification.AmbiguityDecidedNotAmbiguous,
		"supported":                       verification.supported(),
		"policy_version":                  verification.PolicyVersion,
	})
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `INSERT INTO bank_email_evidence_verification(source_event_id,listener_id,bank_email_verification_policy_version,gateway_model,answer_summary_json) VALUES($1,$2,$3,NULLIF($4,''),$5::jsonb) ON CONFLICT(source_event_id) DO UPDATE SET gateway_model=excluded.gateway_model,answer_summary_json=excluded.answer_summary_json,bank_email_verification_policy_version=excluded.bank_email_verification_policy_version`, sourceEventID, listenerID, verification.PolicyVersion, verification.Model, string(summary))
	return err
}

// applyCategoryDecision lets a new merchant confirm without a review when the
// bounded plane decides its category (PRD 9.3). The bounded question is answered
// by the same plane that rules on the rest of the event, Go resolves the
// canonical ID, and an undecided answer or provider failure leaves the policy
// result untouched so the category-only review still applies. It is a pure
// function of the resolver so the confirm-no-review half is testable without a
// full email fixture.
func (p *Processor) applyCategoryDecision(ctx context.Context, sourceEventID, household string, extraction Extraction, result PolicyResult) PolicyResult {
	if result.ReviewType != "AMBIGUOUS_CATEGORY" {
		return result
	}
	categoryID, provenance := p.resolveNewMerchantCategory(ctx, sourceEventID, household, extraction)
	if categoryID == "" {
		return result
	}
	result.CategoryID, result.AutoConfirm = categoryID, true
	result.Status, result.ReviewType = "CONFIRMED", ""
	// A Jev-chosen category is a category we now know, so the row must not keep
	// the review-flavoured placeholder as its ledger description (Hermes #133).
	result.Description = "Pengeluaran dengan kategori yang dipilih otomatis."
	result.CategoryProvenance = &provenance
	return result
}

func applyEmailReceivedTimeFallback(extraction *Extraction, receivedAt time.Time) bool {
	if extraction.Kind != "TRANSACTION" || extraction.AmountIDR == nil || extraction.TransactionAt != nil || receivedAt.IsZero() {
		return false
	}
	extraction.TransactionAt = &receivedAt
	extraction.TransactionAtSource = "EMAIL_RECEIVED_AT"
	extraction.MissingFields = removeMissing(extraction.MissingFields, "transaction_at")
	if extraction.Review != nil {
		extraction.Review.MissingFields = removeMissing(extraction.Review.MissingFields, "transaction_at")
		extraction.Review.SuggestedActions = removeMissing(extraction.Review.SuggestedActions, "USE_EMAIL_RECEIVED_AT")
		extraction.Review.SuggestedActions = removeMissing(extraction.Review.SuggestedActions, "ENTER_TRANSACTION_TIME")
		if len(extraction.Review.MissingFields) == 0 && len(extraction.Review.SuggestedActions) == 0 {
			extraction.Review = nil
		} else {
			extraction.Review.Summary = "Waktu transaksi menggunakan waktu penerimaan email. Lengkapi detail transaksi yang masih diperlukan."
		}
	}
	return true
}

func (p *Processor) persistExtractionFailure(ctx context.Context, sourceID, listenerID, model, validation, policy string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,gateway_model,tool_schema_version,output_json,validation_status,policy_result) VALUES($1,$2,'native_tool',$3,$4,'{}',$5,$6) ON CONFLICT(source_event_id) DO UPDATE SET gateway_model=excluded.gateway_model,validation_status=excluded.validation_status,policy_result=excluded.policy_result`, sourceID, listenerID, model, ToolSchemaVersion, validation, policy); err != nil {
		return err
	}
	status := "FAILED"
	if validation == "INVALID" {
		status = "NEEDS_REVIEW"
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status=$2,parser_name='bank-email-generic',parser_version=$3 WHERE id=$1`, sourceID, status, ToolSchemaVersion); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	description := ledgerDescription(extraction, result)
	merchant := value(extraction.Merchant)
	counterparty := value(extraction.Counterparty)
	reference := value(extraction.Reference)
	var proposalID string
	metadata, _ := json.Marshal(map[string]any{"bank_email_policy": "v4", "transaction_at_source": extraction.TransactionAtSource, "llm_review": extraction.Review})
	err = tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,merchant_raw,counterparty_raw,category_candidate_id,description,confidence,proposal_status,metadata_json) VALUES($1,$2,$3,$4,'IDR',$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,'')::uuid,$9,$10,$11,$12::jsonb) RETURNING id`, listener.HouseholdID, sourceID, transactionType, amount, *at, merchant, counterparty, categoryID, description, extraction.Confidence, proposalStatus, string(metadata)).Scan(&proposalID)
	if err != nil {
		return err
	}
	var transactionID string
	err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,merchant_id,category_id,description,counterparty_name,external_reference,source_confidence,classification_confidence,confirmed_at) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,'IDR',$6,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),$12,$13,CASE WHEN $4='CONFIRMED' THEN now() END) RETURNING id`, listener.HouseholdID, listener.AccountID, transactionType, transactionStatus, amount, *at, merchantID, categoryID, description, counterparty, reference, extraction.Confidence, extraction.Confidence).Scan(&transactionID)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'BANK_EMAIL',$3,jsonb_build_object('proposal_id',$4::uuid,'listener_id',$5::uuid,'transaction_at_source',$6::text))`, transactionID, sourceID, extraction.Confidence, proposalID, listener.ID, extraction.TransactionAtSource); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='EMAIL_RECEIVED_AT_FALLBACK',resolution_values=jsonb_build_object('transaction_at',$2::timestamptz),updated_at=now() WHERE source_event_id=$1 AND transaction_id IS NULL AND status IN ('PENDING_SEND','OPEN')`, sourceID, *at); err != nil {
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
			message := bankReviewMessage(result.ReviewType, amount, *at, description)
			if err = workerTelegram.EnqueueReviewRequest(ctx, tx, transactionID, result.ReviewType, chatID, 0, message); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'WORKER','CREATE_FROM_BANK_EMAIL','transaction',$2,jsonb_build_object('source_event_id',$3::uuid,'listener_id',$4::uuid,'proposal_id',$5::uuid,'policy_result',$6::text,'auto_confirm',$7::boolean,'tool_schema_version',$8::text))`, listener.HouseholdID, transactionID, sourceID, listener.ID, proposalID, result.Status, result.AutoConfirm, ToolSchemaVersion); err != nil {
		return err
	}
	if provenance := result.CategoryProvenance; provenance != nil {
		summary, marshalErr := json.Marshal(map[string]any{"category": provenance.Slug, "category_accepted": provenance.Accepted})
		if marshalErr != nil {
			return marshalErr
		}
		if _, err = tx.Exec(ctx, `INSERT INTO judgment_decision(household_id,source_event_id,task,model,policy_version,question_keys,answer_summary_json,outcome) VALUES($1,$2,'BANK_EMAIL_CATEGORY',NULLIF($3,''),$4,$5,$6::jsonb,'CONFIRMED')`, listener.HouseholdID, sourceID, provenance.Model, provenance.PolicyVersion, []string{"category"}, string(summary)); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func ledgerDescription(extraction Extraction, result PolicyResult) string {
	description := value(extraction.Description)
	if description == "" {
		description = result.Description
	}
	return description
}

func bankReviewMessage(reviewType, amount string, transactionAt time.Time, description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		description = "Belum tersedia"
	}
	context := "Nominal: Rp" + workerTelegram.FormatIDR(amount) +
		"\nWaktu: " + transactionAt.In(time.FixedZone("WIB", 7*60*60)).Format("02/01/2006 15:04") + " WIB" +
		"\nKeterangan: " + description
	if reviewType == "UNKNOWN_MERCHANT" || reviewType == "UNKNOWN_PURPOSE" {
		return context
	}
	return "🏦 Transaksi bank perlu ditinjau\n\n" + context + "\n\nPilih kategori atau lengkapi detail transaksi."
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
	err = tx.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,regexp_replace(trim($2), '[[:space:]]+', ' ', 'g')) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET updated_at=merchant.updated_at RETURNING id`, household, normalized).Scan(&id)
	return id, err
}
