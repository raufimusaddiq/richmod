package operations

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusRequiresAuthentication(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler(nil).Status(response, httptest.NewRequest(http.MethodGet, "/api/v1/operations/status", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d", response.Code)
	}
}
