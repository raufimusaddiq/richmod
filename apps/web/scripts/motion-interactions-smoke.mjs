import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { chromium } from "playwright";

const port = "3231";
const base = `http://127.0.0.1:${port}`;
const server = spawn(process.execPath, ["scripts/static-server.mjs"], { stdio: "inherit", env: { ...process.env, PORT: port } });

const user = { id: "user-1", displayName: "Rafi", email: "rafi@example.test", isSuperAdmin: false, householdName: "Rumah Rafi", household: { role: "OWNER" } };
const transaction = (id, merchant, day) => ({ id, type: "EXPENSE", status: "CONFIRMED", amount: "185000", merchantName: merchant, categoryName: "Belanja rumah", memberName: "Rafi", sourceType: "BANK_EMAIL", transactionAt: `2026-09-${day}T09:20:00+07:00`, accountName: "Jago utama" });
const firstPage = ["Pasar", "Grab", "Kopi", "Listrik", "Air", "Gym", "Buku", "Bensin", "Tol", "Parkir"].map((name, index) => transaction(`tx-${index}`, name, String(10 - Math.floor(index / 3)).padStart(2, "0")));
const probeMarkup = '<details id="probe"><summary>Probe</summary><div style="height:200px">isi</div></details>';

async function api(route) {
  const url = new URL(route.request().url());
  const path = url.pathname;
  const json = (body, headers = {}) => route.fulfill({ contentType: "application/json", headers, body: JSON.stringify(body) });
  if (path === "/api/v1/auth/me") return json(user);
  if (path === "/api/v1/transactions") {
    const second = url.searchParams.get("cursor") === "cursor-2";
    return json(second ? [transaction("tx-90", "Halaman dua", "01"), transaction("tx-91", "Halaman dua b", "01")] : firstPage, second ? {} : { "X-Next-Cursor": "cursor-2" });
  }
  if (path === "/api/v1/categories") return json([{ id: "cat-1", name: "Belanja rumah", active: true }]);
  if (path === "/api/v1/accounts") return json([{ id: "account-1", name: "Jago utama", accountType: "BANK", trackingPolicy: "SPENDING_ONLY", active: true }]);
  if (path === "/api/v1/household/members") return json([{ id: "member-1", displayName: "Rafi", role: "OWNER", active: true }]);
  return json([]);
}

const sample = (page, reader, steps) => page.evaluate(async ([source, times]) => {
  const read = new Function(`return (${source})()`);
  const out = [];
  let last = 0;
  for (const at of times) {
    await new Promise(resolve => setTimeout(resolve, at - last));
    last = at;
    out.push(read());
  }
  return out;
}, [reader.toString(), steps]);

const problems = [];
let browser;

async function open(context, path, ready) {
  const page = await context.newPage();
  await page.route("**/api/v1/**", api);
  await page.goto(`${base}${path}`, { waitUntil: "networkidle" });
  await page.waitForSelector(ready);
  return page;
}

const filterHeight = () => document.querySelector(".filter-more").getBoundingClientRect().height;
const probeHeight = () => document.querySelector("#probe").getBoundingClientRect().height;

