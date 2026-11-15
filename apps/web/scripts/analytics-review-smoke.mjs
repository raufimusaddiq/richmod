import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { cycleCommentary, cycleFacts, stableCycleFacts } from "../tests/fixtures/cycle-review.mjs";

const port = "3210";
const base = `http://127.0.0.1:${port}`;
const output = new URL("../test-results/cycle-review/", import.meta.url);
const user = { id: "synthetic-user", displayName: "Rafi", householdName: "Rumah keluarga", household: { role: "OWNER" } };
// Serve the static export the API embeds; `next start` cannot run an export.
const server = spawn(process.execPath, ["scripts/static-server.mjs"], { stdio: "inherit", env: { ...process.env, PORT: port } });
let browser;

async function waitForServer() {
  for (let attempt = 0; attempt < 80; attempt++) {
    try { if ((await fetch(base)).ok) return; } catch {}
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error("Synthetic review server did not start");
}

// A disclosure can be closed again by a React re-render that lands after the DOM
// was opened, so open it and confirm the content is actually visible.
async function openDisclosure(page, selector, content) {
  for (let attempt = 0; attempt < 10; attempt++) {
    await page.evaluate(sel => { const details = document.querySelector(sel); if (details) details.open = true; }, selector);
    try {
      await page.locator(content).first().waitFor({ state: "visible", timeout: 2000 });
      return;
    } catch {}
  }
  throw new Error(`could not open ${selector}`);
}

try {
  await mkdir(output, { recursive: true });
  await waitForServer();
  browser = await chromium.launch();
  for (const [name, width, height] of [["desktop", 1440, 1050], ["tablet", 1024, 768], ["mobile", 390, 844], ["small-mobile", 320, 740]]) {
    const page = await browser.newPage({ viewport: { width, height }, locale: "id-ID", timezoneId: "Asia/Jakarta", reducedMotion: "reduce" });
    const errors = [];
    const requests = [];
    let aiAvailable = true;
    let generationStatus = 202;
    let olderCommentary = false;
    let invalidFacts = false;
    let merchantsFail = false;
    let noSalary = false;
    let stable = false;
    let salaryCalls = 0;
    let failDecisionSave = false;
    let decisionsFail = false;
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
        body = stable ? stableCycleFacts() : cycleFacts(url.searchParams.get("cycle_start") || undefined);
        if (invalidFacts) status = 503;
        if (noSalary) {
          body.period = { ...body.period, kind: "CALENDAR_MONTH", start: "2026-09-01", state: "ACTIVE", end: "2026-10-01", measuredUntil: "2026-09-07" }; body.cycles = []; body.history = []; body.categoryHistory = { cycleStarts: [], rows: [], other: { amounts: [] } };
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
          if (decisionsFail) status = 500;
        }
      } else if (url.pathname.endsWith("/revoke")) {
        const id = url.pathname.split("/").at(-2);
        for (const [start, items] of notes) notes.set(start, items.filter(item => item.id !== id));
        body = { status: "REVOKED" };
      } else if (url.pathname === "/api/v1/insights") {
        status = aiAvailable ? 200 : 503;
        body = [{ ...cycleCommentary, historical: true, text: "Historical advice should never display." }, { ...cycleCommentary, id: "stale-commentary", createdAt: "2026-09-05T12:00:00+07:00", text: "Stale cutoff must never display.", metrics: { ...cycleCommentary.metrics, period_end: "2026-09-06" } }, stable ? { ...cycleCommentary, text: "Tidak ada perubahan yang menonjol terhadap tiga siklus sebelumnya.", metrics: { period_kind: "SALARY_CYCLE", period_start: "2026-08-26", period_end: "2026-09-01" } } : olderCommentary ? { ...cycleCommentary, text: "Older daily snapshot is visible.", metrics: { ...cycleCommentary.metrics, period_end: "2026-09-06" } } : { ...cycleCommentary }];
      } else if (url.pathname === "/api/v1/insights/generate") {
        status = aiAvailable ? generationStatus : 503;
        body = { id: cycleCommentary.id };
      } else if (url.pathname === "/api/v1/analytics/cashflow") {
        body = [{ period: "2026-07", income: "0", expense: "0", refund: "0", netCashflow: "0" }, { period: "2026-08", income: "12000000", expense: "2800000", netCashflow: "9200000" }, { period: "2026-09", income: "12000000", expense: "4200000", netCashflow: "7800000" }];
      } else if (url.pathname === "/api/v1/analytics/categories") {
        body = cycleFacts().categoryChanges;
      } else if (url.pathname === "/api/v1/analytics/merchants") { if (merchantsFail) status = 500; body = cycleFacts().merchantDrivers; }
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
    const explanation = page.locator("#spending-shape .section-title .review-explainer");
    assert.equal(await explanation.locator("p").isVisible(), false, "explanation is secondary to the figures");
    await explanation.locator("summary").focus();
    await page.keyboard.press("Enter");
    assert.equal(await explanation.locator("p").isVisible(), true, "native disclosure is keyboard accessible");
    await page.keyboard.press("Enter");
    assert.equal(await explanation.locator("p").isVisible(), false);
    assert.equal(await page.locator(".kpi-note").isVisible(), true, "refund amount stays visible");
    await page.evaluate(() => document.fonts.ready);
    assert.equal(await page.evaluate(() => document.fonts.check('500 16px "Fraunces"') && document.fonts.check('400 14px "Inter"')), true, "brand fonts loaded");
    assert.equal((await page.locator(".kpi-cell[data-lead] dt").textContent()).startsWith("Pengeluaran bersih"), true, "a running cycle leads with spending, not a net cashflow that looks large on day 1");
    assert.equal(await page.locator(".kpi-cell[data-lead] dd").textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(4200000n));
    assert.equal(await page.locator("#position").getByText("Arus kas bersih sejauh ini", { exact: true }).isVisible(), true, "net cashflow stays visible as a regular metric");
    assert.equal(requests.filter(request => request.method === "POST").length, 0, "opening review never invokes generation");
    assert.equal(await page.getByRole("button", { name: "Tinjau siklus ini", exact: true }).count(), 0, "active cycle cannot enter closed-cycle meeting");
    assert.equal(await page.getByRole("button", { name: "Simpan keputusan", exact: true }).count(), 0, "active cycle has no save action");
    assert.equal(salaryCalls, 1, "explicit cycle uses one facts request");
    assert.equal(await page.getByText("Historical advice should never display.").count(), 0);
    assert.equal(await page.getByText("Stale cutoff must never display.").count(), 0);
    for (const id of ["merchants", "household", "discussion", "decisions"]) {
      assert.equal(await page.locator(`#${id} > details`).evaluate(element => element.open), false, `${id} is available on demand, not a wall of text`);
    }
    assert.equal(await page.locator("#quality").getByRole("link", { name: "Buka Inbox" }).isVisible(), true, "blockers stay actionable without expansion");
    assert.equal(await page.locator(".comparison-bars > div").count(), 4);
    assert.equal(await page.locator(".diverge > li").count(), 3, "Yang berubah draws the served changes (at most five)");
    assert.equal(await page.locator(".change-ranking").count(), 0, "the 25-row list is gone");
    assert.equal(await page.locator("#where .where-list > li").count(), 3, "three ranked categories; an empty remainder is not drawn");
    assert.equal(await page.locator("#where .where-total strong").textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(4200000n), "the list total is the served net expense");
    const rowAmounts = await page.locator("#where .where-list > li:not([data-other]) .where-amount").allTextContents();
    assert.deepEqual(rowAmounts, [2100000n, 1400000n, 700000n].map(value => new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(value)), "ranking order is the served order");
    const fullComparison = page.locator("#changes details.review-daily");
    assert.equal(await fullComparison.evaluate(element => element.open), false, "the full comparison is secondary to the two cards");
    await fullComparison.locator(":scope > summary").focus();
    await page.keyboard.press("Enter");
    assert.equal(await fullComparison.evaluate(element => element.open), true, "complete comparison is keyboard accessible");
    assert.equal(await page.getByRole("columnheader", { name: "Selisih vs hari yang sama", exact: true }).isVisible(), true);
    assert.equal(await page.getByRole("columnheader", { name: "Sebelumnya · penuh", exact: true }).isVisible(), true);
    await page.getByRole("region", { name: "Perbandingan kategori lengkap, geser untuk semua kolom", exact: true }).focus();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} expanded comparison has no page overflow`);
    if (name === "desktop") await page.screenshot({ path: fileURLToPath(new URL("readme-analytics.png", output)), fullPage: false, animations: "disabled" });
    assert.equal(await page.locator("#changes tbody tr").count(), 3);
    assert.equal(await page.locator("#changes tbody tr").nth(1).locator("td").nth(2).textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(1350000n), "previous outlier does not hide recent median");
    // Detail perubahan stays open: the category buttons below live inside it.
    await page.locator("#discussion > details > summary").click();
    await page.getByRole("heading", { name: "Pembahasan siklus terpilih" }).waitFor();
    await page.locator("#discussion > details > summary").click();
    const groceries = page.locator("#where").getByRole("button", { name: /^Belanja rumah/ });
    const drawer = page.getByRole("dialog", { name: "Belanja rumah", exact: true });
    await page.keyboard.press("Tab");
    await groceries.focus();
    assert.notEqual(await groceries.evaluate(element => getComputedStyle(element).outlineStyle), "none");
    const rankedAmount = await groceries.locator(".where-amount").textContent();
    await page.keyboard.press("Enter");
    await drawer.waitFor();
    assert.equal(new URL(page.url()).searchParams.get("category"), "11111111-1111-4111-8111-111111111111");
    assert.equal(await drawer.evaluate(element => element.contains(document.activeElement)), true, "focus moves into the drawer");
    assert.equal(await drawer.locator(".detail-amount").textContent(), rankedAmount, "drill-down total equals the row it was opened from");
    for (let index = 0; index < 40; index += 1) await page.keyboard.press("Tab");
    assert.equal(await drawer.evaluate(element => element.contains(document.activeElement)), true, "Tab stays inside the modal");
    await page.keyboard.press("Escape");
    await drawer.waitFor({ state: "detached" });
    await page.waitForFunction(() => !new URL(location.href).searchParams.has("category"));
    assert.equal(await groceries.evaluate(element => element === document.activeElement), true, "Escape returns focus to the row that opened it");
    await groceries.click();
    await drawer.waitFor();
    await page.goBack();
    await drawer.waitFor({ state: "detached" });
    assert.equal(new URL(page.url()).searchParams.has("category"), false, "Back closes the drawer");
    await groceries.click();
    await drawer.waitFor();
    assert.equal(salaryCalls, 1, "category drill-down reuses returned facts");
    assert.equal(await drawer.locator(".drawer-list li").count(), 3);
    const merchant = drawer.locator("a").filter({ hasText: "Pasar Keluarga" }).first();
    const merchantHref = new URL(await merchant.getAttribute("href"), base);
    assert.equal(merchantHref.searchParams.get("merchantId"), "22222222-2222-4222-8222-222222222222");
    assert.equal(merchantHref.searchParams.get("to"), "2026-09-06");
    assert.equal(merchantHref.searchParams.get("type"), "SPENDING");
    const transaction = drawer.getByRole("link", { name: /^Buka transaksi/ }).first();
    const transactionLink = new URL(await transaction.getAttribute("href"), base);
    assert.equal(transactionLink.searchParams.get("id"), "33333333-3333-4333-8333-333333333333");
    await transaction.click();
    await page.getByRole("dialog", { name: "Detail transaksi", exact: true }).waitFor();
    await page.getByRole("heading", { name: "Pasar Keluarga", exact: true }).waitFor();
    assert.equal(await page.locator(".transaction-row").count(), 1, "evidence link scopes the ledger to one row");
    await page.goBack({ waitUntil: "networkidle" });
    await drawer.waitFor();
    const ledger = page.getByRole("link", { name: "Lihat transaksi Belanja rumah dalam periode ini" });
    await ledger.click();
    await page.getByRole("link", { name: "Kembali ke tinjauan siklus" }).waitFor();
    await page.goBack({ waitUntil: "networkidle" });
    await drawer.waitFor();
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-09-01");
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} review has no page overflow`);
    await page.screenshot({ path: fileURLToPath(new URL(`${name}.png`, output)), fullPage: true, animations: "disabled" });
    await page.screenshot({ path: fileURLToPath(new URL(`${name}-drawer.png`, output)), animations: "disabled" });
    await page.keyboard.press("Escape");
    await drawer.waitFor({ state: "detached" });
    await page.locator(".cycle-spending-chart .recharts-bar-rectangle").first().hover();
    await page.locator(".chart-tooltip").waitFor();
    assert.match(await page.locator(".chart-tooltip").textContent(), /Pengeluaran bersih.*Rp.*Refund.*Rp/);
    await page.screenshot({ path: fileURLToPath(new URL(`${name}-tooltip.png`, output)), fullPage: true, animations: "disabled" });

    // Kategori × siklus heatmap: same-day default while the cycle runs, a full-cycle
    // toggle that leaves the running column untinted, and served values only.
    await page.goto(`${base}/analytics?view=cycle&cycle=2026-09-01`, { waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Kategori × siklus", exact: true }).waitFor();
    await openDisclosure(page, "#matrix details", ".heat-table");
    const heat = page.locator(".heat-table");
    assert.equal(await heat.locator("thead th:not(:first-child):not(.heat-delta-head)").count(), 6, "one column per served cycle");
    assert.equal(await heat.locator('thead th[aria-current="true"]').count(), 1, "the selected cycle is marked");
    const sameDay = page.getByRole("button", { name: "Hari ke-6 yang sama", exact: true });
    const fullCycle = page.getByRole("button", { name: "Siklus penuh", exact: true });
    assert.equal(await sameDay.getAttribute("aria-pressed"), "true", "same-day is the default while the cycle runs");
    assert.equal(await fullCycle.getAttribute("aria-pressed"), "false");
    assert.equal(await heat.locator("thead th small").filter({ hasText: "hari 1–6" }).count(), 6, "every column names its cut");
    assert.equal(await heat.locator("tbody tr").count(), 4, "three categories plus the served remainder");
    assert.match(await heat.locator("tbody tr").last().locator("th").textContent(), /^Kategori lain/, "the served remainder closes the matrix");
    assert.equal(await heat.locator("tfoot th").textContent(), "Total pengeluaran bersih");
    await fullCycle.click();
    assert.equal(await fullCycle.getAttribute("aria-pressed"), "true");
    assert.equal(await heat.locator("thead th[data-running]").count(), 1, "the running column is flagged in full mode");
    assert.equal(await heat.locator("td[data-running]").count(), 5, "every row and the total mark the running column");
    assert.equal(await heat.locator("td[data-running] .heat-cell").first().evaluate(element => element.style.getPropertyValue("--tint").trim()), "0", "the running column carries no tint");
    assert.equal(await heat.locator("td[data-running] .heat-cell").first().evaluate(element => getComputedStyle(element).borderStyle), "dashed", "the running column is dashed instead of tinted");
    assert.deepEqual(await heat.locator("td[data-running] .heat-cell").evaluateAll(cells => cells.map(cell => cell.style.getPropertyValue("--tint").trim())), ["0", "0", "0", "0"], "no tinted cell sits in the running column");
    await sameDay.click();
    assert.equal(await sameDay.getAttribute("aria-pressed"), "true");
    assert.match(await page.locator(".kpi-cell[data-lead]").textContent(), /vs siklus lalu di hari ke-6/);
    await page.getByRole("heading", { name: "Siklus ke siklus", exact: true }).waitFor();
    assert.equal(await page.locator("#history .history-cols > li").count(), 6, "the cycle columns card draws every served cycle");
    const matrixRow = heat.getByRole("button", { name: "Buka bukti Belanja rumah", exact: true });
    assert.equal(await matrixRow.count(), 1);
    assert.equal(await heat.locator("tbody tr").first().locator("td.heat-amount").last().textContent(), "2,1", "the selected column shows the served category amount");
    await matrixRow.focus();
    await page.keyboard.press("Enter");
    await page.getByRole("dialog", { name: "Belanja rumah", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("category"), "11111111-1111-4111-8111-111111111111");
    await page.keyboard.press("Escape");
    // The URL changes before React re-renders (next 16.4 defers the update), so wait for the state this line asserts.
    await page.getByRole("dialog", { name: "Belanja rumah", exact: true }).waitFor({ state: "detached" });
    await page.waitForFunction(() => !new URL(location.href).searchParams.has("category"));
    assert.equal(await matrixRow.evaluate(element => element === document.activeElement), true, "closing the drawer returns focus to the matrix row");

    // A closed cycle has no same-day matrix; returning to the active cycle re-seeds it.
    await page.getByLabel("Siklus yang ditinjau").selectOption("2026-08-26");
    await page.waitForFunction(() => document.querySelector(".cycle-status strong")?.textContent.startsWith("Ditutup"));
    await openDisclosure(page, "#matrix details", ".heat-table");
    await page.waitForFunction(() => !document.querySelector(".heat-section .segmented"));
    assert.equal(await page.getByRole("button", { name: "Siklus penuh", exact: true }).count(), 0, "a closed cycle has no same-day toggle");
    assert.equal(await page.locator(".heat-table thead th[data-running]").count(), 0, "a closed cycle has no running column");
    await page.getByLabel("Siklus yang ditinjau").selectOption("2026-09-01");
    await page.waitForFunction(() => document.querySelector(".cycle-status strong")?.textContent.startsWith("Berjalan"));
    await openDisclosure(page, "#matrix details", ".heat-section .segmented");
    await page.waitForFunction(() => document.querySelector('.heat-section .segmented button[aria-pressed="true"]')?.textContent.includes("Hari ke-6"));
    assert.equal(await page.getByRole("button", { name: "Hari ke-6 yang sama", exact: true }).getAttribute("aria-pressed"), "true", "returning to the active cycle re-seeds same-day");

    // The cycle-columns card shows a served amount and a real bar, not Rp0/NaN.
    const historyColumn = page.locator("#history .history-cols > li").first();
    assert.match(await historyColumn.locator("em").textContent(), /[1-9]/, "a history column shows a served amount");
    assert.ok(await historyColumn.locator(".history-plot i").evaluate(element => element.getBoundingClientRect().height) > 0, "the history bar has a real height");

    // Merchant pendukung: a real table, largest first, with the served total.
    await openDisclosure(page, "#merchants details", "#merchants table.merchant-rank");
    const merchants = page.locator("#merchants table.merchant-rank");
    for (const header of ["Merchant", "Transaksi", "Siklus ini", "Siklus lalu, hari yang sama", "Selisih"]) assert.equal(await merchants.getByRole("columnheader", { name: header, exact: true }).count(), 1, `merchant column ${header}`);
    assert.equal(await merchants.locator("tbody tr").count(), 2, "one row per served payee plus the remainder");
    assert.equal(await merchants.locator("tfoot th").textContent(), "Total");
    assert.match(await merchants.locator("tfoot").textContent(), /Rp\s?4\.200\.000/, "the total equals the cycle's net expense");
    assert.equal(new URL(await merchants.getByRole("link", { name: "Pasar Keluarga", exact: true }).getAttribute("href"), base).searchParams.get("merchantId"), "22222222-2222-4222-8222-222222222222");

    // Catatan rumah tangga: fixed order, automatic bucket first, served shares.
    await openDisclosure(page, "#household details", "#household .household-list");
    const household = page.locator("#household .household-list > li");
    assert.equal(await household.count(), 2);
    assert.match(await household.first().textContent(), /otomatis/, "the automatic bucket comes first");
    assert.match(await household.first().textContent(), /47,6%/, "the served share is shown");
    assert.equal(await page.locator("#household .household-bar i").count(), 2);
    assert.equal(requests.filter(request => request.method === "POST").length, 0, "opening the detail sections never writes");
    assert.equal(await page.locator(".cycle-pace-chart").isVisible(), true, "pace is its own single-series chart");
    const rows = await page.evaluate(() => { const box = selector => document.querySelector(selector)?.getBoundingClientRect(); const same = (x, y) => Boolean(x && y) && Math.abs(x.top - y.top) < 4 && x.right <= y.left + 1; return { where: same(box("#where"), box("#changes")), daily: same(box("#daily"), box("#history")) }; });
    if (name !== "tablet") { assert.equal(rows.where, name === "desktop", `${name} "Ke mana uangnya" and "Yang berubah" ${name === "desktop" ? "share a row" : "stack"}`); assert.equal(rows.daily, name === "desktop", `${name} daily and cycle columns ${name === "desktop" ? "share a row" : "stack"}`); }
    const charts = await page.evaluate(() => { const [a, b] = ["#spending-shape", "#savings-wealth"].map(selector => document.querySelector(selector)?.getBoundingClientRect()); return { sameRow: Boolean(a && b) && Math.abs(a.top - b.top) < 4 && a.right <= b.left + 1, section: Math.round(a?.height || 0) }; });
    if (name !== "tablet") assert.equal(charts.sameRow, name === "desktop", `${name} daily charts ${name === "desktop" ? "share a row" : "stack"} (section ${charts.section}px)`);
    assert.equal(await page.locator(".cycle-pace-chart .recharts-line-curve").count(), 3, "the selected cycle, the previous cycle and the median are drawn as curves");
    assert.equal(await page.getByRole("button", { name: "Siklus berikutnya ›", exact: true }).isDisabled(), true, "the active cycle has no newer neighbour");
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} matrix has no page overflow`);
    await page.screenshot({ path: fileURLToPath(new URL(`${name}-ledger.png`, output)), fullPage: true, animations: "disabled" });
    decisionsFail = true;
    await page.getByLabel("Siklus yang ditinjau").selectOption("2026-08-26");
    await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-08-26");
    await page.waitForFunction(() => document.querySelector(".kpi-cell[data-lead]")?.textContent.includes("(penuh)"));
    assert.match(await page.locator(".kpi-cell[data-lead]").textContent(), /vs siklus lalu .penuh./);
    await openDisclosure(page, "#decisions > details", "#decisions form.decision-form");
    await page.getByRole("alert").filter({ hasText: "Keputusan belum dapat dimuat" }).waitFor();
    assert.equal(await page.getByLabel("Keputusan baru untuk siklus ini", { exact: true }).count(), 1, "a failed list still allows recording a decision");
    decisionsFail = false;
    await openDisclosure(page, "#decisions > details", "#decisions form.decision-form");
    await page.locator("#decisions").getByRole("button", { name: "Coba lagi", exact: true }).click();
    await page.locator("#decisions .decision-list, #decisions .empty").first().waitFor();
    assert.equal(await page.getByRole("button", { name: "‹ Siklus sebelumnya", exact: true }).isDisabled(), true, "the served list has no older cycle");
    await page.getByRole("button", { name: "Siklus berikutnya ›", exact: true }).click();
    await page.waitForFunction(() => document.querySelector(".cycle-status strong")?.textContent.startsWith("Berjalan"));
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-09-01");
    await page.getByRole("button", { name: "‹ Siklus sebelumnya", exact: true }).click();
    await page.waitForFunction(() => document.querySelector(".cycle-status strong")?.textContent.startsWith("Ditutup"));
    await page.getByRole("button", { name: "Kalender", exact: true }).click();
    await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-08-26", "Calendar keeps the reviewed cycle in the URL");
    assert.equal(await page.getByRole("heading", { name: "Analisis kalender", exact: true }).count(), 1, "the page title follows the view");
    await page.getByRole("button", { name: "Siklus Gaji", exact: true }).click();
    await page.getByRole("heading", { name: "Pengeluaran harian", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-08-26", "returning from Calendar keeps the reviewed cycle");

    olderCommentary = true;
    await page.goto(`${base}/analytics?view=cycle&cycle=2026-09-01`, { waitUntil: "networkidle" });
    await page.locator("#discussion > details > summary").click();
    await page.getByText("Older daily snapshot is visible.", { exact: true }).waitFor();
    assert.match(await page.locator("#discussion .insight-muted").first().textContent(), /Data pembahasan: .*5 Sep 2026.*bukan data terkini/);
    generationStatus = 429;
    await page.getByRole("button", { name: "Perbarui pembahasan", exact: true }).click();
    await page.getByText("Batas satu pembahasan per jam. Coba lagi setelah jeda satu jam dari permintaan terakhir.", { exact: true }).waitFor();
    assert.equal(await page.getByText("Older daily snapshot is visible.", { exact: true }).isVisible(), true, "hourly cap does not erase the dated snapshot");
    olderCommentary = false; generationStatus = 202;

    // Selected closed cycle, no AI: full deterministic review and supporting
    // data remain usable. No automatic retries or synthesis request.
    aiAvailable = false;
    requests.length = 0;
    await page.getByLabel("Siklus yang ditinjau").selectOption("2026-08-26");
    await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
    await page.locator("#discussion > details > summary").getByText("Belum tersedia", { exact: true }).waitFor();
    for (const id of ["position", "spending-shape", "where", "changes", "daily", "history", "merchants", "household", "savings-wealth", "quality"]) assert.equal(await page.locator(`#${id}`).count(), 1);
    assert.equal(requests.filter(request => request.method === "POST").length, 0);
    assert.equal(await page.locator("#quality").getByRole("link", { name: "Buka Inbox" }).count(), 1);
    await page.screenshot({ path: fileURLToPath(new URL(`${name}-closed-ai-unavailable.png`, output)), fullPage: true });

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
    await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
    await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
    await page.getByRole("button", { name: "Belanja rumah", exact: true }).click();
    await page.getByRole("dialog", { name: "Belanja rumah", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("review"), "drivers");
    const meetingLedger = page.getByRole("link", { name: "Lihat transaksi Belanja rumah dalam periode ini" });
    assert.equal(new URL(await meetingLedger.getAttribute("href"), base).searchParams.get("review"), "drivers");
    await meetingLedger.click();
    const returnLink = page.getByRole("link", { name: "Kembali ke tinjauan siklus", exact: true });
    await returnLink.waitFor();
    assert.equal(new URL(await returnLink.getAttribute("href"), base).searchParams.get("review"), "drivers");
    await returnLink.click();
    await page.getByRole("dialog", { name: "Belanja rumah", exact: true }).waitFor();
    await page.keyboard.press("Escape");
    for (const [button, section] of [["5. Tabungan & kekayaan", "savings-wealth"], ["6. Tindak lanjut", "quality"], ["7. Pembahasan", "discussion"], ["8. Keputusan", "decisions"]]) {
      await page.getByRole("navigation", { name: "Langkah tinjauan siklus" }).getByRole("button", { name: button, exact: true }).click();
      await page.locator(`#${section}`).waitFor({ state: "visible" });
      assert.equal(await page.locator(`#${section}`).isVisible(), true);
      assert.equal(new URL(page.url()).searchParams.get("review"), section);
    }
    await page.getByText("Jaga lebih banyak kas likuid.", { exact: true }).waitFor();
    const draft = "Simpan lebih banyak kas untuk kebutuhan keluarga.";
    await page.getByLabel("Keputusan baru untuk siklus ini", { exact: true }).fill(draft);
    assert.equal(requests.filter(request => request.method === "POST").length, 0, "no automatic decision write or AI generation during meeting");
    await page.getByRole("button", { name: "Langkah sebelumnya", exact: true }).click();
    await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
    assert.equal(await page.getByLabel("Keputusan baru untuk siklus ini", { exact: true }).inputValue(), draft);
    failDecisionSave = true;
    await page.getByRole("button", { name: "Simpan keputusan", exact: true }).click();
    await page.getByRole("alert").filter({ hasText: "Keputusan belum dapat disimpan" }).waitFor();
    assert.equal(await page.getByLabel("Keputusan baru untuk siklus ini", { exact: true }).inputValue(), draft);
    failDecisionSave = false;
    await page.getByLabel("Keputusan baru untuk siklus ini", { exact: true }).press("Control+Enter");
    await page.locator("#decisions .decision-list").getByText(draft, { exact: true }).waitFor();
    assert.equal(await page.getByLabel("Keputusan baru untuk siklus ini", { exact: true }).inputValue(), "");
    assert.equal(requests.filter(request => request.path === "/api/v1/analytics/cycle-decisions" && request.method === "POST").length, 2, "only explicit save and explicit retry");
    assert.equal(requests.filter(request => request.path === "/api/v1/insights/generate").length, 0);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} meeting has no overflow`);
    await page.screenshot({ path: fileURLToPath(new URL(`${name}-meeting-decisions.png`, output)), fullPage: true, animations: "disabled" });
    await page.getByRole("button", { name: "Batalkan keputusan", exact: true }).click();
    await page.locator("dialog[open]").getByRole("button", { name: "Ya, batalkan", exact: true }).click();
    await page.getByText("Keputusan dibatalkan. Riwayat tetap tersimpan.", { exact: true }).waitFor();
    await page.getByRole("button", { name: "Selesai meninjau", exact: true }).click();
    // Same rule: closing the meeting clears the URL first, then swaps the view back.
    await page.waitForFunction(() => !new URL(location.href).searchParams.has("review") && document.querySelector("#position")?.checkVisibility({ checkVisibilityCSS: true, checkOpacity: false }) === true);
    assert.equal(new URL(page.url()).searchParams.has("review"), false);
    assert.equal(await page.locator("#position").isVisible(), true);

    await page.getByRole("button", { name: "Kalender", exact: true }).click();
    await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("view"), "calendar");
    await page.getByRole("button", { name: "3 Bulan", exact: true }).click();
    assert.equal(new URL(page.url()).searchParams.get("range"), "3");
    // Calendar hygiene: months are formatted, and a custom range is validated beside its fields instead of failing after a request.
    assert.equal(await page.getByRole("rowheader", { name: /Sep 26/ }).count(), 1, "months are formatted, not raw YYYY-MM");
    const monthTable = page.getByRole("region", { name: "Arus kas per bulan, geser untuk semua kolom", exact: true });
    const monthLink = monthTable.getByRole("link", { name: "Lihat pengeluaran Agu 26", exact: true });
    assert.equal(await monthLink.getAttribute("href"), "/transactions?from=2026-08-01&to=2026-08-31&status=CONFIRMED&type=SPENDING&cycle=2026-08-26");
    assert.equal(await monthTable.getByRole("row").filter({ hasText: "belum ada transaksi" }).count(), 1, "empty month remains visible");
    assert.equal(await monthTable.getByRole("row").filter({ hasText: "belum ada transaksi" }).getByRole("link").count(), 0, "empty month stays non-interactive");
    await monthLink.focus();
    await page.keyboard.press("Enter");
    await page.getByRole("link", { name: "Kembali ke tinjauan siklus", exact: true }).waitFor();
    assert.equal(new URL(page.url()).pathname, "/transactions", "month link works by keyboard");
    await page.goBack({ waitUntil: "networkidle" });
    await monthTable.waitFor();
    await page.getByLabel("Bulan mulai").fill("2025-03");
    await page.getByLabel("Bulan selesai").fill("2025-01");
    await page.getByRole("button", { name: "Kustom", exact: true }).click();
    await page.getByRole("alert").filter({ hasText: "Bulan mulai harus sebelum atau sama dengan bulan selesai." }).waitFor();
    assert.equal(new URL(page.url()).searchParams.has("from"), false, "an invalid range never reaches the URL");
    await page.getByLabel("Bulan mulai").fill("2025-01");
    await page.getByLabel("Bulan selesai").fill("2025-03");
    await page.getByRole("button", { name: "Kustom", exact: true }).click();
    await page.getByText("Rentang bulan Jan 25 sampai Mar 25").waitFor();
    assert.equal(new URL(page.url()).searchParams.get("from"), "2025-01");
    assert.equal(await page.getByRole("button", { name: "Kustom", exact: true }).getAttribute("aria-pressed"), "true");
    assert.equal(await page.getByRole("alert").filter({ hasText: "Bulan mulai harus sebelum atau sama dengan bulan selesai." }).count(), 0, "a valid range clears the field error");
    await page.goBack({ waitUntil: "networkidle" });
    await page.waitForFunction(() => document.querySelector('input[name="from"]')?.value === "");
    assert.equal(await page.getByLabel("Bulan mulai").inputValue(), "", "the inputs follow the URL after Back");
    // One failing section does not blank the rest, and it retries on its own.
    merchantsFail = true;
    await page.getByRole("button", { name: "6 Bulan", exact: true }).click();
    await page.getByRole("alert").filter({ hasText: "Analisis kalender belum dapat dimuat" }).waitFor();
    assert.equal(await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).isVisible(), true, "the cashflow chart survives a failed merchants request");
    assert.equal(await page.getByRole("rowheader", { name: /Sep 26/ }).count(), 1, "the monthly table survives too");
    merchantsFail = false;
    await page.getByRole("button", { name: "Coba lagi", exact: true }).click();
    await page.getByRole("alert").filter({ hasText: "Analisis kalender belum dapat dimuat" }).waitFor({ state: "detached" });
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
    noSalary = false; aiAvailable = true; stable = true;
    await page.goto(`${base}/analytics?view=cycle&cycle=2026-08-26`, { waitUntil: "networkidle" });
    await page.locator("#discussion > details > summary").click();
    await page.getByText("Tidak ada perubahan yang menonjol terhadap tiga siklus sebelumnya.", { exact: true }).waitFor();
    assert.equal(await page.locator(".kpi-cell:nth-child(3) dd").textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(0n));
    assert.equal(await page.locator(".insight-text p").count(), 1, "stable commentary is not expanded into filler");
    assert.equal(await page.locator("#changes tbody tr").count(), 1);
    assert.equal(requests.filter(request => request.path === "/api/v1/insights/generate").length, 0, "stable review never auto-generates commentary");

    // Requested URL, synthetic facts only. Default is scan-first; one explicit
    // action exposes the complete report without deleting any data.
    stable = false;
    await page.goto(`${base}/analytics?view=cycle&cycle=2026-09-25`, { waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Posisi siklus", exact: true }).waitFor();
    assert.equal(await page.locator(".kpi-cell[data-lead] dd").evaluate(element => element.scrollWidth > element.clientWidth + 1), false, "headline amount fits its metric cell");
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-09-25");
    await page.screenshot({ path: fileURLToPath(new URL(`${name}-september25-overview.png`, output)), fullPage: true, animations: "disabled" });
    assert.equal(await page.locator("#changes table").isVisible(), false);
    assert.equal(await page.locator("#discussion .insight-card").isVisible(), false);
    await page.evaluate(() => document.querySelectorAll(".cycle-review details").forEach(details => { details.open = true; }));
    assert.equal(await page.locator("#changes table").isVisible(), true);
    for (const id of ["merchants", "household", "discussion", "decisions"]) assert.equal(await page.locator(`#${id} > details`).evaluate(element => element.open), true);
    await page.evaluate(() => document.querySelectorAll(".cycle-review details").forEach(details => { details.open = true; }));
    assert.equal(await page.locator("#daily .review-daily table").locator("tbody tr").count(), 7, "all exact daily rows retained");
    const dailyTable = page.getByRole("region", { name: "Nilai pengeluaran harian, geser untuk semua kolom", exact: true });
    const dayName = "Lihat pengeluaran 25 Sep 2026";
    const dayHref = "/transactions?from=2026-09-25&to=2026-09-25&status=CONFIRMED&type=SPENDING&cycle=2026-09-25";
    const dayLink = dailyTable.getByRole("link", { name: dayName, exact: true });
    assert.equal(await dayLink.getAttribute("href"), dayHref, "spending day row links to exactly that day");
    assert.equal(await dailyTable.getByRole("row").filter({ hasText: "26 Sep 2026" }).count(), 1, "zero-spend day remains visible");
    assert.equal(await dailyTable.getByRole("row").filter({ hasText: "26 Sep 2026" }).getByRole("link").count(), 0, "zero-spend day has no table link");
    const dayChart = page.locator(".cycle-spending-chart");
    assert.equal(await dayChart.getByRole("link", { name: dayName, exact: true }).getAttribute("href"), dayHref, "chart and table share the same drill");
    assert.equal(await dayChart.getByRole("link", { name: "Lihat pengeluaran 26 Sep 2026", exact: true }).count(), 0, "zero-spend day has no chart link");
    const rentChange = page.locator(".diverge > li").filter({ hasText: "Tempat Tinggal" });
    assert.match(await rentChange.textContent(), /naik.*1\.950\.000/, "a running cycle shows the served same-day change");
    assert.doesNotMatch(await rentChange.textContent(), /100%/);
    const rentRow = page.locator("#changes table tbody tr").filter({ hasText: "Tempat Tinggal" });
    assert.equal(await rentRow.locator("td").nth(6).textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(1950000n));
    assert.match(await rentRow.locator("td").nth(7).textContent(), /0%/);
    assert.equal(await page.locator("#changes table tbody tr").count(), 3, "all exact category comparisons retained");
    assert.equal(await page.locator("#household .household-list > li").count(), 2);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} complete report has no page overflow`);
    await page.screenshot({ path: fileURLToPath(new URL(`${name}-september25-complete.png`, output)), fullPage: true, animations: "disabled" });
    generationStatus = 429;
    await page.getByRole("button", { name: "Buat pembahasan", exact: true }).click();
    await page.getByText("Batas satu pembahasan per jam. Coba lagi setelah jeda satu jam dari permintaan terakhir.", { exact: true }).waitFor();
    generationStatus = 409;
    await page.locator("#discussion").getByRole("button", { name: "Coba lagi", exact: true }).click();
    await page.getByText("Pembahasan sebelumnya masih diproses. Tunggu sampai selesai.", { exact: true }).waitFor();
    await page.keyboard.press("Tab");
    await dayLink.focus();
    assert.equal(await dayLink.evaluate(element => element === document.activeElement && getComputedStyle(element).outlineStyle !== "none"), true, "daily row has visible keyboard focus");
    await page.keyboard.press("Enter");
    const dayReturn = page.getByRole("link", { name: "Kembali ke tinjauan siklus", exact: true });
    await dayReturn.waitFor();
    assert.equal(new URL(page.url()).search, dayHref.slice(dayHref.indexOf("?")), "daily row navigates with the same-day query");
    await dayReturn.click();
    await dayChart.getByRole("link", { name: dayName, exact: true }).waitFor();
    const chartLink = dayChart.getByRole("link", { name: dayName, exact: true });
    await chartLink.focus();
    await page.keyboard.press("Tab");
    assert.equal(await dayChart.getByRole("link", { name: "Lihat pengeluaran 27 Sep 2026", exact: true }).evaluate(element => element === document.activeElement), true, "Tab reaches the next spending day, skipping zero");
    await chartLink.focus();
    assert.equal(await chartLink.evaluate(element => getComputedStyle(element).outlineStyle !== "none"), true, "chart day has visible keyboard focus");
    await page.keyboard.press("Enter");
    await dayReturn.waitFor();
    assert.equal(new URL(page.url()).search, dayHref.slice(dayHref.indexOf("?")), "chart link works by keyboard");
    await dayReturn.click();
    await dayChart.getByRole("link", { name: dayName, exact: true }).click();
    await dayReturn.waitFor();
    assert.equal(new URL(page.url()).search, dayHref.slice(dayHref.indexOf("?")), "chart link works by pointer");
    await page.goto(`${base}/analytics?view=calendar`, { waitUntil: "networkidle" });
    await monthTable.getByRole("link", { name: "Lihat pengeluaran Agu 26", exact: true }).click();
    await page.getByRole("heading", { name: "Riwayat Transaksi", exact: true }).waitFor();
    assert.equal(new URL(page.url()).search, "?from=2026-08-01&to=2026-08-31&status=CONFIRMED&type=SPENDING&cycle=", "direct calendar entry keeps month bounds without guessing a salary anchor");
    assert.equal(await page.getByRole("link", { name: "Kembali ke tinjauan siklus", exact: true }).count(), 0, "no invalid cycle backlink without a reviewed cycle");
    assert.deepEqual(errors, [], `${name} runtime errors`);
    console.log(`Analytics review PASS: ${name}, including daily chart/table and active/empty month drills`);
    await page.close();
  }
} finally {
  await browser?.close();
  server.kill("SIGTERM");
}
