package emailingress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestProvisionRequiresOwner(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/email-ingress", nil)
	request = request.WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{UserID: "user", Memberships: []auth.Membership{{HouseholdID: "household", Role: "MEMBER"}}}))
	response := httptest.NewRecorder()
	handler.Integration(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}
