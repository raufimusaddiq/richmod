package document

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNoticeAmountGroupsWholeRupiahAndDropsAnythingElse(t *testing.T) {
	for in, want := range map[string]string{
		"125000": " Rp125.000", "999": " Rp999", "1000": " Rp1.000", "1234567": " Rp1.234.567", "0": " Rp0",
		"": "", "12.5": "", "-5": "", "Rp125000": "", "125000 <b>": "", "１２３": "",
	} {
		if got := noticeAmount(in); got != want {
			t.Fatalf("noticeAmount(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNoticeSubjectIsOneBoundedLineOfPlainText(t *testing.T) {
	if got := noticeSubject("  Mirota\n\nSwalayan\t Yogya "); got != " Mirota Swalayan Yogya" {
		t.Fatalf("subject = %q", got)
	}
	if noticeSubject("   ") != "" {
		t.Fatal("blank evidence text produced a subject")
	}
	long := noticeSubject(strings.Repeat("x", 200))
	if n := len([]rune(long)); n != 41 || !strings.HasSuffix(long, "…") {
		t.Fatalf("long subject has %d runes: %q", n, long)
	}
}

func TestNoticeCopyAsksForCorrectionsByReply(t *testing.T) {
	for _, text := range []string{
		receiptRecordedNotice("Mirota", "125000"),
		receiptLinkedNotice("Mirota", "125000"),
		payslipRecordedNotice("PT Maju", "9500000"),
		receiptRecordedNotice("", ""),
	} {
		if !strings.HasSuffix(text, "Balas pesan ini kalau ada yang perlu dibetulkan.") {
			t.Fatalf("notice does not invite a reply: %q", text)
		}
		if strings.Contains(text, "  ") || strings.Contains(text, "\n") {
			t.Fatalf("notice has stray whitespace: %q", text)
		}
	}
	if got := receiptRecordedNotice("Mirota", "125000"); !strings.HasPrefix(got, "Struk Mirota Rp125.000 sudah tercatat.") {
		t.Fatalf("recorded notice = %q", got)
	}
	if got := payslipRecordedNotice("PT Maju", "9500000"); !strings.HasPrefix(got, "Slip gaji PT Maju tercatat, gaji bersih Rp9.500.000.") {
		t.Fatalf("payslip notice = %q", got)
	}
}

func noticeFixture(t *testing.T, ctx context.Context, withChat bool, sourceType string) (*pgxpool.Pool, string, string, int64) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	stamp := time.Now().UnixNano()
	var householdID, sourceID, attachmentID, documentID string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("notice %d", stamp)).Scan(&householdID))
	chat := stamp % 9_000_000_000
	chatArg := any(nil)
	if withChat {
		chatArg = chat
	}
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_message_id,telegram_chat_id) VALUES($1,$2,$3,now(),$4,'PROCESSING',321,$5) RETURNING id`,
		householdID, sourceType, fmt.Sprintf("notice-%d", stamp), []byte(fmt.Sprintf("notice-%d", stamp)), chatArg).Scan(&sourceID))
	must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',3,1,1,$3) RETURNING id`, householdID, []byte(fmt.Sprintf("h-%d", stamp)), fmt.Sprintf("t/%d.jpg", stamp)).Scan(&attachmentID))
	must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status) VALUES($1,$2,$3,'RECEIVED') RETURNING id`, householdID, sourceID, attachmentID).Scan(&documentID))
	return pool, sourceID, documentID, chat
}

func TestEvidenceNoticeIsOnePerDocumentToTheUploadChatAsAReply(t *testing.T) {
	ctx := context.Background()
	pool, sourceID, documentID, chat := noticeFixture(t, ctx, true, "TELEGRAM_IMAGE")
	for i := 0; i < 3; i++ { // a retried processor re-runs the same path
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := enqueueEvidenceNotice(ctx, tx, sourceID, documentID, receiptRecordedNotice("Mirota", "125000")); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var jobs int
	var gotChat, replyTo int64
	var text, bind string
	if err := pool.QueryRow(ctx, `SELECT count(*) OVER(),(payload_json->>'chat_id')::bigint,(payload_json->>'reply_to_message_id')::bigint,payload_json->>'text',payload_json->>'bind_document_id'
		FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'bind_document_id'=$1`, documentID).Scan(&jobs, &gotChat, &replyTo, &text, &bind); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || gotChat != chat || replyTo != 321 || bind != documentID || !strings.HasPrefix(text, "Struk Mirota Rp125.000") {
		t.Fatalf("jobs=%d chat=%d reply=%d bind=%s text=%q", jobs, gotChat, replyTo, bind, text)
	}
}

func TestEvidenceNoticeIsSkippedWithoutAnUploadChat(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		withChat   bool
		sourceType string
	}{"telegram upload that recorded no chat": {false, "TELEGRAM_IMAGE"}, "non-Telegram evidence": {true, "BANK_EMAIL"}} {
		pool, sourceID, documentID, _ := noticeFixture(t, ctx, c.withChat, c.sourceType)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := enqueueEvidenceNotice(ctx, tx, sourceID, documentID, "x"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE payload_json->>'bind_document_id'=$1`, documentID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: queued %d notices (err=%v), want none", name, n, err)
		}
	}
}

func TestEvidenceNoticeRollsBackWithItsMutation(t *testing.T) {
	ctx := context.Background()
	pool, sourceID, documentID, _ := noticeFixture(t, ctx, true, "TELEGRAM_IMAGE")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := enqueueEvidenceNotice(ctx, tx, sourceID, documentID, "x"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE payload_json->>'bind_document_id'=$1`, documentID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a rolled-back mutation left %d notices (err=%v)", n, err)
	}
}
