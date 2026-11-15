import { baselineFor, deltaView } from "./cycleKpi.js";
import { changeWidth } from "./cycleReview.js";

// View models for the "where / what changed" row. Order, amounts, shares and deltas
// are served (cycle-review-v1: categoryRanking, categoryChanges, merchantDrivers);
// nothing here sorts, adds, subtracts or divides money. Bar lengths are a visual
// mapping of served magnitudes, never a displayed figure.

export const categoryKey = item => item.id || "uncategorized";

// Fraction of the widest served magnitude, 0..1, for bar lengths only.
function fraction(value, peak) {
  const number = Math.abs(Number(value));
  return peak > 0 && Number.isFinite(number) ? Math.min(1, number / peak) : 0;
}

// "Ke mana uangnya": rows exactly as served (amount-ranked by the API), plus the served
// remainder bucket. A row opens the drawer only when its category's evidence is served.
export function rankingView(facts) {
  const ranking = facts.categoryRanking;
  if (!ranking || !Array.isArray(ranking.rows)) return null;
  const basis = baselineFor(facts.period, facts.spendingShape?.days);
  const openable = new Set((facts.categoryChanges || []).map(categoryKey));
  const peak = Math.max(0, ...[...ranking.rows, ...(ranking.other ? [ranking.other] : [])].map(row => Math.abs(Number(row.amount))).filter(Number.isFinite));
  const view = row => ({ ...row, delta: deltaView(row.deltaVsBaseline ?? null, { relative: row.relativeDeltaVsBaseline ?? null, lowerIsBetter: true }), length: fraction(row.amount, peak) });
  const rows = ranking.rows.map(row => ({ ...view(row), key: categoryKey(row), openable: openable.has(categoryKey(row)) }));
  const other = ranking.other && Number(ranking.other.categoryCount) > 0 ? { ...view(ranking.other), key: "other" } : null;
  return { rows, other, baseline: basis.label, total: facts.cashflow.expense };
}

// "Yang berubah": the first five served category changes, in server order, against the
// baseline that matches the cycle state (same elapsed day while running, full cycle once closed).
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

// Daily bars: the running day of an active cycle is only partly measured.
export function partialDay(period, items = []) {
  return period?.state === "ACTIVE" && items.length ? items[items.length - 1].period : "";
}
