package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/financialentity"
)

type transferReconciliationIntent struct {
	accountID, amount, description, purpose, wealthID string
	at                                                time.Time
}

func resolveUniqueAccountHint(ctx context.Context, tx pgx.Tx, householdID, hint string) (string, error) {
	id, err := financialentity.Account(ctx, tx, householdID, hint)
	if err != nil || id == "" {
		return "", fmt.Errorf("account hint is not unique")
	}
	return id, nil
}

func resolveUniqueWealthHint(ctx context.Context, tx pgx.Tx, householdID, hint string) (string, error) {
	id, err := financialentity.WealthAccount(ctx, tx, householdID, hint)
	if err != nil || id == "" {
		return "", fmt.Errorf("wealth hint is not unique")
	}
	return id, nil
}
