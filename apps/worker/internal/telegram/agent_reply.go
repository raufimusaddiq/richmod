package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

const genericMutationReply = "Permintaan keuanganmu sudah diproses."
const ambiguousMerchantReply = "Nama merchant itu cocok dengan lebih dari satu tempat, jadi belum ada yang diubah. Sebutkan nama merchant yang lebih spesifik, ya."
const unusableMerchantReply = "Nama merchant tidak bisa dipakai. Tulis nama merchant yang jelas."

func merchantLinkProblemReply(err error) (string, bool) {
	switch {
	case errors.Is(err, reviewdomain.ErrUnusableMerchantName):
		return unusableMerchantReply, true
	case errors.Is(err, reviewdomain.ErrAmbiguousMerchantMatch):
		return ambiguousMerchantReply, true
	}
	return "", false
}

const sourceEvidenceFixesAmountMessage = "Nominal dan merchant transaksi ini berasal dari bukti sumbernya (email bank, bukti penyedia, atau dokumen), jadi tidak bisa diubah dari chat. Kalau memang salah, buka transaksinya di aplikasi atau kirim bukti yang benar. Kategori, keterangan, dan tanggalnya tetap bisa kuubah."

const factsLockedMessage = "Nominal dan merchant transaksi ini tidak bisa diubah dari chat karena transaksinya sudah digabung, berupa gaji, atau jenisnya tidak mendukung. Buka transaksinya di aplikasi untuk melihat pilihannya. Kategori, keterangan, dan tanggalnya tetap bisa kuubah."

var nonMutatingRefusalStatuses = map[string]bool{
	"TRANSFER_PURPOSE_REQUIRED":        true,
	"INCOMPATIBLE_TRANSFER_PURPOSE":    true,
	"INCOMPATIBLE_WEALTH_ACCOUNT":      true,
	"WEALTH_ACCOUNT_AMBIGUOUS":         true,
	"ACCOUNT_AMBIGUOUS":                true,
	"TOO_MANY_CANDIDATES":              true,
	"INVALID_AMOUNT":                   true,
	"INVALID_TIME":                     true,
	"TRANSFER_RECONCILIATION_REQUIRED": true,
	"TRANSACTION_UNDER_REVIEW":         true,
	"TRANSFER_CLASSIFICATION_REQUIRED": true,
	"TRANSFER_NOT_CATEGORIZABLE":       true,
	"TRANSACTION_VOIDED":               true,
	statusFactsLocked:                  true,
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
	line := joinDot(label, IDR(amount))
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
			next = "Lengkapi di " + reviewInboxName + " di aplikasi, ya."
		}
		return "⏳ Kucatat dulu: " + line + detail + "\n" + next
	}
	return "✅ Tersimpan: " + line + detail
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
	return "Sepertinya ini " + joinDot(label, IDR(amount)) + " yang sudah tercatat. Mau ubah tanggalnya" + when + " saja, bukan mencatat baru?"
}

