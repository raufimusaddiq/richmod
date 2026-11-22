import { test, expect, reply } from "../support/fixtures.js";
import { idr } from "../support/data.js";
import { cycleFacts } from "../../tests/fixtures/cycle-review.mjs";

test.use({ viewport: { width: 1440, height: 1000 } });

const HERO = "6796525";
const heroText = idr(HERO);
const settleMs = 1600;

const daily = Array.from({ length: 12 }, (_, index) => ({ period: `2026-11-${String(index + 1).padStart(2, "0")}`, expense: String(150000 + ((index * 97000) % 600000)), refund: "0" }));
const categories = [["c1", "Belanja rumah", "2100000"], ["c2", "Makan di luar", "1400000"], ["c3", "Transportasi", "700000"], ["c4", "Hiburan", "300000"]].map(([id, name, amount]) => ({ id, name, amount, share: Number(amount) / 4500000 }));
const months = range => Array.from({ length: Number(range) }, (_, index) => ({ period: `2026-${String((index % 12) + 1).padStart(2, "0")}`, income: "12000000", expense: String(3000000 + index * 400000), refund: "0", netCashflow: String(9000000 - index * 400000) }));
const snapshots = () => Array.from({ length: 4 }, (_, index) => ({ id: `s${index}`, netWorthIdr: String(10000000 + index * 2500000), assetTotalIdr: String(12000000 + index * 2500000), liabilityTotalIdr: "2000000", observedAt: `2026-${String(8 + index).padStart(2, "0")}-03T10:00:00+07:00`, items: [{ wealthAccountId: "asset-1", valueIdr: "12000000", side: "ASSET" }] })).reverse();

function chartApi(api) {
  const history = snapshots();
  api.use(request => {
    const { path, query } = request;
    if (path === "/api/v1/analytics/overview") return { periodKind: "CURRENT_CYCLE", income: "12000000", expense: "4500000", netCashflow: HERO, savingsAllocated: "1000000", unallocatedSurplus: "5796525", confirmedCount: 12, reviewCount: 0 };
    if (path === "/api/v1/analytics/cycle/daily") return { daily, daysElapsed: daily.length };
    if (path === "/api/v1/analytics/categories" && !query.has("range")) return reply(categories, { headers: { "X-Window-Start": "2026-11-01", "X-Window-End": "2026-11-30" } });
    if (path === "/api/v1/analytics/categories") return categories.map(item => ({ ...item, amountValue: Number(item.amount) }));
    if (path === "/api/v1/analytics/cashflow") return months(query.get("range") || "6");
    if (path === "/api/v1/analytics/cycle") return { kind: "CURRENT_CYCLE", start: "2026-11-01", end: "2026-11-30" };
    if (path === "/api/v1/analytics/cycle-review") return cycleFacts(query.get("cycle_start") || undefined);
    if (path === "/api/v1/analytics/cycle-decisions") return { items: [], previous: [], previousCycleStart: "2026-08-26" };
    if (path === "/api/v1/wealth/snapshots/latest") return history[0];
    if (path === "/api/v1/wealth/summary") return { latest: history[0] };
    if (path === "/api/v1/wealth/accounts") return [{ id: "asset-1", name: "Tabungan", institution: "Bank", wealthType: "BANK", usageRole: "SPENDING", active: true }];
    if (path === "/api/v1/wealth/history") return history;
    if (path === "/api/v1/wealth/current-cycle-savings") return { periodKind: "CURRENT_CYCLE", savingsAllocated: "0", periodStart: "2026-11-01", periodEnd: "2026-11-30", savingsByDestination: [] };
    return undefined;
  });
}

