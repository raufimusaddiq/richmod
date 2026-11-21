const rupiah = new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 });

// One minus sign everywhere: U+2212, never the hyphen Intl emits for negatives.
export function minusSign(text) { return String(text).replace(/^-/, "−"); }

export function money(value) {
  try { return minusSign(rupiah.format(BigInt(String(value ?? "0").trim().split(".")[0] || "0"))); } catch { return "Rp0"; }
}

// Zero is neutral, never a warning: only a real signed amount gets a tone.
// A liability is shown negative when it is above zero, because owing money is
// the negative side even though the stored figure is positive.
export function amountTone(value, liability = false) {
  try {
    const amount = BigInt(String(value ?? "0").trim().split(".")[0] || "0");
    if (amount === 0n) return "";
    return liability || amount < 0n ? "negative" : "positive";
  } catch { return ""; }
}

export function dateTime(value) {
  if (!value) return "—";
  return new Date(value).toLocaleString("id-ID", { timeZone: "Asia/Jakarta", dateStyle: "medium", timeStyle: "short" });
}

// Display date without time, e.g. "22 Okt 2026". Accepts a date-only string
// ("2026-10-22", read as a Jakarta calendar day) or any timestamp.
export function displayDate(value) {
  if (!value) return "—";
  const text = String(value);
  const date = new Date(/^\d{4}-\d{2}-\d{2}$/.test(text) ? `${text}T00:00:00+07:00` : value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", year: "numeric" });
}

export function monthLabel(value) {
  if (!value) return "";
  const [year, month] = value.split("-").map(Number);
  return new Date(Date.UTC(year, month - 1, 1)).toLocaleDateString("id-ID", { month: "short", year: "2-digit", timeZone: "UTC" });
}

export function percent(value) {
  return `${Math.round(Number(value || 0) * 100)}%`;
}

export const typeLabel = { INCOME: "Pemasukan", EXPENSE: "Pengeluaran", REFUND: "Refund", TRANSFER: "Transfer", UNCLASSIFIED: "Belum diklasifikasi" };
export const sourceLabel = { BANK_EMAIL: "Email bank", TELEGRAM_TEXT: "Pesan Telegram", TELEGRAM_IMAGE: "Gambar Telegram", WEB_MANUAL: "Input web", WEB_IMAGE: "Dokumen web", FINANCIAL_EMAIL: "Email finansial", TELEGRAM_CALLBACK: "Telegram", SYSTEM: "Sistem" };
export const statusLabel = { CONFIRMED: "Terkonfirmasi", NEEDS_REVIEW: "Perlu dicek", VOIDED: "Dibatalkan" };
