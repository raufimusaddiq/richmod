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
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/category"
)

// OwnerInviteTTL matches the dashboard invite lifetime of ADR-021.
const OwnerInviteTTL = 24 * time.Hour

var (
	ErrInvalidInput       = errors.New("household name, owner name and a valid owner email are required")
	ErrUserExists         = errors.New("a user with this email already exists; a new household needs a new owner email")
	ErrTelegramIDLinked   = errors.New("this Telegram user ID is already linked to a household")
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

// Normalize trims and validates the input fields in place.
func (in *HouseholdInput) Normalize() error {
	in.HouseholdName = strings.TrimSpace(in.HouseholdName)
	in.OwnerName = strings.TrimSpace(in.OwnerName)
	in.OwnerEmail = strings.ToLower(strings.TrimSpace(in.OwnerEmail))
	if in.HouseholdName == "" || len(in.HouseholdName) > 120 || in.OwnerName == "" || len(in.OwnerName) > 120 ||
		len(in.OwnerEmail) < 3 || len(in.OwnerEmail) > 254 || !strings.Contains(in.OwnerEmail, "@") || in.TelegramUserID < 0 {
		return ErrInvalidInput
	}
	return nil
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
		if _, err := tx.Exec(ctx, `INSERT INTO telegram_identity (telegram_user_id,household_id,user_id) VALUES ($1,$2,$3)`, in.TelegramUserID, created.ID, created.OwnerUserID); err != nil {
			return Household{}, fmt.Errorf("authorize owner Telegram ID: %w", err)
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

// IssueOwnerInvite creates a single-use dashboard invite for a household owner
// who has not enrolled yet, revoking any earlier pending one. It returns the
// plaintext token (only its SHA-256 is stored) and its expiry.
func IssueOwnerInvite(ctx context.Context, tx pgx.Tx, householdID, ownerUserID, createdBy string, now time.Time) (string, time.Time, error) {
	var enrolled bool
	err := tx.QueryRow(ctx, `SELECT u.password_initialized_at IS NOT NULL
		FROM household_member hm JOIN "user" u ON u.id=hm.user_id AND u.active
		WHERE hm.household_id=$1 AND hm.user_id=$2 AND hm.role='OWNER' AND hm.active
		FOR UPDATE OF hm`, householdID, ownerUserID).Scan(&enrolled)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, ErrOwnerNotFound
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("load household owner: %w", err)
	}
	if enrolled {
		return "", time.Time{}, ErrOwnerAlreadyActive
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate invite token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	expires := now.Add(OwnerInviteTTL)
	if _, err := tx.Exec(ctx, `UPDATE dashboard_account_invite SET status='REVOKED',revoked_at=now() WHERE household_id=$1 AND user_id=$2 AND status='PENDING'`, householdID, ownerUserID); err != nil {
		return "", time.Time{}, fmt.Errorf("revoke earlier owner invite: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dashboard_account_invite(household_id,user_id,token_hash,status,expires_at,created_by_user_id) VALUES($1,$2,$3,'PENDING',$4,$5)`, householdID, ownerUserID, digest[:], expires, createdBy); err != nil {
		return "", time.Time{}, fmt.Errorf("create owner invite: %w", err)
	}
	return token, expires, nil
}
