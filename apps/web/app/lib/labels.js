// Indonesian labels for values the API returns as machine enums (MUTUAL_FUND,
// TRANSFER_PROOF, ...). Every map falls back to the raw value through label(), so a
// backend value added later degrades to its code; tests/ui-audit-locks.test.mjs checks
// the document types against the worker's own list.

export const wealthTypeLabel = { BANK: "Bank", CASH: "Tunai", EWALLET: "Dompet digital", MUTUAL_FUND: "Reksa dana", GOLD: "Emas", BROKERAGE: "Rekening efek", DEPOSIT: "Deposito", CRYPTO: "Kripto", LOAN: "Pinjaman", OTHER: "Lainnya" };
export const usageRoleLabel = { TRANSACTIONAL: "Transaksional", SAVINGS: "Tabungan", INVESTMENT: "Investasi", OTHER: "Lainnya" };
export const sideLabel = { ASSET: "Aset", LIABILITY: "Kewajiban" };
export const relationshipLabel = { OWN_ACCOUNT: "Rekening sendiri", HOUSEHOLD_ACCOUNT: "Rekening keluarga", INVESTMENT_ACCOUNT: "Investasi / RDN", OTHER: "Lainnya" };
export const capabilityLabel = { CASH_MOVEMENT: "Pergerakan dana", WEALTH_VALUE: "Nilai aset / portfolio" };
export const financialSourceStatusLabel = { ACTIVE: "Aktif", DISABLED: "Dinonaktifkan", PROVISIONED: "Menunggu aktivasi" };
export const emailDeliveryStatusLabel = { INGESTED: "Diproses", IGNORED_AUTH: "Diabaikan", IGNORED_UNMATCHED: "Diabaikan", DUPLICATE: "Duplikat", PROVISIONED_RECEIVED: "Diterima sebelum aktivasi" };
export const emailDeliveryReasonLabel = { UNTRUSTED_AUTHENTICATION: "Pengirim tidak lolos verifikasi autentikasi email.", TRUSTED_AUTHSERV_UNCONFIGURED: "Verifikasi autentikasi belum dikonfigurasi di server.", UNMATCHED_SENDER: "Pengirim tidak cocok dengan daftar pengirim tepercaya.", DUPLICATE_PAYLOAD: "Email yang sama sudah pernah diterima." };

// The ten types the document worker can assign (apps/worker/internal/document/processor.go),
// plus two older names that may still exist on stored documents.
export const documentTypeLabels = {
  RECEIPT: "Struk",
  PAYSLIP: "Slip gaji",
  BANK_TRANSACTION_SCREENSHOT: "Tangkapan layar transaksi bank",
  TRANSFER_PROOF: "Bukti transfer",
  EWALLET_SCREENSHOT: "Tangkapan layar dompet digital",
  BILL_OR_INVOICE: "Tagihan atau faktur",
  TRANSACTION_HISTORY_SCREENSHOT: "Riwayat transaksi",
  WEALTH_OBSERVATION: "Saldo kekayaan",
  OTHER_FINANCIAL_DOCUMENT: "Dokumen keuangan lain",
  NON_FINANCIAL_OR_UNSUPPORTED: "Bukan dokumen keuangan",
  TRANSACTION_SCREENSHOT: "Bukti transaksi",
  BANK_STATEMENT: "Mutasi rekening",
};

export function label(map, value, fallback = "") {
  return map[value] || value || fallback;
}

export function labels(map, values) {
  return (values || []).map(value => label(map, value)).join(", ");
}

