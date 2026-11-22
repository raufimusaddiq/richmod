import { test, expect, FIXED_NOW } from "../support/fixtures.js";
import { idr } from "../support/data.js";
import { cycleReviewApi, cycleUser } from "../support/cycleApi.js";
import { defaultRange } from "../../app/lib/calendarReview.js";

test.use({ user: cycleUser });

const CYCLE = "/analytics?view=cycle&cycle=2026-09-01";
const CLOSED = "/analytics?view=cycle&cycle=2026-08-26";
const GROCERIES = "11111111-1111-4111-8111-111111111111";
const MERCHANT = "22222222-2222-4222-8222-222222222222";
const DAY_HREF = "/transactions?from=2026-09-25&to=2026-09-25&status=CONFIRMED&type=SPENDING&cycle=2026-09-25";

const overflows = page => page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1);
const searchOf = page => new URL(page.url()).searchParams;

async function openDisclosure(page, selector, content) {
  const details = page.locator(selector).first();
  await details.waitFor({ state: "visible" });
  await page.waitForFunction(([sel, contentSel]) => {
    const el = document.querySelector(sel);
    if (!el) return false;
    el.open = true;
    const body = document.querySelector(contentSel);
    return el.open === true && Boolean(body && body.getClientRects().length > 0);
  }, [selector, content]);
  await page.locator(content).first().waitFor({ state: "visible" });
}

async function visit(page, path) {
  await page.goto(path, { waitUntil: "networkidle" });
}

let flags;

test.beforeEach(async ({ api }) => {
  flags = cycleReviewApi(api);
});

test("an active cycle opens calm: four KPI tiles, takeaways, exact amounts and nothing generated", async ({ page, api }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  await expect(page.getByRole("heading", { name: "Laporan", level: 1 }), "the page is Laporan").toBeVisible();
  const tabs = page.getByRole("tablist", { name: "Jenis laporan" }).getByRole("tab");
  expect(await tabs.allTextContents()).toEqual(["Arus kas", "Kekayaan"]);
  await expect(tabs.nth(0)).toHaveAttribute("aria-selected", "true");

  const explanation = page.locator("#spending-shape .review-daily");
  await expect(explanation.locator("table"), "values are secondary to the chart").toBeHidden();
  await explanation.locator("summary").focus();
  await page.keyboard.press("Enter");
  await expect(explanation.locator("table"), "native disclosure is keyboard accessible").toBeVisible();
  await page.keyboard.press("Enter");
  await expect(explanation.locator("table")).toBeHidden();

  await expect(page.locator(".kpi-cell"), "the KPI strip has four calm tiles").toHaveCount(4);
  for (const id of ["where", "history", "changes", "spending-shape"]) expect((await page.locator(`#${id} .takeaway`).textContent()).length, `${id} shows a takeaway sentence`).toBeGreaterThan(10);
  await expect(page.locator(".kpi-note"), "refund/explainer line is removed").toHaveCount(0);
  await page.evaluate(() => document.fonts.ready);
  expect(await page.evaluate(() => document.fonts.check('500 16px "Fraunces"') && document.fonts.check('400 14px "Inter"')), "brand fonts loaded").toBe(true);
  expect((await page.locator(".kpi-cell[data-lead] dt").textContent()).startsWith("Pengeluaran bersih"), "a running cycle leads with spending, not a net cashflow that looks large on day 1").toBe(true);
  await expect(page.locator(".kpi-cell[data-lead] dd")).toHaveText(idr(4200000));
  await expect(page.locator("#position").getByText("Arus kas bersih sejauh ini", { exact: true }), "net cashflow stays visible as a regular metric").toBeVisible();

  expect(api.requests.filter(request => request.method === "POST"), "opening review never invokes generation").toHaveLength(0);
  await expect(page.getByRole("button", { name: "Tinjau siklus ini", exact: true }), "active cycle cannot enter closed-cycle meeting").toHaveCount(0);
  await expect(page.getByRole("button", { name: "Simpan keputusan", exact: true }), "active cycle has no save action").toHaveCount(0);
  expect(flags.salaryCalls, "explicit cycle uses one facts request").toBe(1);
  await expect(page.getByText("Historical advice should never display.")).toHaveCount(0);
  await expect(page.getByText("Stale cutoff must never display.")).toHaveCount(0);
  for (const id of ["daily", "family-talk"]) expect(await page.locator(`#${id} > details`).evaluate(element => element.open), `${id} is available on demand, not a wall of text`).toBe(false);

  await expect(page.locator("#where .where-delta"), "Ke mana uangnya omits delta column").toHaveCount(0);
  await expect(page.locator("#where .where-share"), "Ke mana uangnya shows share percentages").toHaveCount(3);
  await expect(page.locator("#quality").getByRole("link", { name: "Buka Inbox" }), "blockers stay actionable without expansion").toBeVisible();
  await expect(page.locator(".comparison-bars > div")).toHaveCount(4);
  await expect(page.locator(".diverge > li"), "Yang berubah draws the served changes (at most five)").toHaveCount(3);
  await expect(page.locator(".change-ranking"), "the 25-row list is gone").toHaveCount(0);
  await expect(page.locator("#where .where-list > li"), "three ranked categories; an empty remainder is not drawn").toHaveCount(3);
  await expect(page.locator("#where .where-total strong"), "the list total is the served net expense").toHaveText(idr(4200000));
  expect(await page.locator("#where .where-list > li:not([data-other]) .where-amount").allTextContents(), "ranking order is the served order").toEqual([2100000, 1400000, 700000].map(idr));
  await expect(page.locator("#changes"), "direction is worded neutrally, never as thrift or waste").toContainText("◀ lebih rendah");
  await expect(page.locator("#changes")).toContainText("lebih tinggi ▶");
  await expect(page.locator("#main-content")).not.toContainText(/hemat|boros/i);
  expect(await overflows(page)).toBe(false);
});

