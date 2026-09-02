package gmail

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Processor struct {
	pool   *pgxpool.Pool
	client *client
}

type HistoryPayload struct {
	SourceEventID string `json:"source_event_id"`
	HistoryID     string `json:"history_id"`
}

type RenewPayload struct {
	HouseholdID string `json:"household_id"`
}

func NewProcessor(pool *pgxpool.Pool, config Config) (*Processor, error) {
	apiClient, err := newClient(config)
	if err != nil || apiClient == nil {
		return nil, err
	}
	return &Processor{pool: pool, client: apiClient}, nil
}

func DecodeHistoryPayload(raw json.RawMessage) (HistoryPayload, error) {
	var payload HistoryPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.SourceEventID == "" || payload.HistoryID == "" {
		return HistoryPayload{}, fmt.Errorf("invalid Gmail history job payload")
	}
	return payload, nil
}

func DecodeRenewPayload(raw json.RawMessage) (RenewPayload, error) {
	var payload RenewPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.HouseholdID == "" {
		return RenewPayload{}, fmt.Errorf("invalid Gmail watch job payload")
	}
	return payload, nil
}

// SeedRenewalJobs creates at most one active renewal job per integration. It is
// safe to call on every maintenance tick and recovers installations connected
// before watch support was deployed.
func (p *Processor) SeedRenewalJobs(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO job (type,payload_json,max_attempts)
		SELECT 'RENEW_GMAIL_WATCH',jsonb_build_object('household_id',g.household_id),20
		FROM gmail_integration g
		WHERE g.status IN ('CONNECTED','WATCH_ACTIVE','ERROR')
		  AND (g.watch_expiration IS NULL OR g.watch_expiration < now()+interval '2 days')
		  AND NOT EXISTS (
			SELECT 1 FROM job j
			WHERE j.type='RENEW_GMAIL_WATCH'
			  AND j.status IN ('PENDING','RUNNING')
			  AND j.payload_json->>'household_id'=g.household_id::text
		  )`)
	return err
}

func (p *Processor) RenewWatch(ctx context.Context, householdID string) error {
	refreshToken, err := p.refreshToken(ctx, householdID)
	if err != nil {
		return err
	}
	accessToken, err := p.client.accessToken(ctx, refreshToken)
	if err != nil {
		return p.markIntegrationError(ctx, householdID, err)
	}
	watch, err := p.client.watch(ctx, accessToken)
	if err != nil {
		return p.markIntegrationError(ctx, householdID, err)
	}
	expirationMilliseconds, _ := strconv.ParseInt(watch.Expiration, 10, 64)
	expiration := time.UnixMilli(expirationMilliseconds)
	_, err = p.pool.Exec(ctx, `UPDATE gmail_integration SET status='WATCH_ACTIVE',history_id=$2,watch_expiration=$3,updated_at=now() WHERE household_id=$1`, householdID, watch.HistoryID, expiration)
	return err
}

func (p *Processor) ProcessHistory(ctx context.Context, payload HistoryPayload) error {
	var householdID, storedHistoryID string
	var encryptedToken []byte
	err := p.pool.QueryRow(ctx, `
		SELECT s.household_id,g.encrypted_refresh_token,COALESCE(g.history_id,'')
		FROM source_event s JOIN gmail_integration g ON g.household_id=s.household_id
		WHERE s.id=$1 AND s.source_type='SYSTEM'`, payload.SourceEventID).Scan(&householdID, &encryptedToken, &storedHistoryID)
	if err != nil {
		return fmt.Errorf("load Gmail notification: %w", err)
	}
	if storedHistoryID == "" {
		return fmt.Errorf("Gmail watch has no starting history ID")
	}
	refreshToken, err := p.client.decrypt(householdID, encryptedToken)
	if err != nil {
		return err
	}
	accessToken, err := p.client.accessToken(ctx, refreshToken)
	if err != nil {
		return p.markIntegrationError(ctx, householdID, err)
	}

	latestHistoryID := payload.HistoryID
	pageToken := ""
	seen := make(map[string]struct{})
	for {
		history, err := p.client.history(ctx, accessToken, storedHistoryID, pageToken)
		if err != nil {
			return err
		}
		if history.HistoryID != "" {
			latestHistoryID = history.HistoryID
		}
		for _, record := range history.History {
			for _, added := range record.MessagesAdded {
				messageID := added.Message.ID
				if messageID == "" {
					continue
				}
				if _, exists := seen[messageID]; exists {
					continue
				}
				seen[messageID] = struct{}{}
				metadata, err := p.client.messageMetadata(ctx, accessToken, messageID)
				if err != nil {
					return err
				}
				if !p.isTrustedSender(metadata) {
					continue
				}
				message, raw, err := p.client.messageFull(ctx, accessToken, messageID)
				if err != nil {
					return err
				}
				if err := p.ingestMessage(ctx, householdID, message, raw); err != nil {
					return err
				}
			}
		}
		if history.NextPageToken == "" {
			break
		}
		pageToken = history.NextPageToken
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE gmail_integration SET history_id=$2,status='WATCH_ACTIVE',updated_at=now() WHERE household_id=$1`, householdID, latestHistoryID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='gmail-history',parser_version='1' WHERE id=$1`, payload.SourceEventID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) isTrustedSender(message gmailMessage) bool {
	_, err := mail.ParseAddress(header(message, "From"))
	if err != nil {
		return false
	}
	auth := strings.ToLower(header(message, "Authentication-Results"))
	return strings.Contains(auth, "dkim=pass") && strings.Contains(auth, "dmarc=pass")
}

func (p *Processor) refreshToken(ctx context.Context, householdID string) (string, error) {
	var encrypted []byte
	if err := p.pool.QueryRow(ctx, `SELECT encrypted_refresh_token FROM gmail_integration WHERE household_id=$1`, householdID).Scan(&encrypted); err != nil {
		return "", fmt.Errorf("load Gmail integration: %w", err)
	}
	return p.client.decrypt(householdID, encrypted)
}

func (p *Processor) markIntegrationError(ctx context.Context, householdID string, cause error) error {
	_, updateErr := p.pool.Exec(ctx, `UPDATE gmail_integration SET status='ERROR',updated_at=now() WHERE household_id=$1`, householdID)
	if updateErr != nil {
		return fmt.Errorf("%v; mark Gmail integration error: %w", cause, updateErr)
	}
	return cause
}

type parsedMessage struct {
	fromDomain string
	subject    string
	auth       string
	html       string
	body       string
	emailDate  string
}

func parseMessage(message gmailMessage) (parsedMessage, error) {
	var fromValue string
	result := parsedMessage{}
	for _, header := range message.Payload.Headers {
		switch strings.ToLower(header.Name) {
		case "from":
			fromValue = header.Value
		case "subject":
			result.subject = header.Value
		case "authentication-results":
			result.auth += " " + header.Value
		case "date":
			result.emailDate = header.Value
		}
	}
	address, err := mail.ParseAddress(fromValue)
	if err != nil {
		return parsedMessage{}, fmt.Errorf("invalid Gmail From header")
	}
	result.fromDomain = senderDomain(address.Address)
	result.html = htmlBody(message.Payload.MimeType, message.Payload.Body.Data, message.Payload.Parts)
	result.body = result.html
	if result.body == "" {
		result.body = textBody(message.Payload.MimeType, message.Payload.Body.Data, message.Payload.Parts)
	}
	return result, nil
}

func htmlBody(mimeType, body string, parts []messagePart) string {
	if strings.EqualFold(mimeType, "text/html") && body != "" {
		return decodeBody(body)
	}
	for _, part := range parts {
		if strings.EqualFold(part.MimeType, "text/html") && part.Body.Data != "" {
			return decodeBody(part.Body.Data)
		}
		if nested := htmlBody(part.MimeType, part.Body.Data, part.Parts); nested != "" {
			return nested
		}
	}
	return ""
}

func textBody(mimeType, body string, parts []messagePart) string {
	if strings.EqualFold(mimeType, "text/plain") && body != "" {
		return decodeBody(body)
	}
	for _, part := range parts {
		if strings.EqualFold(part.MimeType, "text/plain") && part.Body.Data != "" {
			return decodeBody(part.Body.Data)
		}
		if nested := textBody(part.MimeType, part.Body.Data, part.Parts); nested != "" {
			return nested
		}
	}
	return ""
}

func decodeBody(value string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(value)
	}
	if err != nil {
		return ""
	}
	return string(decoded)
}

func (p *Processor) ingestMessage(ctx context.Context, householdID string, message gmailMessage, raw []byte) error {
	parsed, err := parseMessage(message)
	if err != nil {
		return nil
	}
	fromAddress := header(message, "From")
	address, addressErr := mail.ParseAddress(fromAddress)
	if addressErr != nil {
		return nil
	}
	listenerID, listenerFound := p.listener(ctx, householdID, address.Address, parsed.auth)
	if !listenerFound {
		return nil
	}

	digest := sha256.Sum256(raw)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var sourceEventID string
	err = tx.QueryRow(ctx, `
		INSERT INTO source_event (household_id,source_type,external_id,received_at,payload_hash,processing_status,parser_name,parser_version)
		VALUES ($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED','gmail-listener','1')
		ON CONFLICT (household_id,source_type,external_id) WHERE external_id IS NOT NULL
		DO UPDATE SET external_id=excluded.external_id RETURNING id`, householdID, message.ID, digest[:]).Scan(&sourceEventID)
	if err != nil {
		return fmt.Errorf("create Gmail source event: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO source_event_payload (source_event_id,payload_json) VALUES ($1,$2::jsonb) ON CONFLICT DO NOTHING`, sourceEventID, string(raw)); err != nil {
		return err
	}
	if listenerFound {
		if _, err := tx.Exec(ctx, `INSERT INTO bank_email_event(source_event_id,listener_id,observed_sender,gmail_message_id,subject,email_date,authentication_results,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, sourceEventID, listenerID, address.Address, message.ID, parsed.subject, parsed.emailDate, parsed.auth, parsed.body); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO job(type,lane,payload_json,max_attempts) SELECT 'PROCESS_BANK_EMAIL','BACKGROUND',jsonb_build_object('source_event_id',$1::uuid,'shadow',false),5 WHERE NOT EXISTS (SELECT 1 FROM job WHERE type='PROCESS_BANK_EMAIL' AND payload_json->>'source_event_id'=$1::text AND status IN ('PENDING','RUNNING','SUCCEEDED'))`, sourceEventID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// listener is the Gmail trust boundary for config-driven bank sources. It
// requires both authentication results before returning a listener; callers
// never send unmatched or unauthenticated mail to the LLM.
func (p *Processor) listener(ctx context.Context, householdID, sender, authentication string) (string, bool) {
	auth := strings.ToLower(authentication)
	if !strings.Contains(auth, "dkim=pass") || !strings.Contains(auth, "dmarc=pass") {
		return "", false
	}
	var id string
	err := p.pool.QueryRow(ctx, `SELECT id FROM bank_email_listener WHERE household_id=$1 AND sender_address=lower($2) AND active`, householdID, strings.TrimSpace(sender)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return id, id != ""
}

func header(message gmailMessage, name string) string {
	for _, item := range message.Payload.Headers {
		if strings.EqualFold(item.Name, name) {
			return item.Value
		}
	}
	return ""
}

func senderDomain(address string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(address)), "@")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}
