import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { buildCategoryPalette, categoryColorFor, categoryColorMap, changesView, OTHER_CATEGORY_COLOR, partialDay, rankingView } from "../app/lib/cycleWhere.js";
import { categoryVisual } from "../app/lib/categoryIcons.js";
import { cycleFacts, stableCycleFacts } from "./fixtures/cycle-review.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("the ranking is the served order and total; the web sorts and sums nothing", () => {
  const facts = cycleFacts();
  facts.categoryRanking.rows = [{ ...facts.categoryRanking.rows[0], amount: "9000", name: "B" }, { ...facts.categoryRanking.rows[0], amount: "10000", name: "A" }];
  const view = rankingView(facts);
  assert.deepEqual(view.rows.map(row => row.name), ["B", "A"], "served order is kept");
  assert.equal(view.total, facts.cashflow.expense, "the footer total is the served expense");
  assert.equal(view.rows[0].length, 0.9);
  assert.equal(view.rows[1].length, 1);
});

test("a ranking row opens the drawer only when its category evidence is served", () => {
  const facts = cycleFacts();
  facts.categoryRanking.rows[0].id = "no-evidence";
  const view = rankingView(facts);
  assert.equal(view.rows[0].openable, false);
  assert.equal(view.rows[1].openable, true);
  assert.equal(rankingView({ ...facts, categoryRanking: undefined }), null);
});

test("ranking and change deltas follow the baseline of the cycle state", () => {
  const active = cycleFacts();
  assert.match(rankingView(active).baseline, /hari ke-6/);
  const closed = cycleFacts("2026-08-26");
  assert.match(rankingView(closed).baseline, /penuh/);
  const dining = view => view.rows.find(row => row.name === "Makan di luar");
  active.categoryChanges[1].deltaVsPrevious = "111";
  active.categoryChanges[1].deltaVsPreviousFullCycle = "222";
  assert.match(dining(changesView(active)).delta.text, /111/);
  closed.categoryChanges[1].deltaVsPrevious = "111";
  closed.categoryChanges[1].deltaVsPreviousFullCycle = "222";
  assert.match(dining(changesView(closed)).delta.text, /222/);
});

test("Yang berubah takes the first five in server order, never re-sorted", () => {
  const facts = cycleFacts();
  facts.categoryChanges = Array.from({ length: 8 }, (_, index) => ({ ...facts.categoryChanges[0], id: `c${index}`, name: `K${index}`, deltaVsPrevious: String(index % 2 ? -(index * 100) : index * 100) }));
  const view = changesView(facts);
  assert.deepEqual(view.rows.map(row => row.name), ["K0", "K1", "K2", "K3", "K4"]);
  assert.equal(view.rows[1].side, "less");
  assert.equal(view.rows[2].side, "more");
  assert.equal(view.rows[0].side, "", "a zero change has no side");
  assert.equal(view.rows[4].width, "100%");
});

test("without a comparison cycle the card says so instead of drawing deltas", () => {
  const facts = cycleFacts();
  facts.comparison = { ...facts.comparison, previous: null, previousFullCycle: null };
  assert.equal(changesView(facts).hasBaseline, false);
  const empty = rankingView({ ...cycleFacts(), categoryRanking: { ...cycleFacts().categoryRanking, rows: cycleFacts().categoryRanking.rows.map(row => ({ ...row, deltaVsBaseline: null, relativeDeltaVsBaseline: null })) } });
  assert.equal(empty.rows[0].delta.empty, true);
});

test("only the running day of an active cycle is partial", () => {
  const facts = stableCycleFacts();
  assert.equal(partialDay({ state: "CLOSED" }, facts.daily), "");
  const active = cycleFacts();
  assert.equal(partialDay(active.period, active.daily), active.daily.at(-1).period);
  assert.equal(partialDay(active.period, []), "");
});

