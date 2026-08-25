package ledger

import (
	"context"
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
