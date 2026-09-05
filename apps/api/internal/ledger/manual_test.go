package ledger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestCreateRejectsInvalidManualTransactionBeforeDatabaseAccess(t *testing.T) {
	handler := NewHandler(nil)
	principal := auth.Principal{Memberships: []auth.Membership{{HouseholdID: "unused"}}}
	for _, input := range []manualInput{
		{Type: "TRANSFER", Amount: "1000"},
		{Type: "EXPENSE", Amount: "0"},
		{Type: "EXPENSE", Amount: "1000.50"},
		{Type: "EXPENSE", Amount: "001000"},
	} {
		if _, err := handler.create(context.Background(), principal, input); err == nil {
			t.Fatalf("create accepted %#v", input)
		}
	}
}

func TestCreateManualTransactionRejectsZeroHouseholdPrincipal(t *testing.T) {
	handler := NewHandler(nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/manual", strings.NewReader(`{"type":"EXPENSE","amount":"1000","description":"Lunch"}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: "zero-household"}))
	response := httptest.NewRecorder()
	handler.CreateManualTransaction(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
