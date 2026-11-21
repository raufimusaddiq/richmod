// Beranda decisions that are pure arithmetic or state selection. Money arrives as decimal strings
// and is summed as integers (BigInt); only chart geometry uses plain numbers.

export function toRupiahInt(value) {
  try { return BigInt(String(value ?? "0").trim().split(".")[0] || "0"); } catch { return 0n; }
}

// "empty": no confirmed transaction and nothing on the list yet. "expenses-only": spending but no
// income recorded in the period, where a "remaining" balance would read as a misleading negative.
// Everything else is the normal hero.
export function homeState(overview, transactions = []) {
  if (!overview) return "normal";
  const confirmed = Number(overview.confirmedCount ?? 0);
  if (confirmed === 0 && transactions.length === 0) return "empty";
  if (toRupiahInt(overview.income) === 0n && toRupiahInt(overview.expense) !== 0n) return "expenses-only";
  return "normal";
}

// Expense against the previous salary cycle / month, from the server's deterministic delta.
export function comparisonLine(overview) {
  if (!overview || overview.expenseDelta == null || overview.previousExpense == null) return null;
  const delta = toRupiahInt(overview.expenseDelta);
  const unit = overview.periodKind === "CURRENT_CYCLE" ? "siklus lalu" : "bulan lalu";
  const days = overview.previousDays ? ` (${overview.previousDays} hari)` : "";
  if (delta === 0n) return { direction: "same", amount: "0", text: `Sama dengan ${unit}${days}` };
  const lower = delta < 0n;
  const amount = (lower ? -delta : delta).toString();
  return { direction: lower ? "down" : "up", amount, text: `${lower ? "lebih rendah" : "lebih tinggi"} dari ${unit}${days}` };
}

// How much of the period's income went out, 0..100, for the spending-share bar.
export function spendingShare(overview) {
  const income = Number(toRupiahInt(overview?.income)), expense = Number(toRupiahInt(overview?.expense));
  if (income <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round((expense / income) * 100)));
}

function median(values) {
  if (!values.length) return 0;
  const sorted = [...values].sort((a, b) => a - b), mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

// One outlier day must not flatten the rest of the month: bars are capped at 3x the median day, but
// only when the largest day exceeds that and the median is above zero. `rows` keep their real values.
export function dailyChartModel(items = []) {
  const values = items.map(item => Math.max(Number(item.expenseValue ?? item.expense ?? 0), 0));
  const max = values.reduce((top, value) => Math.max(top, value), 0), mid = median(values);
  const cap = mid > 0 && max > 3 * mid ? 3 * mid : null;
  const rows = items.map((item, index) => ({ ...item, plotValue: cap == null ? values[index] : Math.min(values[index], cap), capped: cap != null && values[index] > cap }));
  let total = 0n, biggest = null, spendDays = 0;
  for (const item of items) {
    const amount = toRupiahInt(item.expense);
    total += amount < 0n ? 0n : amount;
    if (amount > 0n) spendDays += 1;
    if (amount > 0n && (!biggest || amount > toRupiahInt(biggest.expense))) biggest = item;
  }
  const average = items.length ? (total / BigInt(items.length)).toString() : "0";
  return { rows, cap, max, median: mid, average, biggest, spendDays };
}

// Day bars and category rows drill into the ledger for exactly that window.
export function dayLedgerHref(period) { return `/transactions?from=${period}&to=${period}`; }
export function categoryLedgerHref(id, window) {
  const params = new URLSearchParams();
  params.set("categoryId", id || "uncategorized");
  if (window?.start) params.set("from", window.start);
  if (window?.end) params.set("to", window.end);
  return `/transactions?${params}`;
}

// Top rows plus one "Kategori lain" bucket, each with a share of the net spend.
export function rankedCategories(items = [], limit = 6) {
  const positive = items.filter(item => toRupiahInt(item.amount) > 0n);
  const total = positive.reduce((sum, item) => sum + toRupiahInt(item.amount), 0n);
  const toShare = amount => (total > 0n ? Number((amount * 10000n) / total) / 10000 : 0);
  const top = positive.slice(0, limit).map(item => ({ ...item, share: toShare(toRupiahInt(item.amount)) }));
  const rest = positive.slice(limit);
  if (rest.length) {
    const amount = rest.reduce((sum, item) => sum + toRupiahInt(item.amount), 0n);
    top.push({ id: "other", name: "Kategori lain", amount: amount.toString(), share: toShare(amount), other: true });
  }
  return top;
}
