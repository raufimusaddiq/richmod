import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";
import { dateTime, displayDate, money } from "../app/lib/format.js";
import { snapshotSourceLabel } from "../app/lib/labels.js";
import { globalCss } from "./source.mjs";

const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("displayDate renders Indonesian day-month-year in Asia/Jakarta", () => {
  assert.equal(displayDate("2026-10-22"), "22 Okt 2026");
  assert.equal(displayDate("2026-10-22T14:53:00+07:00"), "22 Okt 2026");
  // 17:30Z is already the next day in Jakarta.
  assert.equal(displayDate("2026-10-22T17:30:00Z"), "23 Okt 2026");
  assert.equal(displayDate(""), "—");
  assert.equal(displayDate("bukan tanggal"), "—");
});

test("dateTime keeps the dotted Jakarta clock format", () => {
  assert.equal(dateTime("2026-10-22T14:53:00+07:00"), "22 Okt 2026, 14.53");
});

test("the home wealth card uses the shared formatter, not a numeric locale date", () => {
  const home = read("app/page.js");
  assert.doesNotMatch(home, /toLocaleDateString\("id-ID"\)/);
  assert.match(home, /dateTime\(latestWealth\.observedAt\)/);
});

test("not-found page is Indonesian, links to Beranda and sets metadata", () => {
  assert.ok(existsSync(new URL("../app/not-found.js", import.meta.url)));
  const page = read("app/not-found.js");
  assert.match(page, /Halaman tidak ditemukan/);
  assert.match(page, /href="\/"[^>]*>Kembali ke Beranda|Kembali ke Beranda/);
  assert.match(page, /export const metadata/);
});

test("every app route sets its own title", () => {
  const titles = new Map();
  for (const route of ["transactions", "analytics", "wealth", "documents", "household", "settings", "inbox", "admin", "login", "invite"]) {
    const layout = read(`app/${route}/layout.js`);
    const title = layout.match(/title: "([^"]+)"/)?.[1];
    assert.match(title, / · Richmod$/, route);
    assert.match(layout, /description: "[^"]+"/, route);
    titles.set(title, route);
  }
  assert.equal(titles.size, 10);
  assert.equal(read("app/transactions/layout.js").match(/title: "([^"]+)"/)[1], "Transaksi · Richmod");
});

test("settings wraps unbreakable generated addresses on mobile", () => {
  const css = globalCss();
  assert.match(css, /\.integration-grid small[^{]*\{[^}]*overflow-wrap: anywhere/);
  assert.match(css, /\.email-checklist li small[^{]*\{[^}]*overflow-wrap: anywhere/);
});

test("Indonesian copy replaces raw MANUAL and the Test button", () => {
  assert.match(read("app/lib/labels.js"), /MANUAL: "Input manual"/);
  assert.match(read("app/wealth/page.js"), /snapshotSourceLabel\[item\.source\]/);
  assert.match(read("app/components/SettingsPanels.js"), /testFinancialSource\(item\)\}>Uji</);
  assert.doesNotMatch(read("app/components/SettingsPanels.js"), />Test</);
});

test("displayDate's date-only guard is a real digit pattern", () => {
  // A date-only value must be read as a Jakarta calendar day, never as UTC midnight.
  assert.ok(read("app/lib/format.js").includes(String.raw`/^\d{4}-\d{2}-\d{2}$/`));
  assert.equal(displayDate("2026-12-31"), "31 Des 2026");
});

test("snapshot source labels cover exactly the values the schema allows", () => {
  assert.deepEqual(Object.keys(snapshotSourceLabel).sort(), ["DOCUMENT", "MANUAL", "SYSTEM"]);
});

test("money accepts integer and decimal strings with the real formatter", () => {
  const norm = v => money(v).replace(/\s/g, " ");
  assert.equal(norm("45000"), "Rp 45.000");
  assert.equal(norm("45000.00"), "Rp 45.000");
  assert.equal(norm("45000.99"), "Rp 45.000");
  assert.equal(norm("-1500.50"), "−Rp 1.500");
  assert.equal(norm(null), "Rp 0");
  assert.equal(norm("abc"), "Rp0");
});
