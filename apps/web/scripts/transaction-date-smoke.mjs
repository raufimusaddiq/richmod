import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { chromium, webkit } from "playwright";

const base = "http://127.0.0.1:3211";
const output = new URL("../test-results/transaction-date/", import.meta.url);
const server = spawn(process.execPath, ["scripts/static-server.mjs"], { stdio: "inherit", env: { ...process.env, PORT: "3211" } });
let browser;

async function openFilters(page) {
  const toggle = page.locator(".filter-toggle");
  await toggle.waitFor({ state: "visible" });
  if ((await toggle.getAttribute("aria-expanded")) !== "true") await toggle.click();
  await page.locator("#transaction-filters-more").waitFor({ state: "visible" });
}

async function fits(input) {
  let box;
  for (let attempt = 0; attempt < 20; attempt++) {
    box = await measure(input);
    if (box.height >= 44 && box.width >= 44) break;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  return check(input, box);
}

async function measure(input) {
  return input.evaluate(element => {
    const rect = element.getBoundingClientRect();
    const parent = element.parentElement.getBoundingClientRect();
    const style = getComputedStyle(element);
    return { left: rect.left, right: rect.right, width: rect.width, height: rect.height, parentLeft: parent.left, parentRight: parent.right, overflow: element.scrollWidth > element.clientWidth + 1, fontSize: Number.parseFloat(style.fontSize) };
  });
}

async function check(input, box) {
  assert.ok(box.left >= box.parentLeft - 1 && box.right <= box.parentRight + 1, `input exceeds container: ${JSON.stringify(box)}`);
  assert.equal(box.overflow, false, `date text overflows: ${JSON.stringify(box)}`);
  assert.ok(box.height >= 44 && box.width >= 44, `date input remains touch accessible: ${JSON.stringify(box)}`);
  await input.focus();
  assert.notEqual(await input.evaluate(element => getComputedStyle(element).outlineStyle), "none", "date input has visible keyboard focus");
  return box;
}

try {
  await mkdir(output, { recursive: true });
  let ready = false;
  for (let attempt = 0; attempt < 80 && !ready; attempt++) {
    try { ready = (await fetch(base)).ok; } catch {}
    if (!ready) await new Promise(resolve => setTimeout(resolve, 250));
  }
  assert.ok(ready, "synthetic transaction server started");
  for (const engine of [chromium, webkit]) {
    browser = await engine.launch();
    for (const width of [320, 390, 600, 1440]) {
      const page = await browser.newPage({ viewport: { width, height: 844 }, locale: "id-ID", timezoneId: "Asia/Jakarta", reducedMotion: "reduce", serviceWorkers: "block" });
      const errors = [];
      page.on("pageerror", error => errors.push(error.message));
      await page.route("**/api/v1/**", route => route.fulfill({ contentType: "application/json", body: JSON.stringify(new URL(route.request().url()).pathname === "/api/v1/auth/me" ? { id: "synthetic-user", displayName: "Rafi", householdName: "Test household", household: { role: "OWNER" } } : []) }));
      await page.goto(`${base}/transactions`, { waitUntil: "networkidle" });
      await openFilters(page);
      for (const [label, value] of [["Dari tanggal", "2026-09-25"], ["Sampai tanggal", "2026-10-01"]]) {
        const input = page.getByLabel(label, { exact: true });
        await fits(input);
        await input.fill(value);
        const box = await fits(input);
        if (width <= 680) assert.ok(box.fontSize >= 16, "mobile date text prevents focus zoom");
      }
      await page.getByRole("button", { name: "Terapkan filter", exact: true }).click();
      assert.equal(new URL(page.url()).searchParams.get("from"), "2026-09-25");
      assert.equal(new URL(page.url()).searchParams.get("to"), "2026-10-01");
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, "ledger fits viewport");
      await page.screenshot({ path: fileURLToPath(new URL(`${engine.name()}-${width}-filters.png`, output)), fullPage: true });
      await page.getByRole("button", { name: "Tambah transaksi", exact: true }).click();
      const dialog = page.locator("dialog[open]");
      await dialog.getByText("Waktu, akun, dan catatan", { exact: true }).click();
      const time = dialog.getByLabel("Waktu", { exact: true });
      await fits(time);
      await time.fill("2026-09-25T12:30");
      await fits(time);
      assert.equal(await time.inputValue(), "2026-09-25T12:30");
      assert.equal(await dialog.evaluate(element => element.scrollWidth > element.clientWidth + 1), false, "transaction dialog fits viewport");
      await page.screenshot({ path: fileURLToPath(new URL(`${engine.name()}-${width}-dialog.png`, output)), fullPage: true });
      await page.keyboard.press("Escape");
      assert.equal(await dialog.count(), 0, "native dialog closes with Escape");
      assert.deepEqual(errors, [], "no transaction runtime errors");
      console.log(`${engine.name()} ${width}px: date filters and manual entry fit`);
      await page.close();
    }
    await browser.close();
    browser = null;
  }
} finally {
  await browser?.close();
  server.kill("SIGTERM");
}
