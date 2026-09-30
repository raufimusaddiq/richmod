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
    let failDecisionSave = false;
    const notes = new Map([["2026-07-26", [{ id: "77777777-7777-4777-8777-777777777777", cycleStart: "2026-07-26", body: "Jaga lebih banyak kas likuid.", author: "Dina", authorUserId: "synthetic-user", createdAt: "2026-08-26T10:00:00+07:00" }]]]);
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
          body.period = { ...body.period, kind: "CALENDAR_MONTH", start: "2026-09-01", state: "ACTIVE", end: "2026-10-01", measuredUntil: "2026-09-07" }; body.cycles = [];
          body.dataQuality.push({ kind: "MISSING_SALARY_ANCHOR", count: 1, impact: "ANALYSIS_PARTIAL", action: "/settings" });
        }
      } else if (url.pathname === "/api/v1/analytics/cycle-decisions") {
        if (route.request().method() === "POST") {
          const input = route.request().postDataJSON();
          assert.deepEqual(Object.keys(input).sort(), ["body", "cycleStart"]);
          status = failDecisionSave ? 503 : 201;
          body = { ...input, id: "88888888-8888-4888-8888-888888888888", author: "Rafi", authorUserId: "synthetic-user", createdAt: "2026-09-06T12:00:00+07:00" };
          if (!failDecisionSave) notes.set(input.cycleStart, [...(notes.get(input.cycleStart) || []), body]);
        } else {
          const start = url.searchParams.get("cycle_start");
          const previous = cycleFacts(start).comparison.previous.start;
          body = { items: notes.get(start) || [], previous: notes.get(previous) || [], previousCycleStart: previous };
        }
      } else if (url.pathname.endsWith("/revoke")) {
        const id = url.pathname.split("/").at(-2);
        for (const [start, items] of notes) notes.set(start, items.filter(item => item.id !== id));
        body = { status: "REVOKED" };
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
      else if (url.pathname.startsWith("/api/v1/transactions")) {
        const transactions = cycleFacts(url.searchParams.get("cycle") || undefined).categoryChanges[0].transactions.map(item => ({ ...item, merchantName: item.merchant, categoryName: "Belanja rumah", status: "CONFIRMED" }));
        const exact = url.searchParams.get("id");
        if (url.pathname === "/api/v1/transactions") body = exact ? transactions.filter(item => item.id === exact) : transactions;
        else if (/\/evidence$|\/audit$/.test(url.pathname)) body = [];
        else body = transactions.find(item => url.pathname.endsWith(item.id)) || transactions[0];
      }
      await route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    });
    await page.goto(`${base}/analytics?view=cycle&cycle=2026-09-01`, { waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
    assert.equal(await page.locator(".cycle-net dd").textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(7800000n));
    assert.equal(requests.filter(request => request.method === "POST").length, 0, "opening review never invokes generation");
    assert.equal(await page.getByRole("button", { name: "Tinjau siklus ini", exact: true }).count(), 0, "active cycle cannot enter closed-cycle meeting");
    assert.equal(await page.getByRole("button", { name: "Simpan keputusan", exact: true }).count(), 0, "active cycle has no save action");
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
    const transaction = page.locator("#category-drivers").getByRole("link", { name: "Buka transaksi", exact: true }).first();
    const transactionLink = new URL(await transaction.getAttribute("href"), base);
    assert.equal(transactionLink.searchParams.get("id"), "33333333-3333-4333-8333-333333333333");
    await transaction.click();
    await page.getByRole("dialog", { name: "Detail transaksi", exact: true }).waitFor();
    await page.getByRole("heading", { name: "Pasar Keluarga", exact: true }).waitFor();
    assert.equal(await page.locator(".transaction-row").count(), 1, "evidence link scopes the ledger to one row");
    await page.goBack({ waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Belanja rumah", exact: true }).waitFor();
    const ledger = page.getByRole("link", { name: "Lihat transaksi Belanja rumah dalam periode ini" });
    await ledger.click();
    await page.getByRole("link", { name: "Kembali ke tinjauan siklus" }).waitFor();
    await page.goBack({ waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Belanja rumah", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-09-01");
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} review has no page overflow`);
    await page.screenshot({ path: new URL(`${name}.png`, output).pathname, fullPage: true, animations: "disabled" });
    await page.locator(".cycle-spending-chart .recharts-bar-rectangle").first().hover();
    await page.locator(".chart-tooltip").waitFor();
    assert.match(await page.locator(".chart-tooltip").textContent(), /Pengeluaran bersih.*Rp.*Refund.*Rp/);
    await page.screenshot({ path: new URL(`${name}-tooltip.png`, output).pathname, fullPage: true, animations: "disabled" });

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

    // A complete, URL-bound closed-cycle meeting works without AI. Typing does
    // not write; explicit failed save preserves the draft for retry.
    await page.getByRole("button", { name: "Tinjau siklus ini", exact: true }).click();
    await page.waitForFunction(() => document.activeElement?.id === "position-title");
    assert.equal(new URL(page.url()).searchParams.get("review"), "position");
    assert.equal(await page.locator("#position h2").evaluate(element => element === document.activeElement), true);
    assert.equal(await page.locator("#changes").isVisible(), false);
    const nav = page.getByRole("navigation", { name: "Langkah tinjauan siklus" });
    assert.equal(await nav.getByRole("button").count(), 8);
    await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
    await page.getByRole("heading", { name: "Pola pengeluaran siklus terpilih" }).waitFor();
    await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
    await page.getByRole("button", { name: "Belanja rumah", exact: true }).click();
    assert.equal(new URL(page.url()).searchParams.get("review"), "drivers");
    const meetingLedger = page.getByRole("link", { name: "Lihat transaksi Belanja rumah dalam periode ini" });
    assert.equal(new URL(await meetingLedger.getAttribute("href"), base).searchParams.get("review"), "drivers");
    await meetingLedger.click();
    const returnLink = page.getByRole("link", { name: "Kembali ke tinjauan siklus", exact: true });
    await returnLink.waitFor();
    assert.equal(new URL(await returnLink.getAttribute("href"), base).searchParams.get("review"), "drivers");
    await returnLink.click();
    await page.getByRole("heading", { name: "Belanja rumah", exact: true }).waitFor();
    for (const [button, section] of [["5. Tabungan & kekayaan", "savings-wealth"], ["6. Tindak lanjut", "quality"], ["7. Pembahasan", "discussion"], ["8. Keputusan", "decisions"]]) {
      await page.getByRole("navigation", { name: "Langkah tinjauan siklus" }).getByRole("button", { name: button, exact: true }).click();
      await page.locator(`#${section}`).waitFor({ state: "visible" });
      assert.equal(await page.locator(`#${section}`).isVisible(), true);
      assert.equal(new URL(page.url()).searchParams.get("review"), section);
    }
    await page.getByText("Jaga lebih banyak kas likuid.", { exact: true }).waitFor();
    const draft = "Simpan lebih banyak kas untuk kebutuhan keluarga.";
    await page.getByLabel("Keputusan untuk siklus ini", { exact: true }).fill(draft);
    assert.equal(requests.filter(request => request.method === "POST").length, 0, "no automatic decision write or AI generation during meeting");
    await page.getByRole("button", { name: "Langkah sebelumnya", exact: true }).click();
    await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
    assert.equal(await page.getByLabel("Keputusan untuk siklus ini", { exact: true }).inputValue(), draft);
    failDecisionSave = true;
    await page.getByRole("button", { name: "Simpan keputusan", exact: true }).click();
    await page.getByRole("alert").filter({ hasText: "Keputusan belum dapat disimpan" }).waitFor();
    assert.equal(await page.getByLabel("Keputusan untuk siklus ini", { exact: true }).inputValue(), draft);
    failDecisionSave = false;
    await page.getByLabel("Keputusan untuk siklus ini", { exact: true }).press("Control+Enter");
    await page.locator("#decisions .decision-list").getByText(draft, { exact: true }).waitFor();
    assert.equal(await page.getByLabel("Keputusan untuk siklus ini", { exact: true }).inputValue(), "");
    assert.equal(requests.filter(request => request.path === "/api/v1/analytics/cycle-decisions" && request.method === "POST").length, 2, "only explicit save and explicit retry");
    assert.equal(requests.filter(request => request.path === "/api/v1/insights/generate").length, 0);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} meeting has no overflow`);
    await page.screenshot({ path: new URL(`${name}-meeting-decisions.png`, output).pathname, fullPage: true, animations: "disabled" });
    page.once("dialog", dialog => dialog.accept());
    await page.getByRole("button", { name: "Batalkan keputusan", exact: true }).click();
    await page.getByText("Keputusan dibatalkan. Riwayat tetap tersimpan.", { exact: true }).waitFor();
    await page.getByRole("button", { name: "Selesai meninjau", exact: true }).click();
    assert.equal(new URL(page.url()).searchParams.has("review"), false);
    assert.equal(await page.locator("#position").isVisible(), true);

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
