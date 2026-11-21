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
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/merchantmemory"
	workerTelegram "github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

type Processor struct {
	pool                   *pgxpool.Pool
	extractor              *Extractor
	verifier               jeverifier
	categoryAutoConfirmOff bool
}

func NewProcessor(pool *pgxpool.Pool, extractor *Extractor) *Processor {
	return &Processor{pool: pool, extractor: extractor}
}

func (p *Processor) SetVerifier(verifier jeverifier) { p.verifier = verifier }

func (p *Processor) SetCategoryAutoConfirm(enabled bool) { p.categoryAutoConfirmOff = !enabled }

func applyCategoryAutoConfirmSwitch(result PolicyResult, enabled bool) PolicyResult {
	if enabled || !result.AutoConfirm || result.CategoryID == "" {
		return result
	}
	result.AutoConfirm = false
	result.Status, result.ReviewType = "NEEDS_REVIEW", "AMBIGUOUS_CATEGORY"
	return result
}

func applyVerificationGate(result PolicyResult, verified bool) PolicyResult {
	if verified || !result.AutoConfirm {
		return result
	}
	result.AutoConfirm = false
	if result.Status == "" || result.Status == "CONFIRMED" {
		result.Status = "NEEDS_REVIEW"
	}
	if result.ReviewType == "" {
		switch result.Type {
		case "TRANSFER":
			result.ReviewType = "TRANSFER_CLASSIFICATION"
		case "EXPENSE":
			result.ReviewType = "AMBIGUOUS_CATEGORY"
		default:
			result.ReviewType = "DOCUMENT_EXTRACTION_LOW_CONFIDENCE"
		}
	}
	if result.Description == "" {
		result.Description = "Ekstraksi belum diverifikasi oleh bidang semantik."
	}
	return result
}

type Payload struct {
	SourceEventID  string  `json:"source_event_id"`
	ReviewID       string  `json:"review_id,omitempty"`
	AmountIDR      *string `json:"amount_idr,omitempty"`
	TransactionAt  *string `json:"transaction_at,omitempty"`
	TelegramChatID int64   `json:"telegram_chat_id,omitempty"`
	UserID         string  `json:"user_id,omitempty"`
}

