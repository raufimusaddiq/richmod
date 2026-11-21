import { baselineFor, deltaView } from "./cycleKpi.js";
import { changeWidth } from "./cycleReview.js";

export const categoryKey = item => item.id || "uncategorized";
export const CATEGORY_COLORS = [
  "var(--chart-category-1)",
  "var(--chart-category-2)",
  "var(--chart-category-3)",
  "var(--chart-category-4)",
  "var(--chart-category-5)",
  "var(--chart-category-6)",
];
export const OTHER_CATEGORY_COLOR = "var(--chart-category-other)";

export function categoryColorMap(facts) {
  const colors = new Map();
  const order = [];
  const register = key => {
    if (!key || colors.has(key)) return;
    colors.set(key, CATEGORY_COLORS[order.length % CATEGORY_COLORS.length]);
    order.push(key);
  };
  for (const row of facts?.categoryRanking?.rows || []) register(categoryKey(row));
  for (const row of facts?.categoryHistory?.rows || []) register(categoryKey(row));
  return { colors, order };
}

function fraction(value, peak) {
  const number = Math.abs(Number(value));
  return peak > 0 && Number.isFinite(number) ? Math.min(1, number / peak) : 0;
}

export function rankingView(facts) {
  const ranking = facts.categoryRanking;
  if (!ranking || !Array.isArray(ranking.rows)) return null;
  const basis = baselineFor(facts.period, facts.spendingShape?.days);
  const openable = new Set((facts.categoryChanges || []).map(categoryKey));
  const peak = Math.max(0, ...[...ranking.rows, ...(ranking.other ? [ranking.other] : [])].map(row => Math.abs(Number(row.amount))).filter(Number.isFinite));
  const { colors } = categoryColorMap(facts);
  const view = row => ({ ...row, delta: deltaView(row.deltaVsBaseline ?? null, { relative: row.relativeDeltaVsBaseline ?? null, lowerIsBetter: true }), length: fraction(row.amount, peak) });
  const rows = ranking.rows.map(row => {
    const key = categoryKey(row);
    return { ...view(row), key, color: colors.get(key) || CATEGORY_COLORS[0], openable: openable.has(key) };
  });
  const other = ranking.other && Number(ranking.other.categoryCount) > 0 ? { ...view(ranking.other), key: "other", color: OTHER_CATEGORY_COLOR } : null;
  return { rows, other, baseline: basis.label, total: facts.cashflow.expense };
}

export function changesView(facts, { limit = 5, movers = 3 } = {}) {
  const { comparison, period } = facts;
  const basis = baselineFor(period, facts.spendingShape?.days);
  const hasBaseline = Boolean(comparison?.previousFullCycle || comparison?.previous);
  const items = (facts.categoryChanges || []).slice(0, limit);
  const rows = items.map(item => {
    const delta = item[basis.delta] ?? null;
    return { key: categoryKey(item), name: item.name, amount: item.amount, delta: deltaView(delta, { relative: item[basis.relative] ?? null, lowerIsBetter: true }), side: delta == null || /^[-+]?0+(\.0+)?$/.test(String(delta)) ? "" : String(delta).startsWith("-") ? "less" : "more", width: changeWidth(item, items, basis.delta) };
  });
  const merchants = (facts.merchantDrivers || []).slice(0, movers).map(item => ({ key: `${item.id || "none"}:${item.name}`, id: item.id || "", name: item.name, delta: deltaView(item[basis.delta] ?? null, { relative: item[basis.relative] ?? null, lowerIsBetter: true }) }));
  return { rows, merchants, hasBaseline, baseline: basis.label };
}

export function partialDay(period, items = []) {
  return period?.state === "ACTIVE" && items.length ? items[items.length - 1].period : "";
}
