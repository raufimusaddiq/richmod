package telegram_test

// Realistic dispute pipeline: synthetic screenshots run through the real
// document pipeline (ProcessScreenshot → matching → MERGED), the dispute is
// raised through real text intake (source_event without chat/message ids, as in
// production) and resolved through real callback intake. Only the model and the
// image bytes are scripted.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/blob"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/document"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/telegram"
)

type scriptGW struct {
	mu         sync.Mutex
	extraction json.RawMessage
	agent      func(call int, request gateway.AgentRequest) gateway.AgentResponse
	calls      int
}

func (g *scriptGW) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{CallID: "extract", Name: "extract_transaction_screenshot", Arguments: g.extraction}, gateway.Metadata{Model: "stub-vision"}, nil
}

func (g *scriptGW) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.mu.Lock()
	call := g.calls
	g.calls++
	g.mu.Unlock()
	if g.agent != nil {
		return g.agent(call, request), nil
	}
	return gateway.AgentResponse{Text: "Baik."}, nil
}

func disputeCall(call int, _ gateway.AgentRequest) gateway.AgentResponse {
	if call%2 == 0 {
		return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: fmt.Sprintf("dispute-%d", call), Name: "dispute_evidence_link", Arguments: []byte(`{}`)}}}
	}
	return gateway.AgentResponse{Text: "Baik."}
}

type pipe struct {
	t                     *testing.T
	ctx                   context.Context
	pool                  *pgxpool.Pool
	gw                    *scriptGW
	docs                  *document.Processor
	tg                    *telegram.Processor
	household, user       string
	tgUser, chat          int64
	catFood, catGroceries string
	seq                   int64
	storage               *blob.Store
}

type upload struct {
	source, document string
	message          int64
}

func newPipe(t *testing.T) *pipe {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	stamp := time.Now().UnixNano()
	p := &pipe{t: t, ctx: ctx, pool: pool, tgUser: 1_000_000_000 + stamp%1_000_000_000, seq: 100}
	p.chat = p.tgUser
	p.must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id::text`, fmt.Sprintf("Dispute pipeline %d", stamp)).Scan(&p.household))
	p.must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id::text`, fmt.Sprintf("pipe-%d@example.test", stamp)).Scan(&p.user))
	p.exec(`INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, p.household, p.user)
	p.exec(`INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, p.tgUser, p.household, p.user)
	p.must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan & Minuman','food-and-drink') RETURNING id::text`, p.household).Scan(&p.catFood))
	p.must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Belanja Rumah','groceries') RETURNING id::text`, p.household).Scan(&p.catGroceries))
	store, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-30 * time.Minute).In(time.FixedZone("WIB", 7*3600)).Truncate(time.Minute).Format(time.RFC3339)
	extraction := fmt.Sprintf(`{"account_hint":"","payment_status":"PAID","confidence":0.97,"transactions":[{"direction":"OUT","amount":"7500","currency":"IDR","transaction_at":%q,"merchant":"Synthetic Store","description":"Payment to Synthetic Store","category_slug":"food-and-drink","category_confidence":0.96,"confidence":0.97}]}`, at)
	p.gw = &scriptGW{extraction: json.RawMessage(extraction), agent: disputeCall}
	p.docs = document.NewProcessorWithStorage(pool, p.gw, store)
	p.tg = telegram.NewProcessor(pool, p.gw)
	p.storage = store
	return p
}

func (p *pipe) must(err error) {
	p.t.Helper()
	if err != nil {
		p.t.Fatal(err)
	}
}

func (p *pipe) exec(sql string, args ...any) {
	p.t.Helper()
	_, err := p.pool.Exec(p.ctx, sql, args...)
	p.must(err)
}

