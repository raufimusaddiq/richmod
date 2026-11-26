package telegram

import (
	"fmt"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

const (
	bankKind     = "UNKNOWN_BANK_TEMPLATE"
	amountKind   = "MISSING_AMOUNT"
	dateKind     = "MISSING_TRANSACTION_DATE"
	factsKind    = "TRANSACTION_FACTS_MISSING"
	purposeKind  = "UNKNOWN_PURPOSE"
	bankQueued   = `SELECT count(*) FROM job WHERE type='COMPLETE_BANK_REVIEW' AND payload_json->>'review_id'=$1`
	bankAmountOK = "✅ Nominal Rp54.000 diterima."
)

func withUnlinkedBank() contractOption {
	return func(h *contractHarness) { h.unlinked = true }
}

func withKnownBankAmount() contractOption {
	return func(h *contractHarness) { h.knownAmount = true }
}

func withCategory() contractOption {
	return func(h *contractHarness) { h.categorized = true }
}

func withMissingFacts(facts ...string) contractOption {
	return func(h *contractHarness) { h.missingFacts = facts }
}

func withKnownBankFacts() contractOption {
	return func(h *contractHarness) { h.knownAmount, h.knownTime = true, true }
}

func (h *contractHarness) openBank() {
	h.t.Helper()
	stamp := testsupport.UniqueStamp()
	var listener, source string
	account := ""
	for _, name := range []string{"Rekening A", "Rekening B"} {
		var id string
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,$2,'BANK','FULL_LEDGER') ON CONFLICT(household_id,name) DO UPDATE SET name=excluded.name RETURNING id`, h.f.householdID, name).Scan(&id))
		if account == "" {
			account = id
		}
	}
	linked := account
	if h.unlinked {
		linked = ""
	}
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT COALESCE((SELECT id::text FROM bank_email_listener WHERE household_id=$1 AND sender_address='notifikasi@bank.example.test'),'')`, h.f.householdID).Scan(&listener))
	if listener == "" {
		mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,account_id,created_by_user_id) VALUES($1,'Bank Contoh','notifikasi@bank.example.test',NULLIF($2,'')::uuid,$3) RETURNING id`, h.f.householdID, linked, h.f.userID).Scan(&listener))
	}
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, h.f.householdID, fmt.Sprintf("contract-bank-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&source))
	h.exec(`INSERT INTO bank_email_event(source_event_id,listener_id,observed_sender,message_id,subject,email_date,body) VALUES($1,$2,'notifikasi@bank.example.test',$3,E'Info  rekening\nbaru','',  'isi email')`, source, listener, fmt.Sprintf("msg-%d", stamp))
	output := `{"kind":"OTHER"}`
	if h.knownAmount {
		output = `{"kind":"TRANSACTION","amount_idr":"54000"}`
	}
	if h.knownTime {
		output = `{"kind":"UNKNOWN","amount_idr":"54000","transaction_at":"2026-11-25T08:07:00Z"}`
	}
	h.exec(`INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','v1',$3::jsonb,'VALID')`, source, listener, output)
	decision, _ := reviewdec.Preset(bankKind, "source_event", source)
	encoded, err := decision.JSON()
	mustAgentTest(h.t, err)
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN',$3::jsonb) RETURNING id`, h.f.householdID, source, string(encoded)).Scan(&h.itemID))
	h.project(h.itemID)
}

