import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { chromium } from "playwright";

// A one-shot motion marker must survive exactly the render that follows the
// user's action. This drives the real static export: a filter must fade the
// ledger, and the pagination that follows must not; a save must mark the
// figures it moved, and a later retry must not replay that flash. Regression
// for issue #406.
const port = "3212";
const base = `http://127.0.0.1:${port}`;
const output = new URL("../test-results/motion-one-shot/", import.meta.url);
const server = spawn(process.execPath, ["scripts/static-server.mjs"], { stdio: "inherit", env: { ...process.env, PORT: port } });

const user = { id: "user-1", displayName: "Rafi", email: "rafi@example.test", isSuperAdmin: false, householdName: "Rumah Rafi", household: { role: "OWNER" } };
const transaction = (id, merchant) => ({ id, type: "EXPENSE", status: "CONFIRMED", amount: "185000", merchantName: merchant, categoryName: "Belanja rumah", memberName: "Rafi", sourceType: "BANK_EMAIL", transactionAt: "2026-09-06T09:20:00+07:00", accountName: "Jago utama" });
const accounts = [
  { id: "asset-1", name: "Tabungan", institution: "Bank", wealthType: "BANK", usageRole: "SPENDING", active: true },
  { id: "liability-1", name: "Kartu kredit", institution: "Bank", wealthType: "CREDIT_CARD", usageRole: "SPENDING", active: true },
];
const snapshot = (id, value) => ({ id, netWorthIdr: value, assetTotalIdr: value, liabilityTotalIdr: "0", observedAt: "2026-11-03T10:00:00+07:00", items: [{ wealthAccountId: "asset-1", valueIdr: value }, { wealthAccountId: "liability-1", valueIdr: "0" }] });
let latest = snapshot("wealth-1", "1000000");
let saves = 0;

async function api(route) {
  const url = new URL(route.request().url());
  const path = url.pathname;
  const json = (body, headers = {}) => route.fulfill({ contentType: "application/json", headers, body: JSON.stringify(body) });
  if (path === "/api/v1/auth/me") return json(user);
  if (path === "/api/v1/transactions") {
    const second = url.searchParams.get("cursor") === "cursor-2";
    return json([second ? transaction("tx-3", "Grab") : transaction("tx-1", "Pasar Minggu")], second ? {} : { "X-Next-Cursor": "cursor-2" });
  }
  if (path === "/api/v1/categories") return json([{ id: "cat-1", name: "Belanja rumah", active: true }]);
  if (path === "/api/v1/accounts") return json([{ id: "account-1", name: "Jago utama", accountType: "BANK", trackingPolicy: "SPENDING_ONLY", active: true }]);
  if (path === "/api/v1/wealth/accounts") return json(accounts);
  if (path === "/api/v1/wealth/summary") return json({ latest });
  if (path === "/api/v1/wealth/snapshots/latest") return json(latest);
  if (path === "/api/v1/wealth/history") return json([latest]);
  if (path === "/api/v1/wealth/current-cycle-savings") return json({ periodKind: "CURRENT_CYCLE", savingsAllocated: "0", periodStart: "2026-11-01", periodEnd: "2026-11-30", savingsByDestination: [] });
  if (path === "/api/v1/wealth/cycle-recaps") return json([]);
  if (path === "/api/v1/wealth/observations") return json([]);
  if (path === "/api/v1/wealth/snapshots" && route.request().method() === "POST") {
    saves += 1;
    // The second save is the "retry" path: it fails, the user retries the load.
    if (saves > 1) return route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "synthetic failure" }) });
    latest = snapshot("wealth-2", "1500000");
    return json({});
  }
  if (path === "/api/v1/household/members") return json([{ id: "member-1", displayName: "Rafi", role: "OWNER", active: true }]);
  if (path === "/api/v1/reviews") return json([]);
  return json([]);
}

// Marker present and the CSS animation that reads it is running.
async function animated(page, selector, attribute, animation) {
  await page.waitForFunction(([selector, attribute, animation]) => {
    const element = document.querySelector(selector);
    return Boolean(element) && element.hasAttribute(attribute) && getComputedStyle(element).animationName.includes(animation);
  }, [selector, attribute, animation], { timeout: 3000 });
}

