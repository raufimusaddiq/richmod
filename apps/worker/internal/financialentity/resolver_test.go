package financialentity

import "testing"

func TestNormalizeAndGenericTokenMatch(t *testing.T) {
	if got := Normalize(" JÁGO—Autodebit "); got != "jágo autodebit" {
		t.Fatalf("Normalize() = %q", got)
	}
	if !plausible("Jago Autodebit", "Bank Jago") {
		t.Fatal("generic shared token must be a candidate")
	}
	if plausible("Jago", "Bank BCA") {
		t.Fatal("unrelated entity matched")
	}
}

func TestGenericFinancialWordsNeverCreateACandidate(t *testing.T) {
	for _, pair := range [][2]string{
		{"Tabungan Mandiri", "Tabungan BCA"},
		{"Dompet Digital OVO", "Dompet Digital Dana"},
		{"Reksadana Pasar Uang", "Reksadana Saham Syariah"},
		{"Emas Digital", "Emas Antam"},
		{"Kartu Kredit Visa", "Kartu Kredit Mandiri"},
		{"Savings Account Jago", "Savings BCA"},
	} {
		if plausible(pair[0], pair[1]) {
			t.Errorf("%q matched %q on a generic word", pair[0], pair[1])
		}
	}
	if !plausible("Tabungan Mandiri", "Mandiri Utama") {
		t.Error("a shared institution name must remain a candidate")
	}
}
