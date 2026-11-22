package document

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/blob"
)

func TestReprocessQueuesFailedDocumentOnceAndAudits(t *testing.T) {
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
	var householdID, userID, sourceID, attachmentID, documentID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Reprocess %d", stamp)).Scan(&householdID); err != nil {
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
	if err = pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'EWALLET_SCREENSHOT','EXTRACTED') RETURNING id`, householdID, sourceID, attachmentID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	storage, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithStorage(pool, storage)
	call := func(household, id string) int {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/documents/"+id+"/reprocess", nil)
		request.SetPathValue("id", id)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Household: &auth.Membership{HouseholdID: household, Role: "OWNER"}}))
		response := httptest.NewRecorder()
		handler.Reprocess(response, request)
		return response.Code
	}
	count := func(query string) (n int) {
		if err := pool.QueryRow(ctx, query, documentID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	jobs := `SELECT count(*) FROM job WHERE type='PROCESS_DOCUMENT' AND payload_json->>'document_id'=$1::text AND status='PENDING'`
	if got := call(householdID, documentID); got != http.StatusConflict {
		t.Fatalf("a non-failed document must be refused, got %d", got)
	}
	if _, err = pool.Exec(ctx, `UPDATE document SET status='FAILED' WHERE id=$1`, documentID); err != nil {
		t.Fatal(err)
	}
	var other string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Other %d", stamp)).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if got := call(other, documentID); got != http.StatusNotFound {
		t.Fatalf("a foreign household must not see the document, got %d", got)
	}
	for i := 0; i < 2; i++ {
		if got := call(householdID, documentID); got != http.StatusAccepted {
			t.Fatalf("call %d: status=%d", i, got)
		}
	}
	var reviewID string
	if err = pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,document_id,review_type,status,decision) VALUES($1,$2,$3,'DOCUMENT_EXTRACTION_LOW_CONFIDENCE','OPEN','{"reasonCode":"EXTRACTION_FAILED","allowedActions":["REPROCESS_DOCUMENT","IGNORE"]}'::jsonb) RETURNING id`, householdID, sourceID, documentID).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}
	if got := call(householdID, documentID); got != http.StatusAccepted {
		t.Fatalf("with open review: status=%d", got)
	}
	var reviewStatus, action string
	if err = pool.QueryRow(ctx, `SELECT status,COALESCE(resolution_action,'') FROM review_item WHERE id=$1`, reviewID).Scan(&reviewStatus, &action); err != nil || reviewStatus != "RESOLVED" || action != "REPROCESS_DOCUMENT" {
		t.Fatalf("open review must be resolved by web reprocess: %s %s %v", reviewStatus, action, err)
	}
	if n := count(jobs); n != 1 {
		t.Fatalf("expected exactly one pending job, got %d", n)
	}
	if n := count(`SELECT count(*) FROM audit_log WHERE action='REPROCESS_DOCUMENT' AND after_json->>'document_id'=$1::text`); n != 1 {
		t.Fatalf("expected exactly one audit row, got %d", n)
	}
}
