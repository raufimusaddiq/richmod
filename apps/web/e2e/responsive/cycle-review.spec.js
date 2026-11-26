import { test, expect, FIXED_NOW } from "../support/fixtures.js";
import { idr } from "../support/data.js";
import { cycleReviewApi, cycleUser } from "../support/cycleApi.js";
import { fullCycleFacts } from "../../tests/fixtures/cycle-review.mjs";
import { defaultRange } from "../../app/lib/calendarReview.js";

test.use({ user: cycleUser });

const CYCLE = "/analytics?view=cycle&cycle=2026-09-01";
const EARLY = "/analytics?view=cycle&cycle=2026-09-05";
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

test("an active cycle opens calm: wide verdict lead tile, pace first, unified category table, balanced row and always-visible daily strip", async ({ page, api }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  await expect(page.getByRole("heading", { name: "Laporan", level: 1 }), "the page is Laporan").toBeVisible();
  const tabs = page.getByRole("tablist", { name: "Jenis laporan" }).getByRole("tab");
  expect(await tabs.allTextContents()).toEqual(["Arus kas", "Kekayaan"]);
  await expect(tabs.nth(0)).toHaveAttribute("aria-selected", "true");

  const paceFold = page.locator("#spending-shape .review-daily");
  await expect(paceFold.locator("table"), "values are secondary to the chart").toBeHidden();
  await paceFold.locator("summary").focus();
  await page.keyboard.press("Enter");
  await expect(paceFold.locator("table"), "native disclosure is keyboard accessible").toBeVisible();
  await page.keyboard.press("Enter");
  await expect(paceFold.locator("table")).toBeHidden();

  await expect(page.locator(".kpi-cell"), "the KPI strip keeps four tiles").toHaveCount(4);
  for (const id of ["where", "history", "spending-shape", "daily"]) expect((await page.locator(`#${id} .takeaway`).textContent()).length, `${id} shows a takeaway sentence`).toBeGreaterThan(10);
  await expect(page.locator(".kpi-cell[data-lead] .kpi-verdict")).toContainText("lebih tinggi dari siklus lalu");
  await expect(page.locator(".kpi-cell[data-lead] .kpi-chip")).toHaveCount(2);
  await page.evaluate(() => document.fonts.ready);
  expect(await page.evaluate(() => document.fonts.check('500 16px "Fraunces"') && document.fonts.check('400 14px "Inter"')), "brand fonts loaded").toBe(true);
  expect((await page.locator(".kpi-cell[data-lead] dt").textContent()).startsWith("Pengeluaran bersih"), "a running cycle leads with spending").toBe(true);
  await expect(page.locator(".kpi-cell[data-lead] dd")).toHaveText(idr(4200000));
  await expect(page.locator("#position").getByText("Arus kas bersih sejauh ini", { exact: true }), "net cashflow stays visible as a regular metric").toBeVisible();

  expect(api.requests.filter(request => request.method === "POST"), "opening review never invokes generation").toHaveLength(0);
  await expect(page.getByRole("button", { name: "Mulai tinjauan · 7 langkah", exact: true }), "active cycle cannot enter closed-cycle meeting").toHaveCount(0);
  await expect(page.getByRole("button", { name: "Simpan keputusan", exact: true }), "active cycle has no save action").toHaveCount(0);
  expect(flags.salaryCalls, "explicit cycle uses one facts request").toBe(1);
  await expect(page.getByText("Historical advice should never display.")).toHaveCount(0);
  await expect(page.getByText("Stale cutoff must never display.")).toHaveCount(0);
  for (const id of ["family-talk", "quality"]) expect(await page.locator(`#${id} > details`).evaluate(element => element.open), `${id} is folded by default`).toBe(false);
  await expect(page.locator(".cycle-status")).toContainText("dari ±30");
  await expect(page.locator("#daily .cycle-spending-chart"), "Ritme harian chart is mounted immediately").toBeVisible();
  await expect(page.locator("#daily .review-daily table"), "daily value table stays folded").toBeHidden();

  await expect(page.locator("#where .cat-list > li"), "three ranked categories; an empty remainder is not drawn").toHaveCount(3);
  await expect(page.locator("#where .cat-total strong"), "the list total is the served net expense").toHaveText(idr(4200000));
  expect(await page.locator("#where .cat-list > li:not([data-other]) .cat-amount").allTextContents(), "ranking order is the served order").toEqual([2100000, 1400000, 700000].map(idr));
  await page.locator("#where").getByRole("button", { name: "Paling berubah", exact: true }).click();
  await expect(page.locator("#where .cat-list > li")).toHaveCount(3);
  await page.locator("#where").getByRole("button", { name: "Terbesar", exact: true }).click();

  await expect(page.locator(".notice-strip").getByRole("link", { name: "Buka Inbox" }), "blocking notice stays actionable in top banner").toBeVisible();
  await expect(page.locator("#quality"), "QualityFooter always renders").toHaveCount(1);
  await expect(page.locator("#main-content")).not.toContainText(/hemat|boros/i);
  expect(await overflows(page)).toBe(false);
});