const sampler = () => {
  const probes = {
    barCount: () => document.querySelectorAll(".recharts-bar-rectangle path, .recharts-bar-rectangle rect:not([fill=\"transparent\"])").length,
    barHeight: () => [...document.querySelectorAll(".recharts-bar-rectangle path, .recharts-bar-rectangle rect:not([fill=\"transparent\"])")].reduce((sum, node) => sum + node.getBoundingClientRect().height, 0),
    lineDash: () => document.querySelector(".recharts-line-curve")?.getAttribute("stroke-dasharray") ?? "",
    historyHeight: () => [...document.querySelectorAll(".history-bar")].reduce((sum, node) => sum + node.getBoundingClientRect().height, 0),
    hero: () => { const node = document.querySelector(".home-hero-amount, .kpi-cell[data-lead] dd, .wealth-position-primary > strong"); return node ? (node.querySelector(".count-up")?.textContent ?? node.textContent) : ""; },
    heroFinal: () => { const node = document.querySelector(".home-hero-amount, .kpi-cell[data-lead] dd, .wealth-position-primary > strong"); return node ? node.textContent : ""; },
  };
  window.__samples = [];
  window.__animations = [];
  document.addEventListener("animationstart", event => window.__animations.push(event.animationName), true);
  const sample = mutation => {
    const row = { t: performance.now(), mutation };
    for (const [name, probe] of Object.entries(probes)) row[name] = probe();
    window.__samples.push(row);
  };
  const tick = () => { sample(false); requestAnimationFrame(tick); };
  requestAnimationFrame(tick);
  new MutationObserver(() => sample(true)).observe(document, { childList: true, subtree: true });
};

const rows = page => page.evaluate(() => window.__samples);
const animationNames = page => page.evaluate(() => window.__animations);
const starved = list => {
  const frames = list.filter(row => !row.mutation);
  const start = list.find(row => row.barCount > 0 || row.hero)?.t ?? 0;
  const span = frames.filter(row => row.t >= start && row.t <= start + 1000);
  return span.length < 2 || span.some((row, index) => index > 0 && row.t - span[index - 1].t > 200);
};
const distinct = (list, key) => new Set(list.map(row => row[key]).filter(Boolean));

async function visit(page, api, path, ready) {
  chartApi(api);
  await page.addInitScript(sampler);
  await page.goto(path, { waitUntil: "networkidle" });
  await page.waitForSelector(ready, { timeout: 8000 });
}