test("the full category comparison is keyboard reachable and keeps exact figures", async ({ page }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  const full = page.locator("#changes details.review-daily");
  expect(await full.evaluate(element => element.open), "the full comparison is secondary to the two cards").toBe(false);
  await full.locator(":scope > summary").focus();
  await page.keyboard.press("Enter");
  expect(await full.evaluate(element => element.open), "complete comparison is keyboard accessible").toBe(true);
  await expect(page.getByRole("columnheader", { name: "Selisih vs hari yang sama", exact: true })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Sebelumnya · penuh", exact: true })).toBeVisible();
  await page.getByRole("region", { name: "Perbandingan kategori lengkap, geser untuk semua kolom", exact: true }).focus();
  expect(await overflows(page), "expanded comparison has no page overflow").toBe(false);
  await expect(page.locator("#changes tbody tr")).toHaveCount(3);
  await expect(page.locator("#changes tbody tr").nth(1).locator("td").nth(2), "previous outlier does not hide recent median").toHaveText(idr(1350000));
  await page.locator("#family-talk > details > summary").click();
  await page.getByRole("heading", { name: "Pembahasan siklus terpilih" }).waitFor();
});

test("the category drawer is a modal that traps focus, returns it, and follows Back", async ({ page }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  const groceries = page.locator("#where").getByRole("button", { name: /^Belanja rumah/ });
  const drawer = page.getByRole("dialog", { name: "Belanja rumah", exact: true });
  await page.keyboard.press("Tab");
  await groceries.focus();
  expect(await groceries.evaluate(element => getComputedStyle(element).outlineStyle)).not.toBe("none");
  const rankedAmount = await groceries.locator(".where-amount").textContent();
  await page.keyboard.press("Enter");
  await drawer.waitFor();
  expect(searchOf(page).get("category")).toBe(GROCERIES);
  expect(await drawer.evaluate(element => element.contains(document.activeElement)), "focus moves into the drawer").toBe(true);
  await expect(drawer.getByRole("button", { name: "Tutup rincian kategori" }), "the drawer has a close button as well as Escape and Back").toBeVisible();
  await expect(drawer.locator(".detail-amount"), "drill-down total equals the row it was opened from").toHaveText(rankedAmount);
  for (let index = 0; index < 40; index += 1) await page.keyboard.press("Tab");
  expect(await drawer.evaluate(element => element.contains(document.activeElement)), "Tab stays inside the modal").toBe(true);
  await page.keyboard.press("Escape");
  await drawer.waitFor({ state: "detached" });
  await page.waitForFunction(() => !new URL(location.href).searchParams.has("category"));
  expect(await groceries.evaluate(element => element === document.activeElement), "Escape returns focus to the row that opened it").toBe(true);
  await groceries.click();
  await drawer.waitFor();
  await page.goBack();
  await drawer.waitFor({ state: "detached" });
  expect(searchOf(page).has("category"), "Back closes the drawer").toBe(false);
});

test("the category drawer links to its merchant, its transactions and the scoped ledger and comes back", async ({ page }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  const groceries = page.locator("#where").getByRole("button", { name: /^Belanja rumah/ });
  const drawer = page.getByRole("dialog", { name: "Belanja rumah", exact: true });
  await groceries.click();
  await drawer.waitFor();
  expect(flags.salaryCalls, "category drill-down reuses returned facts").toBe(1);
  await expect(drawer.locator(".drawer-list li")).toHaveCount(3);
  const merchantHref = new URL(await drawer.locator("a").filter({ hasText: "Pasar Keluarga" }).first().getAttribute("href"), page.url());
  expect(merchantHref.searchParams.get("merchantId")).toBe(MERCHANT);
  expect(merchantHref.searchParams.get("to")).toBe("2026-09-06");
  expect(merchantHref.searchParams.get("type")).toBe("SPENDING");
  const transaction = drawer.getByRole("link", { name: /^Buka transaksi/ }).first();
  expect(new URL(await transaction.getAttribute("href"), page.url()).searchParams.get("id")).toBe("33333333-3333-4333-8333-333333333333");
  await transaction.click();
  await page.getByRole("dialog", { name: "Detail transaksi", exact: true }).waitFor();
  await page.getByRole("heading", { name: "Pasar Keluarga", exact: true }).waitFor();
  await expect(page.locator(".transaction-row"), "evidence link scopes the ledger to one row").toHaveCount(1);
  await page.goBack({ waitUntil: "networkidle" });
  await drawer.waitFor();
  await page.getByRole("link", { name: "Lihat transaksi Belanja rumah dalam periode ini" }).click();
  await page.getByRole("link", { name: "Kembali ke tinjauan siklus" }).waitFor();
  await page.goBack({ waitUntil: "networkidle" });
  await drawer.waitFor();
  expect(searchOf(page).get("cycle")).toBe("2026-09-01");
  expect(await overflows(page), "review has no page overflow").toBe(false);
  await page.keyboard.press("Escape");
  await drawer.waitFor({ state: "detached" });
});

test("the daily chart tooltip names net spending and refunds", async ({ page }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  await openDisclosure(page, "#daily > details", ".cycle-spending-chart");
  await page.locator(".cycle-spending-chart .recharts-bar-rectangle").first().hover();
  await page.locator(".chart-tooltip").waitFor();
  await expect(page.locator(".chart-tooltip")).toHaveText(/Pengeluaran bersih.*Rp.*Refund.*Rp/);
});

test("the category matrix marks the running column, switches same-day and full, and opens evidence by keyboard", async ({ page }) => {
  await visit(page, CYCLE);
  await openDisclosure(page, "#history details", ".heat-table");
  await page.getByRole("heading", { name: "Angka per kategori", exact: true }).waitFor();
  const heat = page.locator(".heat-table");
  await expect(heat.locator("thead th:not(:first-child):not(.heat-delta-head)"), "one column per served cycle").toHaveCount(6);
  await expect(heat.locator('thead th[aria-current="true"]'), "the selected cycle is marked").toHaveCount(1);
  const sameDay = page.getByRole("button", { name: "Hari ke-6 yang sama", exact: true });
  const fullCycle = page.getByRole("button", { name: "Siklus penuh", exact: true });
  await expect(sameDay, "same-day is the default while the cycle runs").toHaveAttribute("aria-pressed", "true");
  await expect(fullCycle).toHaveAttribute("aria-pressed", "false");
  await expect(heat.locator("thead th small").filter({ hasText: "hari 1–6" }), "every column names its cut").toHaveCount(6);
  await expect(heat.locator("tbody tr"), "three categories plus the served remainder").toHaveCount(4);
  await expect(heat.locator("tbody tr").last().locator("th")).toHaveText(/^Kategori lain/);
  await expect(heat.locator("tfoot th")).toHaveText("Total pengeluaran bersih");
  await fullCycle.click();
  await expect(fullCycle).toHaveAttribute("aria-pressed", "true");
  await expect(heat.locator("thead th[data-running]"), "the running column is flagged in full mode").toHaveCount(1);
  await expect(heat.locator("td[data-running]"), "every row and the total mark the running column").toHaveCount(5);
  const firstRunning = heat.locator("td[data-running] .heat-cell").first();
  expect((await firstRunning.evaluate(element => element.style.getPropertyValue("--tint"))).trim(), "the running column carries no tint").toBe("0");
  expect(await firstRunning.evaluate(element => getComputedStyle(element).borderStyle), "the running column is dashed instead of tinted").toBe("dashed");
  expect(await heat.locator("td[data-running] .heat-cell").evaluateAll(cells => cells.map(cell => cell.style.getPropertyValue("--tint").trim())), "no tinted cell sits in the running column").toEqual(["0", "0", "0", "0"]);
  await sameDay.click();
  await expect(sameDay).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator(".kpi-cell[data-lead]")).toHaveText(/vs siklus lalu di hari ke-6/);
  await page.getByRole("heading", { name: "Siklus ke siklus", exact: true }).waitFor();
  await expect(page.locator("#history .history-cols > li"), "the cycle columns card draws every served cycle").toHaveCount(6);
  const matrixRow = heat.getByRole("button", { name: "Buka bukti Belanja rumah", exact: true });
  await expect(matrixRow).toHaveCount(1);
  await expect(heat.locator("tbody tr").first().locator("td.heat-amount").last(), "the selected column shows the served category amount").toHaveText("2,1");
  await matrixRow.focus();
  await page.keyboard.press("Enter");
  await page.getByRole("dialog", { name: "Belanja rumah", exact: true }).waitFor();
  expect(searchOf(page).get("category")).toBe(GROCERIES);
  await page.keyboard.press("Escape");
  await page.getByRole("dialog", { name: "Belanja rumah", exact: true }).waitFor({ state: "detached" });
  await page.waitForFunction(() => !new URL(location.href).searchParams.has("category"));
  expect(await matrixRow.evaluate(element => element === document.activeElement), "closing the drawer returns focus to the matrix row").toBe(true);
});

