package document

import (
	"context"
	"fmt"

	workerTelegram "github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

type documentLimitError struct {
	pages     int
	pageBytes int64
}

func (e documentLimitError) Error() string {
	return fmt.Sprintf("document has %d pages and %d bytes, above the %d page and %d byte limits", e.pages, e.pageBytes, workerTelegram.MaxAlbumPages, workerTelegram.MaxAlbumBytes)
}

func (documentLimitError) Permanent() bool { return true }

func (p *Processor) enforceDocumentLimits(ctx context.Context, documentID string) error {
	var pages int
	var pageBytes int64
	if err := p.pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(a.byte_size),0) FROM document_page dp JOIN attachment a ON a.id=dp.attachment_id WHERE dp.document_id=$1`, documentID).Scan(&pages, &pageBytes); err != nil {
		return fmt.Errorf("measure document pages: %w", err)
	}
	if pages > workerTelegram.MaxAlbumPages || pageBytes > workerTelegram.MaxAlbumBytes {
		return documentLimitError{pages: pages, pageBytes: pageBytes}
	}
	return nil
}
