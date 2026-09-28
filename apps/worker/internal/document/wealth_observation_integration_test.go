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

// wealthDocumentGateway answers the classification call and the bounded wealth
// extraction call so the complete-observation path can be exercised end to end.
type wealthDocumentGateway struct {
	extraction json.RawMessage
}

func (g *wealthDocumentGateway) NativeToolCall(_ context.Context, _ string, _ string, _ any, tools []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	if len(tools) == 1 && tools[0].Name == "classify_financial_document" {
		return gateway.ToolCall{Name: "classify_financial_document", Arguments: json.RawMessage(`{"document_type":"WEALTH_OBSERVATION","confidence":0.99,"reason":"visible balance"}`)}, gateway.Metadata{Model: "vision-model"}, nil
	}
	if len(tools) == 1 && tools[0].Name == "extract_wealth_observation" {
		return gateway.ToolCall{Name: "extract_wealth_observation", Arguments: g.extraction}, gateway.Metadata{Model: "vision-model"}, nil
	}
	return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("unexpected tool set")
}

func seedWealthDocument(t *testing.T, ctx context.Context, pool *pgxpool.Pool, stamp int64) (householdID, documentID, wealthAccountID string) {
	t.Helper()
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Wealth doc %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	var sourceID, attachmentID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'WEB_IMAGE',$2,now(),$3,'PROCESSING') RETURNING id`, householdID, fmt.Sprintf("wealth:%d", stamp), []byte(fmt.Sprintf("wealth-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',3,1,1,$3) RETURNING id`, householdID, []byte(fmt.Sprintf("whash-%d", stamp)), fmt.Sprintf("test/wealth-%d.jpg", stamp)).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,'Bibit Reksadana','Bibit','ASSET','MUTUAL_FUND','INVESTMENT') RETURNING id`, householdID).Scan(&wealthAccountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status) VALUES($1,$2,$3,'RECEIVED') RETURNING id`, householdID, sourceID, attachmentID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	return householdID, documentID, wealthAccountID
}

func TestWealthObservationAppliesWithoutReviewWhenAccountResolves(t *testing.T) {
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
	householdID, documentID, wealthAccountID := seedWealthDocument(t, ctx, pool, stamp)
	defer pool.Exec(ctx, `DELETE FROM household WHERE id=$1`, householdID)
	storage, err := blob.NewLocal(filepath.Join(t.TempDir(), "documents"))
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Put(ctx, fmt.Sprintf("test/wealth-%d.jpg", stamp), []byte("img"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	gw := &wealthDocumentGateway{extraction: json.RawMessage(`{"institution":"Bibit","account_hint":"Reksadana","observed_value_idr":"42700000","quantity":null,"unit":null,"unit_price_idr":null,"observed_date":"2026-09-01","confidence":0.5}`)}
	processor := &Processor{pool: pool, gateway: gw, storage: storage}
	if err = processor.Process(ctx, documentID); err != nil {
		t.Fatal(err)
	}
	var observationStatus, documentStatus, resolvedAccount string
	if err = pool.QueryRow(ctx, `SELECT status,resolved_wealth_account_id::text FROM wealth_observation WHERE document_id=$1`, documentID).Scan(&observationStatus, &resolvedAccount); err != nil {
		t.Fatal(err)
	}
	var reviews int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE wealth_observation_id=(SELECT id FROM wealth_observation WHERE document_id=$1)`, documentID).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM document WHERE id=$1`, documentID).Scan(&documentStatus); err != nil {
		t.Fatal(err)
	}
	if observationStatus != "APPLIED" || resolvedAccount != wealthAccountID || reviews != 0 || documentStatus != "EXTRACTED" {
		t.Fatalf("status=%s account=%s/%s reviews=%d document=%s", observationStatus, resolvedAccount, wealthAccountID, reviews, documentStatus)
	}
}
