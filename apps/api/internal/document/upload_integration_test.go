package document

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/blob"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

func TestMalformedDocumentIDIsRejectedBeforeQuerying(t *testing.T) {
	handler := &Handler{}
	for name, call := range map[string]http.HandlerFunc{"reprocess": handler.Reprocess, "extraction": handler.Extraction, "pages": handler.Pages, "page": handler.PageContent, "thumbnail": handler.Thumbnail} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/documents/not-a-uuid", nil)
		request.SetPathValue("id", "not-a-uuid")
		request.SetPathValue("index", "0")
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: "u", Household: &auth.Membership{HouseholdID: "h", Role: "OWNER"}}))
		response := httptest.NewRecorder()
		call(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, response.Code)
		}
	}
}

func TestUploadDeduplicatesBlobsAndRefusesDuplicatePages(t *testing.T) {
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
	stamp := testsupport.UniqueStamp()
	var householdID, userID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Upload %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Uploader','unused') RETURNING id`, fmt.Sprintf("upload-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	storage, err := blob.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithStorage(pool, storage)
	upload := func(images ...[]byte) int {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for index, raw := range images {
			part, err := writer.CreateFormFile("file", fmt.Sprintf("page-%d.png", index))
			if err != nil {
				t.Fatal(err)
			}
			_, _ = part.Write(raw)
		}
		_ = writer.Close()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/documents", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Household: &auth.Membership{HouseholdID: householdID, Role: "OWNER"}}))
		response := httptest.NewRecorder()
		handler.Upload(response, request)
		return response.Code
	}
	blobs := func() int {
		count := 0
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				count++
			}
			return nil
		})
		return count
	}
	page := testPNG(t, 12, 8)
	if got := upload(page); got != http.StatusCreated {
		t.Fatalf("first upload = %d", got)
	}
	if got := upload(page); got != http.StatusCreated {
		t.Fatalf("repeat upload = %d", got)
	}
	if got := blobs(); got != 1 {
		t.Fatalf("a repeated page left %d stored blobs, want 1", got)
	}
	other := testPNG(t, 14, 9)
	if got := upload(other, other); got != http.StatusConflict {
		t.Fatalf("the same page twice in one upload = %d, want 409", got)
	}
	if got := blobs(); got != 1 {
		t.Fatalf("a refused upload left %d stored blobs, want 1", got)
	}
}
