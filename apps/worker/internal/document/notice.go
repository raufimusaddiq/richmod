package document

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const noticeFooter = " Balas pesan ini kalau ada yang perlu dibetulkan."

var (
	digitsOnly   = regexp.MustCompile(`^[0-9]+$`)
	noticeSpaces = regexp.MustCompile(`\s+`)
)

// enqueueEvidenceNotice queues one short "recorded" message for a document that
// resolved without a human, so a reply to it binds back to that document
// (CEU-02, ADR-050).
//
// It runs in the same transaction as the mutation it announces, so the notice
// exists exactly when the result does. It goes to the chat the upload came from,
// as a reply to the upload, and only for Telegram uploads that recorded their
// chat. At most one notice is ever queued per document, which keeps a retried
// processor from announcing the same result twice. That guarantee rests on a
// durable marker on the document (document.evidence_notice_at), not on job rows,
// because job rows are pruned.
func enqueueEvidenceNotice(ctx context.Context, tx pgx.Tx, sourceEventID, documentID, text string) error {
	var chatID, messageID *int64
	err := tx.QueryRow(ctx, `SELECT telegram_chat_id,telegram_message_id FROM source_event WHERE id=$1 AND source_type='TELEGRAM_IMAGE'`, sourceEventID).Scan(&chatID, &messageID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && chatID == nil) {
		return nil
	}
	if err != nil {
		return err
	}
	// Claim the document's one notice. The marker and the job commit or roll back
	// together with the mutation they announce.
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

func receiptLinkedNotice(merchant, total string) string {
	return "Struk" + noticeSubject(merchant) + noticeAmount(total) + " dilampirkan ke transaksi yang sudah ada." + noticeFooter
}

func payslipRecordedNotice(employer, netPay string) string {
	return "Slip gaji" + noticeSubject(employer) + " tercatat, gaji bersih" + noticeAmount(netPay) + "." + noticeFooter
}

// noticeSubject renders evidence-derived text for the user's own chat: one line,
// bounded. It is data, never markup, and is only ever sent as plain text.
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

// noticeAmount groups a whole-rupiah amount with dots. Anything that is not a
// plain digit string is left out rather than echoed.
func noticeAmount(amount string) string {
	amount = strings.TrimSpace(amount)
	if !digitsOnly.MatchString(amount) {
		return ""
	}
	// amount is ASCII digits (checked above), so byte positions are digit positions.
	var grouped []byte
	for index := 0; index < len(amount); index++ {
		if index > 0 && (len(amount)-index)%3 == 0 {
			grouped = append(grouped, '.')
		}
		grouped = append(grouped, amount[index])
	}
	return " Rp" + string(grouped)
}
