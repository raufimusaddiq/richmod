package telegram

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type showCardGateway struct {
	*scriptedPicker
	args     string
	before   func()
	requests []gateway.AgentRequest
}

func (g *showCardGateway) AgentTurn(_ context.Context, _ string, request gateway.AgentRequest) (gateway.AgentResponse, error) {
	g.agentCalls++
	g.requests = append(g.requests, request)
	if len(request.ToolOutputs) == 0 {
		if g.before != nil {
			g.before()
		}
		return gateway.AgentResponse{ResponseID: "show", ToolCalls: []gateway.ToolCall{{CallID: "call-show", Name: toolShowReviewCard, Arguments: []byte(g.args)}}}, nil
	}
	return gateway.AgentResponse{Text: "Kartunya ada di chat, tinggal ketuk."}, nil
}

func (g *showCardGateway) offered(phase int) bool {
	return phase < len(g.requests) && slices.ContainsFunc(g.requests[phase].Tools, func(tool gateway.ToolDefinition) bool { return tool.Name == toolShowReviewCard })
}

func (g *showCardGateway) outcome(t *testing.T) string {
	t.Helper()
	for _, request := range g.requests {
		for _, output := range request.ToolOutputs {
			encoded, err := json.Marshal(output.Output)
			mustAgentTest(t, err)
			var decoded struct {
				Facts map[string]any `json:"facts"`
			}
			mustAgentTest(t, json.Unmarshal(encoded, &decoded))
			if value, _ := decoded.Facts["outcome"].(string); value != "" {
				if hint, _ := decoded.Facts["step_hint"].(string); hint != "" {
					return value + "/" + hint
				}
				return value
			}
		}
	}
	return ""
}

func (h *contractHarness) assistant(args string) *showCardGateway {
	gw := &showCardGateway{scriptedPicker: h.picker, args: args}
	h.p = NewProcessor(h.f.pool, gw)
	return gw
}

func (h *contractHarness) ask(text string) {
	h.t.Helper()
	h.chat(text)
	h.deliver()
}

func (h *contractHarness) shownCopies() int {
	h.t.Helper()
	var count int
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT count(*) FROM review_request_message WHERE review_request_id=$1 AND assistant_shown`, h.reviewID).Scan(&count))
	return count
}

func (h *contractHarness) wantShownJobs(want int) {
	h.t.Helper()
	h.wantJobs(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'assistant_shown'='true' AND (payload_json->>'chat_id')::bigint=ANY($1)`, want, h.chats())
}

func (h *contractHarness) wantAssistantAnswers(want int) {
	h.t.Helper()
	h.wantJobs(`SELECT count(*) FROM product_telemetry_event WHERE household_id=$1 AND event_type='INTERACTION' AND action='ANSWERED' AND decision_source='ASSISTANT_CARD'`, want, h.f.householdID)
}

