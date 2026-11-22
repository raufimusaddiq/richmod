import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { cycleFacts } from "../tests/fixtures/cycle-review.mjs";

const port = process.env.RICHMOD_MOBILE_LAYOUT_PORT || "3214";
const baseURL = `http://127.0.0.1:${port}`;
const output = new URL("../test-results/mobile-layout/", import.meta.url);
const routes = ["/", "/transactions", "/analytics", "/wealth", "/inbox", "/documents", "/household", "/admin"];
const widths = [320, 360, 390, 1366];

const superAdmin = { id: "user-1", displayName: "Rafi", email: "rafi@example.test", isSuperAdmin: true, householdName: "Rumah Rafi", household: { role: "OWNER" } };
const regularOwner = { ...superAdmin, isSuperAdmin: false };
const categories = Array.from({ length: 14 }, (_, index) => ({
  id: `cat-${index + 1}`,
  name: index === 0 ? "Belanja Bahan Makanan & Kebutuhan Rumah Tangga" : `Kategori Keluarga ${index + 1}`,
  amount: String((14 - index) * 120000),
  active: true,
  slug: `kategori-${index + 1}`,
}));
const accounts = [
  { id: "acc-1", name: "Rekening Bank Jago Utama Keluarga", accountType: "BANK", trackingPolicy: "SPENDING_ONLY", active: true },
  { id: "acc-2", name: "BCA Tabungan Bersama", accountType: "BANK", trackingPolicy: "FULL_LEDGER", active: true },
];
const members = [
  { id: "member-1", displayName: "Rafi Musaddiq", email: "rafi@example.test", role: "OWNER", active: true, telegramConnected: true, passwordInitialized: true },
  { id: "member-2", displayName: "Dina Rahmawati", email: "dina@example.test", role: "MEMBER", active: true, telegramConnected: false, passwordInitialized: true },
];
const transactions = Array.from({ length: 24 }, (_, index) => ({
  id: `tx-${index + 1}`,
  type: index % 5 === 0 ? "INCOME" : "EXPENSE",
  status: "CONFIRMED",
  amount: String(45000 + index * 15000),
  merchantName: `Merchant Contoh Panjang ${index + 1}`,
  description: `Transaksi harian rumah tangga nomor ${index + 1}`,
  categoryId: categories[index % categories.length].id,
  categoryName: categories[index % categories.length].name,
  accountId: accounts[index % accounts.length].id,
  accountName: accounts[index % accounts.length].name,
  memberName: members[index % members.length].displayName,
  sourceType: "BANK_EMAIL",
  transactionAt: `2026-09-${String(28 - (index % 20)).padStart(2, "0")}T09:20:00+07:00`,
  updatedAt: "2026-09-28T10:00:00Z",
}));
const daily = Array.from({ length: 12 }, (_, index) => ({
  period: `2026-09-${String(index + 1).padStart(2, "0")}`,
  grossExpense: String((index % 4) * 80000 + 120000),
  refund: index === 2 ? "15000" : "0",
  expense: String((index % 4) * 80000 + 120000),
  income: index === 0 ? "12500000" : "0",
}));
const wealthAccounts = [
  { id: "wa-1", name: "Tabungan utama", institution: "Bank Jago", wealthType: "BANK", usageRole: "SAVINGS", active: true },
  { id: "wa-2", name: "Reksa dana pasar uang", institution: "Bibit", wealthType: "MUTUAL_FUND", usageRole: "INVESTMENT", active: true },
];
const wealthItems = [
  { wealthAccountId: "wa-1", name: "Tabungan utama", side: "ASSET", valueIdr: "30000000" },
  { wealthAccountId: "wa-2", name: "Reksa dana pasar uang", side: "ASSET", valueIdr: "20000000" },
];
const wealthSnapshots = [
  { id: "wealth-2", observedAt: "2026-09-09T15:00:00+07:00", netWorthIdr: "50000000", assetTotalIdr: "50000000", liabilityTotalIdr: "0", items: wealthItems },
  { id: "wealth-1", observedAt: "2026-09-08T10:00:00+07:00", netWorthIdr: "48000000", assetTotalIdr: "48000000", liabilityTotalIdr: "0", items: wealthItems },
];

function alignedCycleFacts() {
  const facts = cycleFacts();
  const idMap = new Map([
    ["11111111-1111-4111-8111-111111111111", "cat-1"],
    ["55555555-5555-4555-8555-555555555555", "cat-2"],
    ["66666666-6666-4666-8666-666666666666", "cat-3"],
  ]);
  const remap = row => (row?.id && idMap.has(row.id) ? { ...row, id: idMap.get(row.id) } : row);
  facts.categoryChanges = facts.categoryChanges.map(remap);
  facts.categoryRanking = { ...facts.categoryRanking, rows: facts.categoryRanking.rows.map(remap) };
  facts.categoryHistory = {
    ...facts.categoryHistory,
    rows: facts.categoryHistory.rows.map(remap),
    sameDay: facts.categoryHistory.sameDay ? { ...facts.categoryHistory.sameDay, rows: facts.categoryHistory.sameDay.rows.map(remap) } : null,
  };
  return facts;
}

