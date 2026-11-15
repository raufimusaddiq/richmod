package settings

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
)

// Unique indexes that enforce one active sender per household.
const (
	bankListenerSenderUnique = "bank_email_listener_sender_unique"
	senderRouteActiveUnique  = "email_sender_route_active_unique"
)

// isDuplicateSender reports whether err is the active-sender uniqueness
// violation. Any other failure is a real error and must not be reported as a
// duplicate.
func isDuplicateSender(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return pgErr.ConstraintName == bankListenerSenderUnique || pgErr.ConstraintName == senderRouteActiveUnique
}

// writeSenderWriteError answers a failed sender write: 409 only for a real
// duplicate active sender, 500 with the fallback message otherwise.
func writeSenderWriteError(w http.ResponseWriter, err error, fallback string) {
	if isDuplicateSender(err) {
		jsonError(w, http.StatusConflict, "active sender already exists")
		return
	}
	jsonError(w, http.StatusInternalServerError, fallback)
}