func (p *pipe) str(sql string, args ...any) string {
	p.t.Helper()
	var v *string
	if err := p.pool.QueryRow(p.ctx, sql, args...).Scan(&v); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		p.t.Fatal(err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func (p *pipe) n(sql string, args ...any) int {
	p.t.Helper()
	var v int
	p.must(p.pool.QueryRow(p.ctx, sql, args...).Scan(&v))
	return v
}

// upload stores a synthetic screenshot as Telegram image intake does (chat and
// message ids present) and runs the real screenshot pipeline on it.
func (p *pipe) upload() upload {
	p.t.Helper()
	p.seq += 2
	stamp := time.Now().UnixNano()
	u := upload{message: p.seq}
	p.must(p.pool.QueryRow(p.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_message_id,telegram_chat_id) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'RECEIVED',$4,$5) RETURNING id::text`, p.household, fmt.Sprintf("img-%d", stamp), []byte(fmt.Sprintf("img-%d", stamp)), u.message, p.chat).Scan(&u.source))
	ref := fmt.Sprintf("%s/%d.jpg", p.household, stamp)
	p.must(p.storage.Put(p.ctx, ref, []byte("synthetic"), "image/jpeg"))
	var attachment string
	p.must(p.pool.QueryRow(p.ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/jpeg',9,1,1,$3) RETURNING id::text`, p.household, []byte(fmt.Sprintf("h-%d", stamp)), ref).Scan(&attachment))
	p.must(p.pool.QueryRow(p.ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status,document_type) VALUES($1,$2,$3,'RECEIVED','EWALLET_SCREENSHOT') RETURNING id::text`, p.household, u.source, attachment).Scan(&u.document))
	p.must(p.docs.ProcessScreenshot(p.ctx, u.document))
	return u
}

// text delivers a typed reply through real text intake (no chat/message id on
// the source_event, exactly as production stores it) and the real Process lane.
func (p *pipe) text(body string, replyTo int64, from int64) (string, error) {
	p.t.Helper()
	p.gw.mu.Lock()
	p.gw.calls = 0
	p.gw.mu.Unlock()
	p.seq += 2
	update := map[string]any{"message": map[string]any{"message_id": p.seq, "text": body, "from": map[string]any{"id": from}, "chat": map[string]any{"id": from}}}
	if replyTo != 0 {
		update["message"].(map[string]any)["reply_to_message"] = map[string]any{"message_id": replyTo}
	}
	raw, _ := json.Marshal(update)
	stamp := time.Now().UnixNano()
	var source string
	p.must(p.pool.QueryRow(p.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED') RETURNING id::text`, p.household, fmt.Sprintf("txt-%d", stamp), []byte(fmt.Sprintf("txt-%d", stamp))).Scan(&source))
	p.exec(`INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, source, string(raw))
	return source, p.tg.Process(p.ctx, source)
}

func (p *pipe) press(data string, message int64, from int64) (string, error) {
	p.t.Helper()
	raw, _ := json.Marshal(map[string]any{"callback_query": map[string]any{"id": fmt.Sprintf("cb-%d", time.Now().UnixNano()), "data": data, "from": map[string]any{"id": from}, "message": map[string]any{"message_id": message, "chat": map[string]any{"id": p.chat}}}})
	stamp := time.Now().UnixNano()
	var source string
	p.must(p.pool.QueryRow(p.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id::text`, p.household, fmt.Sprintf("cb-%d", stamp), []byte(fmt.Sprintf("cb-%d", stamp))).Scan(&source))
	p.exec(`INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, source, string(raw))
	return source, p.tg.Process(p.ctx, source)
}

// card binds the open dispute card to a Telegram message id, as the send job does.
func (p *pipe) card() int64 {
	p.t.Helper()
	request := p.str(`SELECT r.id::text FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.household_id=$1 AND r.status IN ('OPEN','PENDING_SEND') AND ri.decision->'decisionProvenance'->>'evidence_link_dispute'='true' ORDER BY r.created_at DESC LIMIT 1`, p.household)
	if request == "" {
		p.t.Fatal("no open dispute card")
	}
	p.seq += 2
	p.exec(`UPDATE review_request_recipient SET telegram_message_id=$2 WHERE review_request_id=$1::uuid AND telegram_chat_id=$3`, request, p.seq, p.chat)
	p.exec(`UPDATE review_request SET status='OPEN' WHERE id=$1::uuid AND status='PENDING_SEND'`, request)
	return p.seq
}

func (p *pipe) markupOf(request string) string {
	return p.str(`SELECT COALESCE(string_agg(payload_json->>'reply_markup',' '),'') FROM job WHERE payload_json->>'review_request_id'=$1`, request)
}

func (p *pipe) confirmed() int {
	return p.n(`SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, p.household)
}