test("the new row does no arithmetic on money", () => {
  for (const file of ["app/lib/cycleWhere.js", "app/analytics/CycleWhere.js"]) {
    const source = text(file);
    assert.doesNotMatch(source, /\.reduce\(|Math\.round|\.sort\(|BigInt/, `${file} only formats served figures`);
  }
});

test("the category drawer is a labelled modal that reuses the shared focus trap", () => {
  const source = text("app/analytics/CycleWhere.js");
  assert.match(source, /from "\.\.\/lib\/focusTrap"/);
  assert.match(source, /role="dialog" aria-modal="true" aria-labelledby="drivers-title"/);
  assert.match(source, /opener\.isConnected/, "focus returns to the trigger");
  assert.match(source, /className="category-drawer-title"><i className="where-swatch"/, "category drawer shows its shared category swatch");
});

test("categoryColorMap assigns one stable distinct colour per category across screens, subsets, and icons", () => {
  const eight = [
    { id: "c-ortu", name: "Orang Tua", amount: "4500000" },
    { id: "c-tinggal", name: "Tempat Tinggal", amount: "3200000" },
    { id: "c-kopi", name: "Kopi & Camilan", amount: "1400000" },
    { id: "c-jalan", name: "Perjalanan", amount: "1100000" },
    { id: "c-makan", name: "Makan di Luar", amount: "900000" },
    { id: "c-belanja", name: "Belanja Bahan Makanan", amount: "750000" },
    { id: "c-keluarga", name: "Keluarga", amount: "600000" },
    { id: "c-hiburan", name: "Hiburan", amount: "200000" },
  ];
  const five = eight.slice(0, 5);
  const palette = buildCategoryPalette(eight);
  const paletteReversed = buildCategoryPalette([...eight].reverse());
  const berandaFive = categoryColorMap(five, palette);
  const berandaEight = categoryColorMap(eight, palette);
  const laporan = categoryColorMap({ categoryRanking: { rows: eight.slice(0, 7) }, categoryHistory: { rows: eight } }, palette);
  const reordered = categoryColorMap({ categoryRanking: { rows: [...eight.slice(0, 7)].reverse() }, categoryHistory: { rows: [...eight].reverse() } }, paletteReversed);
  const assigned = new Set();
  for (const row of eight.slice(0, 7)) {
    const color = laporan.colors.get(row.id);
    assert.ok(color && color !== OTHER_CATEGORY_COLOR, `${row.name} gets a category colour`);
    assert.equal(berandaEight.colors.get(row.id), color, `${row.name} keeps the same colour on Beranda and Laporan`);
    assert.equal(reordered.colors.get(row.id), color, `${row.name} keeps the same colour when list order changes`);
    assert.equal(categoryColorFor(row, palette), color, `${row.name} categoryColorFor matches chart map`);
    assert.equal(categoryVisual(row, palette).color, color, `${row.name} CategoryIcon visual matches chart map`);
    assigned.add(color);
  }
  for (const row of five) {
    assert.equal(berandaFive.colors.get(row.id), berandaEight.colors.get(row.id), `${row.name} keeps the same colour across 5- and 8-category subsets`);
    assert.equal(categoryVisual(row, palette).color, berandaFive.colors.get(row.id), `${row.name} icon matches 5-category subset`);
    assert.equal(categoryColorMap(five).colors.get(row.id), categoryColorMap(eight).colors.get(row.id), `${row.name} stays stable across subsets even without a household palette`);
    assert.equal(categoryVisual(row).color, categoryColorMap(eight).colors.get(row.id), `${row.name} icon matches chart map even without a household palette`);
  }
  assert.equal(assigned.size, 7, "all seven ranked categories have distinct colours with no repeats");
  assert.notEqual(laporan.colors.get("c-keluarga"), laporan.colors.get("c-ortu"));
  assert.notEqual(berandaEight.colors.get("c-kopi"), berandaEight.colors.get("c-jalan"));
  assert.equal(new Set(eight.map(row => laporan.colors.get(row.id))).size, 8, "all eight categories stay distinct");
  assert.equal(categoryVisual(eight[7], palette).color, laporan.colors.get("c-hiburan"), "8th category icon matches chart map");
});
