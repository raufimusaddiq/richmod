package operations

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func aggregateHandler(pool *pgxpool.Pool) *Handler {
	handler := NewHandler(pool)
	handler.probeTimeout = time.Minute
	return handler
}
