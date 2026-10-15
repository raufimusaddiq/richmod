package document

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type screenshotPagesGateway struct {
	t        *testing.T
	pages    []string
	category string
	calls    int
}

func (g *screenshotPagesGateway) NativeToolCall(_ context.Context, _ string, _ string, input any, tools []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	g.t.Helper()
	g.calls++
	if len(tools) != 1 || tools[0].Name != "extract_transaction_screenshot" {
		g.t.Fatal("unexpected extraction tool")
	}
	content := input.([]map[string]any)
	if len(content) != len(g.pages)+1 {
		g.t.Fatalf("extraction received %d images; want %d", len(content)-1, len(g.pages))
	}
	at := time.Now().Format(time.RFC3339)
	var rows []screenshotRow
	for i, page := range g.pages {
		if content[i+1]["image_url"] != "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString([]byte(page)) {
			g.t.Fatalf("page %d missing or out of order", i)
		}
		amount := fmt.Sprint(1000 + i)
		rows = append(rows, screenshotRow{Direction: "OUT", Amount: &amount, Currency: "IDR", TransactionAt: &at, Merchant: fmt.Sprintf("Synthetic page %d", i), CategorySlug: &g.category, CategoryConfidence: .99, Confidence: .99})
	}
	args, err := json.Marshal(screenshotExtraction{Transactions: rows, Confidence: .99})
	return gateway.ToolCall{Name: "extract_transaction_screenshot", Arguments: args}, gateway.Metadata{Model: "synthetic"}, err
}

func TestScreenshotExtractionReadsEveryPage(t *testing.T) {
	for _, mode := range []string{"web_multipage", "telegram_album", "legacy_primary", "unreadable_secondary"} {
		t.Run(mode, func(t *testing.T) {
			f := seedScreenshotFixture(t, "Synthetic screenshot pages")
			ctx := context.Background()
			storage, err := blob.NewLocal(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			pages := []string{"synthetic first page", "synthetic second page"}
			if mode == "legacy_primary" {
				pages = pages[:1]
			}
			for i, page := range pages {
				ref := fmt.Sprintf("synthetic/%s/page-%d.jpg", f.documentID, i)
				if mode != "unreadable_secondary" || i == 0 {
					if err := storage.Put(ctx, ref, []byte(page), "image/jpeg"); err != nil {
						t.Fatal(err)
					}
				}
				var attachment string
				if err := f.pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',$3,1,1,$4) RETURNING id`, f.householdID, []byte(page), len(page), ref).Scan(&attachment); err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					if _, err := f.pool.Exec(ctx, `UPDATE document SET attachment_id=$2 WHERE id=$1`, f.documentID, attachment); err != nil {
						t.Fatal(err)
					}
				}
				source := f.sourceID
				if mode == "telegram_album" && i > 0 {
					if err := f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',now(),$2,'PROCESSING') RETURNING id`, f.householdID, []byte(page)).Scan(&source); err != nil {
						t.Fatal(err)
					}
				}
				if mode != "legacy_primary" {
					if _, err := f.pool.Exec(ctx, `INSERT INTO document_page(document_id,source_event_id,attachment_id,page_index) VALUES($1,$2,$3,$4)`, f.documentID, source, attachment, i); err != nil {
						t.Fatal(err)
					}
				}
			}
			gw := &screenshotPagesGateway{t: t, pages: pages, category: f.categorySlug}
			p := &Processor{pool: f.pool, gateway: gw, storage: storage}
			err = p.ProcessScreenshot(ctx, f.documentID)
			if mode == "unreadable_secondary" {
				if err == nil || gw.calls != 0 {
					t.Fatal("missing page must fail before extraction")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var transactions, proposals, linked int
			if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1),(SELECT count(*) FROM transaction_proposal WHERE source_event_id=$2),(SELECT count(*) FROM transaction_evidence WHERE source_event_id=$2 AND metadata_json->>'document_id'=$3)`, f.householdID, f.sourceID, f.documentID).Scan(&transactions, &proposals, &linked); err != nil {
				t.Fatal(err)
			}
			want := len(pages)
			if mode == "unreadable_secondary" {
				want = 0
			}
			if transactions != want || proposals != want || linked != want {
				t.Fatalf("transactions=%d proposals=%d linked=%d want=%d", transactions, proposals, linked, want)
			}
			if mode == "telegram_album" {
				var unfinished int
				if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM document_page dp JOIN source_event s ON s.id=dp.source_event_id WHERE dp.document_id=$1 AND s.processing_status<>'PROCESSED'`, f.documentID).Scan(&unfinished); err != nil {
					t.Fatal(err)
				}
				if unfinished != 0 {
					t.Fatalf("album has %d unfinished sources after successful extraction", unfinished)
				}
			}
		})
	}
}
