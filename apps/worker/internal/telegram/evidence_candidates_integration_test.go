package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type duplicateEvidenceSeed struct {
	evidence                                evidenceSeed
	newTransactionID, existingTransactionID string
	reviewID                                string
}

// seedDuplicateEvidence builds what the document pipeline leaves for a receipt
// that may repeat a recorded expense: a NEEDS_REVIEW transaction linked to the
// receipt, an open POSSIBLE_DUPLICATE review bound to a Telegram card, and one
// confirmed transaction (same type, amount, nearby time) it may merge into.
func seedDuplicateEvidence(t *testing.T, ctx context.Context, f agentIntegrationFixture, label string, uploadMessage, cardMessage int64) duplicateEvidenceSeed {
	t.Helper()
	var s duplicateEvidenceSeed
	s.evidence = seedEvidence(t, ctx, f, label, evidenceSeedOptions{amount: "57500", merchant: "Indomaret", messageID: uploadMessage})
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',57500,'IDR',now(),'Indomaret',$2) RETURNING id`, f.householdID, f.userID).Scan(&s.newTransactionID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,'IDR',now()-interval '2 hours','Indomaret Dago',$2,now()) RETURNING id`, f.householdID, f.userID).Scan(&s.existingTransactionID))
	_, err := f.pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'RECEIPT_IMAGE')`, s.newTransactionID, s.evidence.sourceID)
	mustAgentTest(t, err)
	decision := `{"version":1,"reasonCode":"POSSIBLE_DUPLICATE","allowedActions":["MERGE_EXISTING","CONFIRM_REVIEW","IGNORE"],"missingFacts":["duplicate_relationship"],"knownFacts":{},"decisionClass":"HUMAN_FACT","decisionSource":"GO","decisionPolicyVersion":"t","whyNotAutoConfirm":"t","interactionMode":"FREE_TEXT","subject":{"type":"transaction","id":"x"}}`
	var itemID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN',$3::jsonb) RETURNING id`, f.householdID, s.newTransactionID, decision).Scan(&itemID))
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'POSSIBLE_DUPLICATE','OPEN',$4) RETURNING id`, f.householdID, itemID, s.newTransactionID, f.chatID).Scan(&s.reviewID))
	_, err = f.pool.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, s.reviewID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, s.reviewID, f.chatID, cardMessage)
	mustAgentTest(t, err)
	return s
}

func TestPossibleDuplicateEvidenceOffersOpaqueCandidatesAndNoIds(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "cand-context")
	s := seedDuplicateEvidence(t, ctx, f, "a", 101, 301)
	p := NewProcessor(f.pool, nil)

	contexts, err := p.loadEvidenceContexts(ctx, f.householdID, f.sourceID, f.update, []canonicalDocumentID{canonicalDocumentID(s.evidence.documentID)})
	mustAgentTest(t, err)
	workflow, _ := contexts[0]["workflow"].(map[string]any)
	candidates, _ := workflow["candidates"].([]map[string]any)
	if workflow == nil || workflow["review_type"] != "POSSIBLE_DUPLICATE" || len(candidates) != 1 {
		t.Fatalf("workflow=%v, want one duplicate candidate", workflow)
	}
	raw, _ := json.Marshal(contexts[0])
	for name, id := range map[string]string{"existing transaction": s.existingTransactionID, "new transaction": s.newTransactionID, "review": s.reviewID, "document": s.evidence.documentID} {
		if strings.Contains(string(raw), id) {
			t.Fatalf("the candidate context leaks the canonical %s id: %s", name, raw)
		}
	}
	if !strings.Contains(candidates[0]["merchant"].(string), "<untrusted_ledger_text>Indomaret Dago</untrusted_ledger_text>") {
		t.Fatalf("candidate merchant is not wrapped as untrusted: %v", candidates[0])
	}
	ref, _ := candidates[0]["ref"].(string)
	if got, err := p.resolveTransactionReference(ctx, f.householdID, f.update, ref); err != nil || got != s.existingTransactionID {
		t.Fatalf("candidate ref resolves to %q (%v), want the existing transaction", got, err)
	}
	if !slicesContains(reviewActionsForType("POSSIBLE_DUPLICATE"), "MERGE_EXISTING") {
		t.Fatal("POSSIBLE_DUPLICATE does not offer MERGE_EXISTING")
	}
}

func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func mergeCallFromContext(t *testing.T, request gateway.AgentRequest, useRef string) gateway.AgentResponse {
	t.Helper()
	ref := useRef
	if ref == "" {
		content, _ := request.Content.(map[string]any)
		turn, _ := content["turn_context"].(map[string]any)
		bound, _ := turn["bound_evidence"].(map[string]any)
		workflow, _ := bound["workflow"].(map[string]any)
		candidates, _ := workflow["candidates"].([]map[string]any)
		if len(candidates) != 1 {
			t.Fatalf("the model was shown %d candidates, want 1: %v", len(candidates), workflow)
		}
		ref, _ = candidates[0]["ref"].(string)
	}
	args, _ := json.Marshal(map[string]any{"action": "MERGE_EXISTING", "candidate_ref": ref})
	return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "merge", Name: "resolve_review", Arguments: args}}}
}

func TestReceiptReplyMergesIntoTheNamedExistingTransactionWithoutADuplicate(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "cand-merge")
	s := seedDuplicateEvidence(t, ctx, f, "a", 101, 301)
	model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
		if call == 0 {
			return mergeCallFromContext(t, request, "")
		}
		return gateway.AgentResponse{Text: "Sudah digabung dengan transaksi yang tadi."}
	}}
	p, _ := evidenceTurnProcessor(f, model, "CORRECT_TRANSACTION")

	sourceID, err := runEvidenceTurn(t, ctx, f, p, "struk ini buat transaksi yang tadi", 101)
	mustAgentTest(t, err)
	if !model.toolNames(0)["resolve_review"] {
		t.Fatal("the evidence's duplicate review was not offered to the model")
	}
	var existingStatus, newStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, s.existingTransactionID).Scan(&existingStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, s.newTransactionID).Scan(&newStatus))
	if existingStatus != "CONFIRMED" || newStatus == "NEEDS_REVIEW" || newStatus == "CONFIRMED" {
		t.Fatalf("after the merge existing=%s new=%s, want the new row folded into the existing one", existingStatus, newStatus)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status IN ('CONFIRMED','NEEDS_REVIEW')`, f.householdID); n != 1 {
		t.Fatalf("live transactions = %d, want exactly the one existing record", n)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND evidence_type='RECEIPT_IMAGE'`, s.existingTransactionID); n < 1 {
		t.Fatal("the receipt did not become evidence on the existing transaction")
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM review_item WHERE household_id=$1 AND status IN ('OPEN','PENDING_SEND')`, f.householdID); n != 0 {
		t.Fatalf("open reviews after a merge = %d, want 0", n)
	}

	// Duplicate delivery of the same update changes nothing more.
	mustAgentTest(t, p.ProcessAgent(ctx, sourceID))
	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status IN ('CONFIRMED','NEEDS_REVIEW')`, f.householdID); n != 1 {
		t.Fatalf("a replayed update left %d live transactions", n)
	}
}

func TestMergeRefusesRefsThatAreNotTheReviewsCurrentCandidates(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "cand-refuse")
	s := seedDuplicateEvidence(t, ctx, f, "a", 101, 301)
	// A confirmed transaction of another amount is not a candidate, even though
	// the model holds a valid, unexpired ref to it.
	var otherID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',99000,'IDR',now(),'Other',$2,now()) RETURNING id`, f.householdID, f.userID).Scan(&otherID))
	p := NewProcessor(f.pool, nil)
	otherRefs, err := p.persistAgentTransactionReferences(ctx, f.householdID, f.sourceID, f.update, "p7r0", []string{otherID})
	mustAgentTest(t, err)
	foreign := newAgentIntegrationFixture(t, "cand-refuse-other")
	var foreignTx string
	mustAgentTest(t, foreign.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,'IDR',now(),'Foreign',$2,now()) RETURNING id`, foreign.householdID, foreign.userID).Scan(&foreignTx))
	foreignRefs, err := p.persistAgentTransactionReferences(ctx, foreign.householdID, foreign.sourceID, foreign.update, "p7r0", []string{foreignTx})
	mustAgentTest(t, err)

	for name, ref := range map[string]string{
		"valid ref to a non-candidate": otherRefs[0].Ref,
		"another household's ref":      foreignRefs[0].Ref,
		"a canonical id":               s.existingTransactionID,
		"a made-up ref":                "a00000000_p0r0_tx1",
		"empty":                        "",
	} {
		ref := ref
		model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
			if call == 0 {
				args, _ := json.Marshal(map[string]any{"action": "MERGE_EXISTING", "candidate_ref": ref})
				return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "merge", Name: "resolve_review", Arguments: args}}}
			}
			return gateway.AgentResponse{Text: "Tidak bisa digabung."}
		}}
		proc, _ := evidenceTurnProcessor(f, model, "CORRECT_TRANSACTION")
		_, _ = runEvidenceTurn(t, ctx, f, proc, fmt.Sprintf("gabungkan (%s)", name), 101)
		var newStatus, existingStatus string
		mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, s.newTransactionID).Scan(&newStatus))
		mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, s.existingTransactionID).Scan(&existingStatus))
		if newStatus != "NEEDS_REVIEW" || existingStatus != "CONFIRMED" {
			t.Fatalf("%s: new=%s existing=%s; a refused merge must change nothing", name, newStatus, existingStatus)
		}
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM review_item WHERE household_id=$1 AND status='OPEN'`, f.householdID); n != 1 {
		t.Fatalf("open reviews = %d, want the duplicate review still open", n)
	}
}

