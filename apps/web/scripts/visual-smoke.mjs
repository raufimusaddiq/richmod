import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { chromium } from "playwright";

const port = process.env.RICHMOD_VISUAL_PORT || "3200";
const baseURL = process.env.RICHMOD_VISUAL_BASE_URL || `http://127.0.0.1:${port}`;
const output = new URL("../test-results/visual-smoke/", import.meta.url);
const regressionOutput = new URL("../test-results/visual-regression/", import.meta.url);
const baselines = new URL("../tests/visual-baselines/", import.meta.url);
const updateBaselines = process.env.UPDATE_VISUAL_BASELINES === "1";
const routes = ["/", "/transactions", "/analytics", "/inbox", "/documents", "/household", "/settings", "/admin"];
const viewports = [
  ["desktop", 1440, 900],
  ["tablet", 1024, 768],
  ["mobile", 390, 844],
  ["wide", 2560, 1440],
];

const user = { id: "user-1", displayName: "Rafi", email: "rafi@example.test", isSuperAdmin: true, householdName: "Rumah Rafi", household: { role: "OWNER" } };
const transactions = [
  { id: "tx-1", type: "EXPENSE", status: "CONFIRMED", amount: "185000", merchantName: "Pasar Minggu", categoryName: "Belanja rumah", memberName: "Rafi", sourceType: "BANK_EMAIL", transactionAt: "2026-09-06T09:20:00+07:00", accountName: "Jago utama" },
  { id: "tx-2", type: "INCOME", status: "CONFIRMED", amount: "12500000", merchantName: "Gaji September", categoryName: "Pendapatan", memberName: "Rafi", sourceType: "BANK_EMAIL", transactionAt: "2026-09-01T08:00:00+07:00", accountName: "Jago utama" },
  { id: "tx-3", type: "TRANSFER", status: "NEEDS_REVIEW", amount: "750000", counterpartyName: "Transfer ke rekening lain", categoryName: null, memberName: "Rafi", sourceType: "BANK_EMAIL", transactionAt: "2026-09-05T15:00:00+07:00", accountName: "Jago utama" },
];
const categories = [
  { id: "cat-1", name: "Belanja rumah", amount: "1250000", active: true, slug: "belanja-rumah" },
  { id: "cat-2", name: "Transportasi", amount: "640000", active: true, slug: "transportasi" },
  { id: "cat-3", name: "Makan", amount: "420000", active: true, slug: "makan" },
  { id: "cat-4", name: "Tagihan", amount: "280000", active: true, slug: "tagihan" },
];
const daily = Array.from({ length: 12 }, (_, index) => ({ period: `2026-09-${String(index + 1).padStart(2, "0")}`, expense: String((index % 4) * 80000 + 120000), income: index === 0 ? "12500000" : "0" }));
const review = [{ id: "review-1", reason: "AMBIGUOUS_CATEGORY", amount: "750000", merchantName: "Transfer ke rekening lain", transactionAt: "2026-09-05T15:00:00+07:00", sourceType: "BANK_EMAIL", proposalStatus: "NEEDS_REVIEW", missingFields: ["merchant"], description: "Tujuan transfer belum jelas" }];
const members = [{ id: "member-1", displayName: "Rafi", email: "rafi@example.test", role: "OWNER", active: true, telegramConnected: true }, { id: "member-2", displayName: "Dina", email: "dina@example.test", role: "MEMBER", active: true, telegramConnected: false }];
const document = { id: "doc-1", status: "SUCCEEDED", documentType: "RECEIPT", sourceType: "WEB_IMAGE", createdAt: "2026-09-06T10:00:00+07:00", confidence: 0.93, linkedTransactionIds: ["tx-1"], summary: { merchant: "Pasar Minggu", amount: "185000" }, needsReview: false };