test("the matrix follows the selected cycle: a closed cycle has no same-day toggle and the active cycle re-seeds it", async ({ page }) => {
  await visit(page, CYCLE);
  await openDisclosure(page, "#history details", ".heat-table");
  await page.getByLabel("Siklus yang ditinjau").selectOption("2026-08-26");
  await page.waitForFunction(() => document.querySelector(".cycle-status strong")?.textContent.startsWith("Ditutup"));
  await openDisclosure(page, "#history details", ".heat-table");
  await page.waitForFunction(() => !document.querySelector(".heat-section .segmented"));
  await expect(page.getByRole("button", { name: "Siklus penuh", exact: true }), "a closed cycle has no same-day toggle").toHaveCount(0);
  await expect(page.locator(".heat-table thead th[data-running]"), "a closed cycle has no running column").toHaveCount(0);
  await page.getByLabel("Siklus yang ditinjau").selectOption("2026-09-01");
  await page.waitForFunction(() => document.querySelector(".cycle-status strong")?.textContent.startsWith("Berjalan"));
  await openDisclosure(page, "#history details", ".heat-section .segmented");
  await page.waitForFunction(() => document.querySelector('.heat-section .segmented button[aria-pressed="true"]')?.textContent.includes("Hari ke-6"));
  await expect(page.getByRole("button", { name: "Hari ke-6 yang sama", exact: true }), "returning to the active cycle re-seeds same-day").toHaveAttribute("aria-pressed", "true");
});

