package settings

import (
	"testing"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestOwnerAndEnumPolicy(t *testing.T) {
	if !owner(auth.Principal{HouseholdRole: "OWNER", HasHousehold: true}) {
		t.Fatal("owner role denied")
	}
	if owner(auth.Principal{HouseholdRole: "MEMBER", HasHousehold: true}) {
		t.Fatal("member role accepted")
	}
	if !oneOf("SPENDING_ONLY", "FULL_LEDGER", "SPENDING_ONLY") || oneOf("INVALID", "BANK") {
		t.Fatal("enum validation failed")
	}
}