function fixture(path, currentUser) {
  if (path === "/api/v1/auth/me") return currentUser;
  if (path === "/api/v1/analytics/cycle-review") return alignedCycleFacts();
  if (path === "/api/v1/analytics/cycle-decisions") return { items: [], previous: [], previousCycleStart: "2026-08-26" };
  if (path === "/api/v1/analytics/overview") return { income: "12500000", expense: "2590000", netCashflow: "9910000", savingsAllocated: "6500000", unallocatedSurplus: "3410000", reviewCount: 1, periodKind: "CURRENT_CYCLE" };
  if (path === "/api/v1/analytics/cycle" || path === "/api/v1/analytics/cycle/daily") return { kind: "CURRENT_CYCLE", start: "2026-09-01", end: "2026-09-30", cycleStart: "2026-09-01", daysElapsed: 12, daysTotal: 30, daily };
  if (path.startsWith("/api/v1/analytics/categories")) return categories;
  if (path.startsWith("/api/v1/analytics/cashflow")) return [{ period: "2026-08", income: "12500000", expense: "8100000", refund: "0", netCashflow: "4400000" }, { period: "2026-09", income: "12500000", expense: "2590000", refund: "0", netCashflow: "9910000" }];
  if (path.startsWith("/api/v1/analytics/merchants")) return [{ name: "Pasar Minggu", amount: "1250000" }];
  if (path.startsWith("/api/v1/analytics/members")) return [{ name: "Rafi", amount: "2590000" }];
  if (path === "/api/v1/insights") return [];
  if (path === "/api/v1/wealth/snapshots/latest") return wealthSnapshots[0];
  if (path === "/api/v1/wealth/accounts") return wealthAccounts;
  if (path === "/api/v1/wealth/history") return wealthSnapshots;
  if (path === "/api/v1/wealth/summary") return { latest: wealthSnapshots[0], previous: wealthSnapshots[1], netWorthChangeIdr: "2000000", confirmedCashflowIdr: "1500000", valuationAndOtherChangeIdr: "500000" };
  if (path === "/api/v1/wealth/current-cycle-savings") return null;
  if (path === "/api/v1/wealth/cycle-recaps" || path === "/api/v1/wealth/observations") return [];
  if (path === "/api/v1/transactions/quick-suggestions") return {
    frequent: [
      { description: "Kopi susu", amount: "25000", type: "EXPENSE", categoryId: "cat-1", accountId: "acc-1" },
      { description: "Makan siang", amount: "45000", type: "EXPENSE", categoryId: "cat-2", accountId: "acc-1" },
      { description: "Parkir harian", amount: "10000", type: "EXPENSE", categoryId: "cat-3", accountId: "acc-1" },
      { description: "Belanja pasar", amount: "120000", type: "EXPENSE", categoryId: "cat-1", accountId: "acc-2" },
    ],
    recentCategories: ["cat-1", "cat-2", "cat-3"],
    descriptionMap: {},
  };
  if (/\/api\/v1\/transactions\/[^/]+\/evidence$/.test(path)) return [{ id: "ev-1", evidenceType: "BANK_EMAIL", sourceType: "BANK_EMAIL", receivedAt: "2026-09-28T09:20:00+07:00" }];
  if (/\/api\/v1\/transactions\/[^/]+\/audit$/.test(path)) return [{ id: "audit-1", action: "CONFIRM", actorType: "SYSTEM", createdAt: "2026-09-28T09:21:00+07:00" }];
  if (/\/api\/v1\/transactions\/[^/]+$/.test(path)) return transactions[0];
  if (path.startsWith("/api/v1/transactions")) return transactions;
  if (path === "/api/v1/reviews") return [{ id: "review-1", reason: "AMBIGUOUS_CATEGORY", amount: "750000", merchantName: "Transfer ke rekening lain", transactionAt: "2026-09-05T15:00:00+07:00", sourceType: "BANK_EMAIL", proposalStatus: "NEEDS_REVIEW", missingFacts: ["merchant"], description: "Tujuan transfer belum jelas" }];
  if (path === "/api/v1/integration-actions") return [];
  if (path === "/api/v1/categories") return categories;
  if (path === "/api/v1/accounts") return accounts;
  if (path === "/api/v1/household") return { id: "household-1", name: "Rumah Rafi" };
  if (path === "/api/v1/household/members") return members;
  if (path === "/api/v1/documents") return [{ id: "doc-1", status: "SUCCEEDED", documentType: "RECEIPT", sourceType: "WEB_IMAGE", createdAt: "2026-09-06T10:00:00+07:00", confidence: 0.93, linkedTransactionIds: ["tx-1"], summary: { merchant: "Pasar Minggu", amount: "185000" }, needsReview: false }];
  if (path === "/api/v1/merchant-aliases") return [{ id: "alias-1", normalizedName: "Pasar Minggu", rawName: "PASAR MINGGU", defaultCategoryName: "Belanja", autoApply: true }];
  if (path === "/api/v1/known-accounts") return [{ id: "known-1", institution: "Bank", displayName: "Rekening keluarga", matchHint: "1234", relationship: "HOUSEHOLD_ACCOUNT" }];
  if (path === "/api/v1/operations/status") return { status: "HEALTHY", checkedAt: "2026-09-07T10:00:00+07:00", jobs: { pending: 0, failed: 0 }, reviewBacklog: 1, worker: { healthy: true }, llmGateway: { configured: true } };
  if (path === "/api/v1/salary/sources") return [{ id: "salary-1", employer: "Richmod Labs", active: true, isPrimary: true }];
  if (path === "/api/v1/bank-email-listeners" || path === "/api/v1/financial-email-sources") return [];
  if (path === "/api/v1/integrations/email-ingress") return { address: "household@example.richmod.link", status: "ACTIVE", lastReceivedAt: "2026-09-06T09:20:00+07:00" };
  if (path === "/api/v1/admin/overview") return { status: "HEALTHY", checkedAt: "2026-09-07T10:00:00Z", worker: { healthy: true, lastHeartbeatAt: "2026-09-07T10:00:00Z" }, jobs: { pending: 0, running: 0, failed24h: 0, lanes: ["INTERACTIVE", "CHAT", "DEFAULT", "BACKGROUND"].map(lane => ({ lane, pending: 0, running: 0, oldestDueAgeMs: null })) }, llm: { calls24h: 12, failed24h: 0, successRate: 1, p95DurationMs: 820 }, reviews: { open: 1 }, households: { total: 1 }, integrations: { llmGatewayConfigured: true, llmProtocol: "Cloud gateway" }, recentEvents: [] };
  if (path.startsWith("/api/v1/admin/")) return { items: [], nextCursor: null };
  return [];
}