test("history columns share the category colours of the ranking", async ({ page }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  const column = page.locator("#history .history-cols > li").first();
  await expect(column.locator("em"), "a history column shows a served amount").toHaveText(/[1-9]/);
  expect(await column.locator(".history-bar").evaluate(element => element.getBoundingClientRect().height), "the history bar has a real height").toBeGreaterThan(0);
  expect(await column.locator(".history-seg").count(), "the history bar splits into category segments").toBeGreaterThanOrEqual(1);
  const whereColor = await page.locator("#where .where-swatch").first().evaluate(el => getComputedStyle(el).backgroundColor);
  const historyColor = await page.locator("#history .history-cols > li").last().locator(".history-seg").first().evaluate(el => getComputedStyle(el).backgroundColor);
  expect(historyColor, "Siklus ke siklus uses the same category colours as Ke mana uangnya").toBe(whereColor);
});

test("the merchant ranking and the household split are exact and never write", async ({ page, api }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  await openDisclosure(page, "#where details", "#merchants table.merchant-rank");
  const merchants = page.locator("#merchants table.merchant-rank");
  for (const header of ["Merchant", "Transaksi", "Siklus ini", "Siklus lalu, hari yang sama", "Selisih"]) await expect(merchants.getByRole("columnheader", { name: header, exact: true }), `merchant column ${header}`).toHaveCount(1);
  await expect(merchants.locator("tbody tr"), "one row per served payee plus the remainder").toHaveCount(2);
  await expect(merchants.locator("tfoot th")).toHaveText("Total");
  await expect(merchants.locator("tfoot"), "the total equals the cycle's net expense").toHaveText(/Rp\s?4\.200\.000/);
  expect(new URL(await merchants.getByRole("link", { name: "Pasar Keluarga", exact: true }).getAttribute("href"), page.url()).searchParams.get("merchantId")).toBe(MERCHANT);

  await openDisclosure(page, "#family-talk > details", "#household .household-list");
  const household = page.locator("#household .household-list > li");
  await expect(household).toHaveCount(2);
  await expect(household.first(), "the automatic bucket comes first").toHaveText(/otomatis/);
  await expect(household.first(), "the served share is shown").toHaveText(/47,6%/);
  await expect(page.locator("#household .household-bar i")).toHaveCount(2);
  expect(api.requests.filter(request => request.method === "POST"), "opening the detail sections never writes").toHaveLength(0);
});

test("the pace chart draws three curves and the sections lay out per width", async ({ page }) => {
  const name = test.info().project.name;
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  await expect(page.locator(".cycle-pace-chart"), "pace is its own single-series chart").toBeVisible();
  await expect(page.locator(".pace-marker-note"), "a running cycle says today").toHaveText("Hari ini · hari ke-6");
  const rows = await page.evaluate(() => {
    const box = selector => document.querySelector(selector)?.getBoundingClientRect();
    const same = (x, y) => Boolean(x && y) && Math.abs(x.top - y.top) < 4 && x.right <= y.left + 1;
    return { where: same(box("#where"), box("#history")), changes: same(box("#changes"), box("#spending-shape")) };
  });
  if (name !== "tablet") {
    const shared = name === "desktop";
    expect(rows.where, `"Ke mana uangnya" and "Siklus ke siklus" ${shared ? "share a row" : "stack"}`).toBe(shared);
    expect(rows.changes, `"Yang berubah" and "Laju pengeluaran" ${shared ? "share a row" : "stack"}`).toBe(shared);
  }
  await expect(page.locator(".cycle-pace-chart .recharts-line-curve"), "the selected cycle, the previous cycle and the median are drawn as curves").toHaveCount(3);
  await expect(page.getByRole("button", { name: "Siklus berikutnya", exact: true }), "the active cycle has no newer neighbour").toBeDisabled();
  expect(await overflows(page), "ledger has no page overflow").toBe(false);
});

test("a closed cycle still lets a decision be recorded when the list fails, and cycles step back and forth", async ({ page }) => {
  flags.decisionsFail = true;
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  await page.getByLabel("Siklus yang ditinjau").selectOption("2026-08-26");
  await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
  expect(searchOf(page).get("cycle")).toBe("2026-08-26");
  await page.waitForFunction(() => document.querySelector(".kpi-cell[data-lead]")?.textContent.includes("(penuh)"));
  await expect(page.locator(".kpi-cell[data-lead]")).toHaveText(/vs siklus lalu .penuh./);
  await openDisclosure(page, "#family-talk > details", "#decisions form.decision-form");
  await page.getByRole("alert").filter({ hasText: "Keputusan belum dapat dimuat" }).waitFor();
  await expect(page.getByLabel("Keputusan baru untuk siklus ini", { exact: true }), "a failed list still allows recording a decision").toHaveCount(1);
  flags.decisionsFail = false;
  await openDisclosure(page, "#family-talk > details", "#decisions form.decision-form");
  await page.locator("#decisions").getByRole("button", { name: "Coba lagi", exact: true }).click();
  await page.locator("#decisions .decision-list, #decisions .empty").first().waitFor();
  await expect(page.getByRole("button", { name: "Siklus sebelumnya", exact: true }), "the served list has no older cycle").toBeDisabled();
  await page.getByRole("button", { name: "Siklus berikutnya", exact: true }).click();
  await page.waitForFunction(() => document.querySelector(".cycle-status strong")?.textContent.startsWith("Berjalan"));
  expect(searchOf(page).get("cycle")).toBe("2026-09-01");
  await page.getByRole("button", { name: "Siklus sebelumnya", exact: true }).click();
  await page.waitForFunction(() => document.querySelector(".cycle-status strong")?.textContent.startsWith("Ditutup"));
});

