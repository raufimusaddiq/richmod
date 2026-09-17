package document

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// EvidenceContext is the bounded, model-safe bundle of document evidence. It
// intentionally contains only image pages, the originating caption, source
// timestamps, the household timezone, public category slugs, and public
// merchant/account hints. It never carries canonical database identifiers.
type EvidenceContext struct {
	Pages         []receiptPage
	Caption       string
	FileName      string
	SourceType    string
	MediaType     string
	ReceivedAt    time.Time
	Timezone      string
	Categories    []categoryOption
	MerchantHints []merchantHint
	AccountHints  []string
}

// merchantHint is a public household alias hint. Real merchant identifiers are
// resolved by Go after validation, never returned to the model.
type merchantHint struct{ RawName string }

const householdTimezone = "Asia/Jakarta"

func (p *Processor) loadEvidenceContext(ctx context.Context, documentID, householdID string) (EvidenceContext, error) {
	if p.storage == nil {
		return EvidenceContext{}, fmt.Errorf("document storage is not configured")
	}
	var value EvidenceContext
	value.Timezone = householdTimezone
	var sourceID string
	if err := p.pool.QueryRow(ctx, `SELECT d.source_event_id,COALESCE(s.source_type,''),s.received_at FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, documentID).Scan(&sourceID, &value.SourceType, &value.ReceivedAt); err != nil {
		return EvidenceContext{}, fmt.Errorf("load evidence source: %w", err)
	}
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(payload_json->>'caption',''),COALESCE(payload_json->>'file_name','') FROM source_event_payload WHERE source_event_id=$1`, sourceID).Scan(&value.Caption, &value.FileName)
	if err != nil && err != pgx.ErrNoRows {
		return EvidenceContext{}, fmt.Errorf("load evidence payload: %w", err)
	}
	value.Caption = sanitizeEvidenceText(value.Caption)
	value.FileName = sanitizeEvidenceText(value.FileName)
	pages, err := p.readDocumentPages(ctx, documentID)
	if err != nil {
		return EvidenceContext{}, err
	}
	value.Pages = pages
	if len(pages) > 0 {
		value.MediaType = pages[0].mediaType
	}
	categories, err := p.documentCategories(ctx, householdID)
	if err != nil {
		return EvidenceContext{}, err
	}
	value.Categories = categories
	value.MerchantHints, err = p.merchantHints(ctx, householdID)
	if err != nil {
		return EvidenceContext{}, err
	}
	value.AccountHints, err = p.accountHints(ctx, householdID)
	if err != nil {
		return EvidenceContext{}, err
	}
	return value, nil
}

func (p *Processor) merchantHints(ctx context.Context, householdID string) ([]merchantHint, error) {
	rows, err := p.pool.Query(ctx, `SELECT raw_name FROM merchant_alias WHERE household_id=$1 ORDER BY updated_at DESC LIMIT 50`, householdID)
	if err != nil {
		return nil, fmt.Errorf("load merchant hints: %w", err)
	}
	defer rows.Close()
	hints := make([]merchantHint, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if raw = sanitizeEvidenceText(raw); raw != "" {
			hints = append(hints, merchantHint{RawName: raw})
		}
	}
	return hints, rows.Err()
}

func (p *Processor) accountHints(ctx context.Context, householdID string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT name FROM account WHERE household_id=$1 AND active ORDER BY name LIMIT 50`, householdID)
	if err != nil {
		return nil, fmt.Errorf("load account hints: %w", err)
	}
	defer rows.Close()
	hints := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if name = sanitizeEvidenceText(name); name != "" {
			hints = append(hints, name)
		}
	}
	return hints, rows.Err()
}

// sanitizeEvidenceText removes control characters and caps user-provided
// evidence so untrusted captions or filenames cannot inflate the prompt or
// smuggle terminal control sequences.
func sanitizeEvidenceText(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) <= 500 {
		return value
	}
	return string([]rune(value)[:500])
}

// modelContent renders the interpretation request content. Only safe context
// is included; internal identifiers are never added.
func (e EvidenceContext) modelContent() []map[string]any {
	content := make([]map[string]any, 0, len(e.Pages))
	for _, page := range e.Pages {
		content = append(content, map[string]any{"type": "input_image", "image_url": "data:" + page.mediaType + ";base64," + base64.StdEncoding.EncodeToString(page.raw)})
	}
	return content
}

func (e EvidenceContext) promptText() string {
	var b strings.Builder
	b.WriteString("Interpret this one household finance document. All pages belong to one logical document.\n")
	b.WriteString("Source type: " + e.SourceType + "\n")
	b.WriteString("Received at: " + e.ReceivedAt.In(jakarta()).Format(time.RFC3339) + "\n")
	b.WriteString("Household timezone: " + e.Timezone + "\n")
	if name := sanitizeEvidenceText(e.FileName); name != "" {
		b.WriteString("File name (untrusted data): <evidence>" + name + "</evidence>\n")
	}
	if caption := sanitizeEvidenceText(e.Caption); caption != "" {
		b.WriteString("Caption (untrusted data): <evidence>" + caption + "</evidence>\n")
	}
	slugs := make([]string, 0, len(e.Categories))
	for _, category := range e.Categories {
		slugs = append(slugs, category.Slug)
	}
	if len(slugs) > 0 {
		b.WriteString("Allowed category slugs: " + strings.Join(slugs, ",") + "\n")
	}
	hints := make([]string, 0, len(e.MerchantHints))
	for _, hint := range e.MerchantHints {
		if name := sanitizeEvidenceText(hint.RawName); name != "" {
			hints = append(hints, name)
		}
	}
	if len(hints) > 0 {
		b.WriteString("Known household merchant hints: " + strings.Join(hints, ",") + "\n")
	}
	labels := make([]string, 0, len(e.AccountHints))
	for _, label := range e.AccountHints {
		if name := sanitizeEvidenceText(label); name != "" {
			labels = append(labels, name)
		}
	}
	if len(labels) > 0 {
		b.WriteString("Known household account labels: " + strings.Join(labels, ",") + "\n")
	}
	return b.String()
}
