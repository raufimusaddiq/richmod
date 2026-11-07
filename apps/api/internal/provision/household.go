// Package provision creates households. Both the operator bootstrap command
// and the Super Admin console use it, so a tenant is always born the same way:
// household, a new OWNER user, the optional Telegram identity and the default
// categories in one transaction, audited in the new household (ADR-058,
// ADR-059).
package provision

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raufimusaddiq/richmod/apps/api/internal/category"
)

// OwnerInviteTTL matches the dashboard invite lifetime of ADR-021.
const OwnerInviteTTL = 24 * time.Hour

var (
	ErrInvalidInput       = errors.New("household name, owner name and a valid owner email are required")
	ErrUserExists         = errors.New("a user with this email already exists; a new household needs a new owner email")
	ErrTelegramIDLinked   = errors.New("this Telegram user ID is already bound to a user")
	ErrOwnerAlreadyActive = errors.New("the household owner has already enrolled")
	ErrOwnerNotFound      = errors.New("household owner not found")
)

// HouseholdInput describes a new tenant. The owner is always a new user: an
// existing user is never attached or moved (ADR-035).
type HouseholdInput struct {
	HouseholdName  string
	OwnerName      string
	OwnerEmail     string
	TelegramUserID int64
	// PasswordHash is the owner's password hash. With PasswordSet false the
	// hash is an unusable random value and the owner enrolls through an
	// invite; login refuses an uninitialized password.
	PasswordHash string
	PasswordSet  bool
	// Audit attribution in the new household's audit_log.
	ActorType   string // "SYSTEM" or "USER"
	ActorUserID string // "" means the new owner (bootstrap)
	Action      string
}

type Household struct {
	ID          string
	OwnerUserID string
}

// Normalize trims and validates the input fields in place. Every refusal wraps
// ErrInvalidInput and names the field.
func (in *HouseholdInput) Normalize() error {
	in.HouseholdName = strings.TrimSpace(in.HouseholdName)
	in.OwnerName = strings.TrimSpace(in.OwnerName)
	in.OwnerEmail = strings.ToLower(strings.TrimSpace(in.OwnerEmail))
	// Names are limited in characters, as the database checks them.
	switch {
	case in.HouseholdName == "" || utf8.RuneCountInString(in.HouseholdName) > 120:
		return fmt.Errorf("%w: household name must be 1-120 characters", ErrInvalidInput)
	case in.OwnerName == "" || utf8.RuneCountInString(in.OwnerName) > 120:
		return fmt.Errorf("%w: owner name must be 1-120 characters", ErrInvalidInput)
	case len(in.OwnerEmail) > 254 || !plainEmail(in.OwnerEmail):
		return fmt.Errorf("%w: owner email must be a plain address such as owner@example.com", ErrInvalidInput)
	case in.TelegramUserID < 0:
		return fmt.Errorf("%w: Telegram user ID must be a positive number", ErrInvalidInput)
	case in.PasswordSet && in.PasswordHash == "":
		return fmt.Errorf("%w: password is required", ErrInvalidInput)
	}
	return nil
}

// plainEmail accepts a bare addr-spec whose domain has a dot, so an
// unreachable owner such as "a@b" or "Name <a@b.c>" is refused.
func plainEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" || address.Address != value {
		return false
	}
	at := strings.LastIndex(value, "@")
	return at > 0 && strings.Contains(value[at+1:], ".") && !strings.HasSuffix(value, ".")
}

