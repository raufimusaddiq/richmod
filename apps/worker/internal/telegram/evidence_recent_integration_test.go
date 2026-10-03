package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// ageEvidence moves a document back in time, so recency is tested against the
// database clock the production query uses.
func ageEvidence(t *testing.T, ctx context.Context, f agentIntegrationFixture, documentID string, minutes int) {
	t.Helper()
	_, err := f.pool.Exec(ctx, `UPDATE document SET created_at=now()-make_interval(mins => $2) WHERE id=$1`, documentID, minutes)
	mustAgentTest(t, err)
}

func TestSingleFreshEvidenceBindsAsImmediateContext(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "recent-immediate")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	p := NewProcessor(f.pool, nil)

	evidence, ambiguous, err := p.bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update)
	mustAgentTest(t, err)
	if evidence == nil || ambiguous != nil || evidence.Document != canonicalDocumentID(a.documentID) {
		t.Fatalf("evidence=%+v ambiguous=%v, want the one fresh document", evidence, ambiguous)
	}
	if evidence.Context["binding"] != evidenceBindingImmediate {
		t.Fatalf("binding = %v, want %s", evidence.Context["binding"], evidenceBindingImmediate)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM product_telemetry_event WHERE household_id=$1 AND action='RECENT_CONTEXT_BINDING'`, f.householdID); n != 1 {
		t.Fatalf("RECENT_CONTEXT_BINDING events = %d, want 1", n)
	}
}

func TestTwoRecentReceiptsAreAmbiguousAndNeverChosen(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "recent-ambiguous")
	seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	seedEvidence(t, ctx, f, "b", evidenceSeedOptions{amount: "83000", merchant: "Gacoan", messageID: 102})
	p := NewProcessor(f.pool, nil)

	evidence, ambiguous, err := p.bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update)
	mustAgentTest(t, err)
	if evidence != nil {
		t.Fatalf("two candidates and no reply: the server chose %+v", evidence)
	}
	if len(ambiguous) != 2 {
		t.Fatalf("ambiguous set = %d, want both candidates", len(ambiguous))
	}
	raw, _ := json.Marshal(ambiguous)
	for _, want := range []string{"Mirota", "Gacoan", "125000", "83000"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("the clarification set hides %q: %s", want, raw)
		}
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM product_telemetry_event WHERE household_id=$1 AND action='AMBIGUOUS_CONTEXT'`, f.householdID); n != 1 {
		t.Fatalf("AMBIGUOUS_CONTEXT events = %d, want 1", n)
	}
	// Choosing among evidence creates no state.
	if n := countRows(t, ctx, f, `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1)+(SELECT count(*) FROM review_item WHERE household_id=$1)`, f.householdID); n != 0 {
		t.Fatalf("ambiguity handling created %d rows", n)
	}
}

func TestRecencyWindowsDecideBindingDeterministically(t *testing.T) {
	ctx := context.Background()
	p := func(f agentIntegrationFixture) *Processor { return NewProcessor(f.pool, nil) }

	t.Run("one older receipt in the hour binds as RECENT", func(t *testing.T) {
		f := newAgentIntegrationFixture(t, "recent-hour")
		a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
		ageEvidence(t, ctx, f, a.documentID, 30)
		evidence, ambiguous, err := p(f).bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update)
		mustAgentTest(t, err)
		if evidence == nil || ambiguous != nil || evidence.Context["binding"] != evidenceBindingRecent {
			t.Fatalf("evidence=%+v ambiguous=%v, want RECENT binding", evidence, ambiguous)
		}
	})
	t.Run("a fresh receipt wins over an older one", func(t *testing.T) {
		f := newAgentIntegrationFixture(t, "recent-fresh-wins")
		old := seedEvidence(t, ctx, f, "old", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
		fresh := seedEvidence(t, ctx, f, "fresh", evidenceSeedOptions{amount: "83000", merchant: "Gacoan", messageID: 102})
		ageEvidence(t, ctx, f, old.documentID, 30)
		evidence, _, err := p(f).bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update)
		mustAgentTest(t, err)
		if evidence == nil || evidence.Document != canonicalDocumentID(fresh.documentID) || evidence.Context["binding"] != evidenceBindingImmediate {
			t.Fatalf("evidence=%+v, want the fresh receipt bound IMMEDIATE", evidence)
		}
	})
	t.Run("two older receipts and none fresh are ambiguous", func(t *testing.T) {
		f := newAgentIntegrationFixture(t, "recent-two-old")
		a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
		b := seedEvidence(t, ctx, f, "b", evidenceSeedOptions{amount: "83000", merchant: "Gacoan", messageID: 102})
		ageEvidence(t, ctx, f, a.documentID, 20)
		ageEvidence(t, ctx, f, b.documentID, 40)
		evidence, ambiguous, err := p(f).bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update)
		mustAgentTest(t, err)
		if evidence != nil || len(ambiguous) != 2 {
			t.Fatalf("evidence=%+v ambiguous=%d, want an ambiguous pair", evidence, len(ambiguous))
		}
	})
	t.Run("evidence older than the window is ignored", func(t *testing.T) {
		f := newAgentIntegrationFixture(t, "recent-expired")
		a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
		ageEvidence(t, ctx, f, a.documentID, 61)
		evidence, ambiguous, err := p(f).bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update)
		mustAgentTest(t, err)
		if evidence != nil || ambiguous != nil {
			t.Fatalf("stale evidence bound: %+v %v", evidence, ambiguous)
		}
	})
	t.Run("the carried set is bounded", func(t *testing.T) {
		f := newAgentIntegrationFixture(t, "recent-bounded")
		for i, label := range []string{"a", "b", "c", "d", "e"} {
			seedEvidence(t, ctx, f, label, evidenceSeedOptions{amount: "1000", merchant: "x", messageID: int64(101 + i)})
		}
		evidence, ambiguous, err := p(f).bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update)
		mustAgentTest(t, err)
		if evidence != nil || len(ambiguous) != maxRecentEvidence {
			t.Fatalf("evidence=%+v ambiguous=%d, want %d", evidence, len(ambiguous), maxRecentEvidence)
		}
	})
}

