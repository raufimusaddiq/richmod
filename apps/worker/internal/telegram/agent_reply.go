package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const genericMutationReply = "Permintaan keuanganmu sudah diproses."

const sourceEvidenceFixesAmountMessage = "Nominal dan merchant transaksi ini berasal dari bukti sumbernya (email bank, bukti penyedia, atau dokumen), jadi tidak bisa diubah dari chat. Kalau memang salah, buka transaksinya di web atau kirim bukti yang benar. Kategori, keterangan, dan tanggalnya tetap bisa kuubah."

var nonMutatingRefusalStatuses = map[string]bool{
	"TRANSFER_PURPOSE_REQUIRED":        true,
	"INCOMPATIBLE_TRANSFER_PURPOSE":    true,
	"INCOMPATIBLE_WEALTH_ACCOUNT":      true,
	"ALLOCATION_TOTAL_MISMATCH":        true,
	"MISSING_ASSET_PURCHASE_FACTS":     true,
	"WEALTH_ACCOUNT_AMBIGUOUS":         true,
	"MISSING_WEALTH_ACCOUNT":           true,
	"ACCOUNT_AMBIGUOUS":                true,
	"TOO_MANY_CANDIDATES":              true,
	"INVALID_AMOUNT":                   true,
	"INVALID_TIME":                     true,
	"INVALID_CHOICE":                   true,
	"NO_PENDING_SALARY_CHOICE":         true,
	"NO_MERCHANT_LEARNING_PENDING":     true,
	"TRANSFER_RECONCILIATION_REQUIRED": true,
	"TRANSACTION_UNDER_REVIEW":         true,
	"IGNORE_NOT_ALLOWED":               true,
	"TRANSFER_CLASSIFICATION_REQUIRED": true,
	"TRANSFER_NOT_CATEGORIZABLE":       true,
	"TRANSACTION_VOIDED":               true,
}

func isNonMutatingRefusal(result agentToolResult) bool {
	return result.Mutation == nil && nonMutatingRefusalStatuses[result.Status]
}

func agentMutationFallback(result agentToolResult) string {
	message, _ := agentMutationReply(result)
	return message
}

func (p *Processor) queueTurnReplyTx(ctx context.Context, tx pgx.Tx, state *agentState, result agentToolResult) error {
	message := clean(agentMutationFallback(result), 4000)
	var err error
	if markup := agentPendingMarkup([]agentToolResult{result}); markup != nil {
		err = enqueueReplyMarkup(ctx, tx, state.Update, message, markup)
	} else {
		err = enqueueReply(ctx, tx, state.Update, message)
	}
	if err != nil {
		return err
	}
	if err := insertTurnTx(ctx, tx, state.HouseholdID, state.SourceEventID, state.Update, "ASSISTANT", message, "", map[string]any{
		"agent_model_phases": state.ModelPhases, "agent_read_calls": state.ReadCalls, "agent_side_effects": state.SideEffects + 1, "reply_with_mutation": true,
	}); err != nil {
		return err
	}
	state.ReplyQueued = true
	return nil
}

func mutationInt(mutation map[string]any, key string) int {
	switch value := mutation[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	}
	return 0
}

func friendlyWhen(at time.Time, precision, period string) string {
	local := at.In(jakartaLocation())
	today := time.Now().In(jakartaLocation())
	day := formatIDDate(local)
	switch local.Format("2006-01-02") {
	case today.Format("2006-01-02"):
		day = "hari ini"
	case today.AddDate(0, 0, -1).Format("2006-01-02"):
		day = "kemarin"
	}
	if precision == "APPROXIMATE" && period != "" {
		return day + ", sekitar " + strings.ToLower(period)
	}
	return day + " " + local.Format("15:04")
}

