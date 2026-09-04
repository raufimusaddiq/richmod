package emailingress

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	StatusProvisioned = "PROVISIONED"
	StatusActive      = "ACTIVE"
	StatusDisabled    = "DISABLED"
)

var ErrActivationNotReady = errors.New("email ingress transport verification and trusted authserv configuration required")

type Service struct {
	pool             *pgxpool.Pool
	domain           string
	trustedAuthservs []string
	now              func() time.Time
}

type Address struct {
	ID             string     `json:"-"`
	HouseholdID    string     `json:"-"`
	LocalPart      string     `json:"-"`
	Address        string     `json:"address"`
	Status         string     `json:"status"`
	Provider       string     `json:"provider"`
	LastReceivedAt *time.Time `json:"lastReceivedAt"`
}

type deliveryInput struct {
	Signed SignedRequest
	Email  ParsedEmail
	Raw    []byte
}

func NewService(pool *pgxpool.Pool, domain string, trustedAuthservs []string) *Service {
	return &Service{pool: pool, domain: strings.ToLower(strings.TrimSpace(domain)), trustedAuthservs: trustedAuthservs, now: time.Now}
}

func (s *Service) Current(ctx context.Context, householdID string) (Address, bool, error) {
	var out Address
	err := s.pool.QueryRow(ctx, `SELECT id,household_id,local_part,status,provider,last_received_at FROM email_ingress_address WHERE household_id=$1 AND purpose='BANK_EMAIL' AND status IN ('PROVISIONED','ACTIVE') ORDER BY created_at DESC LIMIT 1`, householdID).Scan(&out.ID, &out.HouseholdID, &out.LocalPart, &out.Status, &out.Provider, &out.LastReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Address{}, false, nil
	}
	if err != nil {
		return Address{}, false, err
	}
	out.Address = out.LocalPart + "@" + s.domain
	return out, true, nil
}

func (s *Service) Provision(ctx context.Context, householdID, userID string) (Address, error) {
	if address, found, err := s.Current(ctx, householdID); err != nil || found {
		return address, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Address{}, err
	}
	defer tx.Rollback(ctx)
	var existing Address
	err = tx.QueryRow(ctx, `SELECT id,household_id,local_part,status,provider,last_received_at FROM email_ingress_address WHERE household_id=$1 AND purpose='BANK_EMAIL' AND status IN ('PROVISIONED','ACTIVE') FOR UPDATE`, householdID).Scan(&existing.ID, &existing.HouseholdID, &existing.LocalPart, &existing.Status, &existing.Provider, &existing.LastReceivedAt)
	if err == nil {
		existing.Address = existing.LocalPart + "@" + s.domain
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Address{}, err
	}
	var local string
	for attempt := 0; attempt < 3; attempt++ {
		local, err = randomLocalPart()
		if err != nil {
			return Address{}, err
		}
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO email_ingress_address(household_id,local_part,purpose,provider,status,created_by_user_id) VALUES($1,$2,'BANK_EMAIL','CLOUDFLARE_EMAIL','PROVISIONED',$3) ON CONFLICT DO NOTHING RETURNING id`, householdID, local, userID).Scan(&id)
		if err == nil {
			if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'PROVISION_EMAIL_INGRESS','email_ingress_address',$3,jsonb_build_object('local_part',$4::text,'provider','CLOUDFLARE_EMAIL','status','PROVISIONED'))`, householdID, userID, id, local); err != nil {
				return Address{}, err
			}
			if err = tx.Commit(ctx); err != nil {
				return Address{}, err
			}
			return Address{ID: id, HouseholdID: householdID, LocalPart: local, Address: local + "@" + s.domain, Status: StatusProvisioned, Provider: "CLOUDFLARE_EMAIL"}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Address{}, err
		}
		_ = tx.Rollback(ctx)
		if current, found, currentErr := s.Current(ctx, householdID); currentErr != nil || found {
			return current, currentErr
		}
		return Address{}, fmt.Errorf("provision email ingress address conflict")
	}
	return Address{}, fmt.Errorf("generate unique ingress address")
}

