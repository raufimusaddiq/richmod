import { monthLabel } from "./format.js";

// Calendar-view helpers. The API decides which months exist and what they total; this
// module validates a requested range with the API's rules (so the reason shows beside
// the field), labels the running month, and turns the API's English 400 reasons into
// guidance.

const MAX_CUSTOM_MONTHS = 24;
const MONTH = /^\d{4}-(0[1-9]|1[0-2])$/;

// "YYYY-MM" in Asia/Jakarta. Used only to label the running month and to bound the
// month pickers; the API remains authoritative.
export function currentMonthKey(now = new Date()) {
  const parts = new Intl.DateTimeFormat("en-US", { timeZone: "Asia/Jakarta", year: "numeric", month: "2-digit" }).formatToParts(now);
  return `${parts.find(part => part.type === "year").value}-${parts.find(part => part.type === "month").value}`;
}

export function addMonths(key, delta) {
  const [year, month] = key.split("-").map(Number);
  const total = year * 12 + (month - 1) + delta;
  return `${String(Math.floor(total / 12)).padStart(4, "0")}-${String((total % 12) + 1).padStart(2, "0")}`;
}

export function nextMonthKey(now = new Date()) {
  return addMonths(currentMonthKey(now), 1);
}

// Inclusive count of months from `from` to `to`.
export function monthSpan(from, to) {
  const [fromYear, fromMonth] = from.split("-").map(Number);
  const [toYear, toMonth] = to.split("-").map(Number);
  return (toYear - fromYear) * 12 + (toMonth - fromMonth) + 1;
}

// Mirrors the API: both months required, from <= to, at most 24 months, and the
// last month at most next month. Returns an empty string when the range is valid.
export function validateCustomRange(from, to, now = new Date()) {
  if (!MONTH.test(from || "") || !MONTH.test(to || "")) return "Isi bulan mulai dan bulan selesai.";
  if (from > to) return "Bulan mulai harus sebelum atau sama dengan bulan selesai.";
  if (monthSpan(from, to) > MAX_CUSTOM_MONTHS) return `Rentang kustom paling lama ${MAX_CUSTOM_MONTHS} bulan.`;
  if (to > nextMonthKey(now)) return "Bulan selesai paling jauh bulan depan.";
  return "";
}

const serverReasons = {
  "custom range must span 1 to 24 months": "Rentang kustom harus 1 sampai 24 bulan, dan bulan selesai paling jauh bulan depan.",
  "from and to months are both required": "Isi bulan mulai dan bulan selesai.",
  "invalid from month": "Bulan mulai tidak valid.",
  "invalid to month": "Bulan selesai tidak valid.",
  "range must be 3, 6, or 12 months": "Pilih rentang 3, 6, atau 12 bulan.",
};

export function calendarErrorMessage(status, serverReason) {
  if (serverReasons[serverReason]) return serverReasons[serverReason];
  if (status === 400) return "Rentang tidak dapat dipakai. Periksa bulan mulai dan bulan selesai.";
  return "Analisis kalender belum dapat dimuat. Coba lagi.";
}

// A zero-filled month (no confirmed transactions at all) is shown as such, not as
// Rp0 of income and spending.
export function hasActivity(item) {
  return ["income", "expense", "refund"].some(key => item?.[key] != null && Number(item[key]) !== 0);
}

// A range can start months before the household's first confirmed transaction. Those
// leading months are one line ("Data mulai ..."), not a "belum ada transaksi" row each.
// Returns the index of the first month with activity, or -1 when the range is empty.
export function firstActiveMonth(items = []) {
  return items.findIndex(hasActivity);
}

// The refund column is all Rp0 for most households, so it appears only when a month in
// the range actually had a refund.
export function hasRefunds(items = []) {
  return items.some(item => item?.refund != null && Number(item.refund) !== 0);
}

// The category chart shows the ten largest categories plus one "Kategori lain" bar, so
// it stays a chart. The order is the server's (the browser never re-ranks money); the
// full list stays in "Lihat nilai kategori".
export const CHART_CATEGORY_LIMIT = 10;

function rupiah(value) {
  try { return BigInt(String(value ?? "0").trim().split(".")[0] || "0"); } catch { return 0n; }
}

export function chartCategories(items = [], limit = CHART_CATEGORY_LIMIT) {
  if (items.length <= limit) return items;
  const rest = items.slice(limit).reduce((sum, item) => sum + rupiah(item.amount), 0n);
  return [...items.slice(0, limit), { id: "other", name: "Kategori lain", amount: rest.toString() }];
}

// The month fields start on the range already shown, so a phone never renders two
// empty native month inputs. Display only: the API still decides which months exist.
export function defaultRange({ from, to, range }, now = new Date()) {
  if (from && to) return { from, to };
  const end = currentMonthKey(now);
  return { from: addMonths(end, -(Number(range || 3) - 1)), to: end };
}

export function monthRowLabel(period, now = new Date()) {
  return `${monthLabel(period)}${period === currentMonthKey(now) ? " · berjalan" : ""}`;
}