func (p *Processor) Complete(ctx context.Context, payload Payload) error {
	if payload.ReviewID == "" {
		return fmt.Errorf("bank review id is required")
	}
	var household, listenerID, bank, sender, accountID string
	var itemStatus string
	var raw []byte
	if err := p.pool.QueryRow(ctx, `SELECT s.household_id,l.id,l.bank_name,l.sender_address,COALESCE(l.account_id::text,''),e.output_json,ri.status FROM review_item ri JOIN source_event s ON s.id=ri.source_event_id JOIN bank_email_extraction e ON e.source_event_id=s.id JOIN bank_email_listener l ON l.id=e.listener_id WHERE ri.id=$1 AND ri.source_event_id=$2 FOR UPDATE`, payload.ReviewID, payload.SourceEventID).Scan(&household, &listenerID, &bank, &sender, &accountID, &raw, &itemStatus); err != nil {
		return err
	}
	if itemStatus != "OPEN" && itemStatus != "PENDING_SEND" {
		return nil
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
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='COMPLETE_BANK_FACTS',updated_at=now() WHERE id=$1 AND status IN ('OPEN','PENDING_SEND')`, payload.ReviewID); err != nil {
			return err
		}
		if err = resolveBankReviewProjection(ctx, tx, payload.ReviewID); err != nil {
			return err
		}
		if err = auditBankCompletion(ctx, tx, household, payload); err != nil {
			return err
		}
		if err = reviewdomain.RefreshSourceLifecycle(ctx, tx, household, payload.SourceEventID); err != nil {
			return err
		}
		return tx.Commit(ctx)
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
	if err := p.persist(ctx, listener, payload.SourceEventID, extraction, result, payload.ReviewID); err != nil {
		return err
	}
	var linked bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM transaction_proposal WHERE source_event_id=$1 AND proposal_status='MERGED' AND metadata_json ? 'matched_transaction_id')`, payload.SourceEventID).Scan(&linked); err != nil {
		return err
	}
	if linked {
		result.Status = "LINKED"
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE bank_email_extraction SET output_json=$2::jsonb,validation_status='VALID',policy_result=$3 WHERE source_event_id=$1`, payload.SourceEventID, mustJSON(extraction), result.Status); err != nil {
		return err
	}
	if payload.TelegramChatID != 0 {
		message := "Email bank diproses tanpa mencatat transaksi."
		switch result.Status {
		case "LINKED":
			message = "Email bank ditautkan ke transaksi yang sudah tercatat."
		case "CONFIRMED":
			message = "Transaksi bank dicatat."
		case "NEEDS_REVIEW":
			message = "Fakta bank tersimpan. Transaksi masih perlu ditinjau melalui kartu review baru."
		}
		if _, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) SELECT 'SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$2::bigint,'text',$3::text) WHERE EXISTS(SELECT 1 FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.review_item_id=$1 AND rr.telegram_chat_id=$2)`, payload.ReviewID, payload.TelegramChatID, message); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='COMPLETE_BANK_FACTS',resolution_values=jsonb_build_object('amount_idr',$2::text,'transaction_at',$3::timestamptz),updated_at=now() WHERE id=$1 AND status IN ('OPEN','PENDING_SEND')`, payload.ReviewID, value(extraction.AmountIDR), extraction.TransactionAt); err != nil {
		return err
	}
	if err = resolveBankReviewProjection(ctx, tx, payload.ReviewID); err != nil {
		return err
	}
	if err = auditBankCompletion(ctx, tx, household, payload); err != nil {
		return err
	}
	if err = reviewdomain.RefreshSourceLifecycle(ctx, tx, household, payload.SourceEventID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func auditBankCompletion(ctx context.Context, tx pgx.Tx, household string, payload Payload) error {
	actor := "USER"
	if payload.TelegramChatID != 0 {
		actor = "TELEGRAM"
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,NULLIF($4,'')::uuid,'RESOLVE_REVIEW','review_item',$3,jsonb_build_object('action','COMPLETE_BANK_FACTS'))`, household, actor, payload.ReviewID, payload.UserID)
	return err
}

func resolveBankReviewProjection(ctx context.Context, tx pgx.Tx, reviewItemID string) error {
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('PENDING_SEND','OPEN')`, reviewItemID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1)`, reviewItemID)
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

