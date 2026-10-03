export const reasons = { AMBIGUOUS_CATEGORY: "Kategori belum pasti", MISSING_TRANSACTION_DATE: "Tanggal transaksi belum ada", TRANSACTION_FACTS_MISSING: "Detail transaksi belum lengkap", POSSIBLE_DUPLICATE: "Kemungkinan duplikat", UNKNOWN_MERCHANT: "Tempat transaksi belum dikenal", UNKNOWN_PURPOSE: "Tujuan belum jelas", TRANSFER_CLASSIFICATION: "Transfer perlu klasifikasi" };

export const reviewTypes = { PAYSLIP_CONFIRMATION: "Konfirmasi slip gaji", MISSING_PAY_DATE: "Tanggal pembayaran belum ada", MISSING_AMOUNT: "Jumlah transaksi belum terlihat", FINANCIAL_EMAIL_RESOLUTION: "Email finansial perlu konfirmasi", FINANCIAL_EMAIL_FACTS: "Bukti email finansial belum pasti", TRANSFER_CLASSIFICATION: "Transfer perlu klasifikasi", WEALTH_OBSERVATION_CONFIRMATION: "Observasi kekayaan", CYCLE_RESIDUAL_ALLOCATION: "Alokasi sisa siklus" };

// PRD §13.1: every card answers the same four questions — what happened, what
// Richmod thinks, why it needs you, and the one primary action. Full editing
// controls appear only after "Ubah" (PRD §13.3), and `missingFacts` from the
// stored ReviewDecision decides which inputs a card may require (PRD §13.4).
export function ProposalFacts({ item, known, missing }) {
  const text = value => value === undefined || value === null || value === "" ? null : String(value);
  const knownRow = known.filter(([, value]) => text(value));
  const proposedFacts = item.proposedFacts || {};
  const proposedRow = Object.entries(proposedFacts).filter(([, value]) => text(value));
  return <>{knownRow.length > 0 && <dl className="review-proposal">{knownRow.map(([key, value]) => <div key={key}><dt>{label(key)}</dt><dd>{text(value)}</dd></div>)}</dl>}{proposedRow.length > 0 && <><p className="review-source">Richmod mengusulkan</p><dl className="review-proposal">{proposedRow.map(([key, value]) => <div key={key}><dt>{label(key)}</dt><dd>{text(value)}</dd></div>)}</dl></>}{item.whyNotAutoConfirm && <p className="review-why"><b>Kenapa perlu kamu:</b> {item.whyNotAutoConfirm}</p>}{missing.length > 0 && <p className="review-missing">Yang belum pasti: {missing.map(label).join(", ")}</p>}</>;
}

const fieldLabels = { amount_idr: "Jumlah", amount: "Jumlah", transaction_at: "Waktu", direction: "Arah", merchant: "Merchant", category: "Kategori", categorySlug: "Kategori", funding_account: "Rekening sumber", wealth_account: "Akun kekayaan", wealthAccountId: "Akun kekayaan", transfer_relationship: "Jenis transfer", duplicate_relationship: "Hubungan duplikat", transaction_semantics: "Jenis transaksi", evidence_support: "Dukungan bukti", observation_type: "Jenis observasi", cash_movement: "Pergerakan dana", transaction_ambiguity: "Ambiguitas transaksi", resolvedAccountId: "Rekening sumber", resolvedWealthAccountId: "Akun kekayaan" };

function label(key) { return fieldLabels[key] || (reasons[key] || reviewTypes[key] || String(key).replaceAll("_", " ")); }

export function MissingInputs(item) {
  // The decision contract uses missingFacts; a legacy transaction-backed review
  // carries missingFields from the API instead and never gets a decision. Read
  // whichever the server actually sent; an absent list means the server computed
  // nothing missing, so the card must not invent a required field (PRD 13.4).
  const missing = item.missingFacts || item.missingFields || [];
  return { category: missing.includes("category"), merchant: missing.includes("merchant"), transactionAt: missing.includes("transaction_at") };
}