func TestRecentEvidenceIsScopedToTheChatHouseholdAndUsableDocuments(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "recent-scope")
	other := newAgentIntegrationFixture(t, "recent-scope-other")
	seedEvidence(t, ctx, other, "theirs", evidenceSeedOptions{amount: "999000", merchant: "Other", messageID: 101})
	failed := seedEvidence(t, ctx, f, "failed", evidenceSeedOptions{amount: "1000", merchant: "x", messageID: 102})
	_, err := f.pool.Exec(ctx, `UPDATE document SET status='FAILED' WHERE id=$1`, failed.documentID)
	mustAgentTest(t, err)
	otherMember := seedEvidence(t, ctx, f, "member", evidenceSeedOptions{amount: "5000", merchant: "y", messageID: 103})
	_, err = f.pool.Exec(ctx, `UPDATE source_event SET telegram_chat_id=$2 WHERE id=$1`, otherMember.sourceID, f.chatID+5)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)

	evidence, ambiguous, err := p.bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update)
	mustAgentTest(t, err)
	if evidence != nil || ambiguous != nil {
		t.Fatalf("another household, a failed document, or another member's chat leaked in: %+v %v", evidence, ambiguous)
	}
}

func evidenceReadCall(t *testing.T, ref string) (gateway.ToolCall, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"evidence_ref": ref})
	call := gateway.ToolCall{CallID: "read-evidence", Name: "get_evidence_context", Arguments: raw}
	args, err := ValidateNativeToolCall(call)
	mustAgentTest(t, err)
	return call, args
}

