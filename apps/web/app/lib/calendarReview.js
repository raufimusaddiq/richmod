import { monthLabel } from "./format.js";

const MAX_CUSTOM_MONTHS = 24;
const MONTH = /^\d{4}-(0[1-9]|1[0-2])$/;

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

const MONTH_CHOICES = 36;

export function monthName(key) {
  const [year, month] = key.split("-").map(Number);
  return new Date(Date.UTC(year, month - 1, 1)).toLocaleDateString("id-ID", { month: "long", year: "numeric", timeZone: "UTC" });
}

export function monthChoices(latest, ...selected) {
  const keys = new Set(Array.from({ length: MONTH_CHOICES }, (_, index) => addMonths(latest, -index)));
  for (const key of selected) if (MONTH.test(key || "")) keys.add(key);
  return [...keys].sort().reverse().map(key => ({ value: key, label: monthName(key) }));
}

export function monthSpan(from, to) {
  const [fromYear, fromMonth] = from.split("-").map(Number);
  const [toYear, toMonth] = to.split("-").map(Number);
  return (toYear - fromYear) * 12 + (toMonth - fromMonth) + 1;
}

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

export function hasActivity(item) {
  return ["income", "expense", "refund"].some(key => item?.[key] != null && Number(item[key]) !== 0);
}

export function firstActiveMonth(items = []) {
  return items.findIndex(hasActivity);
}

export function hasRefunds(items = []) {
  return items.some(item => item?.refund != null && Number(item.refund) !== 0);
}

export const CHART_CATEGORY_LIMIT = 10;

function rupiah(value) {
  try { return BigInt(String(value ?? "0").trim().split(".")[0] || "0"); } catch { return 0n; }
}

export function chartCategories(items = [], limit = CHART_CATEGORY_LIMIT) {
  if (items.length <= limit) return items;
  const rest = items.slice(limit).reduce((sum, item) => sum + rupiah(item.amount), 0n);
  return [...items.slice(0, limit), { id: "other", name: "Kategori lain", amount: rest.toString(), remainder: true }];
}

export function rangeSummary(from, to) {
  return from === to ? monthLabel(from) : `${monthLabel(from)} sampai ${monthLabel(to)}`;
}

export function defaultRange({ from, to, range }, now = new Date()) {
  if (from && to) return { from, to };
  const end = currentMonthKey(now);
  return { from: addMonths(end, -(Number(range || 3) - 1)), to: end };
}

export function monthRowLabel(period, now = new Date()) {
  return `${monthLabel(period)}${period === currentMonthKey(now) ? " · berjalan" : ""}`;
}
