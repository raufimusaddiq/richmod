import { baselineFor, deltaView, isZero } from "./cycleKpi.js";
import { CATEGORY_COLORS, OTHER_CATEGORY_COLOR, buildCategoryPalette, categoryColorFor, categoryColorMap, categoryKey } from "./categoryColors.js";

export { CATEGORY_COLORS, OTHER_CATEGORY_COLOR, buildCategoryPalette, categoryColorFor, categoryColorMap, categoryKey };

function fraction(value, peak) {
  const number = Math.abs(Number(value));
  return peak > 0 && Number.isFinite(number) ? Math.min(1, number / peak) : 0;
}

export function rankingView(facts, palette = null) {
  const ranking = facts.categoryRanking;
  if (!ranking || !Array.isArray(ranking.rows)) return null;
  const basis = baselineFor(facts.period, facts.spendingShape?.days);
  const evidenceKeys = new Set((facts.categoryChanges || []).map(categoryKey));
  const peak = Math.max(0, ...[...ranking.rows, ...(ranking.other ? [ranking.other] : [])].map(row => Math.abs(Number(row.amount))).filter(Number.isFinite));
  const { colors } = categoryColorMap(facts, palette);
  const deltaFor = row => deltaView(row.deltaVsBaseline ?? null, { relative: row.relativeDeltaVsBaseline ?? null, lowerIsBetter: true });
  const view = row => ({ ...row, delta: deltaFor(row), length: fraction(row.amount, peak) });
  const rows = ranking.rows.map(row => {
    const key = categoryKey(row);
    const color = colors.get(key) || OTHER_CATEGORY_COLOR;
    return { ...view(row), key, color, openable: evidenceKeys.has(key) };
  });
  const otherRows = Array.isArray(ranking.other?.rows)
    ? ranking.other.rows.map(row => {
      const key = categoryKey(row);
      return { ...view(row), key, color: "", openable: evidenceKeys.has(key) };
    })
    : [];
  const other = ranking.other && (Number(ranking.other.categoryCount) > 0 || otherRows.length > 0)
    ? { ...view(ranking.other), key: "other", color: OTHER_CATEGORY_COLOR, rows: otherRows }
    : null;
  const hasBaseline = Boolean(facts.comparison?.previousFullCycle || facts.comparison?.previous);
  return { rows, other, baseline: basis.label, hasBaseline, total: facts.cashflow.expense };
}

export function nonZeroCategoryChanges(facts) {
  const basis = baselineFor(facts.period, facts.spendingShape?.days);
  return (facts.categoryChanges || []).filter(item => {
    const delta = item?.[basis.delta];
    return delta != null && !isZero(delta);
  });
}

export function changesView(facts, { limit = 8, palette = null } = {}) {
  const { comparison, period } = facts;
  const basis = baselineFor(period, facts.spendingShape?.days);
  const hasBaseline = Boolean(comparison?.previousFullCycle || comparison?.previous);
  const items = nonZeroCategoryChanges(facts).slice(0, limit);
  const peak = Math.max(0, ...items.map(item => Math.abs(Number(item.amount))).filter(Number.isFinite));
  const { colors } = categoryColorMap(facts, palette);
  const rows = items.map(item => {
    const key = categoryKey(item);
    const color = colors.get(key) || OTHER_CATEGORY_COLOR;
    return {
      key,
      id: item.id,
      name: item.name,
      amount: item.amount,
      shareOfExpense: item.shareOfExpense ?? null,
      length: fraction(item.amount, peak),
      color,
      openable: true,
      delta: deltaView(item[basis.delta] ?? null, { relative: item[basis.relative] ?? null, lowerIsBetter: true }),
    };
  });
  return { rows, hasBaseline, baseline: basis.label };
}

export function partialDay(period, items = []) {
  return period?.state === "ACTIVE" && items.length ? items[items.length - 1].period : "";
}