func TestGetEvidenceContextToolIsAReadOnlyOpaqueRefLookup(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "recent-tool")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", linkTransaction: true})
	b := seedEvidence(t, ctx, f, "b", evidenceSeedOptions{amount: "83000", merchant: "Gacoan"})
	p := NewProcessor(f.pool, nil)

	// The tool is catalogued, classified READ, and survives a read-only catalog.
	if class, known := agentToolClassFor("get_evidence_context"); !known || class != agentToolRead {
		t.Fatalf("class=%s known=%v, want READ", class, known)
	}
	found := false
	for _, tool := range readOnlyAgentTools(NativeFinanceTools(nil, false, false, false, "", false, false, "")) {
		found = found || tool.Name == "get_evidence_context"
	}
	if !found {
		t.Fatal("get_evidence_context is missing from the read-only catalog")
	}

	// Argument validation: only an opaque ref, never a database id or free text.
	for _, bad := range []string{a.documentID, a.sourceID, "ev_1", "", "tx_1", "'; DROP TABLE document;--"} {
		raw, _ := json.Marshal(map[string]any{"evidence_ref": bad})
		if _, err := ValidateNativeToolCall(gateway.ToolCall{Name: "get_evidence_context", Arguments: raw}); err == nil {
			t.Fatalf("evidence_ref %q was accepted", bad)
		}
	}
	if _, err := ValidateNativeToolCall(gateway.ToolCall{Name: "get_evidence_context", Arguments: json.RawMessage(`{"evidence_ref":"a1b2c3d4_p0r0_ev1","document_id":"x"}`)}); err == nil {
		t.Fatal("an extra document_id argument was accepted")
	}

	// A turn first loads both documents, then re-reads only B. A's ref must keep
	// pointing at A: refs are keyed by document, not by call order.
	contexts, err := p.loadEvidenceContexts(ctx, f.householdID, f.sourceID, f.update, []canonicalDocumentID{canonicalDocumentID(a.documentID), canonicalDocumentID(b.documentID)})
	mustAgentTest(t, err)
	refA, refB := contexts[0]["evidence_ref"].(string), contexts[1]["evidence_ref"].(string)
	state := &agentState{SourceEventID: f.sourceID, HouseholdID: f.householdID, Update: f.update}
	call, args := evidenceReadCall(t, refB)
	result, err := p.executeAgentRead(ctx, state, call, args, "p1r1")
	mustAgentTest(t, err)
	if result.Class != agentToolRead || result.Status != "OK" || result.Facts["evidence"] == nil {
		t.Fatalf("read result = %+v", result)
	}
	if got, outcome, _ := p.resolveEvidenceRef(ctx, f.householdID, f.sourceID, f.update, evidenceRef(refA)); outcome != ceuResolved || string(got) != a.documentID {
		t.Fatalf("a mid-turn re-read re-pointed A's ref: %q/%s", got, outcome)
	}
	rawResult, _ := json.Marshal(result)
	if strings.Contains(string(rawResult), a.documentID) || strings.Contains(string(rawResult), b.documentID) || strings.Contains(string(rawResult), a.transactionID) {
		t.Fatalf("tool result leaks a canonical id: %s", rawResult)
	}

	// Expired and foreign refs are bounded answers with no side effect.
	_, err = f.pool.Exec(ctx, `UPDATE telegram_turn_reference SET expires_at=now()-interval '1 minute' WHERE household_id=$1 AND entity_type='EVIDENCE'`, f.householdID)
	mustAgentTest(t, err)
	state2 := `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1)+(SELECT count(*) FROM review_item WHERE household_id=$1)`
	before := countRows(t, ctx, f, state2, f.householdID)
	call, args = evidenceReadCall(t, refB)
	result, err = p.executeAgentRead(ctx, state, call, args, "p1r2")
	mustAgentTest(t, err)
	if result.Status != string(ceuReferenceExpired) {
		t.Fatalf("expired read status = %s, want REFERENCE_EXPIRED", result.Status)
	}
	other := newAgentIntegrationFixture(t, "recent-tool-other")
	foreign := &agentState{SourceEventID: other.sourceID, HouseholdID: other.householdID, Update: other.update}
	call, args = evidenceReadCall(t, refA)
	result, err = p.executeAgentRead(ctx, foreign, call, args, "p1r3")
	mustAgentTest(t, err)
	if result.Status != string(ceuReferenceInvalid) {
		t.Fatalf("foreign read status = %s, want REFERENCE_INVALID", result.Status)
	}
	if after := countRows(t, ctx, f, state2, f.householdID); before != after {
		t.Fatalf("a read changed state: %d -> %d", before, after)
	}
}

// ADR-050 binds "the newest evidence from this user in this chat". Evidence a
// different sender recorded must not bind, even when it shares the chat id.
func TestRecentEvidenceIsScopedToTheSendingUser(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "recent-user")
	mine := seedEvidence(t, ctx, f, "mine", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	theirs := seedEvidence(t, ctx, f, "theirs", evidenceSeedOptions{amount: "83000", merchant: "Gacoan", messageID: 102})
	// Same household and chat id, but sent by another Telegram user.
	_, err := f.pool.Exec(ctx, `UPDATE source_event_payload SET payload_json=payload_json||jsonb_build_object('telegram_user_id',$2::bigint) WHERE source_event_id=$1`, theirs.sourceID, f.chatID+9)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)

	evidence, ambiguous, err := p.bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update)
	mustAgentTest(t, err)
	if evidence == nil || ambiguous != nil || evidence.Document != canonicalDocumentID(mine.documentID) {
		t.Fatalf("evidence=%+v ambiguous=%v, want only this user's receipt", evidence, ambiguous)
	}

	// Older uploads that predate the recorded sender fall back to the chat id,
	// which equals the user id in a private chat.
	_, err = f.pool.Exec(ctx, `UPDATE source_event_payload SET payload_json=payload_json-'telegram_user_id' WHERE source_event_id=$1`, mine.sourceID)
	mustAgentTest(t, err)
	evidence, _, err = p.bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update)
	mustAgentTest(t, err)
	if evidence == nil || evidence.Document != canonicalDocumentID(mine.documentID) {
		t.Fatalf("a row without a recorded sender was not matched through the chat id: %+v", evidence)
	}
}

// Recent evidence is optional context. A broken lookup must degrade to "no
// context", never fail the household's message.
func TestBrokenRecentEvidenceLookupDegradesToNoContext(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "recent-broken")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	// A non-numeric sender makes the lookup's ::bigint cast fail.
	_, err := f.pool.Exec(ctx, `UPDATE source_event_payload SET payload_json=payload_json||'{"telegram_user_id":"not-a-number"}'::jsonb WHERE source_event_id=$1`, a.sourceID)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)

	if _, _, err := p.bindRecentEvidence(ctx, f.householdID, f.sourceID, f.update); err == nil {
		t.Fatal("the poisoned row did not break the lookup; the test would prove nothing")
	}
	evidence, candidates := p.recentEvidenceContext(ctx, f.householdID, f.sourceID, f.update)
	if evidence != nil || candidates != nil {
		t.Fatalf("a failed lookup produced context: %+v %v", evidence, candidates)
	}
}