test("days 1-7 early cycle renders the LastCycleCard, wealth chip, and disables Paling berubah", async ({ page }) => {
  await visit(page, EARLY);
  await page.locator("#last-cycle").waitFor();
  await expect(page.locator("#last-cycle")).toContainText("Siklus lalu sudah ditutup");
  await expect(page.locator("#last-cycle")).toContainText(idr(10960000));
  await expect(page.locator("#last-cycle")).toContainText("32 hari");
  await expect(page.locator("#last-cycle").getByRole("link", { name: /Inbox/ })).toHaveAttribute("href", "/inbox");
  await expect(page.locator(".notice-strip"), "wealth-only reminder is not shown as a blocking banner").toHaveCount(0);
  const chip = page.locator(".quality-chip");
  await expect(chip).toHaveText("1 perlu dilengkapi");
  await expect(page.locator("#where").getByRole("button", { name: "Paling berubah", exact: true })).toBeDisabled();
  await expect(page.locator("#history").getByRole("button", { name: "Siklus penuh", exact: true })).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("#merchants .merchant-row-delta"), "merchant card hides deltas during days 1-3").toHaveCount(0);
  const dailyTicks = page.locator("#daily .cycle-spending-chart [data-day-tick]");
  await expect(dailyTicks.first(), "day 1 keeps its tick label on day 2").toHaveText("05");
  await expect(dailyTicks.nth(1), "partial day keeps its label").toHaveText("06belum penuh");
  const firstTwoOverlap = await dailyTicks.evaluateAll(nodes => {
    const firstLine = node => {
      if (!node?.firstChild) return null;
      const range = document.createRange();
      range.selectNodeContents(node.firstChild);
      return range.getBoundingClientRect();
    };
    const first = firstLine(nodes[0]);
    const second = firstLine(nodes[1]);
    return Boolean(first && second && first.right > second.left - 1);
  });
  expect(firstTwoOverlap, "day 1 and day 2 tick labels never overlap").toBe(false);
  await chip.click();
  expect(await page.locator("#quality > details").evaluate(el => el.open), "clicking wealth chip opens Kelengkapan data").toBe(true);
  await expect(page.locator("#quality")).toContainText("Belum ada pengamatan kekayaan di siklus ini");
  await page.locator("#last-cycle").getByRole("button", { name: "Tinjau siklus lalu · 7 langkah", exact: true }).click();
  await page.waitForFunction(() => new URL(location.href).searchParams.get("review") === "position");
  expect(searchOf(page).get("cycle")).toBe("2026-08-26");
  expect(searchOf(page).get("review")).toBe("position");
  expect(await overflows(page)).toBe(false);
});