func (h *contractHarness) openProposalAmount() {
	h.t.Helper()
	stamp := testsupport.UniqueStamp()
	var image, attachment, document, proposal, category string
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `SELECT id FROM category WHERE household_id=$1 AND slug='makan'`, h.f.householdID).Scan(&category))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, h.f.householdID, fmt.Sprintf("contract-amount-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&image))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, h.f.householdID, []byte(fmt.Sprint(stamp)), fmt.Sprintf("contract/%d.png", stamp)).Scan(&attachment))
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'BANK_TRANSACTION_SCREENSHOT','NEEDS_REVIEW') RETURNING id`, h.f.householdID, image, attachment).Scan(&document))
	candidate := ""
	if h.txType != "INCOME" {
		candidate = category
	}
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,confidence,proposal_status,metadata_json) VALUES($1,$2,'row-001',$3,NULL,'IDR',now()-interval '2 hours','Toko Contoh',NULLIF($4,'')::uuid,.9,'NEEDS_REVIEW',jsonb_build_object('document_id',$5::uuid,'row_index',0,'date_known',true,'direction','OUT')) RETURNING id`, h.f.householdID, image, h.txType, candidate, document).Scan(&proposal))
	decision := `{"version":1,"reasonCode":"MISSING_AMOUNT","decisionClass":"EVIDENCE_GAP","interactionMode":"SINGLE_FIELD","knownFacts":{},"missingFacts":["amount"],"allowedActions":["SET_AMOUNT","IGNORE"]}`
	mustAgentTest(h.t, h.f.pool.QueryRow(h.ctx, `INSERT INTO review_item(household_id,proposal_id,source_event_id,document_id,review_type,status,decision) VALUES($1,$2,$3,$4,'MISSING_AMOUNT','OPEN',$5::jsonb) RETURNING id`, h.f.householdID, proposal, image, document, decision).Scan(&h.itemID))
	h.project(h.itemID)
}

func (h *contractHarness) wantBankQueued(want int, amount string) {
	h.t.Helper()
	h.wantJobs(bankQueued, want, h.itemID)
	if want > 0 && amount != "" {
		h.wantJobs(`SELECT count(*) FROM job WHERE type='COMPLETE_BANK_REVIEW' AND payload_json->>'review_id'=$1 AND payload_json->>'amount_idr'=$2 AND payload_json->>'transaction_at' IS NOT NULL`, 1, h.itemID, amount)
	}
}

func (h *contractHarness) wantProposalTransactions(want int) {
	h.t.Helper()
	h.wantJobs(`SELECT count(*) FROM transaction WHERE household_id=$1`, want, h.f.householdID)
}

func bankAmountRow(label, answer string) contractRow {
	return contractRow{"the amount " + label + " is read", bankKind, nil, func(h *contractHarness) {
		h.reply(answer)
		h.wantCard(bankAmountOK, "Transaksinya terjadi kapan?", "Nominal: Rp54.000")
		h.wantButtons("Waktu email", "Hari ini", "Kemarin", "Tanggal lain", "Abaikan")
		h.wantBankQueued(0, "")
	}}
}