test("Laporan keeps the reviewed cycle across Calendar and switches views from the keyboard", async ({ page }) => {
  await visit(page, CLOSED);
  await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
  const tabs = page.getByRole("tablist", { name: "Jenis laporan" }).getByRole("tab");
  await page.getByRole("button", { name: "Kalender", exact: true }).click();
  await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).waitFor();
  expect(searchOf(page).get("cycle"), "Calendar keeps the reviewed cycle in the URL").toBe("2026-08-26");
  await expect(page.getByRole("heading", { name: "Laporan", level: 1, exact: true }), "Laporan keeps its title in Calendar mode").toHaveCount(1);
  await expect(tabs.nth(0), "Calendar is still Arus kas").toHaveAttribute("aria-selected", "true");
  await tabs.nth(0).focus();
  await page.keyboard.press("ArrowRight");
  await page.getByRole("heading", { name: "Kekayaan belum diatur", exact: true }).waitFor();
  expect(searchOf(page).get("view")).toBe("wealth");
  await expect(tabs.nth(1)).toHaveAttribute("aria-selected", "true");
  expect(await tabs.nth(1).evaluate(element => element === document.activeElement), "arrow keys move focus with the selection").toBe(true);
  await expect(page.getByRole("link", { name: "Kelola akun kekayaan" })).toHaveAttribute("href", "/household?tab=akun#wealth-accounts");
  expect(await overflows(page), "Kekayaan has no page overflow").toBe(false);
  await page.keyboard.press("Home");
  await page.locator(".kpi-cell").first().waitFor();
  expect(searchOf(page).get("view")).toBe("cycle");

  await page.goto("/wealth", { waitUntil: "networkidle" });
  await page.getByRole("heading", { name: "Kekayaan belum diatur", exact: true }).waitFor();
  await expect(page.getByRole("tab", { name: "Kekayaan" }), "/wealth renders Laporan in Kekayaan mode").toHaveAttribute("aria-selected", "true");

  await visit(page, CLOSED);
  await page.getByRole("button", { name: "Kalender", exact: true }).click();
  await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).waitFor();
  await page.getByRole("button", { name: "Siklus Gaji", exact: true }).click();
  await page.getByRole("heading", { name: "Pengeluaran harian", exact: true }).waitFor();
  expect(searchOf(page).get("cycle"), "returning from Calendar keeps the reviewed cycle").toBe("2026-08-26");
});

test("commentary shows a dated older snapshot and an hourly cap never erases it", async ({ page }) => {
  flags.olderCommentary = true;
  await visit(page, CYCLE);
  await page.locator("#family-talk > details > summary").click();
  await page.getByText("Older daily snapshot is visible.", { exact: true }).waitFor();
  await expect(page.locator("#discussion .insight-muted").first()).toHaveText(/Data pembahasan: .*5 Sep 2026.*bukan data terkini/);
  flags.generationStatus = 429;
  await page.getByRole("button", { name: "Perbarui pembahasan", exact: true }).click();
  await page.getByText("Batas satu pembahasan per jam. Coba lagi setelah jeda satu jam dari permintaan terakhir.", { exact: true }).waitFor();
  await expect(page.getByText("Older daily snapshot is visible.", { exact: true }), "hourly cap does not erase the dated snapshot").toBeVisible();
});

test("without AI every section is still there and nothing is posted", async ({ page, api }) => {
  flags.aiAvailable = false;
  await visit(page, CLOSED);
  await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
  await page.locator("#family-talk > details > summary").getByText(/bahan pembahasan belum tersedia/).waitFor();
  for (const id of ["position", "spending-shape", "where", "changes", "daily", "history", "family-talk", "household", "quality"]) await expect(page.locator(`#${id}`)).toHaveCount(1);
  expect(api.requests.filter(request => request.method === "POST")).toHaveLength(0);
  await expect(page.locator("#quality").getByRole("link", { name: "Buka Inbox" })).toHaveCount(1);
});

