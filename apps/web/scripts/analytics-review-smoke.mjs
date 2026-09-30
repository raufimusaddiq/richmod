import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { chromium } from "playwright";
import { cycleCommentary, cycleFacts } from "../tests/fixtures/cycle-review.mjs";

const port = "3210";
const base = `http://127.0.0.1:${port}`;
const output = new URL("../test-results/cycle-review/", import.meta.url);
const user = { id: "synthetic-user", displayName: "Rafi", householdName: "Rumah keluarga", household: { role: "OWNER" } };
const server = spawn(process.execPath, ["node_modules/next/dist/bin/next", "start", "-H", "127.0.0.1", "-p", port], { stdio: "inherit" });
let browser;

async function waitForServer() {
  for (let attempt = 0; attempt < 80; attempt++) {
    try { if ((await fetch(base)).ok) return; } catch {}
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error("Synthetic review server did not start");
}

try {
  await mkdir(output, { recursive: true });
  await waitForServer();
  browser = await chromium.launch();
  for (const [name, width, height] of [["desktop", 1440, 900], ["tablet", 1024, 768], ["mobile", 390, 844], ["small-mobile", 320, 740]]) {
    const page = await browser.newPage({ viewport: { width, height }, locale: "id-ID", timezoneId: "Asia/Jakarta", reducedMotion: "reduce" });
    const errors = [];
    const requests = [];
    let aiAvailable = true;
    let invalidFacts = false;
    let noSalary = false;
    let salaryCalls = 0;
    page.on("pageerror", error => errors.push(error.message));
    await page.route("**/api/v1/**", async route => {
      const url = new URL(route.request().url());
      requests.push({ path: url.pathname, search: url.search, method: route.request().method() });
      let body = [];
      let status = 200;
      if (url.pathname === "/api/v1/auth/me") body = user;
      else if (url.pathname === "/api/v1/reviews") body = [];
      else if (url.pathname === "/api/v1/analytics/cycle-review") {
        salaryCalls++;
        body = cycleFacts(url.searchParams.get("cycle_start") || undefined);
        if (invalidFacts) status = 503;
        if (noSalary) {
          body.period.kind = "CALENDAR_MONTH"; body.cycles = [];
          body.dataQuality.push({ kind: "MISSING_SALARY_ANCHOR", count: 1, impact: "ANALYSIS_PARTIAL", action: "/settings" });
        }
      } else if (url.pathname === "/api/v1/insights") {
        status = aiAvailable ? 200 : 503;
        body = [{ ...cycleCommentary, historical: true, text: "Historical advice should never display." }, { ...cycleCommentary }];
      } else if (url.pathname === "/api/v1/insights/generate") {
        status = aiAvailable ? 202 : 503;
        body = { id: cycleCommentary.id };
      } else if (url.pathname === "/api/v1/analytics/cashflow") {
        body = [{ period: "2026-08", income: "12000000", expense: "2800000", netCashflow: "9200000" }, { period: "2026-09", income: "12000000", expense: "4200000", netCashflow: "7800000" }];
      } else if (url.pathname === "/api/v1/analytics/categories") {
        body = cycleFacts().categoryChanges;
      } else if (url.pathname === "/api/v1/analytics/spending") {
        body = [{ period: "2026-08", netSpending: "2800000" }, { period: "2026-09", netSpending: "4200000" }];
      } else if (url.pathname === "/api/v1/analytics/merchants") body = cycleFacts().merchantDrivers;
      else if (url.pathname === "/api/v1/analytics/members") body = cycleFacts().memberAttribution;
      else if (url.pathname === "/api/v1/transactions") body = [];
      await route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    });
    await page.goto(`${base}/analytics?view=cycle&cycle=2026-09-01`, { waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
    assert.equal(await page.locator(".cycle-net dd").textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(7800000n));
    assert.equal(requests.filter(request => request.method === "POST").length, 0, "opening review never invokes generation");
    assert.equal(salaryCalls, 1, "explicit cycle uses one facts request");
    assert.equal(await page.getByText("Historical advice should never display.").count(), 0);
    await page.getByRole("heading", { name: "Pembahasan siklus terpilih" }).waitFor();
    assert.equal(await page.locator("#changes tbody tr").count(), 3);
    assert.equal(await page.locator("#changes tbody tr").nth(1).locator("td").nth(2).textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(1350000n), "previous outlier does not hide recent median");
    const groceries = page.getByRole("button", { name: "Belanja rumah", exact: true });
    await groceries.focus();
    assert.notEqual(await groceries.evaluate(element => getComputedStyle(element).outlineStyle), "none");
    await page.keyboard.press("Enter");
    await page.getByRole("heading", { name: "Belanja rumah", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("category"), "11111111-1111-4111-8111-111111111111");
    assert.equal(salaryCalls, 1, "category drill-down reuses returned facts");
    assert.equal(await page.locator("#category-drivers tbody tr").count(), 3);
    const merchant = page.locator("#category-drivers a").filter({ hasText: "Pasar Keluarga" }).first();
    const merchantHref = new URL(await merchant.getAttribute("href"), base);
    assert.equal(merchantHref.searchParams.get("merchantId"), "22222222-2222-4222-8222-222222222222");
    assert.equal(merchantHref.searchParams.get("to"), "2026-09-06");
    assert.equal(merchantHref.searchParams.get("type"), "SPENDING");
    const ledger = page.getByRole("link", { name: "Lihat transaksi Belanja rumah dalam periode ini" });
    await ledger.click();
    await page.getByRole("link", { name: "Kembali ke tinjauan siklus" }).waitFor();
    await page.goBack({ waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Belanja rumah", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-09-01");
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} review has no page overflow`);
    await page.screenshot({ path: new URL(`${name}.png`, output).pathname, fullPage: true, animations: "disabled" });

    // Selected closed cycle, no AI: full deterministic review and supporting
    // data remain usable. No automatic retries or synthesis request.
    aiAvailable = false;
    requests.length = 0;
    await page.getByLabel("Siklus yang ditinjau").selectOption("2026-08-26");
    await page.getByRole("heading", { name: "Pola pengeluaran siklus terpilih" }).waitFor();
    await page.getByText("Pembahasan belum dapat dimuat.", { exact: true }).waitFor();
    for (const id of ["position", "spending-shape", "changes", "drivers", "destinations", "household", "savings-wealth", "quality"]) assert.equal(await page.locator(`#${id}`).count(), 1);
    assert.equal(requests.filter(request => request.method === "POST").length, 0);
    assert.equal(await page.locator("#quality").getByRole("link", { name: "Buka Inbox" }).count(), 1);
    await page.screenshot({ path: new URL(`${name}-closed-ai-unavailable.png`, output).pathname, fullPage: true });

    await page.getByRole("button", { name: "Kalender", exact: true }).click();
    await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("view"), "calendar");
    await page.getByRole("button", { name: "3 Bulan", exact: true }).click();
    assert.equal(new URL(page.url()).searchParams.get("range"), "3");
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false);

    invalidFacts = true;
    await page.goto(`${base}/analytics?view=cycle&cycle=2026-08-26`, { waitUntil: "networkidle" });
    await page.getByRole("alert").filter({ hasText: "Tinjauan siklus belum dapat dimuat." }).waitFor();
    assert.equal(await page.locator("#position").count(), 0, "failed facts never become zero financial amounts");
    invalidFacts = false; noSalary = true; requests.length = 0;
    await page.getByRole("button", { name: "Coba lagi", exact: true }).click();
    await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
    await page.getByText("Belum ada gaji utama terkonfirmasi untuk menentukan siklus", { exact: true }).waitFor();
    assert.equal(requests.filter(request => request.path.startsWith("/api/v1/insights")).length, 0);
    assert.deepEqual(errors, [], `${name} runtime errors`);
    await page.close();
  }
} finally {
  await browser?.close();
  server.kill("SIGTERM");
}
