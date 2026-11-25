package telegram

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

type evidenceSetup struct {
	onTransaction bool
	candidates    int
	noCategory    bool
	missing       []string
	knownAcct     bool
	noAccounts    bool
	accounts      []string
	wealth        []string
}

func onTransaction() contractOption {
	return func(h *contractHarness) { h.ev.onTransaction = true }
}

func withCandidates(count int) contractOption {
	return func(h *contractHarness) { h.ev.candidates = count }
}

func withoutCategory() contractOption {
	return func(h *contractHarness) { h.ev.noCategory = true }
}

func withEmailMissing(facts ...string) contractOption {
	return func(h *contractHarness) { h.ev.missing = facts }
}

func withKnownWealthAccount() contractOption {
	return func(h *contractHarness) { h.ev.knownAcct = true }
}

func withNamedAccounts(names ...string) contractOption {
	return func(h *contractHarness) { h.ev.accounts = names }
}

func withNamedWealth(names ...string) contractOption {
	return func(h *contractHarness) { h.ev.wealth = names }
}

func (h *contractHarness) openEvidenceReview() bool {
	switch h.kind {
	case "POSSIBLE_DUPLICATE", "RECEIPT_MISMATCH", "MANUAL_CORRECTION":
		h.openTransactionEvidence()
	case "CONFLICTING_EVIDENCE":
		if h.ev.onTransaction {
			h.openTransactionEvidence()
		} else {
			h.openEmailEvidence()
		}
	case "FINANCIAL_EMAIL_RESOLUTION", "FINANCIAL_EMAIL_FACTS":
		h.openEmailEvidence()
	case "WEALTH_OBSERVATION_CONFIRMATION":
		h.openWealthEvidence()
	case "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", "DOCUMENT_CLASSIFICATION":
		h.openDocumentEvidence()
	default:
		return false
	}
	return true
}

func (h *contractHarness) openTransactionEvidence() {
	h.t.Helper()
	var merchant, category string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Warung Contoh') RETURNING id`, h.f.householdID).Scan(&merchant))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM category WHERE household_id=$1 AND slug='makan'`, h.f.householdID).Scan(&category))
	withCategory := category
	if (h.kind == "POSSIBLE_DUPLICATE" || h.kind == "CONFLICTING_EVIDENCE") && !h.categorized {
		withCategory = ""
	}
	if h.ev.noCategory {
		withCategory = ""
	}
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,category_id,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',25000,'IDR',now(),$2,NULLIF($3,'')::uuid,$4) RETURNING id`, h.f.householdID, merchant, withCategory, h.f.userID).Scan(&h.txID))
	for index := 0; index < h.ev.candidates; index++ {
		h.exec(`INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,category_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',25000,'IDR',now()-make_interval(hours => $4::int),$2,$3,now())`, h.f.householdID, merchant, category, 3+index)
	}
	decision, ok := reviewdec.Preset(h.kind, "transaction", h.txID)
	if !ok {
		h.t.Fatalf("no preset for %s", h.kind)
	}
	tx, err := h.f.pool.Begin(h.ctx)
	mustAgentTest(h.t, err)
	mustAgentTest(h.t, EnqueueReviewRequest(h.ctx, tx, h.txID, h.kind, h.f.chatID, 0, "Nominal: Rp25.000\nMerchant: Warung Contoh", decision))
	mustAgentTest(h.t, tx.Commit(h.ctx))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM review_request WHERE transaction_id=$1`, h.txID).Scan(&h.reviewID))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM review_item WHERE transaction_id=$1`, h.txID).Scan(&h.itemID))
	h.deliver()
}

func (h *contractHarness) openEmailEvidence() {
	h.t.Helper()
	stamp := testsupport.UniqueStamp()
	accounts, wealth := h.ev.accounts, h.ev.wealth
	if len(accounts) == 0 {
		accounts = []string{"Rekening A", "Rekening B"}
	}
	if len(wealth) == 0 {
		wealth = []string{"Emas Contoh", "Reksa Contoh"}
	}
	for _, name := range accounts {
		h.exec(`INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,$2,'BANK','FULL_LEDGER') ON CONFLICT(household_id,name) DO NOTHING`, h.f.householdID, name)
	}
	for _, name := range wealth {
		h.exec(`INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,$2,'ASSET','GOLD','OTHER')`, h.f.householdID, name)
	}
	var source, observation string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'FINANCIAL_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, h.f.householdID, fmt.Sprintf("contract-email-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&source))
	knownAccount := ""
	if len(h.ev.missing) == 1 && h.ev.missing[0] == "wealth_account" {
		knownAccount = accounts[0]
	}
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status,resolved_account_id) VALUES($1,$2,0,'CASH_MOVEMENT','{"amount_idr":"150000","funding_account_hint":"Rekening Utama","provider_account_hint":"Emas Digital","occurred_at":"2026-11-20T08:00:00+07:00"}'::jsonb,'REVIEW',(SELECT id FROM account WHERE household_id=$1 AND name=NULLIF($3,''))) RETURNING id`, h.f.householdID, source, knownAccount).Scan(&observation))
	decision, ok := reviewdec.Preset(h.kind, "financial_email_observation", observation)
	if !ok {
		h.t.Fatalf("no preset for %s", h.kind)
	}
	decision.SourceEventID = source
	if h.kind == "CONFLICTING_EVIDENCE" {
		decision.AllowedActions, decision.InteractionMode = []string{"IGNORE"}, reviewdec.ModeSingleField
	}
	if len(h.ev.missing) > 0 {
		decision.MissingFacts = h.ev.missing
	}
	encoded, err := decision.JSON()
	mustAgentTest(h.t, err)
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO review_item(household_id,financial_email_observation_id,review_type,status,decision) VALUES($1,$2,$3,'OPEN',$4::jsonb) RETURNING id`, h.f.householdID, observation, h.kind, string(encoded)).Scan(&h.itemID))
	h.caseID = observation
	h.project(h.itemID)
}