func (p *pipe) staged() int {
	return p.n(`SELECT count(*) FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW'`, p.household)
}

func (p *pipe) stagedID() string {
	return p.str(`SELECT id::text FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW' ORDER BY created_at DESC LIMIT 1`, p.household)
}

func (p *pipe) toolStatus(source string) string {
	return p.str(`SELECT public_context_json->>'status' FROM telegram_conversation_turn WHERE source_event_id=$1::uuid AND tool_name='dispute_evidence_link' ORDER BY created_at DESC LIMIT 1`, source)
}

func (p *pipe) mergedPair() (first, second upload, originalTx string) {
	p.t.Helper()
	first = p.upload()
	second = p.upload()
	if got := p.str(`SELECT proposal_status FROM transaction_proposal WHERE source_event_id=$1::uuid`, second.source); got != "MERGED" {
		p.t.Fatalf("second screenshot proposal = %q, want MERGED (real pipeline)", got)
	}
	originalTx = p.str(`SELECT transaction_id::text FROM transaction_evidence WHERE source_event_id=$1::uuid`, first.source)
	return first, second, originalTx
}

type txSnapshot struct{ status, category, merchant, updated, amount string }

func (p *pipe) snapshot(id string) txSnapshot {
	p.t.Helper()
	return txSnapshot{
		status:   p.str(`SELECT status FROM transaction WHERE id=$1::uuid`, id),
		category: p.str(`SELECT category_id::text FROM transaction WHERE id=$1::uuid`, id),
		merchant: p.str(`SELECT merchant_id::text FROM transaction WHERE id=$1::uuid`, id),
		updated:  p.str(`SELECT updated_at::text FROM transaction WHERE id=$1::uuid`, id),
		amount:   p.str(`SELECT amount::text FROM transaction WHERE id=$1::uuid`, id),
	}
}

func TestMergedProposalKeepsTheValidatedCategoryCandidate(t *testing.T) {
	p := newPipe(t)
	_, second, _ := p.mergedPair()
	if got := p.str(`SELECT category_candidate_id::text FROM transaction_proposal WHERE source_event_id=$1::uuid`, second.source); got != p.catFood {
		t.Fatalf("MERGED proposal category candidate = %q, want the validated extraction category %q", got, p.catFood)
	}
}