test("the full category comparison is keyboard reachable and keeps exact figures", async ({ page }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  const full = page.locator("details#changes");
  expect(await full.evaluate(element => element.open), "the full comparison is folded by default").toBe(false);
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
  const rankedAmount = await groceries.locator(".cat-amount").textContent();
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

test("the daily chart tooltip names net spending and refunds for the hovered date when a long cycle repeats a day of month", async ({ page, api }) => {
  const facts = fullCycleFacts();
  facts.daily.push({ period: "2026-10-01", expense: "250000", grossExpense: "250000", refund: "0", income: "0", cumulativeExpense: "3250000" });
  facts.period = { ...facts.period, measuredUntil: "2026-10-01" };
  facts.pace = { previousFullCycle: [...facts.pace.previousFullCycle, "2900000"], median3: [...facts.pace.median3, "3900000"] };
  api.use(({ path }) => (path === "/api/v1/analytics/cycle-review" ? facts : undefined));
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Ritme harian" }).waitFor();
  await page.locator(".cycle-spending-chart .recharts-bar-rectangle").last().hover();
  await page.locator(".chart-tooltip").waitFor();
  await expect(page.locator(".chart-tooltip")).toHaveText(/^1 Okt 2026Pengeluaran bersih.*Rp.*250\.000.*Refund.*Rp/);
});

test("history and matrix share the same-day and full toggle, show Rp/hr and arus kas, and open evidence by keyboard", async ({ page }) => {
  await visit(page, CYCLE);
  await openDisclosure(page, "#history details", ".heat-table");
  await page.getByRole("heading", { name: "Angka per kategori", exact: true }).waitFor();
  const heat = page.locator(".heat-table");
  await expect(heat.locator("thead th:not(:first-child):not(.heat-delta-head)"), "one column per served cycle").toHaveCount(6);
  await expect(heat.locator('thead th[aria-current="true"]'), "the selected cycle is marked").toHaveCount(1);
  const sameDay = page.locator("#history").getByRole("button", { name: "Hari 1–6", exact: true });
  const fullCycle = page.locator("#history").getByRole("button", { name: "Siklus penuh", exact: true });
  await expect(sameDay, "same-day is the default while the cycle runs").toHaveAttribute("aria-pressed", "true");
  await expect(fullCycle).toHaveAttribute("aria-pressed", "false");
  await expect(heat.locator("thead th small").filter({ hasText: "hari 1–6" }), "every column names its cut").toHaveCount(6);
  await expect(heat.locator("tbody tr"), "three categories plus the served remainder").toHaveCount(4);
  await expect(heat.locator("tbody tr").last().locator("th")).toHaveText(/^Kategori lain/);
  await expect(heat.locator("tfoot th")).toHaveText("Total pengeluaran bersih");
  await fullCycle.click();
  await expect(fullCycle).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("#history .history-cols > li").first()).toContainText("/hr");
  await expect(page.locator("#history .history-cols > li").first()).toContainText("arus kas");
  await expect(heat.locator("thead th[data-running]"), "the running column is flagged in full mode").toHaveCount(1);
  await expect(heat.locator("td[data-running]"), "every row and the total mark the running column").toHaveCount(5);
  const firstRunning = heat.locator("td[data-running] .heat-cell").first();
  expect((await firstRunning.evaluate(element => element.style.getPropertyValue("--tint"))).trim(), "the running column carries no tint").toBe("0");
  expect(await firstRunning.evaluate(element => getComputedStyle(element).borderStyle), "the running column is dashed instead of tinted").toBe("dashed");
  expect(await heat.locator("td[data-running] .heat-cell").evaluateAll(cells => cells.map(cell => cell.style.getPropertyValue("--tint").trim())), "no tinted cell sits in the running column").toEqual(["0", "0", "0", "0"]);
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
  await expect(fullCycle, "history toggle survives same-cycle navigation").toHaveAttribute("aria-pressed", "true");
  await sameDay.click();
  await expect(sameDay).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("#history .history-cols > li"), "the cycle columns card draws every served cycle").toHaveCount(6);
});