func (h *contractHarness) openWealthEvidence() {
	h.t.Helper()
	wealth := h.ev.wealth
	if len(wealth) == 0 {
		wealth = []string{"Emas Contoh", "Reksa Contoh"}
	}
	var known string
	for _, name := range wealth {
		var id string
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,$2,'ASSET','GOLD','OTHER') RETURNING id`, h.f.householdID, name).Scan(&id))
		if known == "" {
			known = id
		}
	}
	var observation string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO wealth_observation(household_id,institution,account_hint,observed_value_idr,observed_date,status) VALUES($1,'Bank Contoh','Emas Digital',15000000,'2026-11-20','PENDING') RETURNING id`, h.f.householdID).Scan(&observation))
	decision, _ := reviewdec.Preset(h.kind, "wealth_observation", observation)
	decision.KnownFacts["observed_value_idr"] = "15000000"
	decision.KnownFacts["institution"] = "Bank Contoh"
	decision.KnownFacts["account_hint"] = "Emas Digital"
	if h.ev.knownAcct {
		decision.KnownFacts["wealth_account"] = known
	}
	encoded, err := decision.JSON()
	mustAgentTest(h.t, err)
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO review_item(household_id,wealth_observation_id,review_type,status,decision) VALUES($1,$2,$3,'OPEN',$4::jsonb) RETURNING id`, h.f.householdID, observation, h.kind, string(encoded)).Scan(&h.itemID))
	h.caseID = observation
	h.project(h.itemID)
}

func (h *contractHarness) openDocumentEvidence() {
	h.t.Helper()
	stamp := testsupport.UniqueStamp()
	var image, attachment, document string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, h.f.householdID, fmt.Sprintf("contract-document-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&image))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, h.f.householdID, []byte(fmt.Sprint(stamp)), fmt.Sprintf("contract/%d.png", stamp)).Scan(&attachment))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'RECEIPT','NEEDS_REVIEW') RETURNING id`, h.f.householdID, image, attachment).Scan(&document))
	decision, _ := reviewdec.Preset(h.kind, "document", document)
	decision.SourceEventID = image
	encoded, err := decision.JSON()
	mustAgentTest(h.t, err)
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO review_item(household_id,source_event_id,document_id,review_type,status,decision) VALUES($1,$2,$3,$4,'OPEN',$5::jsonb) RETURNING id`, h.f.householdID, image, document, h.kind, string(encoded)).Scan(&h.itemID))
	h.imageID, h.caseID = image, document
	h.project(h.itemID)
}

func (h *contractHarness) wantItemResolution(action string) {
	h.t.Helper()
	var got string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT COALESCE(resolution_action,'') FROM review_item WHERE id=$1`, h.itemID).Scan(&got))
	if got != action {
		h.t.Fatalf("review item resolution = %q, want %q", got, action)
	}
}

func (h *contractHarness) openOtherCard() {
	h.t.Helper()
	var merchant string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Toko Lain') RETURNING id`, h.f.householdID).Scan(&merchant))
	var other string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,created_by_user_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',9000,'IDR',now(),$2,$3) RETURNING id`, h.f.householdID, merchant, h.f.userID).Scan(&other))
	tx, err := h.f.pool.Begin(h.ctx)
	mustAgentTest(h.t, err)
	mustAgentTest(h.t, EnqueueReviewRequest(h.ctx, tx, other, "AMBIGUOUS_CATEGORY", h.f.chatID, 0, "Nominal: Rp9.000"))
	mustAgentTest(h.t, tx.Commit(h.ctx))
	h.deliver()
}

func (h *contractHarness) seedTap(chat, messageID int64, text, data string) string {
	h.t.Helper()
	update := callbackUpdate(chat, messageID, data)
	update.CallbackQuery.Message.Text = text
	raw, err := json.Marshal(update)
	mustAgentTest(h.t, err)
	return h.f.seedSourceEvent(h.ctx, h.t, fmt.Sprintf("contract-tap-%d", nextMessageID()), raw)
}

func (h *contractHarness) answerTogether(label string) {
	h.t.Helper()
	var sources []string
	for _, chat := range h.chats() {
		messageID, text, buttons := h.card(chat)
		for _, button := range buttons {
			if strings.HasPrefix(button, label+"|") {
				sources = append(sources, h.seedTap(chat, messageID, text, strings.TrimPrefix(button, label+"|")))
			}
		}
	}
	if len(sources) != 2 {
		h.t.Fatalf("two copies of the card should offer %q, got %d", label, len(sources))
	}
	errs := make(chan error, len(sources))
	for _, source := range sources {
		go func() { errs <- h.p.Process(h.ctx, source) }()
	}
	for range sources {
		mustAgentTest(h.t, <-errs)
	}
	h.absorb()
	h.deliver()
}

const (
	longAccountName = "Rekening Tabungan Bersama Keluarga Besar"
	longWealthName  = "Emas Digital Simpanan Jangka Panjang Keluarga"
	dupKind         = "POSSIBLE_DUPLICATE"
	qualityKind     = "RECEIPT_MISMATCH"
	fixKind         = "MANUAL_CORRECTION"
	emailKind       = "FINANCIAL_EMAIL_RESOLUTION"
	factsEmail      = "FINANCIAL_EMAIL_FACTS"
	conflict        = "CONFLICTING_EVIDENCE"
	wealthKind      = "WEALTH_OBSERVATION_CONFIRMATION"
	readKind        = "DOCUMENT_EXTRACTION_LOW_CONFIDENCE"
	classKind       = "DOCUMENT_CLASSIFICATION"
)

