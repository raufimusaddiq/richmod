import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { chromium } from "playwright";
import { cycleFacts } from "../tests/fixtures/cycle-review.mjs";

const port = "3215";
const base = `http://127.0.0.1:${port}`;
const server = spawn(process.execPath, ["scripts/static-server.mjs"], { stdio: "inherit", env: { ...process.env, PORT: port } });

const user = { id: "user-1", displayName: "Rafi", email: "rafi@example.test", isSuperAdmin: false, householdName: "Rumah Rafi", household: { role: "OWNER" } };
const transaction = id => ({ id, type: "EXPENSE", status: "CONFIRMED", amount: "185000", merchantName: "Pasar Minggu", categoryName: "Belanja rumah", memberName: "Rafi", sourceType: "BANK_EMAIL", transactionAt: "2026-09-06T09:20:00+07:00", accountName: "Jago utama" });
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));

async function api(route) {
  const path = new URL(route.request().url()).pathname;
  const json = body => route.fulfill({ contentType: "application/json", body: JSON.stringify(body) });
  if (path === "/api/v1/auth/me") return json(user);
  if (path === "/api/v1/transactions") {
    await pause(400);
    return json([transaction("tx-1")]);
  }
  if (path.startsWith("/api/v1/transactions/tx-")) return json(path.endsWith("/evidence") || path.endsWith("/audit") ? [] : transaction("tx-1"));
  if (path === "/api/v1/analytics/cycle-review") return json(cycleFacts());
  if (path === "/api/v1/analytics/cycle-decisions") return json({ items: [], previous: [], previousCycleStart: "2026-08-26" });
  if (path === "/api/v1/categories") return json([{ id: "cat-1", name: "Belanja rumah", active: true }]);
  if (path === "/api/v1/accounts") return json([{ id: "account-1", name: "Jago utama", accountType: "BANK", trackingPolicy: "SPENDING_ONLY", active: true }]);
  if (path === "/api/v1/household/members") return json([{ id: "member-1", displayName: "Rafi", role: "OWNER", active: true }]);
  return json([]);
}

async function leavingGone(page) {
  await page.waitForFunction(() => !document.querySelector(".presence[data-presence='leaving']"), null, { timeout: 3000 });
}

