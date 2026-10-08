package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// capturingGateway is a scripted conversational model that records every request
// it was given, so a test can assert exactly what the model was shown.
type capturingGateway struct {
	requests []gateway.AgentRequest
	script   []gateway.AgentResponse
	err      error
	// respond, when set, decides each response from what the model was shown, so a
	// test can act on a ref the server issued this turn.
	respond func(call int, request gateway.AgentRequest) gateway.AgentResponse
}

func (*capturingGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	panic("evidence turns use the conversational agent path")
}

func (g *capturingGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.requests = append(g.requests, request)
	if g.err != nil {
		return gateway.AgentResponse{}, g.err
	}
	if g.respond != nil {
		return g.respond(len(g.requests)-1, request), nil
	}
	if len(g.requests) <= len(g.script) {
		return g.script[len(g.requests)-1], nil
	}
	return gateway.AgentResponse{Text: "Baik."}, nil
}

func (g *capturingGateway) turnContext(t *testing.T, call int) map[string]any {
	t.Helper()
	if len(g.requests) <= call {
		t.Fatalf("the model was called %d times, wanted call %d", len(g.requests), call)
	}
	content, _ := g.requests[call].Content.(map[string]any)
	turn, _ := content["turn_context"].(map[string]any)
	if turn == nil {
		t.Fatalf("model request %d has no turn_context: %#v", call, g.requests[call].Content)
	}
	return turn
}

func (g *capturingGateway) toolNames(call int) map[string]bool {
	names := map[string]bool{}
	for _, tool := range g.requests[call].Tools {
		names[tool.Name] = true
	}
	return names
}

// routeEngine is a Jev stand-in that always decides one route, and records the
// requests it was shown.
type routeEngine struct {
	route    string
	requests []judgment.Request
}

func (e *routeEngine) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	e.requests = append(e.requests, request)
	answers := map[string]judgment.Answer{}
	if question, ok := request.Questions["route"]; ok {
		if criteria, ok := question.Criteria.(map[string]any); ok {
			answers["route"] = confidentChoice(criteria, e.route)
		}
	}
	return judgment.Result{Model: "stub-jev", Answers: answers}, nil
}

var turnCounter int64

// runEvidenceTurn delivers one typed Telegram message through the real
// ProcessAgent lane, as a new source event.
func runEvidenceTurn(t *testing.T, ctx context.Context, f agentIntegrationFixture, p *Processor, text string, replyTo int64) (string, error) {
	t.Helper()
	turnCounter++
	update := f.update
	update.Message.Text = text
	update.Message.MessageID = 9000 + turnCounter
	if replyTo != 0 {
		update.Message.ReplyToMessage = &struct {
			MessageID int64 `json:"message_id"`
		}{MessageID: replyTo}
	}
	raw, err := json.Marshal(update)
	mustAgentTest(t, err)
	stamp := time.Now().UnixNano()
	var sourceID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_message_id,telegram_chat_id) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED',$4,$5) RETURNING id`,
		f.householdID, fmt.Sprintf("turn-%d-%d", stamp, turnCounter), []byte(fmt.Sprintf("turn-%d-%d", stamp, turnCounter)), update.Message.MessageID, f.chatID).Scan(&sourceID))
	_, err = f.pool.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(raw))
	mustAgentTest(t, err)
	return sourceID, p.ProcessAgent(ctx, sourceID)
}

func evidenceTurnProcessor(f agentIntegrationFixture, model *capturingGateway, route string) (*Processor, *routeEngine) {
	p := NewProcessor(f.pool, model)
	engine := &routeEngine{route: route}
	p.SetJudgment(engine)
	return p, engine
}

func TestTurnReplyToUploadShowsTheModelOnlyThatEvidence(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "turn-reply")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101, linkTransaction: true})
	seedEvidence(t, ctx, f, "b", evidenceSeedOptions{amount: "83000", merchant: "Gacoan", messageID: 102})
	model := &capturingGateway{}
	p, _ := evidenceTurnProcessor(f, model, "CORRECT_TRANSACTION")

	_, err := runEvidenceTurn(t, ctx, f, p, "makan", 101)
	mustAgentTest(t, err)
	turn := model.turnContext(t, 0)
	bound, _ := turn["bound_evidence"].(map[string]any)
	raw, _ := json.Marshal(turn)
	if bound == nil || !strings.Contains(string(raw), "Mirota") || strings.Contains(string(raw), "Gacoan") {
		t.Fatalf("the model was not shown exactly receipt A: %s", raw)
	}
	for _, id := range []string{a.documentID, a.sourceID, a.transactionID, f.householdID, f.userID} {
		if strings.Contains(string(raw), id) {
			t.Fatalf("the model-visible turn leaks a canonical id: %s", raw)
		}
	}
	if turn["explicit_reply_unbound"] != nil || turn["evidence_ambiguous"] != nil || turn["recent_evidence"] != nil {
		t.Fatalf("an exact reply carried unbound/ambiguity state: %v", turn)
	}
	// The user's own words stay data; the evidence is a separate, wrapped block.
	if text, _ := turn["current_user_text"].(string); !strings.Contains(text, "<untrusted_user_message>makan</untrusted_user_message>") {
		t.Fatalf("current_user_text = %q", text)
	}
	tools := model.toolNames(0)
	if !tools["propose_transaction_correction"] || tools["record_transaction"] || tools["resolve_review"] || !tools["get_evidence_context"] {
		t.Fatalf("evidence-bound tool catalog = %v", tools)
	}
}

