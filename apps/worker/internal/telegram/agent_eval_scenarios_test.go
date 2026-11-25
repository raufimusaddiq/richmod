package telegram

type evalScenario struct {
	name          string
	text          string
	openCards     []map[string]any
	repeated      bool
	previous      string
	tools         []agentToolResult
	mutationMade  bool
	existingFacts bool
	leadTerms     []string
	needsHelp     bool
	needsDigit    bool
	goodReply     string
}

func evalFacts(tool string, facts map[string]any) agentToolResult {
	return agentToolResult{Tool: tool, Class: agentToolRead, Status: "OK", Facts: facts}
}

func evalRecorded(action, amount string) agentToolResult {
	return agentToolResult{Tool: "record_transaction", Class: agentToolSideEffect, Status: "CONFIRMED", Mutation: map[string]any{"action": action, "amount_idr": amount}}
}

var evalCard = []map[string]any{{"topic": "kategori yang belum pasti", "amount_idr": "48000", "description": "kopi"}}

var evalScenarios = []evalScenario{
	{
		name: "spending total", text: "Bulan ini habis berapa buat makan?",
		tools:     []agentToolResult{evalFacts("query_spending", map[string]any{"period": map[string]any{"kind": "SALARY_CYCLE"}, "net_expense_idr": "2400000", "categories": []any{map[string]any{"name": "Makan", "amount_idr": "2400000"}}})},
		leadTerms: []string{"2,4", "2.400.000", "2,4 juta"}, needsDigit: true,
		goodReply: "Siklus ini kamu habis Rp2,4 juta buat makan.\nMau kulihat transaksi terbesarnya?",
	},
	{
		name: "why spending rose", text: "Kenapa pengeluaranku naik?",
		tools:     []agentToolResult{evalFacts("get_cycle_changes", map[string]any{"change_idr": "300000", "drivers": []any{map[string]any{"category": "Makan", "delta_idr": "210000"}, map[string]any{"category": "Transport", "delta_idr": "90000"}}})},
		leadTerms: []string{"300", "naik"}, needsDigit: true,
		goodReply: "Naiknya Rp300 ribu, sebagian besar dari makan (Rp210 ribu) dan sisanya transport.\nMau kulihat transaksi makan yang paling besar?",
	},
	{
		name: "why with codes in facts", text: "kenapa siklus ini kelihatan kurang lengkap datanya?",
		tools:     []agentToolResult{evalFacts("get_cycle_data_quality", map[string]any{"findings": []any{map[string]any{"scope": "CASHFLOW", "kind": "MISSING_SALARY_ANCHOR"}, map[string]any{"scope": "PROCESSING", "kind": "PROCESSING_INCOMPLETE"}}})},
		leadTerms: []string{"gaji", "tanggal", "diproses"},
		goodReply: "Karena tanggal gajian belum diketahui dan ada data yang masih diproses.\nBegitu keduanya lengkap, perbandingannya bisa dibuat.",
	},
	{
		name: "repeated question deeper", text: "Berapa total bulan ini?", repeated: true,
		previous:   "Total pengeluaranmu bulan ini Rp5,1 juta.",
		tools:      []agentToolResult{evalFacts("query_cashflow", map[string]any{"expense_idr": "5100000", "income_idr": "9000000", "largest_counterparty": "Warung Contoh", "largest_counterparty_expense_idr": "900000"})},
		needsDigit: true,
		goodReply:  "Rp5,1 juta keluar dari pemasukan Rp9 juta, sisa Rp3,9 juta.\nPaling besar ke Warung Contoh, Rp900 ribu. Mau kuurai per kategori?",
	},
	{
		name: "card waiting, user answers loosely", text: "ya yang itu kopi", openCards: evalCard,
		goodReply: "Ada satu kartu kopi Rp48.000 yang masih menunggu jawabanmu.\nSelesaikan lewat tombolnya, ya.",
	},
	{
		name: "card waiting, asks status", text: "ada yang belum beres?", openCards: evalCard,
		goodReply: "Ada satu, kopi Rp48.000 yang kategorinya belum pasti.\nPilih kategorinya lewat tombol di kartunya, ya.",
	},
	{
		name: "recording without an amount", text: "tadi beli kopi",
		leadTerms: []string{"berapa", "nominal"},
		goodReply: "Berapa harga kopinya?\nBegitu kamu sebut, langsung kucatat.",
	},
	{
		name: "recording in english without an amount", text: "bought lunch today",
		leadTerms: []string{"how much", "amount"},
		goodReply: "How much was the lunch?\nOnce you tell me, I will log it.",
	},
	{
		name: "mixed language, did it save", text: "thanks, udah masuk kan yang listrik?",
		tools: []agentToolResult{evalFacts("get_transaction_details", map[string]any{"label": "listrik", "amount_idr": "350000", "status": "CONFIRMED"})}, existingFacts: true,
		leadTerms: []string{"sudah", "listrik"}, needsDigit: true,
		goodReply: "Sudah, listrik Rp350.000 tercatat.\nAda yang mau diubah?",
	},
	{
		name: "out of scope joke", text: "ceritain lelucon dong", needsHelp: true,
		goodReply: "Itu di luar urusanku, aku cuma bantu keuangan rumah.\nCoba /help untuk contohnya.",
	},
	{
		name: "out of scope code", text: "write me a python script to scrape a website", needsHelp: true,
		goodReply: "I only help with household finances, so that one is outside what I can do.\nTry /help for examples.",
	},
	{
		name: "investment advice", text: "saham apa yang sebaiknya aku beli?", needsHelp: true,
		goodReply: "Aku tidak bisa kasih saran beli saham, aku cuma bantu mencatat dan membaca keuanganmu.\nCoba /help untuk contohnya.",
	},
	{
		name: "wealth question", text: "berapa total kekayaanku sekarang?",
		tools:      []agentToolResult{evalFacts("query_wealth", map[string]any{"net_worth_idr": "152000000", "accounts": []any{map[string]any{"name": "Tabungan", "kind": "BANK"}, map[string]any{"name": "Reksa", "kind": "MUTUAL_FUND"}}})},
		leadTerms:  []string{"152", "Rp152"},
		needsDigit: true,
		goodReply:  "Total kekayaanmu Rp152 juta, dari tabungan dan reksa dana.\nMau kulihat perubahannya sejak bulan lalu?",
	},
	{
		name: "savings reconciliation", text: "uang sisa bulan ini ke mana?",
		tools:     []agentToolResult{evalFacts("get_savings_reconciliation", map[string]any{"destinations": []any{map[string]any{"purpose": "SAVINGS_TRANSFER", "amount_idr": "1000000"}, map[string]any{"purpose": "ASSET_PURCHASE", "amount_idr": "500000"}}})},
		leadTerms: []string{"1 juta", "1.000.000", "tabungan"}, needsDigit: true,
		goodReply: "Sisa uangmu Rp1,5 juta: Rp1 juta ke tabungan dan Rp500 ribu untuk pembelian aset.\nSudah sesuai catatanmu?",
	},
	{
		name: "nothing found", text: "cari transaksi bengkel",
		tools:     []agentToolResult{evalFacts("search_transactions", map[string]any{"count": 0, "transactions": []any{}})},
		leadTerms: []string{"belum", "tidak", "nggak", "no "},
		goodReply: "Aku belum menemukan transaksi bengkel.\nCoba sebut merchant atau tanggalnya, ya.",
	},
	{
		name: "correction staged", text: "ubah kategorinya jadi transport",
		tools: []agentToolResult{{Tool: "propose_transaction_correction", Class: agentToolSideEffect, Status: "STAGED", Mutation: map[string]any{"action": "CORRECTION_STAGED", "category_name": "Transport"}}}, mutationMade: false,
		leadTerms: []string{"transport"},
		goodReply: "Perubahan ke Transport menunggu konfirmasimu lewat tombol.\nBelum ada yang berubah sampai kamu setuju.",
	},
	{
		name: "refusal needs missing detail", text: "catat bensin",
		tools:     []agentToolResult{{Tool: "record_transaction", Class: agentToolSideEffect, Status: "INVALID_AMOUNT"}},
		leadTerms: []string{"nominal", "berapa"},
		goodReply: "Berapa nominal bensinnya?\nBelum ada yang kucatat.",
	},
	{
		name: "salary cycle term", text: "gaji kapan terakhir masuk?",
		tools:     []agentToolResult{evalFacts("get_cycle_overview", map[string]any{"period": map[string]any{"kind": "SALARY_CYCLE", "start": "2026-10-25"}, "anchor": "PRIMARY_SALARY"})},
		leadTerms: []string{"25", "Okt"}, needsDigit: true,
		goodReply: "Gajimu terakhir masuk 25 Oktober.\nSiklus gaji sekarang dihitung dari tanggal itu.",
	},
	{
		name: "english analysis question", text: "why was this cycle more expensive than last?",
		tools:     []agentToolResult{evalFacts("get_cycle_changes", map[string]any{"change_idr": "450000", "drivers": []any{map[string]any{"category": "Travel", "delta_idr": "400000"}}})},
		leadTerms: []string{"450", "Rp"}, needsDigit: true,
		goodReply: "It was Rp450k higher, almost all from travel (Rp400k).\nWant to see the trips behind it?",
	},
	{
		name: "repeated recording question", text: "udah kecatat belum yang tadi?", repeated: true,
		previous:  "Sudah kucatat.",
		tools:     []agentToolResult{evalFacts("get_transaction_details", map[string]any{"label": "kopi", "amount_idr": "25000", "status": "CONFIRMED", "transaction_at": "2026-11-26T08:10:00+07:00"})},
		leadTerms: []string{"sudah", "kopi"}, needsDigit: true, existingFacts: true,
		goodReply: "Sudah, kopi Rp25.000 tercatat tadi pagi jam 08.10 dan statusnya sudah final.\nMau kuubah sesuatu?",
	},
	{
		name: "evidence mismatch card", text: "kok kartunya muncul lagi?", openCards: []map[string]any{{"topic": "transaksi yang mungkin dobel", "amount_idr": "75000", "description": "makan"}},
		goodReply: "Itu kartu makan Rp75.000 yang mungkin dobel, masih menunggu jawabanmu.\nPilih lewat tombolnya, ya.",
	},
}