try {
  let up = false;
  for (let attempt = 0; attempt < 80 && !up; attempt++) {
    try { up = (await fetch(base)).ok; } catch {}
    if (!up) await new Promise(resolve => setTimeout(resolve, 250));
  }
  assert.ok(up, "static export server started");
  browser = await chromium.launch();

  const motion = await browser.newContext({ viewport: { width: 1280, height: 900 }, locale: "id-ID", timezoneId: "Asia/Jakarta" });
  const page = await motion.newPage();
  await page.route("**/api/v1/**", api);
  await page.addInitScript(() => {
    window.__stagger = 0;
    new MutationObserver(records => {
      for (const record of records) if (record.target.hasAttribute("data-stagger-item")) window.__stagger++;
    }).observe(document, { subtree: true, attributes: true, attributeFilter: ["data-stagger-item"] });
  });
  await page.goto(`${base}/transactions`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector(".transaction-row");
  const first = await page.evaluate(() => ({ marked: window.__stagger, rows: document.querySelectorAll(".transaction-row").length, delays: [...document.querySelectorAll("[data-stagger-item]")].map(row => getComputedStyle(row).animationDelay) }));
  const delays = first.delays.map(value => Number.parseFloat(value) * (value.endsWith("ms") ? 1 : 1000));
  if (first.marked !== first.rows) problems.push(`first load: ${first.marked} of ${first.rows} rows were staggered`);
  if (Math.max(...delays) > 400) problems.push(`stagger waits ${Math.max(...delays)}ms`);
  if (!(delays[1] > delays[0] && delays[3] > delays[2])) problems.push(`rows are not staggered apart: ${delays.join(",")}`);
  await page.waitForFunction(() => !document.querySelector("[data-stagger-item]"), null, { timeout: 3000 });
  await page.evaluate(() => { window.__stagger = 0; });
  await page.getByRole("button", { name: "Berikutnya", exact: true }).click();
  await page.waitForSelector(".transaction-row:has-text('Halaman dua')");
  await page.waitForTimeout(700);
  const again = await page.evaluate(() => window.__stagger);
  if (again !== 0) problems.push(`pagination staggered ${again} rows`);
  console.log(`stagger: first load ${first.marked} rows, max delay ${Math.max(...delays)}ms, pagination ${again}`);

  const wasHidden = await page.evaluate(() => document.querySelector(".filter-more").hidden);
  await page.locator(".filter-toggle").click();
  const heights = await sample(page, filterHeight, [60, 160, 700]);
  if (!wasHidden || !(heights[0] < heights[2]) || heights[2] < 40 || heights[1] <= 0) problems.push(`filter panel did not animate open: ${heights.join(",")}`);
  await page.locator(".filter-toggle").click();
  const closing = await sample(page, filterHeight, [120, 700]);
  if (!(closing[0] > 0 && closing[0] < heights[2]) || closing[1] !== 0) problems.push(`filter panel did not animate closed: ${closing.join(",")}`);
  console.log(`filter panel: open ${heights.map(Math.round)} closed ${closing.map(Math.round)}`);

  const shut = await page.evaluate(markup => {
    document.querySelector("main").insertAdjacentHTML("beforeend", markup);
    return document.querySelector("#probe").getBoundingClientRect().height;
  }, probeMarkup);
  await page.locator("#probe summary").click();
  const opening = await sample(page, probeHeight, [100, 700]);
  if (!(opening[0] > shut && opening[0] < opening[1] - 10)) problems.push(`details did not animate open: ${shut},${opening.join(",")}`);
  await page.locator("#probe summary").click();
  const shutting = await sample(page, probeHeight, [100, 700]);
  if (!(shutting[0] < opening[1] - 10 && shutting[0] > shut && Math.abs(shutting[1] - shut) < 2)) problems.push(`details did not animate closed: ${shutting.join(",")}`);
  console.log(`details: closed ${Math.round(shut)} opening ${opening.map(Math.round)} closing ${shutting.map(Math.round)}`);

  const inbox = await open(motion, "/inbox", ".inbox-tabs[data-slide-ready]");
  const startX = await inbox.evaluate(() => Number.parseFloat(document.querySelector(".inbox-tabs").style.getPropertyValue("--slide-x")));
  await inbox.locator(".inbox-tabs > button").nth(1).click();
  const slide = await sample(inbox, () => {
    const container = document.querySelector(".inbox-tabs");
    const panel = document.querySelector("#inbox-panel");
    return { x: new DOMMatrix(getComputedStyle(container, "::before").transform).m41, goal: container.querySelector('[aria-selected="true"]').offsetLeft, entering: panel.hasAttribute("data-panel-enter"), opacity: getComputedStyle(panel).opacity };
  }, [80, 700]);
  if (!(slide[0].x > startX && slide[0].x < slide[0].goal - 2)) problems.push(`pill jumped: start ${startX}, at 80ms ${slide[0].x}, goal ${slide[0].goal}`);
  if (Math.abs(slide[1].x - slide[1].goal) > 1) problems.push(`pill did not land: ${slide[1].x} vs ${slide[1].goal}`);
  const fit = await inbox.evaluate(() => {
    const container = document.querySelector(".inbox-tabs");
    const pill = getComputedStyle(container, "::before");
    const box = container.querySelector('[aria-selected="true"]').getBoundingClientRect();
    return Math.abs(Number.parseFloat(pill.width) - box.width) + Math.abs(Number.parseFloat(pill.height) - box.height);
  });
  if (fit > 0.5) problems.push(`pill box differs from the active button by ${fit}px`);
  if (!slide[0].entering ||Number(slide[0].opacity) >= 1) problems.push("panel did not re-enter on tab change");
  if (slide[1].entering) problems.push("panel marker was not consumed");
  const roles = await inbox.evaluate(() => [...document.querySelectorAll(".inbox-tabs > button")].map(button => `${button.getAttribute("role")}:${button.getAttribute("aria-selected")}:${button.tabIndex}`));
  if (roles.join() !== "tab:false:-1,tab:true:0") problems.push(`tab semantics changed: ${roles}`);
  console.log(`pill: start ${startX} mid ${Math.round(slide[0].x)} end ${Math.round(slide[1].x)} goal ${slide[1].goal}`);
  await motion.close();

  const reduced = await browser.newContext({ viewport: { width: 1280, height: 900 }, locale: "id-ID", timezoneId: "Asia/Jakarta", reducedMotion: "reduce" });
  const still = await open(reduced, "/inbox", ".inbox-tabs[data-slide-ready]");
  await still.locator(".inbox-tabs > button").nth(1).click();
  const calm = await sample(still, () => {
    const container = document.querySelector(".inbox-tabs");
    return { x: new DOMMatrix(getComputedStyle(container, "::before").transform).m41, goal: container.querySelector('[aria-selected="true"]').offsetLeft };
  }, [40]);
  if (Math.abs(calm[0].x - calm[0].goal) > 1) problems.push(`reduced motion still slides: ${calm[0].x} vs ${calm[0].goal}`);
  await still.goto(`${base}/transactions`, { waitUntil: "networkidle" });
  await still.waitForSelector(".transaction-row");
  const reducedRows = await still.evaluate(() => [...document.querySelectorAll(".transaction-row")].filter(row => row.getAnimations().length).length);
  if (reducedRows) problems.push(`reduced motion animated ${reducedRows} rows`);
  await still.locator(".filter-toggle").click();
  const instant = await sample(still, filterHeight, [30]);
  if (instant[0] < 40) problems.push(`reduced motion delayed the filter panel: ${instant[0]}`);
  console.log("reduced motion: instant");

  assert.deepEqual(problems, [], `motion interactions failed:\n${problems.join("\n")}`);
  console.log("PASS motion interactions");
} finally {
  await browser?.close();
  server.kill();
}
