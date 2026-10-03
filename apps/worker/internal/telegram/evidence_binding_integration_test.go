package telegram

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func replyUpdate(update telegramUpdate, messageID int64) telegramUpdate {
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: messageID}
	return update
}

type seedExpect struct{ documentID, amount string }

func TestReplyToUploadBindsOnlyThatEvidence(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "reply-upload")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	b := seedEvidence(t, ctx, f, "b", evidenceSeedOptions{amount: "83000", merchant: "Gacoan", messageID: 102})
	p := NewProcessor(f.pool, nil)

	for messageID, want := range map[int64]seedExpect{101: {a.documentID, "125000"}, 102: {b.documentID, "83000"}} {
		update := replyUpdate(f.update, messageID)
		got, found, err := p.resolveReplyEvidence(ctx, f.householdID, update)
		mustAgentTest(t, err)
		if !found || string(got) != want.documentID {
			t.Fatalf("reply to %d resolved %q found=%v, want %s", messageID, got, found, want.documentID)
		}
		evidence, review, err := p.bindTurnEvidence(ctx, f.householdID, f.sourceID, update, nil, false, true)
		mustAgentTest(t, err)
		if evidence == nil || evidence.Document != canonicalDocumentID(want.documentID) || review != nil {
			t.Fatalf("reply to %d bound %+v review=%+v", messageID, evidence, review)
		}
		observed, _ := evidence.Context["observed"].(map[string]any)
		if observed["amount_idr"] != want.amount {
			t.Fatalf("reply to %d carries amount %v, want %s", messageID, observed["amount_idr"], want.amount)
		}
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM product_telemetry_event WHERE household_id=$1 AND action='EXACT_REPLY_BINDING'`, f.householdID); n != 2 {
		t.Fatalf("EXACT_REPLY_BINDING events = %d, want 2", n)
	}
}

func TestUnresolvedReplyNeverFallsBackToRecentEvidence(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "reply-unresolved")
	seedEvidence(t, ctx, f, "recent", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	p := NewProcessor(f.pool, nil)

	update := replyUpdate(f.update, 999) // points at a message Richmod cannot resolve
	if _, found, err := p.resolveReplyEvidence(ctx, f.householdID, update); err != nil || found {
		t.Fatalf("unknown reply resolved: found=%v err=%v", found, err)
	}
	evidence, review, err := p.bindTurnEvidence(ctx, f.householdID, f.sourceID, update, nil, false, true)
	mustAgentTest(t, err)
	if evidence != nil || review != nil {
		t.Fatalf("an unresolved explicit reply fell back to recent evidence: %+v %+v", evidence, review)
	}
	// With no reply this slice binds nothing either: recency binding is CEU-03.
	evidence, _, err = p.bindTurnEvidence(ctx, f.householdID, f.sourceID, f.update, nil, false, false)
	mustAgentTest(t, err)
	if evidence != nil {
		t.Fatalf("a message with no reply bound evidence: %+v", evidence)
	}
}

func TestReplyBindingIsScopedToTheChat(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "reply-chat")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	b := seedEvidence(t, ctx, f, "b", evidenceSeedOptions{amount: "83000", merchant: "Gacoan", messageID: 101})
	// Another household member's private chat reuses message id 101.
	otherChat := f.chatID + 11
	_, err := f.pool.Exec(ctx, `UPDATE source_event SET telegram_chat_id=$2 WHERE id=$1`, b.sourceID, otherChat)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)

	got, found, err := p.resolveReplyEvidence(ctx, f.householdID, replyUpdate(f.update, 101))
	mustAgentTest(t, err)
	if !found || string(got) != a.documentID {
		t.Fatalf("own chat resolved %q, want %s", got, a.documentID)
	}
	memberUpdate := f.update
	memberUpdate.Message.Chat.ID = otherChat
	got, found, err = p.resolveReplyEvidence(ctx, f.householdID, replyUpdate(memberUpdate, 101))
	mustAgentTest(t, err)
	if !found || string(got) != b.documentID {
		t.Fatalf("other chat resolved %q, want %s", got, b.documentID)
	}
	stranger := f.update
	stranger.Message.Chat.ID = f.chatID + 99
	if _, found, _ := p.resolveReplyEvidence(ctx, f.householdID, replyUpdate(stranger, 101)); found {
		t.Fatal("a chat with no upload resolved someone else's message id")
	}
}

func TestAlbumMemberReplyResolvesToTheAlbumDocument(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "reply-album")
	first := seedEvidence(t, ctx, f, "first", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 201})
	// A later album image has its own source event and only a document_page row.
	var secondSource string
	stamp := time.Now().UnixNano()
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_message_id,telegram_chat_id,telegram_media_group_id) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSED',202,$4,'album-1') RETURNING id`,
		f.householdID, fmt.Sprintf("album-%d", stamp), []byte(fmt.Sprintf("album-%d", stamp)), f.chatID).Scan(&secondSource))
	_, err := f.pool.Exec(ctx, `INSERT INTO document_page(document_id,source_event_id,attachment_id,page_index) VALUES($1,$2,$3,1)`, first.documentID, secondSource, first.attachmentID)
	mustAgentTest(t, err)
	p := NewProcessor(f.pool, nil)

	for _, messageID := range []int64{201, 202} {
		got, found, err := p.resolveReplyEvidence(ctx, f.householdID, replyUpdate(f.update, messageID))
		mustAgentTest(t, err)
		if !found || string(got) != first.documentID {
			t.Fatalf("album reply to %d resolved %q found=%v, want the album document", messageID, got, found)
		}
	}
}

