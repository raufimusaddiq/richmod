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
	"github.com/raufimusaddiq/richmod/apps/api/internal/blob"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
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
	if err = pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'RECEIPT','EXTRACTED') RETURNING id`, householdID, sourceID, attachmentID).Scan(&documentID); err != nil {
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
	var historicalTransactionID string
	if err = pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',50000,now(),now()) RETURNING id`, householdID).Scan(&historicalTransactionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'DOCUMENT','{"link_status":"SUPERSEDED"}')`, historicalTransactionID, sourceID); err != nil {
		t.Fatal(err)
	}
	storage, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithStorage(pool, storage)
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
	for _, step := range []struct {
		name, status string
		want         bool
	}{
		{"superseded review link", "EXTRACTED", false},
		{"stored aggregate without active transaction", "NEEDS_REVIEW", true},
	} {
		t.Run(step.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `UPDATE transaction_evidence SET metadata_json='{"link_status":"SUPERSEDED"}' WHERE transaction_id=$1`, transactionID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE document SET status=$2 WHERE id=$1`, documentID, step.status); err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			handler.List(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].NeedsReview != step.want || len(items[0].Linked) != 0 {
				t.Fatalf("needsReview/active links do not match %s", step.name)
			}
		})
	}
	t.Run("foreign transaction cannot flag or link this document", func(t *testing.T) {
		var foreignHousehold, foreignTransaction string
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Synthetic other household') RETURNING id`).Scan(&foreignHousehold); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',100,now()) RETURNING id`, foreignHousehold).Scan(&foreignTransaction); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'DOCUMENT')`, foreignTransaction, sourceID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE document SET status='EXTRACTED' WHERE id=$1`, documentID); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		handler.List(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].NeedsReview || len(items[0].Linked) != 0 {
			t.Fatal("foreign transaction leaked into document review state")
		}
	})
}

func TestDocumentListIncludesStagedEvidenceDispute(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := fmt.Sprintf("dispute-document-%d", time.Now().UnixNano())
	var household, user, source, reply, attachment, document, original, proposal string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Synthetic document household') RETURNING id`).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Synthetic owner','unused') RETURNING id`, stamp+"@example.test").Scan(&user))
	_, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSED') RETURNING id`, household, stamp, []byte(stamp)).Scan(&source))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'PROCESSED') RETURNING id`, household, stamp+"-reply", []byte(stamp+"-reply")).Scan(&reply))
	must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',10,1,1,$3) RETURNING id`, household, []byte(stamp), stamp+".png").Scan(&attachment))
	must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'EWALLET_SCREENSHOT','EXTRACTED') RETURNING id`, household, source, attachment).Scan(&document))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',100,now(),now()) RETURNING id`, household).Scan(&original))
	must(pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,transaction_at,confidence,proposal_status,metadata_json) VALUES($1,$2,'EXPENSE',100,now(),1,'MERGED',jsonb_build_object('document_id',$3::text,'date_known',true,'matched_transaction_id',$4::text)) RETURNING id`, household, source, document, original).Scan(&proposal))
	_, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TRANSACTION_SCREENSHOT',jsonb_build_object('proposal_id',$3::text,'document_id',$4::text))`, original, source, proposal, document)
	must(err)
	tx, err := pool.Begin(ctx)
	must(err)
	defer tx.Rollback(ctx)
	staged, err := reviewdomain.StageEvidenceDispute(ctx, tx, reviewdomain.EvidenceDisputeCommand{HouseholdID: household, ActorUserID: user, DocumentID: document, SourceEventID: reply})
	must(err)
	must(tx.Commit(ctx))
	if !staged.Created || staged.Status != "NEEDS_REVIEW" {
		t.Fatalf("dispute stage: created=%v status=%s", staged.Created, staged.Status)
	}
	storage, err := blob.NewLocal(t.TempDir())
	must(err)
	handler := NewHandlerWithStorage(pool, storage)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/documents", nil)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}))
	response := httptest.NewRecorder()
	handler.List(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("document list: status=%d body=%s", response.Code, response.Body.String())
	}
	var items []struct {
		Status      string `json:"status"`
		NeedsReview bool   `json:"needsReview"`
	}
	must(json.Unmarshal(response.Body.Bytes(), &items))
	if len(items) != 1 || items[0].Status != "EXTRACTED" || !items[0].NeedsReview {
		t.Fatalf("staged dispute hidden: items=%d", len(items))
	}
}
