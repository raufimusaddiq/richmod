import { inclusiveEnd } from "./cycleReview.js";

// Cycle ledger helpers. Every amount, delta, median and total is served by the API
// (cycle-review-v1); this module only formats, maps amounts to visual lengths, and
// picks served fields. It never derives a total, delta, median or share.

const millions = new Intl.NumberFormat("id-ID", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
const shortDay = new Intl.DateTimeFormat("id-ID", { day: "numeric", month: "short", timeZone: "UTC" });

function dayMonth(iso) {
  if (!iso) return "";
  const date = new Date(`${iso}T00:00:00Z`);
  return Number.isNaN(date.getTime()) ? "" : shortDay.format(date);
}

// Display only: "12,5" for Rp12.500.000. Never feed this back into arithmetic.
export function compactMillions(value) {
  if (value == null) return "—";
  const number = Number(value) / 1e6;
  if (!Number.isFinite(number)) return "—";
  return `${number < 0 ? "−" : ""}${millions.format(Math.abs(number))}`;
}

export function signedMillions(value) {
  const text = compactMillions(value);
  if (text === "—" || text.startsWith("−") || text === "0,0") return text;
  return `+${text}`;
}

// Direction is a neutral marker beside the signed value, never a colour alone.
export function directionMark(value) {
  if (value == null) return "";
  const text = String(value);
  if (text.startsWith("-")) return "▼";
  return /^0+(\.0+)?$/.test(text) ? "" : "▲";
}

// History columns: the cycle's range and served net cashflow, for the cycle-columns
// card. No bar length and no derived total (the matrix owns the category series).
export function buildLedger(history = [], selectedStart = "") {
  return {
    columns: history.map(item => ({
      start: item.start,
      label: dayMonth(item.start),
      until: item.state === "ACTIVE" ? "berjalan" : `– ${dayMonth(inclusiveEnd(item.end))}`,
      running: item.state === "ACTIVE",
      selected: item.start === selectedStart,
      net: item.netCashflow,
    })),
  };
}

// `cycles` is newest first, as served. Older is the next entry, newer the one before.
export function adjacentCycles(cycles = [], start = "") {
  const index = cycles.findIndex(item => item.start === start);
  if (index < 0) return { older: "", newer: "" };
  return { older: cycles[index + 1]?.start || "", newer: index > 0 ? cycles[index - 1].start : "" };
}

// Pace references are the served comparison totals. Equal-day values are totals over
// the same elapsed days, so they are markers at the latest day; full-cycle values are levels.
export function paceReferences({ comparison, period }) {
  const expense = comparison?.expense;
  if (!expense || !period) return [];
  if (period.state === "ACTIVE") {
    return [
      expense.previous != null && { key: "previous-same-day", label: "Siklus sebelumnya, hari yang sama", value: expense.previous, shape: "marker", tone: "previous" },
      expense.median3 != null && { key: "median-same-day", label: "Median 3 siklus, hari yang sama", value: expense.median3, shape: "marker", tone: "median" },
      expense.previousFullCycle != null && { key: "previous-full", label: "Siklus sebelumnya, penuh", value: expense.previousFullCycle, shape: "line", tone: "previous" },
    ].filter(Boolean);
  }
  const previous = expense.previousFullCycle ?? expense.previous ?? null;
  return [
    previous != null && { key: "previous-full", label: "Siklus sebelumnya", value: previous, shape: "line", tone: "previous" },
    expense.median3 != null && { key: "median", label: "Median 3 siklus", value: expense.median3, shape: "line", tone: "median" },
  ].filter(Boolean);
}

export function hasPace(items = []) {
  return items.length > 0 && items.every(item => item.cumulativeExpense != null && Number.isFinite(Number(item.cumulativeExpense)));
}

// Served day-by-day curves (index i is day i+1). A curve is used only when every
// point is a number, so a partial series is never drawn.
export function paceCurves(pace) {
  const usable = series => Array.isArray(series) && series.length > 0 && series.every(value => value != null && Number.isFinite(Number(value)));
  return { previous: usable(pace?.previousFullCycle) ? pace.previousFullCycle : null, median: usable(pace?.median3) ? pace.median3 : null };
}

// `runningTotal` is the served cumulative net expense, converted for plotting only.
// The previous-cycle and median curves share the day axis; each ends where it ends.
export function mapPace(items = [], pace) {
  const { previous, median } = paceCurves(pace);
  const length = Math.max(items.length, previous?.length ?? 0, median?.length ?? 0);
  const point = (series, index) => (series?.[index] == null ? null : Number(series[index]));
  return Array.from({ length }, (_, index) => ({
    day: index + 1,
    period: items[index]?.period,
    runningTotal: items[index] ? Number(items[index].cumulativeExpense) : null,
    exact: items[index]?.cumulativeExpense,
    previousTotal: point(previous, index), previousExact: previous?.[index],
    medianTotal: point(median, index), medianExact: median?.[index],
  }));
}

// Calendar date of cycle day `day` (1-based), continuing past the measured days for the
// previous-cycle tail. Date labelling only.
export function paceDate(items = [], day) {
  const first = items[0]?.period;
  if (!first || !(day >= 1)) return "";
  const time = Date.parse(`${first}T00:00:00Z`);
  return Number.isNaN(time) ? "" : new Date(time + (day - 1) * 86400000).toISOString().slice(0, 10);
}

export function paceDateLabel(items, day) {
  return dayMonth(paceDate(items, day));
}

// Weekly x ticks (day 1, 8, 15 …) up to the last plotted day.
export function paceTicks(length, step = 7) {
  const ticks = [];
  for (let day = 1; day <= length; day += step) ticks.push(day);
  return ticks;
}
