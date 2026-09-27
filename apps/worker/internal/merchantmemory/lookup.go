package merchantmemory

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Match struct {
	MerchantID string
	CategoryID string
	Slug       string
}

// Lookup returns only one household-confirmed, auto-applicable exact alias.
func Lookup(ctx context.Context, q Querier, household, raw string) (*Match, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var match Match
	err := q.QueryRow(ctx, `SELECT min(ma.normalized_merchant_id::text), min(c.id::text), min(c.slug)
		FROM merchant_alias ma
		JOIN merchant m ON m.id=ma.normalized_merchant_id AND m.household_id=ma.household_id
		JOIN category c ON c.id=ma.default_category_id AND c.household_id=ma.household_id AND c.active
		WHERE ma.household_id=$1
		  AND lower(regexp_replace(btrim(ma.raw_name),'[[:space:]]+',' ','g'))=lower(regexp_replace(btrim($2),'[[:space:]]+',' ','g'))
		  AND ma.auto_apply AND ma.created_from_user_confirmation
		GROUP BY ma.household_id,lower(regexp_replace(btrim(ma.raw_name),'[[:space:]]+',' ','g'))
		HAVING count(DISTINCT ma.normalized_merchant_id)=1 AND count(DISTINCT ma.default_category_id)=1`, household, raw).Scan(&match.MerchantID, &match.CategoryID, &match.Slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &match, nil
}