func TestInteractionEngineBankContract(t *testing.T) {
	unlinked := withUnlinkedBank()
	rows := []contractRow{
		{"an unreadable email is asked for its amount, then its time, and queued once", bankKind, nil, func(h *contractHarness) {
			h.wantCard("Info rekening baru", "notifikasi@bank.example.test", "Berapa nominalnya?")
			h.wantButtons("Abaikan")
			h.reply("54rb")
			h.wantCard(bankAmountOK, "Transaksinya terjadi kapan?")
			h.tapLabel("Waktu email")
			h.wantCard("✅ Diterima: Rp54.000 ·", "Transaksinya sedang dicatat.")
			h.wantNoButtons()
			h.wantBankQueued(1, "54000")
			h.wantItemActive()
			h.wantModelCalls(0)
			h.wantAnswered("REPLY", "TAP")
		}},
		bankAmountRow("54rb", "54rb"),
		bankAmountRow("54.000", "54.000"),
		bankAmountRow("Rp54.000", "Rp54.000"),
		bankAmountRow("0,054jt", "0,054jt"),
		{"a decimal million amount is exact", bankKind, nil, func(h *contractHarness) {
			h.reply("1,5jt")
			h.wantCard("✅ Nominal Rp1.500.000 diterima.")
		}},
		{"the amount typed as a plain message to the single waiting card", bankKind, nil, func(h *contractHarness) {
			h.plain("54rb")
			h.wantCard(bankAmountOK, "Transaksinya terjadi kapan?")
			h.wantAnswered("PLAIN")
			h.wantModelCalls(0)
		}},
		{"the amount typed as a thread reply", bankKind, nil, func(h *contractHarness) {
			h.threadReply("54rb")
			h.wantCard(bankAmountOK)
			h.wantAnswered("THREAD_REPLY")
		}},
		{"an amount that cannot be read is asked again on the same step", bankKind, nil, func(h *contractHarness) {
			h.reply("lumayan banyak")
			h.wantCard("Nominalnya belum terbaca. Berapa nominalnya?")
			h.reply("54")
			h.wantCard("Nominalnya belum terbaca. Berapa nominalnya?")
			h.reply("99999999999999")
			h.wantCard("Nominalnya belum terbaca. Berapa nominalnya?")
			h.reply("1,234,5rb")
			h.wantCard("Nominalnya belum terbaca. Berapa nominalnya?")
			h.wantButtons("Abaikan")
			h.wantBankQueued(0, "")
		}},
		{"a plain yes re-shows the amount step and records nothing", bankKind, nil, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Berapa nominalnya?")
			h.wantButtons("Abaikan")
			h.wantBankQueued(0, "")
		}},
		{"a question while the amount card waits goes to the assistant", bankKind, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.chat("berapa pengeluaranku minggu ini?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
		}},
		{"a stale tap from another step is refused with the current step shown", bankKind, nil, func(h *contractHarness) {
			h.tap("review:date:today")
			h.wantCard("Pilihan itu tidak berlaku untuk kartu ini. Berapa nominalnya?")
			h.wantBankQueued(0, "")
		}},
		{"the time can be a button: today", bankKind, nil, func(h *contractHarness) {
			h.reply("54rb")
			h.tapLabel("Hari ini")
			h.wantCard("✅ Diterima: Rp54.000 ·")
			h.wantBankQueued(1, "54000")
		}},
		{"the time can be a button: yesterday", bankKind, nil, func(h *contractHarness) {
			h.reply("54rb")
			h.tapLabel("Kemarin")
			h.wantCard("✅ Diterima: Rp54.000 ·")
			h.wantBankQueued(1, "54000")
		}},
		{"the email time can be typed as its label", bankKind, nil, func(h *contractHarness) {
			h.reply("54rb")
			h.reply("Waktu email")
			h.wantCard("✅ Diterima: Rp54.000 ·")
			h.wantAnswered("REPLY", "LABEL_TEXT")
			h.wantBankQueued(1, "54000")
		}},
		{"a typed date with a clock time is queued with that time", bankKind, nil, func(h *contractHarness) {
			h.reply("54rb")
			h.tapLabel("Tanggal lain")
			h.wantCard("Ketik tanggalnya, misalnya 3 Okt atau 25/9.")
			h.reply("kemarin 15.07")
			h.wantCard("✅ Diterima: Rp54.000 ·", "15:07")
		}},
		{"a future time is refused and the step is shown again", bankKind, nil, func(h *contractHarness) {
			h.reply("54rb")
			h.reply("25/12/2099")
			h.wantCard("Tanggal itu belum terjadi.", "Transaksinya terjadi kapan?")
			h.wantButtons("Hari ini")
			h.wantBankQueued(0, "")
		}},
		{"a date that is too old is refused", bankKind, nil, func(h *contractHarness) {
			h.reply("54rb")
			h.reply("1/1/2020")
			h.wantCard("Tanggal itu terlalu lama.", "Transaksinya terjadi kapan?")
			h.wantBankQueued(0, "")
		}},
		{"a plain yes on the time step re-shows it", bankKind, nil, func(h *contractHarness) {
			h.reply("54rb")
			h.plain("oke")
			h.wantCard("Transaksinya terjadi kapan?")
			h.wantBankQueued(0, "")
		}},
		{"an email whose amount is already known starts at the time", bankKind, []contractOption{withKnownBankAmount()}, func(h *contractHarness) {
			h.wantCard("Nominal: Rp54.000", "Transaksinya terjadi kapan?")
			h.tapLabel("Hari ini")
			h.wantBankQueued(1, "54000")
		}},
		{"an email whose amount and time are both read only asks to record it", bankKind, []contractOption{withKnownBankFacts()}, func(h *contractHarness) {
			h.wantCard("Nominal: Rp54.000", "Waktu: 25 Nov 15:07", "Catat transaksi ini?")
			h.wantButtons("Catat", "Abaikan")
			h.plain("ya")
			h.wantBankQueued(1, "54000")
			h.wantCard("✅ Diterima: Rp54.000 · 25 Nov 15:07.")
		}},
		{"an email not linked to an account asks for the account before it queues", bankKind, []contractOption{unlinked}, func(h *contractHarness) {
			h.reply("54rb")
			h.tapLabel("Waktu email")
			h.wantCard("Email ini untuk rekening yang mana?")
			h.wantButtons("Rekening A", "Rekening B", "Abaikan")
			h.wantBankQueued(0, "")
			h.tapLabel("Rekening B")
			h.wantCard("✅ Diterima: Rp54.000 ·")
			h.wantBankQueued(1, "54000")
			h.wantJobs(`SELECT count(*) FROM bank_email_listener WHERE household_id=$1 AND account_id IS NOT NULL`, 1, h.f.householdID)
		}},
		{"the account can be typed", bankKind, []contractOption{unlinked}, func(h *contractHarness) {
			h.reply("54rb")
			h.tapLabel("Hari ini")
			h.reply("rekening a")
			h.wantCard("✅ Diterima: Rp54.000 ·")
			h.wantBankQueued(1, "54000")
		}},
		{"a reply may name the account by a unique part of its name", bankKind, []contractOption{unlinked}, func(h *contractHarness) {
			h.reply("54rb")
			h.tapLabel("Hari ini")
			h.reply("b")
			h.wantCard("✅ Diterima: Rp54.000 ·")
			h.wantBankQueued(1, "54000")
		}},
		{"an untargeted plain part of an account name binds nothing, an exact name does", bankKind, []contractOption{unlinked}, func(h *contractHarness) {
			h.reply("54rb")
			h.tapLabel("Hari ini")
			h.chat("b")
			h.wantAssistantCalls(1)
			h.wantCard("Email ini untuk rekening yang mana?")
			h.wantBankQueued(0, "")
			h.plain("rekening b")
			h.wantCard("✅ Diterima: Rp54.000 ·")
			h.wantBankQueued(1, "54000")
		}},
		{"an account that is not offered cannot be chosen", bankKind, []contractOption{unlinked}, func(h *contractHarness) {
			h.reply("54rb")
			h.tapLabel("Hari ini")
			h.reply("rekening c")
			h.wantCard("Aku belum menemukan satu rekening yang cocok.", "Email ini untuk rekening yang mana?")
			h.wantBankQueued(0, "")
		}},
		{"ignoring the email closes it and queues nothing", bankKind, nil, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan: email bank. Tidak dicatat.")
			h.wantNoButtons()
			h.wantItem("RESOLVED")
			h.wantBankQueued(0, "")
		}},
		{"answering on one phone closes the other member's copy and a later tap on it queues nothing twice", bankKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, textB, _ := h.card(h.chatB)
			if messageB == 0 {
				t.Fatal("the second member never received the card")
			}
			h.reply("54rb")
			if text := h.texts[messageB]; !strings.Contains(text, "Transaksinya terjadi kapan?") {
				t.Fatalf("the other member's copy did not follow the step: %q", text)
			}
			h.tapLabel("Waktu email")
			if text := h.texts[messageB]; !strings.Contains(text, "✅ Diterima: Rp54.000 ·") || len(h.markups[messageB]) != 0 {
				t.Fatalf("the other member's copy was not closed: %q %v", text, h.markups[messageB])
			}
			h.staleTapOn(h.chatB, messageB, textB, "review:date:today")
			h.wantBankQueued(1, "54000")
		}},
		{"a plain amount while two bank cards wait asks which card and saves nothing", bankKind, nil, func(h *contractHarness) {
			first := h.itemID
			h.openBank()
			h.itemID = first
			h.chat("54rb")
			h.wantAssistantCalls(0)
			h.wantJobs(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text' LIKE 'Ada beberapa kartu%'`, 1, h.f.chatID)
			h.wantBankQueued(0, "")
		}},
	}
	runContractRows(t, rows)
}