func transactionRecordedMessage(result agentToolResult) string {
	m := result.Mutation
	amount, _ := m["amount_idr"].(string)
	label, _ := m["merchant"].(string)
	if strings.TrimSpace(label) == "" {
		label, _ = m["description"].(string)
	}
	typ, _ := m["type"].(string)
	if strings.TrimSpace(label) == "" {
		label = "Pengeluaran"
		if typ == "INCOME" {
			label = "Pemasukan"
		}
	}
	line := label
	if amount != "" {
		line += " · Rp" + FormatIDR(amount)
	}
	details := []string{}
	if name, _ := m["category_name"].(string); name != "" {
		details = append(details, name)
	}
	if at, err := time.Parse(time.RFC3339, fmt.Sprint(m["transaction_at"])); err == nil {
		precision, _ := m["time_precision"].(string)
		details = append(details, friendlyWhen(at, precision, approximatePeriodName(at)))
	}
	detail := ""
	if len(details) > 0 {
		detail = "\n" + strings.Join(details, " · ")
	}
	if result.Status == "NEEDS_REVIEW" {
		next := "Tinggal satu langkah di kartu di bawah, ya."
		if inChat, _ := result.Review["card_in_chat"].(bool); !inChat {
			next = "Lengkapi di Kotak Tinjauan, ya."
		}
		return "📝 Kucatat dulu: " + line + detail + "\n" + next
	}
	return "✅ Tercatat: " + line + detail
}

func approximatePeriodName(at time.Time) string {
	hour := at.In(jakartaLocation()).Hour()
	for _, value := range approximateLocalTimes {
		if value.hour == hour {
			return value.period
		}
	}
	return ""
}

func possibleExistingMessage(m map[string]any) string {
	label, _ := m["label"].(string)
	amount, _ := m["amount_idr"].(string)
	if label == "" || amount == "" {
		return "Aku menemukan transaksi serupa. Mau mengubah transaksi yang sudah ada?"
	}
	when := ""
	if at, err := time.Parse(time.RFC3339, fmt.Sprint(m["transaction_at"])); err == nil {
		when = " ke " + formatIDDateTime(at)
	}
	return "Sepertinya ini " + label + " · Rp" + FormatIDR(amount) + " yang sudah tercatat. Mau ubah tanggalnya" + when + " saja, bukan mencatat baru?"
}

func correctionStagedMessage(m map[string]any) string {
	label, _ := m["label"].(string)
	if label == "" {
		label = "Transaksi"
	}
	amount, _ := m["amount_idr"].(string)
	header := "Perubahan untuk " + label
	if amount != "" {
		header += " · Rp" + FormatIDR(amount)
	}
	changes := []string{}
	if value, _ := m["new_amount_idr"].(string); value != "" {
		changes = append(changes, "nominal → Rp"+FormatIDR(value))
	}
	if value, _ := m["new_merchant"].(string); value != "" {
		changes = append(changes, "merchant → "+value)
	}
	if value, _ := m["category_name"].(string); value != "" {
		changes = append(changes, "kategori → "+value)
	} else if value, _ := m["category_slug"].(string); value != "" {
		changes = append(changes, "kategori → "+value)
	}
	if value, _ := m["description"].(string); value != "" {
		changes = append(changes, "keterangan → "+value)
	}
	if at, err := time.Parse(time.RFC3339, fmt.Sprint(m["transaction_at"])); err == nil {
		changes = append(changes, "tanggal → "+formatIDDateTime(at))
	}
	body := ""
	for _, change := range changes {
		body += "\n• " + change
	}
	return header + ":" + body + "\n\nPilih tombol konfirmasi kalau sudah benar. Perubahan ini menunggu 30 menit."
}