test("the history toggle follows the selected cycle: a closed cycle has no same-day toggle and the active cycle re-seeds it", async ({ page }) => {
  await visit(page, CYCLE);
  await openDisclosure(page, "#history details", ".heat-table");
  await page.getByLabel("Siklus yang ditinjau").selectOption("2026-08-26");
  await page.waitForFunction(() => document.querySelector(".cycle-status strong")?.textContent.startsWith("Ditutup"));
  await openDisclosure(page, "#history details", ".heat-table");
  await expect(page.locator("#history").getByRole("button", { name: "Siklus penuh", exact: true }), "a closed cycle has no same-day toggle").toHaveCount(0);
  await expect(page.locator(".heat-table thead th[data-running]"), "a closed cycle has no running column").toHaveCount(0);
  await page.getByLabel("Siklus yang ditinjau").selectOption("2026-09-01");
  await page.waitForFunction(() => document.querySelector(".cycle-status strong")?.textContent.startsWith("Berjalan"));
  await expect(page.locator("#history").getByRole("button", { name: "Hari 1–6", exact: true }), "returning to the active cycle re-seeds same-day").toHaveAttribute("aria-pressed", "true");
});

test("history columns share the category colours of the ranking", async ({ page }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  const column = page.locator("#history .history-cols > li").first();
  await expect(column.locator("em"), "a history column shows a served amount").toHaveText(/[1-9]/);
  expect(await column.locator(".history-bar").evaluate(element => element.getBoundingClientRect().height), "the history bar has a real height").toBeGreaterThan(0);
  expect(await column.locator(".history-seg").count(), "the history bar splits into category segments").toBeGreaterThanOrEqual(1);
  const whereColor = await page.locator("#where .cat-swatch").first().evaluate(el => getComputedStyle(el).backgroundColor);
  const historyColor = await page.locator("#history .history-cols > li").last().locator(".history-seg").first().evaluate(el => getComputedStyle(el).backgroundColor);
  expect(historyColor, "Siklus ke siklus uses the same category colours as Ke mana uangnya").toBe(whereColor);
});

test("the merchant card and household split are exact and never write", async ({ page, api }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Merchant teratas" }).waitFor();
  const rows = page.locator("#merchants .merchant-list > li");
  await expect(rows).toHaveCount(2);
  await expect(page.locator("#merchants details"), "merchant card has no redundant fold").toHaveCount(0);
  await expect(rows.first()).toContainText(idr(2100000));
  await expect(rows.last()).toContainText("Merchant lain");
  expect(new URL(await page.locator("#merchants").getByRole("link", { name: "Pasar Keluarga", exact: true }).getAttribute("href"), page.url()).searchParams.get("merchantId")).toBe(MERCHANT);

  await openDisclosure(page, "#family-talk > details", "#household .household-list");
  const household = page.locator("#household .household-list > li");
  await expect(household).toHaveCount(2);
  await expect(household.first(), "the automatic bucket comes first").toHaveText(/otomatis/);
  await expect(household.first(), "the served share is shown").toHaveText(/47,6%/);
  await expect(page.locator("#household .household-bar i")).toHaveCount(2);
  expect(api.requests.filter(request => request.method === "POST"), "opening the detail sections never writes").toHaveLength(0);
});

