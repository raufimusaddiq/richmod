package category

// DefaultCategory is an initial household category. Categories remain editable
// after seeding; this list only gives deterministic classification a common base.
type DefaultCategory struct {
	Name       string
	Slug       string
	ParentSlug string
	SortOrder  int
}

// IndonesianDefaults intentionally excludes investments and asset categories,
// which are outside the MVP ledger scope.
var IndonesianDefaults = []DefaultCategory{
	{Name: "Makanan & Minuman", Slug: "makanan-minuman", SortOrder: 10},
	{Name: "Makan di Luar", Slug: "makan-di-luar", ParentSlug: "makanan-minuman", SortOrder: 10},
	{Name: "Belanja Bahan Makanan", Slug: "belanja-bahan-makanan", ParentSlug: "makanan-minuman", SortOrder: 20},
	{Name: "Kopi & Camilan", Slug: "kopi-camilan", ParentSlug: "makanan-minuman", SortOrder: 30},
	{Name: "Pesan Antar", Slug: "pesan-antar", ParentSlug: "makanan-minuman", SortOrder: 40},

	{Name: "Rumah Tangga", Slug: "rumah-tangga", SortOrder: 20},
	{Name: "Belanja Rumah Tangga", Slug: "belanja-rumah-tangga", ParentSlug: "rumah-tangga", SortOrder: 10},
	{Name: "Utilitas", Slug: "utilitas", ParentSlug: "rumah-tangga", SortOrder: 20},
	{Name: "Perawatan Rumah", Slug: "perawatan-rumah", ParentSlug: "rumah-tangga", SortOrder: 30},
	{Name: "Perlengkapan Rumah", Slug: "perlengkapan-rumah", ParentSlug: "rumah-tangga", SortOrder: 40},

	{Name: "Transportasi", Slug: "transportasi", SortOrder: 30},
	{Name: "Bahan Bakar", Slug: "bahan-bakar", ParentSlug: "transportasi", SortOrder: 10},
	{Name: "Transportasi Online", Slug: "transportasi-online", ParentSlug: "transportasi", SortOrder: 20},
	{Name: "Parkir & Tol", Slug: "parkir-tol", ParentSlug: "transportasi", SortOrder: 30},
	{Name: "Servis Kendaraan", Slug: "servis-kendaraan", ParentSlug: "transportasi", SortOrder: 40},

	{Name: "Kesehatan", Slug: "kesehatan", SortOrder: 40},
	{Name: "Obat & Perawatan", Slug: "obat-perawatan", ParentSlug: "kesehatan", SortOrder: 10},
	{Name: "Asuransi", Slug: "asuransi", ParentSlug: "kesehatan", SortOrder: 20},

	{Name: "Keluarga", Slug: "keluarga", SortOrder: 50},
	{Name: "Anak", Slug: "anak", ParentSlug: "keluarga", SortOrder: 10},
	{Name: "Pasangan", Slug: "pasangan", ParentSlug: "keluarga", SortOrder: 20},
	{Name: "Orang Tua", Slug: "orang-tua", ParentSlug: "keluarga", SortOrder: 30},

	{Name: "Pendidikan", Slug: "pendidikan", SortOrder: 60},
	{Name: "Belanja Pribadi", Slug: "belanja-pribadi", SortOrder: 70},
	{Name: "Hiburan", Slug: "hiburan", SortOrder: 80},
	{Name: "Langganan", Slug: "langganan", SortOrder: 90},
	{Name: "Perjalanan", Slug: "perjalanan", SortOrder: 100},
	{Name: "Tagihan & Cicilan", Slug: "tagihan-cicilan", SortOrder: 110},
	{Name: "Donasi & Hadiah", Slug: "donasi-hadiah", SortOrder: 120},
	{Name: "Pajak & Biaya", Slug: "pajak-biaya", SortOrder: 130},
	{Name: "Lainnya", Slug: "lainnya", SortOrder: 999},
}