function fixture(path) {
  if (path === "/api/v1/auth/me") return user;
  if (path === "/api/v1/analytics/overview") return { income: "12500000", expense: "2590000", netCashflow: "9910000", reviewCount: 1, periodKind: "CURRENT_CYCLE" };
  if (path === "/api/v1/analytics/cycle" || path === "/api/v1/analytics/cycle/daily") return { kind: "CURRENT_CYCLE", start: "2026-09-01", end: "2026-09-30", cycleStart: "2026-09-01", salary: "12500000", spent: "2590000", remaining: "9910000", daysElapsed: 6, daysTotal: 30, daily };
  if (path.startsWith("/api/v1/analytics/categories")) return categories;
  if (path.startsWith("/api/v1/analytics/cashflow")) return [{ period: "2026-07", income: "11800000", expense: "7200000", netCashflow: "4600000" }, { period: "2026-08", income: "12500000", expense: "8100000", netCashflow: "4400000" }, { period: "2026-09", income: "12500000", expense: "2590000", netCashflow: "9910000" }];
  if (path.startsWith("/api/v1/analytics/spending")) return daily.map(item => ({ period: item.period, expense: item.expense, refund: "0", netSpending: item.expense }));
  if (path.startsWith("/api/v1/analytics/merchants")) return [{ name: "Pasar Minggu", amount: "1250000" }, { name: "Grab", amount: "640000" }];
  if (path.startsWith("/api/v1/analytics/members")) return [{ name: "Rafi", amount: "2590000" }];
  if (path === "/api/v1/insights") return [];
  if (/\/api\/v1\/transactions\/[^/]+\/evidence$/.test(path)) return [{ id: "ev-1", evidenceType: "BANK_EMAIL", sourceType: "BANK_EMAIL", receivedAt: "2026-09-06T09:20:00+07:00" }];
  if (/\/api\/v1\/transactions\/[^/]+\/audit$/.test(path)) return [{ id: "audit-1", action: "CONFIRM", actorType: "SYSTEM", createdAt: "2026-09-06T09:21:00+07:00" }];
  if (/\/api\/v1\/transactions\/[^/]+$/.test(path)) return transactions.find(item => path.endsWith(item.id)) || transactions[0];
  if (path.startsWith("/api/v1/transactions")) return transactions;
  if (path === "/api/v1/reviews") return review;
  if (path === "/api/v1/integration-actions") return [];
  if (path === "/api/v1/categories") return categories;
  if (path === "/api/v1/accounts") return [{ id: "account-1", name: "Jago utama", accountType: "BANK", trackingPolicy: "SPENDING_ONLY", active: true }];
  if (path === "/api/v1/household") return { id: "household-1", name: "Rumah Rafi" };
  if (path === "/api/v1/household/members") return members;
  if (path === "/api/v1/documents") return [document];
  if (/\/api\/v1\/documents\/[^/]+\/pages$/.test(path)) return [{ index: 0 }, { index: 1 }];
  if (/\/api\/v1\/documents\/[^/]+\/extraction$/.test(path)) return [{ stage: "RECEIPT", schemaVersion: 1, validated: true, output: { merchant: "Pasar Minggu", amount: "185000" } }];
  if (path === "/api/v1/merchant-aliases") return [{ id: "alias-1", normalizedName: "Pasar Minggu", rawName: "PASAR MINGGU", defaultCategoryName: "Belanja rumah", autoApply: true }];
  if (path === "/api/v1/known-accounts") return [{ id: "known-1", institution: "Bank", displayName: "Rekening keluarga", matchHint: "1234", relationship: "HOUSEHOLD" }];
  if (path === "/api/v1/operations/status") return { status: "HEALTHY", checkedAt: "2026-09-07T10:00:00+07:00", jobs: { pending: 0, failed: 0 }, reviewBacklog: 1, worker: { healthy: true }, llmGateway: { configured: true } };
  if (path === "/api/v1/salary/sources") return [{ id: "salary-1", employer: "Richmod Labs", active: true, isPrimary: true }];
  if (path === "/api/v1/bank-email-listeners") return [];
  if (path === "/api/v1/integrations/email-ingress") return { address: "household@example.richmod.link", status: "ACTIVE", lastReceivedAt: "2026-09-06T09:20:00+07:00" };
  if (path === "/api/v1/admin/overview") return { status: "HEALTHY", checkedAt: "2026-09-07T10:00:00Z", worker: { healthy: true, lastHeartbeatAt: "2026-09-07T10:00:00Z" }, jobs: { pending: 0, running: 0, failed24h: 0, lanes: ["INTERACTIVE", "CHAT", "DEFAULT", "BACKGROUND"].map(lane => ({ lane, pending: 0, running: 0, oldestDueAgeMs: null })) }, llm: { calls24h: 12, failed24h: 0, successRate: 1, p95DurationMs: 820 }, reviews: { open: 1 }, households: { total: 1 }, integrations: { llmGatewayConfigured: true, llmProtocol: "Cloud gateway" }, recentEvents: [] };
  if (path === "/api/v1/admin/jobs") return { items: [{ id: "job-1234567890abcdef", status: "SUCCEEDED", type: "SYNC", lane: "DEFAULT", attempts: 1, maxAttempts: 3, startedAt: "2026-09-07T09:58:00Z", finishedAt: "2026-09-07T09:58:02Z", updatedAt: "2026-09-07T09:58:02Z" }], nextCursor: null };
  if (path.startsWith("/api/v1/admin/")) return { items: [], nextCursor: null };
  return [];
}

