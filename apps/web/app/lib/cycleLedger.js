import { inclusiveEnd, signedMoney } from "./cycleReview.js";

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

// Axis ticks in Rp juta. The decimals follow the tick step, so a small series
// (0,025 juta steps) never prints "0,0 0,0 0,1 0,1 0,1".
export function millionTickFormatter(maxValue) {
  const max = Number(maxValue);
  const step = Number.isFinite(max) && max > 0 ? max / 5 : 0;
  const decimals = step > 0 ? Math.max(1, Math.ceil(-Math.log10(step / 1e6))) : 1;
  const format = new Intl.NumberFormat("id-ID", { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
  return value => {
    const number = Number(value) / 1e6;
    if (!Number.isFinite(number)) return "—";
    return `${number < 0 ? "−" : ""}${format.format(Math.abs(number))}`;
  };
}

// Direction is a neutral marker beside the signed value, never a colour alone.
export function directionMark(value) {
  if (value == null) return "";
  const text = String(value);
  if (text.startsWith("-")) return "▼";
  return /^0+(\.0+)?$/.test(text) ? "" : "▲";
}

export function directionText(value) {
  return value == null ? "—" : `${directionMark(value)} ${signedMoney(value)}`.trim();
}

// History columns: the cycle's range, served net expense and net cashflow, plus a
// bar length that is a fraction of the largest served expense (visual mapping only).
export function buildLedger(history = [], selectedStart = "") {
  const peak = Math.max(0, ...history.map(item => Number(item.expense)).filter(Number.isFinite));
  const length = value => (peak > 0 && Number.isFinite(Number(value)) ? Math.min(1, Math.max(0, Number(value)) / peak) : 0);
  return {
    columns: history.map(item => ({
      start: item.start,
      label: dayMonth(item.start),
      until: item.state === "ACTIVE" ? "berjalan" : `– ${dayMonth(inclusiveEnd(item.end))}`,
      running: item.state === "ACTIVE",
      selected: item.start === selectedStart,
      expense: item.expense,
      expenseLength: length(item.expense),
      net: item.netCashflow,
    })),
  };
}

// The category x cycle matrix shares the ledger's columns. Rows and the "Lainnya"
// remainder are served and reconcile to each cycle's net expense; the browser only
// sizes the tint relative to the largest positive value in its row. A series that does
// not line up with history[] is not drawn.
export function buildMatrix(categoryHistory, history = [], selectedCategory = "", selectable = []) {
  const starts = history.map(item => item.start);
  const served = categoryHistory?.cycleStarts ?? [];
  const rows = categoryHistory?.rows ?? [];
  const other = categoryHistory?.other?.amounts ?? [];
  if (!rows.length || served.length !== starts.length || served.some((start, index) => start !== starts[index])) return [];
  if (other.length !== starts.length || rows.some(row => row.amounts?.length !== starts.length)) return [];
  const tints = amounts => {
    const peak = Math.max(0, ...amounts.map(Number).filter(Number.isFinite));
    return amounts.map(value => (peak > 0 && Number.isFinite(Number(value)) ? Math.max(0, Number(value)) / peak : 0));
  };
  const pickable = new Set(selectable);
  const matrix = rows.map(row => {
    const id = row.id || "uncategorized";
    return { key: id, id, name: row.name, amounts: row.amounts, tints: tints(row.amounts), other: false, selected: id === selectedCategory, selectable: pickable.has(id) };
  });
  matrix.push({ key: "other", id: "", name: "Kategori lain", amounts: other, tints: tints(other), other: true, selected: false, selectable: false });
  return matrix;
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

// Three or four x ticks whatever the cycle length: a phone cannot fit a weekly label
// on a 30-day axis, and four dates read the same on a wide card.
export function paceTicks(length, step) {
  const stride = step || Math.max(1, Math.ceil(length / 4));
  const ticks = [];
  for (let day = 1; day <= length; day += stride) ticks.push(day);
  return ticks;
}

// A marker in the right half of the axis has no room to the right, so its label
// anchors to the left of the line; the left half keeps the label to the right.
export function paceMarkerPosition(day, length) {
  return day > Math.max(length, 2) / 2 ? "insideTopRight" : "insideTopLeft";
}

// The visual ceiling of the pace plot: the highest served point of any drawn series or
// reference. Tick positions only; the values themselves stay served.
export function paceCeiling({ data = [], levels = [], markers = [], last = null } = {}) {
  let top = Number(last) || 0;
  for (const row of data) {
    for (const key of ["previousTotal", "medianTotal"]) {
      const value = Number(row?.[key]);
      if (Number.isFinite(value) && value > top) top = value;
    }
  }
  for (const value of [...levels, ...markers]) {
    const number = Number(value);
    if (Number.isFinite(number) && number > top) top = number;
  }
  return top;
}

// A round upper bound and its five evenly spaced ticks, for an axis whose top is
// pushed by a reference level rather than by the series. Tick positions are visual
// mapping only; the plotted values stay served.
export function paceAxis(maxValue) {
  const number = Number(maxValue);
  if (!Number.isFinite(number) || number <= 0) return { domain: [0, 1], ticks: [0, 1] };
  const raw = number / 4;
  const magnitude = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 5, 10].map(multiplier => multiplier * magnitude).find(candidate => raw <= candidate) ?? 10 * magnitude;
  return { domain: [0, step * 4], ticks: [0, step, step * 2, step * 3, step * 4] };
}