func (p *Processor) recoverUnansweredTurn(ctx context.Context, sourceEventID, householdID, payloadText, parserName string) error {
	if !strings.HasPrefix(parserName, "telegram") || !strings.Contains(parserName, "agent") {
		return nil
	}
	var update telegramUpdate
	if err := json.Unmarshal([]byte(payloadText), &update); err != nil || update.Message.Chat.ID == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, "telegram_turn:"+sourceEventID+":ASSISTANT"); err != nil {
		return err
	}
	var answered bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM telegram_conversation_turn WHERE source_event_id=$1::uuid AND role='ASSISTANT')
		OR EXISTS(SELECT 1 FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND created_at > now()-interval '1 day'
			AND (payload_json->>'chat_id')::bigint=$2::bigint AND (payload_json->>'reply_to_message_id')::bigint=$3::bigint
			AND NOT (payload_json->>'text')=ANY($4::text[]))`, sourceEventID, update.Message.Chat.ID, update.Message.MessageID, progressNoticeTexts).Scan(&answered); err != nil {
		return err
	}
	if answered {
		return nil
	}
	message := "Pesanmu sudah kuproses, tapi balasannya sempat tertunda. Cek hasilnya di riwayat transaksi atau Kotak Tinjauan di web, ya."
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT public_context_json FROM telegram_conversation_turn WHERE source_event_id=$1::uuid AND role='TOOL' AND public_context_json->>'class'='SIDE_EFFECT' ORDER BY created_at DESC,id DESC LIMIT 1`, sourceEventID).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		var result agentToolResult
		if json.Unmarshal(raw, &result) == nil {
			if reply, ok := agentMutationReply(result); ok {
				message = reply
			}
		}
	}
	if err := enqueueReply(ctx, tx, update, message); err != nil {
		return err
	}
	if err := insertTurnTx(ctx, tx, householdID, sourceEventID, update, "ASSISTANT", message, "", map[string]any{"recovered_reply": true}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const merchantLearningQuestion = "✅ Sudah dicatat. Pakai kategori ini juga untuk transaksi berikutnya dari merchant yang sama? Pilih tombol di bawah, atau balas ya / tidak."

func merchantLearningMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ingat kategori", CallbackData: "review:remember"}, {Text: "Hanya kali ini", CallbackData: "review:once"}}}}
}

func pendingMerchantLearningReviewID(history []agentToolResult) string {
	for index := len(history) - 1; index >= 0; index-- {
		if history[index].Class != agentToolSideEffect {
			continue
		}
		if history[index].Status == "AWAITING_MERCHANT_DECISION" {
			return history[index].merchantLearningReviewID
		}
		return ""
	}
	return ""
}

