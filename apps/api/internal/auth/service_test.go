package auth

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNewSessionTokenIsOpaqueAndHasStableHash(t *testing.T) {
	token, hash, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken() error = %v", err)
	}
	if len(token) < 40 {
		t.Fatalf("token is unexpectedly short: %d", len(token))
	}
	if string(hash) != string(hashToken(token)) {
		t.Fatal("stored token hash does not match token")
	}
}

func TestPrincipalMembershipCardinality(t *testing.T) {
	base := Principal{UserID: "user-1", Email: "user@example.test", DisplayName: "User"}

	zero, err := principalWithMemberships(base, nil)
	if err != nil || zero.HasHousehold || zero.Household != nil {
		t.Fatalf("zero membership principal = %#v, err=%v", zero, err)
	}

	one, err := principalWithMemberships(base, []Membership{{HouseholdID: "household-1", Role: "OWNER"}})
	if err != nil || !one.HasHousehold || one.HouseholdID != "household-1" || one.HouseholdRole != "OWNER" {
		t.Fatalf("one membership principal = %#v, err=%v", one, err)
	}

	_, err = principalWithMemberships(base, []Membership{{HouseholdID: "household-1", Role: "OWNER"}, {HouseholdID: "household-2", Role: "MEMBER"}})
	if !errors.Is(err, ErrTenantInvariantViolation) {
		t.Fatalf("multiple memberships error = %v", err)
	}
}

func TestPrincipalJSONExposesOneHousehold(t *testing.T) {
	principal, err := principalWithMemberships(Principal{UserID: "user-1"}, []Membership{{HouseholdID: "household-1", Role: "MEMBER"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(principal)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if got != `{"userId":"user-1","email":"","displayName":"","isSuperAdmin":false,"household":{"id":"household-1","role":"MEMBER"}}` {
		t.Fatalf("principal JSON = %s", got)
	}
}

func TestTenantFromPrincipalRequiresHousehold(t *testing.T) {
	if _, err := TenantFromPrincipal(Principal{UserID: "user-1"}); !errors.Is(err, ErrHouseholdRequired) {
		t.Fatalf("zero-household error = %v", err)
	}
	tenant, err := TenantFromPrincipal(Principal{UserID: "user-1", HouseholdID: "household-1", HouseholdRole: "OWNER", HasHousehold: true})
	if err != nil || tenant.HouseholdID != "household-1" || tenant.Role != "OWNER" {
		t.Fatalf("tenant = %#v, err=%v", tenant, err)
	}
}
