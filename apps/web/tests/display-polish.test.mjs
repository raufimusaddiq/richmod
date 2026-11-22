import assert from "node:assert/strict";
import test from "node:test";
import { dateTime, displayDate, money } from "../app/lib/format.js";
import { snapshotSourceLabel } from "../app/lib/labels.js";
import { notFoundMeta, routeMeta } from "../app/lib/siteMeta.js";

test("displayDate renders Indonesian day-month-year in Asia/Jakarta", () => {
  assert.equal(displayDate("2026-10-22"), "22 Okt 2026");
  assert.equal(displayDate("2026-10-22T14:53:00+07:00"), "22 Okt 2026");
  assert.equal(displayDate("2026-10-22T17:30:00Z"), "23 Okt 2026");
  assert.equal(displayDate(""), "—");
  assert.equal(displayDate("bukan tanggal"), "—");
});

test("dateTime keeps the dotted Jakarta clock format", () => {
  assert.equal(dateTime("2026-10-22T14:53:00+07:00"), "22 Okt 2026, 14.53");
});

test("the not-found page has its own Indonesian metadata", () => {
  assert.equal(notFoundMeta.title, "Halaman tidak ditemukan · Richmod");
  assert.ok(notFoundMeta.description.length > 0);
});

test("every app route sets its own title", () => {
  const titles = new Map();
  for (const route of ["transactions", "analytics", "wealth", "documents", "household", "settings", "inbox", "admin", "login", "invite"]) {
    const { title, description } = routeMeta[`/${route}`];
    assert.match(title, / · Richmod$/, route);
    assert.ok(description.length > 0, route);
    titles.set(title, route);
  }
  assert.equal(titles.size, 10);
  assert.equal(routeMeta["/transactions"].title, "Transaksi · Richmod");
});

test("displayDate treats a date-only string as a Jakarta calendar day", () => {
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
