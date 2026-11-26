import assert from "node:assert/strict";
import test from "node:test";
import { addMonths, calendarCategoryView, calendarErrorMessage, calendarKpiCells, calendarPeriodLabel, calendarVerdict, currentMonthKey, firstActiveMonth, hasActivity, hasRefunds, monthChoices, monthName, monthRowLabel, monthSpan, nextMonthKey, rangeSummary, validateCustomRange } from "../app/lib/calendarReview.js";

const october = new Date("2026-10-02T05:00:00Z");

test("the running month follows Jakarta time, including across the UTC date line", () => {
  assert.equal(currentMonthKey(october), "2026-10");
  assert.equal(currentMonthKey(new Date("2026-09-30T18:00:00Z")), "2026-10", "01:00 on 1 Oct in Jakarta is still 30 Sep in UTC");
  assert.equal(currentMonthKey(new Date("2026-09-30T16:59:00Z")), "2026-09");
  assert.equal(nextMonthKey(new Date("2026-12-15T00:00:00Z")), "2027-01");
  assert.equal(addMonths("2026-01", -1), "2025-12");
  assert.equal(monthSpan("2024-12", "2026-11"), 24);
  assert.equal(monthSpan("2026-07", "2026-07"), 1);
});

test("a custom range is validated beside its fields with the API's own rules", () => {
  assert.equal(validateCustomRange("2026-07", "2026-09", october), "");
  assert.equal(validateCustomRange("2026-09", "2026-11", october), "", "next month is the latest allowed");
  assert.equal(validateCustomRange("2024-12", "2026-11", october), "", "exactly 24 months is allowed");
  assert.match(validateCustomRange("", "2026-09", october), /Isi bulan mulai dan bulan selesai/);
  assert.match(validateCustomRange("2026-07", "", october), /Isi bulan mulai dan bulan selesai/);
  assert.match(validateCustomRange("2026-13", "2026-09", october), /Isi bulan mulai/, "an impossible month is not a month");
  assert.match(validateCustomRange("2026-09", "2026-07", october), /sebelum atau sama dengan/);
  assert.match(validateCustomRange("2024-11", "2026-11", october), /paling lama 24 bulan/);
  assert.match(validateCustomRange("2026-09", "2026-12", october), /paling jauh bulan depan/);
});

test("API reasons become guidance and the cause is never swallowed", () => {
  assert.match(calendarErrorMessage(400, "custom range must span 1 to 24 months"), /1 sampai 24 bulan/);
  assert.match(calendarErrorMessage(400, "invalid from month"), /Bulan mulai tidak valid/);
  assert.match(calendarErrorMessage(400, "from and to months are both required"), /Isi bulan mulai/);
  assert.match(calendarErrorMessage(400, "something new"), /Rentang tidak dapat dipakai/);
  assert.match(calendarErrorMessage(500, undefined), /belum dapat dimuat/);
  assert.doesNotMatch(calendarErrorMessage(400, "custom range must span 1 to 24 months"), /custom range/, "no raw English reason is shown");
});

test("zero-filled months are marked and the running month says so", () => {
  assert.equal(hasActivity({ income: "0", expense: "0", refund: "0", netCashflow: "0" }), false);
  assert.equal(hasActivity({ income: "0", expense: "0", refund: "250" }), true, "a refund alone is activity");
  assert.equal(hasActivity({ income: "12500000", expense: "0" }), true);
  assert.equal(hasActivity({}), false);
  assert.match(monthRowLabel("2026-10", october), /Okt 26 · berjalan$/);
  assert.doesNotMatch(monthRowLabel("2026-09", october), /berjalan/);
});

test("months before the first transaction collapse into one line, and the refund column only appears when it has a value", () => {
  const months = [
    { period: "2026-06", income: "0", expense: "0", refund: "0", netCashflow: "0" },
    { period: "2026-07", income: "0", expense: "0", refund: "0", netCashflow: "0" },
    { period: "2026-08", income: "12500000", expense: "8100000", refund: "0", netCashflow: "4400000" },
    { period: "2026-09", income: "12500000", expense: "2590000", refund: "0", netCashflow: "9910000" },
  ];
  assert.equal(firstActiveMonth(months), 2, "the first month with a confirmed transaction");
  assert.equal(firstActiveMonth(months.slice(2)), 0, "a range that starts with data needs no line");
  assert.equal(firstActiveMonth([{ period: "2026-06", income: "0", expense: "0" }]), -1, "an empty range has no start");
  assert.equal(firstActiveMonth([]), -1);
  assert.equal(hasRefunds(months), false, "an all-Rp0 refund column is hidden");
  assert.equal(hasRefunds([{ refund: "0" }, { refund: "250" }]), true, "one refund keeps the column");
  assert.equal(hasRefunds([{ period: "2026-08" }]), false, "a missing refund field is not a refund");
});

