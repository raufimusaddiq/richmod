import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { chromium } from "playwright";
import { cycleCommentary, cycleFacts, stableCycleFacts } from "../tests/fixtures/cycle-review.mjs";

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
        body = [{ period: "2026-08", income: "12000000", expense: "2800000", netCashflow: "9200000" }, { period: "2026-09", income: "12000000", expense: "4200000", netCashflow: "7800000" }];
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
    const explanation = page.locator("#position .section-title .review-explainer");
    assert.equal(await explanation.locator("p").isVisible(), false, "explanation is secondary to the figures");
    await explanation.locator("summary").focus();
    await page.keyboard.press("Enter");
    assert.equal(await explanation.locator("p").isVisible(), true, "native disclosure is keyboard accessible");
    await page.keyboard.press("Enter");
    assert.equal(await explanation.locator("p").isVisible(), false);
    assert.equal(await page.locator(".cycle-footnote > span").isVisible(), true, "refund amount stays visible");
    await page.evaluate(() => document.fonts.ready);
    assert.equal(await page.evaluate(() => document.fonts.check('500 16px "Fraunces"') && document.fonts.check('400 14px "Inter"')), true, "brand fonts loaded");
    assert.equal(await page.locator(".cycle-net dt").textContent(), "Pengeluaran bersih sejauh ini", "a running cycle leads with spending, not a net cashflow that looks large on day 1");
    assert.equal(await page.locator(".cycle-net dd").textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(4200000n));
    assert.equal(await page.locator("#position").getByText("Arus kas bersih sejauh ini", { exact: true }).isVisible(), true, "net cashflow stays visible as a regular metric");
    assert.equal(requests.filter(request => request.method === "POST").length, 0, "opening review never invokes generation");
    assert.equal(await page.getByRole("button", { name: "Tinjau siklus ini", exact: true }).count(), 0, "active cycle cannot enter closed-cycle meeting");
    assert.equal(await page.getByRole("button", { name: "Simpan keputusan", exact: true }).count(), 0, "active cycle has no save action");
    assert.equal(salaryCalls, 1, "explicit cycle uses one facts request");
    assert.equal(await page.getByText("Historical advice should never display.").count(), 0);
    assert.equal(await page.getByText("Stale cutoff must never display.").count(), 0);
    for (const id of ["drivers", "destinations", "household", "discussion", "decisions"]) {
      assert.equal(await page.locator(`#${id} > details`).evaluate(element => element.open), false, `${id} is available on demand, not a wall of text`);
    }
    assert.equal(await page.locator("#quality").getByRole("link", { name: "Buka Inbox" }).isVisible(), true, "blockers stay actionable without expansion");
    assert.equal(await page.locator(".comparison-bars > div").count(), 4);
    assert.equal(await page.locator(".change-ranking > li").count(), 3);
    const completeComparison = page.locator("#changes > details");
    assert.equal(await completeComparison.evaluate(element => element.open), false, "detail perubahan is secondary to the ledger and matrix");
    await completeComparison.locator(":scope > summary").focus();
    await page.keyboard.press("Enter");
    assert.equal(await completeComparison.evaluate(element => element.open), true, "detail perubahan is keyboard accessible");
    const fullComparison = page.locator("#changes details.review-daily");
    assert.equal(await fullComparison.evaluate(element => element.open), false);
    await fullComparison.locator(":scope > summary").focus();
    await page.keyboard.press("Enter");
    assert.equal(await fullComparison.evaluate(element => element.open), true, "complete comparison is keyboard accessible");
    assert.equal(await page.getByRole("columnheader", { name: "Selisih vs hari yang sama", exact: true }).isVisible(), true);
    assert.equal(await page.getByRole("columnheader", { name: "Sebelumnya · penuh", exact: true }).isVisible(), true);
    await page.getByRole("region", { name: "Perbandingan kategori lengkap, geser untuk semua kolom", exact: true }).focus();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} expanded comparison has no page overflow`);
    if (name === "desktop") await page.screenshot({ path: new URL("readme-analytics.png", output).pathname, fullPage: false, animations: "disabled" });
    assert.equal(await page.locator("#changes tbody tr").count(), 3);
    assert.equal(await page.locator("#changes tbody tr").nth(1).locator("td").nth(2).textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(1350000n), "previous outlier does not hide recent median");
    // Detail perubahan stays open: the category buttons below live inside it.
    await page.locator("#discussion > details > summary").click();
    await page.getByRole("heading", { name: "Pembahasan siklus terpilih" }).waitFor();
    await page.locator("#discussion > details > summary").click();
    const groceries = page.getByRole("button", { name: "Belanja rumah", exact: true });
    await page.keyboard.press("Tab");
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

    // Cycle ledger: one column per served cycle; selection is a URL change and the
    // reviewed cycle survives a Calendar visit. Numbers are served, never derived.
    await page.goto(`${base}/analytics?view=cycle&cycle=2026-09-01`, { waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Siklus ke siklus", exact: true }).waitFor();
    const cycleLedger = page.locator("#ledger");
    assert.equal(await cycleLedger.locator("thead th button").count(), 6, "one column per served cycle");
    assert.equal(await cycleLedger.locator('thead th[aria-current="true"] button').getAttribute("aria-pressed"), "true");
    assert.match(await cycleLedger.locator(".ledger-verdict").textContent(), /Siklus sebelumnya di hari yang sama/);
    assert.equal(await cycleLedger.locator("tbody tr").count(), 7, "bars, net cashflow, change, three categories, and Lainnya");
    assert.equal(await cycleLedger.locator("tbody tr").nth(6).locator("th").textContent(), "Lainnya", "the served remainder closes the matrix");
    if (name === "mobile" || name === "small-mobile") {
      // Phones: whole cycle columns beside the sticky label; none cut at its edge; labels fit; it scrolls and snaps.
      const ribbon = await page.evaluate(() => {
        const figure = document.querySelector("#ledger .ledger-figure");
        const label = figure.querySelector("thead th:first-child").getBoundingClientRect();
        const box = figure.getBoundingClientRect();
        const heads = [...figure.querySelectorAll("thead th:not(:first-child)")].map(element => element.getBoundingClientRect());
        return {
          scrolls: figure.scrollWidth > figure.clientWidth + 1,
          fullyVisible: heads.filter(rect => rect.left >= label.right - 1 && rect.right <= box.right - 1).length,
          straddling: heads.filter(rect => rect.left < label.right - 1 && rect.right > label.right + 1).length,
          labelOverflow: [...figure.querySelectorAll("tbody th")].filter(cell => cell.scrollWidth > cell.clientWidth + 1).map(cell => cell.textContent.trim()),
          snap: getComputedStyle(figure).scrollSnapType,
        };
      });
      assert.equal(ribbon.scrolls, true, `${name} the ribbon scrolls sideways instead of squeezing six cycles`);
      assert.equal(ribbon.straddling, 0, `${name} no cycle column is cut at the label edge`);
      assert.equal(ribbon.fullyVisible, name === "mobile" ? 4 : 3, `${name} shows whole cycle columns`);
      assert.deepEqual(ribbon.labelOverflow, [], `${name} row labels fit their column`);
      assert.match(ribbon.snap, /x mandatory/, `${name} columns snap`);
    }
    const matrixRow = cycleLedger.getByRole("button", { name: "Buka bukti Belanja rumah", exact: true });
    assert.equal(await matrixRow.count(), 1);
    assert.equal(await cycleLedger.locator("tbody tr").nth(3).locator("td").last().textContent(), "2,1", "the selected column shows the served category amount");
    await matrixRow.focus();
    await page.keyboard.press("Enter");
    await page.getByRole("heading", { name: "Belanja rumah", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("category"), "11111111-1111-4111-8111-111111111111");
    assert.equal(await page.locator("#drivers > details").evaluate(element => element.open), true, "a matrix row opens the category evidence");
    assert.equal(await matrixRow.evaluate(element => element === document.activeElement), true, "choosing a category leaves focus on the row instead of jumping to the evidence");
    const selection = page.locator(".ledger-selection");
    assert.match(await selection.textContent(), /Belanja rumah/, "the choice is announced next to the ledger");
    await page.locator("#drivers > details > summary").click();
    assert.equal(await page.locator("#drivers > details").evaluate(element => element.open), false, "the reader can collapse the evidence");
    await selection.getByRole("link", { name: "Lihat bukti", exact: true }).click();
    assert.equal(await page.locator("#drivers > details").evaluate(element => element.open), true, "Lihat bukti reopens a collapsed panel");
    await page.waitForFunction(() => document.activeElement?.id === "drivers-title");
    assert.equal(await page.locator("#category-drivers").getByRole("link", { name: "Kembali ke ringkasan siklus", exact: true }).isVisible(), true, "the evidence links back to the ledger");
    await matrixRow.focus();
    await page.keyboard.press("Enter");
    await page.waitForFunction(() => !new URL(location.href).searchParams.has("category"));
    assert.equal(await matrixRow.getAttribute("aria-pressed"), "false", "choosing the selected category again clears it");
    assert.equal(await page.locator(".cycle-pace-chart").isVisible(), true, "pace is its own single-series chart");
    const charts = await page.evaluate(() => { const [a, b] = [".shape-charts > :nth-child(1)", ".shape-charts > :nth-child(2)"].map(selector => document.querySelector(selector)?.getBoundingClientRect()); return { sameRow: Boolean(a && b) && Math.abs(a.top - b.top) < 4 && a.right <= b.left + 1, section: Math.round(document.querySelector("#spending-shape")?.getBoundingClientRect().width ?? 0) }; });
    if (name !== "tablet") assert.equal(charts.sameRow, name === "desktop", `${name} daily charts ${name === "desktop" ? "share a row" : "stack"} (section ${charts.section}px)`);
    assert.equal(await page.locator(".cycle-pace-chart .recharts-line-curve").count(), 3, "the selected cycle, the previous cycle and the median are drawn as curves");
    assert.equal(await page.getByRole("button", { name: "Siklus berikutnya ›", exact: true }).isDisabled(), true, "the active cycle has no newer neighbour");
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} ledger has no page overflow`);
    await page.screenshot({ path: new URL(`${name}-ledger.png`, output).pathname, fullPage: true, animations: "disabled" });
    await cycleLedger.locator("thead th button", { hasText: "26 Agu" }).click();
    await page.getByRole("heading", { name: "Pola pengeluaran siklus terpilih" }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-08-26");
    assert.match(await cycleLedger.locator(".ledger-verdict").textContent(), /Dibanding siklus sebelumnya/);
    assert.equal(await page.getByRole("button", { name: "‹ Siklus sebelumnya", exact: true }).isDisabled(), true, "the served list has no older cycle");
    await page.getByRole("button", { name: "Siklus berikutnya ›", exact: true }).click();
    await page.getByRole("heading", { name: "Pola pengeluaran siklus ini" }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-09-01");
    await page.getByRole("button", { name: "‹ Siklus sebelumnya", exact: true }).click();
    await page.getByRole("heading", { name: "Pola pengeluaran siklus terpilih" }).waitFor();
    await page.getByRole("button", { name: "Kalender", exact: true }).click();
    await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-08-26", "Calendar keeps the reviewed cycle in the URL");
    assert.equal(await page.getByRole("heading", { name: "Analisis kalender", exact: true }).count(), 1, "the page title follows the view");
    await page.getByRole("button", { name: "Siklus Gaji", exact: true }).click();
    await page.getByRole("heading", { name: "Siklus ke siklus", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-08-26", "returning from Calendar keeps the reviewed cycle");

    olderCommentary = true;
    await page.goto(`${base}/analytics?view=cycle&cycle=2026-09-01`, { waitUntil: "networkidle" });
    await page.locator("#discussion > details > summary").click();
    await page.getByText("Older daily snapshot is visible.", { exact: true }).waitFor();
    assert.match(await page.locator("#discussion .insight-muted").textContent(), /Snapshot sebelumnya.*5 Sep 2026.*bukan data terkini/);
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
    await page.getByRole("heading", { name: "Pola pengeluaran siklus terpilih" }).waitFor();
    await page.locator("#discussion > details > summary").getByText("Belum tersedia", { exact: true }).waitFor();
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
    await page.getByRole("button", { name: "Batalkan keputusan", exact: true }).click();
    await page.locator("dialog[open]").getByRole("button", { name: "Ya, batalkan", exact: true }).click();
    await page.getByText("Keputusan dibatalkan. Riwayat tetap tersimpan.", { exact: true }).waitFor();
    await page.getByRole("button", { name: "Selesai meninjau", exact: true }).click();
    assert.equal(new URL(page.url()).searchParams.has("review"), false);
    assert.equal(await page.locator("#position").isVisible(), true);

    await page.getByRole("button", { name: "Kalender", exact: true }).click();
    await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).waitFor();
    assert.equal(new URL(page.url()).searchParams.get("view"), "calendar");
    await page.getByRole("button", { name: "3 Bulan", exact: true }).click();
    assert.equal(new URL(page.url()).searchParams.get("range"), "3");
    // Calendar hygiene: months are formatted, and a custom range is validated beside its fields instead of failing after a request.
    assert.equal(await page.getByRole("rowheader", { name: /Sep 26/ }).count(), 1, "months are formatted, not raw YYYY-MM");
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
    assert.equal(await page.locator(".cycle-net dd").textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(0n));
    assert.equal(await page.locator(".insight-text p").count(), 1, "stable commentary is not expanded into filler");
    assert.equal(await page.locator("#changes tbody tr").count(), 1);
    assert.equal(requests.filter(request => request.path === "/api/v1/insights/generate").length, 0, "stable review never auto-generates commentary");

    // Requested URL, synthetic facts only. Default is scan-first; one explicit
    // action exposes the complete report without deleting any data.
    stable = false;
    await page.goto(`${base}/analytics?view=cycle&cycle=2026-09-25`, { waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Posisi siklus", exact: true }).waitFor();
    assert.equal(await page.locator(".cycle-net dd").evaluate(element => element.scrollWidth > element.clientWidth + 1), false, "headline amount fits its metric cell");
    assert.equal(new URL(page.url()).searchParams.get("cycle"), "2026-09-25");
    await page.screenshot({ path: new URL(`${name}-september25-overview.png`, output).pathname, fullPage: true, animations: "disabled" });
    assert.equal(await page.locator("#changes table").isVisible(), false);
    assert.equal(await page.locator("#discussion .insight-card").isVisible(), false);
    await page.getByRole("button", { name: "Buka semua detail", exact: true }).click();
    assert.equal(await page.locator("#changes table").isVisible(), true);
    for (const id of ["drivers", "destinations", "household", "discussion", "decisions"]) assert.equal(await page.locator(`#${id} > details`).evaluate(element => element.open), true);
    await page.getByRole("button", { name: "Tutup semua detail", exact: true }).click();
    for (const id of ["drivers", "destinations", "household", "discussion", "decisions"]) assert.equal(await page.locator(`#${id} > details`).evaluate(element => element.open), false, "the same button closes everything it opened");
    await page.getByRole("button", { name: "Buka semua detail", exact: true }).click();
    assert.equal(await page.locator(".review-daily table").first().locator("tbody tr").count(), 7, "all exact daily rows retained");
    const rentRanking = page.locator(".change-ranking > li").filter({ has: page.getByRole("button", { name: "Tempat Tinggal", exact: true }) });
    assert.match(await rentRanking.textContent(), /0%/);
    assert.doesNotMatch(await rentRanking.textContent(), /100%/);
    const rentRow = page.locator("#changes table tbody tr").filter({ hasText: "Tempat Tinggal" });
    assert.equal(await rentRow.locator("td").nth(6).textContent(), new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(1950000n));
    assert.match(await rentRow.locator("td").nth(7).textContent(), /0%/);
    assert.equal(await page.locator("#changes table tbody tr").count(), 3, "all exact category comparisons retained");
    assert.equal(await page.locator("#household .review-attribution > div").count(), 2);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} complete report has no page overflow`);
    await page.screenshot({ path: new URL(`${name}-september25-complete.png`, output).pathname, fullPage: true, animations: "disabled" });
    generationStatus = 429;
    await page.getByRole("button", { name: "Buat pembahasan", exact: true }).click();
    await page.getByText("Batas satu pembahasan per jam. Coba lagi setelah jeda satu jam dari permintaan terakhir.", { exact: true }).waitFor();
    generationStatus = 409;
    await page.locator("#discussion").getByRole("button", { name: "Coba lagi", exact: true }).click();
    await page.getByText("Pembahasan sebelumnya masih diproses. Tunggu sampai selesai.", { exact: true }).waitFor();
    assert.deepEqual(errors, [], `${name} runtime errors`);
    await page.close();
  }
} finally {
  await browser?.close();
  server.kill("SIGTERM");
}
