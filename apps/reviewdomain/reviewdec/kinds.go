package reviewdec

type Kind struct {
	Reason string

	Class       string
	Missing     []string
	Actions     []string
	Mode        string
	Why         string
	Consequence Consequence
	Affected    []string
	KnownNil    []string
	Choices     []Choice

	Inbox []string
	Agent []string

	Telegram        bool
	TelegramActions []string
	WhyID           string
}

var transactionDateChoices = []Choice{{Key: "TODAY", Label: "Hari ini"}, {Key: "YESTERDAY", Label: "Kemarin"}, {Key: "MESSAGE_DATE", Label: "Tanggal pesan"}}

var kinds = []Kind{
	{
		Reason: "CYCLE_RESIDUAL_ALLOCATION", Class: ClassHumanPolicyChoice,
		Missing:  []string{"residual_allocation"},
		Actions:  []string{"ALLOCATE_RETAINED_BALANCE", "TRANSACTION_MISSING", "LEAVE_UNALLOCATED"},
		Mode:     ModePolicyChoice,
		Why:      "allocating retained cycle money is household intent and must not be inferred",
		WhyID:    "Alokasi sisa saldo adalah keputusan keluarga, jadi tidak ditebak.",
		Telegram: true,
		Inbox:    []string{"ALLOCATE_RETAINED_BALANCE", "TRANSACTION_MISSING", "LEAVE_UNALLOCATED"},
		Agent:    []string{"ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED", "TRANSACTION_MISSING"},
	},
	{
		Reason: "WEALTH_OBSERVATION_CONFIRMATION", Class: ClassEvidenceGap,
		Missing:         []string{"wealth_snapshot_confirmation"},
		Actions:         []string{"SET_WEALTH_ACCOUNT", "IGNORE"},
		Mode:            ModeBoundedChoice,
		Why:             "a wealth value is an observation; the complete active account set must be confirmed",
		WhyID:           "Nilai kekayaan hanya observasi; akunnya perlu dipastikan.",
		Telegram:        true,
		TelegramActions: []string{"SET_WEALTH_ACCOUNT", "RECORD_ASSET_PURCHASE", "IGNORE"},
		Inbox:           []string{"SET_WEALTH_ACCOUNT", "IGNORE"},
	},
	{
		Reason: "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", Class: ClassEvidenceGap,
		Missing:  []string{"document_extraction"},
		Actions:  []string{"REPROCESS_DOCUMENT", "IGNORE"},
		Mode:     ModeSingleField,
		Why:      "the document extraction could not be validated deterministically",
		WhyID:    "Isi dokumen belum bisa divalidasi otomatis.",
		Telegram: true,
		Inbox:    []string{"REPROCESS_DOCUMENT", "IGNORE"},
		Agent:    []string{"REPROCESS_DOCUMENT", "IGNORE"},
	},
	{
		Reason: "DOCUMENT_CLASSIFICATION", Class: ClassEvidenceGap,
		Missing:  []string{"document_type"},
		Actions:  []string{"REPROCESS_DOCUMENT", "IGNORE"},
		Mode:     ModeSingleField,
		Why:      "the document type could not be classified with enough confidence",
		WhyID:    "Jenis dokumen belum bisa dikenali dengan yakin.",
		Telegram: true,
		Inbox:    []string{"REPROCESS_DOCUMENT", "IGNORE"},
		Agent:    []string{"REPROCESS_DOCUMENT", "IGNORE"},
	},
	{
		Reason: "UNKNOWN_BANK_TEMPLATE", Class: ClassEvidenceGap,
		Missing:  []string{"transaction_semantics"},
		Actions:  []string{"COMPLETE_BANK_FACTS", "IGNORE"},
		Mode:     ModeBoundedChoice,
		Why:      "the bank evidence could not be mapped to a supported transaction type",
		WhyID:    "Format bukti bank ini belum bisa dipetakan ke jenis transaksi.",
		Telegram: true,
		Inbox:    []string{"COMPLETE_BANK_FACTS", "IGNORE"},
		Agent:    []string{"COMPLETE_BANK_FACTS", "IGNORE"},
	},
	{
		Reason: "PAYSLIP_CONFIRMATION", Class: ClassHumanPolicyChoice,
		Missing:  []string{"salary_classification"},
		Actions:  []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"},
		Mode:     ModePolicyChoice,
		Why:      "the first salary from a source is a human policy choice",
		WhyID:    "Gaji pertama dari sumber ini perlu dipilih jenisnya.",
		Telegram: true,
		Inbox:    []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"},
		Agent:    []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"},
	},
	{
		Reason: "MISSING_PAY_DATE", Class: ClassEvidenceGap,
		Missing:  []string{"transaction_at"},
		Actions:  []string{"SET_PAY_DATE", "IGNORE"},
		Mode:     ModeSingleField,
		Why:      "the pay date is not present in the evidence",
		WhyID:    "Tanggal pembayaran tidak tertulis di bukti.",
		Telegram: true,
		Inbox:    []string{"SET_PAY_DATE", "IGNORE"},
		Agent:    []string{"SET_PAY_DATE", "IGNORE"},
	},
	{
		Reason: "MISSING_TRANSACTION_DATE", Class: ClassEvidenceGap,
		Missing:  []string{"transaction_at"},
		Actions:  []string{"SET_TRANSACTION_DATE", "IGNORE"},
		Mode:     ModeSingleField,
		Why:      "the source did not print or state the transaction date",
		Choices:  transactionDateChoices,
		WhyID:    "Tanggal transaksi tidak tertulis di bukti.",
		Telegram: true,
	},
	{
		Reason: "TRANSACTION_FACTS_MISSING", Class: ClassEvidenceGap,
		Missing:  []string{"category", "transaction_at"},
		Actions:  []string{"CONFIRM_REVIEW", "SET_TRANSACTION_DATE", "IGNORE"},
		Mode:     ModeSingleField,
		Why:      "the source did not support a category or a transaction date",
		Choices:  transactionDateChoices,
		WhyID:    "Bukti tidak menunjukkan kategori dan tanggal transaksi.",
		Telegram: true,
	},
	{
		Reason: "TRANSFER_CLASSIFICATION", Class: ClassEvidenceGap,
		Missing:  []string{"transfer_relationship"},
		Actions:  []string{"CLASSIFY_TRANSFER", "OWN_ACCOUNT", "HOUSEHOLD_ACCOUNT", "INVESTMENT_ACCOUNT", "EXPENSE", "ASSET_PURCHASE", "IGNORE"},
		Mode:     ModeBoundedChoice,
		Why:      "the transfer relationship is ambiguous, so the household must classify it",
		WhyID:    "Jenis transfer tidak bisa dipastikan dari bukti.",
		Telegram: true,
		Agent:    []string{"EXPENSE", "OWN_ACCOUNT_TRANSFER", "HOUSEHOLD_TRANSFER", "INVESTMENT_TRANSFER", "ASSET_PURCHASE", "IGNORE"},
	},
	{
		Reason: "FINANCIAL_EMAIL_RESOLUTION", Class: ClassEvidenceGap,
		Missing:  []string{"funding_account", "wealth_account"},
		Actions:  []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"},
		Mode:     ModeBoundedChoice,
		Why:      "an entity the evidence identifies only in prose must be bound by the household",
		WhyID:    "Rekening di email hanya disebut dalam teks, jadi perlu dipilih.",
		Telegram: true,
		Inbox:    []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"},
		Agent:    []string{"SET_FINANCIAL_EMAIL_ENTITIES", "IGNORE"},
	},
	{
		Reason: "FINANCIAL_EMAIL_FACTS", Class: ClassEvidenceGap,
		Missing:  []string{"evidence_support"},
		Actions:  []string{"IGNORE"},
		Mode:     ModeSingleField,
		Why:      "the provider email does not support all extracted financial facts; no transaction was written",
		WhyID:    "Email tidak mendukung semua data keuangannya; belum ada transaksi yang dicatat.",
		Telegram: true,
		Agent:    []string{"IGNORE"},
	},
	{
		Reason: "UNKNOWN_MERCHANT", Class: ClassEvidenceGap,
		Missing:  []string{"merchant", "category"},
		Actions:  []string{"CONFIRM_REVIEW", "IGNORE"},
		Mode:     ModeSingleField,
		Why:      "merchant is not present in the evidence; collect it before resolving the category",
		WhyID:    "Nama merchant tidak ada di bukti.",
		Telegram: true,
		KnownNil: []string{"merchant"},
		Agent:    []string{"CONFIRM", "ASSET_PURCHASE", "IGNORE"},
	},
	{
		Reason: "AMBIGUOUS_CATEGORY", Class: ClassEvidenceGap,
		Missing:  []string{"category"},
		Actions:  []string{"CONFIRM_REVIEW", "IGNORE"},
		Mode:     ModeSingleField,
		Why:      "the category is not supported strongly enough to confirm",
		WhyID:    "Kategorinya belum cukup pasti untuk dicatat otomatis.",
		Telegram: true,
		Agent:    []string{"CONFIRM", "ASSET_PURCHASE", "IGNORE"},
	},
	{
		Reason: "UNKNOWN_PURPOSE", Class: ClassEvidenceGap,
		Missing:  []string{"transaction_semantics"},
		Actions:  []string{"CONFIRM_REVIEW", "IGNORE"},
		Mode:     ModeSingleField,
		Why:      "the transaction purpose is not supported strongly enough to classify",
		WhyID:    "Tujuan transaksi belum cukup jelas.",
		Telegram: true,
	},
	{
		Reason: "MANUAL_CORRECTION", Class: ClassCorrectionConfirmation,
		Missing:  []string{"correction_details"},
		Actions:  []string{"CONFIRM_REVIEW", "IGNORE"},
		Mode:     ModeSingleField,
		Why:      "the extracted payroll transaction needs a human correction or confirmation",
		WhyID:    "Transaksi dari slip gaji ini perlu dikoreksi atau dikonfirmasi.",
		Telegram: true,
	},
	{
		Reason: "CONFLICTING_EVIDENCE", Class: ClassDuplicateAmbiguity,
		Missing:  []string{"duplicate_relationship"},
		Actions:  []string{"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"},
		Mode:     ModeConflictResolution,
		Why:      "two sources disagree about the same provider reference, so Go refuses to guess",
		WhyID:    "Dua bukti tidak sepakat untuk referensi yang sama.",
		Telegram: true,
	},
	{
		Reason: "POSSIBLE_DUPLICATE", Class: ClassDuplicateAmbiguity,
		Missing:  []string{"duplicate_relationship"},
		Actions:  []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"},
		Mode:     ModeBoundedChoice,
		Why:      "a plausible duplicate exists; choose the matching event, confirm as new, or ignore",
		WhyID:    "Ada catatan lain yang mirip; mungkin duplikat.",
		Telegram: true,
		Agent:    []string{"MERGE_EXISTING", "CONFIRM", "IGNORE"},
	},
	{
		Reason: "RECEIPT_MISMATCH", Class: ClassCorrectionConfirmation,
		Consequence: QualitySignal, Affected: []string{"receipt_arithmetic"},
		Actions:  []string{"CONFIRM_REVIEW", "IGNORE"},
		Mode:     ModeOneTapConfirmation,
		Why:      "the printed lines do not reconcile with the printed total",
		WhyID:    "Rincian struk tidak cocok dengan totalnya.",
		Telegram: true,
	},

	{
		Reason: "MISSING_AMOUNT", Inbox: []string{"SET_AMOUNT", "IGNORE"},
		Telegram: true, TelegramActions: []string{"SET_AMOUNT", "IGNORE"},
		WhyID: "Nominal tidak terlihat di bukti.",
	},
	{Reason: "SALARY_SOURCE_CONFIRMATION", Agent: []string{"PRIMARY_SALARY", "ORDINARY_INCOME", "IGNORE"}},
	{Reason: "TRANSFER_RECONCILIATION", Agent: []string{"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"}},
	{Reason: "WEALTH_OBSERVATION", Agent: []string{"SET_WEALTH_ACCOUNT", "RECORD_ASSET_PURCHASE", "IGNORE"}},
}

