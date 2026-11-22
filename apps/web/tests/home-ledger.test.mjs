import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { FALLBACK_ICON, categoryVisual, normalizeCategoryName } from "../app/lib/categoryIcons.js";
import { categoryLedgerHref, clippedBarLabels, comparisonLine, dailyAxis, dailyChartModel, dayLedgerHref, formatDailyTick, homeState, rankedCategories, spendingShare } from "../app/lib/home.js";
import { groupByDay, jakartaDay } from "../app/lib/ledgerDays.js";
import { hidesVoided, originTimeline, periodRange, periodValue, transactionDisplayName, withVoidedPolicy } from "../app/lib/transactionView.js";

test("category names map to a Phosphor icon, never a letter box", () => {
  const expected = { "Kopi & Snack": "Coffee", "Belanja Bulanan": "Basket", Makan: "ForkKnife", "Rumah Tangga": "House", Transportasi: "Car", Kesehatan: "FirstAidKit", Listrik: "Lightning", Langganan: "Repeat", "Orang tua": "Users", Anak: "Baby", Hiburan: "Ticket", Belanja: "ShoppingBag", Tagihan: "Receipt", Liburan: "Airplane", "Tempat Tinggal": "House", "Belanja Bahan Makanan": "Basket", Perjalanan: "Airplane" };
  for (const [name, icon] of Object.entries(expected)) assert.equal(categoryVisual(name).icon, icon, name);
  assert.equal(categoryVisual({ name: "Lain-lain", slug: "lain-lain" }).icon, FALLBACK_ICON);
  assert.equal(categoryVisual({ name: "", slug: "groceries" }).icon, "Basket", "the slug is matched too");
  assert.equal(categoryVisual(null).icon, FALLBACK_ICON);
  assert.equal(normalizeCategoryName("Késehatan & Obat"), "kesehatan obat");
});

test("category tone is stable and one of the eight chart tones", () => {
  const first = categoryVisual("Dana darurat"), again = categoryVisual("Dana darurat");
  assert.deepEqual(first, again);
  assert.ok(first.tone >= 1 && first.tone <= 8);
  assert.equal(first.color, `var(--chart-category-${first.tone})`);
  const bars = readFileSync(new URL("../app/components/CategoryBars.js", import.meta.url), "utf8");
  assert.match(bars, /categoryColorMap\(items, palette\)/, "Beranda Ke mana uangnya shares categoryColorMap");
  assert.match(bars, /row\.other \? OTHER_CATEGORY_COLOR/, "Kategori lain uses sand --chart-category-other");
  assert.doesNotMatch(bars, /var\(--line-strong\)/, "Beranda Kategori lain no longer uses green-grey line-strong");
});

const day = (period, expense) => ({ period, expense: String(expense), expenseValue: expense });

test("the daily chart caps near the 90th-percentile day so at most ~3 bars are clipped", () => {
  const calm = dailyChartModel([day("2026-11-01", 100), day("2026-11-02", 120), day("2026-11-03", 90), day("2026-11-04", 250)]);
  assert.equal(calm.cap, null, "modest spread stays uncapped");
  assert.ok(calm.rows.every(row => !row.capped));
  const spike = dailyChartModel([day("2026-11-01", 100), day("2026-11-02", 120), day("2026-11-03", 90), day("2026-11-04", 5000)]);
  assert.equal(spike.cap, 150, "p90 of 120 rounds cleanly to 150");
  assert.equal(spike.rows[3].capped, true);
  assert.equal(spike.rows[3].plotValue, 150);
  assert.equal(spike.rows[3].expenseValue, 5000, "the real value survives for the label");
  assert.equal(spike.rows[0].plotValue, 100);
  assert.equal(spike.biggest.period, "2026-11-04");

  const realCycle = [2507500, 101885, 0, 975685, 38000, 36500, 250397, 215672, 374640, 2152100, 636507, 296909, 126959, 30000, 86000, 231346, 335250, 47308, 51700, 570920, 40383, 199001, 0, 152411, 193332, 36032, 138750, 47847, 50800, 0]
    .map((amount, index) => day(`2026-11-${String(index + 1).padStart(2, "0")}`, amount));
  const model = dailyChartModel(realCycle);
  const clipped = model.rows.filter(row => row.capped);
  assert.equal(model.cap, 750_000);
  assert.ok(clipped.length > 0 && clipped.length <= 3, `expected at most 3 clipped bars, got ${clipped.length}`);
  assert.deepEqual(model.axis.ticks, [0, 250_000, 500_000, 750_000]);
  assert.deepEqual(model.axis.ticks.map(formatDailyTick), ["0", "250rb", "500rb", "750rb"]);
});

