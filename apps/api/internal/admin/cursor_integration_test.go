package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// TestAdminPagedEndpointsRejectMalformedCursor covers the pre-existing bug where
// a crafted cursor fell back to page one or reached SQL as ::uuid and 500'd.
// Every admin keyset endpoint must answer 400 with INVALID_CURSOR instead.
func TestAdminPagedEndpointsRejectMalformedCursor(t *testing.T) {
	pool := provisionPool(t)
	ctx := context.Background()
	var householdID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, "cursor-"+time.Now().Format(time.RFC3339Nano)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(pool, false, "responses")
	cases := []struct {
		name  string
		new   func(http.ResponseWriter, *http.Request)
		path  string
		nonID string
	}{
		{"jobs", handler.Jobs, "/api/v1/admin/jobs", "not-a-uuid"},
		{"llmCalls", handler.LLMCalls, "/api/v1/admin/llm/calls", "not-a-uuid"},
		{"logs", handler.Logs, "/api/v1/admin/logs", "not-a-uuid"},
		{"platformAudit", handler.PlatformAudit, "/api/v1/admin/audit/platform", "not-a-number"},
		{"householdAudit", handler.HouseholdAudit, "/api/v1/admin/audit/household?householdId=" + url.QueryEscape(householdID), "not-a-uuid"},
		{"reviewProjections", handler.ReviewOpsProjections, "/api/v1/admin/reviews/projections", "not-a-uuid"},
	}
	// Well-formed base64 that decodes to nonsense, and a well-formed cursor whose
	// id cannot be cast by the endpoint's SQL.
	cursors := func(nonID string) []string {
		return []string{
			"not-a-cursor",
			makeCursor(time.Now(), nonID),
			url.QueryEscape("%%%"),
		}
	}
	for _, tc := range cases {
		for _, cursor := range cursors(tc.nonID) {
			path := tc.path
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			request := httptest.NewRequest(http.MethodGet, path+sep+"cursor="+cursor, nil)
			request = request.WithContext(auth.ContextWithPrincipal(ctx, auth.Principal{UserID: "00000000-0000-0000-0000-000000000000", IsSuperAdmin: true}))
			response := httptest.NewRecorder()
			handler.Require(http.HandlerFunc(tc.new)).ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("%s cursor=%q: status=%d body=%s", tc.name, cursor, response.Code, response.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["error"] != "INVALID_CURSOR" {
				t.Fatalf("%s cursor=%q: body=%s", tc.name, cursor, response.Body.String())
			}
		}
	}
}
