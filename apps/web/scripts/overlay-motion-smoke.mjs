import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { chromium } from "playwright";

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
  await page.click(".sidebar nav a[href='/transactions']");
  await page.waitForSelector(".ledger-panel");
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
  await leavingGone(page);
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

  assert.deepEqual(errors, [], `page errors:\n${errors.join("\n")}`);
  console.log("PASS overlay motion");
} finally {
  await browser?.close();
  server.kill();
}
