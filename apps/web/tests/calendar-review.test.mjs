import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { addMonths, calendarErrorMessage, chartCategories, currentMonthKey, firstActiveMonth, hasActivity, hasRefunds, monthRowLabel, monthSpan, nextMonthKey, rangeSummary, validateCustomRange } from "../app/lib/calendarReview.js";
import { globalCss, tree } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const october = new Date("2026-10-02T05:00:00Z"); // 12:00 in Jakarta

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

test("the category chart keeps the top ten and folds the rest into Kategori lain", () => {
  const items = Array.from({ length: 25 }, (_, index) => ({ id: `cat-${index}`, name: `Kategori ${index}`, amount: String((25 - index) * 100000) }));
  const chart = chartCategories(items);
  assert.equal(chart.length, 11, "ten bars plus one");
  assert.deepEqual(chart.slice(0, 10).map(item => item.name), items.slice(0, 10).map(item => item.name), "the server's order is kept");
  assert.equal(chart[10].name, "Kategori lain");
  assert.equal(chart[10].remainder, true, "the remainder bar is marked so it can read as a remainder");
  assert.equal(chart[10].amount, String(items.slice(10).reduce((sum, item) => sum + Number(item.amount), 0)), "the tail is summed, not dropped");
  assert.equal(chartCategories(items.slice(0, 10)).length, 10, "a short list is untouched");
  assert.equal(chartCategories([]).length, 0);
});

test("a one-month range is named once, not twice", () => {
  assert.equal(rangeSummary("2026-09", "2026-09"), "Sep 26");
  assert.equal(rangeSummary("2026-08", "2026-09"), "Agu 26 sampai Sep 26");
});

test("calendar source: independent sections, controlled range, visible reason, formatted months", () => {
  const page = tree("app/analytics");
  for (const name of ["cashflow", "categories", "merchants", "members"]) assert.match(page, new RegExp(`useCalendarSection\\("${name}", queryString\\)`), `${name} loads on its own`);
  assert.doesNotMatch(page, /Promise\.all/, "one failing request must not blank the whole view");
  assert.match(page, /calendarErrorMessage\(response\.status, body\?\.error\)/, "the API's reason is read, not replaced by a fixed sentence");
  assert.match(page, /onSubmit=\{submitRange\} noValidate/);
  assert.doesNotMatch(page, /defaultValue=\{selection\.(from|to)\}/, "range inputs follow the URL after Back or a preset");
  assert.match(page, /const shown = defaultRange\(\{ from: selectedFrom, to: selectedTo, range: selectedRange \}\)/);
  assert.match(page, /setFrom\(shown\.from\); setTo\(shown\.to\);/);
  assert.match(page, /const fallback = defaultRange\(selection\)/, "the month fields open on the range already shown");
  assert.match(page, /<button type="submit" disabled=\{!draft\}>Terapkan<\/button>/, "the apply button is an action, disabled until the fields differ");
  assert.doesNotMatch(page, /aria-pressed=\{custom\}/, "the apply button is not a preset toggle");
  assert.match(page, /max: latest/);
  assert.match(page, /monthRowLabel\(item\.period, now\)/);
  assert.doesNotMatch(page, /<th scope="row">\{item\.period\}<\/th>/, "no raw YYYY-MM row headers");
  assert.match(page, /<Skeleton cards=\{1\} rows=\{rows\}/, "each section shows its own skeleton on first load");
  assert.match(page, /setState\(\{ data: null, error: err\.message, loading: false \}\)/, "a failed section clears its own old numbers");
  assert.match(page, /<ErrorNotice message=\{state\.error\} retry=\{state\.retry\}\/>/, "each failed section retries on its own");
  assert.match(page, /const allFailed = sections\.every\(section => section\.error\)/, "a range every section rejects shows one notice, not four");
  assert.match(page, /hideError/, "the table sharing the chart's request does not repeat its error");
});

test("calendar charts follow the cycle rules: bar-order legend, server ranking, readable values", () => {
  const charts = readFileSync(new URL("../app/components/Charts.js", import.meta.url), "utf8");
  assert.ok(charts.includes('<li data-series="income"><i aria-hidden="true"/>Pemasukan</li><li data-series="expense">'), "legend order matches bar order");
  assert.doesNotMatch(charts, /Math\.round\(item\.value/, "the tooltip never rounds a served amount");
  assert.match(charts, /money\(item\?\.income\)/, "the tooltip prints the served string");
  const page = tree("app/analytics");
  const css = globalCss();
  assert.match(page, /<CategoryRankingChart items=\{chartCategories\(items\)\} height=\{160\}/, "the calendar category chart keeps the server's amount order and folds the tail");
  assert.doesNotMatch(page, /serverOwned/, "the web never re-sorts a money ranking");
  assert.match(page, /Lihat nilai kategori/, "category values are readable without a tooltip");
  assert.match(page, /role="group" aria-label="Rentang bulan"/);
  assert.match(page, /<MonthlyTable items=\{items\} now=\{now\} cycle=\{selection\.cycle\}\/>/, "the monthly table is one component");
  assert.match(page, /Data mulai \{monthLabel\(items\[start\]\.period\)\}/, "leading empty months are one line");
  assert.match(page, /data-label=\{labels\[/, "stacked rows keep their column names");
  assert.match(page, /Rentang bulan \$\{rangeSummary\(selection\.from, selection\.to\)\}/, "a one-month range is named once");
  assert.match(charts, /item\.remainder \? "var\(--line-strong\)" : "var\(--chart-category-2\)"/, "the remainder bar uses a muted fill");
  assert.match(css, /\.cycle-review \.custom-range button:disabled \{ opacity: 1;/, "a disabled apply button keeps readable contrast");
});

test("the phone table stacks with its labels and the apply button shares the field baseline", () => {
  const responsive = readFileSync(new URL("../app/styles/11-responsive.css", import.meta.url), "utf8");
  assert.match(responsive, /\.review-table-wrap:has\(\.monthly-table\)/, "only the monthly table drops its scroll frame on a phone");
  assert.match(responsive, /\.monthly-table thead \{ position: absolute; width: 1px; height: 1px;/, "the headers stay in the accessibility tree while rows stack");
  assert.match(responsive, /\.monthly-table td::before \{ content: attr\(data-label\)/, "a stacked value keeps its column name");
  const review = readFileSync(new URL("../app/styles/06-analytics-and-cycle-review.css", import.meta.url), "utf8");
  assert.match(review, /\.cycle-review \.custom-range \{ align-items: flex-end; \}/, "the apply button shares the month fields' baseline");
  assert.match(review, /\.cycle-review \.custom-range button \{ min-height: 44px; padding: 0 16px; border-color: var\(--ink\)/, "and is a 44px control whose outline stays visible while disabled");
});
