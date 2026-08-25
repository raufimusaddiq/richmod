package category

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SeedIndonesianDefaults creates missing default categories for a household.
// It is idempotent and never changes or removes an existing user category.
func SeedIndonesianDefaults(ctx context.Context, tx pgx.Tx, householdID string) error {
	for _, item := range IndonesianDefaults {
		if item.ParentSlug == "" {
			if _, err := tx.Exec(ctx, `
                INSERT INTO category (household_id, name, slug, sort_order)
                VALUES ($1, $2, $3, $4)
                ON CONFLICT DO NOTHING`, householdID, item.Name, item.Slug, item.SortOrder); err != nil {
				return fmt.Errorf("seed root category %q: %w", item.Slug, err)
			}
			continue
		}
		if _, err := tx.Exec(ctx, `
            INSERT INTO category (household_id, parent_id, name, slug, sort_order)
            SELECT $1, id, $3, $4, $5
            FROM category
            WHERE household_id = $1 AND slug = $2 AND parent_id IS NULL
            ON CONFLICT DO NOTHING`, householdID, item.ParentSlug, item.Name, item.Slug, item.SortOrder); err != nil {
			return fmt.Errorf("seed child category %q: %w", item.Slug, err)
		}
	}
	return nil
}