test("the pace chart comes first under KPI, draws three curves, and history + merchants balance height on desktop", async ({ page }) => {
  const name = test.info().project.name;
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  await expect(page.locator(".cycle-pace-chart"), "pace is its own single-series chart").toBeVisible();
  await expect(page.locator(".pace-marker-note"), "a running cycle says today").toHaveText("Hari ini · hari ke-6");
  const layout = await page.evaluate(() => {
    const box = selector => document.querySelector(selector)?.getBoundingClientRect();
    const kpi = box("#position");
    const pace = box("#spending-shape");
    const where = box("#where");
    const history = box("#history");
    const merchants = box("#merchants");
    return {
      paceBelowKpi: Boolean(kpi && pace) && pace.top >= kpi.bottom - 2,
      whereBelowPace: Boolean(pace && where) && where.top >= pace.bottom - 2,
      splitSideBySide: Boolean(history && merchants) && Math.abs(history.top - merchants.top) < 4 && history.right <= merchants.left + 1,
      equalHeight: Boolean(history && merchants) && Math.abs(history.height - merchants.height) <= 2,
    };
  });
  expect(layout.paceBelowKpi, "PaceCard sits directly under the KPI row").toBe(true);
  expect(layout.whereBelowPace, "CategoryTable sits below PaceCard").toBe(true);
  if (name === "desktop") {
    expect(layout.splitSideBySide, "HistoryCard and MerchantCard share the 7fr/5fr row on desktop").toBe(true);
    expect(layout.equalHeight, "HistoryCard and MerchantCard stretch to equal height").toBe(true);
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
  await page.getByRole("heading", { name: "Ritme harian" }).waitFor();
  expect(searchOf(page).get("cycle")).toBe("2026-08-26");
  await page.waitForFunction(() => document.querySelector(".kpi-cell[data-lead]")?.textContent.includes("(penuh)"));
  await expect(page.locator(".kpi-cell[data-lead]")).toHaveText(/vs siklus lalu \(penuh\)/);
  await expect(page.locator(".kpi-cell[data-lead]").getByRole("button", { name: "Mulai tinjauan · 7 langkah", exact: true })).toBeVisible();
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
  await page.getByRole("heading", { name: "Ritme harian" }).waitFor();
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
  await page.getByRole("heading", { name: "Ritme harian" }).waitFor();
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
  await page.getByRole("heading", { name: "Ritme harian" }).waitFor();
  await page.locator("#family-talk > details > summary").getByText(/bahan pembahasan belum tersedia/).waitFor();
  for (const id of ["position", "spending-shape", "where", "changes", "daily", "history", "merchants", "family-talk", "household", "quality"]) await expect(page.locator(`#${id}`)).toHaveCount(1);
  expect(api.requests.filter(request => request.method === "POST")).toHaveLength(0);
  await expect(page.locator(".notice-strip").getByRole("link", { name: "Buka Inbox" })).toHaveCount(1);
  await openDisclosure(page, "#quality > details", "#quality .review-quality");
  await expect(page.locator("#quality").getByRole("link", { name: "Buka Inbox" })).toHaveCount(1);
});

test("the closed-cycle meeting walks all 7 steps, auto-opens folds, keeps a draft, and records decisions only on explicit save", async ({ page, api }) => {
  flags.aiAvailable = false;
  flags.advisory = true;
  await visit(page, CLOSED);
  await page.getByRole("heading", { name: "Ritme harian" }).waitFor();
  await page.getByRole("button", { name: "Mulai tinjauan · 7 langkah", exact: true }).click();
  await page.waitForFunction(() => document.activeElement?.id === "position-title");
  expect(searchOf(page).get("review")).toBe("position");
  expect(await page.locator("#position h2").evaluate(element => element === document.activeElement)).toBe(true);
  await expect(page.locator("#position")).toBeVisible();
  await expect(page.locator("#history")).toBeVisible();
  await expect(page.locator("#history .heat-table"), "step 1 auto-opens Angka per kategori").toBeVisible();
  await expect(page.locator("#where")).toBeHidden();
  await expect(page.locator("#spending-shape")).toBeHidden();

  await page.locator(".quality-chip").click();
  expect(searchOf(page).get("review"), "quality chip navigates to quality step during review").toBe("quality");
  await expect(page.locator("#quality .review-quality"), "quality fold opens on quality step").toBeVisible();
  await page.getByRole("navigation", { name: "Langkah tinjauan siklus" }).getByRole("button", { name: "1. Posisi siklus", exact: true }).click();
  expect(searchOf(page).get("review")).toBe("position");
  await expect(page.locator("#quality > details"), "leaving quality step closes its fold").not.toHaveAttribute("open");

  const nav = page.getByRole("navigation", { name: "Langkah tinjauan siklus" });
  await expect(nav.getByRole("button")).toHaveCount(7);

  await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
  expect(searchOf(page).get("review")).toBe("spending-shape");
  await expect(page.locator("#spending-shape")).toBeVisible();
  await expect(page.locator("#spending-shape .review-daily table"), "step 2 auto-opens Tabel laju").toBeVisible();
  await expect(page.locator("#daily")).toBeVisible();
  await expect(page.locator("#daily .review-daily table"), "step 2 auto-opens Nilai harian").toBeVisible();
  await expect(page.locator("#position")).toBeHidden();

  await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
  expect(searchOf(page).get("review")).toBe("changes");
  await expect(page.locator("#where")).toBeVisible();
  await expect(page.locator("#where").getByRole("button", { name: "Paling berubah", exact: true }), "step 3 forces Paling berubah").toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("details#changes table"), "step 3 auto-opens Perbandingan lengkap").toBeVisible();
  await expect(page.locator("#merchants")).toBeHidden();

  await page.getByRole("button", { name: "Langkah berikutnya", exact: true }).click();
  expect(searchOf(page).get("review")).toBe("drivers");
  await expect(page.locator("#where").getByRole("button", { name: "Terbesar", exact: true }), "step 4 forces Terbesar").toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("#merchants")).toBeVisible();
  await expect(page.locator("#merchants .merchant-list > li")).toHaveCount(2);
  await page.locator("#where").getByRole("button", { name: /^Belanja rumah/ }).click();
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
  await page.getByRole("heading", { name: "Ritme harian" }).waitFor();
  await page.getByRole("button", { name: "Kalender", exact: true }).click();
  await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).waitFor();
  expect(searchOf(page).get("view")).toBe("calendar");
  await expect(page.locator(".range-controls"), "calendar controls live in the single control bar").toHaveCount(1);
  await expect(page.locator("#position .kpi-cell[data-lead]")).toContainText(idr(7000000));
  await expect(page.locator("#position .kpi-verdict")).toContainText("September 2026");
  await expect(page.locator(".review-explainer"), "calendar mode has no Tentang explainers").toHaveCount(0);
  await expect(page.locator("#where .segmented"), "calendar category table hides the Terbesar/Paling berubah toggle").toHaveCount(0);
  await expect(page.locator("#changes"), "calendar category table hides the comparison fold").toHaveCount(0);
  await expect(page.locator("#household > details"), "household notes start collapsed at the bottom").not.toHaveAttribute("open", "");
  await page.getByRole("button", { name: "3 Bulan", exact: true }).click();
  expect(searchOf(page).get("range")).toBe("3");
  expect(await page.locator(".dash-legend li").allTextContents(), "the legend follows the bar order").toEqual(expect.arrayContaining(["Pemasukan", "Pengeluaran"]));
  await expect(page.getByRole("group", { name: "Rentang bulan" })).toBeVisible();
  await page.locator(".calendar-body details.review-daily > summary").click();
  const monthTable = page.getByRole("region", { name: "Arus kas per bulan, geser untuk semua kolom", exact: true });
  await expect(monthTable.getByRole("rowheader", { name: /Sep 26/ }), "months are formatted, not raw YYYY-MM").toHaveCount(1);
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
  await page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }).waitFor();

  const periodButton = page.locator(".calendar-period-button");
  await periodButton.click();
  await expect(page.getByLabel("Bulan mulai"), "opening the range popover moves focus to the first select").toBeFocused();
  await expect(page.getByLabel("Bulan mulai").locator("option[value='2025-01']"), "months are named in Indonesian").toHaveText("Januari 2025");
  await page.getByLabel("Bulan mulai").selectOption("2025-03");
  await page.getByLabel("Bulan selesai").selectOption("2025-01");
  await page.getByRole("button", { name: "Terapkan", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "Bulan mulai harus sebelum atau sama dengan bulan selesai." }).waitFor();
  expect(searchOf(page).has("from"), "an invalid range never reaches the URL").toBe(false);
  await page.getByLabel("Bulan mulai").selectOption("2025-01");
  await page.getByLabel("Bulan selesai").selectOption("2025-03");
  await page.getByRole("button", { name: "Terapkan", exact: true }).click();
  await page.getByText(/Rentang bulan Jan 25 sampai Mar 25/).waitFor();
  expect(searchOf(page).get("from")).toBe("2025-01");
  await periodButton.click();
  await page.waitForFunction(() => document.querySelector(".custom-range button")?.disabled === true);
  await expect(page.getByRole("button", { name: "Terapkan", exact: true }), "nothing left to apply once the fields match the shown range").toBeDisabled();
  await expect(page.getByRole("alert").filter({ hasText: "Bulan mulai harus sebelum atau sama dengan bulan selesai." }), "a valid range clears the field error").toHaveCount(0);
  await page.goBack({ waitUntil: "networkidle" });
  const shownRange = defaultRange({ from: "", to: "", range: searchOf(page).get("range") || "3" }, FIXED_NOW);
  await periodButton.click();
  await page.waitForFunction(expected => document.querySelector('select[name="from"]')?.value === expected, shownRange.from);
  await expect(page.getByLabel("Bulan mulai"), "the inputs follow the URL after Back").toHaveValue(shownRange.from);
  await page.keyboard.press("Escape");
  await expect(periodButton, "Escape returns focus to the range trigger").toBeFocused();

  flags.merchantsFail = true;
  await page.getByRole("button", { name: "6 Bulan", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "Analisis kalender belum dapat dimuat" }).waitFor();
  await expect(page.getByRole("heading", { name: "Pemasukan vs pengeluaran", exact: true }), "the cashflow chart survives a failed merchants request").toBeVisible();
  await expect(page.locator("#position .kpi-cell[data-lead]"), "the KPI strip survives too").toContainText(idr(7000000));
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
  await page.locator(".notice-strip").getByText("Belum ada gaji utama terkonfirmasi untuk menentukan siklus", { exact: true }).waitFor();
  expect(api.requests.filter(request => request.path.startsWith("/api/v1/insights"))).toHaveLength(0);
});

