import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { addMonths, calendarErrorMessage, currentMonthKey, hasActivity, monthRowLabel, monthSpan, nextMonthKey, validateCustomRange } from "../app/lib/calendarReview.js";
import { tree } from "./source.mjs";

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

test("calendar source: no duplicate request, controlled range, visible reason, formatted months", () => {
  const page = tree("app/analytics");
  for (const name of ["cashflow", "categories", "merchants", "members"]) assert.match(page, new RegExp(`useCalendarSection\\("${name}", queryString\\)`), `${name} loads on its own`);
  assert.doesNotMatch(page, /Promise\.all/, "one failing request must not blank the whole view");
  assert.doesNotMatch(page, /"spending"/, "monthly spending duplicated the cashflow expense column");
  assert.doesNotMatch(page, /Pengeluaran setelah refund/, "the redundant section is gone; its numbers are the cashflow expense column");
  assert.match(page, /calendarErrorMessage\(response\.status, body\?\.error\)/, "the API's reason is read, not replaced by a fixed sentence");
  assert.match(page, /onSubmit=\{submitRange\} noValidate/);
  assert.doesNotMatch(page, /defaultValue=\{selection\.(from|to)\}/, "range inputs follow the URL after Back or a preset");
  assert.match(page, /useEffect\(\(\) => \{ setFrom\(selection\.from\); setTo\(selection\.to\);/);
  assert.match(page, /aria-pressed=\{custom\}>Kustom/);
  assert.match(page, /max: latest/);
  assert.match(page, /monthRowLabel\(item\.period, now\)/);
  assert.doesNotMatch(page, /<th scope="row">\{item\.period\}<\/th>/, "no raw YYYY-MM row headers");
  assert.match(page, /<Skeleton cards=\{1\} rows=\{rows\}/, "each section shows its own skeleton on first load");
  assert.match(page, /setState\(\{ data: null, error: err\.message, loading: false \}\)/, "a failed section clears its own old numbers");
  assert.match(page, /<ErrorNotice message=\{state\.error\} retry=\{state\.retry\}\/>/, "each failed section retries on its own");
  assert.match(page, /const allFailed = sections\.every\(section => section\.error\)/, "a range every section rejects shows one notice, not four");
  assert.match(page, /hideError/, "the table sharing the chart's request does not repeat its error");
  assert.match(page, /Pengeluaran bersih<\/th><th scope="col">Refund<\/th>/);
  assert.match(text("app/styles/06-analytics-and-cycle-review.css"), /\.calendar-section\[data-stale\]/);
});
