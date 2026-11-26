package telegram_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/document"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

func (p *pipe) readableExtraction(category string) json.RawMessage {
	return p.extraction(category, fmt.Sprintf(`[{"kind":"TRANSACTION_ID","value":"SYNTHETIC-REPROCESS-%d"}]`, time.Now().UnixNano()))
}

func (p *pipe) extraction(category, identifiers string) json.RawMessage {
	at := time.Now().Add(-30 * time.Minute).In(time.FixedZone("WIB", 7*3600)).Truncate(time.Minute).Format(time.RFC3339)
	slug := "null"
	confidence := "0.2"
	if category != "" {
		slug, confidence = fmt.Sprintf("%q", category), "0.96"
	}
	return json.RawMessage(fmt.Sprintf(`{"account_hint":"","payment_status":"PAID","confidence":0.97,"transactions":[{"direction":"OUT","amount":"7500","currency":"IDR","transaction_at":%q,"merchant":"Toko Contoh","description":"Pembayaran","category_slug":%s,"category_confidence":%s,"confidence":0.97,"transaction_identifiers":%s,"transaction_time_precision":"TIME"}]}`, at, slug, confidence, identifiers))
}

func (p *pipe) unreadableUpload() (upload, string, int64) {
	p.t.Helper()
	p.gw.extraction = p.extraction("food-and-drink", `[{"kind":"ACCOUNT_NUMBER","value":"123456"},{"kind":"REFERENCE_NUMBER","value":"a"},{"kind":"REFERENCE_NUMBER","value":"b"}]`)
	u := p.upload()
	item := p.str(`SELECT id::text FROM review_item WHERE document_id=$1::uuid AND review_type='DOCUMENT_EXTRACTION_LOW_CONFIDENCE'`, u.document)
	tx, err := p.pool.Begin(p.ctx)
	p.must(err)
	p.must(telegram.ProjectReviewItem(p.ctx, tx, p.household, item, u.message, "", p.chat))
	p.must(tx.Commit(p.ctx))
	request := p.str(`SELECT r.id::text FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE ri.document_id=$1::uuid AND ri.review_type='DOCUMENT_EXTRACTION_LOW_CONFIDENCE'`, u.document)
	if request == "" {
		p.t.Fatal("an unreadable screenshot produced no review card")
	}
	p.seq += 2
	p.must(p.tg.BindReviewMessage(p.ctx, request, p.chat, p.seq, "Dokumen belum terbaca"))
	return u, request, p.seq
}

func (p *pipe) lastEdit(message int64) string {
	return p.str(`SELECT payload_json->>'text' FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 ORDER BY created_at DESC,id DESC LIMIT 1`, p.chat, message)
}

func TestReprocessedDocumentFinishesOnTheSameCard(t *testing.T) {
	t.Parallel()
	p := newPipe(t)
	u, request, message := p.unreadableUpload()
	sendsBefore := p.n(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1`, p.chat)
	if _, err := p.press("review:reprocess", message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	if got := p.lastEdit(message); !strings.Contains(got, "Dokumen sedang dibaca ulang") {
		t.Fatalf("the tapped card says %q", got)
	}
	if p.n(`SELECT count(*) FROM job WHERE type='PROCESS_DOCUMENT' AND payload_json->>'document_id'=$1`, u.document) != 1 {
		t.Fatal("the reprocess job was not queued once")
	}

	p.gw.extraction = p.readableExtraction("food-and-drink")
	p.must(p.docs.ProcessScreenshot(p.ctx, u.document))

	if got := p.lastEdit(message); !strings.Contains(got, "1 transaksi ditemukan") || !strings.Contains(got, "1 tersimpan") {
		t.Fatalf("the finished card says %q", got)
	}
	if after := p.n(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1`, p.chat); after != sendsBefore {
		t.Fatalf("the result came as %d loose message(s)", after-sendsBefore)
	}
	if p.n(`SELECT count(*) FROM review_conversation WHERE review_request_id=$1::uuid AND context_json->'reprocess'->>'continued' IS NOT NULL`, request) != 1 {
		t.Fatal("the reprocess was not marked as continued")
	}
	if p.confirmed() != 1 {
		t.Fatalf("confirmed transactions = %d, want the reread record", p.confirmed())
	}
}

