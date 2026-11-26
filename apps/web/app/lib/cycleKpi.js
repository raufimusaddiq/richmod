import { money } from "./format.js";
import { inclusiveEnd } from "./cycleDates.js";

const isNegative = value => String(value).trim().startsWith("-");
export const isZero = value => /^[-+]?0+(\.0+)?$/.test(String(value).trim());
const magnitude = value => String(value).trim().replace(/^[-+]/, "");
const shortDay = new Intl.DateTimeFormat("id-ID", { day: "numeric", month: "short", timeZone: "UTC" });

function dayMonth(iso) {
  if (!iso) return "";
  const date = new Date(`${iso}T00:00:00Z`);
  return Number.isNaN(date.getTime()) ? "" : shortDay.format(date);
}

export function deltaTone(value, lowerIsBetter) {
  if (value == null || isZero(value)) return "none";
  return isNegative(value) === Boolean(lowerIsBetter) ? "good" : "bad";
}

export function percentText(value) {
  if (value == null || !Number.isFinite(Number(value))) return "";
  const text = new Intl.NumberFormat("id-ID", { style: "percent", maximumFractionDigits: 1, signDisplay: "never" }).format(Number(value));
  return `${Number(value) < 0 ? "−" : ""}${text}`;
}

export function unsignedPercentText(value) {
  if (value == null || !Number.isFinite(Number(value))) return "";
  return new Intl.NumberFormat("id-ID", { style: "percent", maximumFractionDigits: 1, signDisplay: "never" }).format(Math.abs(Number(value)));
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

export function averageDailyText(shape) {
  return shape?.averageDailyExpenseRounded ?? null;
}

export function shouldShowLastCycleCard(facts) {
  const days = facts?.spendingShape?.days;
  return Boolean(
    facts?.period?.kind === "SALARY_CYCLE"
    && facts.period.state === "ACTIVE"
    && Number.isInteger(days)
    && days >= 1
    && days <= 7
    && facts.previousCycle,
  );
}

export function isComparisonTooEarly(facts) {
  const days = facts?.spendingShape?.days;
  return Boolean(facts?.period?.state === "ACTIVE" && Number.isInteger(days) && days <= 3);
}

function deltaChip(key, value, relative, suffix) {
  const view = deltaView(value ?? null, { relative: relative ?? null, lowerIsBetter: true });
  if (view.empty) return null;
  if (view.zero) return { key, tone: "none", mark: "", word: "", text: `Sama ${suffix}` };
  const figure = unsignedPercentText(relative) || view.text;
  return { key, tone: view.tone, mark: view.mark, word: view.word, text: `${figure} ${suffix}` };
}

function leadChips(facts, active) {
  if (isComparisonTooEarly(facts)) return [];
  const expense = facts.comparison?.expense;
  const chips = [];
  if (active) {
    const prev = deltaChip("previous", expense?.deltaVsPrevious, expense?.relativeDeltaVsPrevious, "vs siklus lalu");
    if (prev) chips.push(prev);
    if (facts.comparison?.median3Available) {
      const med = deltaChip("median", expense?.deltaVsMedian3, expense?.relativeDeltaVsMedian3, "vs median 3 siklus");
      if (med) chips.push(med);
    }
  } else {
    const full = deltaChip("previous-full", expense?.deltaVsPreviousFullCycle, expense?.relativeDeltaVsPreviousFullCycle, "vs siklus lalu (penuh)");
    if (full) chips.push(full);
  }
  return chips;
}

function closedDaysFootnote(facts) {
  const days = facts.spendingShape?.days;
  if (!days) return "";
  const prevStart = facts.comparison?.previousFullCycle?.start;
  const prevDays = prevStart ? facts.history?.find(item => item.start === prevStart)?.days ?? null : null;
  if (prevDays) return `${days} hari · siklus lalu ${prevDays} hari · per hari ada di Siklus ke siklus`;
  return `${days} hari`;
}

export function kpiCells(facts) {
  const { period, cashflow, spendingShape } = facts;
  const active = period.state === "ACTIVE";
  const days = spendingShape?.days;
  const closedRange = !active && period.start && period.end ? `${dayMonth(period.start)} – ${dayMonth(inclusiveEnd(period.end))}` : "";
  const chips = leadChips(facts, active);
  const hasComparison = Boolean(facts.comparison?.previousFullCycle || facts.comparison?.previous);
  return [
    {
      key: "expense",
      lead: true,
      label: active ? `Pengeluaran bersih${days ? ` · hari ke-${days}` : ""}` : `Pengeluaran bersih${closedRange ? ` · ${closedRange}` : ""}`,
      value: money(cashflow.expense),
      verdict: facts.takeaways?.verdict || "",
      chips,
      emptyComparison: !chips.length && !hasComparison ? "Belum ada pembanding" : "",
      footnote: !active ? closedDaysFootnote(facts) : "",
    },
    { key: "income", label: "Pemasukan", value: money(cashflow.income) },
    { key: "net", label: active ? "Arus kas bersih sejauh ini" : "Arus kas bersih", value: signedValue(cashflow.netCashflow), footnote: active ? "belum final" : "" },
    { key: "saved", label: "Ditabung", value: money(cashflow.savingsAllocated), sub: `Belum dialokasikan ${money(cashflow.unallocatedSurplus)}` },
  ];
}

export function signedValue(value) {
  if (value == null) return "—";
  if (isZero(value)) return money("0");
  return `${isNegative(value) ? "−" : "+"}${money(magnitude(value))}`;
}