test("daily axis produces distinct round ticks with rb below 1 jt and jt at/above 1 jt", () => {
  const halfMillion = dailyAxis(450_000);
  assert.deepEqual(halfMillion.ticks, [0, 250_000, 500_000]);
  assert.deepEqual(halfMillion.ticks.map(formatDailyTick), ["0", "250rb", "500rb"]);
  const million = dailyAxis(1_200_000);
  assert.deepEqual(million.ticks, [0, 500_000, 1_000_000, 1_500_000]);
  assert.deepEqual(million.ticks.map(formatDailyTick), ["0", "500rb", "1 jt", "1,5 jt"]);
  assert.equal(formatDailyTick(975_685), "976rb");
  assert.equal(formatDailyTick(2_507_500), "2,5 jt");
  assert.equal(formatDailyTick(999_600), "1 jt");
  assert.equal(formatDailyTick(2_500), "2,5rb");
  assert.equal(formatDailyTick(0), "0");
});

test("clipped bar labels stagger nearby spikes and label only the largest on narrow widths", () => {
  const rows = Array.from({ length: 15 }, (_, index) => ({
    period: `2026-11-${String(index + 1).padStart(2, "0")}`,
    expenseValue: index === 0 ? 2_500_000 : index === 9 ? 2_150_000 : index === 10 ? 900_000 : 100_000,
    capped: index === 0 || index === 9 || index === 10,
  }));
  const wide = clippedBarLabels(rows, { narrow: false });
  assert.equal(wide.get(0)?.tier, 0);
  assert.equal(wide.get(9)?.tier, 0);
  assert.equal(wide.get(10)?.tier, 1, "adjacent clipped bar staggers onto the upper row");
  assert.equal(wide.get(10)?.text, "900rb");
  const narrow = clippedBarLabels(rows, { narrow: true });
  assert.equal(narrow.size, 1, "narrow widths label only the largest clipped bar");
  assert.equal(narrow.get(0)?.text, "2,5 jt");

  const charts = readFileSync(new URL("../app/components/Charts.js", import.meta.url), "utf8");
  assert.match(charts, /fill="var\(--ink-soft\)"/, "daily bars render in neutral ink, not expense red");
  assert.doesNotMatch(charts.slice(charts.indexOf("function DailyBarShape"), charts.indexOf("export function CycleSpendingPatternChart")), /var\(--chart-expense\)/);
});

test("transaction display name prefers merchant, then counterparty, then description, and labels transfers", () => {
  assert.equal(transactionDisplayName({ type: "EXPENSE", merchantName: "Grab", counterpartyName: "GRAB*1234", description: "Payment to GRAB*1234" }), "Grab");
  assert.equal(transactionDisplayName({ type: "EXPENSE", merchantName: null, counterpartyName: "ShopeeFood", description: "Transaksi menggunakan kartu debit Jago" }), "ShopeeFood");
  assert.equal(transactionDisplayName({ type: "EXPENSE", merchantName: "", counterpartyName: "", description: "Beli kopi" }), "Beli kopi");
  assert.equal(transactionDisplayName({ type: "TRANSFER", counterpartyName: "DNID RAUXX MUSXXXXX", description: "You have just transferred IDR300,000 to DNID RAUXX MUSXXXXX." }), "Transfer ke DNID RAUXX MUSXXXXX");
  assert.equal(transactionDisplayName({ type: "TRANSFER", counterpartyName: "Transfer ke Tabungan", description: "raw" }), "Transfer ke Tabungan");
  assert.equal(transactionDisplayName({ type: "TRANSFER", counterpartyName: null, description: "Pindah ke tabungan" }), "Pindah ke tabungan");
  assert.equal(transactionDisplayName({ type: "TRANSFER" }), "Transfer");
  assert.equal(transactionDisplayName({}), "Transaksi");
  assert.match(readFileSync(new URL("../app/components/TransactionList.js", import.meta.url), "utf8"), /transactionDisplayName\(item\)/);
  assert.match(readFileSync(new URL("../app/transactions/page.js", import.meta.url), "utf8"), /<h2>\{transactionDisplayName\(selected\)\}<\/h2>/);
});