func TestDisputeConfirmCarriesCategoryMerchantAndConfidencesAndMovesCorrectionTarget(t *testing.T) {
	p := newPipe(t)
	first, second, original := p.mergedPair()
	before := p.snapshot(original)

	source, err := p.text("tapi ini transaksi baru", second.message, p.tgUser)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.toolStatus(source); got != "EVIDENCE_DISPUTE_STAGED" {
		t.Fatalf("dispute tool status = %q", got)
	}
	staged := p.stagedID()
	if p.staged() != 1 || p.confirmed() != 1 {
		t.Fatalf("staging confirmed something: staged=%d confirmed=%d", p.staged(), p.confirmed())
	}
	if got := p.str(`SELECT category_id::text FROM transaction WHERE id=$1::uuid`, staged); got != p.catFood {
		t.Fatalf("staged category = %q, want extraction category", got)
	}
	card := p.card()
	if _, err = p.press("review:dispute:confirm", card, p.tgUser); err != nil {
		t.Fatalf("confirm callback: %v", err)
	}

	if p.confirmed() != 2 || p.staged() != 0 {
		t.Fatalf("after confirm confirmed=%d staged=%d", p.confirmed(), p.staged())
	}
	if got := p.snapshot(original); got != before {
		t.Fatalf("original transaction changed: %+v -> %+v", before, got)
	}
	// Parity with the normal screenshot path on the same extraction.
	for _, column := range []string{"category_id", "source_confidence", "classification_confidence"} {
		a := p.str(fmt.Sprintf(`SELECT %s::text FROM transaction WHERE id=$1::uuid`, column), original)
		b := p.str(fmt.Sprintf(`SELECT %s::text FROM transaction WHERE id=$1::uuid`, column), staged)
		if a == "" || a != b {
			t.Fatalf("%s: original=%q repaired=%q, want equal and set", column, a, b)
		}
	}
	if p.str(`SELECT merchant_id::text FROM transaction WHERE id=$1::uuid`, staged) == "" {
		t.Fatal("repaired transaction has no merchant_id")
	}
	if p.str(`SELECT metadata_json->>'link_status' FROM transaction_evidence WHERE source_event_id=$1::uuid AND transaction_id=$2::uuid`, second.source, original) != "SUPERSEDED" {
		t.Fatal("old link was not superseded")
	}
	if p.n(`SELECT count(*) FROM transaction_evidence WHERE source_event_id=$1::uuid AND evidence_type='TRANSACTION_SCREENSHOT' AND COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE'`, second.source) != 1 {
		t.Fatal("want exactly one active link for the second screenshot")
	}
	_ = first

	// F1: a correction replying to the second screenshot must target the NEW transaction.
	p.gw.agent = func(call int, request gateway.AgentRequest) gateway.AgentResponse {
		if call%2 != 0 {
			return gateway.AgentResponse{Text: "Menunggu konfirmasi."}
		}
		content, _ := request.Content.(map[string]any)
		turn, _ := content["turn_context"].(map[string]any)
		bound, _ := turn["bound_evidence"].(map[string]any)
		canonical, _ := bound["canonical"].(map[string]any)
		ref, _ := canonical["transaction_ref"].(string)
		args, _ := json.Marshal(map[string]any{"target_ref": ref, "category_slug": "groceries", "period": "THIS_MONTH"})
		return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "correct", Name: "propose_transaction_correction", Arguments: args}}}
	}
	p.gw.calls = 0
	if _, err = p.text("kategorinya belanja rumah", second.message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	if got := p.str(`SELECT transaction_id::text FROM telegram_pending_action WHERE household_id=$1 ORDER BY created_at DESC LIMIT 1`, p.household); got != staged {
		t.Fatalf("correction targets %q, want the new transaction %q (original %q)", got, staged, original)
	}

	// A repeated confirm is stale: nothing changes.
	if _, err = p.press("review:dispute:confirm", card, p.tgUser); err != nil {
		t.Fatalf("repeat confirm: %v", err)
	}
	if p.confirmed() != 2 {
		t.Fatalf("repeat confirm created a transaction: %d", p.confirmed())
	}
}

