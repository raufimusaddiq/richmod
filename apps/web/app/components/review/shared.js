// Every review_type the database accepts (review_item_review_type_check) has
// an Indonesian label and a one-line explanation, so no card ever shows a raw
// code such as "UNKNOWN BANK TEMPLATE". tests/review-labels.test.mjs keeps this
// table in step with the latest migration.
export const reviewTypeCopy = {
  UNKNOWN_MERCHANT: ["Tempat transaksi belum dikenal", "Bukti tidak menyebut nama tempat transaksi, jadi kategorinya belum bisa ditentukan."],
  UNKNOWN_PURPOSE: ["Tujuan belum jelas", "Bukti belum cukup untuk menentukan jenis transaksi ini."],
  AMBIGUOUS_CATEGORY: ["Kategori belum pasti", "Richmod belum cukup yakin memilih kategorinya."],
  POSSIBLE_DUPLICATE: ["Kemungkinan duplikat", "Ada transaksi lain dengan jumlah dan waktu mirip; pilih apakah ini kejadian yang sama."],
  CONFLICTING_EVIDENCE: ["Bukti saling bertentangan", "Dua sumber menyebut hal berbeda untuk kejadian yang sama, jadi Richmod tidak menebak."],
  UNKNOWN_EMAIL_TEMPLATE: ["Format email belum dikenal", "Isi email ini belum bisa dibaca sebagai transaksi."],
  RECEIPT_MISMATCH: ["Rincian struk tidak pas", "Jumlah per baris tidak sama dengan total tercetak; total tercetak yang dipakai."],
  DOCUMENT_EXTRACTION_LOW_CONFIDENCE: ["Isi bukti belum terbaca jelas", "Sebagian fakta penting belum bisa dibaca dari bukti ini."],
  TRANSFER_CLASSIFICATION: ["Transfer perlu klasifikasi", "Richmod tidak tahu apakah ini transfer ke rekening sendiri, pengeluaran, atau lainnya."],
  MANUAL_CORRECTION: ["Perlu koreksi", "Transaksi dari dokumen ini perlu kamu periksa sebelum dicatat."],
  DOCUMENT_CLASSIFICATION: ["Jenis dokumen belum dikenal", "Richmod belum yakin ini struk, slip gaji, atau tangkapan layar transaksi."],
  PAYSLIP_CONFIRMATION: ["Konfirmasi slip gaji", "Gaji pertama dari sumber ini perlu kamu tentukan cara mencatatnya."],
  MISSING_PAY_DATE: ["Tanggal pembayaran belum ada", "Slip gaji tidak mencantumkan tanggal pembayaran."],
  SALARY_SOURCE_CONFIRMATION: ["Konfirmasi sumber gaji", "Pilih apakah sumber ini gaji utama atau pemasukan biasa."],
  UNKNOWN_BANK_TEMPLATE: ["Email bank belum terbaca", "Email bank ini belum bisa dipetakan ke transaksi; lengkapi jumlah dan waktunya."],
  INVOICE_PAYMENT_STATUS: ["Status tagihan belum pasti", "Tagihan saja bukan bukti pembayaran."],
  CYCLE_RESIDUAL_ALLOCATION: ["Alokasi sisa siklus", "Ada sisa uang di siklus gaji yang perlu kamu tentukan ke mana perginya."],
  WEALTH_OBSERVATION_CONFIRMATION: ["Observasi kekayaan", "Pilih akun kekayaan untuk nilai yang terlihat di bukti."],
  FINANCIAL_EMAIL_RESOLUTION: ["Email finansial perlu konfirmasi", "Pilih rekening yang hanya disebut samar di email."],
  MISSING_TRANSACTION_DATE: ["Tanggal transaksi belum ada", "Bukti tidak mencantumkan tanggal; pilih tanggal yang paling tepat."],
  TRANSACTION_FACTS_MISSING: ["Detail transaksi belum lengkap", "Kategori dan tanggal transaksi belum ada di bukti."],
  MISSING_AMOUNT: ["Jumlah transaksi belum terlihat", "Tangkapan layar tidak menampilkan jumlahnya."],
  FINANCIAL_EMAIL_FACTS: ["Email finansial dicatat sebagai info", "Email ini tidak cukup mendukung transaksi, jadi tidak ada yang dicatat."],
};

