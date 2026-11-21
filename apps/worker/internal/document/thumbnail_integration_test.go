package document

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type thumbnailGateway struct {
	storage   *blob.Store
	thumbRef  string
	seenThumb bool
}

func (g *thumbnailGateway) NativeToolCall(ctx context.Context, _ string, _ string, _ any, _ []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	if _, err := g.storage.Read(ctx, g.thumbRef); err == nil {
		g.seenThumb = true
	}
	return gateway.ToolCall{Name: "classify_financial_document", Arguments: json.RawMessage(`{"document_type":"OTHER_FINANCIAL_DOCUMENT","confidence":0.9,"reason":"test"}`)}, gateway.Metadata{Model: "vision-model"}, nil
}

func thumbnailPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value.Set(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 5), B: uint8((x * y) % 251), A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, value); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func seedThumbnailDocument(t *testing.T, ctx context.Context, pool *pgxpool.Pool, storage *blob.Store, stamp int64, raw []byte) (householdID, documentID, ref string) {
	t.Helper()
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Thumbnail doc %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	var sourceID, attachmentID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'WEB_IMAGE',$2,now(),$3,'PROCESSING') RETURNING id`, householdID, fmt.Sprintf("thumbnail:%d", stamp), []byte(fmt.Sprintf("thumbnail-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	ref = fmt.Sprintf("test/thumbnail-%d.png", stamp)
	if err := pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',$3,600,900,$4) RETURNING id`, householdID, []byte(fmt.Sprintf("thash-%d", stamp)), len(raw), ref).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status) VALUES($1,$2,$3,'RECEIVED') RETURNING id`, householdID, sourceID, attachmentID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO document_page(document_id,source_event_id,attachment_id,page_index) VALUES($1,$2,$3,0)`, documentID, sourceID, attachmentID); err != nil {
		t.Fatal(err)
	}
	if err := storage.Put(ctx, ref, raw, "image/png"); err != nil {
		t.Fatal(err)
	}
	return householdID, documentID, ref
}

func TestProcessWritesThumbnailBeforeTheGatewayCall(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	storage, err := blob.NewLocal(filepath.Join(t.TempDir(), "documents"))
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UnixNano()
	householdID, documentID, ref := seedThumbnailDocument(t, ctx, pool, storage, stamp, thumbnailPNG(t, 600, 900))
	defer pool.Exec(ctx, `DELETE FROM household WHERE id=$1`, householdID)
	gw := &thumbnailGateway{storage: storage, thumbRef: ref + ".thumb.jpg"}
	if err := (&Processor{pool: pool, gateway: gw, storage: storage}).Process(ctx, documentID); err != nil {
		t.Fatal(err)
	}
	if !gw.seenThumb {
		t.Fatal("thumbnail was not written before the gateway call")
	}
	thumb, err := storage.Read(ctx, ref+".thumb.jpg")
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(thumb))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 133 || config.Height != 200 {
		t.Fatalf("thumbnail %dx%d does not preserve the 2:3 aspect ratio", config.Width, config.Height)
	}
}

func TestProcessKeepsAnExistingThumbnail(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	storage, err := blob.NewLocal(filepath.Join(t.TempDir(), "documents"))
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UnixNano()
	householdID, documentID, ref := seedThumbnailDocument(t, ctx, pool, storage, stamp, thumbnailPNG(t, 600, 900))
	defer pool.Exec(ctx, `DELETE FROM household WHERE id=$1`, householdID)
	if err := storage.Put(ctx, ref+".thumb.jpg", []byte("existing"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	gw := &thumbnailGateway{storage: storage, thumbRef: ref + ".thumb.jpg"}
	if err := (&Processor{pool: pool, gateway: gw, storage: storage}).Process(ctx, documentID); err != nil {
		t.Fatal(err)
	}
	thumb, err := storage.Read(ctx, ref+".thumb.jpg")
	if err != nil || string(thumb) != "existing" {
		t.Fatalf("existing thumbnail was rewritten: %q %v", thumb, err)
	}
}

func TestUndecodableImageDoesNotFailTheDocumentJob(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	storage, err := blob.NewLocal(filepath.Join(t.TempDir(), "documents"))
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UnixNano()
	householdID, documentID, ref := seedThumbnailDocument(t, ctx, pool, storage, stamp, []byte("not an image"))
	defer pool.Exec(ctx, `DELETE FROM household WHERE id=$1`, householdID)
	gw := &thumbnailGateway{storage: storage, thumbRef: ref + ".thumb.jpg"}
	if err := (&Processor{pool: pool, gateway: gw, storage: storage}).Process(ctx, documentID); err != nil {
		t.Fatalf("undecodable image failed the job: %v", err)
	}
	if _, err := storage.Read(ctx, ref+".thumb.jpg"); err == nil {
		t.Fatal("undecodable image produced a thumbnail")
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM document WHERE id=$1`, documentID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "CLASSIFIED" {
		t.Fatalf("document status = %s, want CLASSIFIED", status)
	}
}

func TestBackfillThumbnailsSkipsExistingAndStaysHouseholdScoped(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	storage, err := blob.NewLocal(filepath.Join(t.TempDir(), "documents"))
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UnixNano()
	householdID, _, ref := seedThumbnailDocument(t, ctx, pool, storage, stamp, thumbnailPNG(t, 600, 900))
	defer pool.Exec(ctx, `DELETE FROM household WHERE id=$1`, householdID)
	otherHouseholdID, _, otherRef := seedThumbnailDocument(t, ctx, pool, storage, stamp+1, thumbnailPNG(t, 600, 900))
	defer pool.Exec(ctx, `DELETE FROM household WHERE id=$1`, otherHouseholdID)
	if err := storage.Put(ctx, ref+".thumb.jpg", []byte("existing"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	processor := &Processor{pool: pool, storage: storage}
	if err := processor.BackfillThumbnails(ctx, householdID); err != nil {
		t.Fatal(err)
	}
	if thumb, err := storage.Read(ctx, ref+".thumb.jpg"); err != nil || string(thumb) != "existing" {
		t.Fatalf("backfill rewrote an existing thumbnail: %q %v", thumb, err)
	}
	if _, err := storage.Read(ctx, otherRef+".thumb.jpg"); err == nil {
		t.Fatal("backfill crossed the household boundary")
	}
	if err := processor.BackfillThumbnails(ctx, otherHouseholdID); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Read(ctx, otherRef+".thumb.jpg"); err != nil {
		t.Fatalf("backfill did not write the missing thumbnail: %v", err)
	}
}
