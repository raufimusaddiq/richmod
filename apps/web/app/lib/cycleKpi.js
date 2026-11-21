import { money } from "./format.js";

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

function spendingDelta(row, basis) {
  const view = deltaView(row?.[basis.delta] ?? null, { relative: row?.[basis.relative] ?? null, lowerIsBetter: true });
  if (view.empty) return { ...view, note: "Belum ada pembanding" };
  if (view.zero) return { ...view, note: basis.same };
  return { ...view, word: undefined, note: `${view.mark === "▼" ? "lebih rendah" : "lebih tinggi"} ${basis.label}` };
}

export function averageDailyText(shape) {
  return shape?.averageDailyExpenseRounded ?? null;
}

export function kpiCells(facts) {
  const { period, cashflow, comparison, spendingShape } = facts;
  const active = period.state === "ACTIVE";
  const days = spendingShape?.days;
  const basis = baselineFor(period, days);
  return [
    { key: "expense", lead: true, label: `Pengeluaran bersih${active && days ? ` · hari ke-${days}` : ""}`, value: money(cashflow.expense), delta: spendingDelta(comparison?.expense, basis) },
    { key: "income", label: "Pemasukan", value: money(cashflow.income) },
    { key: "net", label: active ? "Arus kas bersih sejauh ini" : "Arus kas bersih", value: signedValue(cashflow.netCashflow), footnote: active ? "belum final" : "" },
    { key: "saved", label: "Ditabung", value: money(cashflow.savingsAllocated), sub: `Belum dialokasikan ${money(cashflow.unallocatedSurplus)}` },
  ];
}

function signedValue(value) {
  if (value == null) return "—";
  if (isZero(value)) return money("0");
  return `${isNegative(value) ? "−" : "+"}${money(magnitude(value))}`;
}
