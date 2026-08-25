package settings

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func auditCreate(ctx context.Context, tx pgx.Tx, householdID, userID, entityType, entityID string, after any) error {
	payload, err := json.Marshal(after)
	if err != nil {
		return fmt.Errorf("encode audit payload: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'CREATE',$3,$4,$5::jsonb)`, householdID, userID, entityType, entityID, payload)
	if err != nil {
		return fmt.Errorf("create audit record: %w", err)
	}
	return nil
}
