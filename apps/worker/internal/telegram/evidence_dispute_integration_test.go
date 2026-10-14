package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestExplicitNewTransactionDisputeOnMergedScreenshotStagesReviewOnly(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "dispute-staged")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "7500", merchant: "Test Merchant", messageID: 101, linkTransaction: true})
	// Make the seeded document look like an auto-merged single-row screenshot.
	_, err := f.pool.Exec(ctx, `UPDATE document SET document_type='EWALLET_SCREENSHOT' WHERE id=$1`, a.documentID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='MERGED',proposal_key='row-001',metadata_json=jsonb_build_object('document_id',$2::uuid,'row_index',0,'date_known',true,'matched_transaction_id',$3::uuid,'match_score',.95) WHERE source_event_id=$1`, a.sourceID, a.documentID, a.transactionID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE transaction_evidence SET evidence_type='TRANSACTION_SCREENSHOT',metadata_json=jsonb_build_object('proposal_id',(SELECT id FROM transaction_proposal WHERE source_event_id=$1)) WHERE source_event_id=$1`, a.sourceID)
	mustAgentTest(t, err)

	model := &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
		if call == 0 {
			return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "dispute", Name: "dispute_evidence_link", Arguments: []byte(`{}`)}}}
		}
		return gateway.AgentResponse{Text: "Koreksi menunggu konfirmasi."}
	}}
	p := evidenceTurnProcessor(f, model)
	confirmedBefore := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, f.householdID)

	_, err = runEvidenceTurn(t, ctx, f, p, "tapi ini transaksi baru", 101)
	mustAgentTest(t, err)

	if !model.toolNames(0)["dispute_evidence_link"] {
		t.Fatalf("evidence-bound catalog omitted the dispute tool: %v", model.toolNames(0))
	}
	if after := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, f.householdID); after != confirmedBefore {
		t.Fatalf("a dispute confirmed %d transaction(s) without a human button", after-confirmedBefore)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW'`, f.householdID); n != 1 {
		t.Fatalf("staged transactions = %d, want exactly one NEEDS_REVIEW", n)
	}
	var proposal string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT proposal_status FROM transaction_proposal WHERE source_event_id=$1`, a.sourceID).Scan(&proposal))
	if proposal != "MERGED" {
		t.Fatalf("original proposal mutated to %s", proposal)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM review_item WHERE household_id=$1 AND review_type='POSSIBLE_DUPLICATE' AND decision->'decisionProvenance'->>'evidence_link_dispute'='true' AND status IN ('OPEN','PENDING_SEND')`, f.householdID); n != 1 {
		t.Fatalf("dispute review items = %d, want 1", n)
	}
	var markup string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(string_agg(payload_json->>'reply_markup',' '),'') FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id' IN (SELECT id::text FROM review_request WHERE household_id=$1)`, f.householdID).Scan(&markup))
	if !strings.Contains(markup, "review:dispute:confirm") || strings.Contains(markup, "review:dup:") {
		t.Fatalf("dispute card must offer only dispute buttons: %s", markup)
	}

	// A retry of the same dispute does not stage a second row.
	_, err = runEvidenceTurn(t, ctx, f, p, "tapi ini transaksi baru", 101)
	mustAgentTest(t, err)
	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW'`, f.householdID); n != 1 {
		t.Fatalf("retried dispute staged %d transactions", n)
	}
}

func seedMergedDisputeEvidence(t *testing.T, ctx context.Context, f agentIntegrationFixture) evidenceSeed {
	t.Helper()
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "7500", merchant: "Test Merchant", messageID: 101, linkTransaction: true})
	_, err := f.pool.Exec(ctx, `UPDATE document SET document_type='EWALLET_SCREENSHOT' WHERE id=$1`, a.documentID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='MERGED',proposal_key='row-001',metadata_json=jsonb_build_object('document_id',$2::uuid,'row_index',0,'date_known',true,'matched_transaction_id',$3::uuid,'match_score',.95) WHERE source_event_id=$1`, a.sourceID, a.documentID, a.transactionID)
	mustAgentTest(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE transaction_evidence SET evidence_type='TRANSACTION_SCREENSHOT',metadata_json=jsonb_build_object('proposal_id',(SELECT id FROM transaction_proposal WHERE source_event_id=$1)) WHERE source_event_id=$1`, a.sourceID)
	mustAgentTest(t, err)
	return a
}