// Transaction audit trail ("Riwayat Perubahan"): the action codes the API writes.
// An action added later falls back to a readable form, never the raw code.
export const auditActionLabel = {
  CREATE: "Transaksi dibuat",
  CREATE_FROM_BANK_EMAIL: "Dibuat dari email bank",
  UPDATE: "Transaksi diperbarui",
  UPDATE_DETAILS: "Detail transaksi diubah",
  UPDATE_REVIEW_DETAIL: "Detail tinjauan diperbarui",
  UPDATE_REVIEW_FACTS: "Fakta tinjauan diperbarui",
  RESOLVE_REVIEW: "Tinjauan diselesaikan",
  RESOLVE_DOCUMENT_REVIEW: "Tinjauan dokumen diselesaikan",
  CONFIRM_REVIEW: "Tinjauan dikonfirmasi",
  REJECT_REVIEW: "Tinjauan ditolak",
  MERGE_REVIEW: "Digabung dengan transaksi lain",
  MERGE_EXISTING: "Digabung dengan transaksi yang ada",
  REVERSE_MERGE: "Penggabungan dibatalkan",
  CLASSIFY_TRANSFER: "Transfer diklasifikasikan",
  CONFIRM_NEW_TRANSFER: "Transfer baru dikonfirmasi",
  TELEGRAM_CONFIRMED: "Dikonfirmasi lewat Telegram",
  TELEGRAM_REJECTED: "Ditolak lewat Telegram",
  TELEGRAM_MERGE_REVIEW: "Digabung lewat Telegram",
  CONFIRMED: "Transaksi dikonfirmasi",
  VOIDED: "Transaksi diabaikan",
  IGNORE: "Diabaikan",
  AUTO_CONFIRM_SCREENSHOT_ROW: "Baris tangkapan layar dikonfirmasi otomatis",
  CREATE_FROM_TELEGRAM: "Dibuat dari Telegram",
  CREATE_SCREENSHOT_REVIEW: "Tinjauan tangkapan layar dibuat",
  LINK_BANK_EMAIL_EVIDENCE: "Bukti email bank ditautkan",
  LINK_RECEIPT_EVIDENCE: "Bukti struk ditautkan",
  LINK_SCREENSHOT_EVIDENCE: "Bukti tangkapan layar ditautkan",
  AUTO_CONFIRM_RECEIPT: "Struk dikonfirmasi otomatis",
  CREATE_RECEIPT_REVIEW: "Tinjauan struk dibuat",
  CREATE_FROM_PAYSLIP: "Dibuat dari slip gaji",
  REMEMBER_MERCHANT: "Merchant diingat",
  EDIT_TRANSACTION: "Transaksi diubah",
  CONFIRM_PENDING_BATCH: "Kumpulan transaksi tertunda dikonfirmasi",
  REVIEW_DISTINCT_EVENT: "Ditinjau sebagai kejadian terpisah",
  REVIEW_INCOME_CHOICE: "Pilihan pemasukan ditinjau",
  STAGE_EVIDENCE_DISPUTE: "Sengketa bukti diajukan",
  SET_SCREENSHOT_AMOUNT: "Jumlah dari tangkapan layar ditetapkan",
  REOPEN_LEGACY_UNCATEGORIZED: "Transaksi lama tanpa kategori dibuka kembali",
  ROLLBACK_REOPEN_LEGACY_UNCATEGORIZED: "Pembukaan kembali transaksi lama dibatalkan",
};

// Known code -> Indonesian label; unknown code -> sentence-cased words
// ("SOME_NEW_ACTION" -> "Some new action"); empty -> a dash.
export function auditActionText(action) {
  if (!action) return "—";
  const code = String(action).trim().toUpperCase().replace(/[\s-]+/g, "_");
  return auditActionLabel[action] || auditActionLabel[code] || humanizeCode(action);
}

export const snapshotSourceLabel = { MANUAL: "Input manual", DOCUMENT: "Dokumen", SYSTEM: "Sistem" };

// Who performed an audit-trail action (audit_event.actor_type).
export const actorLabel = { USER: "Pengguna", SYSTEM: "Sistem", WORKER: "Sistem otomatis", TELEGRAM: "Telegram", TELEGRAM_CALLBACK: "Telegram", WEB: "Web", ADMIN: "Admin" };

// Evidence kinds shown under "Sumber pencatatan" on a transaction.
export const evidenceTypeLabel = {
  BANK_EMAIL: "Email bank",
  FINANCIAL_EMAIL: "Email penyedia keuangan",
  TELEGRAM_REVIEW_REPLY: "Balasan tinjauan Telegram",
  TELEGRAM_CALLBACK: "Tombol Telegram",
  TELEGRAM_TEXT: "Pesan Telegram",
  TELEGRAM_IMAGE: "Gambar Telegram",
  MANUAL_ENTRY: "Input manual",
  WEB_MANUAL: "Input web",
  WEB_IMAGE: "Dokumen web",
  PAYSLIP_IMAGE: "Slip gaji",
  RECEIPT_IMAGE: "Struk",
  TRANSACTION_SCREENSHOT: "Bukti transaksi",
  BANK_TRANSACTION_SCREENSHOT: "Tangkapan layar transaksi bank",
  EWALLET_SCREENSHOT: "Tangkapan layar dompet digital",
  TRANSACTION_HISTORY_SCREENSHOT: "Riwayat transaksi",
};

// Unknown codes become sentence-cased words, never the raw enum.
export function humanizeCode(value, fallback = "—") {
  const words = String(value ?? "").toLowerCase().replace(/[_\s]+/g, " ").trim();
  return words ? words[0].toUpperCase() + words.slice(1) : fallback;
}

export const actorText = value => actorLabel[value] || humanizeCode(value);
export const evidenceTypeText = value => evidenceTypeLabel[value] || humanizeCode(value, "Sumber pencatatan");