func TestAssistantShowsWaitingCard(t *testing.T) {
	const transfer = "TRANSFER_CLASSIFICATION"
	unclassified := withTransactionType("UNCLASSIFIED")
	rows := []contractRow{
		{"the assistant shows a transfer card at the asset step and one tap finishes it", transfer, []contractOption{unclassified, withAssetAccounts("Emas", "Reksa Contoh")}, func(h *contractHarness) {
			gw := h.assistant(`{"card_ref":"k1","step":"asset"}`)
			original, _, _ := h.current()
			h.ask("mana kartunya tadi?")
			if !gw.offered(0) {
				h.t.Fatal("show_review_card was not offered while a card waits")
			}
			if got := gw.outcome(h.t); got != "card_shown/hint_applied" {
				h.t.Fatalf("tool outcome = %q", got)
			}
			shown, _, _ := h.current()
			if shown == original {
				h.t.Fatal("the card was not shown as a new message")
			}
			h.wantCard("Beli aset ke akun mana?", "Nominal: Rp25.000")
			h.wantButtons("Emas", "Reksa Contoh", "Kembali", "Abaikan")
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
			h.wantItemActive()
			if h.shownCopies() != 1 {
				h.t.Fatalf("shown copies = %d, want 1", h.shownCopies())
			}
			h.wantJobs(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'assistant_shown'='true' AND (payload_json->>'reply_to_message_id')::bigint>0 AND (payload_json->>'chat_id')::bigint=$1`, 1, h.f.chatID)
			h.tapLabel("Emas")
			h.wantCard("✅ Tersimpan: Beli aset · Emas · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Emas")
			h.wantItem("RESOLVED")
			h.wantAnswered("TAP")
			h.wantAssistantAnswers(1)
		}},
		{"a shown copy is answered by a reply", transfer, []contractOption{unclassified, withAssetAccountAt("Emas", "Antam")}, func(h *contractHarness) {
			h.assistant(`{"card_ref":"k1","step":"asset"}`)
			h.ask("mana kartunya tadi?")
			h.reply("Antam")
			h.wantCard("✅ Tersimpan: Beli aset · Emas · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Emas")
			h.wantAssistantAnswers(1)
		}},
		{"a shown copy is answered by a thread reply", transfer, []contractOption{unclassified, withAssetAccountAt("Emas", "Antam")}, func(h *contractHarness) {
			h.assistant(`{"card_ref":"k1","step":"asset"}`)
			h.ask("mana kartunya tadi?")
			h.threadReply("Antam")
			h.wantCard("✅ Tersimpan: Beli aset · Emas · Rp25.000")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Emas")
			h.wantAnswered("THREAD_REPLY")
			h.wantAssistantAnswers(1)
		}},
		{"a tap on the original copy after the shown copy records nothing and the shown copy still finishes it", transfer, []contractOption{unclassified, withAssetAccounts("Emas")}, func(h *contractHarness) {
			h.assistant(`{"card_ref":"k1","step":"asset"}`)
			original, originalText, _ := h.current()
			h.ask("mana kartunya tadi?")
			h.tapOn(h.f.chatID, original, originalText, callbackOwn)
			h.wantType("UNCLASSIFIED", "NEEDS_REVIEW")
			h.wantItemActive()
			h.wantAssistantAnswers(0)
			h.tapLabel("Emas")
			h.wantTransfer("TRANSFER", "CONFIRMED", "ASSET_PURCHASE", "Emas")
			h.wantAssistantAnswers(1)
		}},
		{"a hint that would skip a required answer is refused and the card shows at its current step", "UNKNOWN_MERCHANT", nil, func(h *contractHarness) {
			gw := h.assistant(`{"card_ref":"k1","step":"category"}`)
			h.ask("mana kartunya tadi?")
			if got := gw.outcome(h.t); got != "card_shown/hint_not_applied" {
				h.t.Fatalf("tool outcome = %q", got)
			}
			h.wantCard("Ini bayar ke mana?")
			h.wantTransaction("NEEDS_REVIEW", "", "")
			h.wantItemActive()
		}},
		{"a finished card is reported as finished and nothing is sent", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			gw := h.assistant(`{"card_ref":"k1","step":null}`)
			gw.before = func() {
				h.exec(`UPDATE review_item SET status='RESOLVED',resolved_at=now() WHERE id=(SELECT review_item_id FROM review_request WHERE id=$1)`, h.reviewID)
			}
			h.ask("mana kartunya tadi?")
			if got := gw.outcome(h.t); got != "already_finished" {
				h.t.Fatalf("tool outcome = %q", got)
			}
			h.wantShownJobs(0)
		}},
		{"no waiting card means the tool is not offered", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			gw := h.assistant(`{"card_ref":"k1","step":null}`)
			h.tapLabel("Ke rekening sendiri")
			h.wantItem("RESOLVED")
			h.ask("mana kartunya tadi?")
			if len(gw.requests) == 0 {
				h.t.Fatal("the assistant never ran")
			}
			if gw.offered(0) {
				h.t.Fatal("show_review_card was offered with no waiting card")
			}
		}},
		{"a repeat show within the window is not sent again", transfer, []contractOption{unclassified, withAssetAccounts("Emas")}, func(h *contractHarness) {
			gw := h.assistant(`{"card_ref":"k1","step":"asset"}`)
			h.ask("mana kartunya tadi?")
			shown, _, _ := h.current()
			gw.requests = nil
			h.ask("kartunya mana ya?")
			if got := gw.outcome(h.t); got != "already_shown_recently" {
				h.t.Fatalf("tool outcome = %q", got)
			}
			if after, _, _ := h.current(); after != shown {
				h.t.Fatalf("the card was sent again: %d then %d", shown, after)
			}
			h.wantShownJobs(1)
		}},
		{"answering the shown copy updates then closes the other member's copy", transfer, []contractOption{unclassified, withAssetAccounts("Emas"), withSecondMember()}, func(h *contractHarness) {
			h.assistant(`{"card_ref":"k1","step":"asset"}`)
			messageB, _, _ := h.card(h.chatB)
			if messageB == 0 {
				h.t.Fatal("the second member never received the card")
			}
			h.ask("mana kartunya tadi?")
			if text := h.texts[messageB]; !strings.Contains(text, "Beli aset ke akun mana?") {
				h.t.Fatalf("the other member's copy still shows the old step: %q", text)
			}
			h.tapLabel("Emas")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 AND payload_json->>'status'='RESOLVED'`, 1, h.chatB, messageB)
		}},
		{"an answer on an ordinary copy carries no assistant source", transfer, []contractOption{unclassified}, func(h *contractHarness) {
			h.tapLabel("Ke rekening sendiri")
			h.wantAnswered("TAP")
			h.wantAssistantAnswers(0)
		}},
	}
	runContractRows(t, rows)
}