async function intercept(page, currentUser = superAdmin) {
  await page.route("**/api/v1/**", route => {
    const url = new URL(route.request().url());
    if (url.pathname === "/api/v1/telemetry/events") return route.fulfill({ status: 204 });
    if (/\/api\/v1\/documents\/[^/]+\/(thumbnail|content|pages\/\d+\/content)$/.test(url.pathname)) {
      return route.fulfill({ contentType: "image/png", body: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64") });
    }
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(fixture(url.pathname, currentUser)) });
  });
}

async function waitForServer() {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    try { if ((await fetch(baseURL)).ok) return; } catch {}
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(`Next.js static server did not start at ${baseURL}`);
}

async function assertNoHorizontalOverflow(page, label) {
  const metrics = await page.evaluate(() => {
    const doc = document.documentElement;
    const frame = document.querySelector(".app-frame")?.getBoundingClientRect();
    const main = document.querySelector(".app-main")?.getBoundingClientRect();
    const fade = document.querySelector(".route-fade")?.getBoundingClientRect();
    return {
      scrollWidth: doc.scrollWidth,
      clientWidth: doc.clientWidth,
      frameRight: frame ? Math.round(frame.right) : 0,
      mainRight: main ? Math.round(main.right) : 0,
      fadeRight: fade ? Math.round(fade.right) : 0,
    };
  });
  assert.ok(metrics.scrollWidth <= metrics.clientWidth, `${label}: horizontal overflow ${JSON.stringify(metrics)}`);
  assert.ok(metrics.frameRight <= metrics.clientWidth + 1, `${label}: .app-frame wider than viewport ${JSON.stringify(metrics)}`);
  assert.ok(metrics.mainRight <= metrics.clientWidth + 1, `${label}: .app-main wider than viewport ${JSON.stringify(metrics)}`);
  assert.ok(metrics.fadeRight <= metrics.clientWidth + 1, `${label}: .route-fade wider than viewport ${JSON.stringify(metrics)}`);
}