func TestTurnWithoutReplyBindsUniqueRecentEvidenceAsContextOnly(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "turn-recent")
	seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101, linkTransaction: true})
	model := &capturingGateway{}
	p, _ := evidenceTurnProcessor(f, model, "CORRECT_TRANSACTION")

	_, err := runEvidenceTurn(t, ctx, f, p, "masukin ke makan aja", 0)
	mustAgentTest(t, err)
	turn := model.turnContext(t, 0)
	bound, _ := turn["bound_evidence"].(map[string]any)
	if bound == nil || bound["binding"] != evidenceBindingImmediate {
		t.Fatalf("bound_evidence = %v, want an IMMEDIATE binding", turn["bound_evidence"])
	}
	// Inferred binding is context: the tool catalog is the ordinary one.
	if model.toolNames(0)["resolve_review"] {
		t.Fatal("an inferred evidence binding exposed a review mutation tool")
	}
}

func TestTurnWithTwoRecentReceiptsCarriesCandidatesNotAChoice(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "turn-ambiguous")
	seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	seedEvidence(t, ctx, f, "b", evidenceSeedOptions{amount: "83000", merchant: "Gacoan", messageID: 102})
	model := &capturingGateway{}
	p, _ := evidenceTurnProcessor(f, model, "CORRECT_TRANSACTION")

	_, err := runEvidenceTurn(t, ctx, f, p, "yang ini makan", 0)
	mustAgentTest(t, err)
	turn := model.turnContext(t, 0)
	candidates, _ := turn["recent_evidence"].([]map[string]any)
	if turn["bound_evidence"] != nil || turn["evidence_ambiguous"] != true || len(candidates) != 2 {
		t.Fatalf("ambiguous turn bound_evidence=%v ambiguous=%v candidates=%d", turn["bound_evidence"], turn["evidence_ambiguous"], len(candidates))
	}
}

func TestBareAmountAfterAReceiptIsNeverHarvestedAsASecondTransaction(t *testing.T) {
	if (turnAgentContextState{HasRecentEvidence: true}).harvestable() {
		t.Fatal("a turn with recent evidence is still harvestable")
	}
	if !(turnAgentContextState{}).harvestable() {
		t.Fatal("an ordinary turn lost its fast path")
	}

	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "turn-bare-amount")
	seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101, linkTransaction: true})
	model := &capturingGateway{}
	// "nominalnya 125 ribu" is exactly what Jev would call a new transaction.
	p, engine := evidenceTurnProcessor(f, model, "CREATE_TRANSACTION")

	before := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID)
	_, err := runEvidenceTurn(t, ctx, f, p, "nominalnya 125 ribu", 0)
	mustAgentTest(t, err)
	if after := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID); after != before {
		t.Fatalf("a bare amount after a receipt created %d new transactions", after-before)
	}
	if len(engine.requests) == 0 {
		t.Fatal("Jev was not consulted")
	}
	state, _ := engine.requests[0].State.(map[string]any)
	if _, ok := state["amount_candidates"]; ok || state == nil {
		t.Fatalf("an amount candidate was harvested (or the state was unreadable) while evidence was in context: %v", engine.requests[0].State)
	}
	if len(model.requests) == 0 || model.turnContext(t, 0)["bound_evidence"] == nil {
		t.Fatal("the turn did not reach the agent with the bound evidence")
	}
}

func TestNaturalReplyResolvesTheReceiptReviewWithoutReaskingKnownFacts(t *testing.T) {
	for _, replyTo := range []int64{101, 0} {
		t.Run(fmt.Sprint(replyTo), func(t *testing.T) {
			testNaturalCategoryReviewReply(t, replyTo)
		})
	}
}

