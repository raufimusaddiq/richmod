export function toRupiahInt(value) {
  try { return BigInt(String(value ?? "0").trim().split(".")[0] || "0"); } catch { return 0n; }
}

export function homeState(overview, transactions = []) {
  if (!overview) return "normal";
  const confirmed = Number(overview.confirmedCount ?? 0);
  if (confirmed === 0 && transactions.length === 0) return "empty";
  if (toRupiahInt(overview.income) === 0n && toRupiahInt(overview.expense) !== 0n) return "expenses-only";
  return "normal";
}

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

function percentile(values, ratio) {
  if (!values.length) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.max(0, Math.floor((sorted.length - 1) * ratio)));
  return sorted[index];
}

const compactOneDecimal = new Intl.NumberFormat("id-ID", { maximumFractionDigits: 1 });
const compactInteger = new Intl.NumberFormat("id-ID", { maximumFractionDigits: 0 });

export function dailyAxis(ceiling) {
  const target = Number(ceiling);
  if (!Number.isFinite(target) || target <= 0) return { domain: [0, 1], ticks: [0, 1] };
  const exp = Math.floor(Math.log10(target));
  let best = null;
  for (const power of [exp - 1, exp]) {
    const base = 10 ** power;
    for (const multiplier of [1, 2, 2.5, 5]) {
      const step = multiplier * base;
      if (step <= 0) continue;
      const count = Math.ceil(target / step);
      if (count < 2 || count > 4) continue;
      const top = count * step;
      const better = !best || top < best.top - 1e-9 || (Math.abs(top - best.top) < 1e-9 && count < best.count);
      if (better) best = { step, count, top };
    }
  }
  if (!best) {
    const step = 10 ** exp;
    const count = Math.max(2, Math.ceil(target / step));
    best = { step, count, top: count * step };
  }
  const ticks = Array.from({ length: best.count + 1 }, (_, index) => Math.round(index * best.step * 1e6) / 1e6);
  return { domain: [0, ticks[ticks.length - 1]], ticks };
}

export function formatDailyTick(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "—";
  const sign = number < 0 ? "−" : "";
  const abs = Math.abs(number);
  if (abs === 0) return "0";
  if (abs >= 1_000_000) return `${sign}${compactOneDecimal.format(abs / 1_000_000)} jt`;
  if (abs >= 1_000) {
    const thousands = abs / 1_000;
    if (Math.round(thousands) >= 1_000) return `${sign}1 jt`;
    const text = abs >= 10_000 ? compactInteger.format(Math.round(thousands)) : compactOneDecimal.format(thousands);
    return `${sign}${text}rb`;
  }
  return `${sign}${compactInteger.format(Math.round(abs))}`;
}

export function clippedBarLabels(rows = [], { narrow = false, minGap = 4 } = {}) {
  const capped = rows
    .map((row, index) => ({ index, value: Number(row.expenseValue ?? row.expense ?? 0), text: formatDailyTick(row.expenseValue ?? row.expense ?? 0) }))
    .filter((_, index) => rows[index]?.capped);
  if (!capped.length) return new Map();
  capped.sort((a, b) => (b.value - a.value) || (a.index - b.index));
  if (narrow) {
    const top = capped[0];
    return new Map([[top.index, { text: top.text, tier: 0 }]]);
  }
  const tiers = [[], []];
  const placed = new Map();
  for (const item of capped) {
    const tier = tiers.findIndex(list => list.every(other => Math.abs(other - item.index) >= minGap));
    if (tier < 0) continue;
    tiers[tier].push(item.index);
    placed.set(item.index, { text: item.text, tier });
  }
  return placed;
}

export function dailyChartModel(items = []) {
  const values = items.map(item => Math.max(Number(item.expenseValue ?? item.expense ?? 0), 0));
  const max = values.reduce((top, value) => Math.max(top, value), 0), mid = median(values), p90 = percentile(values, 0.9);
  const candidateCap = p90 > 0 ? dailyAxis(p90).domain[1] : null;
  const cap = candidateCap != null && max > candidateCap * 2 ? candidateCap : null;
  const axis = dailyAxis(cap ?? max);
  const rows = items.map((item, index) => ({ ...item, plotValue: cap == null ? values[index] : Math.min(values[index], cap), capped: cap != null && values[index] > cap }));
  let total = 0n, biggest = null, spendDays = 0;
  for (const item of items) {
    const amount = toRupiahInt(item.expense);
    total += amount < 0n ? 0n : amount;
    if (amount > 0n) spendDays += 1;
    if (amount > 0n && (!biggest || amount > toRupiahInt(biggest.expense))) biggest = item;
  }
  const average = items.length ? (total / BigInt(items.length)).toString() : "0";
  return { rows, cap, axis, max, p90, median: mid, average, biggest, spendDays };
}

export function dayLedgerHref(period) { return `/transactions?from=${period}&to=${period}`; }
export function categoryLedgerHref(id, window) {
  const params = new URLSearchParams();
  params.set("categoryId", id || "uncategorized");
  if (window?.start) params.set("from", window.start);
  if (window?.end) params.set("to", window.end);
  return `/transactions?${params}`;
}

export function rankedCategories(items = [], limit = 6, palette = null) {
  const positive = items.filter(item => toRupiahInt(item.amount) > 0n);
  const total = positive.reduce((sum, item) => sum + toRupiahInt(item.amount), 0n);
  const toShare = amount => (total > 0n ? Number((amount * 10000n) / total) / 10000 : 0);
  const hasPalette = Boolean(palette && palette.size > 0);
  const eligible = hasPalette ? positive.filter(item => item.id && palette.has(String(item.id))) : positive;
  const excluded = hasPalette ? positive.filter(item => !item.id || !palette.has(String(item.id))) : [];
  const top = eligible.slice(0, limit).map(item => ({ ...item, share: toShare(toRupiahInt(item.amount)) }));
  const rest = [...eligible.slice(limit), ...excluded];
  if (rest.length) {
    const amount = rest.reduce((sum, item) => sum + toRupiahInt(item.amount), 0n);
    top.push({ id: "other", name: "Kategori lain", amount: amount.toString(), share: toShare(amount), other: true });
  }
  return top;
}
