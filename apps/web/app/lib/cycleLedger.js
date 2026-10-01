import { money } from "./format.js";
import { inclusiveEnd, ratioLabel, signedMoney } from "./cycleReview.js";

// Cycle ledger helpers. Every amount, delta, median and total is served by the
// API (cycle-review-v1). This module only formats numbers for display, maps
// amounts to visual lengths, and picks which served fields to show. It never
// derives a total, delta, median or share.

const millions = new Intl.NumberFormat("id-ID", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
const shortDay = new Intl.DateTimeFormat("id-ID", { day: "numeric", month: "short", timeZone: "UTC" });

export function dayMonth(iso) {
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

export function directionText(value) {
  return value == null ? "—" : `${directionMark(value)} ${signedMoney(value)}`.trim();
}

const CLOSED_FOR_CHART = 4;

// Columns for the ledger. Lengths are fractions of the largest income/expense
// shown, so equal amounts have equal bars. The median tick uses the same scale.
export function buildLedger(history = [], selectedStart = "", median = null) {
  const amounts = history.flatMap(item => [Number(item.income), Number(item.expense)]).filter(Number.isFinite);
  const peak = Math.max(0, ...amounts);
  const length = value => (peak > 0 && Number.isFinite(Number(value)) ? Math.min(1, Math.max(0, Number(value)) / peak) : 0);
  const columns = history.map(item => ({
    start: item.start,
    label: dayMonth(item.start),
    until: item.state === "ACTIVE" ? "berjalan" : `– ${dayMonth(inclusiveEnd(item.end))}`,
    running: item.state === "ACTIVE",
    selected: item.start === selectedStart,
    income: item.income,
    expense: item.expense,
    net: item.netCashflow,
    delta: item.expenseDelta ?? null,
    incomeLength: length(item.income),
    expenseLength: length(item.expense),
  }));
  const closed = history.filter(item => item.state === "CLOSED").length;
  return { columns, mode: closed >= CLOSED_FOR_CHART ? "chart" : "cards", medianLength: median == null ? null : length(median) };
}

// `cycles` is newest first, as served. Older is the next entry, newer the one before.
export function adjacentCycles(cycles = [], start = "") {
  const index = cycles.findIndex(item => item.start === start);
  if (index < 0) return { older: "", newer: "" };
  return { older: cycles[index + 1]?.start || "", newer: index > 0 ? cycles[index - 1].start : "" };
}

// Label : value pairs from served fields only; no sentence is composed.
export function verdictPairs({ comparison, period, spendingShape }) {
  const expense = comparison?.expense;
  if (!expense || !period) return [];
  if (period.state === "ACTIVE") {
    const days = spendingShape?.days;
    const pairs = [[days ? `Pengeluaran bersih · hari ke-${days}` : "Pengeluaran bersih", money(expense.amount)]];
    pairs.push(["Siklus sebelumnya di hari yang sama", expense.previous == null ? "Belum ada pembanding" : `${money(expense.previous)} · ${directionText(expense.deltaVsPrevious)}`]);
    if (expense.median3 != null) pairs.push(["Median 3 siklus di hari yang sama", `${money(expense.median3)} · ${directionText(expense.deltaVsMedian3)}`]);
    return pairs;
  }
  const pairs = [["Pengeluaran bersih", money(expense.amount)]];
  pairs.push(["Dibanding siklus sebelumnya", expense.deltaVsPreviousFullCycle == null ? "Belum ada pembanding" : `${directionText(expense.deltaVsPreviousFullCycle)} · ${ratioLabel(expense.relativeDeltaVsPreviousFullCycle)}`]);
  if (expense.deltaVsMedian3 != null) pairs.push(["Dibanding median 3 siklus", directionText(expense.deltaVsMedian3)]);
  return pairs;
}

// Pace references are the served comparison totals. Equal-day values are totals
// over the same elapsed days, so they are markers at the latest day; full-cycle
// values are levels.
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

// `runningTotal` is the served cumulative net expense, converted for plotting only.
export function mapPace(items = []) {
  return items.map((item, index) => ({ day: index + 1, period: item.period, runningTotal: Number(item.cumulativeExpense), exact: item.cumulativeExpense }));
}

export function monthMarkers(items = []) {
  return items.flatMap((item, index) => (index > 0 && item.period?.slice(8) === "01" ? [{ day: index + 1, label: dayMonth(item.period) }] : []));
}