func TestInteractionEngineDuplicateContract(t *testing.T) {
	one := withCandidates(1)
	runContractRows(t, []contractRow{
		{"the card shows the new record beside the one that already exists, then a tap merges", dupKind, []contractOption{one}, func(h *contractHarness) {
			h.wantCard("Ini sama dengan catatan yang sudah ada?", "Baru: Warung Contoh · Rp25.000", "Sudah ada: Warung Contoh · Rp25.000", "Alasan: ")
			h.wantButtons("Sama, gabungkan", "Beda, catat baru", "Abaikan")
			h.tapLabel("Sama, gabungkan")
			h.wantCard("✅ Digabung dengan catatan yang sudah ada: Warung Contoh · Rp25.000")
			h.wantNoButtons()
			h.wantType("EXPENSE", "VOIDED")
			h.wantItem("RESOLVED")
			h.wantModelCalls(0)
			h.wantAnswered("TAP")
		}},
		{"the merge button carries a stamped token the number prefix could not hold", dupKind, []contractOption{one}, func(h *contractHarness) {
			if data := h.buttonData("Sama, gabungkan"); !strings.HasPrefix(data, "review:dupm:") || data != "review:dupm:1000" {
				t.Fatalf("merge callback = %q", data)
			}
		}},
		{"Beda, catat baru asks for the category when the expense has none, then records it as new", dupKind, []contractOption{one}, func(h *contractHarness) {
			h.tapLabel("Beda, catat baru")
			h.wantCard("✅ Dicatat sebagai transaksi baru.", "Masuk kategori apa?")
			h.wantButtons("Makan", "Abaikan")
			h.tapLabel("Makan")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantItem("RESOLVED")
		}},
		{"Beda, catat baru finishes the card when the category is already known", dupKind, []contractOption{one, withCategory()}, func(h *contractHarness) {
			h.tapLabel("Beda, catat baru")
			h.wantCard("✅ Tersimpan: Warung Contoh · Makan · Rp25.000", "Dicatat sebagai transaksi baru.")
			h.wantNoButtons()
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantModelCalls(0)
		}},
		{"Abaikan voids the new record and keeps the old one", dupKind, []contractOption{one}, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: Warung Contoh · Rp25.000")
			h.wantType("EXPENSE", "VOIDED")
		}},
		{"typing the button label as a reply merges", dupKind, []contractOption{one}, func(h *contractHarness) {
			h.reply("sama, gabungkan")
			h.wantCard("✅ Digabung dengan catatan yang sudah ada")
			h.wantType("EXPENSE", "VOIDED")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"typing the button label as a thread reply records as new", dupKind, []contractOption{one, withCategory()}, func(h *contractHarness) {
			h.threadReply("Beda, catat baru")
			h.wantCard("✅ Tersimpan: Warung Contoh · Makan · Rp25.000")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"typing the label as a plain message to the single waiting card merges", dupKind, []contractOption{one}, func(h *contractHarness) {
			h.plain("Sama, gabungkan")
			h.wantCard("✅ Digabung dengan catatan yang sudah ada")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"with two cards waiting a label only the duplicate card has still reaches it", dupKind, []contractOption{one}, func(h *contractHarness) {
			h.openOtherCard()
			h.plain("Sama, gabungkan")
			h.wantCard("✅ Digabung dengan catatan yang sudah ada")
			h.wantAssistantCalls(0)
		}},
		{"with two cards waiting an unrelated plain message goes to the assistant", dupKind, []contractOption{one}, func(h *contractHarness) {
			h.openOtherCard()
			messageID, text, _ := h.current()
			h.chat("berapa pengeluaranku minggu ini?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantType("EXPENSE", "NEEDS_REVIEW")
		}},
		{"a plain yes shows the question again and records nothing", dupKind, []contractOption{one}, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Maaf, aku belum menangkap itu. Ini sama dengan catatan yang sudah ada?")
			h.wantButtons("Sama, gabungkan")
			h.wantType("EXPENSE", "NEEDS_REVIEW")
			h.wantAssistantCalls(0)
		}},
		{"a question thread-replied to the card reaches the assistant", dupKind, []contractOption{one}, func(h *contractHarness) {
			messageID, text, _ := h.current()
			followUp := nextMessageID()
			h.absorb()
			seedBotMessage(h.t, h.ctx, h.f, "Catatan tambahan.", messageID, followUp)
			h.absorb()
			_, err := sendText(h.t, h.ctx, h.f, h.p, "ini yang mana maksudnya?", followUp)
			mustAgentTest(h.t, err)
			h.absorb()
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantType("EXPENSE", "NEEDS_REVIEW")
		}},
		{"a button from an older candidate list is refused and the card is shown again", dupKind, []contractOption{one}, func(h *contractHarness) {
			stale := h.buttonData("Sama, gabungkan")
			h.exec(`INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',25000,'IDR',now()-interval '6 hours',now())`, h.f.householdID)
			h.tap("review:duppage:0")
			h.tap(stale)
			h.wantCard("Catatan itu sudah berubah, jadi belum ada yang digabung.", "Ini sama dengan catatan yang sudah ada?")
			h.wantType("EXPENSE", "NEEDS_REVIEW")
			if fresh := h.buttonData("Sama, gabungkan 1"); fresh == stale {
				t.Fatalf("the refreshed card reuses the stale stamp %q", fresh)
			}
		}},
		{"several candidates are numbered and each button merges its own record", dupKind, []contractOption{withCandidates(2)}, func(h *contractHarness) {
			h.wantCard("Sudah ada (1): ", "Sudah ada (2): ")
			h.wantButtons("Sama, gabungkan 1", "Sama, gabungkan 2", "Beda, catat baru")
			h.tapLabel("Sama, gabungkan 2")
			h.wantCard("✅ Digabung dengan catatan yang sudah ada")
			h.wantJobs(`SELECT count(*) FROM reconciliation_merge m JOIN transaction t ON t.id=m.target_transaction_id WHERE m.household_id=$1 AND m.source_transaction_id=$2 AND t.transaction_at<now()-interval '4 hours'`, 1, h.f.householdID, h.txID)
		}},
		{"more candidates than a page fit are paged with their global numbers", dupKind, []contractOption{withCandidates(10)}, func(h *contractHarness) {
			h.wantButtons("Sama, gabungkan 1", "Sama, gabungkan 8", "Berikutnya")
			h.wantNoButton("Sama, gabungkan 9")
			h.tapLabel("Berikutnya")
			h.wantCard("(halaman 2)", "Sudah ada (9): ")
			h.wantButtons("Sama, gabungkan 9", "Sama, gabungkan 10", "Sebelumnya")
			h.tapLabel("Sama, gabungkan 10")
			h.wantCard("✅ Digabung dengan catatan yang sudah ada")
			h.wantType("EXPENSE", "VOIDED")
		}},
		{"when no similar record is left the card offers only a new record or ignoring", dupKind, nil, func(h *contractHarness) {
			h.wantCard("Aku tidak menemukan catatan yang mirip lagi.")
			h.wantNoButton("Sama, gabungkan")
			h.wantButtons("Beda, catat baru", "Abaikan")
		}},
		{"the other member's copy follows a merge", dupKind, []contractOption{one, withSecondMember()}, func(h *contractHarness) {
			messageB, _, _ := h.card(h.chatB)
			h.tapLabel("Sama, gabungkan")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2`, 1, h.chatB, messageB)
		}},
		{"a tap from the other member's older copy after a merge records nothing twice", dupKind, []contractOption{one, withSecondMember()}, func(h *contractHarness) {
			messageB, textB, _ := h.card(h.chatB)
			data := h.buttonData("Sama, gabungkan")
			h.tapLabel("Sama, gabungkan")
			h.staleTapOn(h.chatB, messageB, textB, data)
			h.wantJobs(`SELECT count(*) FROM reconciliation_merge WHERE household_id=$1 AND source_transaction_id=$2`, 1, h.f.householdID, h.txID)
			h.wantJobs(`SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='STALE_REVIEW_ACTION'`, 1, h.f.householdID)
		}},
		{"two members answering at once merge once", dupKind, []contractOption{one, withSecondMember()}, func(h *contractHarness) {
			h.answerTogether("Sama, gabungkan")
			h.wantJobs(`SELECT count(*) FROM reconciliation_merge WHERE household_id=$1 AND source_transaction_id=$2`, 1, h.f.householdID, h.txID)
			h.wantType("EXPENSE", "VOIDED")
			h.wantItem("RESOLVED")
		}},
	})
}

func (h *contractHarness) wantObservation(status, account, wealth string) {
	h.t.Helper()
	var gotStatus, gotAccount, gotWealth string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT fo.status,COALESCE(a.name,''),COALESCE(w.name,'') FROM financial_email_observation fo
		LEFT JOIN account a ON a.id=fo.resolved_account_id LEFT JOIN wealth_account w ON w.id=fo.resolved_wealth_account_id WHERE fo.id=$1`, h.caseID).Scan(&gotStatus, &gotAccount, &gotWealth))
	if gotStatus != status || gotAccount != account || gotWealth != wealth {
		h.t.Fatalf("observation = %s / %q / %q, want %s / %q / %q", gotStatus, gotAccount, gotWealth, status, account, wealth)
	}
}

func (h *contractHarness) wantProcessEmailJobs(want int) {
	h.t.Helper()
	h.wantJobs(`SELECT count(*) FROM job WHERE type='PROCESS_FINANCIAL_EMAIL' AND payload_json->>'source_event_id'=(SELECT source_event_id::text FROM financial_email_observation WHERE id=$1)`, want, h.caseID)
}

func TestInteractionEngineFinancialEmailContract(t *testing.T) {
	wealthOnly := withEmailMissing("wealth_account")
	runContractRows(t, []contractRow{
		{"the funding account is asked first with stamped buttons, then the wealth account, then one done line", emailKind, nil, func(h *contractHarness) {
			h.wantCard("Rekening mana yang dipakai di email ini?", "Nominal: Rp150.000", "Rekening di email: Rekening Utama", "Alasan: ")
			h.wantButtons("Rekening A", "Rekening B", "Abaikan")
			if data := h.buttonData("Rekening B"); data != "review:fea:1001" {
				t.Fatalf("funding account callback = %q", data)
			}
			h.tapLabel("Rekening B")
			h.wantCard("✅ Rekening dipilih.", "Email ini untuk akun kekayaan yang mana?")
			h.wantButtons("Emas Contoh", "Reksa Contoh", "Abaikan")
			if data := h.buttonData("Reksa Contoh"); data != "review:few:1001" {
				t.Fatalf("wealth account callback = %q", data)
			}
			h.wantObservation("REVIEW", "Rekening B", "")
			h.tapLabel("Reksa Contoh")
			h.wantCard("✅ Tersimpan: Rekening B · Reksa Contoh", "Email ini sedang diproses lagi")
			h.wantNoButtons()
			h.wantObservation("PENDING", "Rekening B", "Reksa Contoh")
			h.wantProcessEmailJobs(1)
			h.wantItem("RESOLVED")
			h.wantModelCalls(0)
			h.wantAnswered("TAP", "TAP")
		}},
		{"a card that only lacks the wealth account asks only that", emailKind, []contractOption{wealthOnly}, func(h *contractHarness) {
			h.wantCard("Email ini untuk akun kekayaan yang mana?")
			h.tapLabel("Emas Contoh")
			h.wantCard("✅ Tersimpan: Rekening A · Emas Contoh")
			h.wantObservation("PENDING", "Rekening A", "Emas Contoh")
			h.wantProcessEmailJobs(1)
		}},
		{"a button label typed as a reply answers like the tap", emailKind, nil, func(h *contractHarness) {
			h.reply("  REKENING b ")
			h.wantCard("✅ Rekening dipilih.", "Email ini untuk akun kekayaan yang mana?")
			h.wantObservation("REVIEW", "Rekening B", "")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"a full account name too long for its button, typed as a reply, answers the step", emailKind, []contractOption{withNamedAccounts(longAccountName)}, func(h *contractHarness) {
			h.reply(strings.ToUpper(longAccountName))
			h.wantCard("✅ Rekening dipilih.", "Email ini untuk akun kekayaan yang mana?")
			h.wantObservation("REVIEW", longAccountName, "")
			h.wantAnswered("REPLY")
		}},
		{"a full name typed as a thread reply answers the step", emailKind, []contractOption{withNamedWealth(longWealthName), wealthOnly}, func(h *contractHarness) {
			h.threadReply(longWealthName)
			h.wantCard("✅ Tersimpan: Rekening A · " + longWealthName)
			h.wantAnswered("THREAD_REPLY")
		}},
		{"a full name typed as a plain message answers the single waiting card", emailKind, []contractOption{withNamedAccounts(longAccountName)}, func(h *contractHarness) {
			h.plain(longAccountName)
			h.wantCard("✅ Rekening dipilih.")
			h.wantObservation("REVIEW", longAccountName, "")
			h.wantAnswered("PLAIN")
		}},
		{"with two cards waiting a full account name still reaches the one that asks for accounts", emailKind, []contractOption{withNamedAccounts(longAccountName)}, func(h *contractHarness) {
			h.openOtherCard()
			h.plain(longAccountName)
			h.wantCard("✅ Rekening dipilih.")
			h.wantAssistantCalls(0)
		}},
		{"a name that matches no account asks again with the same buttons", emailKind, nil, func(h *contractHarness) {
			h.reply("bank yang lain")
			h.wantCard("Aku belum menemukan satu akun yang cocok dengan “bank yang lain”.", "Rekening mana yang dipakai di email ini?")
			h.wantButtons("Rekening A", "Rekening B")
			h.wantObservation("REVIEW", "", "")
		}},
		{"a button from an older account list is refused and the list is shown again", emailKind, nil, func(h *contractHarness) {
			stale := h.buttonData("Rekening B")
			h.exec(`UPDATE account SET active=false WHERE household_id=$1 AND name='Rekening A'`, h.f.householdID)
			h.tap("review:fepage:0")
			h.tap(stale)
			h.wantCard("Pilihan itu sudah tidak berlaku.", "Rekening mana yang dipakai di email ini?")
			h.wantNoButton("Rekening A")
			h.wantObservation("REVIEW", "", "")
		}},
		{"an account deactivated after the card was drawn never gets recorded", emailKind, nil, func(h *contractHarness) {
			data := h.buttonData("Rekening A")
			h.exec(`UPDATE account SET active=false WHERE household_id=$1 AND name='Rekening A'`, h.f.householdID)
			h.tap(data)
			h.wantCard("Pilihan itu sudah tidak berlaku.")
			h.wantObservation("REVIEW", "", "")
		}},
		{"a plain yes shows the question again and records nothing", emailKind, nil, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Maaf, aku belum menangkap itu. Rekening mana yang dipakai di email ini?")
			h.wantObservation("REVIEW", "", "")
			h.wantAssistantCalls(0)
		}},
		{"a question goes to the assistant and leaves the card alone", emailKind, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.chat("rekening apa saja yang aku punya?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantObservation("REVIEW", "", "")
		}},
		{"Abaikan ignores the email and records nothing", emailKind, nil, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: bukti email. Tidak ada transaksi yang dicatat.")
			h.wantNoButtons()
			h.wantObservation("IGNORED", "", "")
			h.wantProcessEmailJobs(0)
			h.wantItem("RESOLVED")
		}},
		{"the other member's copy moves to the wealth step with the first answer", emailKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, _, _ := h.card(h.chatB)
			h.tapLabel("Rekening A")
			if after, text, _ := h.card(h.chatB); after != messageB || !strings.Contains(text, "Email ini untuk akun kekayaan yang mana?") {
				t.Fatalf("the other copy did not follow: %d %q", after, text)
			}
		}},
		{"the other member's finished copy refuses a late tap", emailKind, []contractOption{withSecondMember(), wealthOnly}, func(h *contractHarness) {
			messageB, textB, _ := h.card(h.chatB)
			data := h.buttonData("Emas Contoh")
			h.tapLabel("Emas Contoh")
			h.staleTapOn(h.chatB, messageB, textB, data)
			h.wantProcessEmailJobs(1)
		}},
		{"two members answering at once record one choice and queue one reprocess", emailKind, []contractOption{withSecondMember(), wealthOnly}, func(h *contractHarness) {
			h.answerTogether("Emas Contoh")
			h.wantObservation("PENDING", "Rekening A", "Emas Contoh")
			h.wantProcessEmailJobs(1)
			h.wantJobs(`SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='RESOLVE_FINANCIAL_EMAIL_ENTITIES'`, 1, h.caseID)
		}},
		{"an observation that left review is not changed by a late tap", emailKind, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			data := h.buttonData("Rekening A")
			h.exec(`UPDATE financial_email_observation SET status='IGNORED' WHERE id=$1`, h.caseID)
			h.staleTapOn(h.f.chatID, messageID, text, data)
			h.wantObservation("IGNORED", "", "")
			h.wantProcessEmailJobs(0)
		}},
	})
}

func TestInteractionEngineUnsupportedEmailContract(t *testing.T) {
	runContractRows(t, []contractRow{
		{"unsupported email evidence is one plain line with Abaikan", factsEmail, nil, func(h *contractHarness) {
			h.wantCard("Bukti email ini belum bisa dicatat otomatis.", "Nominal: Rp150.000", "Belum ada transaksi yang dicatat.")
			h.wantButtons("Abaikan")
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: bukti email. Tidak ada transaksi yang dicatat.")
			h.wantNoButtons()
			h.wantObservation("IGNORED", "", "")
			h.wantJobs(`SELECT count(*) FROM transaction WHERE household_id=$1`, 0, h.f.householdID)
			h.wantAnswered("TAP")
		}},
		{"the Abaikan label typed as a reply ignores it", factsEmail, nil, func(h *contractHarness) {
			h.reply("abaikan")
			h.wantCard("🗑️ Diabaikan: bukti email.")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"typed text on an unsupported card shows the line again", factsEmail, nil, func(h *contractHarness) {
			h.reply("tolong dicatat saja")
			h.wantCard("Maaf, aku belum menangkap itu. Bukti email ini belum bisa dicatat otomatis.")
			h.wantObservation("REVIEW", "", "")
		}},
		{"a plain yes shows the line again", factsEmail, nil, func(h *contractHarness) {
			h.plain("oke")
			h.wantCard("Maaf, aku belum menangkap itu.")
			h.wantObservation("REVIEW", "", "")
			h.wantAssistantCalls(0)
		}},
		{"a question goes to the assistant", factsEmail, nil, func(h *contractHarness) {
			h.chat("kenapa ini tidak dicatat?")
			h.wantAssistantCalls(1)
			h.wantObservation("REVIEW", "", "")
		}},
		{"two cards waiting keep the plain Abaikan label ambiguous and hand it to the assistant", factsEmail, nil, func(h *contractHarness) {
			h.openOtherCard()
			h.chat("abaikan")
			h.wantObservation("REVIEW", "", "")
		}},
		{"two members answering at once ignore once and the other copy closes", factsEmail, []contractOption{withSecondMember()}, func(h *contractHarness) {
			h.answerTogether("Abaikan")
			h.wantObservation("IGNORED", "", "")
			h.wantItem("RESOLVED")
			h.wantJobs(`SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='RESOLVE_REVIEW'`, 1, h.itemID)
		}},
		{"evidence that two sources disagree on is explained without guessing", conflict, nil, func(h *contractHarness) {
			h.wantCard("Dua bukti tidak sepakat.", "Aku tidak menebak mana yang benar", "Alasan: ")
			h.wantButtons("Abaikan")
			h.wantNoButton("Sama, gabungkan")
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: bukti email.")
			h.wantObservation("IGNORED", "", "")
		}},
		{"a disagreement that has a record to merge with offers the duplicate choices", conflict, []contractOption{onTransaction(), withCandidates(1)}, func(h *contractHarness) {
			h.wantCard("Ini sama dengan catatan yang sudah ada?", "Baru: Warung Contoh · Rp25.000", "Sudah ada: Warung Contoh · Rp25.000")
			h.wantButtons("Sama, gabungkan", "Beda, catat baru", "Abaikan")
			h.tapLabel("Sama, gabungkan")
			h.wantCard("✅ Digabung dengan catatan yang sudah ada")
			h.wantType("EXPENSE", "VOIDED")
		}},
	})
}

func (h *contractHarness) wantWealth(status, account string) {
	h.t.Helper()
	var gotStatus, gotAccount string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT wo.status,COALESCE(w.name,'') FROM wealth_observation wo LEFT JOIN wealth_account w ON w.id=wo.resolved_wealth_account_id WHERE wo.id=$1`, h.caseID).Scan(&gotStatus, &gotAccount))
	if gotStatus != status || gotAccount != account {
		h.t.Fatalf("wealth observation = %s / %q, want %s / %q", gotStatus, gotAccount, status, account)
	}
	h.wantJobs(`SELECT count(*) FROM transaction WHERE household_id=$1 AND amount=15000000`, 0, h.f.householdID)
}

func TestInteractionEngineWealthContract(t *testing.T) {
	known := withKnownWealthAccount()
	runContractRows(t, []contractRow{
		{"the card shows the balance, the institution and the date, then a tap records the snapshot", wealthKind, nil, func(h *contractHarness) {
			h.wantCard("Saldo ini milik akun yang mana?", "Akun: Bank Contoh Emas Digital", "Nilai: Rp15.000.000", "Tanggal: 20 Nov 2026", "Alasan: ")
			h.wantButtons("Emas Contoh", "Reksa Contoh", "Abaikan")
			if data := h.buttonData("Reksa Contoh"); data != "review:wo:1001" {
				t.Fatalf("wealth callback = %q", data)
			}
			h.tapLabel("Reksa Contoh")
			h.wantCard("✅ Saldo dicatat: Reksa Contoh · Rp15.000.000", "bukan transaksi")
			h.wantNoButtons()
			h.wantWealth("ACCEPTED", "Reksa Contoh")
			h.wantItem("RESOLVED")
			h.wantJobs(`SELECT count(*) FROM financial_entity_alias WHERE household_id=$1 AND entity_type='WEALTH_ACCOUNT' AND normalized_alias='emas digital'`, 1, h.f.householdID)
			h.wantModelCalls(0)
			h.wantAnswered("TAP")
		}},
		{"a recognised account is confirmed with one tap", wealthKind, []contractOption{known}, func(h *contractHarness) {
			h.wantCard("Catat saldo ini ke akun yang sudah dikenali?", "Akun yang dikenali: Emas Contoh", "Nilai: Rp15.000.000")
			h.wantButtons("Ya, catat", "Akun lain", "Abaikan")
			h.tapLabel("Ya, catat")
			h.wantCard("✅ Saldo dicatat: Emas Contoh · Rp15.000.000")
			h.wantWealth("ACCEPTED", "Emas Contoh")
		}},
		{"Akun lain moves a recognised account to the account list", wealthKind, []contractOption{known}, func(h *contractHarness) {
			h.tapLabel("Akun lain")
			h.wantCard("Saldo ini milik akun yang mana?")
			h.wantButtons("Emas Contoh", "Reksa Contoh")
			h.tapLabel("Reksa Contoh")
			h.wantWealth("ACCEPTED", "Reksa Contoh")
		}},
		{"a button label typed as a reply records like the tap", wealthKind, nil, func(h *contractHarness) {
			h.reply("reksa contoh")
			h.wantCard("✅ Saldo dicatat: Reksa Contoh")
			h.wantWealth("ACCEPTED", "Reksa Contoh")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"a full account name too long for its button, typed as a reply, records the snapshot", wealthKind, []contractOption{withNamedWealth(longWealthName)}, func(h *contractHarness) {
			h.reply(longWealthName)
			h.wantCard("✅ Saldo dicatat: " + longWealthName)
			h.wantWealth("ACCEPTED", longWealthName)
			h.wantAnswered("REPLY")
		}},
		{"a full name typed as a thread reply records the snapshot", wealthKind, []contractOption{withNamedWealth(longWealthName)}, func(h *contractHarness) {
			h.threadReply(longWealthName)
			h.wantWealth("ACCEPTED", longWealthName)
			h.wantAnswered("THREAD_REPLY")
		}},
		{"a full name typed as a plain message answers the single waiting card", wealthKind, []contractOption{withNamedWealth(longWealthName)}, func(h *contractHarness) {
			h.plain(longWealthName)
			h.wantWealth("ACCEPTED", longWealthName)
			h.wantAnswered("PLAIN")
		}},
		{"with two cards waiting a full account name still finds the wealth card", wealthKind, []contractOption{withNamedWealth(longWealthName)}, func(h *contractHarness) {
			h.openOtherCard()
			h.plain(longWealthName)
			h.wantWealth("ACCEPTED", longWealthName)
			h.wantAssistantCalls(0)
		}},
		{"a button from an older list is refused and nothing is recorded", wealthKind, nil, func(h *contractHarness) {
			stale := h.buttonData("Emas Contoh")
			h.exec(`UPDATE wealth_account SET active=false WHERE household_id=$1 AND name='Reksa Contoh'`, h.f.householdID)
			h.tap("review:wopage:0")
			h.tap(stale)
			h.wantCard("Pilihan itu sudah tidak berlaku.", "Saldo ini milik akun yang mana?")
			h.wantWealth("PENDING", "")
		}},
		{"a plain yes shows the question again and records nothing", wealthKind, nil, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Maaf, aku belum menangkap itu. Saldo ini milik akun yang mana?")
			h.wantWealth("PENDING", "")
			h.wantAssistantCalls(0)
		}},
		{"a plain yes on the confirmation shows it again and records nothing", wealthKind, []contractOption{known}, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Maaf, aku belum menangkap itu. Catat saldo ini ke akun yang sudah dikenali?")
			h.wantWealth("PENDING", "")
		}},
		{"a question goes to the assistant", wealthKind, nil, func(h *contractHarness) {
			h.chat("berapa total kekayaanku?")
			h.wantAssistantCalls(1)
			h.wantWealth("PENDING", "")
		}},
		{"Abaikan dismisses the observation and records nothing", wealthKind, nil, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: saldo.")
			h.wantNoButtons()
			h.wantWealth("DISMISSED", "")
			h.wantItem("RESOLVED")
		}},
		{"the other member's copy closes with the answer", wealthKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, _, _ := h.card(h.chatB)
			h.tapLabel("Emas Contoh")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2`, 1, h.chatB, messageB)
		}},
		{"a late tap on the other member's older copy records nothing twice", wealthKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, textB, _ := h.card(h.chatB)
			data := h.buttonData("Emas Contoh")
			h.tapLabel("Emas Contoh")
			h.staleTapOn(h.chatB, messageB, textB, data)
			h.wantWealth("ACCEPTED", "Emas Contoh")
		}},
		{"two members answering at once record one snapshot", wealthKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			h.answerTogether("Emas Contoh")
			h.wantWealth("ACCEPTED", "Emas Contoh")
			h.wantJobs(`SELECT count(*) FROM audit_log WHERE action='RESOLVE_REVIEW' AND after_json->>'wealth_observation_id'=$1`, 1, h.caseID)
		}},
	})
}

