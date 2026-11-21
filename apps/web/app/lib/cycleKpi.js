import { displayDate, money } from "./format.js";

const isNegative = value => String(value).trim().startsWith("-");
const isZero = value => /^[-+]?0+(\.0+)?$/.test(String(value).trim());
const magnitude = value => String(value).trim().replace(/^[-+]/, "");

export function deltaTone(value, lowerIsBetter) {
  if (value == null || isZero(value)) return "none";
  return isNegative(value) === Boolean(lowerIsBetter) ? "good" : "bad";
}

export function percentText(value) {
  if (value == null || !Number.isFinite(Number(value))) return "";
  const text = new Intl.NumberFormat("id-ID", { style: "percent", maximumFractionDigits: 1, signDisplay: "never" }).format(Number(value));
  return `${Number(value) < 0 ? "−" : ""}${text}`;
}

export function deltaView(value, { relative = null, lowerIsBetter = false } = {}) {
  if (value == null) return { tone: "none", empty: true, mark: "", text: "—", percent: "" };
  if (isZero(value)) return { tone: "none", zero: true, mark: "", text: money("0"), percent: "" };
  const percent = percentText(relative);
  return { tone: deltaTone(value, lowerIsBetter), mark: isNegative(value) ? "▼" : "▲", word: isNegative(value) ? "turun" : "naik", text: money(magnitude(value)), percent: percent ? `(${percent})` : "" };
}

export function baselineFor(period, days) {
  if (period?.state === "ACTIVE") {
    return { field: "previous", delta: "deltaVsPrevious", relative: "relativeDeltaVsPrevious", label: days ? `vs siklus lalu di hari ke-${days}` : "vs siklus lalu di hari yang sama", same: "sama dengan siklus lalu di hari yang sama" };
  }
  return { field: "previousFullCycle", delta: "deltaVsPreviousFullCycle", relative: "relativeDeltaVsPreviousFullCycle", label: "vs siklus lalu (penuh)", same: "sama dengan siklus lalu (penuh)" };
}

function comparisonCell(row, basis, lowerIsBetter) {
  const value = row?.[basis.delta] ?? null;
  const view = deltaView(value, { relative: row?.[basis.relative] ?? null, lowerIsBetter });
  const previous = row?.[basis.field];
  let note;
  if (view.empty) note = "Belum ada pembanding";
  else if (view.zero) note = basis.same;
  else note = previous == null ? basis.label : `${basis.label} (${money(previous)})`;
  return { ...view, note };
}

export function averageDailyText(shape) {
  return shape?.averageDailyExpenseRounded ?? null;
}

export function kpiCells(facts) {
  const { period, cashflow, comparison, wealth, spendingShape } = facts;
  const active = period.state === "ACTIVE";
  const days = spendingShape?.days;
  const basis = baselineFor(period, days);
  const expense = comparisonCell(comparison?.expense, basis, true);
  const median = comparison?.expense?.deltaVsMedian3 != null ? { ...deltaView(comparison.expense.deltaVsMedian3, { relative: comparison.expense.relativeDeltaVsMedian3, lowerIsBetter: true }), note: active ? "vs median 3 siklus · hari yang sama" : "vs median 3 siklus" } : null;
  const snapshot = wealth?.current ?? wealth?.previous ?? null;
  const worthChange = snapshot && wealth.netWorthChange != null
    ? { ...deltaView(wealth.netWorthChange), note: wealth.previous ? `vs pengamatan ${displayDate(wealth.previous.observedAt)}` : "vs pengamatan sebelumnya" }
    : null;
  return [
    { key: "expense", lead: true, label: `Pengeluaran bersih${active && days ? ` · hari ke-${days}` : ""}`, value: money(cashflow.expense), delta: expense, extra: median },
    { key: "income", label: "Pemasukan", value: money(cashflow.income), delta: comparisonCell(comparison?.income, basis, false) },
    { key: "net", label: active ? "Arus kas bersih sejauh ini" : "Arus kas bersih", value: signedValue(cashflow.netCashflow), delta: comparisonCell(comparison?.netCashflow, basis, false), footnote: active ? "belum final" : "" },
    { key: "unallocated", label: "Belum dialokasikan", value: money(cashflow.unallocatedSurplus), sub: `Ditabung ${money(cashflow.savingsAllocated)}` },
    { key: "worth", label: "Kekayaan bersih", value: snapshot ? money(snapshot.netWorth) : "—", delta: worthChange || { ...deltaView(null), note: "" }, observed: snapshot ? (wealth.netWorthChange == null || !wealth.current ? "Pengamatan terakhir " : "Pengamatan ") + displayDate(snapshot.observedAt) : "Belum ada pengamatan" },
  ];
}

function signedValue(value) {
  if (value == null) return "—";
  if (isZero(value)) return money("0");
  return `${isNegative(value) ? "−" : "+"}${money(magnitude(value))}`;
}
