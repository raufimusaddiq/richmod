// Package categoryctx builds the household context that a bounded category
// judgment needs: what each offered category means to this household, how this
// household has categorized the same merchant before, and when the purchase
// happened. It only gathers facts; the model still decides and Go still
// resolves the chosen slug to a canonical ID (ADR-038).
package categoryctx

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Querier is the read seam onto PostgreSQL.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// StateKey names the state entry carrying the same-merchant precedent.
const StateKey = "household_history_same_merchant"

// Label renders a category as the household names it: a sub-category carries
// its parent so "Kopi & Camilan" is read inside "Makanan & Minuman".
func Label(name, parentName string) string {
	name, parentName = strings.TrimSpace(name), strings.TrimSpace(parentName)
	if parentName == "" {
		return name
	}
	return parentName + " > " + name
}

// History reports how many earlier confirmed expenses of the same merchant this
// household filed under each offered category. Counts under a category that is
// not offered roll up to its parent when the parent is offered. It returns nil
// when there is no merchant to look up.
func History(ctx context.Context, q Querier, householdID, merchant string, offered []string) (map[string]any, error) {
	merchant = strings.TrimSpace(merchant)
	if merchant == "" {
		return nil, nil
	}
	rows, err := q.Query(ctx, `SELECT c.slug, COALESCE(p.slug,''), count(*)
FROM transaction t
JOIN merchant m ON m.id=t.merchant_id AND m.household_id=t.household_id
JOIN category c ON c.id=t.category_id
LEFT JOIN category p ON p.id=c.parent_id
WHERE t.household_id=$1 AND t.type='EXPENSE' AND t.status='CONFIRMED'
  AND lower(regexp_replace(btrim(m.normalized_name), '[[:space:]]+', ' ', 'g'))=lower(regexp_replace(btrim($2), '[[:space:]]+', ' ', 'g'))
GROUP BY c.slug, p.slug`, householdID, merchant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	offeredSet := make(map[string]bool, len(offered))
	for _, slug := range offered {
		offeredSet[slug] = true
	}
	counts, total := map[string]int{}, 0
	for rows.Next() {
		var slug, parent string
		var n int
		if err := rows.Scan(&slug, &parent, &n); err != nil {
			return nil, err
		}
		total += n
		switch {
		case offeredSet[slug]:
			counts[slug] += n
		case parent != "" && offeredSet[parent]:
			counts[parent] += n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byCategory := make(map[string]any, len(counts))
	for slug, n := range counts {
		byCategory[slug] = n
	}
	return map[string]any{"confirmed_before": total, "categories": byCategory}, nil
}

var householdLocation = func() *time.Location {
	if location, err := time.LoadLocation("Asia/Jakarta"); err == nil {
		return location
	}
	return time.FixedZone("WIB", 7*60*60)
}()

// LocalTime renders the purchase moment in the household timezone, for example
// "Mon 09:36".
func LocalTime(at time.Time) string {
	return at.In(householdLocation).Format("Mon 15:04")
}
