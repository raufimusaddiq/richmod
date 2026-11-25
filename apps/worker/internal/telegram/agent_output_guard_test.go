package telegram

import (
	"context"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
	"strings"
	"testing"
)

func TestGuardReplyPassesCleanText(t *testing.T) {
	clean := "Bulan ini kamu habis Rp2,4 juta buat makan, naik Rp300 ribu dari bulan lalu.\nMau kulihat yang terbesar?"
	got, fault := guardReply(clean, replyFallbackMessage)
	if fault != replyFaultNone || got != clean {
		t.Fatalf("clean reply changed: fault=%q got=%q", fault, got)
	}
}

func TestGuardReplyFaults(t *testing.T) {
	cases := []struct {
		name         string
		text         string
		fault        replyFault
		wantFallback bool
		wantContains string
	}{
		{"empty", "", replyFaultEmpty, true, ""},
		{"whitespace", " \n\t ", replyFaultEmpty, true, ""},
		{"dangling fragment only", "Masih perlu .", replyFaultBroken, true, ""},
		{"dangling fragment is dropped", "Sudah kucatat.\nMasih perlu .\nMau lanjut?", replyFaultBroken, false, "Mau lanjut?"},
		{"lone punctuation line is dropped", "Sudah kucatat.\n-\nMau lanjut?", replyFaultBroken, false, "Mau lanjut?"},
		{"truncated tail is dropped", "Totalnya Rp2,4 juta. Paling banyak di kopi dan", replyFaultBroken, false, "Totalnya Rp2,4 juta."},
		{"unmatched markup is stripped", "Totalnya **Rp2,4 juta.", replyFaultBroken, false, "Totalnya Rp2,4 juta."},
		{"code duplicating its label is stripped", "Ini pembelian aset (ASSET_PURCHASE) ya.", replyFaultCode, false, "pembelian aset ya."},
		{"known code becomes its label", "Jenisnya SAVINGS_TRANSFER.", replyFaultCode, false, "transfer ke tabungan"},
		{"unknown code falls back", "Statusnya FOO_BAR_BAZ sekarang.", replyFaultCode, true, ""},
		{"uuid falls back", "Transaksinya 0b8c1d2e-1111-4222-8333-444455556666 sudah ada.", replyFaultCode, true, ""},
		{"raw json falls back", `{"status":"OK","amount":"25000"}`, replyFaultCode, true, ""},
		{"mechanics sentence is dropped", "Sudah kucatat. Balas pesan ini untuk mengubahnya.", replyFaultMechanics, false, "Sudah kucatat."},
		{"mechanics only falls back", "(Reply) callback message id", replyFaultMechanics, true, ""},
		{"tombol di atas pesan ini", "Pakai tombol di atas pesan ini, ya.", replyFaultMechanics, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, fault := guardReply(tc.text, replyFallbackMessage)
			if fault != tc.fault {
				t.Fatalf("fault = %q, want %q (got %q)", fault, tc.fault, got)
			}
			if (got == replyFallbackMessage) != tc.wantFallback {
				t.Fatalf("fallback = %v, want %v (got %q)", got == replyFallbackMessage, tc.wantFallback, got)
			}
			if tc.wantContains != "" && !strings.Contains(got, tc.wantContains) {
				t.Fatalf("got %q, want it to contain %q", got, tc.wantContains)
			}
			if _, again := guardReply(got, replyFallbackMessage); again != replyFaultNone {
				t.Fatalf("guard output must itself be clean, got fault %q for %q", again, got)
			}
		})
	}
}

func TestOpenCardsHandOnlyPlainTopics(t *testing.T) {
	cards := plainOpenCards([]map[string]any{{"review_type": "CYCLE_RESIDUAL_ALLOCATION", "review_mode": "X", "binding": "reply_or_server_message", "amount_idr": "1000"}})
	if len(cards) != 1 || cards[0]["topic"] == "" || cards[0]["review_type"] != nil || cards[0]["binding"] != nil || cards[0]["amount_idr"] != "1000" {
		t.Fatalf("unexpected cards %#v", cards)
	}
}

func TestMutationOutcomeCopyChoosesByResult(t *testing.T) {
	if _, ok := mutationOutcomeCopy(agentToolResult{Status: "RESOLVED", Mutation: map[string]any{"action": "SOMETHING_NEW"}}); !ok {
		t.Fatal("a mutation must get fixed copy")
	}
	if _, ok := mutationOutcomeCopy(agentToolResult{Status: "SOMETHING", Facts: map[string]any{"reason": "x"}}); ok {
		t.Fatal("a result that carries facts to explain must reach the model")
	}
	if got, ok := mutationOutcomeCopy(agentToolResult{Status: "IGNORED"}); !ok || got != outcomeIgnoredMessage {
		t.Fatalf("ignored outcome = %q %v", got, ok)
	}
}

func TestMarkRepeatedQuestionOnlyWhenRepeated(t *testing.T) {
	turn := map[string]any{}
	markRepeatedQuestion(turn, false)
	if _, ok := turn["repeated_question"]; ok {
		t.Fatal("flag must be absent when the question is new")
	}
	markRepeatedQuestion(turn, true)
	if turn["repeated_question"] != true {
		t.Fatal("flag must be set when the question repeats")
	}
}

func TestRepliedGuardRecordsTelemetryAndNeverSendsBrokenText(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "guard-broken")
	p := NewProcessor(f.pool, &capturingGateway{script: []gateway.AgentResponse{{Text: "Masih perlu ."}}})
	_, err := sendText(t, ctx, f, p, "Kenapa bulan ini boros", 0)
	mustAgentTest(t, err)
	var reply string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT payload_json->>'text' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, f.chatID).Scan(&reply))
	if reply != replyFallbackMessage {
		t.Fatalf("broken model text was sent: %q", reply)
	}
	var source string
	mustAgentTest(t, f.pool.QueryRow(ctx, `SELECT decision_source FROM product_telemetry_event WHERE household_id=$1 AND event_type='INTERACTION' AND action='REPLY_REPAIRED'`, f.householdID).Scan(&source))
	if source != string(replyFaultBroken) {
		t.Fatalf("decision_source = %q", source)
	}
}
