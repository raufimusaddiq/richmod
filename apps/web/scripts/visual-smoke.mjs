import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { chromium } from "playwright";

const port = process.env.RICHMOD_VISUAL_PORT || "3200";
const baseURL = process.env.RICHMOD_VISUAL_BASE_URL || `http://127.0.0.1:${port}`;
const output = new URL("../test-results/visual-smoke/", import.meta.url);
const routes = ["/", "/transactions", "/analytics", "/inbox", "/documents", "/household", "/settings", "/admin"];
const viewports = [
  ["desktop", 1440, 900],
  ["tablet", 1024, 768],
  ["mobile", 390, 844],
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
  if (path === "/api/v1/admin/overview") return { status: "HEALTHY", checkedAt: "2026-09-07T10:00:00Z", worker: { healthy: true, lastHeartbeatAt: "2026-09-07T10:00:00Z" }, jobs: { pending: 0, running: 0, failed24h: 0, lanes: [{ lane: "default", pending: 0, running: 0, oldestDueAgeMs: null }] }, llm: { calls24h: 12, failed24h: 0, successRate: 1, p95DurationMs: 820 }, reviews: { open: 1 }, households: { total: 1 }, integrations: { llmGatewayConfigured: true, llmProtocol: "Cloud gateway" }, recentEvents: [] };
  if (path.startsWith("/api/v1/admin/")) return { items: [], nextCursor: null };
  return [];
}

async function intercept(page, authenticated = true) {
  await page.route("**/api/v1/**", route => {
    const path = new URL(route.request().url()).pathname;
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

async function run() {
  await mkdir(output, { recursive: true });
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
        }
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
        await page.locator(".document-card").first().click();
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
      const login = await browser.newPage({ viewport: { width: 390, height: 844 } });
      await intercept(login, false);
      await login.goto(baseURL, { waitUntil: "networkidle" });
      await login.getByRole("button", { name: "Masuk ke Richmod" }).waitFor();
      await login.screenshot({ path: new URL("mobile-login.png", output).pathname, fullPage: true });
      assert.equal(await login.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, "mobile login has horizontal overflow");
      await login.close();
    } finally {
      await browser.close();
    }
  } finally {
    server?.kill("SIGTERM");
  }
}

await run();
