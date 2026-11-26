package reviewdomain

import (
	"testing"
	"unicode/utf8"
)

func TestNormalizePayeeNameStripsAccountTailAndInstitutionSuffix(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"bank and account tail", "JOHN DOE BANK SYARIAH INDONESIA 1128 5635 31", "JOHN DOE"},
		{"account tail only", "JOHN DOE 1128 5635 31", "JOHN DOE"},
		{"long single account group", "JOHN DOE 1128563531", "JOHN DOE"},
		{"ewallet suffix", "JOHN DOE GOPAY 0812 3456 78", "JOHN DOE"},
		{"plain name", "JOHN DOE", "JOHN DOE"},
		{"institution word without account tail", "SARI PERMATA", "SARI PERMATA"},
		{"institution strip leaves one token", "SARI PERMATA 1234567890", ""},
		{"account tail only leaves one token", "SARI 1234567890", "SARI"},
		{"ewallet word without account tail", "TOKO DANA", "TOKO DANA"},
		{"short store number is not an account", "KOPI KENANGAN 7", "KOPI KENANGAN 7"},
		{"digits only", "1128 5635 31", ""},
		{"institution only", "BANK SYARIAH INDONESIA", "BANK SYARIAH INDONESIA"},
		{"case mapping before suffix", "İSTANBUL ŞİRKET BANK MANDIRI 1234567890", "İSTANBUL ŞİRKET"},
		{"sharp s before suffix", "STRAẞE HAUS BANK BCA 1234567890", "STRAẞE HAUS"},
		{"collapses whitespace", "  JOHN   DOE  ", "JOHN DOE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizePayeeName(tc.raw)
			if got != tc.want {
				t.Fatalf("NormalizePayeeName(%q)=%q want %q", tc.raw, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("NormalizePayeeName(%q) produced invalid UTF-8: %q", tc.raw, got)
			}
		})
	}
}