// CreateHousehold creates the tenant inside tx. Creation is serialized so two
// concurrent requests cannot both pass the uniqueness checks.
func CreateHousehold(ctx context.Context, tx pgx.Tx, in HouseholdInput) (Household, error) {
	if err := in.Normalize(); err != nil {
		return Household{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('richmod.household.create'))`); err != nil {
		return Household{}, fmt.Errorf("lock household creation: %w", err)
	}
	var userExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM "user" WHERE email = $1)`, in.OwnerEmail).Scan(&userExists); err != nil {
		return Household{}, fmt.Errorf("check existing user: %w", err)
	}
	if userExists {
		return Household{}, ErrUserExists
	}
	if in.TelegramUserID > 0 {
		var linked bool
		// A Telegram ID stays bound to its user even after removal (1:1).
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM telegram_identity WHERE telegram_user_id = $1)`, in.TelegramUserID).Scan(&linked); err != nil {
			return Household{}, fmt.Errorf("check Telegram identity: %w", err)
		}
		if linked {
			return Household{}, ErrTelegramIDLinked
		}
	}
	var created Household
	if err := tx.QueryRow(ctx, `INSERT INTO household (name) VALUES ($1) RETURNING id`, in.HouseholdName).Scan(&created.ID); err != nil {
		return Household{}, fmt.Errorf("create household: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO "user" (email, display_name, password_hash, password_initialized_at) VALUES ($1, $2, $3, CASE WHEN $4::boolean THEN now() END) RETURNING id`, in.OwnerEmail, in.OwnerName, in.PasswordHash, in.PasswordSet).Scan(&created.OwnerUserID); err != nil {
		return Household{}, fmt.Errorf("create owner user: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO household_member (household_id, user_id, role) VALUES ($1, $2, 'OWNER')`, created.ID, created.OwnerUserID); err != nil {
		return Household{}, fmt.Errorf("create owner membership: %w", err)
	}
	if in.TelegramUserID > 0 {
		if err := BindTelegramIdentity(ctx, tx, in.TelegramUserID, created.ID, created.OwnerUserID); err != nil {
			return Household{}, err
		}
	}
	if err := category.SeedIndonesianDefaults(ctx, tx, created.ID); err != nil {
		return Household{}, err
	}
	actor := in.ActorUserID
	if actor == "" {
		actor = created.OwnerUserID
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO audit_log (household_id, actor_type, actor_id, action, entity_type, entity_id, after_json)
        VALUES ($1::uuid, $2, $3::uuid, $4, 'household', $1::uuid,
                jsonb_build_object('owner_user_id', $5::uuid, 'created_at', $6::timestamptz, 'telegram_linked', $7::boolean))`,
		created.ID, in.ActorType, actor, in.Action, created.OwnerUserID, time.Now().UTC(), in.TelegramUserID > 0); err != nil {
		return Household{}, fmt.Errorf("audit household creation: %w", err)
	}
	return created, nil
}

// BindTelegramIdentity binds a Telegram account to a household member.
// Bindings are 1:1 and permanent (ADR-059, migration 00088): a Telegram user ID
// belongs to exactly one user, and that user to exactly one household. A
// Telegram ID that is already bound to any user, active or not, is refused with
// ErrTelegramIDLinked; it is never rebound to someone else. The only change
// allowed is reactivating the same user's own inactive identity.
func BindTelegramIdentity(ctx context.Context, tx pgx.Tx, telegramUserID int64, householdID, userID string) error {
	tag, err := tx.Exec(ctx, `INSERT INTO telegram_identity (telegram_user_id,household_id,user_id) VALUES ($1,$2,$3)
		ON CONFLICT (telegram_user_id) DO UPDATE SET active=true,updated_at=now()
		WHERE NOT telegram_identity.active AND telegram_identity.user_id=EXCLUDED.user_id AND telegram_identity.household_id=EXCLUDED.household_id`, telegramUserID, householdID, userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// The user already has a different Telegram ID (one per user).
			return ErrTelegramIDLinked
		}
		return fmt.Errorf("bind Telegram identity: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTelegramIDLinked
	}
	return nil
}

// OwnerInvite is a freshly issued owner enrollment link.
type OwnerInvite struct {
	OwnerUserID string
	OwnerEmail  string
	Token       string
	ExpiresAt   time.Time
}

// IssueOwnerInvite creates a single-use dashboard invite for the active owner
// of a household, revoking any earlier pending one. An owner who has already
// enrolled is only re-invited when reset is true (a forgotten password): the
// caller must ask for it explicitly, and accepting the link replaces the
// password and signs the owner out everywhere.
// The owner row is locked first, so concurrent calls serialize and only the
// last returned link is valid. Only the token's SHA-256 is stored.
func IssueOwnerInvite(ctx context.Context, tx pgx.Tx, householdID, createdBy string, now time.Time, reset bool) (OwnerInvite, error) {
	var invite OwnerInvite
	var enrolled bool
	err := tx.QueryRow(ctx, `SELECT hm.user_id::text,u.email,u.password_initialized_at IS NOT NULL
		FROM household_member hm JOIN "user" u ON u.id=hm.user_id AND u.active
		WHERE hm.household_id::text=$1 AND hm.role='OWNER' AND hm.active
		ORDER BY hm.created_at LIMIT 1
		FOR UPDATE OF hm`, householdID).Scan(&invite.OwnerUserID, &invite.OwnerEmail, &enrolled)
	if errors.Is(err, pgx.ErrNoRows) {
		return OwnerInvite{}, ErrOwnerNotFound
	}
	if err != nil {
		return OwnerInvite{}, fmt.Errorf("load household owner: %w", err)
	}
	if enrolled && !reset {
		return OwnerInvite{}, ErrOwnerAlreadyActive
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return OwnerInvite{}, fmt.Errorf("generate invite token: %w", err)
	}
	invite.Token = base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(invite.Token))
	invite.ExpiresAt = now.Add(OwnerInviteTTL)
	if _, err := tx.Exec(ctx, `UPDATE dashboard_account_invite SET status='REVOKED',revoked_at=now() WHERE household_id::text=$1 AND user_id::text=$2 AND status='PENDING'`, householdID, invite.OwnerUserID); err != nil {
		return OwnerInvite{}, fmt.Errorf("revoke earlier owner invite: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dashboard_account_invite(household_id,user_id,token_hash,status,expires_at,created_by_user_id) VALUES($1::uuid,$2::uuid,$3,'PENDING',$4,$5)`, householdID, invite.OwnerUserID, digest[:], invite.ExpiresAt, createdBy); err != nil {
		return OwnerInvite{}, fmt.Errorf("create owner invite: %w", err)
	}
	return invite, nil
}