async function intercept(page, authenticated = true, requests = []) {
  await page.route("**/api/v1/**", route => {
    const path = new URL(route.request().url()).pathname;
    requests.push({ path, method: route.request().method() });
    if (!authenticated && path === "/api/v1/auth/me") return route.fulfill({ status: 401, contentType: "application/json", body: "{}" });
    if (/\/api\/v1\/documents\/[^/]+\/(content|pages\/\d+\/content)$/.test(path)) return route.fulfill({ contentType: "image/png", body: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64") });
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(fixture(path)) });
  });
}

async function waitForServer() {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    try { if ((await fetch(baseURL)).ok) return; } catch {}
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(`Next.js did not start at ${baseURL}`);
}

async function compareScreenshot(page, name) {
  const screenshot = await page.screenshot({ animations: "disabled", caret: "hide", fullPage: true });
  await writeFile(new URL(`${name}.png`, regressionOutput), screenshot);
  const baselinePath = new URL(`${name}.png`, baselines);
  if (updateBaselines) {
    await writeFile(baselinePath, screenshot);
    return;
  }
  const baseline = await readFile(baselinePath);
  const result = await page.evaluate(async ({ actual, expected }) => {
    const pixels = async encoded => {
      const image = new Image();
      image.src = `data:image/png;base64,${encoded}`;
      await image.decode();
      const canvas = document.createElement("canvas");
      canvas.width = image.naturalWidth;
      canvas.height = image.naturalHeight;
      const context = canvas.getContext("2d", { willReadFrequently: true });
      context.drawImage(image, 0, 0);
      return { width: canvas.width, height: canvas.height, data: context.getImageData(0, 0, canvas.width, canvas.height).data };
    };
    const current = await pixels(actual);
    const reference = await pixels(expected);
    if (current.width !== reference.width || current.height !== reference.height) return { width: current.width, height: current.height, expectedWidth: reference.width, expectedHeight: reference.height, ratio: 1 };
    let changed = 0;
    for (let index = 0; index < current.data.length; index += 4) {
      if (Math.abs(current.data[index] - reference.data[index]) > 24 || Math.abs(current.data[index + 1] - reference.data[index + 1]) > 24 || Math.abs(current.data[index + 2] - reference.data[index + 2]) > 24 || Math.abs(current.data[index + 3] - reference.data[index + 3]) > 24) changed += 1;
    }
    return { width: current.width, height: current.height, expectedWidth: reference.width, expectedHeight: reference.height, ratio: changed / (current.width * current.height) };
  }, { actual: screenshot.toString("base64"), expected: baseline.toString("base64") });
  assert.equal(result.width, result.expectedWidth, `${name} width changed`);
  assert.equal(result.height, result.expectedHeight, `${name} height changed`);
  assert.ok(result.ratio <= 0.005, `${name} visual difference ${(result.ratio * 100).toFixed(2)}% exceeds 0.50%`);
}