test("no cap when most days are zero (median 0) and the average is exact integer math", () => {
  const sparse = dailyChartModel([day("2026-11-01", 0), day("2026-11-02", 0), day("2026-11-03", 9000)]);
  assert.equal(sparse.cap, null);
  assert.equal(sparse.average, "3000");
  assert.equal(sparse.spendDays, 1);
  assert.equal(dailyChartModel([]).average, "0");
  assert.equal(dailyChartModel([]).biggest, null);
});

test("Beranda state: empty, expenses-only, or normal", () => {
  assert.equal(homeState({ confirmedCount: 0, income: "0", expense: "0" }, []), "empty");
  assert.equal(homeState({ confirmedCount: 0, income: "0", expense: "0" }, [{ id: "x" }]), "normal", "a review-only row is not an empty household");
  assert.equal(homeState({ confirmedCount: 3, income: "0", expense: "250000" }, [{ id: "x" }]), "expenses-only");
  assert.equal(homeState({ confirmedCount: 3, income: "10000000", expense: "250000" }, [{ id: "x" }]), "normal");
  assert.equal(homeState(null, []), "normal", "no overview yet never claims an empty household");
});

test("the comparison line follows the server delta and names the previous period length", () => {
  assert.equal(comparisonLine({ expenseDelta: null, previousExpense: null }), null);
  const lower = comparisonLine({ periodKind: "CURRENT_CYCLE", previousExpense: "900000", previousDays: 32, expenseDelta: "-700000" });
  assert.deepEqual([lower.direction, lower.amount], ["down", "700000"]);
  assert.match(lower.text, /lebih rendah dari siklus lalu \(32 hari\)/);
  const higher = comparisonLine({ periodKind: "CALENDAR_MONTH", previousExpense: "100", previousDays: 30, expenseDelta: "50" });
  assert.deepEqual([higher.direction, higher.amount], ["up", "50"]);
  assert.match(higher.text, /lebih tinggi dari bulan lalu/);
  assert.equal(comparisonLine({ previousExpense: "5", expenseDelta: "0" }).direction, "same");
});

test("spending share is clamped and zero without income", () => {
  assert.equal(spendingShare({ income: "10000000", expense: "2500000" }), 25);
  assert.equal(spendingShare({ income: "100", expense: "500" }), 100);
  assert.equal(spendingShare({ income: "0", expense: "500" }), 0);
});

test("ranked categories keep the top six and fold the rest into Kategori lain", () => {
  const items = Array.from({ length: 8 }, (_, index) => ({ id: `c${index}`, name: `K${index}`, amount: String((8 - index) * 100) }));
  const rows = rankedCategories(items, 6);
  assert.equal(rows.length, 7);
  assert.deepEqual([rows[6].name, rows[6].amount, rows[6].other], ["Kategori lain", "300", true]);
  assert.ok(Math.abs(rows.reduce((sum, row) => sum + row.share, 0) - 1) < 0.001);
  assert.deepEqual(rankedCategories([{ id: "a", name: "A", amount: "0" }, { id: "b", name: "B", amount: "-5" }]), []);
});

test("drilldown links keep the active window and uncategorized spend", () => {
  assert.equal(dayLedgerHref("2026-11-20"), "/transactions?from=2026-11-20&to=2026-11-20");
  assert.equal(categoryLedgerHref("abc", { start: "2026-10-25", end: "2026-11-21" }), "/transactions?categoryId=abc&from=2026-10-25&to=2026-11-21");
  assert.equal(categoryLedgerHref(null, null), "/transactions?categoryId=uncategorized");
});

const tx = (id, at, type, status, amount) => ({ id, transactionAt: at, type, status, amount });

test("the ledger groups by Jakarta day and subtotals confirmed net spending", () => {
  const days = groupByDay([
    tx("a", "2026-11-20T23:30:00+07:00", "EXPENSE", "CONFIRMED", "30000"),
    tx("b", "2026-11-20T09:00:00+07:00", "REFUND", "CONFIRMED", "5000.00"),
    tx("c", "2026-11-20T08:00:00+07:00", "EXPENSE", "VOIDED", "999999"),
    tx("d", "2026-11-19T20:00:00+07:00", "EXPENSE", "NEEDS_REVIEW", "12000"),
    tx("e", "2026-11-19T08:00:00+07:00", "INCOME", "CONFIRMED", "9000000"),
  ]);
  assert.equal(days.length, 2);
  assert.equal(days[0].day, "2026-11-20");
  assert.equal(days[0].heading, "Jumat, 20 Nov");
  assert.equal(days[0].spend, "25000", "30000 expense - 5000 refund; the voided row never counts");
  assert.equal(days[1].spend, "0");
  assert.equal(days[1].reviewCount, 1);
});