await mkdir(output, { recursive: true });
const server = spawn(process.execPath, ["scripts/static-server.mjs"], { stdio: "inherit", env: { ...process.env, PORT: port } });
let browser;
try {
  await waitForServer();
  browser = await chromium.launch({ headless: true });

  for (const width of widths) {
    const height = width >= 1100 ? 900 : 740;
    const phone = width <= 600;
    const page = await browser.newPage({ viewport: { width, height }, locale: "id-ID", timezoneId: "Asia/Jakarta", reducedMotion: "reduce" });
    const errors = [];
    page.on("pageerror", err => errors.push(err.message));
    await intercept(page, superAdmin);

    for (const path of routes) {
      await page.goto(`${baseURL}${path}`, { waitUntil: "networkidle" });
      await page.locator("#main-content").waitFor();
      await assertNoHorizontalOverflow(page, `${width}px ${path}`);
      if (phone) {
        const smallInputs = await page.evaluate(() => [...document.querySelectorAll("input, select, textarea")].filter(el => {
          if (el.tagName === "INPUT" && ["hidden", "checkbox", "radio", "file", "button", "submit", "reset"].includes(el.type)) return false;
          const rect = el.getBoundingClientRect();
          return rect.width > 0 && rect.height > 0 && getComputedStyle(el).visibility !== "hidden";
        }).map(el => ({
          name: el.name || el.getAttribute("aria-label") || el.tagName,
          fontSize: Number.parseFloat(getComputedStyle(el).fontSize),
        })).filter(item => item.fontSize < 16));
        assert.deepEqual(smallInputs, [], `${width}px ${path} has inputs below 16px: ${JSON.stringify(smallInputs)}`);
      }
    }

    await page.goto(`${baseURL}/`, { waitUntil: "networkidle" });
    const homeCategoryCheck = await page.evaluate(() => {
      const items = [...document.querySelectorAll(".category-bars > li")];
      const ranked = items.filter(el => !el.hasAttribute("data-other")).map(el => getComputedStyle(el.querySelector(".category-bars-name i")).backgroundColor);
      const other = items.find(el => el.hasAttribute("data-other"));
      const probe = document.createElement("span");
      probe.style.backgroundColor = "var(--chart-category-other)";
      document.body.appendChild(probe);
      const sand = getComputedStyle(probe).backgroundColor;
      probe.remove();
      return {
        ranked,
        otherSwatch: other ? getComputedStyle(other.querySelector(".category-bars-name i")).backgroundColor : null,
        otherBar: other ? getComputedStyle(other.querySelector(".category-bars-track > span")).backgroundColor : null,
        sand,
      };
    });
    assert.deepEqual(homeCategoryCheck.ranked, [
      "rgb(31, 111, 104)",
      "rgb(176, 125, 18)",
      "rgb(74, 120, 168)",
      "rgb(138, 79, 134)",
      "rgb(184, 96, 42)",
      "rgb(125, 104, 179)",
    ], `${width}px Beranda category bars use the six Retro hidup colours in rank order`);
    assert.equal(new Set(homeCategoryCheck.ranked).size, homeCategoryCheck.ranked.length, `${width}px Beranda category bars repeat no colour: ${JSON.stringify(homeCategoryCheck.ranked)}`);
    assert.equal(homeCategoryCheck.otherSwatch, homeCategoryCheck.sand, `${width}px Beranda Kategori lain swatch uses sand`);
    assert.equal(homeCategoryCheck.otherBar, homeCategoryCheck.sand, `${width}px Beranda Kategori lain bar uses sand`);
    const heroColors = await page.evaluate(() => {
      const ink = getComputedStyle(document.body).color;
      const amount = getComputedStyle(document.querySelector(".home-hero-amount")).color;
      const figures = [...document.querySelectorAll(".home-figures dd")].map(el => getComputedStyle(el).color);
      return { ink, amount, figures };
    });
    assert.equal(heroColors.amount, heroColors.ink, `${width}px Beranda hero Sisa must render in ink`);
    assert.ok(heroColors.figures.every(color => color === heroColors.ink), `${width}px Beranda hero figures must render in ink: ${JSON.stringify(heroColors)}`);
    if (phone) {
      assert.equal(await page.locator(".overview-trend").isVisible(), false, `${width}px phone Beranda hides daily chart`);
      assert.equal(await page.locator(".overview-wealth").isVisible(), false, `${width}px phone Beranda hides Kekayaan card`);
    } else {
      assert.equal(await page.locator(".overview-trend").isVisible(), true, `${width}px desktop Beranda shows daily chart`);
      assert.equal(await page.locator(".overview-wealth").isVisible(), true, `${width}px desktop Beranda shows Kekayaan card`);
      const lowerBoxes = await page.evaluate(() => ({
        activity: document.querySelector(".overview-activity").getBoundingClientRect().height,
        wealth: document.querySelector(".overview-wealth").getBoundingClientRect().height,
        trendBottom: document.querySelector(".overview-trend").getBoundingClientRect().bottom,
        detailsBottom: document.querySelector(".overview-trend .review-daily").getBoundingClientRect().bottom,
        dailyWrapHeight: document.querySelector(".daily-spend .chart-wrap").getBoundingClientRect().height,
      }));
      assert.ok(lowerBoxes.wealth < lowerBoxes.activity - 40, `${width}px desktop Kekayaan card must not stretch to match Terbaru: ${JSON.stringify(lowerBoxes)}`);
      assert.ok(lowerBoxes.dailyWrapHeight > 240, `${width}px desktop Pengeluaran harian chart grows to fill card: ${JSON.stringify(lowerBoxes)}`);
      assert.ok(lowerBoxes.trendBottom - lowerBoxes.detailsBottom <= 32, `${width}px desktop Pengeluaran harian leaves no dead space: ${JSON.stringify(lowerBoxes)}`);
      await page.getByText("Lihat nilai pengeluaran harian", { exact: true }).click();
      await assertNoHorizontalOverflow(page, `${width}px / expanded daily table`);
    }

    await page.goto(`${baseURL}/analytics`, { waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
    assert.equal(await page.locator(".kpi-note").count(), 0, `${width}px Laporan omits Refund/Tentang posisi siklus line`);
    assert.equal(await page.locator("#where .where-delta").count(), 0, `${width}px Ke mana uangnya omits delta column`);
    assert.ok(await page.locator("#where .where-share").count() >= 1, `${width}px Ke mana uangnya shows share percentages`);
    assert.ok(await page.locator("#history .history-bar .history-seg").count() >= 1, `${width}px Siklus ke siklus draws category-stacked bars`);
    const laporanColors = await page.evaluate(() => ({
      where: [...document.querySelectorAll("#where .where-list > li:not([data-other]) .where-swatch")].map(el => getComputedStyle(el).backgroundColor),
      history: [...document.querySelectorAll("#history .history-cols > li:last-child .history-seg")].map(el => getComputedStyle(el).backgroundColor),
    }));
    assert.deepEqual(laporanColors.where, homeCategoryCheck.ranked.slice(0, 3), `${width}px Laporan Ke mana uangnya shares exact Retro hidup colours with Beranda`);
    assert.deepEqual(laporanColors.history, homeCategoryCheck.ranked.slice(0, 3), `${width}px Laporan Siklus ke siklus shares exact Retro hidup colours with Beranda`);
    const chartPolish = await page.evaluate(() => {
      const barRects = [...document.querySelectorAll("#history .history-bar")].map(el => ({
        width: el.getBoundingClientRect().width,
        buttonWidth: el.closest("button").getBoundingClientRect().width,
      }));
      const plotHeight = document.querySelector("#history .history-plot")?.getBoundingClientRect().height || 0;
      const selectedBg = getComputedStyle(document.querySelector("#history .history-cols > li[data-selected]")).backgroundColor;
      const paceGrid = document.querySelector("#spending-shape .recharts-cartesian-grid")?.getBoundingClientRect();
      const endLabels = [...document.querySelectorAll("#spending-shape [data-pace-end]")].map(el => {
        const rect = el.getBoundingClientRect();
        return { key: el.getAttribute("data-pace-end"), text: el.textContent, left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom };
      });
      return { barRects, plotHeight, selectedBg, endLabels, paceGrid: paceGrid ? { top: paceGrid.top, bottom: paceGrid.bottom } : null };
    });
    if (width >= 1366) {
      assert.ok(chartPolish.barRects.every(item => item.width >= 56 && item.width <= 76), `${width}px Siklus ke siklus bars should be ~56-72px wide: ${JSON.stringify(chartPolish.barRects)}`);
      assert.ok(chartPolish.plotHeight >= 180, `${width}px Siklus ke siklus plot grows to card height: ${chartPolish.plotHeight}`);
    } else if (phone) {
      assert.ok(chartPolish.barRects.every(item => item.width >= item.buttonWidth - 8), `${width}px phone Siklus ke siklus bars fill column width: ${JSON.stringify(chartPolish.barRects)}`);
    }
    assert.notEqual(chartPolish.selectedBg, "rgba(0, 0, 0, 0)", `${width}px Siklus ke siklus keeps current-cycle highlight`);
    assert.equal(new Set(chartPolish.endLabels.map(item => item.key)).size, chartPolish.endLabels.length, `${width}px Laju pengeluaran has one end label per series: ${JSON.stringify(chartPolish.endLabels)}`);
    for (const label of chartPolish.endLabels) {
      if (chartPolish.paceGrid) {
        assert.ok(label.top >= chartPolish.paceGrid.top - 2 && label.bottom <= chartPolish.paceGrid.bottom + 2, `${width}px Laju pengeluaran end label ${label.text} stays inside plot area`);
      }
    }
    for (let i = 0; i < chartPolish.endLabels.length; i += 1) {
      for (let j = i + 1; j < chartPolish.endLabels.length; j += 1) {
        const a = chartPolish.endLabels[i], b = chartPolish.endLabels[j];
        const overlapX = a.left < b.right && b.left < a.right;
        const verticalGap = Math.max(a.top - b.bottom, b.top - a.bottom);
        assert.equal(overlapX && verticalGap < 4, false, `${width}px Laju pengeluaran end labels collide: ${JSON.stringify([a, b])}`);
      }
    }
    const stackRows = await page.evaluate(() => [...document.querySelectorAll(".review-stack .report-disclosure > summary")].map(summary => {
      const style = getComputedStyle(summary);
      const h2 = summary.querySelector("h2");
      const h2Style = getComputedStyle(h2);
      const h2Rect = h2.getBoundingClientRect();
      const span = summary.querySelector(":scope > span");
      const spanStyle = getComputedStyle(span);
      const spanRect = span.getBoundingClientRect();
      return {
        title: h2.textContent,
        pad: `${style.paddingTop} ${style.paddingRight} ${style.paddingBottom} ${style.paddingLeft}`,
        titleSize: h2Style.fontSize,
        titleColor: h2Style.color,
        titleFont: h2Style.fontFamily,
        summarySize: spanStyle.fontSize,
        summaryBelow: spanRect.top >= h2Rect.bottom - 1,
      };
    }));
    assert.ok(stackRows.length >= 2, `${width}px Laporan bottom list has uniform rows`);
    const inkColor = await page.evaluate(() => getComputedStyle(document.body).color);
    for (const row of stackRows) {
      assert.equal(row.pad, "14px 16px 14px 16px", `${width}px ${row.title} equal padding`);
      assert.equal(row.titleSize, "17px", `${width}px ${row.title} title 17px`);
      assert.equal(row.titleColor, inkColor, `${width}px ${row.title} title colour matches ink`);
      assert.match(row.titleFont, /Fraunces/, `${width}px ${row.title} uses Fraunces`);
      assert.equal(row.summarySize, "13px", `${width}px ${row.title} summary 13px`);
      assert.equal(row.summaryBelow, true, `${width}px ${row.title} summary sits on its own line below title`);
    }
    await page.evaluate(() => {
      document.querySelectorAll("details").forEach(details => { details.open = true; });
    });
    await page.waitForTimeout(100);
    await assertNoHorizontalOverflow(page, `${width}px /analytics all disclosures open`);
    if (phone) {
      const wideTables = await page.evaluate(() => [...document.querySelectorAll(".review-table-wrap, .merchant-rank-scroll")].map(el => ({
        cls: el.className,
        label: el.getAttribute("aria-label"),
        scrollWidth: el.scrollWidth,
        clientWidth: el.clientWidth,
      })).filter(item => item.scrollWidth > item.clientWidth));
      assert.deepEqual(wideTables, [], `${width}px Laporan tables still scroll sideways: ${JSON.stringify(wideTables)}`);
    }
    await page.screenshot({ path: fileURLToPath(new URL(`analytics-${width}.png`, output)), fullPage: true });

    await page.goto(`${baseURL}/household?tab=anggota`, { waitUntil: "networkidle" });
    await page.locator("#tambah-anggota").waitFor();
    const anggotaOrder = await page.evaluate(() => {
      const list = [...document.querySelectorAll(".admin-stack > section")].find(section => section.querySelector("h2")?.textContent === "Anggota");
      const form = document.querySelector("#tambah-anggota");
      return { listTop: list.getBoundingClientRect().top, formTop: form.getBoundingClientRect().top };
    });
    assert.ok(anggotaOrder.listTop < anggotaOrder.formTop, `${width}px Keluarga Anggota list must sit above Tambah anggota form: ${JSON.stringify(anggotaOrder)}`);

    await page.goto(`${baseURL}/household?tab=kategori`, { waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Kategori keluarga", exact: true }).waitFor();
    await assertNoHorizontalOverflow(page, `${width}px /household?tab=kategori`);
    const categorySection = await page.locator("section#kategori-keluarga").evaluate(el => ({
      scrollWidth: el.scrollWidth,
      clientWidth: el.clientWidth,
      right: el.getBoundingClientRect().right,
      viewport: document.documentElement.clientWidth,
    }));
    assert.ok(categorySection.scrollWidth <= categorySection.clientWidth + 1 && categorySection.right <= categorySection.viewport + 1, `${width}px Kategori keluarga overflows: ${JSON.stringify(categorySection)}`);

    const adminRow = page.locator("a.mobile-admin-entry[href='/admin']");
    if (width <= 1100) {
      assert.equal(await adminRow.isVisible(), true, `${width}px superAdmin sees Admin Richmod row in Keluarga`);
      assert.match(await adminRow.textContent(), /Admin Richmod/);
    } else {
      assert.equal(await adminRow.isVisible(), false, `${width}px desktop hides mobile Admin Richmod row`);
      assert.equal(await page.locator(".sidebar a.admin-link[href='/admin']").isVisible(), true, `${width}px desktop sidebar keeps Admin link`);
    }
    await page.screenshot({ path: fileURLToPath(new URL(`household-kategori-${width}.png`, output)), fullPage: true });

    await page.goto(`${baseURL}/transactions`, { waitUntil: "networkidle" });
    await page.locator(".transaction-row").first().waitFor();
    await assertNoHorizontalOverflow(page, `${width}px /transactions`);
    const filterPrimary = await page.locator("div.filter-primary").evaluate(el => ({
      scrollWidth: el.scrollWidth,
      clientWidth: el.clientWidth,
      right: el.getBoundingClientRect().right,
      viewport: document.documentElement.clientWidth,
    }));
    assert.ok(filterPrimary.scrollWidth <= filterPrimary.clientWidth + 1 && filterPrimary.right <= filterPrimary.viewport + 1, `${width}px filter-primary overflows: ${JSON.stringify(filterPrimary)}`);

    const catatTrigger = width <= 1100 ? page.getByRole("button", { name: "Catat transaksi", exact: true }) : page.getByRole("button", { name: "Catat", exact: true });
    await catatTrigger.click();
    const quickDialog = page.locator("dialog.quick-record[open]");
    await quickDialog.waitFor();
    const quickMetrics = await quickDialog.evaluate(dialog => {
      const body = dialog.querySelector(".quick-body");
      const seg = dialog.querySelector(".quick-body .segmented");
      const buttons = [...dialog.querySelectorAll(".quick-body .segmented button")].map(button => ({
        text: button.textContent,
        width: Math.round(button.getBoundingClientRect().width),
      }));
      return {
        dialogOverflow: dialog.scrollWidth > dialog.clientWidth,
        bodyScrollWidth: body.scrollWidth,
        bodyClientWidth: body.clientWidth,
        segRight: seg.getBoundingClientRect().right,
        bodyRight: body.getBoundingClientRect().right,
        htmlOverflowY: getComputedStyle(document.documentElement).overflowY,
        buttons,
      };
    });
    assert.equal(quickMetrics.dialogOverflow, false, `${width}px Catat dialog overflows: ${JSON.stringify(quickMetrics)}`);
    assert.ok(quickMetrics.bodyScrollWidth <= quickMetrics.bodyClientWidth, `${width}px .quick-body scrolls sideways: ${JSON.stringify(quickMetrics)}`);
    assert.ok(quickMetrics.segRight <= quickMetrics.bodyRight + 1, `${width}px segmented control overflows .quick-body: ${JSON.stringify(quickMetrics)}`);
    assert.equal(quickMetrics.htmlOverflowY, "hidden", `${width}px background scroll must lock while Catat dialog is open`);
    if (width === 360 || width === 390) {
      const kbHeight = 320;
      const vvHeight = height - kbHeight;
      const beforeKb = await quickDialog.evaluate(dialog => {
        const amount = dialog.querySelector('input[name="amount"]');
        const repeat = dialog.querySelector('[aria-label="Ulangi cepat"]');
        const repeatRow = repeat?.querySelector("div");
        const desc = dialog.querySelector('input[name="description"]');
        return {
          focusedName: document.activeElement?.getAttribute("name") || "",
          amountHint: amount.getAttribute("enterkeyhint"),
          descHint: desc.getAttribute("enterkeyhint"),
          amountBottom: amount.getBoundingClientRect().bottom,
          repeatTop: repeat.getBoundingClientRect().top,
          repeatBottom: repeat.getBoundingClientRect().bottom,
          descTop: desc.getBoundingClientRect().top,
          repeatScrollsX: repeatRow.scrollWidth > repeatRow.clientWidth,
        };
      });
      assert.equal(beforeKb.focusedName, "amount", `${width}px Catat autofocuses amount`);
      assert.equal(beforeKb.amountHint, "next", `${width}px Catat amount uses enterKeyHint=next`);
      assert.equal(beforeKb.descHint, "done", `${width}px Catat description uses enterKeyHint=done`);
      assert.ok(beforeKb.repeatTop >= beforeKb.amountBottom - 2 && beforeKb.repeatBottom <= beforeKb.descTop + 2, `${width}px Ulangi cepat sits directly between amount and description: ${JSON.stringify(beforeKb)}`);
      assert.equal(beforeKb.repeatScrollsX, true, `${width}px Ulangi cepat chip row scrolls horizontally without widening the sheet`);

      await page.evaluate(simulatedHeight => {
        Object.defineProperty(window.visualViewport, "height", { configurable: true, get: () => simulatedHeight });
        window.visualViewport.dispatchEvent(new Event("resize"));
      }, vvHeight);
      await page.waitForTimeout(50);

      const duringKb = await quickDialog.evaluate(dialog => {
        const docStyle = getComputedStyle(document.documentElement);
        const submitBtn = dialog.querySelector('.quick-savebar button[type="submit"]');
        const amount = dialog.querySelector('input[name="amount"]');
        const repeat = dialog.querySelector('[aria-label="Ulangi cepat"]');
        const nav = document.querySelector(".mobile-nav");
        return {
          kbInset: docStyle.getPropertyValue("--kb-inset").trim(),
          vvHeight: docStyle.getPropertyValue("--vv-height").trim(),
          navDisplay: nav ? getComputedStyle(nav).display : "none",
          submitRect: submitBtn.getBoundingClientRect().toJSON(),
          amountRect: amount.getBoundingClientRect().toJSON(),
          repeatRect: repeat.getBoundingClientRect().toJSON(),
          dialogOverflow: dialog.scrollWidth > dialog.clientWidth,
        };
      });
      assert.equal(duringKb.kbInset, `${kbHeight}px`, `${width}px --kb-inset matches keyboard height`);
      assert.equal(duringKb.vvHeight, `${vvHeight}px`, `${width}px --vv-height matches visual viewport height`);
      assert.equal(duringKb.navDisplay, "none", `${width}px .mobile-nav hides while typing`);
      assert.equal(duringKb.dialogOverflow, false, `${width}px Catat dialog has no horizontal overflow with keyboard open`);
      assert.ok(duringKb.submitRect.top >= 0 && duringKb.submitRect.bottom <= vvHeight + 1, `${width}px Catat button must stay inside visual viewport (${vvHeight}px): ${JSON.stringify(duringKb.submitRect)}`);
      assert.ok(duringKb.amountRect.top >= 0 && duringKb.amountRect.bottom <= vvHeight + 1, `${width}px focused amount stays inside visual viewport: ${JSON.stringify(duringKb.amountRect)}`);
      assert.ok(duringKb.repeatRect.top >= 0 && duringKb.repeatRect.bottom <= vvHeight + 1, `${width}px Ulangi cepat row stays visible above keyboard: ${JSON.stringify(duringKb.repeatRect)}`);

      await page.keyboard.press("Enter");
      const afterEnter = await page.evaluate(() => ({
        focusedName: document.activeElement?.getAttribute("name") || "",
        dialogOpen: Boolean(document.querySelector("dialog.quick-record[open]")),
      }));
      assert.equal(afterEnter.focusedName, "description", `${width}px Enter on amount moves focus to description`);
      assert.equal(afterEnter.dialogOpen, true, `${width}px Enter on amount never submits half-filled form`);

      await page.evaluate(() => {
        delete window.visualViewport.height;
        window.visualViewport.dispatchEvent(new Event("resize"));
      });
      await page.waitForTimeout(50);
    }
    await page.screenshot({ path: fileURLToPath(new URL(`catat-${width}.png`, output)) });
    await page.keyboard.press("Escape");
    await quickDialog.waitFor({ state: "detached" });

    await page.locator(".transaction-row").nth(4).click();
    const detailDrawer = page.locator("aside[role='dialog'].detail-drawer");
    await detailDrawer.waitFor();
    await page.waitForFunction(() => {
      const el = document.querySelector("aside[role='dialog'].detail-drawer");
      return el && getComputedStyle(el).transform === "none";
    });
    const scrollBefore = await page.evaluate(() => window.scrollY);
    const drawerState = await detailDrawer.evaluate(el => {
      const rect = el.getBoundingClientRect();
      return {
        htmlOverflowY: getComputedStyle(document.documentElement).overflowY,
        overscrollY: getComputedStyle(el).overscrollBehaviorY,
        left: Math.round(rect.left),
        top: Math.round(rect.top),
        width: Math.round(rect.width),
        height: Math.round(rect.height),
        viewportWidth: document.documentElement.clientWidth,
        viewportHeight: window.innerHeight,
        drawerOverflowX: el.scrollWidth > el.clientWidth,
      };
    });
    assert.equal(drawerState.htmlOverflowY, "hidden", `${width}px html must lock scroll while drawer is open`);
    assert.equal(drawerState.overscrollY, "contain", `${width}px drawer must contain overscroll`);
    assert.equal(drawerState.drawerOverflowX, false, `${width}px drawer must not scroll sideways`);
    if (phone) {
      assert.equal(drawerState.left, 0, `${width}px phone drawer fills full width`);
      assert.equal(drawerState.top, 0, `${width}px phone drawer starts at top`);
      assert.ok(Math.abs(drawerState.width - drawerState.viewportWidth) <= 1, `${width}px phone drawer width matches viewport: ${JSON.stringify(drawerState)}`);
      assert.ok(Math.abs(drawerState.height - drawerState.viewportHeight) <= 1, `${width}px phone drawer height fills screen: ${JSON.stringify(drawerState)}`);
    }
    await page.mouse.move(10, 10);
    await page.mouse.wheel(0, 600);
    await page.waitForTimeout(80);
    const scrollDuring = await page.evaluate(() => window.scrollY);
    assert.equal(scrollDuring, scrollBefore, `${width}px background page must not scroll while drawer is open`);
    await page.screenshot({ path: fileURLToPath(new URL(`drawer-${width}.png`, output)) });
    if (width === 360 || width === 390) {
      const kbHeight = 320;
      const vvHeight = height - kbHeight;
      await detailDrawer.getByRole("button", { name: "Ubah", exact: true }).click();
      const editDialog = page.locator("dialog.create-dialog[open]");
      await editDialog.waitFor();
      await editDialog.locator('input[name="description"]').focus();
      await page.evaluate(simulatedHeight => {
        Object.defineProperty(window.visualViewport, "height", { configurable: true, get: () => simulatedHeight });
        window.visualViewport.dispatchEvent(new Event("resize"));
      }, vvHeight);
      await page.waitForTimeout(50);
      const editKb = await editDialog.evaluate(dialog => {
        const descLabelSpan = dialog.querySelector('label:has(input[name="description"]) > span');
        const mark = descLabelSpan?.querySelector(".required-mark");
        return {
          saveRect: dialog.querySelector('.dialog-actions button:last-child').getBoundingClientRect().toJSON(),
          dialogOverflow: dialog.scrollWidth > dialog.clientWidth,
          descSpanRect: descLabelSpan?.getBoundingClientRect().toJSON() || null,
          markRect: mark?.getBoundingClientRect().toJSON() || null,
        };
      });
      assert.equal(editKb.dialogOverflow, false, `${width}px Ubah dialog must not overflow horizontally`);
      assert.ok(editKb.descSpanRect && editKb.markRect, `${width}px Ubah Deskripsi renders inline required mark inside label span`);
      assert.ok(editKb.descSpanRect.height < 28 && Math.abs(editKb.markRect.top - editKb.descSpanRect.top) < 6, `${width}px Ubah Deskripsi required mark stays inline: ${JSON.stringify(editKb)}`);
      assert.ok(editKb.saveRect.top >= 0 && editKb.saveRect.bottom <= vvHeight + 1, `${width}px Ubah Simpan button stays inside visual viewport (${vvHeight}px): ${JSON.stringify(editKb.saveRect)}`);
      await page.evaluate(() => {
        delete window.visualViewport.height;
        window.visualViewport.dispatchEvent(new Event("resize"));
      });
      await page.waitForTimeout(50);
      await page.keyboard.press("Escape");
      await editDialog.waitFor({ state: "detached" });
    }
    await page.keyboard.press("Escape");
    await detailDrawer.waitFor({ state: "detached" });
    assert.notEqual(await page.evaluate(() => getComputedStyle(document.documentElement).overflowY), "hidden", `${width}px scroll unlocks when drawer closes`);

    assert.deepEqual(errors, [], `${width}px runtime errors: ${errors.join("\n")}`);
    console.log(`Mobile layout PASS: ${width}px`);
    await page.close();
  }

  const nonAdminPage = await browser.newPage({ viewport: { width: 390, height: 844 }, locale: "id-ID", timezoneId: "Asia/Jakarta" });
  await intercept(nonAdminPage, regularOwner);
  await nonAdminPage.goto(`${baseURL}/household`, { waitUntil: "networkidle" });
  await nonAdminPage.locator("#main-content").waitFor();
  assert.equal(await nonAdminPage.locator("a.mobile-admin-entry[href='/admin']").count(), 0, "non-superAdmin never sees Admin Richmod row");
  await nonAdminPage.close();
  console.log("Mobile layout PASS: non-superAdmin hides Admin Richmod");
} finally {
  await browser?.close();
  server.kill("SIGTERM");
}