func testNaturalCategoryReviewReply(t *testing.T, replyTo int64) {
	t.Helper()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "turn-review")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	reviewID := linkReceiptReview(t, ctx, f, a, 301)
	var transactionID, categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT transaction_id FROM review_request WHERE id=$1`, reviewID).Scan(&transactionID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makan','makan') RETURNING id`, f.householdID).Scan(&categoryID))
	model := &capturingGateway{script: []gateway.AgentResponse{
		{ToolCalls: []gateway.ToolCall{{CallID: "c1", Name: "resolve_review", Arguments: json.RawMessage(`{"action":"CONFIRM","category_slug":"makan"}`)}}},
		{Text: "Sudah dicatat sebagai Makan."},
	}}
	route := "CORRECT_TRANSACTION"
	if replyTo == 0 {
		route = "REVIEW_INTERACTION"
	}
	p, engine := evidenceTurnProcessor(f, model, route)

	var amountBefore string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT amount::text FROM transaction WHERE id=$1`, transactionID).Scan(&amountBefore))
	before := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID)
	sourceID, err := runEvidenceTurn(t, ctx, f, p, "masukin ke makan aja", replyTo)
	mustAgentTest(t, err)
	if replyTo == 0 && engine.requests[0].State.(map[string]any)["active_review"].(map[string]any)["awaiting_field"] != "category_slug" {
		t.Fatal("standalone category answer lacks its awaited field")
	}

	turn := model.turnContext(t, 0)
	workflow, _ := turn["bound_evidence"].(map[string]any)["workflow"].(map[string]any)
	if workflow == nil || workflow["review_open"] != true || !model.toolNames(0)["resolve_review"] {
		t.Fatalf("the model was not given the evidence's review and its tool: workflow=%v tools=%v", workflow, model.toolNames(0))
	}
	var status, categorySlug, amountAfter string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT t.status,COALESCE(c.slug,''),t.amount::text FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.id=$1`, transactionID).Scan(&status, &categorySlug, &amountAfter))
	if status != "CONFIRMED" || categorySlug != "makan" {
		t.Fatalf("receipt review result status=%s category=%s", status, categorySlug)
	}
	if amountAfter != amountBefore {
		t.Fatalf("a category reply re-decided the known amount: %s -> %s", amountBefore, amountAfter)
	}
	if after := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID); after != before {
		t.Fatalf("the correction created a second transaction: %d -> %d", before, after)
	}
	// The reply itself is stored as text evidence; the receipt must stay linked once.
	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND evidence_type='RECEIPT_IMAGE'`, transactionID); n != 1 {
		t.Fatalf("receipt evidence links = %d, want 1", n)
	}

	// Duplicate delivery of the same Telegram update is a no-op.
	mustAgentTest(t, p.ProcessAgent(ctx, sourceID))
	if after := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.householdID); after != before {
		t.Fatalf("a replayed update changed the ledger: %d -> %d", before, after)
	}
}

func TestModelFailureOnAnEvidenceTurnCreatesNoReviewAndNoTransaction(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "turn-failure")
	seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101, linkTransaction: true})
	model := &capturingGateway{err: errors.New("provider unavailable")}
	p, _ := evidenceTurnProcessor(f, model, "CORRECT_TRANSACTION")

	state := `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1)+(SELECT count(*) FROM review_item WHERE household_id=$1)`
	before := countRows(t, ctx, f, state, f.householdID)
	_, _ = runEvidenceTurn(t, ctx, f, p, "makan", 101) // a model failure is not human work
	if after := countRows(t, ctx, f, state, f.householdID); after != before {
		t.Fatalf("a model failure changed ledger/review state: %d -> %d", before, after)
	}
}

// The prompt asks the model not to record a second transaction for a document that
// already has one. This proves the ledger, not the prompt: a model that records
// the receipt's amount again anyway must not produce a second ledger row, while a
// genuinely different purchase is left alone.
func TestModelRecordingTheReceiptAmountAgainIsRefusedByTheLedger(t *testing.T) {
	cases := []struct {
		name          string
		merchant      any
		ageMinutes    int
		wantExtraRows int
	}{
		{"same merchant", "Mirota", 0, 0},
		{"merchant spelled differently", "mirota swalayan", 0, 0},
		{"no merchant", nil, 0, 0},
		{"a different merchant, same price", "Gacoan", 0, 1},
		{"an old receipt is left to the existing edit-confirmation guard", "Mirota", 30, 0},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			f := newAgentIntegrationFixture(t, "turn-dup-"+strings.ReplaceAll(c.name, " ", "-"))
			a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101, linkTransaction: true})
			// A real receipt-created transaction carries its merchant.
			_, err := f.pool.Exec(ctx, "UPDATE transaction SET counterparty_name='Mirota' WHERE id=$1", a.transactionID)
			mustAgentTest(t, err)
			if c.ageMinutes > 0 {
				ageEvidence(t, ctx, f, a.documentID, c.ageMinutes)
			}
			args, _ := json.Marshal(map[string]any{
				"type": "EXPENSE", "amount_idr": "125000", "merchant": c.merchant, "category_slug": "dining-a",
				"description": "nominalnya 125 ribu", "note": nil, "date_reference": "TODAY", "date_provenance": "USER_STATED",
				"explicit_date": nil, "local_time": "12:30", "ambiguous": false, "confidence": 1.0, "category_confidence": 1.0,
			})
			model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
				if call == 0 {
					return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "again", Name: "record_transaction", Arguments: args}}}
				}
				return gateway.AgentResponse{Text: "Sudah dicatat."}
			}}
			p, _ := evidenceTurnProcessor(f, model, "CREATE_TRANSACTION")

			// The CEU guard itself only applies to fresh evidence: for an old receipt it
			// stands down, and the existing same-merchant edit guard answers instead.
			candidates, err := p.recentEvidenceCandidates(ctx, f.householdID, f.chatID, f.chatID)
			mustAgentTest(t, err)
			guard := p.evidenceAlreadyRecorded(ctx, f.householdID, freshEvidenceDocuments(candidates), "EXPENSE", "125000", fmt.Sprint(c.merchant))
			if wantGuard := c.ageMinutes == 0 && c.name != "a different merchant, same price"; guard != wantGuard && c.merchant != nil {
				t.Fatalf("%s: evidenceAlreadyRecorded = %v, want %v", c.name, guard, wantGuard)
			}
			live := "SELECT count(*) FROM transaction WHERE household_id=$1 AND status IN ('CONFIRMED','NEEDS_REVIEW')"
			before := countRows(t, ctx, f, live, f.householdID)
			_, _ = runEvidenceTurn(t, ctx, f, p, "nominalnya 125 ribu", 0)
			if extra := countRows(t, ctx, f, live, f.householdID) - before; extra != c.wantExtraRows {
				t.Fatalf("%s: %d extra live transaction(s), want %d", c.name, extra, c.wantExtraRows)
			}
		})
	}
}

// Only evidence from the immediate window takes the fast path away: an ordinary
// new expense typed long after an upload must not be slowed by it.
func TestOnlyFreshEvidenceSuppressesHarvest(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "turn-fresh")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	p := NewProcessor(f.pool, nil)

	fresh := func(user int64) []string {
		candidates, err := p.recentEvidenceCandidates(ctx, f.householdID, f.chatID, user)
		mustAgentTest(t, err)
		return freshEvidenceDocuments(candidates)
	}
	if len(fresh(f.chatID)) != 1 {
		t.Fatal("evidence from the last minute is not fresh")
	}
	ageEvidence(t, ctx, f, a.documentID, 30)
	if len(fresh(f.chatID)) != 0 {
		t.Fatal("evidence from 30 minutes ago still suppresses the fast path")
	}
	ageEvidence(t, ctx, f, a.documentID, 0)
	if len(fresh(f.chatID+9)) != 0 {
		t.Fatal("another user's evidence counted as fresh")
	}
}

// When the model call fails, the user must still be able to tell a refusal from a
// recording (AGENTS.md: deterministic flows survive an unavailable model).
func TestRefusedDuplicateHasADeterministicMessageThatIsNotTheGenericOne(t *testing.T) {
	refused := agentMutationFallback(agentToolResult{Status: "ALREADY_RECORDED_FROM_EVIDENCE"})
	generic := agentMutationFallback(agentToolResult{Status: "OK"})
	if refused == generic || !strings.Contains(refused, "sudah tercatat") || !strings.Contains(refused, "tidak dicatat lagi") {
		t.Fatalf("refusal message = %q (generic = %q)", refused, generic)
	}
}

// The merchant match is bounded: whole words only, and the shorter name must
// carry a distinctive word, so an unrelated merchant at the same price is not
// refused as a duplicate.
func TestMerchantEquivalenceIsBoundedToWholeDistinctiveWords(t *testing.T) {
	words := func(s string) []string { return strings.Fields(strings.ToLower(s)) }
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"Mirota", "Mirota Swalayan", true},
		{"mirota swalayan", "MIROTA", true},
		{"Indomaret", "Indomaret", true},
		{"Indo", "Indomaret", false},
		{"Toko", "Toko Jaya", false},
		{"Kopi Kenangan", "Kopi Tuku", false},
		{"Roti", "Roti", true},
		{"Gacoan", "Mirota", false},
		{"", "Mirota", false},
	} {
		if got := sameMerchantTokens(words(c.a), words(c.b)); got != c.want {
			t.Fatalf("sameMerchantTokens(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
