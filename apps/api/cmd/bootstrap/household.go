package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/category"
)

type bootstrapMode int

const (
	// modeFirstOwner is the one-time platform bootstrap: it refuses to run
	// once any owner exists.
	modeFirstOwner bootstrapMode = iota
	// modeAdditionalHousehold creates one more tenant on a bootstrapped
	// platform. It never reuses or moves an existing user (ADR-035): the owner
	// is always a new user, so no membership changes household implicitly.
	modeAdditionalHousehold
)

var (
	errOwnerExists      = errors.New("an owner already exists; bootstrap can run only once (use --new-household to add a household)")
	errNotBootstrapped  = errors.New("no owner exists yet; run the first bootstrap without --new-household")
	errUserExists       = errors.New("a user with this email already exists; a new household needs a new owner email")
	errTelegramIDLinked = errors.New("this Telegram user ID is already linked to a household")
)

type householdInput struct {
	Email          string
	Name           string
	HouseholdName  string
	TelegramUserID int64
	PasswordHash   string
}

func normalizeInput(email, name, householdName string, telegramUserID int64) (householdInput, error) {
	in := householdInput{Email: strings.ToLower(strings.TrimSpace(email)), Name: strings.TrimSpace(name), HouseholdName: strings.TrimSpace(householdName), TelegramUserID: telegramUserID}
	if in.Email == "" || in.Name == "" || in.HouseholdName == "" {
		return householdInput{}, fmt.Errorf("--email, --name, and --household are required")
	}
	if !strings.Contains(in.Email, "@") {
		return householdInput{}, fmt.Errorf("--email must be an email address")
	}
	if in.TelegramUserID < 0 {
		return householdInput{}, fmt.Errorf("--telegram-user-id must be a positive numeric ID")
	}
	return in, nil
}

// createHousehold creates a household, its OWNER (a new user), the optional
// Telegram identity and the default categories in one transaction, and audits
// it in the new household. It returns the new household ID.
func createHousehold(ctx context.Context, pool *pgxpool.Pool, mode bootstrapMode, in householdInput) (string, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	// Serialize household creation so two concurrent runs cannot both pass
	// the checks below.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('richmod.household.create'))`); err != nil {
		return "", fmt.Errorf("lock household creation: %w", err)
	}
	var ownerExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM household_member WHERE role = 'OWNER')`).Scan(&ownerExists); err != nil {
		return "", fmt.Errorf("check existing owner: %w", err)
	}
	switch {
	case mode == modeFirstOwner && ownerExists:
		return "", errOwnerExists
	case mode == modeAdditionalHousehold && !ownerExists:
		return "", errNotBootstrapped
	}
	var userExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM "user" WHERE email = $1)`, in.Email).Scan(&userExists); err != nil {
		return "", fmt.Errorf("check existing user: %w", err)
	}
	if userExists {
		return "", errUserExists
	}
	if in.TelegramUserID > 0 {
		var linked bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM telegram_identity WHERE telegram_user_id = $1)`, in.TelegramUserID).Scan(&linked); err != nil {
			return "", fmt.Errorf("check Telegram identity: %w", err)
		}
		if linked {
			return "", errTelegramIDLinked
		}
	}
	var householdID, userID string
	if err := tx.QueryRow(ctx, `INSERT INTO household (name) VALUES ($1) RETURNING id`, in.HouseholdName).Scan(&householdID); err != nil {
		return "", fmt.Errorf("create household: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO "user" (email, display_name, password_hash, password_initialized_at) VALUES ($1, $2, $3, now()) RETURNING id`, in.Email, in.Name, in.PasswordHash).Scan(&userID); err != nil {
		return "", fmt.Errorf("create owner user: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO household_member (household_id, user_id, role) VALUES ($1, $2, 'OWNER')`, householdID, userID); err != nil {
		return "", fmt.Errorf("create owner membership: %w", err)
	}
	if in.TelegramUserID > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO telegram_identity (telegram_user_id,household_id,user_id) VALUES ($1,$2,$3)`, in.TelegramUserID, householdID, userID); err != nil {
			return "", fmt.Errorf("authorize owner Telegram ID: %w", err)
		}
	}
	if err := category.SeedIndonesianDefaults(ctx, tx, householdID); err != nil {
		return "", err
	}
	action := "BOOTSTRAP_OWNER"
	if mode == modeAdditionalHousehold {
		action = "CREATE_HOUSEHOLD"
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO audit_log (household_id, actor_type, actor_id, action, entity_type, entity_id, after_json)
        VALUES ($1::uuid, 'SYSTEM', $2::uuid, $3, 'household', $1::uuid,
                jsonb_build_object('owner_user_id', $2::uuid, 'created_at', $4::timestamptz))`,
		householdID, userID, action, time.Now().UTC()); err != nil {
		return "", fmt.Errorf("audit bootstrap: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit bootstrap: %w", err)
	}
	return householdID, nil
}
