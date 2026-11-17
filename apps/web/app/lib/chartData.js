import { monthLabel } from "./format.js";

export function compactCategories(items = [], limit = 5) {
  const sorted = [...items]
    .filter(item => Number(item.amount || 0) > 0)
    .sort((a, b) => Number(b.amount || 0) - Number(a.amount || 0));
  const total = sorted.reduce((sum, item) => sum + Number(item.amount || 0), 0);
  const compact = sorted.slice(0, limit);
  const rest = sorted.slice(limit);
  if (rest.length) {
    compact.push({ id: "other", name: "Kategori lain", amount: String(rest.reduce((sum, item) => sum + Number(item.amount || 0), 0)) });
  }
  return compact.map(item => ({ ...item, share: total > 0 ? Number(item.amount || 0) / total : 0 }));
}

export function elapsedDaily(items = [], daysElapsed) {
  if (daysElapsed == null) return items;
  return items.slice(0, Math.max(Number(daysElapsed || 0), 0));
}

export function mapDailySpending(items = []) {
  return items.map(item => ({ ...item, label: item.period?.slice(8) || "", expenseValue: Number(item.expense || 0) }));
}

// Oldest first. Several snapshots can share a day; only the first keeps the
// date label so the ordinal axis never repeats "9 Sep 9 Sep 9 Sep".
export function netWorthHistoryRows(items = []) {
  const seen = new Set();
  return [...items].reverse().map(item => {
    const at = new Date(item.observedAt);
    const day = at.toLocaleDateString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short" });
    const label = seen.has(day) ? "" : day;
    seen.add(day);
    return { label, day, value: Number(item.netWorthIdr || 0), raw: item.netWorthIdr || "0" };
  });
}

export function dayLabel(value) {
  if (!value) return "";
  return new Date(`${value}T00:00:00+07:00`).toLocaleDateString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", year: "numeric" });
}

// `partialPeriod` ("YYYY-MM") marks the running month so the tooltip can say it is not complete.
// Months before the first confirmed transaction are not drawn as blank slots; the table
// still lists them, marked "belum ada transaksi".
export function mapMonthlyCashflow(items = [], partialPeriod = "") {
  const rows = items.map(item => ({ ...item, partial: Boolean(partialPeriod) && item.period === partialPeriod, label: monthLabel(item.period), incomeValue: Number(item.income || 0), expenseValue: Number(item.expense || 0), netValue: Number(item.netCashflow || 0) }));
  const first = rows.findIndex(row => row.incomeValue !== 0 || row.expenseValue !== 0 || Number(row.refund || 0) !== 0);
  return first < 0 ? [] : rows.slice(first);
}