func TestReplyToBoundNoticeResolvesItsDocumentAndStaysHouseholdScoped(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "reply-notice")
	other := newAgentIntegrationFixture(t, "reply-notice-other")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota"})
	p := NewProcessor(f.pool, nil)

	// A retried send binds the same message twice; the second is a no-op.
	mustAgentTest(t, p.BindEvidenceMessage(ctx, f.chatID, 777, a.documentID))
	mustAgentTest(t, p.BindEvidenceMessage(ctx, f.chatID, 777, a.documentID))
	if n := countRows(t, ctx, f, `SELECT count(*) FROM telegram_message_binding WHERE telegram_chat_id=$1 AND telegram_message_id=777`, f.chatID); n != 1 {
		t.Fatalf("binding rows = %d, want 1", n)
	}
	got, found, err := p.resolveReplyEvidence(ctx, f.householdID, replyUpdate(f.update, 777))
	mustAgentTest(t, err)
	if !found || string(got) != a.documentID {
		t.Fatalf("notice reply resolved %q found=%v", got, found)
	}
	if _, found, _ := p.resolveReplyEvidence(ctx, other.householdID, replyUpdate(f.update, 777)); found {
		t.Fatal("another household resolved this household's notice")
	}
	// Zero ids and unknown documents never create a binding.
	mustAgentTest(t, p.BindEvidenceMessage(ctx, 0, 5, a.documentID))
	mustAgentTest(t, p.BindEvidenceMessage(ctx, f.chatID, 0, a.documentID))
	mustAgentTest(t, p.BindEvidenceMessage(ctx, f.chatID, 778, "00000000-0000-0000-0000-00000000dead"))
	if n := countRows(t, ctx, f, `SELECT count(*) FROM telegram_message_binding WHERE household_id=$1`, f.householdID); n != 1 {
		t.Fatalf("binding rows after invalid binds = %d, want 1", n)
	}
}