test("a stable cycle shows its one-line commentary and never generates", async ({ page, api }) => {
  flags.stable = true;
  await visit(page, CLOSED);
  await page.locator("#family-talk > details > summary").click();
  await page.getByText("Tidak ada perubahan yang menonjol terhadap tiga siklus sebelumnya.", { exact: true }).waitFor();
  await expect(page.locator(".kpi-cell:nth-child(3) dd")).toHaveText(idr(0));
  await expect(page.locator(".insight-text p"), "stable commentary is not expanded into filler").toHaveCount(1);
  await openDisclosure(page, "details#changes", "#changes .empty");
  await expect(page.locator("#changes tbody tr")).toHaveCount(0);
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
  await page.locator("#where").getByRole("button", { name: "Paling berubah", exact: true }).click();
  const rentChange = page.locator("#where .cat-list > li").filter({ hasText: "Tempat Tinggal" });
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
  await dayChart.getByRole("link", { name: dayName, exact: true }).click();
  await dayReturn.waitFor();
  expect(new URL(page.url()).search, "chart link works by pointer").toBe(DAY_HREF.slice(DAY_HREF.indexOf("?")));
});

test("12-month calendar category table renders a palette or neutral swatch on every row and folds non-palette rows into the neutral stack segment", async ({ page, api }) => {
  const paletteCategories = [
    { id: "c-1", name: "Belanja rumah", amount: "4500000", share: "0.225" },
    { id: "c-2", name: "Tempat tinggal", amount: "3800000", share: "0.190" },
    { id: "c-3", name: "Makan di luar", amount: "3100000", share: "0.155" },
    { id: "c-4", name: "Transportasi", amount: "2400000", share: "0.120" },
    { id: "c-5", name: "Tagihan", amount: "1900000", share: "0.095" },
    { id: "c-6", name: "Kopi & camilan", amount: "1400000", share: "0.070" },
  ];
  const calendarTwelve = [
    ...paletteCategories,
    { id: "c-7", name: "Kesehatan", amount: "1100000", share: "0.055" },
    { id: "c-8", name: "Hiburan", amount: "1000000", share: "0.050" },
    { id: "c-9", name: "Pendidikan", amount: "800000", share: "0.040" },
  ];
  api.use(({ path, query }) => {
    if (path === "/api/v1/analytics/categories") {
      return query.get("period") === "last_3_cycles" ? paletteCategories : calendarTwelve;
    }
    return undefined;
  });
  await visit(page, "/analytics?view=calendar&range=12");
  await expect(page.locator("#where .cat-list > li")).toHaveCount(9);
  const audit = await page.evaluate(() => {
    const resolve = token => {
      const probe = document.createElement("span");
      probe.style.backgroundColor = `var(${token})`;
      document.body.appendChild(probe);
      const color = getComputedStyle(probe).backgroundColor;
      probe.remove();
      return color;
    };
    const palette = [1, 2, 3, 4, 5, 6].map(index => resolve(`--chart-category-${index}`));
    const neutral = resolve("--chart-category-other");
    const rows = [...document.querySelectorAll("#where .cat-list > li")].map(li => ({
      swatches: li.querySelectorAll(".cat-swatch").length,
      gaps: li.querySelectorAll(".cat-swatch-gap").length,
      swatchColor: li.querySelector(".cat-swatch") ? getComputedStyle(li.querySelector(".cat-swatch")).backgroundColor : "",
      barColor: li.querySelector(".cat-share-bar i") ? getComputedStyle(li.querySelector(".cat-share-bar i")).backgroundColor : "",
    }));
    const stack = [...document.querySelectorAll("#where .cat-stack > i")].map(el => getComputedStyle(el).backgroundColor);
    return { palette, neutral, rows, stack };
  });
  const allowed = new Set([...audit.palette, audit.neutral]);
  for (const row of audit.rows) {
    expect(row.swatches, "every calendar row has one swatch").toBe(1);
    expect(row.gaps, "no calendar row renders an empty swatch gap").toBe(0);
    expect(allowed.has(row.swatchColor), `swatch colour ${row.swatchColor} is palette or neutral`).toBe(true);
    expect(row.barColor, "share bar matches swatch colour").toBe(row.swatchColor);
  }
  for (const extra of audit.rows.slice(6)) {
    expect(extra.swatchColor, "non-palette 12-month row uses neutral colour").toBe(audit.neutral);
  }
  expect(audit.stack.at(-1), "stack bar folds non-palette rows into neutral segment").toBe(audit.neutral);
  expect(await overflows(page)).toBe(false);
});

