package settings

import (
	"testing"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestOwnerAndEnumPolicy(t *testing.T) {
	if !owner(auth.Principal{Memberships: []auth.Membership{{Role: "OWNER"}}}) {
		t.Fatal("owner role denied")
	}
	if owner(auth.Principal{Memberships: []auth.Membership{{Role: "MEMBER"}}}) {
		t.Fatal("member role accepted")
	}
	if !oneOf("SPENDING_ONLY", "FULL_LEDGER", "SPENDING_ONLY") || oneOf("INVALID", "BANK") {
		t.Fatal("enum validation failed")
	}
}