func (h *contractHarness) wantReprocessJobs(want int) {
	h.t.Helper()
	h.wantJobs(`SELECT count(*) FROM job WHERE type='PROCESS_DOCUMENT' AND payload_json->>'document_id'=$1`, want, h.caseID)
}

func (h *contractHarness) wantSource(status string) {
	h.t.Helper()
	var got string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT processing_status FROM source_event WHERE id=$1`, h.imageID).Scan(&got))
	if got != status {
		h.t.Fatalf("source event = %s, want %s", got, status)
	}
}

func TestInteractionEngineDocumentContract(t *testing.T) {
	runContractRows(t, []contractRow{
		{"an unreadable document asks to read it again and queues the reprocess job once", readKind, nil, func(h *contractHarness) {
			h.wantCard("Dokumen ini belum terbaca jelas. Proses ulang?", "Dokumen: Struk", "Alasan: ")
			h.wantButtons("Proses ulang", "Abaikan")
			h.tapLabel("Proses ulang")
			h.wantCard("⏳ Dokumen sedang dibaca ulang.", "Hasilnya muncul di kartu ini.")
			h.wantNoButtons()
			h.wantReprocessJobs(1)
			h.wantItem("RESOLVED")
			h.wantItemResolution("REPROCESS_DOCUMENT")
			h.wantModelCalls(0)
			h.wantAnswered("TAP")
		}},
		{"an unrecognised document type is asked in its own words", classKind, nil, func(h *contractHarness) {
			h.wantCard("Jenis dokumen ini belum dikenali. Proses ulang?")
			h.wantButtons("Proses ulang", "Abaikan")
		}},
		{"Abaikan ignores the document and keeps the photo as evidence", readKind, nil, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: dokumen. Tidak ada transaksi yang dicatat.")
			h.wantNoButtons()
			h.wantSource("IGNORED")
			h.wantReprocessJobs(0)
			h.wantItemResolution("IGNORE")
		}},
		{"the Proses ulang label typed as a reply reprocesses", readKind, nil, func(h *contractHarness) {
			h.reply("proses ulang")
			h.wantCard("⏳ Dokumen sedang dibaca ulang.")
			h.wantReprocessJobs(1)
			h.wantAnswered("LABEL_TEXT")
		}},
		{"the label typed as a thread reply reprocesses", readKind, nil, func(h *contractHarness) {
			h.threadReply("Proses ulang")
			h.wantReprocessJobs(1)
			h.wantAnswered("LABEL_TEXT")
		}},
		{"the label typed as a plain message answers the single waiting card", readKind, nil, func(h *contractHarness) {
			h.plain("proses ulang")
			h.wantReprocessJobs(1)
			h.wantAnswered("LABEL_TEXT")
		}},
		{"with two cards waiting the label still reaches the document card", readKind, nil, func(h *contractHarness) {
			h.openOtherCard()
			h.plain("proses ulang")
			h.wantReprocessJobs(1)
			h.wantAssistantCalls(0)
		}},
		{"a plain yes shows the question again and queues nothing", readKind, nil, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Maaf, aku belum menangkap itu. Dokumen ini belum terbaca jelas. Proses ulang?")
			h.wantReprocessJobs(0)
			h.wantAssistantCalls(0)
		}},
		{"a question goes to the assistant and leaves the card alone", readKind, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.chat("kenapa dokumennya tidak terbaca?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantReprocessJobs(0)
		}},
		{"an old tap on a card that already asked for a reread queues nothing more", readKind, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			data := h.buttonData("Proses ulang")
			h.tapLabel("Proses ulang")
			h.staleTapOn(h.f.chatID, messageID, text, data)
			h.wantReprocessJobs(1)
		}},
		{"the other member's copy closes and a late tap there queues nothing", readKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, textB, _ := h.card(h.chatB)
			data := h.buttonData("Proses ulang")
			h.tapLabel("Proses ulang")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2`, 1, h.chatB, messageB)
			h.staleTapOn(h.chatB, messageB, textB, data)
			h.wantReprocessJobs(1)
		}},
		{"two members answering at once queue one reprocess", readKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			h.answerTogether("Proses ulang")
			h.wantReprocessJobs(1)
			h.wantItemResolution("REPROCESS_DOCUMENT")
		}},
	})
}