async function run() {
  await mkdir(output, { recursive: true });
  await mkdir(regressionOutput, { recursive: true });
  await mkdir(baselines, { recursive: true });
  const server = process.env.RICHMOD_VISUAL_BASE_URL ? null : spawn("npm", ["run", "start", "--", "-p", port], { stdio: "inherit", shell: process.platform === "win32" });
  try {
    await waitForServer();
    const browser = await chromium.launch({ headless: true });
    try {
      for (const [name, width, height] of viewports) {
        const page = await browser.newPage({ viewport: { width, height }, locale: "id-ID", timezoneId: "Asia/Jakarta" });
        const errors = [];
        await intercept(page);
        page.on("pageerror", error => errors.push(error.message));
        page.on("console", message => { if (message.type() === "error" && !message.text().includes("favicon")) errors.push(message.text()); });
        for (const path of routes) {
          console.log(`Visual smoke: ${name} ${path}`);
          await page.goto(`${baseURL}${path}`, { waitUntil: "networkidle" });
          await page.locator("#main-content").waitFor();
          const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1);
          assert.equal(overflow, false, `${name} ${path} has horizontal overflow`);
          const slug = path === "/" ? "overview" : path.slice(1);
          await page.screenshot({ path: new URL(`${name}-${slug}.png`, output).pathname, fullPage: true });
          if (path === "/transactions") {
            const row = page.locator(".transaction-row").first();
            await row.hover();
            const hover = await row.evaluate(element => getComputedStyle(element).backgroundColor);
            assert.notEqual(hover, "rgb(40, 91, 70)", `${name} transaction row uses primary hover background`);
            await row.focus();
            const focus = await row.evaluate(element => getComputedStyle(element).outlineStyle);
            assert.notEqual(focus, "none", `${name} transaction row has no keyboard focus ring`);
          }
          if (path === "/admin") {
            const metricsBackground = await page.locator(".admin-metrics").evaluate(element => getComputedStyle(element).backgroundColor);
            assert.notEqual(metricsBackground, "rgb(221, 220, 213)", `${name} admin metrics expose an empty separator cell`);
            const lane = page.locator(".admin-lane").first();
            assert.equal(await lane.count(), 1);
            assert.equal(await lane.evaluate(element => getComputedStyle(element).display), "grid");
            await page.getByRole("button", { name: "Jobs" }).click();
            await page.locator("button.admin-link").first().waitFor();
            const link = await page.locator("button.admin-link").first().evaluate(element => { const style = getComputedStyle(element); return { display: style.display, background: style.backgroundColor, minHeight: style.minHeight }; });
            assert.notEqual(link.display, "flex", `${name} admin ID inherited button flex layout`);
            assert.equal(link.background, "rgba(0, 0, 0, 0)", `${name} admin ID has button background`);
            assert.equal(link.minHeight, "0px", `${name} admin ID has button minimum height`);
            await page.screenshot({ path: new URL(`${name}-admin-jobs.png`, output).pathname, fullPage: true });
          }
        }
        await page.goto(`${baseURL}/analytics`, { waitUntil: "networkidle" });
        await page.getByRole("button", { name: "Kalender" }).click();
        await page.getByRole("heading", { name: "Pemasukan vs pengeluaran" }).waitFor();
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} analytics calendar has horizontal overflow`);
        await page.screenshot({ path: new URL(`${name}-analytics-calendar.png`, output).pathname, fullPage: true });
        await page.getByRole("button", { name: "Siklus Gaji" }).click();
        await page.getByRole("heading", { name: "Pola pengeluaran siklus ini" }).waitFor();
        await page.locator(".recharts-bar-rectangle").first().hover();
        await page.locator(".chart-tooltip").waitFor();
        await page.screenshot({ path: new URL(`${name}-analytics-tooltip.png`, output).pathname, fullPage: true });
        await page.goto(`${baseURL}/transactions`, { waitUntil: "networkidle" });
        await page.locator(".transaction-row").first().click();
        await page.getByRole("button", { name: "Tutup detail" }).waitFor();
        await page.screenshot({ path: new URL(`${name}-transaction-drawer.png`, output).pathname, fullPage: true });
        await page.getByRole("button", { name: "Tutup detail" }).click();
        await page.getByRole("button", { name: /Tambah transaksi|Catat transaksi|Transaksi manual/i }).click();
        await page.locator("dialog[open]").waitFor();
        await page.screenshot({ path: new URL(`${name}-transaction-dialog.png`, output).pathname, fullPage: true });
        await page.locator("dialog[open]").getByRole("button", { name: "Batal" }).click();
        await page.goto(`${baseURL}/documents`, { waitUntil: "networkidle" });
        const documentCard = page.locator(".document-card").first();
        await documentCard.hover();
        const documentHover = await documentCard.evaluate(element => { const style = getComputedStyle(element); return { background: style.backgroundColor, color: style.color }; });
        assert.notEqual(documentHover.background, "rgb(86, 52, 72)", `${name} document card uses primary hover background`);
        assert.equal(documentHover.color, "rgb(40, 37, 34)", `${name} document card text changes on hover`);
        await page.screenshot({ path: new URL(`${name}-documents-hover.png`, output).pathname, fullPage: true });
        await documentCard.click();
        await page.getByRole("dialog", { name: "Detail dokumen" }).waitFor();
        await page.screenshot({ path: new URL(`${name}-document-drawer.png`, output).pathname, fullPage: true });
        if (name === "mobile") {
          await page.getByRole("button", { name: "Tutup detail" }).click();
          await page.getByRole("button", { name: "Lainnya" }).click();
          await page.getByRole("dialog", { name: "Menu lainnya" }).waitFor();
          await page.screenshot({ path: new URL("mobile-more-menu.png", output).pathname, fullPage: true });
        }
        assert.deepEqual(errors, [], `${name} browser errors:\n${errors.join("\n")}`);
        await page.close();
      }
      const regressionPage = await browser.newPage({ viewport: { width: 1440, height: 900 }, locale: "id-ID", timezoneId: "Asia/Jakarta" });
      await intercept(regressionPage);
      for (const [name, path] of [["overview-desktop", "/"], ["transactions-desktop", "/transactions"], ["inbox-desktop", "/inbox"], ["analytics-cycle-desktop", "/analytics"]]) {
        await regressionPage.goto(`${baseURL}${path}`, { waitUntil: "networkidle" });
        await regressionPage.locator("#main-content").waitFor();
        await compareScreenshot(regressionPage, name);
      }
      await regressionPage.goto(`${baseURL}/analytics`, { waitUntil: "networkidle" });
      await regressionPage.getByRole("button", { name: "Kalender" }).click();
      await regressionPage.getByRole("heading", { name: "Pemasukan vs pengeluaran" }).waitFor();
      await compareScreenshot(regressionPage, "analytics-calendar-desktop");
      await regressionPage.getByRole("button", { name: "Siklus Gaji" }).click();
      await regressionPage.getByRole("heading", { name: "Pola pengeluaran siklus ini" }).waitFor();
      await regressionPage.locator(".recharts-bar-rectangle").first().hover();
      await regressionPage.locator(".chart-tooltip").waitFor();
      await compareScreenshot(regressionPage, "analytics-cycle-tooltip-desktop");
      await regressionPage.close();
      const wideRegressionPage = await browser.newPage({ viewport: { width: 1920, height: 1080 }, locale: "id-ID", timezoneId: "Asia/Jakarta" });
      await intercept(wideRegressionPage);
      await wideRegressionPage.goto(`${baseURL}/`, { waitUntil: "networkidle" });
      await wideRegressionPage.locator("#main-content").waitFor();
      assert.equal(await wideRegressionPage.evaluate(() => {
        const main = document.querySelector(".app-main");
        return Math.abs(main.getBoundingClientRect().right - window.innerWidth) <= 1;
      }), true, "wide authenticated shell reaches the viewport edge");
      await compareScreenshot(wideRegressionPage, "overview-wide-desktop");
      await wideRegressionPage.close();
      const regressionMobile = await browser.newPage({ viewport: { width: 390, height: 844 }, locale: "id-ID", timezoneId: "Asia/Jakarta" });
      await intercept(regressionMobile);
      await regressionMobile.goto(`${baseURL}/analytics`, { waitUntil: "networkidle" });
      await regressionMobile.locator("#main-content").waitFor();
      await compareScreenshot(regressionMobile, "analytics-cycle-mobile");
      await regressionMobile.close();
      const login = await browser.newPage({ viewport: { width: 390, height: 844 } });
      await intercept(login, false);
      await login.goto(`${baseURL}/login`, { waitUntil: "networkidle" });
      await login.getByRole("button", { name: "Masuk ke Richmod" }).waitFor();
      await login.screenshot({ path: new URL("mobile-login.png", output).pathname, fullPage: true });
      assert.equal(await login.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, "mobile login has horizontal overflow");
      await login.close();

      for (const [name, path, width, height, target] of [["landing-desktop", "/", 1440, 900, "Keuangan keluarga, tanpa menebak."], ["landing-tablet", "/", 1024, 768, "Keuangan keluarga, tanpa menebak."], ["landing-mobile", "/", 390, 844, "Keuangan keluarga, tanpa menebak."], ["login-desktop", "/login", 1440, 900, "Masuk ke Richmod."], ["login-mobile", "/login", 390, 844, "Masuk ke Richmod."]]) {
        const publicPage = await browser.newPage({ viewport: { width, height }, locale: "id-ID", timezoneId: "Asia/Jakarta" });
        const errors = [];
        await intercept(publicPage, false);
        publicPage.on("pageerror", error => errors.push(error.message));
        publicPage.on("console", message => { if (message.type() === "error" && !message.text().includes("favicon") && !message.text().includes("401")) errors.push(message.text()); });
        await publicPage.goto(`${baseURL}${path}`, { waitUntil: "networkidle" });
        await publicPage.getByRole("heading", { name: target }).waitFor();
        assert.equal(await publicPage.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${name} has horizontal overflow`);
        await publicPage.screenshot({ path: new URL(`${name}.png`, output).pathname, fullPage: true });
        await compareScreenshot(publicPage, name);
        if (path === "/" && name === "landing-desktop") {
          await publicPage.getByRole("link", { name: "Privasi" }).first().click();
          await publicPage.getByRole("heading", { name: "Kebijakan Privasi", level: 1 }).waitFor();
          await publicPage.goto(`${baseURL}/`, { waitUntil: "networkidle" });
        }
        assert.deepEqual(errors, [], `${name} browser errors:\n${errors.join("\n")}`);
        await publicPage.close();
      }

      const loginSubmit = await browser.newPage({ viewport: { width: 390, height: 844 } });
      const loginRequests = [];
      await intercept(loginSubmit, false, loginRequests);
      await loginSubmit.goto(`${baseURL}/login`, { waitUntil: "networkidle" });
      await loginSubmit.locator("#login-email").fill("rafi@example.test");
      await loginSubmit.locator("#login-password").fill("password-example");
      await loginSubmit.getByRole("button", { name: "Masuk ke Richmod" }).click();
      await loginSubmit.waitForTimeout(50);
      assert.ok(loginRequests.some(request => request.path === "/api/v1/auth/login" && request.method === "POST"), "login submits to the existing endpoint");
      await loginSubmit.close();

      const authenticatedLogin = await browser.newPage({ viewport: { width: 1440, height: 900 } });
      await intercept(authenticatedLogin, true);
      await authenticatedLogin.goto(`${baseURL}/login`, { waitUntil: "networkidle" });
      await authenticatedLogin.waitForURL(`${baseURL}/`);
      await authenticatedLogin.locator("#main-content").waitFor();
      await authenticatedLogin.close();

      const legalPage = await browser.newPage({ viewport: { width: 1024, height: 768 } });
      await intercept(legalPage, false);
      for (const [path, heading] of [["/privacy", "Kebijakan Privasi"], ["/terms", "Ketentuan Layanan"]]) {
        await legalPage.goto(`${baseURL}${path}`, { waitUntil: "networkidle" });
        await legalPage.getByRole("heading", { name: heading, level: 1 }).waitFor();
        assert.equal(await legalPage.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, `${path} has horizontal overflow`);
      }
      await legalPage.close();
    } finally {
      await browser.close();
    }
  } finally {
    server?.kill("SIGTERM");
  }
}

await run();
