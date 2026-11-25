package telegram_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

type disputeScene struct {
	p        *pipe
	original string
	request  string
	message  int64
	otherTG  int64
	other    int64
}

func (p *pipe) addMember() int64 {
	p.t.Helper()
	stamp := testsupport.UniqueStamp()
	var userID string
	p.must(p.pool.QueryRow(p.ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Member','unused') RETURNING id::text`, fmt.Sprintf("dispute-member-%d@example.test", stamp)).Scan(&userID))
	p.exec(`INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, p.household, userID)
	chat := 2_000_000_000 + stamp%1_000_000_000
	p.exec(`INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chat, p.household, userID)
	return chat
}

func newDisputeScene(t *testing.T, twoMembers bool) *disputeScene {
	t.Helper()
	p := newPipe(t)
	s := &disputeScene{p: p}
	if twoMembers {
		s.otherTG = p.addMember()
	}
	_, second, original := p.mergedPair()
	s.original = original
	if _, err := p.text("tapi ini transaksi baru", second.message, p.tgUser); err != nil {
		t.Fatal(err)
	}
	s.request = p.str(`SELECT r.id::text FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.household_id=$1 AND ri.decision->'decisionProvenance'->>'evidence_link_dispute'='true' ORDER BY r.created_at DESC LIMIT 1`, p.household)
	if s.request == "" {
		t.Fatal("the dispute produced no card")
	}
	p.seq += 2
	s.message = p.seq
	p.must(p.tg.BindReviewMessage(p.ctx, s.request, p.chat, s.message, s.cardText(p.chat)))
	if twoMembers {
		p.seq += 2
		s.other = p.seq
		p.must(p.tg.BindReviewMessage(p.ctx, s.request, s.otherTG, s.other, s.cardText(s.otherTG)))
	}
	return s
}

func (s *disputeScene) cardText(chat int64) string {
	return s.p.str(`SELECT payload_json->>'text' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id'=$1 AND (payload_json->>'chat_id')::bigint=$2 ORDER BY created_at LIMIT 1`, s.request, chat)
}

func (s *disputeScene) cardMarkup() string {
	return s.p.str(`SELECT payload_json->>'reply_markup' FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND payload_json->>'review_request_id'=$1 AND (payload_json->>'chat_id')::bigint=$2 ORDER BY created_at LIMIT 1`, s.request, s.p.chat)
}

func (s *disputeScene) edit(chat, message int64) string {
	return s.p.str(`SELECT payload_json->>'text' FROM job WHERE type='EDIT_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 ORDER BY created_at DESC,id DESC LIMIT 1`, chat, message)
}

func (s *disputeScene) latest() string {
	return s.p.str(`SELECT payload_json->>'text' FROM job WHERE type IN ('SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE') AND (payload_json->>'review_request_id'=$1 OR payload_json->>'outcome_request_id'=$1) ORDER BY created_at DESC,id DESC LIMIT 1`, s.request)
}

func (s *disputeScene) pressIn(chat int64, data string, message int64) (string, error) {
	s.p.t.Helper()
	key := fmt.Sprintf("cb-%d-%d", testsupport.UniqueStamp(), pressSequence.Add(1))
	raw := fmt.Sprintf(`{"callback_query":{"id":%q,"data":%q,"from":{"id":%d},"message":{"message_id":%d,"chat":{"id":%d}}}}`, key, data, chat, message, chat)
	var source string
	s.p.must(s.p.pool.QueryRow(s.p.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') RETURNING id::text`, s.p.household, key, []byte(key)).Scan(&source))
	s.p.exec(`INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, source, raw)
	return source, s.p.tg.Process(s.p.ctx, source)
}

func (s *disputeScene) item() string {
	return s.p.str(`SELECT ri.status FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1::uuid`, s.request)
}

func TestInteractionEngineDisputeContract(t *testing.T) {
	t.Run("the card is plain, offers confirm and cancel, and sends no reply instruction", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, false)
		text := s.cardText(s.p.chat)
		for _, want := range []string{"Screenshot ini transaksi yang berbeda?", "Alasan: ", "Pilih Ya hanya kalau ini memang transaksi lain."} {
			if !strings.Contains(text, want) {
				t.Fatalf("card lacks %q: %s", want, text)
			}
		}
		markup := s.cardMarkup()
		for _, want := range []string{"Ya, transaksi berbeda", "review:dispute:confirm", "Batalkan koreksi", "review:dispute:reject"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("markup lacks %q: %s", want, markup)
			}
		}
		if strings.Contains(markup, "Abaikan") || strings.Contains(text, "Balas pesan ini") {
			t.Fatalf("the dispute card offers ignoring or a reply instruction: %s %s", text, markup)
		}
	})
	t.Run("a confirm tap records the other transaction and settles the card", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, false)
		if _, err := s.pressIn(s.p.chat, "review:dispute:confirm", s.message); err != nil {
			t.Fatal(err)
		}
		if got := s.edit(s.p.chat, s.message); !strings.Contains(got, "✅ Transaksi berbeda dicatat.") {
			t.Fatalf("the card says %q", got)
		}
		if s.p.confirmed() != 2 || s.p.staged() != 0 || s.item() != "RESOLVED" {
			t.Fatalf("confirmed=%d staged=%d item=%s", s.p.confirmed(), s.p.staged(), s.item())
		}
	})
	t.Run("a cancel tap keeps the old link and settles the card", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, false)
		before := s.p.snapshot(s.original)
		if _, err := s.pressIn(s.p.chat, "review:dispute:reject", s.message); err != nil {
			t.Fatal(err)
		}
		if got := s.edit(s.p.chat, s.message); !strings.Contains(got, "✅ Koreksi dibatalkan.") {
			t.Fatalf("the card says %q", got)
		}
		if s.p.confirmed() != 1 || s.p.staged() != 0 || s.p.snapshot(s.original) != before {
			t.Fatalf("a cancel changed records: confirmed=%d staged=%d", s.p.confirmed(), s.p.staged())
		}
	})
	t.Run("the cancel label typed as a reply cancels", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, false)
		if _, err := s.p.text("Batalkan koreksi", s.message, s.p.tgUser); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(s.latest(), "Koreksi dibatalkan.") || s.p.staged() != 0 {
			t.Fatalf("typed label did not cancel: %q", s.latest())
		}
	})
	t.Run("the confirm label typed as a plain message confirms the single waiting card", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, false)
		if _, err := s.p.text("ya, transaksi berbeda", 0, s.p.tgUser); err != nil {
			t.Fatal(err)
		}
		if s.p.confirmed() != 2 {
			t.Fatalf("typed label did not confirm: confirmed=%d", s.p.confirmed())
		}
	})
	t.Run("a plain yes shows the question again and records nothing", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, false)
		if _, err := s.p.text("ya", 0, s.p.tgUser); err != nil {
			t.Fatal(err)
		}
		if got := s.latest(); !strings.Contains(got, "Maaf, aku belum menangkap itu. Screenshot ini transaksi yang berbeda?") {
			t.Fatalf("the card says %q", got)
		}
		if s.p.confirmed() != 1 || s.p.staged() != 1 {
			t.Fatalf("a plain yes resolved the dispute: confirmed=%d staged=%d", s.p.confirmed(), s.p.staged())
		}
	})
	t.Run("a question goes to the assistant and leaves the card alone", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, false)
		if _, err := s.p.text("kenapa harus dikonfirmasi?", s.message, s.p.tgUser); err != nil {
			t.Fatal(err)
		}
		s.p.gw.mu.Lock()
		calls := s.p.gw.calls
		s.p.gw.mu.Unlock()
		if calls == 0 {
			t.Fatal("the question never reached the assistant")
		}
		if got := s.edit(s.p.chat, s.message); got != "" {
			t.Fatalf("the card changed: %q", got)
		}
		if s.p.confirmed() != 1 || s.p.staged() != 1 {
			t.Fatal("a question resolved the dispute")
		}
	})
	t.Run("with two cards waiting a plain message that is not a label goes to the assistant", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, false)
		s.p.exec(`INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',9000,now())`, s.p.household)
		if _, err := s.p.text("berapa pengeluaranku minggu ini?", 0, s.p.tgUser); err != nil {
			t.Fatal(err)
		}
		s.p.gw.mu.Lock()
		calls := s.p.gw.calls
		s.p.gw.mu.Unlock()
		if calls == 0 || s.p.confirmed() != 1 {
			t.Fatalf("plain chat was swallowed: calls=%d", calls)
		}
	})
	t.Run("a button from another card's action is refused and the question is shown again", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, false)
		for _, data := range []string{"review:dup:new", "review:dupm:1000", "review:confirm", "review:ignore"} {
			if _, err := s.pressIn(s.p.chat, data, s.message); err != nil {
				t.Fatalf("%s: %v", data, err)
			}
		}
		if got := s.edit(s.p.chat, s.message); !strings.Contains(got, "Screenshot ini transaksi yang berbeda?") || !strings.Contains(got, "tidak") {
			t.Fatalf("the card says %q", got)
		}
		if s.p.confirmed() != 1 || s.p.staged() != 1 || s.item() == "RESOLVED" {
			t.Fatal("a foreign button resolved the dispute")
		}
	})
	t.Run("the other member's copy closes with the answer and a late tap there records nothing twice", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, true)
		if _, err := s.pressIn(s.p.chat, "review:dispute:confirm", s.message); err != nil {
			t.Fatal(err)
		}
		if s.p.n(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2`, s.otherTG, s.other) != 1 {
			t.Fatal("the other member's copy was not closed")
		}
		if _, err := s.pressIn(s.otherTG, "review:dispute:confirm", s.other); err != nil {
			t.Fatal(err)
		}
		if s.p.confirmed() != 2 {
			t.Fatalf("a late tap recorded again: confirmed=%d", s.p.confirmed())
		}
	})
	t.Run("two members answering at once record the other transaction once", func(t *testing.T) {
		t.Parallel()
		s := newDisputeScene(t, true)
		var wg sync.WaitGroup
		for _, target := range []struct {
			chat, message int64
		}{{s.p.chat, s.message}, {s.otherTG, s.other}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := s.pressIn(target.chat, "review:dispute:confirm", target.message); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if s.p.confirmed() != 2 || s.p.staged() != 0 || s.p.n(`SELECT count(*) FROM audit_log WHERE entity_type='transaction' AND action='CONFIRM_EVIDENCE_DISPUTE' AND household_id=$1`, s.p.household) != 1 {
			t.Fatalf("a double answer recorded twice: confirmed=%d staged=%d", s.p.confirmed(), s.p.staged())
		}
	})
}
