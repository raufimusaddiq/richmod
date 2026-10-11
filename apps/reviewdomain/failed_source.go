package reviewdomain

import (
	"context"
	"regexp"

	"github.com/jackc/pgx/v5"
)

// A source event (a typed message, a button tap, a bank email) that reaches a
// terminal failure becomes one dismissable item in the Inbox's Tindakan tab. It
// is an action, not a review: the failure is infrastructure state, not a
// household fact question (ADR-048), so there is nothing to decide, only
// something to know and close. Dismissing it finalizes the source event so
// analytics stops counting it as unfinished.
const (
	FailedSourceIntegrationType = "SOURCE_PROCESSING"
	FailedSourceActionType      = "SOURCE_FAILED"
	// FailedSourceInboxLink is where analytics sends the household to see them.
	FailedSourceInboxLink = "/inbox?view=actions"
)

var sourceEventIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsSourceEventID reports whether value is a well-formed source event ID, so a
// malformed one can be skipped before any SQL that would abort a transaction.
func IsSourceEventID(value string) bool { return sourceEventIDPattern.MatchString(value) }

// FailedSource names one source event that will not be processed again.
type FailedSource struct {
	HouseholdID   string
	SourceEventID string
	SourceType    string // source_event.source_type
	Reason        string // INVALID, TRANSPORT_FAILED, VERIFICATION_FAILED, TIMEOUT, ERROR, ...
}

// FailedSourceCopy is the household-facing title and description for a failed
// source: what happened, what it means for their data, and what to do.
func FailedSourceCopy(sourceType, reason string) (title, description string) {
	switch sourceType {
	case "TELEGRAM_TEXT":
		if reason == "TIMEOUT" {
			return "Pesan Telegram belum terjawab",
				"Richmod terlalu lambat memproses pesanmu. Kirim ulang bila masih perlu, lalu tutup tindakan ini."
		}
		return "Pesan Telegram belum terjawab",
			"Richmod tidak berhasil memproses pesanmu. Kirim ulang atau tulis ulang dengan kata lain bila masih perlu, lalu tutup tindakan ini."
	case "TELEGRAM_CALLBACK":
		return "Tombol Telegram belum bisa diproses",
			"Penekanan tombol tidak berhasil diproses. Coba lagi dari Telegram, lalu tutup tindakan ini."
	case "BANK_EMAIL":
		if reason == "INVALID" {
			return "Email bank tidak bisa dibaca",
				"Hasil pembacaan email tidak bisa dipakai, jadi belum ada transaksi yang dicatat. Periksa email aslinya dan catat transaksinya manual bila perlu, lalu tutup tindakan ini."
		}
		return "Email bank gagal dibaca",
			"Pembacaan email gagal karena layanan AI sedang bermasalah, jadi belum ada transaksi yang dicatat. Catat transaksinya manual bila perlu, lalu tutup tindakan ini."
	}
	return "Sumber belum selesai diproses",
		"Satu masukan tidak selesai diproses. Periksa datamu, lalu tutup tindakan ini."
}

// RecordFailedSourceAction creates the Tindakan item for a failed source. It is
// idempotent per source event (the dedupe key), and a missing household or a
// malformed event ID is a no-op, never an SQL error that would abort the
// transaction it runs in.
func RecordFailedSourceAction(ctx context.Context, tx pgx.Tx, f FailedSource) error {
	if f.HouseholdID == "" || !IsSourceEventID(f.SourceEventID) {
		return nil
	}
	title, description := FailedSourceCopy(f.SourceType, f.Reason)
	_, err := tx.Exec(ctx, `
		INSERT INTO integration_action(household_id,integration_type,action_type,status,title,description,dedupe_key,metadata_json)
		VALUES($1,$2,$3,'OPEN',$4,$5,$6::text,jsonb_build_object('source_event_id',$6::text,'source_type',$7::text,'reason',$8::text))
		ON CONFLICT (household_id,integration_type,action_type,dedupe_key) DO NOTHING`,
		f.HouseholdID, FailedSourceIntegrationType, FailedSourceActionType, title, description, f.SourceEventID, f.SourceType, f.Reason)
	return err
}

// IgnoreFailedSource finalizes a source event the household has acknowledged. It
// only moves an event that is still unfinished, so a message that was answered
// in the meantime keeps its PROCESSED state. It reports whether a row changed.
func IgnoreFailedSource(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string) (bool, error) {
	if householdID == "" || !IsSourceEventID(sourceEventID) {
		return false, nil
	}
	tag, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='IGNORED' WHERE id=$1::uuid AND household_id=$2 AND processing_status IN ('RECEIVED','PROCESSING','FAILED')`, sourceEventID, householdID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// TxBeginner is the part of a connection pool the sweep needs.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// SweepFailedSources gives every unfinished source whose processing has
// permanently stopped its dismissable Tindakan item. The terminal-failure hooks
// cover typed messages, button taps and bank email; documents, images, financial
// email, and any hook that itself failed leave a FAILED/RECEIVED source that the
// cycle review counts but the Inbox cannot show. A source qualifies when it has
// no queued or running job and is either FAILED or has a FAILED job; a source
// with no job yet is just new. Idempotent through the per-source dedupe key, so
// it only ever adds items to dismiss. It returns how many items were created.
func SweepFailedSources(ctx context.Context, pool TxBeginner, limit int) (int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT s.id::text,s.household_id::text,s.source_type
		FROM source_event s
		WHERE s.processing_status IN ('RECEIVED','PROCESSING','FAILED')
		  AND NOT EXISTS (SELECT 1 FROM integration_action a WHERE a.household_id=s.household_id AND a.integration_type=$1 AND a.action_type=$2 AND a.dedupe_key=s.id::text)
		  AND NOT EXISTS (SELECT 1 FROM job j WHERE j.payload_json->>'source_event_id'=s.id::text AND j.status IN ('PENDING','RUNNING'))
		  AND (s.processing_status='FAILED' OR EXISTS (SELECT 1 FROM job j WHERE j.payload_json->>'source_event_id'=s.id::text AND j.status='FAILED'))
		ORDER BY s.received_at LIMIT $3`, FailedSourceIntegrationType, FailedSourceActionType, limit)
	if err != nil {
		return 0, err
	}
	var found []FailedSource
	for rows.Next() {
		f := FailedSource{Reason: "ERROR"}
		if err := rows.Scan(&f.SourceEventID, &f.HouseholdID, &f.SourceType); err != nil {
			rows.Close()
			return 0, err
		}
		found = append(found, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, f := range found {
		if err := RecordFailedSourceAction(ctx, tx, f); err != nil {
			return 0, err
		}
	}
	return len(found), tx.Commit(ctx)
}
