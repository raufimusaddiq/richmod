// Indonesian labels for values the API returns as machine enums. Screens show these
// instead of the raw value (MUTUAL_FUND, TRANSFER_PROOF, ...). Every map falls back to
// the raw value through label(), so a value added to the backend degrades to its code
// instead of disappearing; tests/ui-audit-locks.test.mjs checks the document types
// against the worker's own list so a new one is noticed.

export const wealthTypeLabel = { BANK: "Bank", CASH: "Tunai", EWALLET: "Dompet digital", MUTUAL_FUND: "Reksa dana", GOLD: "Emas", BROKERAGE: "Rekening efek", DEPOSIT: "Deposito", CRYPTO: "Kripto", LOAN: "Pinjaman", OTHER: "Lainnya" };
export const usageRoleLabel = { TRANSACTIONAL: "Transaksional", SAVINGS: "Tabungan", INVESTMENT: "Investasi", OTHER: "Lainnya" };
export const sideLabel = { ASSET: "Aset", LIABILITY: "Kewajiban" };
export const relationshipLabel = { OWN_ACCOUNT: "Rekening sendiri", HOUSEHOLD_ACCOUNT: "Rekening keluarga", INVESTMENT_ACCOUNT: "Investasi / RDN", OTHER: "Lainnya" };
export const capabilityLabel = { CASH_MOVEMENT: "Pergerakan dana", WEALTH_VALUE: "Nilai aset / portfolio" };
export const financialSourceStatusLabel = { ACTIVE: "Aktif", DISABLED: "Dinonaktifkan", PROVISIONED: "Menunggu aktivasi" };

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
