package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type duplicateEvidenceSeed struct {
	evidence                                evidenceSeed
	newTransactionID, existingTransactionID string
	reviewID                                string
}

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
	_, err = f.pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id,delivery_status) VALUES($1,$2,$3,'SENT')`, s.reviewID, f.chatID, cardMessage)
	mustAgentTest(t, err)
	return s
}

func mergeCallFromContext(t *testing.T, request gateway.AgentRequest) gateway.AgentResponse {
	t.Helper()
	ref := "a00000000_p0r0_tx1"
	content, _ := request.Content.(map[string]any)
	turn, _ := content["turn_context"].(map[string]any)
	bound, _ := turn["bound_evidence"].(map[string]any)
	workflow, _ := bound["workflow"].(map[string]any)
	if candidates, _ := workflow["candidates"].([]map[string]any); len(candidates) == 1 {
		ref, _ = candidates[0]["ref"].(string)
	}
	args, _ := json.Marshal(map[string]any{"action": "MERGE_EXISTING", "candidate_ref": ref})
	return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "merge", Name: "resolve_review", Arguments: args}}}
}

func (s duplicateEvidenceSeed) wantNothingMerged(t *testing.T, ctx context.Context, f agentIntegrationFixture) {
	t.Helper()
	var existingStatus, newStatus string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, s.existingTransactionID).Scan(&existingStatus))
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, s.newTransactionID).Scan(&newStatus))
	if existingStatus != "CONFIRMED" || newStatus != "NEEDS_REVIEW" {
		t.Fatalf("the assistant changed the duplicate: existing=%s new=%s", existingStatus, newStatus)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM review_item WHERE household_id=$1 AND status='OPEN'`, f.householdID); n != 1 {
		t.Fatalf("open reviews = %d, want the duplicate review still waiting", n)
	}
}

func TestReplyToADuplicateScreenshotShowsItsCardAndNeverMerges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "cand-card")
	s := seedDuplicateEvidence(t, ctx, f, "a", 101, 301)
	model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
		if call == 0 {
			return gateway.AgentResponse{ResponseID: "show", ToolCalls: []gateway.ToolCall{{CallID: "show", Name: toolShowReviewCard, Arguments: json.RawMessage(`{"card_ref":"k1","step":null}`)}}}
		}
		return gateway.AgentResponse{Text: "Kartunya sudah kukirim, tinggal diketuk."}
	}}
	p := evidenceTurnProcessor(f, model)

	_, err := runEvidenceTurn(t, ctx, f, p, "struk ini buat transaksi yang tadi", 101)
	mustAgentTest(t, err)
	names := model.toolNames(0)
	if !names[toolShowReviewCard] || names["resolve_review"] {
		t.Fatalf("the duplicate screenshot reply offered the wrong tools: %v", names)
	}
	raw, _ := json.Marshal(model.turnContext(t, 0))
	for name, id := range map[string]string{"existing transaction": s.existingTransactionID, "new transaction": s.newTransactionID, "review": s.reviewID, "document": s.evidence.documentID} {
		if strings.Contains(string(raw), id) {
			t.Fatalf("the turn context leaks the canonical %s id: %s", name, raw)
		}
	}
	if strings.Contains(string(raw), "candidate_ref") || strings.Contains(string(raw), `"candidates"`) || strings.Contains(string(raw), "allowed_actions") {
		t.Fatalf("the turn context still hands the assistant merge candidates or actions: %s", raw)
	}
	var shown int
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'assistant_shown'='true' AND payload_json->>'review_request_id'=$1`, s.reviewID).Scan(&shown))
	if shown != 1 {
		t.Fatalf("shown card jobs = %d, want 1", shown)
	}
	s.wantNothingMerged(t, ctx, f)
}

func TestAssistantCannotMergeADuplicateEvenWhenItTries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "cand-merge-refused")
	s := seedDuplicateEvidence(t, ctx, f, "a", 101, 301)
	model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
		if call == 0 {
			return mergeCallFromContext(t, request)
		}
		return gateway.AgentResponse{Text: "Tidak bisa digabung dari sini."}
	}}
	p := evidenceTurnProcessor(f, model)
	_, _ = runEvidenceTurn(t, ctx, f, p, "gabungkan struk ini", 101)
	s.wantNothingMerged(t, ctx, f)
}
