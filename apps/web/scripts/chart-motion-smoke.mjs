import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { chromium } from "playwright";
import { cycleFacts } from "../tests/fixtures/cycle-review.mjs";

const port = "3247";
const base = `http://127.0.0.1:${port}`;
const server = spawn(process.execPath, ["scripts/static-server.mjs"], { stdio: "inherit", env: { ...process.env, PORT: port } });

const user = { id: "user-1", displayName: "Rafi", email: "rafi@example.test", isSuperAdmin: false, householdName: "Rumah Rafi", household: { role: "OWNER" } };
const HERO = "6796525";
const heroText = new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(BigInt(HERO));
const daily = Array.from({ length: 12 }, (_, index) => ({ period: `2026-11-${String(index + 1).padStart(2, "0")}`, expense: String(150000 + ((index * 97000) % 600000)), refund: "0" }));
const categories = [["c1", "Belanja rumah", "2100000"], ["c2", "Makan di luar", "1400000"], ["c3", "Transportasi", "700000"], ["c4", "Hiburan", "300000"]].map(([id, name, amount]) => ({ id, name, amount, share: Number(amount) / 4500000 }));
const months = range => Array.from({ length: Number(range) }, (_, index) => ({ period: `2026-${String((index % 12) + 1).padStart(2, "0")}`, income: "12000000", expense: String(3000000 + index * 400000), refund: "0", netCashflow: String(9000000 - index * 400000) }));
const snapshots = Array.from({ length: 4 }, (_, index) => ({ id: `s${index}`, netWorthIdr: String(10000000 + index * 2500000), assetTotalIdr: String(12000000 + index * 2500000), liabilityTotalIdr: "2000000", observedAt: `2026-${String(8 + index).padStart(2, "0")}-03T10:00:00+07:00`, items: [{ wealthAccountId: "asset-1", valueIdr: "12000000", side: "ASSET" }] })).reverse();

async function api(route) {
  const url = new URL(route.request().url());
  const path = url.pathname;
  const json = (body, headers = {}) => route.fulfill({ contentType: "application/json", headers, body: JSON.stringify(body) });
  if (path === "/api/v1/auth/me") return json(user);
  if (path === "/api/v1/analytics/overview") return json({ periodKind: "CURRENT_CYCLE", income: "12000000", expense: "4500000", netCashflow: HERO, savingsAllocated: "1000000", unallocatedSurplus: "5796525", confirmedCount: 12, reviewCount: 0 });
  if (path === "/api/v1/analytics/cycle/daily") return json({ daily, daysElapsed: daily.length });
  if (path === "/api/v1/analytics/categories" && !url.searchParams.has("range")) return json(categories, { "X-Window-Start": "2026-11-01", "X-Window-End": "2026-11-30" });
  if (path === "/api/v1/analytics/categories") return json(categories.map(item => ({ ...item, amountValue: Number(item.amount) })));
  if (path === "/api/v1/analytics/cashflow") return json(months(url.searchParams.get("range") || "6"));
  if (path === "/api/v1/analytics/cycle") return json({ kind: "CURRENT_CYCLE", start: "2026-11-01", end: "2026-11-30" });
  if (path === "/api/v1/analytics/cycle-review") return json(cycleFacts(url.searchParams.get("cycle_start") || undefined));
  if (path === "/api/v1/analytics/cycle-decisions") return json({ items: [], previous: [], previousCycleStart: "2026-08-26" });
  if (path === "/api/v1/analytics/merchants" || path === "/api/v1/analytics/members") return json([]);
  if (path === "/api/v1/transactions") return json([]);
  if (path === "/api/v1/wealth/snapshots/latest" || path === "/api/v1/wealth/summary") return json(path.endsWith("summary") ? { latest: snapshots[0] } : snapshots[0]);
  if (path === "/api/v1/wealth/accounts") return json([{ id: "asset-1", name: "Tabungan", institution: "Bank", wealthType: "BANK", usageRole: "SPENDING", active: true }]);
  if (path === "/api/v1/wealth/history") return json(snapshots);
  if (path === "/api/v1/wealth/current-cycle-savings") return json({ periodKind: "CURRENT_CYCLE", savingsAllocated: "0", periodStart: "2026-11-01", periodEnd: "2026-11-30", savingsByDestination: [] });
  return json([]);
}