for (const reduced of [false, true]) {
  const label = reduced ? "reduced" : "full";

  test.describe(label, () => {
    test.use({ reducedMotion: reduced ? "reduce" : "no-preference" });

    test("home: bars draw in and the hero counts to the exact value", async ({ page, api }) => {
      await visit(page, api, "/", ".home-hero-amount");
      await page.waitForSelector(".recharts-bar-rectangle");
      await page.waitForTimeout(settleMs);
      const homeRows = (await rows(page)).filter(row => row.hero);
      const finalBars = homeRows.at(-1).barHeight;
      const drawn = homeRows.filter(row => row.barCount > 0);
      expect(finalBars, "daily bars drew").toBeGreaterThan(40);
      expect(homeRows.at(-1).heroFinal, "hero ends on the exact formatted value").toBe(heroText);
      expect(homeRows.at(-1).hero, "no stale animated text remains").toBe(heroText);
      if (reduced) {
        expect(drawn.every(row => row.barHeight === finalBars), "bars are at their final height from the first render").toBe(true);
        expect([...distinct(homeRows, "hero")], "the hero never shows an intermediate value").toEqual([heroText]);
        expect((await animationNames(page)).some(name => name.startsWith("bar-grow")), "no css bar animation starts").toBe(false);
      } else {
        expect(drawn[0].barHeight, `bars mount small (${drawn[0].barHeight} vs ${finalBars})`).toBeLessThan(finalBars * 0.5);
        expect(distinct(homeRows, "hero").size, "the hero starts below its final value").toBeGreaterThanOrEqual(2);
        expect(await animationNames(page), "the css bars run their grow animation").toContain("bar-grow-x");
        if (!starved(homeRows)) {
          expect(drawn.some(row => row.barHeight > drawn[0].barHeight && row.barHeight < finalBars), "bars pass through intermediate heights").toBe(true);
          expect(distinct(homeRows, "hero").size, "the hero counts through intermediate values").toBeGreaterThanOrEqual(4);
        }
      }
      const before = (await rows(page)).length;
      await page.locator(".recharts-bar-rectangle").nth(3).hover();
      await page.mouse.move(400, 500);
      await page.mouse.move(420, 520);
      await page.waitForTimeout(500);
      const after = (await rows(page)).slice(before);
      expect(after.every(row => row.barHeight === finalBars), "hovering a bar never replays the draw-in").toBe(true);
      expect(after.every(row => row.hero === heroText), "hovering never replays the counter").toBe(true);
    });

    test("wealth: the net-worth line draws in and the value lands exactly", async ({ page, api }) => {
      await visit(page, api, "/wealth", ".wealth-position-primary > strong");
      await page.waitForSelector(".recharts-line-curve");
      await page.waitForTimeout(settleMs);
      const wealthRows = (await rows(page)).filter(row => row.hero);
      expect(wealthRows.at(-1).hero, "wealth ends on the exact value").toBe(wealthRows.at(-1).heroFinal);
      const dashes = wealthRows.map(row => row.lineDash).filter(Boolean);
      if (reduced) expect(dashes.every(value => value === dashes.at(-1)), "the net-worth line is never mid-draw").toBe(true);
      else {
        expect(dashes[0], "the net-worth line mounts partly drawn").not.toBe(dashes.at(-1));
        if (!starved(wealthRows)) expect(new Set(dashes).size, "the net-worth line draws through intermediate dash lengths").toBeGreaterThan(2);
      }
    });

    test("analytics cycle: KPI strip, history bars, and the daily pattern after opening", async ({ page, api }) => {
      await visit(page, api, "/analytics?view=cycle&cycle=2026-09-01", ".kpi-cell[data-lead] dd");
      await page.waitForSelector(".history-bar");
      await page.waitForTimeout(settleMs);
      const cycleRows = (await rows(page)).filter(row => row.hero);
      expect(cycleRows.at(-1).hero, "the KPI strip ends on the exact value").toBe(cycleRows.at(-1).heroFinal);
      const history = cycleRows.map(row => row.historyHeight).filter(value => value > 0);
      const finalHistory = history.at(-1);
      const cycleAnimations = await animationNames(page);
      if (reduced) {
        expect(history.every(value => Math.abs(value - finalHistory) < 0.5), "history bars are at full height immediately").toBe(true);
        expect(cycleAnimations.some(name => name.startsWith("bar-grow")), "no css bar animation starts").toBe(false);
      } else {
        expect(cycleAnimations, "history bars run their grow animation").toContain("bar-grow-y");
        expect(cycleAnimations, "the where-stack runs its grow animation").toContain("bar-grow-x");
      }
      const mark = (await rows(page)).length;
      await page.getByRole("heading", { name: "Pengeluaran harian", exact: true }).click();
      await page.waitForSelector(".cycle-spending-chart .recharts-bar-rectangle");
      await page.waitForTimeout(settleMs);
      const opened = (await rows(page)).slice(mark).filter(row => row.barCount > 0);
      expect(opened.length, "the daily pattern drew after opening").toBeGreaterThan(3);
      const barsFinal = opened.at(-1).barHeight;
      if (reduced) expect(opened.every(row => row.barHeight === barsFinal), "the daily pattern is static").toBe(true);
      else expect(opened[0].barHeight, "the daily pattern mounts at the baseline when opened").toBeLessThan(barsFinal * 0.5);
    });

    test("analytics calendar: switching the range animates the new bars", async ({ page, api }) => {
      await visit(page, api, "/analytics?view=calendar&range=6", ".recharts-bar-rectangle");
      await page.waitForTimeout(settleMs);
      const settled = (await rows(page)).at(-1).barHeight;
      const mark = (await rows(page)).length;
      await page.getByRole("button", { name: "3 Bulan", exact: true }).click();
      await page.waitForTimeout(settleMs);
      const changed = (await rows(page)).slice(mark);
      expect(settled > 0 && changed.at(-1).barHeight > 0, "calendar bars rendered").toBe(true);
      const finalRange = changed.at(-1).barHeight;
      if (!reduced && !starved(changed)) expect(changed.filter(row => row.barHeight > 0 && row.barHeight !== finalRange).length, "switching the range animates the new bars").toBeGreaterThanOrEqual(3);
      else {
        const landed = changed.findIndex(row => row.barHeight === finalRange);
        expect(landed >= 0 && changed.slice(landed).every(row => row.barHeight === finalRange), "once the new bars are in place they never move").toBe(true);
      }
    });
  });
}

test.describe("assistive technology", () => {
  test.use({ reducedMotion: "no-preference" });

  test("a counting hero is hidden from screen readers and its final text is always present", async ({ page, api }) => {
    chartApi(api);
    await page.goto("/", { waitUntil: "domcontentloaded" });
    const hero = page.locator(".home-hero-amount");
    await hero.waitFor();
    await expect(hero.locator("[aria-hidden='true']"), "the animated frames are hidden").toHaveCount(1);
    await expect(hero.locator(".visually-hidden"), "the exact value is always readable").toHaveText(heroText);
  });
});
