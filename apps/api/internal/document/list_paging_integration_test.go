package document

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/blob"
)

func TestDocumentListPagesWithCursor(t *testing.T) {
	t.Parallel()
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
	stamp := uniqueStamp()
	var householdID, userID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Paging %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Paging Owner','unused') RETURNING id`, fmt.Sprintf("document-paging-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	const total = 7
	for i := 0; i < total; i++ {
		var sourceID, attachmentID string
		if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSED') RETURNING id`, householdID, fmt.Sprintf("paging-%d-%d", stamp, i), []byte(fmt.Sprintf("paging-%d-%d", stamp, i))).Scan(&sourceID); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, householdID, []byte(fmt.Sprintf("paging-att-%d-%d", stamp, i)), fmt.Sprintf("%s/%d.png", householdID, i)).Scan(&attachmentID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status,created_at) VALUES($1,$2,$3,'RECEIPT','EXTRACTED',now()-make_interval(mins => $4))`, householdID, sourceID, attachmentID, i/2); err != nil {
			t.Fatal(err)
		}
	}
	storage, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithStorage(pool, storage)
	fetch := func(query string) (*httptest.ResponseRecorder, []struct{ ID string }) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/documents?"+query, nil)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Household: &auth.Membership{HouseholdID: householdID, Role: "OWNER"}}))
		response := httptest.NewRecorder()
		handler.List(response, request)
		var items []struct{ ID string }
		if response.Code == http.StatusOK {
			if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
				t.Fatal(err)
			}
		}
		return response, items
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		query := "limit=3"
		if cursor != "" {
			query += "&cursor=" + url.QueryEscape(cursor)
		}
		response, items := fetch(query)
		if response.Code != http.StatusOK {
			t.Fatalf("page %d status=%d body=%s", page, response.Code, response.Body.String())
		}
		for _, item := range items {
			if seen[item.ID] {
				t.Fatalf("document %s repeated across pages", item.ID)
			}
			seen[item.ID] = true
		}
		cursor = response.Header().Get("X-Next-Cursor")
		if cursor == "" {
			if want := total - 6; page == 2 && len(items) != want {
				t.Fatalf("last page has %d, want %d", len(items), want)
			}
			break
		}
		if len(items) != 3 {
			t.Fatalf("full page has %d items, want 3", len(items))
		}
	}
	if len(seen) != total {
		t.Fatalf("reached %d documents, want %d", len(seen), total)
	}
	if response, _ := fetch(""); response.Code != http.StatusOK || response.Header().Get("X-Next-Cursor") != "" {
		t.Fatalf("a short list must not advertise another page: status=%d cursor=%q", response.Code, response.Header().Get("X-Next-Cursor"))
	}
	for _, bad := range []string{"cursor=not-a-cursor", "limit=0", "limit=101", "limit=abc"} {
		if response, _ := fetch(bad); response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d, want 400", bad, response.Code)
		}
	}
}