func TestDisputeWithMemoryConflictAsksForCategoryThroughTheChooserAndRejectsForeignCategory(t *testing.T) {
	p := newPipe(t)
	_, second, original := p.mergedPair()
	// Household memory now says this merchant is groceries while the extraction says food.
	var merchantID string
	p.must(p.pool.QueryRow(p.ctx, `SELECT merchant_id::text FROM transaction WHERE id=$1::uuid`, original).Scan(&merchantID))
	p.exec(`INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,'Synthetic Store',$2::uuid,$3::uuid,true,true)`, p.household, merchantID, p.catGroceries)

	if _, err := p.text("tapi ini transaksi baru", second.message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	staged := p.stagedID()
	if got := p.str(`SELECT category_id::text FROM transaction WHERE id=$1::uuid`, staged); got != "" {
		t.Fatalf("conflicting category sources must leave the category unresolved, got %q", got)
	}
	card := p.card()
	if _, err := p.press("review:dispute:confirm", card, p.tgUser); err != nil {
		t.Fatal(err)
	}
	if p.confirmed() != 1 || p.staged() != 1 {
		t.Fatalf("confirm without category must not record: confirmed=%d staged=%d", p.confirmed(), p.staged())
	}
	request := p.str(`SELECT r.id::text FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.household_id=$1 AND r.status='OPEN' AND ri.decision->'decisionProvenance'->>'evidence_link_dispute'='true'`, p.household)
	if !strings.Contains(p.markupOf(request), "review:cat:") {
		t.Fatalf("the existing category chooser was not offered: %s", p.markupOf(request))
	}

	other := newPipe(t)
	if _, err := p.press("review:cat:"+other.catFood, card, p.tgUser); err != nil {
		t.Fatal(err)
	}
	if p.confirmed() != 1 {
		t.Fatal("a category from another household was accepted")
	}
	if _, err := p.press("review:cat:"+p.catGroceries, card, p.tgUser); err != nil {
		t.Fatal(err)
	}
	if p.confirmed() != 2 || p.str(`SELECT category_id::text FROM transaction WHERE id=$1::uuid`, staged) != p.catGroceries {
		t.Fatalf("chosen category not applied: confirmed=%d", p.confirmed())
	}
	if p.str(`SELECT merchant_id::text FROM transaction WHERE id=$1::uuid`, staged) == "" {
		t.Fatal("merchant not resolved on confirm")
	}
}

func TestRejectedDisputeCanBeRaisedAgainWithoutDuplicateOpenStaging(t *testing.T) {
	p := newPipe(t)
	_, second, original := p.mergedPair()
	before := p.snapshot(original)

	if _, err := p.text("tapi ini transaksi baru", second.message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	firstStaged := p.stagedID()
	// Raising it again while open must not stage a second row.
	source, err := p.text("tapi ini transaksi baru", second.message, p.tgUser)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.toolStatus(source); got != "EVIDENCE_DISPUTE_ALREADY_STAGED" || p.staged() != 1 {
		t.Fatalf("duplicate open staging: status=%q staged=%d", got, p.staged())
	}
	card := p.card()
	if _, err = p.press("review:dispute:reject", card, p.tgUser); err != nil {
		t.Fatal(err)
	}
	if p.str(`SELECT status FROM transaction WHERE id=$1::uuid`, firstStaged) != "VOIDED" || p.staged() != 0 || p.confirmed() != 1 {
		t.Fatal("reject must void the staged row and record nothing")
	}
	if got := p.snapshot(original); got != before {
		t.Fatalf("reject changed the original: %+v -> %+v", before, got)
	}
	// Re-dispute after reject opens a NEW staging.
	source, err = p.text("sebenarnya ini transaksi baru", second.message, p.tgUser)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.toolStatus(source); got != "EVIDENCE_DISPUTE_STAGED" || p.staged() != 1 || p.stagedID() == firstStaged {
		t.Fatalf("re-dispute after reject: status=%q staged=%d same=%v", got, p.staged(), p.stagedID() == firstStaged)
	}
}

func TestDisputeCallbacksAreAuthorizedAndSafeUnderRaces(t *testing.T) {
	p := newPipe(t)
	_, second, original := p.mergedPair()
	before := p.snapshot(original)
	if _, err := p.text("tapi ini transaksi baru", second.message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	card := p.card()

	// A Telegram user with no identity, and another household's user, cannot resolve it.
	stranger := newPipe(t)
	for name, from := range map[string]int64{"unlinked user": p.tgUser + 424242, "other household user": stranger.tgUser} {
		if _, err := p.press("review:dispute:confirm", card, from); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p.confirmed() != 1 || p.staged() != 1 {
			t.Fatalf("%s resolved the dispute", name)
		}
	}
	// A callback against a different message id is stale.
	if _, err := p.press("review:dispute:confirm", card+1000, p.tgUser); err != nil || p.confirmed() != 1 {
		t.Fatalf("wrong message id: err=%v confirmed=%d", err, p.confirmed())
	}

	// Parallel confirm x2 + reject: exactly one outcome.
	var wg sync.WaitGroup
	for _, data := range []string{"review:dispute:confirm", "review:dispute:confirm", "review:dispute:reject"} {
		wg.Add(1)
		go func(data string) {
			defer wg.Done()
			_, _ = p.press(data, card, p.tgUser)
		}(data)
	}
	wg.Wait()
	confirmed, voided := p.confirmed(), p.n(`SELECT count(*) FROM transaction WHERE household_id=$1 AND status='VOIDED'`, p.household)
	switch {
	case confirmed == 2 && voided == 0 && p.str(`SELECT metadata_json->>'link_status' FROM transaction_evidence WHERE source_event_id=$1::uuid AND transaction_id=$2::uuid`, second.source, original) == "SUPERSEDED":
	case confirmed == 1 && voided == 1 && p.str(`SELECT COALESCE(metadata_json->>'link_status','ACTIVE') FROM transaction_evidence WHERE source_event_id=$1::uuid AND transaction_id=$2::uuid`, second.source, original) == "ACTIVE":
	default:
		t.Fatalf("inconsistent race outcome: confirmed=%d voided=%d", confirmed, voided)
	}
	if got := p.snapshot(original); got.status != before.status || got.category != before.category || got.merchant != before.merchant || got.amount != before.amount {
		t.Fatalf("original transaction changed by the race: %+v -> %+v", before, got)
	}
}

func TestStagedDisputeCannotBeResolvedByTypedReplyOrForgedGenericCallbacks(t *testing.T) {
	p := newPipe(t)
	_, second, _ := p.mergedPair()
	if _, err := p.text("tapi ini transaksi baru", second.message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	card := p.card()

	// Forged generic duplicate callbacks are refused without retry-loop errors.
	for _, data := range []string{"review:dup:new", "review:dup:merge:0", "review:confirm"} {
		if _, err := p.press(data, card, p.tgUser); err != nil {
			t.Fatalf("%s: callback returned %v (would retry/fail the job)", data, err)
		}
		if p.confirmed() != 1 || p.staged() != 1 {
			t.Fatalf("%s resolved the staged dispute", data)
		}
	}

	// A typed reply to the card that asks the model to confirm/merge is refused explicitly.
	p.gw.calls = 0
	p.gw.agent = func(call int, _ gateway.AgentRequest) gateway.AgentResponse {
		if call%2 == 0 {
			return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "confirm", Name: "resolve_review", Arguments: []byte(`{"action":"CONFIRM","candidate_ref":null,"source_account_hint":null,"wealth_account_hint":null,"category_slug":null,"merchant":null,"description":null,"pay_date":null,"amount_idr":null,"transaction_at":null,"allocations":[]}`)}}}
		}
		return gateway.AgentResponse{Text: "Baik."}
	}
	source, err := p.text("ya benar", card, p.tgUser)
	if err != nil {
		t.Fatalf("typed reply failed the job: %v", err)
	}
	if p.confirmed() != 1 || p.staged() != 1 {
		t.Fatalf("typed reply resolved the staged dispute: confirmed=%d staged=%d", p.confirmed(), p.staged())
	}
	status := p.str(`SELECT public_context_json->>'status' FROM telegram_conversation_turn WHERE source_event_id=$1::uuid AND tool_name='resolve_review' ORDER BY created_at DESC LIMIT 1`, source)
	if status != "EVIDENCE_DISPUTE_USE_BUTTONS" {
		t.Fatalf("typed reply status = %q, want EVIDENCE_DISPUTE_USE_BUTTONS", status)
	}
}
