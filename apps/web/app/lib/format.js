export const rupiah = new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 });

export function money(value) {
  try { return rupiah.format(BigInt(value || "0")); } catch { return "Rp0"; }
}

export function dateTime(value) {
  if (!value) return "—";
  return new Date(value).toLocaleString("id-ID", { timeZone: "Asia/Jakarta", dateStyle: "medium", timeStyle: "short" });
}

export function monthLabel(value) {
  if (!value) return "";
  const [year, month] = value.split("-").map(Number);
  return new Date(Date.UTC(year, month - 1, 1)).toLocaleDateString("id-ID", { month: "short", year: "2-digit", timeZone: "UTC" });
}

export function percent(value) {
  return `${Math.round(Number(value || 0) * 100)}%`;
}

export const typeLabel = { INCOME: "Pemasukan", EXPENSE: "Pengeluaran", REFUND: "Refund", TRANSFER: "Transfer", ADJUSTMENT: "Penyesuaian", UNCLASSIFIED: "Belum diklasifikasi" };
export const statusLabel = { CONFIRMED: "Terkonfirmasi", NEEDS_REVIEW: "Perlu review", VOIDED: "Diabaikan", PENDING: "Tertunda" };