func TestInteractionEngineAmountContract(t *testing.T) {
	income := withTransactionType("INCOME")
	rows := []contractRow{
		{"an expense screenshot row without an amount is completed by typing it", amountKind, nil, func(h *contractHarness) {
			h.wantCard("Berapa nominalnya?", "Merchant: Toko Contoh")
			h.wantButtons("Abaikan")
			h.reply("75rb")
			h.wantCard("✅ Tersimpan: Toko Contoh · Makan · Rp75.000")
			h.wantNoButtons()
			h.wantItem("RESOLVED")
			h.wantProposalTransactions(1)
			h.wantModelCalls(0)
		}},
		{"the amount typed as a plain message to the single waiting card", amountKind, nil, func(h *contractHarness) {
			h.plain("75.000")
			h.wantCard("✅ Tersimpan: Toko Contoh · Makan · Rp75.000")
			h.wantAnswered("PLAIN")
		}},
		{"the amount typed as a thread reply", amountKind, nil, func(h *contractHarness) {
			h.threadReply("Rp75.000")
			h.wantCard("✅ Tersimpan: Toko Contoh · Makan · Rp75.000")
			h.wantAnswered("THREAD_REPLY")
		}},
		{"an unreadable amount is asked again", amountKind, nil, func(h *contractHarness) {
			h.reply("sekitar segitu")
			h.wantCard("Nominalnya belum terbaca. Berapa nominalnya?")
			h.wantProposalTransactions(0)
		}},
		{"a plain yes re-shows the amount step", amountKind, nil, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Berapa nominalnya?")
			h.wantProposalTransactions(0)
		}},
		{"an income row asks penghasilan or transfer sendiri after the amount", amountKind, []contractOption{income}, func(h *contractHarness) {
			h.reply("1,5jt")
			h.wantCard("✅ Nominal Rp1.500.000 diterima.", "Rp1.500.000 ini dicatat sebagai apa?")
			h.wantButtons("Penghasilan", "Transfer sendiri", "Abaikan")
			h.wantProposalTransactions(0)
			h.tapLabel("Penghasilan")
			h.wantCard("✅ Tersimpan: Penghasilan · Rp1.500.000")
			h.wantNoButtons()
			h.wantProposalTransactions(1)
		}},
		{"transfer sendiri records nothing", amountKind, []contractOption{income}, func(h *contractHarness) {
			h.reply("1,5jt")
			h.tapLabel("Transfer sendiri")
			h.wantCard("🗑️ Diabaikan. Tidak dicatat.")
			h.wantProposalTransactions(0)
			h.wantItem("RESOLVED")
		}},
		{"the income choice can be typed", amountKind, []contractOption{income}, func(h *contractHarness) {
			h.reply("1,5jt")
			h.reply("penghasilan")
			h.wantCard("✅ Tersimpan: Penghasilan · Rp1.500.000")
			h.wantProposalTransactions(1)
		}},
		{"a plain yes on the income choice re-shows it and records nothing", amountKind, []contractOption{income}, func(h *contractHarness) {
			h.reply("1,5jt")
			h.plain("ya")
			h.wantCard("Rp1.500.000 ini dicatat sebagai apa?")
			h.wantProposalTransactions(0)
		}},
		{"a stale amount-step tap on the income step is refused", amountKind, []contractOption{income}, func(h *contractHarness) {
			h.reply("1,5jt")
			h.tap("review:date:today")
			h.wantCard("Pilihan itu tidak berlaku untuk kartu ini.", "ini dicatat sebagai apa?")
			h.wantProposalTransactions(0)
		}},
		{"ignoring an amount card records nothing", amountKind, nil, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantCard("🗑️ Diabaikan. Tidak dicatat.")
			h.wantProposalTransactions(0)
			h.wantItem("RESOLVED")
		}},
		{"answering on one phone closes the other member's copy", amountKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, _, _ := h.card(h.chatB)
			if messageB == 0 {
				t.Fatal("the second member never received the card")
			}
			h.reply("75rb")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 AND payload_json->>'status'='RESOLVED'`, 1, h.chatB, messageB)
		}},
	}
	runContractRows(t, rows)
}

func TestInteractionEngineFactsContract(t *testing.T) {
	yesterday, _ := yesterdayLabel()
	rows := []contractRow{
		{"a missing transaction date is a one-tap step that finishes the card", dateKind, nil, func(h *contractHarness) {
			h.wantCard("Transaksi ini terjadi tanggal berapa?", "Nominal: Rp25.000")
			h.wantButtons("Hari ini", "Kemarin", "Tanggal lain", "Abaikan")
			h.tapLabel("Kemarin")
			h.wantCard("✅ Tersimpan: Makan · Rp25.000", "Tanggal: "+yesterday)
			h.wantNoButtons()
			h.wantItem("RESOLVED")
			h.wantModelCalls(0)
		}},
		{"the date typed as a reply", dateKind, nil, func(h *contractHarness) {
			h.reply("3 Nov")
			h.wantCard("✅ Tersimpan: Makan · Rp25.000")
			h.wantAnswered("REPLY")
		}},
		{"the date typed as a plain message to the single waiting card", dateKind, nil, func(h *contractHarness) {
			h.plain("3 Nov")
			h.wantCard("✅ Tersimpan: Makan · Rp25.000")
			h.wantAnswered("PLAIN")
		}},
		{"the date typed as a thread reply", dateKind, nil, func(h *contractHarness) {
			h.threadReply("3 Nov")
			h.wantCard("✅ Tersimpan: Makan · Rp25.000")
			h.wantAnswered("THREAD_REPLY")
		}},
		{"a future date is refused and the step is shown again", dateKind, nil, func(h *contractHarness) {
			h.reply("25/12/2099")
			h.wantCard("Tanggal itu belum terjadi.", "Transaksi ini terjadi tanggal berapa?")
			h.wantTransaction("NEEDS_REVIEW", "", "Makan")
		}},
		{"Ubah date step takes a date typed as a reply and corrects the saved transaction", dateKind, nil, func(h *contractHarness) {
			h.tapLabel("Kemarin")
			h.openUbahDate()
			h.wantCard("Transaksi ini terjadi tanggal berapa?", "ketik tanggalnya")
			h.reply("3 Nov")
			h.wantCard("✅ Tersimpan: Makan · Rp25.000", "Tanggal diubah ke")
			h.wantButtons("Ubah")
			h.wantJobs(`SELECT count(*) FROM transaction WHERE id=$1 AND to_char(transaction_at AT TIME ZONE 'Asia/Jakarta','MM-DD')='11-03'`, 1, h.txID)
			h.wantJobs(`SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='EDIT_TRANSACTION'`, 1, h.txID)
			h.wantAssistantCalls(0)
		}},
		{"Ubah date step takes a date typed as a plain message", dateKind, nil, func(h *contractHarness) {
			h.tapLabel("Kemarin")
			h.openUbahDate()
			h.plain("3 Nov")
			h.wantCard("Tanggal diubah ke")
			h.wantJobs(`SELECT count(*) FROM transaction WHERE id=$1 AND to_char(transaction_at AT TIME ZONE 'Asia/Jakarta','MM-DD')='11-03'`, 1, h.txID)
			h.wantJobs(`SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='EDIT_TRANSACTION'`, 1, h.txID)
			h.wantAssistantCalls(0)
		}},
		{"Ubah date step refuses a future date and changes nothing", dateKind, nil, func(h *contractHarness) {
			h.tapLabel("Kemarin")
			h.openUbahDate()
			h.plain("25/12/2099")
			h.wantCard("Tanggal itu belum terjadi.", "Transaksi ini terjadi tanggal berapa?")
			h.wantJobs(`SELECT count(*) FROM transaction WHERE id=$1 AND (transaction_at AT TIME ZONE 'Asia/Jakarta')::date=(now() AT TIME ZONE 'Asia/Jakarta')::date-1`, 1, h.txID)
			h.wantJobs(`SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='EDIT_TRANSACTION'`, 0, h.txID)
			h.wantAssistantCalls(0)
		}},
		{"a date that is too old is refused", dateKind, nil, func(h *contractHarness) {
			h.reply("1/1/2020")
			h.wantCard("Tanggal itu terlalu lama.", "Transaksi ini terjadi tanggal berapa?")
			h.wantTransaction("NEEDS_REVIEW", "", "Makan")
		}},
		{"a plain yes re-shows the date step", dateKind, nil, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Transaksi ini terjadi tanggal berapa?")
			h.wantTransaction("NEEDS_REVIEW", "", "Makan")
		}},
		{"a stale tap is refused with the step shown again", dateKind, nil, func(h *contractHarness) {
			h.tap("review:cat:00000000-0000-0000-0000-000000000000")
			h.wantCard("Transaksi ini terjadi tanggal berapa?")
			h.wantTransaction("NEEDS_REVIEW", "", "Makan")
		}},
		{"a question goes to the assistant and the card stays", dateKind, nil, func(h *contractHarness) {
			messageID, text, _ := h.current()
			h.chat("berapa pengeluaranku minggu ini?")
			h.wantAssistantCalls(1)
			if after, afterText, _ := h.current(); after != messageID || afterText != text {
				t.Fatalf("the card changed: %d %q", after, afterText)
			}
		}},
		{"answering on one phone closes the other member's copy", dateKind, []contractOption{withSecondMember()}, func(h *contractHarness) {
			messageB, _, _ := h.card(h.chatB)
			h.tapLabel("Hari ini")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND (payload_json->>'chat_id')::bigint=$1 AND (payload_json->>'message_id')::bigint=$2 AND payload_json->>'status'='RESOLVED'`, 1, h.chatB, messageB)
		}},
		{"a compound card asks the date, then the category, on the same card", factsKind, nil, func(h *contractHarness) {
			h.wantCard("Transaksi ini terjadi tanggal berapa?")
			h.tapLabel("Hari ini")
			h.wantCard("✅ Tanggal", "Masuk kategori apa?")
			h.wantButtons("Makan", "Transportasi", "Abaikan")
			h.tapLabel("Makan")
			h.wantCard("Rp25.000")
			h.wantTransaction("CONFIRMED", "", "Makan")
			h.wantItem("RESOLVED")
		}},
		{"a compound card asks the date, then the merchant, then the category, one at a time", factsKind, []contractOption{withMissingFacts("category", "merchant", "transaction_at")}, func(h *contractHarness) {
			h.wantCard("Transaksi ini terjadi tanggal berapa?")
			h.tapLabel("Hari ini")
			h.wantCard("✅ Tanggal", "Ini bayar ke mana?")
			h.reply("Warung Contoh")
			h.wantCard("✅ Warung Contoh disimpan. Masuk kategori apa?")
			h.tapLabel("Makan")
			h.wantTransaction("CONFIRMED", "Warung Contoh", "Makan")
		}},
		{"an unknown purpose is asked plainly and then the category", purposeKind, nil, func(h *contractHarness) {
			h.wantCard("Ini untuk apa?")
			h.wantButtons("Abaikan")
			h.reply("bayar listrik")
			h.wantCard("✅ Tujuannya disimpan.", "Masuk kategori apa?")
			h.tapLabel("Belanja")
			h.wantCard("Rp25.000")
			h.wantTransaction("CONFIRMED", "", "Belanja")
		}},
		{"a purpose on a transaction that already has a category finishes the card", purposeKind, []contractOption{withCategory()}, func(h *contractHarness) {
			h.reply("bayar listrik")
			h.wantCard("✅ Tersimpan: Makan · Rp25.000")
			h.wantNoButtons()
			h.wantTransaction("CONFIRMED", "", "Makan")
			h.wantItem("RESOLVED")
		}},
		{"the purpose typed as a plain message to the single waiting card", purposeKind, nil, func(h *contractHarness) {
			h.plain("bayar listrik")
			h.wantCard("Masuk kategori apa?")
			h.wantAnswered("PLAIN")
		}},
		{"the purpose typed as a thread reply", purposeKind, nil, func(h *contractHarness) {
			h.threadReply("bayar listrik")
			h.wantCard("Masuk kategori apa?")
			h.wantAnswered("THREAD_REPLY")
		}},
		{"unusable purpose text is asked again", purposeKind, nil, func(h *contractHarness) {
			h.reply("!!!")
			h.wantCard("Maaf, aku belum menangkap itu. Ini untuk apa?")
		}},
		{"a plain yes re-shows the purpose step", purposeKind, nil, func(h *contractHarness) {
			h.plain("ya")
			h.wantCard("Ini untuk apa?")
			h.wantTransaction("NEEDS_REVIEW", "", "")
		}},
		{"a message that looks like a new expense asks answer or new", purposeKind, nil, func(h *contractHarness) {
			h.chat("makan siang 25rb")
			h.wantJobs(`SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text'=$2`, 1, h.f.chatID, answerOrNewQuestion)
			h.wantAssistantCalls(0)
		}},
		{"a purpose card ignored settles without a record", purposeKind, nil, func(h *contractHarness) {
			h.tapLabel("Abaikan")
			h.wantNoButtons()
			h.wantItem("RESOLVED")
		}},
	}
	runContractRows(t, rows)
}
