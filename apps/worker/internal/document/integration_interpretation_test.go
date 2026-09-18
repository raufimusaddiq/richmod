package document

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type shadowInterpretationGateway struct {
	classificationTool int
	interpretTools     int
	call               gateway.ToolCall
}

func (g *shadowInterpretationGateway) NativeToolCall(_ context.Context, _ string, _ string, _ any, tools []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	if len(tools) == 6 && tools[0].Name == "interpret_receipt" {
		g.interpretTools = len(tools)
		return g.call, gateway.Metadata{Model: "vision-model"}, nil
	}
	if len(tools) == 1 && tools[0].Name == "classify_financial_document" {
		g.classificationTool++
		return gateway.ToolCall{Name: "classify_financial_document", Arguments: json.RawMessage(`{"document_type":"RECEIPT","confidence":0.98,"reason":"visible"}`)}, gateway.Metadata{Model: "vision-model"}, nil
	}
	return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("unexpected tool set")

}

func TestShadowInterpretationPersistsOnlyShadowRecord(t *testing.T) {
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
	stamp := time.Now().UnixNano()
	var householdID, sourceID, attachmentID, documentID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Shadow interp %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM household WHERE id=$1`, householdID)
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'WEB_IMAGE',$2,now(),$3,'PROCESSING') RETURNING id`, householdID, fmt.Sprintf("shadow:%d", stamp), []byte(fmt.Sprintf("shadow-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',3,1,1,$3) RETURNING id`, householdID, []byte(fmt.Sprintf("hash-%d", stamp)), fmt.Sprintf("test/%d.jpg", stamp)).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status) VALUES($1,$2,$3,'RECEIVED') RETURNING id`, householdID, sourceID, attachmentID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	gatewayStub := &shadowInterpretationGateway{call: gateway.ToolCall{Name: "interpret_receipt", Arguments: typedInterpretationFixture(toolInterpretReceipt)}}
	storage, err := blob.NewLocal(filepath.Join(t.TempDir(), "documents"))
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Put(ctx, fmt.Sprintf("test/%d.jpg", stamp), []byte("img"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	processor := &Processor{pool: pool, gateway: gatewayStub, storage: storage, Interpretation: InterpretationShadow}
	if err = processor.Process(ctx, documentID); err != nil {
		t.Fatal(err)
	}
	var documentStatus, sourceStatus string
	var shadowRows, classifiedRows int
	if err = pool.QueryRow(ctx, `SELECT d.status,s.processing_status,(SELECT count(*) FROM document_extraction WHERE document_id=d.id AND stage='INTERPRETATION_SHADOW'),(SELECT count(*) FROM document_extraction WHERE document_id=d.id AND stage='CLASSIFICATION') FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.id=$1`, documentID).Scan(&documentStatus, &sourceStatus, &shadowRows, &classifiedRows); err != nil {
		t.Fatal(err)
	}
	if documentStatus != "CLASSIFIED" || sourceStatus != "PROCESSED" || shadowRows != 1 || classifiedRows != 1 {
		t.Fatalf("document=%s source=%s shadow=%d classified=%d", documentStatus, sourceStatus, shadowRows, classifiedRows)
	}
	var receiptJobs int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='PROCESS_RECEIPT' AND payload_json->>'document_id'=$1`, documentID).Scan(&receiptJobs); err != nil {
		t.Fatal(err)
	}
	if receiptJobs != 1 {
		t.Fatalf("receipt jobs = %d", receiptJobs)
	}
}