func disputeModel() *capturingGateway {
	return &capturingGateway{respond: func(call int, request gateway.AgentRequest) gateway.AgentResponse {
		if call == 0 {
			return gateway.AgentResponse{ToolCalls: []gateway.ToolCall{{CallID: "dispute", Name: "dispute_evidence_link", Arguments: []byte(`{}`)}}}
		}
		return gateway.AgentResponse{Text: "ok"}
	}}
}

func disputeToolStatus(t *testing.T, ctx context.Context, f agentIntegrationFixture, sourceID string) string {
	t.Helper()
	var status string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT COALESCE(public_context_json->>'status','') FROM telegram_conversation_turn WHERE source_event_id=$1 AND tool_name='dispute_evidence_link'`, sourceID).Scan(&status))
	return status
}

// Regression: production text intake leaves source_event.telegram_chat_id NULL, and
// authorization must not depend on it (it made every real dispute UNAUTHORIZED).
func TestDisputeAuthorizesWhenTextIntakeHasNoChatID(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "dispute-null-chat")
	seedMergedDisputeEvidence(t, ctx, f)
	p := evidenceTurnProcessor(f, disputeModel())
	sourceID, err := runEvidenceTurn(t, ctx, f, p, "tapi ini transaksi baru", 101)
	mustAgentTest(t, err)
	if n := countRows(t, ctx, f, `SELECT count(*) FROM source_event WHERE id=$1 AND telegram_chat_id IS NULL AND source_type='TELEGRAM_TEXT'`, sourceID); n != 1 {
		t.Fatal("fixture no longer mirrors production text intake (chat id must be NULL)")
	}
	if got := disputeToolStatus(t, ctx, f, sourceID); got != "EVIDENCE_DISPUTE_STAGED" {
		t.Fatalf("dispute tool status = %q, want EVIDENCE_DISPUTE_STAGED", got)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW'`, f.householdID); n != 1 {
		t.Fatalf("staged transactions = %d, want 1", n)
	}
}

func TestDisputeRefusesUnlinkedTelegramUserAndOtherHousehold(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "dispute-auth-neg")
	a := seedMergedDisputeEvidence(t, ctx, f)

	// A Telegram user with no active identity/membership in this household.
	stranger := f
	stranger.update.Message.From.ID = f.update.Message.From.ID + 7777777
	sourceID, err := runEvidenceTurn(t, ctx, stranger, evidenceTurnProcessor(stranger, disputeModel()), "tapi ini transaksi baru", 101)
	mustAgentTest(t, err)
	if got := disputeToolStatus(t, ctx, f, sourceID); got != "EVIDENCE_DISPUTE_UNAUTHORIZED" {
		t.Fatalf("stranger status = %q, want EVIDENCE_DISPUTE_UNAUTHORIZED", got)
	}

	// Another household replying to the same message id cannot reach this evidence.
	other := newAgentIntegrationFixture(t, "dispute-auth-other")
	otherSource, err := runEvidenceTurn(t, ctx, other, evidenceTurnProcessor(other, disputeModel()), "tapi ini transaksi baru", 101)
	mustAgentTest(t, err)
	if n := countRows(t, ctx, other, `SELECT count(*) FROM transaction WHERE household_id=$1`, other.householdID); n != 0 {
		t.Fatalf("other household staged %d transactions from foreign evidence (source %s)", n, otherSource)
	}

	if n := countRows(t, ctx, f, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW'`, f.householdID); n != 0 {
		t.Fatalf("refused disputes staged %d transactions", n)
	}
	var proposal string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT proposal_status FROM transaction_proposal WHERE source_event_id=$1`, a.sourceID).Scan(&proposal))
	if proposal != "MERGED" {
		t.Fatalf("original proposal mutated to %s", proposal)
	}
}