test("entering the ledger from the calendar directly keeps month bounds without inventing a cycle", async ({ page }) => {
  await visit(page, "/analytics?view=calendar");
  await page.locator(".calendar-body details.review-daily > summary").click();
  const monthTable = page.getByRole("region", { name: "Arus kas per bulan, geser untuk semua kolom", exact: true });
  await monthTable.getByRole("link", { name: "Lihat pengeluaran Agu 26", exact: true }).click();
  await page.getByRole("heading", { name: "Riwayat Transaksi", exact: true }).waitFor();
  expect(new URL(page.url()).search, "direct calendar entry keeps month bounds without guessing a salary anchor").toBe("?from=2026-08-01&to=2026-08-31&status=CONFIRMED&type=SPENDING&cycle=");
  await expect(page.getByRole("link", { name: "Kembali ke tinjauan siklus", exact: true }), "no invalid cycle backlink without a reviewed cycle").toHaveCount(0);
});

test("a running cycle links straight to writing a decision for the previous, closed cycle", async ({ page }) => {
  await visit(page, CYCLE);
  await page.getByRole("heading", { name: "Ritme harian" }).waitFor();
  await page.evaluate(() => { window.__sameDocument = true; });
  await openDisclosure(page, "#family-talk > details", "#decisions");
  await expect(page.locator("#decision-body"), "a running cycle has no decision field").toHaveCount(0);
  const write = page.getByRole("link", { name: /^Tulis keputusan siklus .+ →$/ });
  await expect(write).toHaveAttribute("href", "/analytics?view=cycle&cycle=2026-08-26&review=decisions");
  await write.click();
  await page.locator("#decision-body").waitFor();
  expect(searchOf(page).get("cycle")).toBe("2026-08-26");
  expect(searchOf(page).get("review")).toBe("decisions");
  await page.waitForFunction(() => document.activeElement?.id === "decisions-title");
  expect(await page.evaluate(() => window.__sameDocument), "the link stays in the app without a full reload").toBe(true);
  await expect(page.getByRole("button", { name: "Simpan keputusan", exact: true })).toBeVisible();
});
