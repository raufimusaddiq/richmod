import { money, monthLabel } from "./format.js";
import { isZero, signedValue } from "./cycleKpi.js";
import { OTHER_CATEGORY_COLOR, categoryColorMap, categoryKey } from "./categoryColors.js";
import { inclusiveEnd } from "./cycleReview.js";

const MAX_CUSTOM_MONTHS = 24;
const MONTH = /^\d{4}-(0[1-9]|1[0-2])$/;
const shortMonth = new Intl.DateTimeFormat("id-ID", { month: "short", timeZone: "UTC" });

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

export function rangeSummary(from, to) {
  return from === to ? monthLabel(from) : `${monthLabel(from)} sampai ${monthLabel(to)}`;
}

export function calendarPeriodLabel(from, to) {
  if (!MONTH.test(from || "") || !MONTH.test(to || "")) return "";
  const [fromYear, fromMonth] = from.split("-").map(Number);
  const [toYear, toMonth] = to.split("-").map(Number);
  const startMonth = shortMonth.format(new Date(Date.UTC(fromYear, fromMonth - 1, 1)));
  const endMonth = shortMonth.format(new Date(Date.UTC(toYear, toMonth - 1, 1)));
  if (from === to) return `${startMonth} ${fromYear}`;
  if (fromYear === toYear) return `${startMonth} – ${endMonth} ${toYear}`;
  return `${startMonth} ${fromYear} – ${endMonth} ${toYear}`;
}

export function calendarVerdict(summary) {
  if (!summary || !(Number(summary.months) > 0)) return "";
  const parts = [];
  if (summary.averageMonthlyExpenseRounded != null) parts.push(`Rata-rata ${money(summary.averageMonthlyExpenseRounded)} per bulan`);
  const hasPeak = Boolean(summary.peakMonth) && summary.peakMonthExpense != null && String(summary.peakMonthExpense).trim() !== "" && !isZero(summary.peakMonthExpense);
  if (hasPeak) {
    parts.push(`${parts.length ? "tertinggi" : "Tertinggi"} ${monthName(summary.peakMonth)}, ${money(summary.peakMonthExpense)}`);
  }
  return parts.length ? `${parts.join("; ")}.` : "";
}

export function calendarKpiCells(summary, periodText = "") {
  if (!summary) return [];
  const activeMonths = Number(summary.months) || 0;
  const leadLabel = periodText ? `Pengeluaran bersih · ${periodText}` : "Pengeluaran bersih";
  if (activeMonths <= 0) {
    return [
      { key: "expense", lead: true, label: leadLabel, value: money("0"), emptyComparison: "Belum ada transaksi terkonfirmasi pada rentang ini." },
      { key: "income", label: "Pemasukan", value: money("0") },
      { key: "net", label: "Arus kas bersih", value: money("0") },
      { key: "average", label: "Rata-rata / bulan", value: "—", sub: "Belum ada bulan aktif" },
    ];
  }
  return [
    { key: "expense", lead: true, label: leadLabel, value: money(summary.expense), verdict: calendarVerdict(summary) },
    { key: "income", label: "Pemasukan", value: money(summary.income) },
    { key: "net", label: "Arus kas bersih", value: signedValue(summary.netCashflow) },
    { key: "average", label: "Rata-rata / bulan", value: money(summary.averageMonthlyExpenseRounded), sub: `${activeMonths} bulan aktif` },
  ];
}

export function calendarCategoryView(items = [], palette = null) {
  if (!Array.isArray(items)) return { rows: [] };
  const peak = Math.max(0, ...items.map(row => Math.abs(Number(row.amount))).filter(Number.isFinite));
  const { colors } = categoryColorMap(items, palette);
  const rows = items.map((row, index) => {
    const key = row.id || `uncategorized:${index}`;
    const assigned = colors.get(categoryKey(row));
    const color = assigned && assigned !== OTHER_CATEGORY_COLOR ? assigned : "";
    const numeric = Math.abs(Number(row.amount));
    return {
      key,
      id: row.id ?? null,
      name: row.name,
      amount: row.amount,
      shareOfExpense: row.share ?? row.shareOfExpense ?? null,
      length: peak > 0 && Number.isFinite(numeric) ? Math.min(1, numeric / peak) : 0,
      color,
      openable: false,
    };
  });
  return { rows };
}

export function calendarMembers(items = []) {
  return [...items].sort((left, right) => {
    if (left.kind === "AUTOMATIC" && right.kind !== "AUTOMATIC") return -1;
    if (right.kind === "AUTOMATIC" && left.kind !== "AUTOMATIC") return 1;
    return String(left.name || "").localeCompare(String(right.name || ""), "id");
  });
}

export function calendarTransactionHref({ from, to, cycle = "" }, filters = {}) {
  if (!MONTH.test(from || "") || !MONTH.test(to || "")) return "/transactions";
  const query = new URLSearchParams({ from: `${from}-01`, to: inclusiveEnd(`${addMonths(to, 1)}-01`), status: "CONFIRMED", type: "SPENDING" });
  for (const [key, value] of Object.entries(filters)) if (value) query.set(key, value);
  if (cycle) query.set("cycle", cycle);
  return `/transactions?${query}`;
}

export function defaultRange({ from, to, range }, now = new Date()) {
  if (from && to) return { from, to };
  const end = currentMonthKey(now);
  return { from: addMonths(end, -(Number(range || 3) - 1)), to: end };
}

export function monthRowLabel(period, now = new Date()) {
  return `${monthLabel(period)}${period === currentMonthKey(now) ? " · berjalan" : ""}`;
}