func agentMutationReply(result agentToolResult) (string, bool) {
	if result.Status == "AWAITING_MERCHANT_DECISION" {
		return merchantLearningQuestion, true
	}
	if result.Status == "RESIDUAL_FACTS_REQUIRED" {
		if missing, ok := missingFields(result.Review); ok {
			return reviewNeedsFactsMessage(missing), true
		}
		return "Tinjauan ini masih memerlukan tanggal transaksi atau kategori yang valid.", true
	}
	if result.Status == "ALREADY_RECORDED_FROM_EVIDENCE" {
		return "Dokumen yang kamu kirim sudah tercatat, jadi tidak dicatat lagi. Ubah transaksi itu, atau sebutkan apa yang berbeda.", true
	}
	if result.Status == "INVALID_CANDIDATE" {
		return "Itu bukan salah satu transaksi yang bisa digabung dengan struk ini, jadi belum ada yang diubah. Sebutkan transaksi yang dimaksud.", true
	}
	if result.Mutation != nil {
		action, _ := result.Mutation["action"].(string)
		amount, _ := result.Mutation["amount_idr"].(string)
		switch action {
		case "TRANSACTION_RECORDED":
			return transactionRecordedMessage(result), true
		case "BANK_FACTS_QUEUED":
			return bankFactsQueuedMessage, true
		case "BANK_REVIEW_IGNORED":
			return "Bukti email bank diabaikan.", true
		case "DUPLICATE_MERGED":
			return "Struk digabung dengan transaksi yang sudah ada, tidak ada transaksi baru.", true
		case "POSSIBLE_EXISTING_TRANSACTION":
			return possibleExistingMessage(result.Mutation), true
		case "TRANSFER_RECORDED":
			if amount != "" {
				return "✅ Transfer Rp" + FormatIDR(amount) + " sudah tercatat.", true
			}
			return "✅ Transfer sudah tercatat.", true
		case "TRANSFER_ALREADY_RECORDED":
			return "Transfer ini sudah tercatat sebelumnya, jadi tidak dicatat lagi.", true
		case "TRANSFER_RECONCILIATION_STAGED":
			return "Transfer ini perlu ditinjau karena ada transaksi yang mungkin sama.", true
		case "TRANSFER_RECONCILIATION_RESOLVED":
			return "Rekonsiliasi transfer sudah diselesaikan.", true
		case "TRANSFER_RECONCILIATION_IGNORED":
			return "Rekonsiliasi transfer sudah diabaikan tanpa mencatatnya sebagai transfer baru.", true
		case "BATCH_STAGED":
			return batchStagedMessage(result.Mutation, "Daftar transaksinya begini:"), true
		case "BATCH_UPDATED":
			if _, ok := result.Mutation["items"]; ok {
				return batchStagedMessage(result.Mutation, "Daftarnya sudah kuperbarui:"), true
			}
			return "Daftar transaksi sudah diperbarui.", true
		case "BATCH_CANCELLED":
			return "Daftar transaksi dibatalkan, tidak ada yang dicatat.", true
		case "BATCH_RECORDED":
			recorded, reviewed, skipped := mutationInt(result.Mutation, "recorded"), mutationInt(result.Mutation, "needs_review"), mutationInt(result.Mutation, "skipped_already_recorded")
			if recorded+reviewed+skipped == 0 {
				return "✅ Semua transaksi dalam daftar sudah tercatat.", true
			}
			return batchOutcomeMessage(recorded, reviewed, skipped), true
		case "CORRECTION_STAGED", "CORRECT_EXISTING_TRANSACTION":
			return correctionStagedMessage(result.Mutation), true
		case "CORRECTION_RESOLVED":
			if confirmed, _ := result.Mutation["confirmed"].(bool); confirmed {
				return "✅ Perubahan transaksi sudah disimpan.", true
			}
			return "Perubahan transaksi dibatalkan.", true
		case "SALARY_CHOICE_RESOLVED":
			choice, _ := result.Mutation["choice"].(string)
			switch choice {
			case "PRIMARY":
				return "Gaji utama sudah disimpan dan menjadi acuan siklus keuangan.", true
			case "ORDINARY":
				return "✅ Gaji sudah disimpan sebagai pemasukan biasa.", true
			case "IGNORE":
				return "Gaji tersebut sudah diabaikan.", true
			}
		case "MERCHANT_LEARNING_RESOLVED":
			if remember, _ := result.Mutation["remember"].(bool); remember {
				return "Kategori merchant sudah disimpan sebagai aturan.", true
			}
			return "Kategori merchant tidak disimpan sebagai aturan.", true
		case "REVIEW_CONFIRMED", "REVIEW_TRANSFER_CLASSIFIED":
			return "✅ Tinjauan sudah selesai dan data keuangan sudah diperbarui.", true
		case "REVIEW_IGNORED":
			return "Tinjauan sudah selesai tanpa mencatatnya sebagai transaksi aktif.", true
		case "REVIEW_DETAIL_SAVED":
			if result.Status == "NEEDS_REVIEW" {
				if missing, ok := missingFields(result.Review); ok {
					return reviewNeedsFactsMessage(missing), true
				}
			}
			return "Detail tinjauan sudah diperbarui.", true
		case "REVIEW_DETAIL_SAVED_AND_CONFIRMED":
			return "✅ Detail tinjauan sudah diperbarui.", true
		case "WEALTH_ACCOUNT_SET":
			return "Akun kekayaan untuk observasi tersebut sudah diperbarui.", true
		case "WEALTH_OBSERVATION_RECORDED_AS_ASSET_PURCHASE":
			return "Observasi kekayaan sudah direklasifikasi sebagai pembelian aset.", true
		case "WEALTH_OBSERVATION_IGNORED":
			return "Observasi kekayaan sudah diabaikan.", true
		case "CYCLE_RESIDUAL_RESOLVED":
			return "Rekonsiliasi sisa siklus gaji sudah diselesaikan.", true
		case "CYCLE_RESIDUAL_REFRESHED":
			return "Nilai sisa siklus gaji berubah. Tinjau nilai terbaru sebelum menyelesaikannya.", true
		case "CYCLE_RESIDUAL_CLOSED":
			return "Rekonsiliasi sisa siklus gaji ditutup karena tidak lagi berlaku.", true
		case "ADD_MISSING_TRANSACTION_IN_TELEGRAM":
			return "Kirim transaksi yang belum tercatat sebagai pesan baru di sini (jangan balas kartu tinjauan). Setelah tersimpan, sisa siklus gaji dihitung ulang; tinjauan tetap terbuka jika masih perlu tindakan.", true
		}
	}

	switch result.Status {
	case "EVIDENCE_DISPUTE_STAGED":
		return "Kartu koreksi sudah dikirim. Tekan “Konfirmasi transaksi berbeda” jika screenshot ini memang transaksi terpisah; belum ada yang dicatat.", true
	case "EVIDENCE_DISPUTE_ALREADY_STAGED":
		return "Koreksi untuk screenshot ini sudah menunggu konfirmasi di kartu sebelumnya. Belum ada yang dicatat.", true
	case "EVIDENCE_DISPUTE_INVALID":
		return "Screenshot ini belum bisa dipisahkan lewat chat: hanya satu baris pengeluaran yang tergabung ke catatan lama, dengan nominal dan tanggal terbaca, yang bisa dikoreksi. Tidak ada yang diubah.", true
	case "EVIDENCE_DISPUTE_UNAUTHORIZED":
		return "Aku tidak bisa memverifikasi akun Telegram-mu di keluarga ini, jadi koreksi tidak dibuat.", true
	case "EVIDENCE_DISPUTE_USE_BUTTONS":
		return "Koreksi screenshot hanya bisa diselesaikan dengan tombol di kartunya (Konfirmasi atau Batalkan). Belum ada yang diubah.", true
	case "AMBIGUOUS_TARGET", "AMBIGUOUS_REVIEW":
		return "Ada beberapa item yang mungkin kamu maksud. Balas pesan itemnya atau pilih item yang sesuai, ya.", true
	case "TARGET_UNAVAILABLE":
		return "Referensi transaksi itu sudah tidak tersedia. Cari transaksinya lagi dulu.", true
	case "MISSING_TARGET":
		return "Transaksi mana yang ingin kamu ubah?", true
	case "MISSING_CHANGE":
		return "Bagian mana dari transaksi itu yang ingin kamu ubah?", true
	case "NO_PENDING_BATCH":
		return "Daftar transaksi itu sudah tidak menunggu konfirmasi: sudah selesai, dibatalkan, atau lewat 30 menit. Kirim ulang daftarnya kalau masih mau dicatat, nanti kubuatkan konfirmasi baru.", true
	case "NO_PENDING_ACTION":
		return "Perubahan itu sudah tidak menunggu konfirmasi: sudah selesai, dibatalkan, atau lewat 30 menit. Sebutkan lagi perubahan yang kamu mau, nanti kubuatkan konfirmasi baru.", true
	case "SOURCE_EVIDENCE_FIXES_AMOUNT":
		return sourceEvidenceFixesAmountMessage, true
	case "NO_PENDING_SALARY_CHOICE":
		return "Tidak ada pilihan gaji aktif yang perlu diselesaikan.", true
	case "NO_MERCHANT_LEARNING_PENDING":
		return "Tidak ada konfirmasi aturan merchant yang aktif.", true
	case "ACCOUNT_AMBIGUOUS":
		return "Aku belum bisa memastikan rekening sumbernya. Sebutkan nama rekening yang lebih spesifik, ya.", true
	case "IGNORE_NOT_ALLOWED":
		return "Tinjauan ini tidak bisa diabaikan. Pilih salah satu jawaban di kartunya.", true
	case "TRANSFER_CLASSIFICATION_REQUIRED":
		return "Ini transfer, bukan pengeluaran, jadi perlu diklasifikasikan dulu: ke rekening sendiri, rekening keluarga, atau investasi. Belum ada yang dicatat.", true
	case "TRANSACTION_UNDER_REVIEW":
		return "Transaksi itu masih menunggu tinjauan, jadi belum bisa diubah dari chat. Selesaikan dulu kartu tinjauannya.", true
	case "TRANSFER_NOT_CATEGORIZABLE":
		return "Transfer tidak punya kategori pengeluaran, jadi kategorinya tidak diubah. Belum ada yang berubah.", true
	case "TRANSACTION_VOIDED":
		return "Transaksi itu sudah dibatalkan, jadi perubahannya tidak disimpan.", true
	case "TRANSFER_PURPOSE_REQUIRED":
		return "Aku belum bisa menentukan jenis transfernya. Sebutkan akun tujuannya (tabungan, investasi, atau utang) atau tulis bahwa ini pindah antar rekening. Belum ada yang dicatat.", true
	case "INCOMPATIBLE_TRANSFER_PURPOSE", "INCOMPATIBLE_WEALTH_ACCOUNT":
		return "Akun kekayaan itu tidak cocok untuk aksi ini, jadi belum ada yang diubah. Sebutkan akun yang sesuai, ya.", true
	case "ALLOCATION_TOTAL_MISMATCH":
		return "Total alokasi belum sama dengan nilai sisa siklus, jadi belum ada yang disimpan. Periksa nominalnya, ya.", true
	case "MISSING_ASSET_PURCHASE_FACTS":
		return "Untuk mencatat pembelian aset aku perlu rekening sumber dan waktu transaksinya. Belum ada yang diubah.", true
	case "TOO_MANY_CANDIDATES":
		return "Ada terlalu banyak transaksi yang mirip, jadi belum ada yang dicatat. Sebutkan jam atau detail lain yang lebih spesifik, ya.", true
	case "INVALID_AMOUNT":
		return "Nominalnya belum valid, jadi belum ada yang dicatat. Tulis nominal rupiah tanpa titik atau koma, misalnya 25000.", true
	case "INVALID_TIME":
		return "Tanggal atau jam transaksinya belum terbaca, jadi belum ada yang dicatat.", true
	case "WEALTH_ACCOUNT_AMBIGUOUS", "MISSING_WEALTH_ACCOUNT":
		return "Aku belum bisa memastikan akun kekayaannya. Sebutkan nama akun yang lebih spesifik, ya.", true
	case "MISSING_REVIEW_DETAIL":
		return "Masih ada detail tinjauan yang perlu kamu lengkapi.", true
	case "MISSING_SEARCH_TEXT":
		return "Transaksi apa yang kamu cari? Sebutkan nama merchant atau keperluannya, ya.", true
	case "MISSING_CATEGORY", "INVALID_CATEGORY":
		return "Kategori itu tidak ada di daftar keluarga ini. Pilih salah satu kategori pengeluaran yang tersedia.", true
	case "BANK_ACCOUNT_REQUIRED":
		return "Pilih rekening untuk email bank ini dari tombol yang dikirim.", true
	case "BANK_ACCOUNT_UNAVAILABLE":
		return "Email bank ini belum terhubung ke rekening, dan belum ada rekening aktif. Tambahkan rekening lebih dulu.", true
	case "MISSING_BANK_FACTS":
		return "Nominal dan waktu transaksi masih perlu dilengkapi.", true
	case "INVALID_PAY_DATE":
		return "Tanggal pembayaran belum terbaca. Tulis tanggalnya, misalnya 25 Agu 2026.", true
	case "INVALID_TRANSACTION_DATE":
		return "Tanggal transaksi belum terbaca, jadi belum ada yang disimpan. Balas kartu tinjauan dengan tanggalnya, misalnya 2026-10-03.", true
	case "TRANSFER_RECONCILIATION_REQUIRED":
		return "Ada transaksi transfer yang mungkin sama. Detailnya perlu ditinjau sebelum observasi kekayaan bisa direklasifikasi.", true
	case "MERGE_TARGET_ACTIVE":
		return "Transaksi ini menjadi tujuan penggabungan yang masih aktif, jadi belum bisa dibatalkan atau diabaikan. Pisahkan penggabungannya dulu di halaman Transaksi.", true
	case "STALE_REVIEW_BINDING", "STALE_MERCHANT_LEARNING_BINDING":
		return "Tinjauan ini sudah berubah atau selesai. Buka atau balas pesan tinjauan terbaru untuk melanjutkan.", true
	}
	return genericMutationReply, false
}

func missingFields(review map[string]any) ([]string, bool) {
	value, ok := review["missing_fields"].([]string)
	return value, ok && len(value) > 0
}