test("the closed-cycle meeting walks the steps, keeps a draft, and records decisions only on explicit save", async ({ page, api }) => {
  flags.aiAvailable = false;
  await visit(page, CLOSED);
  await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
  await page.getByRole("button", { name: "Tinjau siklus ini", exact: true }).click();
  await page.waitForFunction(() => document.activeElement?.id === "position-title");
  expect(searchOf(page).get("review")).toBe("position");
  expect(await page.locator("#position h2").evaluate(element => element === document.activeElement)).toBe(true);
  await expect(page.locator("#changes")).toBeHidden();
  const nav = page.getByRole("navigation", { name: "Langkah tinjauan siklus" });
  await expect(nav.getByRole("button")).toHaveCount(7);
  await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
  await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
  await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
  await page.getByRole("button", { name: "Belanja rumah", exact: true }).click();
  await page.getByRole("dialog", { name: "Belanja rumah", exact: true }).waitFor();
  expect(searchOf(page).get("review")).toBe("drivers");
  const meetingLedger = page.getByRole("link", { name: "Lihat transaksi Belanja rumah dalam periode ini" });
  expect(new URL(await meetingLedger.getAttribute("href"), page.url()).searchParams.get("review")).toBe("drivers");
  await meetingLedger.click();
  const returnLink = page.getByRole("link", { name: "Kembali ke tinjauan siklus", exact: true });
  await returnLink.waitFor();
  expect(new URL(await returnLink.getAttribute("href"), page.url()).searchParams.get("review")).toBe("drivers");
  await returnLink.click();
  await page.getByRole("dialog", { name: "Belanja rumah", exact: true }).waitFor();
  await page.keyboard.press("Escape");
  for (const [button, section] of [["5. Tindak lanjut", "quality"], ["6. Pembahasan", "discussion"], ["7. Keputusan", "decisions"]]) {
    await nav.getByRole("button", { name: button, exact: true }).click();
    await page.locator(`#${section}`).waitFor({ state: "visible" });
    await expect(page.locator(`#${section}`)).toBeVisible();
    expect(searchOf(page).get("review")).toBe(section);
  }
  await page.getByText("Jaga lebih banyak kas likuid.", { exact: true }).waitFor();
  const field = page.getByLabel("Keputusan baru untuk siklus ini", { exact: true });
  const draft = "Simpan lebih banyak kas untuk kebutuhan keluarga.";
  await field.fill(draft);
  expect(api.requests.filter(request => request.method === "POST"), "no automatic decision write or AI generation during meeting").toHaveLength(0);
  await page.getByRole("button", { name: "Langkah sebelumnya", exact: true }).click();
  await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
  await expect(field, "the draft survives stepping away").toHaveValue(draft);
  flags.failDecisionSave = true;
  await page.getByRole("button", { name: "Simpan keputusan", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "Keputusan belum dapat disimpan" }).waitFor();
  await expect(field, "a failed save keeps the draft").toHaveValue(draft);
  flags.failDecisionSave = false;
  await field.press("Control+Enter");
  await page.locator("#decisions .decision-list").getByText(draft, { exact: true }).waitFor();
  await expect(field).toHaveValue("");
  const saves = api.calls("/api/v1/analytics/cycle-decisions", "POST");
  expect(saves, "only explicit save and explicit retry").toHaveLength(2);
  for (const save of saves) expect(Object.keys(save.body).sort()).toEqual(["body", "cycleStart"]);
  expect(api.calls("/api/v1/insights/generate")).toHaveLength(0);
  expect(await overflows(page), "meeting has no overflow").toBe(false);
  await page.getByRole("button", { name: "Batalkan keputusan", exact: true }).click();
  await page.locator("dialog[open]").getByRole("button", { name: "Ya, batalkan", exact: true }).click();
  await page.getByText("Keputusan dibatalkan. Riwayat tetap tersimpan.", { exact: true }).waitFor();
  await page.getByRole("button", { name: "Selesai meninjau", exact: true }).click();
  await page.waitForFunction(() => !new URL(location.href).searchParams.has("review") && document.querySelector("#position")?.checkVisibility({ checkVisibilityCSS: true, checkOpacity: false }) === true);
  expect(searchOf(page).has("review")).toBe(false);
  await expect(page.locator("#position")).toBeVisible();
});