test("calendar KPI cells format served summary fields and handle zero active months cleanly", () => {
  const cells = calendarKpiCells({
    income: "36800000",
    expense: "17890000",
    refund: "0",
    netCashflow: "18910000",
    months: 3,
    averageMonthlyExpenseRounded: "5963333",
    peakMonth: "2026-08",
    peakMonthExpense: "8100000",
  }, "Mei – Okt 2026");
  assert.equal(cells.length, 4);
  assert.equal(cells[0].lead, true);
  assert.equal(cells[0].label, "Pengeluaran bersih · Mei – Okt 2026");
  assert.match(cells[0].value, /17\.890\.000/);
  assert.match(cells[0].verdict, /Rata-rata .*5\.963\.333 per bulan; tertinggi Agustus 2026, .*8\.100\.000\./);
  assert.match(cells[1].value, /36\.800\.000/);
  assert.match(cells[2].value, /^\+.*18\.910\.000/);
  assert.match(cells[3].value, /5\.963\.333/);
  assert.equal(cells[3].sub, "3 bulan aktif");

  const empty = calendarKpiCells({
    income: "0",
    expense: "0",
    refund: "0",
    netCashflow: "0",
    months: 0,
    averageMonthlyExpenseRounded: "0",
    peakMonth: null,
    peakMonthExpense: null,
  }, "Mei – Okt 2026");
  assert.equal(empty.length, 4);
  assert.equal(empty[0].verdict, undefined);
  assert.match(empty[0].emptyComparison, /Belum ada transaksi terkonfirmasi/);
  assert.equal(empty[3].value, "—");
  assert.equal(calendarVerdict({ months: 0 }), "");
});

test("calendar category view keeps the server order without summing a remainder in JS", () => {
  const items = Array.from({ length: 12 }, (_, index) => ({
    id: `cat-${index}`,
    name: `Kategori ${index}`,
    amount: String((12 - index) * 100000),
    share: "0.08",
  }));
  const { rows } = calendarCategoryView(items);
  assert.equal(rows.length, 12, "all served rows stay intact without a synthetic Kategori lain");
  assert.deepEqual(rows.map(row => row.name), items.map(item => item.name), "the server order is preserved");
  assert.equal(rows[0].length, 1);
  assert.equal(rows[0].shareOfExpense, "0.08");
  assert.equal(calendarCategoryView([]).rows.length, 0);
});

test("a one-month range is named once, not twice, and period button labels drop duplicate years", () => {
  assert.equal(rangeSummary("2026-09", "2026-09"), "Sep 26");
  assert.equal(rangeSummary("2026-08", "2026-09"), "Agu 26 sampai Sep 26");
  assert.equal(calendarPeriodLabel("2026-05", "2026-10"), "Mei – Okt 2026");
  assert.equal(calendarPeriodLabel("2025-11", "2026-04"), "Nov 2025 – Apr 2026");
  assert.equal(calendarPeriodLabel("2026-09", "2026-09"), "Sep 2026");
});

test("range months are offered by Indonesian name, newest first, and an older chosen month stays selectable", () => {
  assert.equal(monthName("2026-06"), "Juni 2026");
  assert.equal(monthName("2026-11"), "November 2026");
  const choices = monthChoices("2026-12");
  assert.equal(choices.length, 36);
  assert.deepEqual(choices[0], { value: "2026-12", label: "Desember 2026" });
  assert.equal(choices.at(-1).value, "2024-01");
  const withOld = monthChoices("2026-12", "2022-05", "not-a-month");
  assert.equal(withOld.length, 37);
  assert.deepEqual(withOld.at(-1), { value: "2022-05", label: "Mei 2022" });
});
