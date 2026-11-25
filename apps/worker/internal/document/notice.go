package document

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	workerTelegram "github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

const noticeFooter = " Kalau ada yang keliru, tulis koreksinya."

var (
	digitsOnly   = regexp.MustCompile(`^[0-9]+$`)
	noticeSpaces = regexp.MustCompile(`\s+`)
)

func enqueueEvidenceNotice(ctx context.Context, tx pgx.Tx, sourceEventID, documentID, text string) error {
	if continued, err := workerTelegram.ContinueDocumentCard(ctx, tx, documentID, "✅ "+strings.TrimSuffix(text, noticeFooter)); err != nil || continued {
		return err
	}
	var chatID, messageID *int64
	err := tx.QueryRow(ctx, `SELECT telegram_chat_id,telegram_message_id FROM source_event WHERE id=$1 AND source_type='TELEGRAM_IMAGE'`, sourceEventID).Scan(&chatID, &messageID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && chatID == nil) {
		return nil
	}
	if err != nil {
		return err
	}
	var claimed string
	err = tx.QueryRow(ctx, `UPDATE document SET evidence_notice_at=now() WHERE id=$1::uuid AND evidence_notice_at IS NULL RETURNING id::text`, documentID).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var replyTo int64
	if messageID != nil {
		replyTo = *messageID
	}
	_, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',
		jsonb_build_object('chat_id',$1::bigint,'text',$2::text,'bind_document_id',$3::text)
		|| CASE WHEN $4::bigint>0 THEN jsonb_build_object('reply_to_message_id',$4::bigint) ELSE '{}'::jsonb END)`,
		*chatID, text, documentID, replyTo)
	return err
}

func receiptRecordedNotice(merchant, total string) string {
	return "Struk" + noticeSubject(merchant) + noticeAmount(total) + " sudah tercatat." + noticeFooter
}

func receiptRecordedMismatchNotice(merchant, total string) string {
	return "Struk" + noticeSubject(merchant) + noticeAmount(total) + " sudah tercatat memakai total yang tercetak. Rincian barisnya tidak pas dengan total, tetapi tidak ada yang perlu kamu putuskan." + noticeFooter
}

func receiptLinkedNotice(merchant, total string) string {
	return "Struk" + noticeSubject(merchant) + noticeAmount(total) + " dilampirkan ke transaksi yang sudah ada." + noticeFooter
}

func payslipRecordedNotice(employer, netPay string) string {
	return "Slip gaji" + noticeSubject(employer) + " tercatat, gaji bersih" + noticeAmount(netPay) + "." + noticeFooter
}

func unsupportedDocumentNotice() string {
	return "Dokumen keuangan ini sudah disimpan sebagai bukti, tetapi belum bisa dicatat otomatis sebagai transaksi."
}

func noticeSubject(value string) string {
	value = strings.TrimSpace(noticeSpaces.ReplaceAllString(value, " "))
	if value == "" {
		return ""
	}
	if utf8.RuneCountInString(value) > 40 {
		value = string([]rune(value)[:39]) + "…"
	}
	return " " + value
}

func noticeAmount(amount string) string {
	amount = strings.TrimSpace(amount)
	if !digitsOnly.MatchString(amount) {
		return ""
	}
	var grouped []byte
	for index := 0; index < len(amount); index++ {
		if index > 0 && (len(amount)-index)%3 == 0 {
			grouped = append(grouped, '.')
		}
		grouped = append(grouped, amount[index])
	}
	return " Rp" + string(grouped)
}