var kindByReason = func() map[string]*Kind {
	index := make(map[string]*Kind, len(kinds))
	for i := range kinds {
		index[kinds[i].Reason] = &kinds[i]
	}
	return index
}()

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func ActiveReasons() []string {
	reasons := make([]string, 0, len(kinds))
	for i := range kinds {
		if len(kinds[i].Actions) > 0 {
			reasons = append(reasons, kinds[i].Reason)
		}
	}
	return reasons
}

func InboxActions(reason string) []string {
	if kind, ok := kindByReason[reason]; ok && len(kind.Inbox) > 0 {
		return cloneStrings(kind.Inbox)
	}
	return []string{"IGNORE"}
}

func AgentActions(reason string) []string {
	if kind, ok := kindByReason[reason]; ok && len(kind.Agent) > 0 {
		return cloneStrings(kind.Agent)
	}
	return []string{"CONFIRM", "IGNORE"}
}

func AllAgentActions() []string {
	seen := map[string]bool{}
	var all []string
	add := func(actions []string) {
		for _, action := range actions {
			if !seen[action] {
				seen[action] = true
				all = append(all, action)
			}
		}
	}
	add(AgentActions(""))
	for i := range kinds {
		add(kinds[i].Agent)
	}
	return all
}

func TelegramProjectable(reason string) bool {
	kind, ok := kindByReason[reason]
	return ok && kind.Telegram
}

