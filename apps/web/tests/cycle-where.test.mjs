import assert from "node:assert/strict";
import test from "node:test";
import { buildCategoryPalette, categoryColorFor, categoryColorMap, changesView, OTHER_CATEGORY_COLOR, partialDay, rankingView } from "../app/lib/cycleWhere.js";
import { categoryVisual } from "../app/lib/categoryIcons.js";
import { cycleFacts, stableCycleFacts } from "./fixtures/cycle-review.mjs";

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

test("rankingView omits sand on non-other rows and maps expandable other.rows with openable flags", () => {
  const facts = cycleFacts();
  const extraId = "77777777-7777-4777-8777-777777777777";
  facts.categoryChanges.push({ ...facts.categoryChanges[0], id: extraId, name: "Perjalanan", amount: "631000" });
  facts.categoryRanking.other = {
    name: "Kategori lain",
    amount: "631000",
    count: 2,
    categoryCount: 2,
    shareOfExpense: "0.1306",
    rows: [
      { id: extraId, name: "Perjalanan", amount: "631000", count: 2, shareOfExpense: "0.1306" },
      { id: null, name: "Belum dikategorikan", amount: "50000", count: 1, shareOfExpense: "0.0103" },
    ],
  };
  const palette = new Map([
    [facts.categoryRanking.rows[0].id, "var(--chart-category-1)"],
    [facts.categoryRanking.rows[1].id, "var(--chart-category-2)"],
  ]);
  const view = rankingView(facts, palette);
  assert.equal(view.rows[0].color, "var(--chart-category-1)");
  assert.equal(view.rows[1].color, "var(--chart-category-2)");
  assert.equal(view.rows[2].color, "", "unranked row never gets a sand swatch");
  assert.equal(view.other.color, OTHER_CATEGORY_COLOR, "Kategori lain keeps sand");
  assert.equal(view.other.rows.length, 2);
  assert.equal(view.other.rows[0].name, "Perjalanan");
  assert.equal(view.other.rows[0].color, "", "expanded remaining categories have no swatch");
  assert.equal(view.other.rows[0].openable, true);
  assert.equal(view.other.rows[1].key, "uncategorized");
  assert.equal(view.other.rows[1].openable, false);
});

test("buildCategoryPalette ranks top 6 by confirmed expense, folds >6 into other, breaks ties deterministically, and handles <6", () => {
  const ten = [
    { id: "c-ortu", name: "Orang Tua", amount: "4500000" },
    { id: "c-tinggal", name: "Tempat Tinggal", amount: "3200000" },
    { id: "c-langganan", name: "Langganan", amount: "1900000" },
    { id: "c-kopi", name: "Kopi & Camilan", amount: "1400000" },
    { id: "c-jalan", name: "Perjalanan", amount: "1100000" },
    { id: "c-belanja", name: "Belanja Bahan Makanan", amount: "750000" },
    { id: "c-keluarga", name: "Keluarga", amount: "600000" },
    { id: "c-hiburan", name: "Hiburan", amount: "200000" },
    { id: "c-zero", name: "Nol", amount: "0" },
    { id: null, name: "Belum dikategorikan", amount: "9999999" },
  ];
  const palette = buildCategoryPalette(ten);
  const paletteShuffled = buildCategoryPalette([...ten].reverse());
  assert.deepEqual([...palette.entries()], [...paletteShuffled.entries()], "palette depends on amount rank, not input order");
  assert.equal(palette.size, 6, "at most six categories receive a category colour");
  assert.deepEqual([...palette.entries()], [
    ["c-ortu", "var(--chart-category-1)"],
    ["c-tinggal", "var(--chart-category-2)"],
    ["c-langganan", "var(--chart-category-3)"],
    ["c-kopi", "var(--chart-category-4)"],
    ["c-jalan", "var(--chart-category-5)"],
    ["c-belanja", "var(--chart-category-6)"],
  ]);
  assert.equal(new Set(palette.values()).size, 6, "all six ranked categories have distinct colours with no repeats");

  const berandaFive = categoryColorMap(ten.slice(0, 5), palette);
  const berandaEight = categoryColorMap(ten.slice(0, 8), palette);
  const laporan = categoryColorMap({ categoryRanking: { rows: ten.slice(0, 7) }, categoryHistory: { rows: ten } }, palette);
  for (const row of ten.slice(0, 6)) {
    const color = palette.get(row.id);
    assert.equal(laporan.colors.get(row.id), color, `${row.name} keeps rank colour in Laporan`);
    assert.equal(berandaEight.colors.get(row.id), color, `${row.name} keeps rank colour on Beranda`);
    assert.equal(categoryColorFor(row, palette), color, `${row.name} categoryColorFor matches palette`);
    assert.equal(categoryVisual(row, palette).color, color, `${row.name} CategoryIcon visual matches palette`);
  }
  for (const row of ten.slice(0, 5)) {
    assert.equal(berandaFive.colors.get(row.id), berandaEight.colors.get(row.id), `${row.name} stays stable across subsets`);
  }
  for (const extra of [ten[6], ten[7], ten[8], ten[9]]) {
    const key = extra.id || "uncategorized";
    assert.equal(categoryColorFor(extra, palette), OTHER_CATEGORY_COLOR, `${extra.name} outside top 6 uses sand`);
    assert.equal(laporan.colors.get(key), OTHER_CATEGORY_COLOR, `${extra.name} uses sand in Laporan`);
    assert.equal(categoryVisual(extra, palette).color, OTHER_CATEGORY_COLOR, `${extra.name} icon uses sand`);
    assert.equal(categoryVisual(extra, palette).tone, "other", `${extra.name} icon uses other tone`);
  }

  const tiedA = buildCategoryPalette([
    { id: "cat-b", name: "Beta", amount: "500000" },
    { id: "cat-a", name: "Alpha", amount: "500000" },
    { id: "cat-c", name: "Gamma", amount: "500000" },
  ]);
  const tiedB = buildCategoryPalette([
    { id: "cat-c", name: "Gamma", amount: "500000" },
    { id: "cat-a", name: "Alpha", amount: "500000" },
    { id: "cat-b", name: "Beta", amount: "500000" },
  ]);
  assert.deepEqual([...tiedA.entries()], [...tiedB.entries()], "ties break deterministically regardless of input order");
  assert.deepEqual([...tiedA.entries()], [
    ["cat-a", "var(--chart-category-1)"],
    ["cat-b", "var(--chart-category-2)"],
    ["cat-c", "var(--chart-category-3)"],
  ]);

  const fewer = buildCategoryPalette([
    { id: "cat-only-1", name: "Satu", amount: "120000" },
    { id: "cat-only-2", name: "Dua", amount: "80000.50" },
    { id: "cat-neg", name: "Refund", amount: "-5000" },
  ]);
  assert.equal(fewer.size, 2, "fewer than 6 categories only fills available positive ranks");
  assert.equal(fewer.get("cat-only-1"), "var(--chart-category-1)");
  assert.equal(fewer.get("cat-only-2"), "var(--chart-category-2)");
  assert.equal(categoryColorFor({ id: "cat-neg", name: "Refund" }, fewer), OTHER_CATEGORY_COLOR);
  assert.equal(buildCategoryPalette([]).size, 0, "empty input produces an empty palette");
});