test("the calendar view validates ranges, links months by keyboard and survives a failed section", async ({ page }) => {
  await visit(page, CLOSED);
  await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
  await page.getByRole("button", { name: "Kalender", exact: true }).click();
  await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).waitFor();
  expect(searchOf(page).get("view")).toBe("calendar");
  await page.getByRole("button", { name: "3 Bulan", exact: true }).click();
  expect(searchOf(page).get("range")).toBe("3");
  expect(await page.locator(".dash-legend li").allTextContents(), "the legend follows the bar order").toEqual(expect.arrayContaining(["Pemasukan", "Pengeluaran"]));
  await expect(page.getByRole("group", { name: "Rentang bulan" })).toBeVisible();
  await expect(page.locator(".chart-unit").first(), "the unit is on screen").toHaveText("Rp juta");
  await expect(page.getByText("Lihat nilai kategori").first(), "category values are readable without a tooltip").toBeAttached();
  await expect(page.getByRole("rowheader", { name: /Sep 26/ }), "months are formatted, not raw YYYY-MM").toHaveCount(1);
  const monthTable = page.getByRole("region", { name: "Arus kas per bulan, geser untuk semua kolom", exact: true });
  const monthLink = monthTable.getByRole("link", { name: "Lihat pengeluaran Agu 26", exact: true });
  await expect(monthLink).toHaveAttribute("href", "/transactions?from=2026-08-01&to=2026-08-31&status=CONFIRMED&type=SPENDING&cycle=2026-08-26");
  const startRow = monthTable.getByRole("row").filter({ hasText: "Data mulai Agu 26" });
  await expect(startRow, "leading empty months collapse into one line").toHaveCount(1);
  await expect(startRow.getByRole("link"), "the collapsed line is not interactive").toHaveCount(0);
  await expect(monthTable.getByRole("rowheader", { name: /Jul 26/ }), "the collapsed month is not a row of its own").toHaveCount(0);
  const emptyRow = monthTable.getByRole("row").filter({ hasText: "belum ada transaksi" });
  await expect(emptyRow, "an empty month after the first transaction remains visible").toHaveCount(1);
  await expect(emptyRow.getByRole("link"), "empty month stays non-interactive").toHaveCount(0);
  await monthLink.focus();
  await page.keyboard.press("Enter");
  await page.getByRole("link", { name: "Kembali ke tinjauan siklus", exact: true }).waitFor();
  expect(new URL(page.url()).pathname, "month link works by keyboard").toBe("/transactions");
  await page.goBack({ waitUntil: "networkidle" });
  await monthTable.waitFor();

  await expect(page.getByLabel("Bulan mulai").locator("option[value='2025-01']"), "months are named in Indonesian").toHaveText("Januari 2025");
  await page.getByLabel("Bulan mulai").selectOption("2025-03");
  await page.getByLabel("Bulan selesai").selectOption("2025-01");
  await page.getByRole("button", { name: "Terapkan", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "Bulan mulai harus sebelum atau sama dengan bulan selesai." }).waitFor();
  expect(searchOf(page).has("from"), "an invalid range never reaches the URL").toBe(false);
  await page.getByLabel("Bulan mulai").selectOption("2025-01");
  await page.getByLabel("Bulan selesai").selectOption("2025-03");
  await page.getByRole("button", { name: "Terapkan", exact: true }).click();
  await page.getByText("Rentang bulan Jan 25 sampai Mar 25").waitFor();
  expect(searchOf(page).get("from")).toBe("2025-01");
  await page.waitForFunction(() => document.querySelector(".custom-range button")?.disabled === true);
  await expect(page.getByRole("button", { name: "Terapkan", exact: true }), "nothing left to apply once the fields match the shown range").toBeDisabled();
  await expect(page.getByRole("alert").filter({ hasText: "Bulan mulai harus sebelum atau sama dengan bulan selesai." }), "a valid range clears the field error").toHaveCount(0);
  await page.goBack({ waitUntil: "networkidle" });
  const shownRange = defaultRange({ from: "", to: "", range: searchOf(page).get("range") || "3" }, FIXED_NOW);
  await page.waitForFunction(expected => document.querySelector('select[name="from"]')?.value === expected, shownRange.from);
  await expect(page.getByLabel("Bulan mulai"), "the inputs follow the URL after Back").toHaveValue(shownRange.from);

  flags.merchantsFail = true;
  await page.getByRole("button", { name: "6 Bulan", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "Analisis kalender belum dapat dimuat" }).waitFor();
  await expect(page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }), "the cashflow chart survives a failed merchants request").toBeVisible();
  await expect(page.getByRole("rowheader", { name: /Sep 26/ }), "the monthly table survives too").toHaveCount(1);
  flags.merchantsFail = false;
  await page.getByRole("button", { name: "Coba lagi", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "Analisis kalender belum dapat dimuat" }).waitFor({ state: "detached" });
  expect(await overflows(page)).toBe(false);
});

test("a failed facts request never shows zero amounts and recovers, including without a salary anchor", async ({ page, api }) => {
  flags.invalidFacts = true;
  await visit(page, CLOSED);
  await page.getByRole("alert").filter({ hasText: "Tinjauan siklus belum dapat dimuat." }).waitFor();
  await expect(page.locator("#position"), "failed facts never become zero financial amounts").toHaveCount(0);
  flags.invalidFacts = false;
  flags.noSalary = true;
  api.requests.length = 0;
  await page.getByRole("button", { name: "Coba lagi", exact: true }).click();
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  await page.getByText("Belum ada gaji utama terkonfirmasi untuk menentukan siklus", { exact: true }).waitFor();
  expect(api.requests.filter(request => request.path.startsWith("/api/v1/insights"))).toHaveLength(0);
});

test("a stable cycle shows its one-line commentary and never generates", async ({ page, api }) => {
  flags.stable = true;
  await visit(page, CLOSED);
  await page.locator("#family-talk > details > summary").click();
  await page.getByText("Tidak ada perubahan yang menonjol terhadap tiga siklus sebelumnya.", { exact: true }).waitFor();
  await expect(page.locator(".kpi-cell:nth-child(3) dd")).toHaveText(idr(0));
  await expect(page.locator(".insight-text p"), "stable commentary is not expanded into filler").toHaveCount(1);
  await expect(page.locator("#changes tbody tr")).toHaveCount(1);
  expect(api.calls("/api/v1/insights/generate"), "stable review never auto-generates commentary").toHaveLength(0);
});