func TestInteractionEngineReceiptMismatchContract(t *testing.T) {
	runContractRows(t, []contractRow{
		{"the printed total is accepted with one tap and the plain explanation", qualityKind, nil, func(h *contractHarness) {
			h.wantCard("Rincian struk tidak cocok dengan total yang tercetak.", "Nominal: Rp25.000", "Alasan: ", "Cek totalnya dulu.")
			h.wantButtons("Terima total", "Abaikan")
			h.tapLabel("Terima total")
			h.wantCard("✅ Tersimpan: Warung Contoh · Makan · Rp25.000", "Total yang tercetak dipakai.")
			h.wantNoButtons()
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantItem("RESOLVED")
			h.wantAlias(false)
			h.wantModelCalls(0)
			h.wantAnswered("TAP")
		}},
		{"a receipt without a category asks for one before it is recorded", qualityKind, []contractOption{withoutCategory()}, func(h *contractHarness) {
			h.tapLabel("Terima total")
			h.wantCard("Transaksi ini belum punya kategori.", "Masuk kategori apa?")
			h.wantButtons("Makan", "Kembali", "Abaikan")
			h.tapLabel("Makan")
			h.wantCard("✅ Tersimpan: Warung Contoh · Makan · Rp25.000")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
		}},
		{"Kembali from the category step returns to the receipt question", qualityKind, []contractOption{withoutCategory()}, func(h *contractHarness) {
			h.tapLabel("Terima total")
			h.tapLabel("Kembali")
			h.wantCard("Rincian struk tidak cocok dengan total yang tercetak.")
			h.wantButtons("Terima total")
		}},
		{"Abaikan voids the receipt record", qualityKind, nil, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: Warung Contoh · Rp25.000")
			h.wantType("EXPENSE", "VOIDED")
		}},
		{"the label typed as a reply accepts the total", qualityKind, nil, func(h *contractHarness) {
			h.reply("terima total")
			h.wantCard("✅ Tersimpan: Warung Contoh · Makan · Rp25.000")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"the label typed as a thread reply accepts the total", qualityKind, nil, func(h *contractHarness) {
			h.threadReply("Terima total")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"the label typed as a plain message accepts the total", qualityKind, nil, func(h *contractHarness) {
			h.plain("terima total")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"with two cards waiting the label still reaches the receipt card", qualityKind, nil, func(h *contractHarness) {
			h.openOtherCard()
			h.plain("terima total")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantAssistantCalls(0)
		}},
		{"a typed total goes to the assistant and leaves the card alone", qualityKind, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.question(messageID, "totalnya 27 ribu")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantTransaction("NEEDS_REVIEW", "Warung Contoh", "Makan")
		}},
		{"a plain yes shows the question again and records nothing", qualityKind, nil, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Maaf, aku belum menangkap itu. Rincian struk tidak cocok dengan total yang tercetak.")
			h.wantTransaction("NEEDS_REVIEW", "Warung Contoh", "Makan")
			h.wantAssistantCalls(0)
		}},
		{"a question goes to the assistant and leaves the card alone", qualityKind, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.chat("kenapa totalnya beda?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantTransaction("NEEDS_REVIEW", "Warung Contoh", "Makan")
		}},
		{"an old button for another card's action is refused and the question shown again", qualityKind, nil, func(h *contractHarness) {
			h.tap("review:dup:new")
			h.wantCard("Pilihan itu tidak berlaku untuk kartu ini.", "Rincian struk tidak cocok dengan total yang tercetak.")
			h.wantTransaction("NEEDS_REVIEW", "Warung Contoh", "Makan")
		}},
		{"the other member's copy closes and a late tap there records nothing twice", qualityKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, textB, _ := h.card(h.chatB)
			data := h.buttonData("Terima total")
			h.tapLabel("Terima total")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2`, 1, h.chatB, messageB)
			h.staleTapOn(h.chatB, messageB, textB, data)
			h.wantJobs(`SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND evidence_type='TELEGRAM_REVIEW_REPLY' AND metadata_json->>'classification'='CONFIRM'`, 1, h.txID)
		}},
		{"two members answering at once record the total once", qualityKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			h.answerTogether("Terima total")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantJobs(`SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND evidence_type='TELEGRAM_REVIEW_REPLY' AND metadata_json->>'classification'='CONFIRM'`, 1, h.txID)
		}},
	})
}

func TestInteractionEngineManualCorrectionContract(t *testing.T) {
	runContractRows(t, []contractRow{
		{"a correction card offers to confirm or undo with a plain explanation", fixKind, nil, func(h *contractHarness) {
			h.wantCard("Transaksi ini perlu dicek sebelum dicatat.", "Nominal: Rp25.000", "Alasan: ", "Pilih Benar")
			h.wantButtons("Benar", "Batalkan transaksi")
			h.tapLabel("Benar")
			h.wantCard("✅ Tersimpan: Warung Contoh · Makan · Rp25.000", "Transaksinya dikonfirmasi.")
			h.wantNoButtons()
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantItem("RESOLVED")
			h.wantModelCalls(0)
			h.wantAnswered("TAP")
		}},
		{"Batalkan transaksi voids the record", fixKind, nil, func(h *contractHarness) {
			h.tapLabel("Batalkan transaksi")
			h.wantCard("🗑️ Diabaikan: Warung Contoh · Rp25.000")
			h.wantType("EXPENSE", "VOIDED")
		}},
		{"a correction without a category asks for one", fixKind, []contractOption{withoutCategory()}, func(h *contractHarness) {
			h.tapLabel("Benar")
			h.wantCard("Transaksi ini belum punya kategori.", "Masuk kategori apa?")
			h.tapLabel("Makan")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
		}},
		{"the label typed as a reply confirms", fixKind, nil, func(h *contractHarness) {
			h.reply("benar")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"the label typed as a thread reply confirms", fixKind, nil, func(h *contractHarness) {
			h.threadReply("Benar")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"the label typed as a plain message confirms the single waiting card", fixKind, nil, func(h *contractHarness) {
			h.plain("batalkan transaksi")
			h.wantType("EXPENSE", "VOIDED")
			h.wantAnswered("LABEL_TEXT")
		}},
		{"with two cards waiting the label still reaches the correction card", fixKind, nil, func(h *contractHarness) {
			h.openOtherCard()
			h.plain("batalkan transaksi")
			h.wantType("EXPENSE", "VOIDED")
			h.wantAssistantCalls(0)
		}},
		{"a typed correction goes to the assistant and leaves the card alone", fixKind, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.question(messageID, "makan siang tim")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
			h.wantTransaction("NEEDS_REVIEW", "Warung Contoh", "Makan")
		}},
		{"a plain yes shows the question again and records nothing", fixKind, nil, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Maaf, aku belum menangkap itu. Transaksi ini perlu dicek sebelum dicatat.")
			h.wantTransaction("NEEDS_REVIEW", "Warung Contoh", "Makan")
			h.wantAssistantCalls(0)
		}},
		{"a question goes to the assistant", fixKind, nil, func(h *contractHarness) {
			h.chat("apa yang perlu dikoreksi?")
			h.wantAssistantCalls(1)
			h.wantTransaction("NEEDS_REVIEW", "Warung Contoh", "Makan")
		}},
		{"an old button for another card's action is refused and the question shown again", fixKind, nil, func(h *contractHarness) {
			h.tap("review:quality:confirm")
			h.wantCard("Pilihan itu tidak berlaku untuk kartu ini.", "Transaksi ini perlu dicek sebelum dicatat.")
			h.wantTransaction("NEEDS_REVIEW", "Warung Contoh", "Makan")
		}},
		{"the other member's copy closes and a late tap there records nothing twice", fixKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, textB, _ := h.card(h.chatB)
			data := h.buttonData("Benar")
			h.tapLabel("Benar")
			h.staleTapOn(h.chatB, messageB, textB, data)
			h.wantJobs(`SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND evidence_type='TELEGRAM_REVIEW_REPLY' AND metadata_json->>'classification'='CONFIRM'`, 1, h.txID)
		}},
		{"two members answering at once confirm once", fixKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			h.answerTogether("Benar")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
			h.wantJobs(`SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND evidence_type='TELEGRAM_REVIEW_REPLY' AND metadata_json->>'classification'='CONFIRM'`, 1, h.txID)
		}},
	})
}
