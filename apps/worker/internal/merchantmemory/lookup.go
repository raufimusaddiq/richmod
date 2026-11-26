package merchantmemory

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

type Querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Match struct {
	MerchantID string
	CategoryID string
	Slug       string
}

func Lookup(ctx context.Context, q Querier, household, raw string) (*Match, error) {
	match, err := reviewdomain.LookupMerchantMemory(ctx, q, household, raw)
	if err != nil || match == nil {
		return nil, err
	}
	return &Match{MerchantID: match.MerchantID, CategoryID: match.CategoryID, Slug: match.Slug}, nil
}