let browser;
try {
  let ready = false;
  for (let attempt = 0; attempt < 80 && !ready; attempt++) {
    try { ready = (await fetch(base)).ok; } catch {}
    if (!ready) await pause(250);
  }
  assert.ok(ready, "static export server started");
  browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, locale: "id-ID", timezoneId: "Asia/Jakarta" });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.route("**/api/v1/**", api);

  await page.goto(`${base}/`, { waitUntil: "networkidle" });
  await page.evaluate(() => {
    const record = { calls: 0, animations: [] };
    window.__viewTransitions = record;
    const original = document.startViewTransition.bind(document);
    document.startViewTransition = (...args) => {
      record.calls += 1;
      const transition = original(...args);
      transition.ready.then(() => { record.animations.push(...document.getAnimations().map(animation => animation.animationName)); }, () => {});
      return transition;
    };
  });
  await page.click(".sidebar nav a[href='/transactions']");
  await page.waitForSelector(".ledger-panel");
  await pause(600);
  const swap = await page.evaluate(() => window.__viewTransitions);
  assert.ok(swap.calls >= 1, "a sidebar navigation starts a view transition");
  assert.ok(swap.animations.includes("page-enter"), `the entering page runs the page-swap animation, saw: ${swap.animations.join(",")}`);
  console.log("route: sidebar navigation runs a view transition");
  const nested = await page.evaluate(() => ({ tagged: document.querySelectorAll("[data-reveal]").length, compounded: [...document.querySelectorAll("[data-reveal]")].filter(node => node.parentElement.closest("[data-reveal]")).length }));
  assert.ok(nested.tagged > 0, "content that replaces the skeleton is revealed");
  assert.equal(nested.compounded, 0, "a tagged node never sits inside another tagged node");
  console.log("reveal: top-level content tagged once");

  const row = page.locator(".ledger-panel button").first();
  await row.click();
  await page.waitForSelector(".detail-drawer");
  await pause(500);
  await page.click(".detail-drawer .drawer-close");
  await pause(60);
  await row.click();
  await page.waitForSelector(".presence[data-presence='open'] .detail-drawer");
  await pause(450);
  const reopened = await page.evaluate(() => { const p = document.querySelector(".presence"); return { inert: p.inert, focusInside: p.contains(document.activeElement), count: document.querySelectorAll(".detail-drawer").length }; });
  assert.deepEqual(reopened, { inert: false, focusInside: true, count: 1 }, "a drawer reopened while leaving is live and focused");
  console.log("drawer: reopened during exit is live");

  const voidButton = page.locator(".detail-drawer button", { hasText: "Batalkan transaksi" });
  await voidButton.click();
  await page.waitForSelector("dialog[open]:not(.create-dialog)");
  await pause(450);
  await page.keyboard.press("Escape");
  await pause(60);
  await voidButton.click();
  await page.waitForSelector("dialog[open]:not(.create-dialog)", { timeout: 2000 });
  await pause(450);
  assert.equal(await page.locator("dialog[open]:not(.create-dialog)").count(), 1, "a second confirm raised during the exit opens");
  await page.keyboard.press("Escape");
  await leavingGone(page);
  assert.ok(await page.locator(".detail-drawer").count(), "Escape in the confirm leaves the drawer open");
  console.log("confirm: second request during exit opens");

  await page.click(".detail-drawer .drawer-close");
  const exitFocus = async () => page.evaluate(() => ({ leaving: Boolean(document.querySelector(".presence[data-presence='leaving']")), body: document.activeElement === document.body, inDrawer: Boolean(document.activeElement?.closest(".presence")), row: document.activeElement === document.querySelector(".ledger-panel button") }));
  const atExitStart = await exitFocus();
  assert.deepEqual(atExitStart, { leaving: true, body: false, inDrawer: false, row: true }, "focus returns to the opener the moment the drawer starts leaving");
  await pause(120);
  assert.deepEqual(await exitFocus(), { leaving: true, body: false, inDrawer: false, row: true }, "focus stays on the opener for the rest of the exit");
  await leavingGone(page);
  assert.equal(await row.evaluate(node => node === document.activeElement), true, "focus is still on the opener once the drawer is gone");
  console.log("drawer: focus returns to the opener when the exit starts");
  await page.click(".page-actions button:has-text('Tambah transaksi')");
  await page.waitForSelector("dialog.quick-record[open]");
  await pause(450);
  await page.click("dialog.quick-record .dialog-close");
  await pause(60);
  await page.click(".page-actions button:has-text('Tambah transaksi')");
  await page.waitForSelector("dialog.quick-record[open]", { timeout: 2000 });
  await pause(450);
  assert.equal(await page.locator("dialog.quick-record[open]").count(), 1, "Catat reopened during the exit opens");
  await page.click("dialog.quick-record .dialog-close");
  await page.waitForFunction(() => !document.querySelector("dialog.quick-record"), null, { timeout: 3000 });
  console.log("catat: reopen during exit opens, close unmounts");

  await page.goto(`${base}/analytics`, { waitUntil: "networkidle" });
  const category = page.locator("#where .where-list > li:not([data-other]) button").first();
  await category.click();
  await page.waitForSelector("#category-drawer");
  await pause(450);
  await page.click("#category-drawer .drawer-close");
  const categoryExit = await page.evaluate(() => ({ leaving: Boolean(document.querySelector(".presence[data-presence='leaving'] #category-drawer")), body: document.activeElement === document.body, opener: document.activeElement === document.querySelector("#where .where-list > li:not([data-other]) button") }));
  assert.deepEqual(categoryExit, { leaving: true, body: false, opener: true }, "the category drawer hands focus back to its opener as it starts leaving");
  await leavingGone(page);
  console.log("category drawer: focus returns to the opener when the exit starts");

  assert.deepEqual(errors, [], `page errors:\n${errors.join("\n")}`);
  console.log("PASS overlay motion");
} finally {
  await browser?.close();
  server.kill();
}
