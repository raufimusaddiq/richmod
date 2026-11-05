package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/provision"
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
	errUserExists       = provision.ErrUserExists
	errTelegramIDLinked = provision.ErrTelegramIDLinked
)

type householdInput struct {
	Email          string
	Name           string
	HouseholdName  string
	TelegramUserID int64
	PasswordHash   string
}

func normalizeInput(email, name, householdName string, telegramUserID int64) (householdInput, error) {
	in := provision.HouseholdInput{HouseholdName: householdName, OwnerName: name, OwnerEmail: email, TelegramUserID: telegramUserID}
	if err := in.Normalize(); err != nil {
		// Normalize names the rejected field (household name, owner name,
		// owner email or Telegram user ID), matching --household, --name,
		// --email and --telegram-user-id.
		return householdInput{}, err
	}
	return householdInput{Email: in.OwnerEmail, Name: in.OwnerName, HouseholdName: in.HouseholdName, TelegramUserID: in.TelegramUserID}, nil
}

// createHousehold runs the shared provisioning (internal/provision) for the
// operator command and returns the new household ID. The owner's password is
// set directly, so no invite is issued.
func createHousehold(ctx context.Context, pool *pgxpool.Pool, mode bootstrapMode, in householdInput) (string, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	// Serialize with the console path before the owner check below.
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
	action := "BOOTSTRAP_OWNER"
	if mode == modeAdditionalHousehold {
		action = "CREATE_HOUSEHOLD"
	}
	created, err := provision.CreateHousehold(ctx, tx, provision.HouseholdInput{
		HouseholdName: in.HouseholdName, OwnerName: in.Name, OwnerEmail: in.Email, TelegramUserID: in.TelegramUserID,
		PasswordHash: in.PasswordHash, PasswordSet: true, ActorType: "SYSTEM", Action: action,
	})
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit bootstrap: %w", err)
	}
	return created.ID, nil
}