func TestMergeExistingIsOnlyAvailableForPossibleDuplicateReviews(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "cand-type")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	reviewID := linkReceiptReview(t, ctx, f, a, 301) // AMBIGUOUS_CATEGORY, not a duplicate
	var transactionID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT transaction_id FROM review_request WHERE id=$1`, reviewID).Scan(&transactionID))
	var otherID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,created_by_user_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',125000,'IDR',now(),'Mirota',$2,now()) RETURNING id`, f.householdID, f.userID).Scan(&otherID))
	p := NewProcessor(f.pool, nil)
	refs, err := p.persistAgentTransactionReferences(ctx, f.householdID, f.sourceID, f.update, "p7r0", []string{otherID})
	mustAgentTest(t, err)

	model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
		if call == 0 {
			return mergeCallFromContext(t, request, refs[0].Ref)
		}
		return gateway.AgentResponse{Text: "Tidak bisa."}
	}}
	proc, _ := evidenceTurnProcessor(f, model, "CORRECT_TRANSACTION")
	_, _ = runEvidenceTurn(t, ctx, f, proc, "gabungkan", 101)
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, transactionID).Scan(&status))
	if status != "NEEDS_REVIEW" {
		t.Fatalf("a non-duplicate review was merged: status=%s", status)
	}
}

// A merge and a refused merge must read differently even when the model call that
// would have worded them fails (AGENTS.md: deterministic flows survive an
// unavailable model).
func TestMergeOutcomesHaveDistinctDeterministicMessages(t *testing.T) {
	merged := agentMutationFallback(agentToolResult{Status: "RESOLVED", Mutation: map[string]any{"action": "DUPLICATE_MERGED"}})
	refused := agentMutationFallback(agentToolResult{Status: "INVALID_CANDIDATE"})
	generic := agentMutationFallback(agentToolResult{Status: "OK"})
	if merged == generic || refused == generic || merged == refused {
		t.Fatalf("merged=%q refused=%q generic=%q; each must be distinct", merged, refused, generic)
	}
	if !strings.Contains(merged, "digabung") || !strings.Contains(refused, "belum ada yang diubah") {
		t.Fatalf("merged=%q refused=%q", merged, refused)
	}
}