func TelegramReasons() []string {
	var reasons []string
	for i := range kinds {
		if kinds[i].Telegram {
			reasons = append(reasons, kinds[i].Reason)
		}
	}
	return reasons
}

func TelegramActions(reason string) []string {
	kind, ok := kindByReason[reason]
	if !ok || !kind.Telegram {
		return nil
	}
	if len(kind.TelegramActions) > 0 {
		return cloneStrings(kind.TelegramActions)
	}
	return cloneStrings(kind.Actions)
}

func TelegramCompleteActions() []string {
	seen := map[string]bool{"IGNORE": true}
	var all []string
	for i := range kinds {
		for _, action := range TelegramActions(kinds[i].Reason) {
			if !seen[action] {
				seen[action] = true
				all = append(all, action)
			}
		}
	}
	return all
}

var whyTranslations = map[string]string{
	"the bounded category decision did not reach a confident, well-separated choice":                       "Kategori belum cukup yakin untuk dipilih otomatis.",
	"no supported merchant-to-category mapping or decisive category ruling was available":                  "Belum ada kategori tersimpan untuk merchant ini.",
	"own-account versus expense is a human policy choice the evidence cannot decide":                       "Bukti tidak bisa membedakan transfer ke rekening sendiri dan pengeluaran.",
	"the email did not clearly support an ordinary spending classification":                                "Email tidak jelas menunjukkan pengeluaran biasa.",
	"the screenshot does not show an amount":                                                               "Nominal tidak terlihat di screenshot.",
	"evidence cannot separate income from an own-account or household transfer on a screenshot row":        "Screenshot tidak bisa membedakan penghasilan dan transfer sendiri.",
	"the image and the bounded plane named different categories":                                           "Gambar dan penilaian kategori menunjuk kategori berbeda.",
	"the household disputes a previous screenshot link; explicit human button confirmation is required":    "Tautan screenshot sebelumnya dibantah, jadi perlu konfirmasi lewat tombol.",
	"the provider email points to a different funding account than the canonical account already selected": "Email menunjuk rekening sumber yang berbeda dari yang sudah dipilih.",
	"pay date is absent and the first salary source requires household classification":                     "Tanggal pembayaran tidak ada dan jenis gaji pertama perlu dipilih.",
}

func WhyLine(decision Decision) string {
	if line, ok := whyTranslations[decision.WhyNotAuto]; ok {
		return line
	}
	if kind, ok := kindByReason[decision.ReasonCode]; ok && kind.WhyID != "" {
		return kind.WhyID
	}
	return ""
}
