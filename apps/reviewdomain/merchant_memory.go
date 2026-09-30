package reviewdomain

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// LearnedMerchantCategory returns the one household-confirmed, auto-applicable
// category for a merchant name, or "" when none is learned. It is the same
// deterministic rule the worker pipelines apply, shared so the Web Review Inbox
// confirm recalls a learned category instead of asking again (ADR-040).
func LearnedMerchantCategory(ctx context.Context, q pgx.Tx, householdID, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	var categoryID string
	err := q.QueryRow(ctx, `SELECT min(c.id::text)
		FROM merchant_alias ma
		JOIN merchant m ON m.id=ma.normalized_merchant_id AND m.household_id=ma.household_id
		JOIN category c ON c.id=ma.default_category_id AND c.household_id=ma.household_id AND c.active
		WHERE ma.household_id=$1
		  AND lower(regexp_replace(btrim(ma.raw_name),'[[:space:]]+',' ','g'))=lower(regexp_replace(btrim($2),'[[:space:]]+',' ','g'))
		  AND ma.auto_apply AND ma.created_from_user_confirmation
		GROUP BY ma.household_id,lower(regexp_replace(btrim(ma.raw_name),'[[:space:]]+',' ','g'))
		HAVING count(DISTINCT ma.normalized_merchant_id)=1 AND count(DISTINCT ma.default_category_id)=1`, householdID, raw).Scan(&categoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return categoryID, nil
}