func correctionStagedMessage(m map[string]any) string {
	label, _ := m["label"].(string)
	if label == "" {
		label = "Transaksi"
	}
	amount, _ := m["amount_idr"].(string)
	header := "Perubahan untuk " + joinDot(label, IDR(amount))
	changes := []string{}
	if value, _ := m["new_amount_idr"].(string); value != "" {
		changes = append(changes, "nominal → "+IDR(value))
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
	return header + ":" + body + "\n\nPilih Ya, simpan perubahan kalau sudah benar. Perubahan ini menunggu 30 menit."
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
			AND NOT COALESCE((payload_json->>'notice')::boolean,false))`, sourceEventID, update.Message.Chat.ID, update.Message.MessageID).Scan(&answered); err != nil {
		return err
	}
	if answered {
		return nil
	}
	message := "Pesanmu sudah kuproses, tapi jawabannya sempat tertunda. Cek hasilnya di Transaksi atau " + reviewInboxName + " di aplikasi, ya."
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

func agentMutationReply(result agentToolResult) (string, bool) {
	if result.Status == "ALREADY_RECORDED_FROM_EVIDENCE" {
		return "Dokumen yang kamu kirim sudah tercatat, jadi tidak dicatat lagi. Ubah transaksi itu, atau sebutkan apa yang berbeda.", true
	}
	if result.Mutation != nil {
		action, _ := result.Mutation["action"].(string)
		amount, _ := result.Mutation["amount_idr"].(string)
		switch action {
		case "TRANSACTION_RECORDED":
			return transactionRecordedMessage(result), true
		case "POSSIBLE_EXISTING_TRANSACTION":
			return possibleExistingMessage(result.Mutation), true
		case "TRANSFER_RECORDED":
			return savedLine("Transfer", IDR(amount)), true
		case "TRANSFER_ALREADY_RECORDED":
			return "Transfer ini sudah tercatat sebelumnya, jadi tidak dicatat lagi.", true
		case "TRANSFER_RECONCILIATION_STAGED":
			return "Transfer ini perlu dicek dulu karena ada transaksi yang mungkin sama.", true
		case "BATCH_STAGED":
			return batchStagedMessage(result.Mutation, "Daftar transaksinya begini:"), true
		case "BATCH_UPDATED":
			if _, ok := result.Mutation["items"]; ok {
				return batchStagedMessage(result.Mutation, "Daftarnya sudah kuperbarui:"), true
			}
			return "Daftar transaksi sudah diperbarui.", true
		case "BATCH_CANCELLED":
			return "Daftar transaksi dibatalkan. Tidak ada yang dicatat.", true
		case "BATCH_RECORDED":
			recorded, reviewed, skipped := mutationInt(result.Mutation, "recorded"), mutationInt(result.Mutation, "needs_review"), mutationInt(result.Mutation, "skipped_already_recorded")
			if recorded+reviewed+skipped == 0 {
				return "✅ Semua transaksi dalam daftar sudah tercatat.", true
			}
			return batchOutcomeMessage(recorded, reviewed, skipped), true
		case assetPurchaseStagedAction:
			return assetPurchaseStagedMessage(result.Mutation), true
		case "CORRECTION_STAGED", "CORRECT_EXISTING_TRANSACTION":
			return correctionStagedMessage(result.Mutation), true
		case "CORRECTION_RESOLVED":
			if confirmed, _ := result.Mutation["confirmed"].(bool); confirmed {
				return "✅ Perubahan transaksi sudah disimpan.", true
			}
			return "Perubahan dibatalkan. Tidak ada yang berubah.", true
		}
	}

	switch result.Status {
	case "EVIDENCE_DISPUTE_STAGED":
		return "Kartu koreksi sudah dikirim. Pilih Ya, transaksi berbeda kalau screenshot ini memang transaksi terpisah. Belum ada yang dicatat.", true
	case "EVIDENCE_DISPUTE_ALREADY_STAGED":
		return "Koreksi untuk screenshot ini sudah menunggu konfirmasi di kartu sebelumnya. Belum ada yang dicatat.", true
	case "EVIDENCE_DISPUTE_INVALID":
		return "Screenshot ini belum bisa dipisahkan lewat chat: hanya satu baris pengeluaran yang tergabung ke transaksi lama, dengan nominal dan tanggal terbaca, yang bisa dikoreksi. Tidak ada yang diubah.", true
	case "EVIDENCE_DISPUTE_UNAUTHORIZED":
		return "Aku tidak bisa memverifikasi akun Telegram-mu di keluarga ini, jadi koreksi tidak dibuat.", true
	case "AMBIGUOUS_TARGET":
		return "Ada beberapa item yang mungkin kamu maksud. Sebut nama itemnya atau pilih item yang sesuai, ya.", true
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
	case statusFactsLocked:
		return factsLockedMessage, true
	case "ACCOUNT_AMBIGUOUS":
		return "Aku belum bisa memastikan rekening sumbernya. Sebutkan nama rekening yang lebih spesifik, ya.", true
	case "TRANSFER_CLASSIFICATION_REQUIRED":
		return "Ini transfer, bukan pengeluaran, jadi perlu diklasifikasikan dulu: ke rekening sendiri, rekening keluarga, atau investasi. Belum ada yang dicatat.", true
	case "TRANSACTION_UNDER_REVIEW":
		return "Transaksi itu masih menunggu kartunya diselesaikan, jadi belum bisa diubah dari chat. Selesaikan dulu kartunya.", true
	case "TRANSFER_NOT_CATEGORIZABLE":
		return "Transfer tidak punya kategori pengeluaran, jadi kategorinya tidak diubah. Tidak ada yang berubah.", true
	case "TRANSACTION_VOIDED":
		return "Transaksi itu sudah dibatalkan, jadi perubahannya tidak disimpan.", true
	case "AMBIGUOUS_MERCHANT":
		return ambiguousMerchantReply, true
	case "UNUSABLE_MERCHANT":
		return unusableMerchantReply, true
	case "TRANSFER_PURPOSE_REQUIRED":
		return "Aku belum bisa menentukan jenis transfernya. Sebutkan akun tujuannya (tabungan, investasi, atau utang) atau tulis bahwa ini pindah antar rekening. Belum ada yang dicatat.", true
	case "INCOMPATIBLE_TRANSFER_PURPOSE", "INCOMPATIBLE_WEALTH_ACCOUNT":
		return "Akun kekayaan itu tidak cocok untuk aksi ini, jadi belum ada yang diubah. Sebutkan akun yang sesuai, ya.", true
	case "TOO_MANY_CANDIDATES":
		return "Ada terlalu banyak transaksi yang mirip, jadi belum ada yang dicatat. Sebutkan jam atau detail lain yang lebih spesifik, ya.", true
	case "INVALID_AMOUNT":
		return "Nominalnya belum valid, jadi belum ada yang dicatat. Tulis nominal rupiah tanpa titik atau koma, misalnya 25000.", true
	case "INVALID_TIME":
		return "Tanggal atau jam transaksinya belum terbaca, jadi belum ada yang dicatat.", true
	case "WEALTH_ACCOUNT_AMBIGUOUS":
		return "Aku belum bisa memastikan akun kekayaannya. Sebutkan nama akun yang lebih spesifik, ya.", true
	case "MISSING_SEARCH_TEXT":
		return "Transaksi apa yang kamu cari? Sebutkan nama merchant atau keperluannya, ya.", true
	case "INVALID_CATEGORY":
		return "Kategori itu tidak ada di daftar keluarga ini. Pilih salah satu kategori pengeluaran yang tersedia.", true
	case "STALE_REVIEW_BINDING":
		return "Kartu ini sudah berubah atau selesai. Lihat kartu terbarunya untuk melanjutkan.", true
	}
	return genericMutationReply, false
}