const sampler = () => {
  const probes = {
    barCount: () => document.querySelectorAll(".recharts-bar-rectangle path, .recharts-bar-rectangle rect:not([fill=\"transparent\"])").length,
    barHeight: () => [...document.querySelectorAll(".recharts-bar-rectangle path, .recharts-bar-rectangle rect:not([fill=\"transparent\"])")].reduce((sum, node) => sum + node.getBoundingClientRect().height, 0),
    lineDash: () => document.querySelector(".recharts-line-curve")?.getAttribute("stroke-dasharray") ?? "",
    cssClip: () => [...document.querySelectorAll(".home-share > span, .category-bars-track > span, .where-stack")].map(node => getComputedStyle(node).clipPath).join("|"),
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
const settleMs = 1600;

async function visit(browser, reducedMotion, path, ready) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "id-ID", timezoneId: "Asia/Jakarta", reducedMotion: reducedMotion ? "reduce" : "no-preference" });
  const page = await context.newPage();
  await page.addInitScript(sampler);
  await page.route("**/api/v1/**", api);
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.goto(`${base}${path}`, { waitUntil: "networkidle" });
  try { await page.waitForSelector(ready, { timeout: 8000 }); } catch (error) { console.log(errors, (await page.locator("body").innerText()).slice(0, 600)); throw error; }
  return { page, context, errors };
}