func (s *Service) Rotate(ctx context.Context, householdID, userID string) (Address, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Address{}, err
	}
	defer tx.Rollback(ctx)
	var oldID string
	err = tx.QueryRow(ctx, `SELECT id FROM email_ingress_address WHERE household_id=$1 AND purpose='BANK_EMAIL' AND status IN ('PROVISIONED','ACTIVE') FOR UPDATE`, householdID).Scan(&oldID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Address{}, err
	}
	if oldID != "" {
		if _, err = tx.Exec(ctx, `UPDATE email_ingress_address SET status='DISABLED',disabled_at=now() WHERE id=$1`, oldID); err != nil {
			return Address{}, err
		}
	}
	local, err := randomLocalPart()
	if err != nil {
		return Address{}, err
	}
	var id string
	if err = tx.QueryRow(ctx, `INSERT INTO email_ingress_address(household_id,local_part,purpose,provider,status,created_by_user_id) VALUES($1,$2,'BANK_EMAIL','CLOUDFLARE_EMAIL','PROVISIONED',$3) RETURNING id`, householdID, local, userID).Scan(&id); err != nil {
		return Address{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'ROTATE_EMAIL_INGRESS','email_ingress_address',$3,jsonb_build_object('previous_id',$4::text,'local_part',$5::text))`, householdID, userID, id, oldID, local); err != nil {
		return Address{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Address{}, err
	}
	return Address{ID: id, HouseholdID: householdID, LocalPart: local, Address: local + "@" + s.domain, Status: StatusProvisioned, Provider: "CLOUDFLARE_EMAIL"}, nil
}

func (s *Service) Activate(ctx context.Context, householdID, userID string) error {
	trustedConfigured := false
	for _, value := range s.trustedAuthservs {
		if strings.TrimSpace(value) != "" {
			trustedConfigured = true
			break
		}
	}
	if !trustedConfigured {
		return ErrActivationNotReady
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var addressID string
	if err = tx.QueryRow(ctx, `SELECT id FROM email_ingress_address WHERE household_id=$1 AND purpose='BANK_EMAIL' AND status='PROVISIONED' FOR UPDATE`, householdID).Scan(&addressID); err != nil {
		return err
	}
	var transportVerified bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_ingress_delivery WHERE address_id=$1 AND status='PROVISIONED_RECEIVED')`, addressID).Scan(&transportVerified); err != nil {
		return err
	}
	if !transportVerified {
		return ErrActivationNotReady
	}
	if _, err = tx.Exec(ctx, `UPDATE email_ingress_address SET status='ACTIVE',activated_at=now() WHERE id=$1`, addressID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE gmail_integration SET status='DISCONNECTED',updated_at=now() WHERE household_id=$1 AND status <> 'DISCONNECTED'`, householdID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'ACTIVATE_EMAIL_INGRESS','email_ingress_address',$3,jsonb_build_object('provider','CLOUDFLARE_EMAIL','gmail_status','DISCONNECTED'))`, householdID, userID, addressID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) Deliver(ctx context.Context, in deliveryInput) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	local := strings.Split(in.Signed.Recipient, "@")[0]
	var address Address
	err = tx.QueryRow(ctx, `SELECT id,household_id,local_part,status,provider,last_received_at FROM email_ingress_address WHERE local_part=$1 FOR UPDATE`, local).Scan(&address.ID, &address.HouseholdID, &address.LocalPart, &address.Status, &address.Provider, &address.LastReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	address.Address = address.LocalPart + "@" + s.domain
	var alreadyDelivered bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_ingress_delivery WHERE address_id=$1 AND content_sha256=$2)`, address.ID, in.Signed.ContentHash[:]).Scan(&alreadyDelivered); err != nil {
		return err
	}
	if alreadyDelivered {
		_, err = tx.Exec(ctx, `UPDATE email_ingress_address SET last_received_at=now() WHERE id=$1`, address.ID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	status, reason := "", ""
	if address.Status == StatusProvisioned {
		status = "PROVISIONED_RECEIVED"
	}
	if address.Status == StatusDisabled {
		return tx.Commit(ctx)
	}
	if address.Status == StatusActive {
		if !authTrusted(in.Email.AuthenticationResults+"\n"+in.Email.ARCAuthenticationResults, s.trustedAuthservs) {
			status, reason = "IGNORED_AUTH", "UNTRUSTED_AUTHENTICATION"
		} else {
			var listenerID string
			err = tx.QueryRow(ctx, `SELECT id FROM bank_email_listener WHERE household_id=$1 AND sender_address=$2 AND active`, address.HouseholdID, in.Email.Sender).Scan(&listenerID)
			if errors.Is(err, pgx.ErrNoRows) {
				status, reason = "IGNORED_UNMATCHED", "UNMATCHED_SENDER"
			} else if err != nil {
				return err
			} else {
				if err := s.persistActive(ctx, tx, address, listenerID, in); err != nil {
					return err
				}
				return tx.Commit(ctx)
			}
		}
	}
	if status == "" {
		return nil
	}
	if err = s.persistDelivery(ctx, tx, address, "", in, status, reason, ""); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE email_ingress_address SET last_received_at=now() WHERE id=$1`, address.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) persistActive(ctx context.Context, tx pgx.Tx, address Address, listenerID string, in deliveryInput) error {
	externalID := "rfc822:" + strings.TrimSpace(in.Email.MessageID)
	if strings.TrimSpace(in.Email.MessageID) == "" {
		externalID = "sha256:" + hex.EncodeToString(in.Signed.ContentHash[:])
	}
	var sourceID string
	err := tx.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,raw_payload_ref,payload_hash,processing_status,parser_name,parser_version) VALUES($1,'BANK_EMAIL',$2,now(),$3,$4,'RECEIVED','cloudflare-email-ingress','1') ON CONFLICT DO NOTHING RETURNING id`, address.HouseholdID, externalID, "r2://richmod-email-raw/"+in.Signed.ObjectKey, in.Signed.ContentHash[:]).Scan(&sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.persistDelivery(ctx, tx, address, listenerID, in, "DUPLICATE", "DUPLICATE_PAYLOAD", ""); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE email_ingress_address SET last_received_at=now() WHERE id=$1`, address.ID); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	body := in.Email.HTMLBody
	if body == "" {
		body = in.Email.TextBody
	}
	metadata, _ := json.Marshal(map[string]any{"transport": "cloudflare_email", "objectKey": in.Signed.ObjectKey, "envelopeFrom": in.Signed.EnvelopeFrom, "internetMessageID": in.Email.MessageID, "contentSHA256": hex.EncodeToString(in.Signed.ContentHash[:])})
	if _, err = tx.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(metadata)); err != nil {
		return err
	}
	messageID := in.Email.MessageID
	if messageID == "" {
		messageID = "sha256:" + hex.EncodeToString(in.Signed.ContentHash[:])
	}
	if _, err = tx.Exec(ctx, `INSERT INTO bank_email_event(source_event_id,listener_id,observed_sender,message_id,subject,email_date,authentication_results,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, sourceID, listenerID, in.Email.Sender, messageID, in.Email.Subject, in.Email.Date, in.Email.AuthenticationResults+"\n"+in.Email.ARCAuthenticationResults, visibleHTML(body)); err != nil {
		return err
	}
	if err = s.persistDelivery(ctx, tx, address, listenerID, in, "INGESTED", "", sourceID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('PROCESS_BANK_EMAIL',jsonb_build_object('source_event_id',$1::uuid,'shadow',false))`, sourceID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE email_ingress_address SET last_received_at=now() WHERE id=$1`, address.ID); err != nil {
		return err
	}
	return nil
}

func (s *Service) persistDelivery(ctx context.Context, tx pgx.Tx, address Address, listenerID string, in deliveryInput, status, reason, sourceID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO email_ingress_delivery(address_id,household_id,listener_id,source_event_id,provider,object_key,content_sha256,raw_size,envelope_from,observed_sender,internet_message_id,subject,email_date,authentication_results,arc_authentication_results,status,reason_code,received_at) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,'CLOUDFLARE_EMAIL',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NULLIF($16,''),now()) ON CONFLICT DO NOTHING`, address.ID, address.HouseholdID, listenerID, sourceID, in.Signed.ObjectKey, in.Signed.ContentHash[:], len(in.Raw), in.Signed.EnvelopeFrom, in.Email.Sender, in.Email.MessageID, in.Email.Subject, in.Email.Date, in.Email.AuthenticationResults, in.Email.ARCAuthenticationResults, status, reason)
	return err
}

func (s *Service) mustCurrent(ctx context.Context, householdID string) (Address, error) {
	address, found, err := s.Current(ctx, householdID)
	if err != nil || !found {
		return Address{}, fmt.Errorf("load current ingress address: %w", err)
	}
	return address, nil
}
func randomLocalPart() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "h_" + hex.EncodeToString(value[:]), nil
}