test("a running cycle starting 25 Sep keeps exact daily rows, links days by keyboard and pointer, and reports generation limits", async ({ page }) => {
  const name = test.info().project.name;
  await visit(page, "/analytics?view=cycle&cycle=2026-09-25");
  await page.getByRole("heading", { name: "Posisi siklus", exact: true }).waitFor();
  expect(await page.locator(".kpi-cell[data-lead] dd").evaluate(element => element.scrollWidth > element.clientWidth + 1), "headline amount fits its metric cell").toBe(false);
  expect(searchOf(page).get("cycle")).toBe("2026-09-25");
  await expect(page.locator("#changes table")).toBeHidden();
  await expect(page.locator("#discussion .insight-card")).toBeHidden();
  await page.evaluate(() => document.querySelectorAll(".cycle-review details").forEach(details => { details.open = true; }));
  await expect(page.locator("#changes table")).toBeVisible();
  expect(await page.locator("#family-talk > details").evaluate(element => element.open)).toBe(true);
  await page.locator("#daily .review-daily").waitFor({ state: "attached" });
  await page.evaluate(() => document.querySelectorAll(".cycle-review details").forEach(details => { details.open = true; }));
  await expect(page.locator("#daily .review-daily table").locator("tbody tr"), "all exact daily rows retained").toHaveCount(7);
  const dailyTable = page.getByRole("region", { name: "Nilai pengeluaran harian, geser untuk semua kolom", exact: true });
  const dayName = "Lihat pengeluaran 25 Sep 2026";
  const dayLink = dailyTable.getByRole("link", { name: dayName, exact: true });
  await expect(dayLink, "spending day row links to exactly that day").toHaveAttribute("href", DAY_HREF);
  await expect(dailyTable.getByRole("row").filter({ hasText: "26 Sep 2026" }), "zero-spend day remains visible").toHaveCount(1);
  await expect(dailyTable.getByRole("row").filter({ hasText: "26 Sep 2026" }).getByRole("link"), "zero-spend day has no table link").toHaveCount(0);
  const dayChart = page.locator(".cycle-spending-chart");
  await expect(dayChart.getByRole("link", { name: dayName, exact: true }), "chart and table share the same drill").toHaveAttribute("href", DAY_HREF);
  await expect(dayChart.getByRole("link", { name: "Lihat pengeluaran 26 Sep 2026", exact: true }), "zero-spend day has no chart link").toHaveCount(0);
  const rentChange = page.locator(".diverge > li").filter({ hasText: "Tempat Tinggal" });
  await expect(rentChange, "a running cycle shows the served same-day change").toHaveText(/naik.*1\.950\.000/);
  await expect(rentChange).not.toHaveText(/100%/);
  const rentRow = page.locator("#changes table tbody tr").filter({ hasText: "Tempat Tinggal" });
  await expect(rentRow.locator("td").nth(6)).toHaveText(idr(1950000));
  await expect(rentRow.locator("td").nth(7)).toHaveText(/0%/);
  await expect(page.locator("#changes table tbody tr"), "all exact category comparisons retained").toHaveCount(3);
  await expect(page.locator("#household .household-list > li")).toHaveCount(2);
  expect(await overflows(page), `${name} complete report has no page overflow`).toBe(false);

  flags.generationStatus = 429;
  await expect(page.locator("#discussion"), "the hourly limit is stated before it is hit").toContainText("Dibatasi satu pembahasan per jam");
  await page.getByRole("button", { name: "Buat pembahasan", exact: true }).click();
  await page.getByText("Batas satu pembahasan per jam. Coba lagi setelah jeda satu jam dari permintaan terakhir.", { exact: true }).waitFor();
  flags.generationStatus = 409;
  await page.locator("#discussion").getByRole("button", { name: "Coba lagi", exact: true }).click();
  await page.getByText("Pembahasan sebelumnya masih diproses. Tunggu sampai selesai.", { exact: true }).waitFor();

  await page.keyboard.press("Tab");
  await dayLink.focus();
  expect(await dayLink.evaluate(element => element === document.activeElement && getComputedStyle(element).outlineStyle !== "none"), "daily row has visible keyboard focus").toBe(true);
  await page.keyboard.press("Enter");
  const dayReturn = page.getByRole("link", { name: "Kembali ke tinjauan siklus", exact: true });
  await dayReturn.waitFor();
  expect(new URL(page.url()).search, "daily row navigates with the same-day query").toBe(DAY_HREF.slice(DAY_HREF.indexOf("?")));
  await dayReturn.click();
  await openDisclosure(page, "#daily > details", ".cycle-spending-chart");
  await dayChart.getByRole("link", { name: dayName, exact: true }).waitFor();
  const chartLink = dayChart.getByRole("link", { name: dayName, exact: true });
  await chartLink.focus();
  await page.keyboard.press("Tab");
  expect(await dayChart.getByRole("link", { name: "Lihat pengeluaran 27 Sep 2026", exact: true }).evaluate(element => element === document.activeElement), "Tab reaches the next spending day, skipping zero").toBe(true);
  await chartLink.focus();
  expect(await chartLink.evaluate(element => getComputedStyle(element).outlineStyle !== "none"), "chart day has visible keyboard focus").toBe(true);
  await page.keyboard.press("Enter");
  await dayReturn.waitFor();
  expect(new URL(page.url()).search, "chart link works by keyboard").toBe(DAY_HREF.slice(DAY_HREF.indexOf("?")));
  await dayReturn.click();
  await openDisclosure(page, "#daily > details", ".cycle-spending-chart");
  await dayChart.getByRole("link", { name: dayName, exact: true }).click();
  await dayReturn.waitFor();
  expect(new URL(page.url()).search, "chart link works by pointer").toBe(DAY_HREF.slice(DAY_HREF.indexOf("?")));
});

test("entering the ledger from the calendar directly keeps month bounds without inventing a cycle", async ({ page }) => {
  await visit(page, "/analytics?view=calendar");
  const monthTable = page.getByRole("region", { name: "Arus kas per bulan, geser untuk semua kolom", exact: true });
  await monthTable.getByRole("link", { name: "Lihat pengeluaran Agu 26", exact: true }).click();
  await page.getByRole("heading", { name: "Riwayat Transaksi", exact: true }).waitFor();
  expect(new URL(page.url()).search, "direct calendar entry keeps month bounds without guessing a salary anchor").toBe("?from=2026-08-01&to=2026-08-31&status=CONFIRMED&type=SPENDING&cycle=");
  await expect(page.getByRole("link", { name: "Kembali ke tinjauan siklus", exact: true }), "no invalid cycle backlink without a reviewed cycle").toHaveCount(0);
});