test("a UTC late-evening timestamp lands on the next Jakarta day", () => {
  assert.equal(jakartaDay("2026-11-20T18:00:00Z"), "2026-11-21");
});

test("voided rows are hidden unless a status is chosen or ?showVoided=1 is on", () => {
  assert.equal(withVoidedPolicy("?limit=50"), "?limit=50&exclude_voided=true");
  assert.equal(withVoidedPolicy("?status=VOIDED&limit=50"), "?status=VOIDED&limit=50");
  assert.equal(withVoidedPolicy("?showVoided=1&limit=50"), "?limit=50", "the page switch is never sent to the API");
  assert.equal(hidesVoided("?q=x"), true);
  assert.equal(hidesVoided("?showVoided=1"), false);
  assert.equal(hidesVoided("?status=CONFIRMED"), false);
});

test("Periode presets resolve from dates alone and round-trip through the URL", () => {
  const ctx = { cycle: { start: "2026-10-25" }, today: "2026-11-21" };
  assert.deepEqual(periodRange("cycle", ctx), { from: "2026-10-25", to: "2026-11-21" });
  assert.deepEqual(periodRange("7d", ctx), { from: "2026-11-15", to: "2026-11-21" });
  assert.deepEqual(periodRange("30d", ctx), { from: "2026-10-23", to: "2026-11-21" });
  assert.deepEqual(periodRange("all", ctx), { from: "", to: "" });
  assert.deepEqual(periodRange("cycle", { cycle: null, today: "2026-11-21" }), { from: "", to: "" });
  assert.equal(periodValue("?from=2026-10-25&to=2026-11-21", ctx), "cycle");
  assert.equal(periodValue("?limit=50", ctx), "all");
  assert.equal(periodValue("?from=2026-01-01&to=2026-01-31", ctx), "custom");
});

test("Asal-usul & bukti reads evidence and creation events oldest first; the rest is change history", () => {
  const evidence = [{ id: "e1", evidenceType: "BANK_EMAIL", receivedAt: "2026-11-20T10:00:00+07:00" }];
  const audit = [
    { id: "a2", action: "EDIT_TRANSACTION", actorType: "USER", createdAt: "2026-11-21T08:00:00+07:00" },
    { id: "a1", action: "CREATE_FROM_BANK_EMAIL", actorType: "WORKER", createdAt: "2026-11-20T10:00:05+07:00" },
  ];
  const { origin, changes } = originTimeline(evidence, audit);
  assert.deepEqual(origin.map(item => item.key), ["e-e1", "a-a1"]);
  assert.equal(origin[0].title, "Email bank");
  assert.deepEqual(changes.map(item => item.key), ["a-a2"]);
  for (const item of [...origin, ...changes]) assert.doesNotMatch(`${item.title} ${item.detail}`, /[A-Z]{3,}_[A-Z]{3,}/, "no raw enum is shown");
});

test("Beranda hero numbers stay in ink, desktop wealth aligns to content, phone hides daily chart and wealth card", () => {
  const home = readFileSync(new URL("../app/page.js", import.meta.url), "utf8");
  assert.doesNotMatch(home, /home-hero-amount (?:expense|\$\{amountTone)/, "Sisa/Keluar hero numbers stay in ink");
  assert.doesNotMatch(home, /<dd className="(?:income|expense)">/, "hero figures stay in ink");
  const dashCss = readFileSync(new URL("../app/styles/03-dashboard-and-charts.css", import.meta.url), "utf8");
  assert.match(dashCss, /\.overview-lower \{[^}]*align-items: start;/, "desktop Kekayaan card hugs its content height");
  assert.doesNotMatch(dashCss, /\.overview-wealth-link \{[^}]*margin-top: auto;/, "no empty space pushed above the wealth link");
  const dailyChartRule = dashCss.match(/\.daily-spend \.chart-wrap \{[^}]*\}/)?.[0] ?? "";
  assert.match(dailyChartRule, /flex: 1 1 240px;/, "desktop Pengeluaran harian chart grows to fill card height");
  assert.doesNotMatch(dailyChartRule, /(?:^|[;\s{])height:\s*240px;/, "Pengeluaran harian chart is not locked to a fixed 240px height");
  const respCss = readFileSync(new URL("../app/styles/11-responsive.css", import.meta.url), "utf8");
  assert.match(respCss, /@media \(max-width: 680px\)[\s\S]*\.overview-trend, \.overview-wealth, \.home-lower-single \{ display: none; \}/, "phone Beranda hides daily chart and Kekayaan card");
});