export const reviewTypes = Object.fromEntries(Object.entries(reviewTypeCopy).map(([key, [label]]) => [key, label]));
export const reasons = reviewTypes;

export function reviewLabel(type) { return reviewTypes[type] || "Perlu ditinjau"; }
export function reviewExplanation(type) { return reviewTypeCopy[type]?.[1] || "Richmod membutuhkan konfirmasi untuk bukti ini."; }

// Every card answers the same four questions — what happened, what
// Richmod thinks, why it needs you, and the one primary action. Full editing
// controls appear only after "Ubah", and `missingFacts` from the
// stored ReviewDecision decides which inputs a card may require.
export function ProposalFacts({ item, known, missing }) {
  const text = value => value === undefined || value === null || value === "" ? null : String(value);
  const knownRow = known.filter(([, value]) => text(value));
  const proposedFacts = item.proposedFacts || {};
  const proposedRow = Object.entries(proposedFacts).filter(([, value]) => text(value));
  return <>{knownRow.length > 0 && <dl className="review-proposal">{knownRow.map(([key, value]) => <div key={key}><dt>{label(key)}</dt><dd>{text(value)}</dd></div>)}</dl>}{proposedRow.length > 0 && <><p className="review-source">Richmod mengusulkan</p><dl className="review-proposal">{proposedRow.map(([key, value]) => <div key={key}><dt>{label(key)}</dt><dd>{text(value)}</dd></div>)}</dl></>}{item.whyNotAutoConfirm && <p className="review-why"><b>Kenapa perlu kamu:</b> {item.whyNotAutoConfirm}</p>}{missing.length > 0 && <p className="review-missing">Yang belum pasti: {missing.map(label).join(", ")}</p>}</>;
}

const fieldLabels = { amount_idr: "Jumlah", amount: "Jumlah", transaction_at: "Waktu", direction: "Arah", merchant: "Merchant", category: "Kategori", categorySlug: "Kategori", funding_account: "Rekening sumber", wealth_account: "Akun kekayaan", wealthAccountId: "Akun kekayaan", transfer_relationship: "Jenis transfer", duplicate_relationship: "Hubungan duplikat", transaction_semantics: "Jenis transaksi", evidence_support: "Dukungan bukti", observation_type: "Jenis observasi", cash_movement: "Pergerakan dana", transaction_ambiguity: "Ambiguitas transaksi", resolvedAccountId: "Rekening sumber", resolvedWealthAccountId: "Akun kekayaan", residual_allocation: "Alokasi sisa", wealth_snapshot_confirmation: "Konfirmasi akun", document_extraction: "Isi dokumen", document_type: "Jenis dokumen", salary_classification: "Jenis gaji", correction_details: "Koreksi", receipt_arithmetic: "Rincian struk", transaction_observed: "Bukti transaksi", type: "Jenis", channel: "Saluran", kind: "Jenis", residual_idr: "Sisa" };

function label(key) { return fieldLabels[key] || reviewTypes[key] || "Detail lain"; }

export function MissingInputs(item) {
  // The decision contract uses missingFacts; a legacy transaction-backed review
  // carries missingFields from the API instead and never gets a decision. Read
  // whichever the server actually sent; an absent list means the server computed
  // nothing missing, so the card must not invent a required field.
  const missing = item.missingFacts || item.missingFields || [];
  return { category: missing.includes("category"), merchant: missing.includes("merchant"), transactionAt: missing.includes("transaction_at") };
}
