package document

import (
	"context"
	"log/slog"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/imagethumb"
)

// ensureThumbnail writes <storage_ref>.thumb.jpg beside the original so the
// first grid view never waits on a resize. Best effort and idempotent: an
// existing thumbnail is kept, and a decode or storage failure is logged only,
// never returned, so it cannot fail or retry the document job.
func (p *Processor) ensureThumbnail(ctx context.Context, storageRef string, raw []byte) {
	if p.storage == nil {
		return
	}
	thumbRef := storageRef + ".thumb.jpg"
	if _, err := p.storage.Read(ctx, thumbRef); err == nil {
		return
	}
	thumb, err := imagethumb.JPEG(raw)
	if err != nil {
		p.log().Warn("document thumbnail skipped", "storage_ref", storageRef, "error", err)
		return
	}
	if err := p.storage.Put(ctx, thumbRef, thumb, "image/jpeg"); err != nil {
		p.log().Warn("document thumbnail write failed", "storage_ref", storageRef, "error", err)
	}
}

// BackfillThumbnails writes the missing thumbnails of one household's
// documents; an empty householdID covers every household. Reads are
// household-scoped and re-running it is safe: existing thumbnails are kept.
func (p *Processor) BackfillThumbnails(ctx context.Context, householdID string) error {
	households := []string{householdID}
	if householdID == "" {
		rows, err := p.pool.Query(ctx, `SELECT id FROM household ORDER BY id`)
		if err != nil {
			return err
		}
		households = nil
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			households = append(households, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	for _, household := range households {
		refs, err := p.documentStorageRefs(ctx, household)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			if _, err := p.storage.Read(ctx, ref+".thumb.jpg"); err == nil {
				continue
			}
			raw, err := p.readDocument(ctx, ref)
			if err != nil {
				p.log().Warn("document thumbnail backfill skipped", "storage_ref", ref, "error", err)
				continue
			}
			p.ensureThumbnail(ctx, ref, raw)
		}
	}
	return nil
}

func (p *Processor) documentStorageRefs(ctx context.Context, householdID string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT a.storage_ref FROM document d JOIN attachment a ON a.id=d.attachment_id WHERE d.household_id=$1`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (p *Processor) log() *slog.Logger {
	if p.logger == nil {
		return slog.Default()
	}
	return p.logger
}