let browser;
try {
  let ready = false;
  for (let attempt = 0; attempt < 80 && !ready; attempt++) {
    try { ready = (await fetch(base)).ok; } catch {}
    if (!ready) await new Promise(resolve => setTimeout(resolve, 250));
  }
  assert.ok(ready, "static export server started");
  browser = await chromium.launch();

  for (const reduced of [false, true]) {
    const label = reduced ? "reduced" : "full";

    const home = await visit(browser, reduced, "/", ".home-hero-amount");
    await home.page.waitForSelector(".recharts-bar-rectangle");
    await home.page.waitForTimeout(settleMs);
    const homeRows = (await rows(home.page)).filter(row => row.hero);
    const finalBars = homeRows.at(-1).barHeight;
    const drawn = homeRows.filter(row => row.barCount > 0);
    assert.ok(finalBars > 40, `${label}: daily bars drew`);
    assert.equal(homeRows.at(-1).heroFinal, heroText, `${label}: hero ends on the exact formatted value`);
    assert.equal(homeRows.at(-1).hero, heroText, `${label}: no stale animated text remains`);
    if (reduced) {
      assert.ok(drawn.every(row => row.barHeight === finalBars), "reduced: bars are at their final height from the first render");
      assert.deepEqual([...distinct(homeRows, "hero")], [heroText], "reduced: the hero never shows an intermediate value");
      assert.ok(!(await animationNames(home.page)).some(name => name.startsWith("bar-grow")), "reduced: no css bar animation starts");
    } else {
      assert.ok(drawn[0].barHeight < finalBars * 0.5, `full: bars mount small (${drawn[0].barHeight} vs ${finalBars})`);
      assert.ok(distinct(homeRows, "hero").size >= 2, "full: the hero starts below its final value");
      assert.ok((await animationNames(home.page)).includes("bar-grow-x"), "full: the css bars run their grow animation");
      if (!starved(homeRows)) {
        assert.ok(drawn.some(row => row.barHeight > drawn[0].barHeight && row.barHeight < finalBars), "full: bars pass through intermediate heights");
        assert.ok(distinct(homeRows, "hero").size >= 4, "full: the hero counts through intermediate values");
      }
    }
    const before = (await rows(home.page)).length;
    await home.page.locator(".recharts-bar-rectangle").nth(3).hover();
    await home.page.mouse.move(400, 500);
    await home.page.mouse.move(420, 520);
    await home.page.waitForTimeout(500);
    const after = (await rows(home.page)).slice(before);
    assert.ok(after.every(row => row.barHeight === finalBars), `${label}: hovering a bar never replays the draw-in`);
    assert.ok(after.every(row => row.hero === heroText), `${label}: hovering never replays the counter`);
    assert.deepEqual(home.errors, [], `${label}: no page errors on home`);
    console.log(`home ${label}: ok`);
    await home.context.close();

    const wealth = await visit(browser, reduced, "/wealth", ".wealth-position-primary > strong");
    await wealth.page.waitForSelector(".recharts-line-curve");
    await wealth.page.waitForTimeout(settleMs);
    const wealthRows = (await rows(wealth.page)).filter(row => row.hero);
    assert.equal(wealthRows.at(-1).hero, wealthRows.at(-1).heroFinal, `${label}: wealth ends on the exact value`);
    const dashes = wealthRows.map(row => row.lineDash).filter(Boolean);
    if (reduced) assert.ok(dashes.every(value => value === dashes.at(-1)), "reduced: the net-worth line is never mid-draw");
    else {
      assert.notEqual(dashes[0], dashes.at(-1), "full: the net-worth line mounts partly drawn");
      if (!starved(wealthRows)) assert.ok(new Set(dashes).size > 2, "full: the net-worth line draws through intermediate dash lengths");
    }
    assert.deepEqual(wealth.errors, [], `${label}: no page errors on wealth`);
    console.log(`wealth ${label}: ok`);
    await wealth.context.close();

    const cycle = await visit(browser, reduced, "/analytics?view=cycle&cycle=2026-09-01", ".kpi-cell[data-lead] dd");
    await cycle.page.waitForSelector(".history-bar");
    await cycle.page.waitForTimeout(settleMs);
    const cycleRows = (await rows(cycle.page)).filter(row => row.hero);
    assert.equal(cycleRows.at(-1).hero, cycleRows.at(-1).heroFinal, `${label}: the KPI strip ends on the exact value`);
    const history = cycleRows.map(row => row.historyHeight).filter(value => value > 0);
    const finalHistory = history.at(-1);
    const cycleAnimations = await animationNames(cycle.page);
    if (reduced) {
      assert.ok(history.every(value => Math.abs(value - finalHistory) < 0.5), "reduced: history bars are at full height immediately");
      assert.ok(!cycleAnimations.some(name => name.startsWith("bar-grow")), "reduced: no css bar animation starts");
    } else {
      assert.ok(cycleAnimations.includes("bar-grow-y"), "full: history bars run their grow animation");
      assert.ok(cycleAnimations.includes("bar-grow-x"), "full: the where-stack runs its grow animation");
    }
    const mark = (await rows(cycle.page)).length;
    await cycle.page.getByRole("heading", { name: "Pengeluaran harian", exact: true }).click();
    await cycle.page.waitForSelector(".cycle-spending-chart .recharts-bar-rectangle");
    await cycle.page.waitForTimeout(settleMs);
    const opened = (await rows(cycle.page)).slice(mark).filter(row => row.barCount > 0);
    assert.ok(opened.length > 3, "the daily pattern drew after opening");
    const barsFinal = opened.at(-1).barHeight;
    if (reduced) assert.ok(opened.every(row => row.barHeight === barsFinal), "reduced: the daily pattern is static");
    else assert.ok(opened[0].barHeight < barsFinal * 0.5, "full: the daily pattern mounts at the baseline when opened");
    assert.deepEqual(cycle.errors, [], `${label}: no page errors on analytics`);
    console.log(`analytics cycle ${label}: ok`);
    await cycle.context.close();

    const calendar = await visit(browser, reduced, "/analytics?view=calendar&range=6", ".recharts-bar-rectangle");
    await calendar.page.waitForTimeout(settleMs);
    const settled = (await rows(calendar.page)).at(-1).barHeight;
    const calendarMark = (await rows(calendar.page)).length;
    await calendar.page.getByRole("button", { name: "3 Bulan", exact: true }).click();
    await calendar.page.waitForTimeout(settleMs);
    const changed = (await rows(calendar.page)).slice(calendarMark);
    assert.ok(settled > 0 && changed.at(-1).barHeight > 0, `${label}: calendar bars rendered`);
    const finalRange = changed.at(-1).barHeight;
    if (!reduced && !starved(changed)) assert.ok(changed.filter(row => row.barHeight > 0 && row.barHeight !== finalRange).length >= 3, "full: switching the range animates the new bars");
    else {
      const landed = changed.findIndex(row => row.barHeight === finalRange);
      assert.ok(landed >= 0 && changed.slice(landed).every(row => row.barHeight === finalRange), "reduced: once the new bars are in place they never move");
    }
    assert.deepEqual(calendar.errors, [], `${label}: no page errors on calendar`);
    console.log(`analytics calendar ${label}: ok`);
    await calendar.context.close();
  }
  console.log("PASS chart motion");
} finally {
  await browser?.close();
  server.kill();
}