// Marker gone from the element after the unrelated re-render. A timeout is a
// failure, not a pass: the assertion below reports why.
async function settled(page, selector, attribute) {
  return page.waitForFunction(([selector, attribute]) => {
    const element = document.querySelector(selector);
    return Boolean(element) && !element.hasAttribute(attribute);
  }, [selector, attribute], { timeout: 1500 }).then(() => true, () => false);
}

const problems = [];
let browser;

try {
  await mkdir(output, { recursive: true });
  let ready = false;
  for (let attempt = 0; attempt < 80 && !ready; attempt++) {
    try { ready = (await fetch(base)).ok; } catch {}
    if (!ready) await new Promise(resolve => setTimeout(resolve, 250));
  }
  assert.ok(ready, "static export server started");
  browser = await chromium.launch();

  for (const [label, width] of [["desktop", 1440], ["mobile", 390]]) {
    const context = await browser.newContext({ viewport: { width, height: 900 }, locale: "id-ID", timezoneId: "Asia/Jakarta", recordVideo: { dir: output.pathname } });
    const page = await context.newPage();
    await page.route("**/api/v1/**", api);
    saves = 0;
    latest = snapshot("wealth-1", "1000000");

    await page.goto(`${base}/transactions`, { waitUntil: "networkidle" });
    const panel = ".ledger-panel:not([data-refreshed])";
    await page.waitForSelector(panel);

    // The filter re-run is the user's action: it fades once.
    await page.getByRole("button", { name: "Terapkan filter", exact: true }).click();
    await animated(page, ".ledger-panel", "data-refreshed", "list-in");
    await page.screenshot({ path: new URL(`${label}-transactions-filter.png`, output).pathname, fullPage: false });

    // Pagination is not a refresh: the same panel must not fade again.
    await page.getByRole("button", { name: "Berikutnya", exact: true }).click();
    if (!await settled(page, ".ledger-panel", "data-refreshed")) problems.push(`${label}: pagination replayed the filter fade`);
    await page.screenshot({ path: new URL(`${label}-transactions-next-page.png`, output).pathname, fullPage: false });
    console.log(`transactions ${label}: filter faded; pagination checked`);

    await page.goto(`${base}/wealth`, { waitUntil: "networkidle" });
    await page.waitForSelector(".wealth-position-primary > strong:not([data-updated])");

    // The save is the user's action: only the figure it moved flashes.
    await page.getByRole("button", { name: "Perbarui nilai", exact: true }).click();
    await page.locator('input[name="value-asset-1"]').fill("1500000");
    await page.getByRole("button", { name: "Simpan catatan lengkap", exact: true }).click();
    await animated(page, ".wealth-position-primary > strong", "data-updated", "value-updated");
    await page.screenshot({ path: new URL(`${label}-wealth-saved.png`, output).pathname, fullPage: false });

    // A later retry remounts the figures; it must not replay the earlier save.
    await page.getByRole("button", { name: "Perbarui nilai", exact: true }).click();
    await page.getByRole("button", { name: "Simpan catatan lengkap", exact: true }).click();
    await page.locator(".notice.error").waitFor();
    await page.getByRole("button", { name: "Coba lagi", exact: true }).click();
    if (!await settled(page, ".wealth-position-primary > strong", "data-updated")) problems.push(`${label}: a retry replayed the save flash`);
    await page.screenshot({ path: new URL(`${label}-wealth-retry.png`, output).pathname, fullPage: false });
    console.log(`wealth ${label}: save flashed; retry checked`);

    const video = page.video();
    await context.close();
    await video?.saveAs(new URL(`${label}-motion-one-shot.webm`, output).pathname);
  }

  assert.deepEqual(problems, [], `motion replayed on re-renders the user did not cause:\n${problems.join("\n")}`);
  console.log(`PASS motion one-shot: ${output.pathname}`);
} finally {
  await browser?.close();
  server.kill();
}