func TestReprocessedDocumentContinuesIntoTheNextReviewOnTheSameCard(t *testing.T) {
	t.Parallel()
	p := newPipe(t)
	u, request, message := p.unreadableUpload()
	if _, err := p.press("review:reprocess", message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	sendsBefore := p.n(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1`, p.chat)

	p.gw.extraction = p.readableExtraction("")
	p.must(p.docs.ProcessScreenshot(p.ctx, u.document))

	next := p.str(`SELECT r.id::text FROM review_request r JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE rr.telegram_message_id=$1 AND rr.telegram_chat_id=$2 AND r.id<>$3::uuid AND rr.delivery_status='SENT'`, message, p.chat, request)
	if next == "" {
		t.Fatal("the card did not move to the review of the reread row")
	}
	if got := p.lastEdit(message); !strings.Contains(got, "Masuk kategori apa?") || !strings.Contains(got, "Rp7.500") {
		t.Fatalf("the continued card says %q", got)
	}
	if after := p.n(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1`, p.chat); after != sendsBefore {
		t.Fatalf("the next question came as %d loose message(s)", after-sendsBefore)
	}

	if _, err := p.press("review:ignore", message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	if got := p.lastEdit(message); !strings.Contains(got, "Diabaikan") {
		t.Fatalf("the continued card does not answer its new step: %q", got)
	}
}

func TestReprocessedDocumentThatCannotBeReadAgainSaysSoOnTheCard(t *testing.T) {
	t.Parallel()
	p := newPipe(t)
	u, _, message := p.unreadableUpload()
	if _, err := p.press("review:reprocess", message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	p.exec(`UPDATE document SET status='CLASSIFIED' WHERE id=$1::uuid`, u.document)
	tx, err := p.pool.Begin(p.ctx)
	p.must(err)
	p.must(p.docs.TerminalDownstreamFailureTx(p.ctx, tx, u.document))
	p.must(tx.Commit(p.ctx))
	if got := p.lastEdit(message); !strings.Contains(got, "Dokumen ini belum bisa dibaca") {
		t.Fatalf("the card says %q", got)
	}
}

func TestReprocessedDocumentWithoutALiveCardFallsBackToTheNotice(t *testing.T) {
	t.Parallel()
	p := newPipe(t)
	u, _, message := p.unreadableUpload()
	if _, err := p.press("review:reprocess", message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	p.exec(`UPDATE review_request_recipient SET delivery_status='EXPIRED' WHERE telegram_message_id=$1 AND telegram_chat_id=$2`, message, p.chat)
	sendsBefore := p.n(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1`, p.chat)

	p.gw.extraction = p.readableExtraction("food-and-drink")
	p.must(p.docs.ProcessScreenshot(p.ctx, u.document))

	if after := p.n(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text' LIKE '%1 transaksi ditemukan%'`, p.chat); after != 1 {
		t.Fatalf("with no live card the result must still reach the user as the summary, got %d (sends before %d)", after, sendsBefore)
	}
}

func TestALaterNoticeForAnAlreadyContinuedDocumentIsNotSwallowed(t *testing.T) {
	t.Parallel()
	p := newPipe(t)
	u, request, message := p.unreadableUpload()
	if _, err := p.press("review:reprocess", message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	p.gw.extraction = p.readableExtraction("food-and-drink")
	p.must(p.docs.ProcessScreenshot(p.ctx, u.document))
	if p.n(`SELECT count(*) FROM review_conversation WHERE review_request_id=$1::uuid AND context_json->'reprocess'->>'continued' IS NOT NULL`, request) != 1 {
		t.Fatal("the first result did not take over the card")
	}
	tx, err := p.pool.Begin(p.ctx)
	p.must(err)
	handled, err := telegram.ContinueDocumentCard(p.ctx, tx, u.document, "Satu catatan lagi.")
	p.must(err)
	p.must(tx.Commit(p.ctx))
	if handled {
		t.Fatal("a notice from a later run was dropped because an earlier run had used the card")
	}
}

type classifyingGW struct {
	documentType string
	observation  string
}

func (g classifyingGW) NativeToolCall(_ context.Context, _ string, _ string, _ any, tools []gateway.ToolDefinition, _ ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	switch tools[0].Name {
	case "classify_financial_document":
		return gateway.ToolCall{Name: tools[0].Name, Arguments: json.RawMessage(fmt.Sprintf(`{"document_type":%q,"confidence":0.98,"reason":"synthetic"}`, g.documentType))}, gateway.Metadata{Model: "synthetic"}, nil
	case "extract_wealth_observation":
		return gateway.ToolCall{Name: tools[0].Name, Arguments: json.RawMessage(g.observation)}, gateway.Metadata{Model: "synthetic"}, nil
	}
	return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("unexpected tool %s", tools[0].Name)
}

func (p *pipe) rereadAfterTap(documentType, observation string) (string, int64) {
	p.t.Helper()
	u, _, message := p.unreadableUpload()
	if _, err := p.press("review:reprocess", message, p.tgUser); err != nil {
		p.t.Fatal(err)
	}
	sends := p.n(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1`, p.chat)
	reader := document.NewProcessorWithStorage(p.pool, classifyingGW{documentType: documentType, observation: observation}, p.storage)
	p.must(reader.Process(p.ctx, u.document))
	if after := p.n(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1`, p.chat); after != sends {
		p.t.Fatalf("the result came as %d loose message(s)", after-sends)
	}
	return u.document, message
}

func TestRereadThatIsNotAFinancialDocumentSaysSoOnTheCard(t *testing.T) {
	t.Parallel()
	p := newPipe(t)
	_, message := p.rereadAfterTap("NON_FINANCIAL_OR_UNSUPPORTED", "")
	if got := p.lastEdit(message); !strings.Contains(got, "Ini bukan dokumen keuangan, jadi tidak ada yang dicatat.") {
		t.Fatalf("the card says %q", got)
	}
}

func TestRereadThatBecomesAWealthBalanceFinishesOnTheCard(t *testing.T) {
	t.Parallel()
	p := newPipe(t)
	p.exec(`INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Emas Contoh','ASSET','GOLD','OTHER')`, p.household)
	_, message := p.rereadAfterTap("WEALTH_OBSERVATION", `{"institution":"Bank Contoh","account_hint":"Emas Contoh","observed_value_idr":"15000000","quantity":null,"unit":null,"unit_price_idr":null,"observed_date":null,"confidence":0.98}`)
	got := p.lastEdit(message)
	if !strings.Contains(got, "✅ Saldo tersimpan: Emas Contoh · Rp15.000.000") || !strings.Contains(got, "bukan transaksi") {
		t.Fatalf("the card says %q", got)
	}
}

func TestRereadWhoseClassificationFailsForGoodSaysSoOnTheCard(t *testing.T) {
	t.Parallel()
	p := newPipe(t)
	u, _, message := p.unreadableUpload()
	if _, err := p.press("review:reprocess", message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	p.must(p.docs.HandleTerminalFailure(p.ctx, u.document, errors.New("gateway unavailable")))
	if got := p.lastEdit(message); !strings.Contains(got, "Dokumen ini belum bisa dibaca") {
		t.Fatalf("the card says %q", got)
	}
}

func TestRereadOfAnUnpaidInvoiceSaysSoOnTheCard(t *testing.T) {
	t.Parallel()
	p := newPipe(t)
	u, _, message := p.unreadableUpload()
	if _, err := p.press("review:reprocess", message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	p.exec(`UPDATE document SET document_type='BILL_OR_INVOICE' WHERE id=$1::uuid`, u.document)
	p.gw.extraction = json.RawMessage(`{"account_hint":"","payment_status":"UNPAID","confidence":0.9,"transactions":[]}`)
	p.must(p.docs.ProcessScreenshot(p.ctx, u.document))
	if got := p.lastEdit(message); !strings.Contains(got, "Dokumen ini belum bisa dibaca") {
		t.Fatalf("the card says %q", got)
	}
}
