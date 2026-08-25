package document

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestDocumentListIncludesSourceExtractionAndReviewLink(t *testing.T) {
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
	var householdID, userID, sourceID, attachmentID, documentID, transactionID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Documents %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Document Owner','unused') RETURNING id`, fmt.Sprintf("document-list-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("document-%d", stamp), []byte(fmt.Sprintf("document-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, householdID, []byte(fmt.Sprintf("attachment-%d", stamp)), fmt.Sprintf("%s/test.png", householdID)).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'RECEIPT','NEEDS_REVIEW') RETURNING id`, householdID, sourceID, attachmentID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO document_extraction(document_id,stage,schema_version,output_json,confidence,validated) VALUES($1,'extract','v1','{"merchant":"TOKO UJI","total":"50000"}',.96,true)`, documentID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',50000,now()) RETURNING id`, householdID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'DOCUMENT')`, transactionID, sourceID); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(pool, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/documents", nil)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}))
	response := httptest.NewRecorder()
	handler.List(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var items []struct {
		ID, SourceType, Confidence string
		Summary                    map[string]any `json:"summary"`
		Linked                     []string       `json:"linkedTransactionIds"`
		NeedsReview                bool           `json:"needsReview"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != documentID || items[0].SourceType != "TELEGRAM_IMAGE" || items[0].Confidence != "0.9600" || len(items[0].Linked) != 1 || items[0].Linked[0] != transactionID || !items[0].NeedsReview || items[0].Summary["merchant"] != "TOKO UJI" {
		t.Fatalf("items=%#v", items)
	}
}
