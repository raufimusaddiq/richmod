import { money, monthLabel } from "./format.js";

const jakartaDate = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Jakarta", year: "numeric", month: "2-digit", day: "2-digit" });

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

export function netWorthHistoryRows(items = []) {
  const rows = [...items].reverse().map(item => {
    const at = new Date(item.observedAt);
    return { at, key: jakartaDate.format(at), value: Number(item.netWorthIdr || 0), raw: item.netWorthIdr || "0" };
  });
  const spanning = new Set(rows.map(row => row.key.slice(0, 4))).size > 1;
  const seen = new Set();
  return rows.map(row => {
    const day = row.at.toLocaleDateString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", ...(spanning ? { year: "numeric" } : {}) });
    const label = seen.has(row.key) ? "" : day;
    seen.add(row.key);
    return { label, day, value: row.value, raw: row.raw };
  });
}

export function netWorthHistorySummary(rows = []) {
  if (!rows.length) return "";
  const first = rows[0];
  const last = rows[rows.length - 1];
  const highest = rows.reduce((top, row) => { try { return BigInt(row.raw) > BigInt(top) ? row.raw : top; } catch { return top; } }, first.raw);
  return `Riwayat kekayaan bersih: ${rows.length} catatan, ${first.day} sampai ${last.day}, tertinggi ${money(highest)}, terakhir ${money(last.raw)}`;
}

export function dayLabel(value) {
  if (!value) return "";
  return new Date(`${value}T00:00:00+07:00`).toLocaleDateString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", year: "numeric" });
}

export function mapMonthlyCashflow(items = [], partialPeriod = "") {
  const rows = items.map(item => ({ ...item, partial: Boolean(partialPeriod) && item.period === partialPeriod, label: monthLabel(item.period), incomeValue: Number(item.income || 0), expenseValue: Number(item.expense || 0), netValue: Number(item.netCashflow || 0) }));
  const first = rows.findIndex(row => row.incomeValue !== 0 || row.expenseValue !== 0 || Number(row.refund || 0) !== 0);
  return first < 0 ? [] : rows.slice(first);
}