// Receipt reviews are keyed on their transaction and reach the evidence through
// transaction_evidence, not through review_item.document_id.
func linkReceiptReview(t *testing.T, ctx context.Context, f agentIntegrationFixture, e evidenceSeed, cardMessageID int64) (reviewID string) {
	t.Helper()
	review, transactionID := createAgentTransactionReview(t, ctx, f, "125000", cardMessageID)
	_, err := f.pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'RECEIPT_IMAGE')`, transactionID, e.sourceID)
	mustAgentTest(t, err)
	return review
}

func TestReplyToUploadBindsItsOpenReceiptReview(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "reply-review")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	// Another open review in the chat must not be chosen: the document decides.
	createAgentTransactionReview(t, ctx, f, "70000", 401)
	reviewID := linkReceiptReview(t, ctx, f, a, 301)
	p := NewProcessor(f.pool, nil)

	evidence, review, err := p.bindTurnEvidence(ctx, f.householdID, f.sourceID, replyUpdate(f.update, 101), nil, false, true)
	mustAgentTest(t, err)
	if evidence == nil || review == nil {
		t.Fatalf("evidence=%+v review=%+v, want both bound", evidence, review)
	}
	if review.ReviewRequestID != reviewID {
		t.Fatalf("bound review %s, want the document's review %s", review.ReviewRequestID, reviewID)
	}
	workflow, _ := evidence.Context["workflow"].(map[string]any)
	if workflow == nil || workflow["review_open"] != true {
		t.Fatalf("workflow block missing for a transaction-keyed review: %+v", evidence.Context)
	}
	if !evidence.HasLinkedTransaction {
		t.Fatal("the evidence's linked transaction was not reported")
	}
}

func TestReplyToReviewCardKeepsItsBindingAndAttachesEvidence(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "reply-card")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	reviewID := linkReceiptReview(t, ctx, f, a, 301)
	p := NewProcessor(f.pool, nil)

	update := replyUpdate(f.update, 301)
	binding, err := p.exactAgentReviewBinding(ctx, f.householdID, f.chatID, 301)
	mustAgentTest(t, err)
	evidence, review, err := p.bindTurnEvidence(ctx, f.householdID, f.sourceID, update, binding, false, true)
	mustAgentTest(t, err)
	if review == nil || review.ReviewRequestID != reviewID || evidence == nil || evidence.Document != canonicalDocumentID(a.documentID) {
		t.Fatalf("card reply bound evidence=%+v review=%+v", evidence, review)
	}
}

func TestUniqueActiveReviewGainsEvidenceContextOnly(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "reply-active")
	a := seedEvidence(t, ctx, f, "a", evidenceSeedOptions{amount: "125000", merchant: "Mirota", messageID: 101})
	linkReceiptReview(t, ctx, f, a, 301)
	p := NewProcessor(f.pool, nil)

	binding, _, count, err := p.loadAgentReviewBinding(ctx, f.householdID, f.update)
	mustAgentTest(t, err)
	if binding == nil || count != 1 {
		t.Fatalf("active review binding=%v count=%d", binding, count)
	}
	evidence, review, err := p.bindTurnEvidence(ctx, f.householdID, f.sourceID, f.update, binding, false, false)
	mustAgentTest(t, err)
	if evidence == nil || review != binding {
		t.Fatalf("active binding evidence=%+v review=%+v", evidence, review)
	}
	if n := countRows(t, ctx, f, `SELECT count(*) FROM product_telemetry_event WHERE household_id=$1 AND action='ACTIVE_REVIEW_BINDING'`, f.householdID); n != 1 {
		t.Fatalf("ACTIVE_REVIEW_BINDING events = %d, want 1", n)
	}
}

func TestEvidenceToolPolicyAddsOnlyTheLinkedTransactionCorrection(t *testing.T) {
	general := NativeFinanceTools(nil, false, false, false, "", false, false, "")
	unbound := replyUpdate(telegramUpdate{}, 5)
	filtered, scope := applyAgentWorkflowToolPolicy(general, unbound, nil, nil, "")
	if scope != agentWorkflowExplicitUnbound {
		t.Fatalf("setup scope = %s", scope)
	}
	sideEffects := func(name string) bool {
		class, known := agentToolClassFor(name)
		return known && class != agentToolRead
	}
	for _, tool := range filtered {
		if sideEffects(tool.Name) {
			t.Fatalf("an unbound reply exposes mutation tool %s", tool.Name)
		}
	}

	linked := &agentEvidenceBinding{HasLinkedTransaction: true}
	tools, scope := applyEvidenceToolPolicy(general, filtered, scope, linked)
	if scope != agentWorkflowExactEvidence {
		t.Fatalf("scope = %s, want EXACT_EVIDENCE", scope)
	}
	var mutations []string
	for _, tool := range tools {
		if sideEffects(tool.Name) {
			mutations = append(mutations, tool.Name)
		}
	}
	if len(mutations) != 1 || mutations[0] != "propose_transaction_correction" {
		t.Fatalf("evidence-bound mutations = %v, want only propose_transaction_correction", mutations)
	}

	unlinked, scope := applyEvidenceToolPolicy(general, filtered, agentWorkflowExplicitUnbound, &agentEvidenceBinding{})
	if scope != agentWorkflowExactEvidence {
		t.Fatalf("unlinked scope = %s", scope)
	}
	for _, tool := range unlinked {
		if sideEffects(tool.Name) {
			t.Fatalf("evidence with no linked transaction exposes mutation tool %s", tool.Name)
		}
	}
	// Any other scope, or no evidence, is returned untouched.
	if same, s := applyEvidenceToolPolicy(general, filtered, agentWorkflowExactReview, linked); s != agentWorkflowExactReview || len(same) != len(filtered) {
		t.Fatalf("an exact review scope was altered: %s %d", s, len(same))
	}
	if same, s := applyEvidenceToolPolicy(general, filtered, agentWorkflowExplicitUnbound, nil); s != agentWorkflowExplicitUnbound || len(same) != len(filtered) {
		t.Fatalf("a turn without evidence was altered: %s %d", s, len(same))
	}
}