func (p *Processor) Process(ctx context.Context, payload Payload) error {
	var household, listenerID, bank, sender, subject, date, auth, body, messageID string
	var receivedAt time.Time
	err := p.pool.QueryRow(ctx, `SELECT s.household_id,s.received_at,l.id,l.bank_name,l.sender_address,m.subject,m.email_date,m.authentication_results,m.body,m.message_id FROM source_event s JOIN bank_email_event m ON m.source_event_id=s.id JOIN bank_email_listener l ON l.id=m.listener_id AND l.household_id=s.household_id WHERE s.id=$1 AND l.active`, payload.SourceEventID).Scan(&household, &receivedAt, &listenerID, &bank, &sender, &subject, &date, &auth, &body, &messageID)
	if err != nil {
		return fmt.Errorf("load bank email event: %w", err)
	}
	ctx = gateway.WithSourceEvent(ctx, payload.SourceEventID)
	ctx = judgment.WithSourceEvent(ctx, payload.SourceEventID)
	ctx = gateway.WithPhaseMetadata(ctx, "EXTRACTION", "")
	var accountID string
	_ = p.pool.QueryRow(ctx, `SELECT COALESCE(account_id::text,'') FROM bank_email_listener WHERE id=$1 AND household_id=$2`, listenerID, household).Scan(&accountID)
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: bank, SenderAddress: sender, AccountID: accountID, TrackingPolicy: "SPENDING_ONLY", Active: true}
	extraction, meta, err := p.extractor.Extract(ctx, payload.SourceEventID, listener, TrustedEmail{MessageID: messageID, Subject: subject, Date: date, AuthenticationResults: auth, Body: body})
	if err != nil {
		var schemaErr SchemaError
		if errors.As(err, &schemaErr) {
			return p.persistExtractionFailure(ctx, payload.SourceEventID, listenerID, meta.Model, "INVALID", "REPAIR")
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
	result = applyCategoryAutoConfirmSwitch(result, !p.categoryAutoConfirmOff)
	if !p.categoryAutoConfirmOff {
		result, err = p.applyCategoryDecision(ctx, payload.SourceEventID, household, extraction, result)
		if err != nil {
			return err
		}
	}
	status := result.Status
	if status == "" {
		status = "NEEDS_REVIEW"
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
	if missing(extraction, "amount_idr") || missing(extraction, "transaction_at") {
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
		_ = p.persistExtractionFailure(ctx, payload.SourceEventID, listenerID, meta.Model, "VERIFICATION_FAILED", "RETRY")
		return fmt.Errorf("bank email evidence verification unavailable: %w", verifyErr)
	}
	if verified {
		if persistErr := p.persistEvidenceVerification(ctx, payload.SourceEventID, listenerID, meta.Model, verification); persistErr != nil {
			return persistErr
		}
	}
	if verified && !verification.supported() {
		fact, conflict, material := verification.materialResidual()
		if !material {
			return fmt.Errorf("bank email verification has no failed material predicate")
		}
		if verification.needsBankFactsReview() {
			return p.reviewIncompleteExtraction(ctx, household, payload.SourceEventID, ToolSchemaVersion, "UNKNOWN_BANK_TEMPLATE", verificationReviewDecision(household, payload.SourceEventID, extraction, verification, fact, conflict))
		}
		verified = false
	}
	result = applyVerificationGate(result, verified)
	var alreadyPersisted bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM transaction_proposal WHERE source_event_id=$1)`, payload.SourceEventID).Scan(&alreadyPersisted); err == nil && alreadyPersisted {
		return nil
	}
	return p.persist(ctx, listener, payload.SourceEventID, extraction, result, "")
}

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
	var itemID string
	if err = tx.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,$3,'OPEN',$4::jsonb) ON CONFLICT DO NOTHING RETURNING id`, household, sourceEventID, reviewType, string(encoded)).Scan(&itemID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT id FROM review_item WHERE source_event_id=$1 AND review_type=$2 AND status IN ('OPEN','PENDING_SEND') LIMIT 1`, sourceEventID, reviewType).Scan(&itemID); err != nil {
			return err
		}
	}
	if err = workerTelegram.ProjectReviewItem(ctx, tx, household, itemID, 0, "", 0); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func partialDecision(household, sourceEventID string, extraction Extraction, reviewType string, missingFacts []string, whyNotAuto string) reviewdec.Decision {
	known := map[string]any{}
	if amount := value(extraction.AmountIDR); amount != "" {
		known["amount_idr"] = amount
	}
	if at := timeValue(extraction.TransactionAt); at != "" {
		known["transaction_at"] = at
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

func verificationReviewDecision(household, sourceEventID string, extraction Extraction, verification EvidenceVerification, fact string, conflict bool) reviewdec.Decision {
	missing := []string{fact}
	why := "the email did not support a material transaction fact the household must confirm"
	if conflict {
		missing = nil
		why = "the email evidence conflicts on a material fact; Go refuses to guess"
	}
	decision := partialDecision(household, sourceEventID, extraction, "UNKNOWN_BANK_TEMPLATE", missing, why)
	decision.PolicyVersion = verification.PolicyVersion
	decision.Provenance["claim_outcomes"] = verification.ClaimOutcomes
	accepted := []string{}
	for _, entry := range []struct{ claim, dimension string }{{"amount_supported", "amount_idr"}, {"direction_supported", "direction"}, {"transaction_observed", "transaction_observed"}} {
		if verification.ClaimOutcomes[entry.claim] == "YES" {
			accepted = append(accepted, entry.dimension)
		}
	}
	decision.Provenance["accepted_dimensions_at_validation"] = accepted
	decision.AffectedFacts = []string{fact}
	switch {
	case conflict:
		decision.DecisionClass = reviewdec.ClassEvidenceConflict
		decision.Consequence = reviewdec.IndependentEvidenceConflict
		if disputed, ok := decision.KnownFacts[fact]; ok {
			decision.ProposedFacts[fact] = disputed
			delete(decision.KnownFacts, fact)
		}
	default:
		decision.Consequence = reviewdec.BoundedResidual
	}
	return decision
}

func (p *Processor) persistEvidenceVerification(ctx context.Context, sourceEventID, listenerID, model string, verification EvidenceVerification) error {
	summary, err := json.Marshal(map[string]any{
		"transaction_observed": verification.TransactionObserved,
		"amount_supported":     verification.AmountSupported,
		"direction_supported":  verification.DirectionSupported,
		"semantic_grounded":    verification.SemanticGrounded,
		"claim_outcomes":       verification.ClaimOutcomes,
		"supported":            verification.supported(),
		"policy_version":       verification.PolicyVersion,
	})
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `INSERT INTO bank_email_evidence_verification(source_event_id,listener_id,bank_email_verification_policy_version,gateway_model,answer_summary_json) VALUES($1,$2,$3,NULLIF($4,''),$5::jsonb) ON CONFLICT(source_event_id) DO UPDATE SET gateway_model=excluded.gateway_model,answer_summary_json=excluded.answer_summary_json,bank_email_verification_policy_version=excluded.bank_email_verification_policy_version`, sourceEventID, listenerID, verification.PolicyVersion, verification.Model, string(summary))
	return err
}

func (p *Processor) applyCategoryDecision(ctx context.Context, sourceEventID, household string, extraction Extraction, result PolicyResult) (PolicyResult, error) {
	if result.ReviewType != "AMBIGUOUS_CATEGORY" {
		return result, nil
	}
	categoryID, provenance, err := p.resolveNewMerchantCategory(ctx, sourceEventID, household, extraction)
	if err != nil {
		return result, err
	}
	if categoryID == "" {
		return result, nil
	}
	result.CategoryID, result.AutoConfirm = categoryID, true
	result.Status, result.ReviewType = "CONFIRMED", ""
	result.Description = "Pengeluaran dengan kategori yang dipilih otomatis."
	result.CategoryProvenance = &provenance
	return result, nil
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
	if policy == "NEEDS_REVIEW" {
		status = "NEEDS_REVIEW"
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status=$2,parser_name='bank-email-generic',parser_version=$3 WHERE id=$1`, sourceID, status, ToolSchemaVersion); err != nil {
		return err
	}
	if status == "FAILED" && policy == "REPAIR" {
		if err = recordFailedBankEmail(ctx, tx, sourceID, validation); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *Processor) TerminalFailureTx(ctx context.Context, tx pgx.Tx, sourceEventID string) error {
	if !reviewdomain.IsSourceEventID(sourceEventID) {
		return nil
	}
	var claimed string
	err := tx.QueryRow(ctx, `UPDATE source_event SET processing_status='FAILED' WHERE id=$1 AND source_type='BANK_EMAIL' AND processing_status IN ('RECEIVED','PROCESSING','FAILED') RETURNING id::text`, sourceEventID).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	validation := "ERROR"
	var latest string
	err = tx.QueryRow(ctx, `SELECT validation_status FROM bank_email_extraction WHERE source_event_id=$1 ORDER BY created_at DESC LIMIT 1`, sourceEventID).Scan(&latest)
	if err == nil && latest != "" {
		validation = latest
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return recordFailedBankEmail(ctx, tx, sourceEventID, validation)
}

func recordFailedBankEmail(ctx context.Context, tx pgx.Tx, sourceID, reason string) error {
	var household string
	if err := tx.QueryRow(ctx, `SELECT household_id::text FROM source_event WHERE id=$1`, sourceID).Scan(&household); err != nil {
		return err
	}
	return reviewdomain.RecordFailedSourceAction(ctx, tx, reviewdomain.FailedSource{
		HouseholdID: household, SourceEventID: sourceID, SourceType: "BANK_EMAIL", Reason: reason,
	})
}

func (p *Processor) persist(ctx context.Context, listener Listener, sourceID string, extraction Extraction, result PolicyResult, completingReviewID string) error {
	amount, at := value(extraction.AmountIDR), extraction.TransactionAt
	if amount == "" || at == nil {
		return fmt.Errorf("validated bank extraction lacks ledger facts")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var noticeHousehold string
	var noticeAt *time.Time
	err = tx.QueryRow(ctx, `SELECT s.household_id,e.notice_at FROM source_event s JOIN bank_email_extraction e ON e.source_event_id=s.id WHERE s.id=$1 FOR UPDATE OF e`, sourceID).Scan(&noticeHousehold, &noticeAt)
	if err != nil {
		return err
	}
	if noticeHousehold != listener.HouseholdID {
		return fmt.Errorf("bank email source household mismatch")
	}
	autoNotice := result.AutoConfirm && completingReviewID == ""
	if autoNotice {
		if noticeAt != nil {
			return nil
		}
	}
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
	var duplicateCandidates []string
	if transactionType == "EXPENSE" || transactionType == "INCOME" || transactionType == "REFUND" {
		observedMerchant := merchant
		if observedMerchant == "" {
			observedMerchant = counterparty
		}
		if lockErr := reviewdomain.LockLedgerEvent(ctx, tx, listener.HouseholdID, amount); lockErr != nil {
			return lockErr
		}
		matches, matchErr := reviewdomain.FindLedgerMatches(ctx, tx, reviewdomain.LedgerMatchQuery{HouseholdID: listener.HouseholdID, Type: transactionType, AmountIDR: amount, At: *at, Merchant: observedMerchant, ExcludeSourceID: sourceID, ExcludeEvidenceType: "BANK_EMAIL"})
		if matchErr != nil {
			return matchErr
		}
		if strong := reviewdomain.StrongLedgerMatch(matches); strong != nil {
			if err := p.linkBankEvidence(ctx, tx, listener, sourceID, extraction, *strong, bankLink{autoNotice: autoNotice, household: noticeHousehold, amount: amount, at: *at, description: description, merchant: merchant, counterparty: counterparty, reference: reference, completingReviewID: completingReviewID}); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		for _, match := range matches {
			duplicateCandidates = append(duplicateCandidates, match.ID)
		}
		if len(duplicateCandidates) > 0 {
			transactionStatus, proposalStatus, autoNotice = "NEEDS_REVIEW", "NEEDS_REVIEW", false
			result.AutoConfirm, result.ReviewType, result.Status = false, "POSSIBLE_DUPLICATE", "NEEDS_REVIEW"
		}
	}
	var proposalID string
	metadata, _ := json.Marshal(map[string]any{"bank_email_policy": "v4", "transaction_at_source": extraction.TransactionAtSource, "llm_review": extraction.Review})
	err = tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,merchant_raw,counterparty_raw,category_candidate_id,description,confidence,proposal_status,metadata_json) VALUES($1,$2,$3,$4,'IDR',$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,'')::uuid,$9,$10,$11,$12::jsonb) RETURNING id`, listener.HouseholdID, sourceID, transactionType, amount, *at, merchant, counterparty, categoryID, description, extraction.Confidence, proposalStatus, string(metadata)).Scan(&proposalID)
	if err != nil {
		return err
	}
	var transactionID string
	err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,currency,transaction_at,merchant_id,category_id,description,counterparty_name,external_reference,source_confidence,classification_confidence,confirmed_at,auto_confirmed_at) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,'IDR',$6,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),$12,$13,CASE WHEN $4='CONFIRMED' THEN now() END,CASE WHEN $14 THEN now() END) RETURNING id`, listener.HouseholdID, listener.AccountID, transactionType, transactionStatus, amount, *at, merchantID, categoryID, description, counterparty, reference, extraction.Confidence, extraction.Confidence, result.AutoConfirm).Scan(&transactionID)
	if err != nil {
		return err
	}
	if autoNotice {
		var claimed string
		err = tx.QueryRow(ctx, `UPDATE bank_email_extraction SET notice_at=now() WHERE source_event_id=$1 AND notice_at IS NULL RETURNING source_event_id::text`, sourceID).Scan(&claimed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		message := "Transaksi bank sudah tercatat.\n\n" + bankTransactionContext(amount, *at, description)
		if _, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json)
			SELECT 'SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',ti.telegram_user_id,'text',$2::text)
			FROM telegram_identity ti
			JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active
			WHERE ti.household_id=$1 AND ti.active`, noticeHousehold, message); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'BANK_EMAIL',$3,jsonb_build_object('proposal_id',$4::uuid,'listener_id',$5::uuid,'transaction_at_source',$6::text))`, transactionID, sourceID, extraction.Confidence, proposalID, listener.ID, extraction.TransactionAtSource); err != nil {
		return err
	}
	if transactionStatus == "CONFIRMED" {
		if err = reviewdomain.RefreshOpenCycleResiduals(ctx, tx, listener.HouseholdID, *at, ""); err != nil {
			return err
		}
	}
	settled, err := tx.Query(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='EMAIL_RECEIVED_AT_FALLBACK',resolution_values=jsonb_build_object('transaction_at',$2::timestamptz),updated_at=now() WHERE source_event_id=$1 AND transaction_id IS NULL AND status IN ('PENDING_SEND','OPEN') AND ($3='' OR id<>$3::uuid) RETURNING id::text`, sourceID, *at, completingReviewID)
	if err != nil {
		return err
	}
	var settledIDs []string
	for settled.Next() {
		var id string
		if err = settled.Scan(&id); err != nil {
			settled.Close()
			return err
		}
		settledIDs = append(settledIDs, id)
	}
	settled.Close()
	if err = settled.Err(); err != nil {
		return err
	}
	for _, id := range settledIDs {
		if err = resolveBankReviewProjection(ctx, tx, id); err != nil {
			return err
		}
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
		decision := transactionReviewDecision(listener.HouseholdID, sourceID, extraction, result, transactionID)
		if len(duplicateCandidates) > 0 {
			decision = duplicateReviewDecision(decision, transactionID, duplicateCandidates)
		}
		var chatID int64
		if e := tx.QueryRow(ctx, `SELECT ti.telegram_user_id FROM telegram_identity ti JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active WHERE ti.household_id=$1 AND ti.active ORDER BY ti.created_at LIMIT 1`, listener.HouseholdID).Scan(&chatID); e == nil {
			message := bankReviewMessage(result.ReviewType, amount, *at, description)
			if err = workerTelegram.EnqueueReviewRequest(ctx, tx, transactionID, result.ReviewType, chatID, 0, message, decision); err != nil {
				return err
			}
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		} else if _, err = workerTelegram.CreateTransactionReviewItem(ctx, tx, transactionID, result.ReviewType, "OPEN", decision); err != nil {
			return err
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

func bankTransactionContext(amount string, transactionAt time.Time, description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		description = "Belum tersedia"
	}
	return "Nominal: Rp" + workerTelegram.FormatIDR(amount) +
		"\nWaktu: " + transactionAt.In(time.FixedZone("WIB", 7*60*60)).Format("02/01/2006 15:04") + " WIB" +
		"\nKeterangan: " + description
}

func bankReviewMessage(_ string, amount string, transactionAt time.Time, description string) string {
	return bankTransactionContext(amount, transactionAt, description)
}

func loadMerchantMemory(ctx context.Context, q merchantmemory.Querier, household, raw string) (MerchantMemory, error) {
	match, err := merchantmemory.Lookup(ctx, q, household, raw)
	if err != nil || match == nil {
		return MerchantMemory{}, err
	}
	return MerchantMemory{MerchantID: match.MerchantID, CategoryID: match.CategoryID, AutoApply: true}, nil
}
func resolveMerchantID(ctx context.Context, tx pgx.Tx, household, raw string) (string, error) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", nil
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT min(normalized_merchant_id::text) FROM merchant_alias WHERE household_id=$1 AND lower(regexp_replace(btrim(raw_name),'[[:space:]]+',' ','g'))=lower(regexp_replace(btrim($2),'[[:space:]]+',' ','g')) GROUP BY household_id,lower(regexp_replace(btrim(raw_name),'[[:space:]]+',' ','g')) HAVING count(DISTINCT normalized_merchant_id)=1`, household, raw).Scan(&id)
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

type bankLink struct {
	autoNotice                                                        bool
	household, amount, description, merchant, counterparty, reference string
	completingReviewID                                                string
	at                                                                time.Time
}

func (p *Processor) linkBankEvidence(ctx context.Context, tx pgx.Tx, listener Listener, sourceID string, extraction Extraction, match reviewdomain.LedgerMatch, link bankLink) error {
	metadata, _ := json.Marshal(map[string]any{"bank_email_policy": "v4", "transaction_at_source": extraction.TransactionAtSource, "matched_transaction_id": match.ID, "match_score": match.Score})
	var proposalID string
	if err := tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,merchant_raw,counterparty_raw,description,confidence,proposal_status,metadata_json) SELECT $1,$2,t.type,$3,'IDR',$4,NULLIF($5,''),NULLIF($6,''),$7,$8,'MERGED',$9::jsonb FROM transaction t WHERE t.id=$10 AND t.household_id=$1 RETURNING id`, listener.HouseholdID, sourceID, link.amount, link.at, link.merchant, link.counterparty, link.description, extraction.Confidence, string(metadata), match.ID).Scan(&proposalID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'BANK_EMAIL',$3,jsonb_build_object('proposal_id',$4::uuid,'listener_id',$5::uuid,'transaction_at_source',$6::text,'match_score',$7::numeric)) ON CONFLICT DO NOTHING`, match.ID, sourceID, extraction.Confidence, proposalID, listener.ID, extraction.TransactionAtSource, match.Score); err != nil {
		return err
	}
	filled, err := reviewdomain.EnrichLinkedTransaction(ctx, tx, listener.HouseholdID, match.ID, reviewdomain.EvidenceEnrichment{MerchantName: link.merchant, AccountID: listener.AccountID, Reference: link.reference, Counterparty: link.counterparty})
	if err != nil {
		return err
	}
	if filled == nil {
		filled = []string{}
	}
	settled, err := tx.Query(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='LINKED_TO_EXISTING_TRANSACTION',resolution_values=jsonb_build_object('transaction_id',$2::uuid),updated_at=now() WHERE source_event_id=$1 AND transaction_id IS NULL AND status IN ('PENDING_SEND','OPEN') AND ($3='' OR id<>$3::uuid) RETURNING id::text`, sourceID, match.ID, link.completingReviewID)
	if err != nil {
		return err
	}
	settledIDs, err := pgx.CollectRows(settled, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, id := range settledIDs {
		if err := resolveBankReviewProjection(ctx, tx, id); err != nil {
			return err
		}
	}
	if link.autoNotice {
		var claimed string
		err := tx.QueryRow(ctx, `UPDATE bank_email_extraction SET notice_at=now() WHERE source_event_id=$1 AND notice_at IS NULL RETURNING source_event_id::text`, sourceID).Scan(&claimed)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			message := "Email bank ditautkan ke transaksi yang sudah tercatat.\n\n" + bankTransactionContext(link.amount, link.at, link.description)
			if _, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json)
			SELECT 'SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',ti.telegram_user_id,'text',$2::text)
			FROM telegram_identity ti
			JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active
			WHERE ti.household_id=$1 AND ti.active`, link.household, message); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET parser_name='bank-email-generic',parser_version=$2 WHERE id=$1`, sourceID, ToolSchemaVersion); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'WORKER','LINK_BANK_EMAIL_EVIDENCE','transaction',$2,jsonb_build_object('source_event_id',$3::uuid,'listener_id',$4::uuid,'proposal_id',$5::uuid,'match_score',$6::numeric,'enriched',$7::text[]))`, listener.HouseholdID, match.ID, sourceID, listener.ID, proposalID, match.Score, filled); err != nil {
		return err
	}
	return reviewdomain.RefreshSourceLifecycle(ctx, tx, listener.HouseholdID, sourceID)
}

func duplicateReviewDecision(base reviewdec.Decision, transactionID string, candidates []string) reviewdec.Decision {
	decision, ok := reviewdec.Preset("POSSIBLE_DUPLICATE", "transaction", transactionID)
	if !ok {
		return base
	}
	for key, value := range base.KnownFacts {
		decision.KnownFacts[key] = value
	}
	decision.KnownFacts["prior_match_transaction_ids"] = candidates
	decision.SourceEventID = base.SourceEventID
	decision.EvidenceRefs = base.EvidenceRefs
	decision.Provenance = map[string]any{"pipeline": "bank-email-generic", "cross_lane_duplicate": true}
	decision.PolicyVersion = base.PolicyVersion
	decision.WhyNotAuto = "another source already recorded a transaction that may be this same bank event"
	return decision
}
