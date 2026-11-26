package reviewdomain

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func LearnedMerchantCategory(ctx context.Context, q pgx.Tx, householdID, raw string) (string, error) {
	match, err := LookupMerchantMemory(ctx, q, householdID, raw)
	if err != nil || match == nil {
		return "", err
	}
	return match.CategoryID, nil
}
