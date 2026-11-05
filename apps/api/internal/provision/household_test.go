package provision

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeValidatesOwnerAndNames(t *testing.T) {
	valid := func() HouseholdInput {
		return HouseholdInput{HouseholdName: " Keluarga Baru ", OwnerName: " Pemilik ", OwnerEmail: " Owner@Example.TEST "}
	}
	in := valid()
	if err := in.Normalize(); err != nil {
		t.Fatalf("valid input: %v", err)
	}
	if in.HouseholdName != "Keluarga Baru" || in.OwnerName != "Pemilik" || in.OwnerEmail != "owner@example.test" {
		t.Fatalf("not normalized: %+v", in)
	}
	// 120 non-ASCII characters fit the database's character limit.
	in = valid()
	in.HouseholdName = strings.Repeat("é", 120)
	if err := in.Normalize(); err != nil {
		t.Fatalf("120-character name: %v", err)
	}
	for name, mutate := range map[string]func(*HouseholdInput){
		"blank household":        func(in *HouseholdInput) { in.HouseholdName = "  " },
		"121-character name":     func(in *HouseholdInput) { in.OwnerName = strings.Repeat("é", 121) },
		"dotless domain":         func(in *HouseholdInput) { in.OwnerEmail = "a@b" },
		"trailing-dot domain":    func(in *HouseholdInput) { in.OwnerEmail = "a@example." },
		"display-name address":   func(in *HouseholdInput) { in.OwnerEmail = "Owner <owner@example.test>" },
		"no at sign":             func(in *HouseholdInput) { in.OwnerEmail = "owner.example.test" },
		"negative Telegram ID":   func(in *HouseholdInput) { in.TelegramUserID = -1 },
		"password set but empty": func(in *HouseholdInput) { in.PasswordSet = true },
	} {
		in := valid()
		mutate(&in)
		if err := in.Normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: Normalize() = %v, want ErrInvalidInput", name, err)
		}
	}
}
