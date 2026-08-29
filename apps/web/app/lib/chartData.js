import { monthLabel } from "./format.js";

export function compactCategories(items = [], limit = 5) {
  const sorted = [...items]
    .filter(item => Number(item.amount || 0) > 0)
    .sort((a, b) => Number(b.amount || 0) - Number(a.amount || 0));
  const total = sorted.reduce((sum, item) => sum + Number(item.amount || 0), 0);
  const compact = sorted.slice(0, limit);
  const rest = sorted.slice(limit);
  if (rest.length) {
    compact.push({ id: "other", name: "Lainnya", amount: String(rest.reduce((sum, item) => sum + Number(item.amount || 0), 0)) });
  }
  return compact.map(item => ({ ...item, share: total > 0 ? Number(item.amount || 0) / total : 0 }));
}

export function deriveCycleSpendingMetrics({ daily = [], spent = "0", daysElapsed = 0, daysTotal = 0 }) {
  const elapsed = Math.max(Number(daysElapsed || 0), 0);
  const normalized = daily.map(item => ({ ...item, expenseValue: Number(item.expense || 0) }));
  const visible = normalized.slice(0, elapsed || normalized.length);
  const peak = visible.reduce((best, item) => item.expenseValue > best.expenseValue ? item : best, { period: null, expenseValue: 0 });
  return {
    average: Number(spent || 0) / Math.max(elapsed, 1),
    peak,
    zeroSpendDays: visible.filter(item => item.expenseValue === 0).length,
    daysElapsed: elapsed,
    daysTotal: Math.max(Number(daysTotal || 0), 0),
  };
}

export function mapDailySpending(items = []) {
  return items.map(item => ({ ...item, label: item.period?.slice(8) || "", expenseValue: Number(item.expense || 0) }));
}

export function dayLabel(value) {
  if (!value) return "";
  return new Date(`${value}T00:00:00+07:00`).toLocaleDateString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", year: "numeric" });
}

export function mapMonthlyCashflow(items = []) {
  return items.map(item => ({ ...item, label: monthLabel(item.period), incomeValue: Number(item.income || 0), expenseValue: Number(item.expense || 0), netValue: Number(item.netCashflow || 0) }));
}
