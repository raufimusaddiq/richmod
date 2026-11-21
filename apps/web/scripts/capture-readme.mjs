import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { cycleFacts, cycleCommentary } from "../tests/fixtures/cycle-review.mjs";

const baseURL = process.env.RICHMOD_SCREENSHOT_URL || "http://127.0.0.1:3000";
const outputDirectory = fileURLToPath(new URL("../../../docs/assets/", import.meta.url));
const user = { userId: "demo-user", email: "demo@richmod.local", displayName: "Keluarga Richmod", isSuperAdmin: false, household: { id: "demo-household", role: "OWNER" } };
const categories = [
  { id: "dining", name: "Makan di Luar", amount: "1250000", active: true },
  { id: "groceries", name: "Belanja Bahan Makanan", amount: "1180000", active: true },
  { id: "housing", name: "Tempat Tinggal", amount: "1000000", active: true },
  { id: "transport", name: "Transportasi", amount: "460000", active: true },
  { id: "coffee", name: "Kopi & Camilan", amount: "289500", active: true },
  { id: "bills", name: "Tagihan", amount: "500000", active: true },
  { id: "salary", name: "Gaji", amount: "0", active: true },
];
const accounts = [
  { id: "account-jago", name: "Bank Jago", accountType: "BANK", trackingPolicy: "TRACKED", active: true },
  { id: "account-bca", name: "BCA", accountType: "BANK", trackingPolicy: "TRACKED", active: true },
  { id: "account-cash", name: "Tunai", accountType: "CASH", trackingPolicy: "TRACKED", active: true },
];
const transactions = [
  transaction("tx-1", "EXPENSE", "Kopi Tuku", "coffee", "Kopi & Camilan", "68000", "2026-09-14T09:14:00+07:00", "Bank Jago", "BANK_EMAIL"),
  transaction("tx-2", "EXPENSE", "Grab", "transport", "Transportasi", "47500", "2026-09-14T08:10:00+07:00", "Bank Jago", "BANK_EMAIL"),
  transaction("tx-3", "EXPENSE", "Super Indo", "groceries", "Belanja Bahan Makanan", "380000", "2026-09-13T18:42:00+07:00", "BCA", "TELEGRAM_IMAGE"),
  transaction("tx-4", "EXPENSE", "Warung Bu Tini", "dining", "Makan di Luar", "85000", "2026-09-13T12:30:00+07:00", "Tunai", "WEB_MANUAL"),
  transaction("tx-5", "EXPENSE", "PLN Token", "bills", "Tagihan", "205000", "2026-09-12T20:05:00+07:00", "BCA", "BANK_EMAIL"),
  transaction("tx-6", "EXPENSE", "Sewa Kontrakan", "housing", "Tempat Tinggal", "1000000", "2026-09-11T08:30:00+07:00", "Bank Jago", "WEB_MANUAL"),
  transaction("tx-7", "INCOME", "Gaji Bulanan", "salary", "Gaji", "18500000", "2026-09-11T08:00:00+07:00", "BCA", "BANK_EMAIL"),
];
const dailyExpenses = [420000, 275000, 0, 510000, 335000, 554500, 180000, 265000, 720000, 95000, 310000, 430000, 205000, 380000];
const daily = dailyExpenses.map((expense, index) => ({ period: `2026-09-${String(index + 1).padStart(2, "0")}`, income: index === 0 ? "18500000" : "0", grossExpense: String(expense), refund: "0", expense: String(expense), netCashflow: String((index === 0 ? 18500000 : 0) - expense) }));
const suggestion = (id, rank, reason) => ({ ...categories.find(category => category.id === id), rank, reason, count: reason === "MERCHANT_HISTORY" ? 6 - rank : 3 });
const reviews = [
  { id: "review-1", reviewItemId: "review-item-1", reason: "AMBIGUOUS_CATEGORY", type: "EXPENSE", presentation: "DECISION", proposalStatus: "NEEDS_REVIEW", missingFacts: ["category"], allowedActions: ["CONFIRM_REVIEW", "IGNORE"], sourceType: "BANK_EMAIL", merchantName: "Kopi Tuku", amount: "68000", transactionAt: "2026-09-14T09:14:00+07:00", suggestedCategories: [suggestion("coffee", 1, "MERCHANT_HISTORY"), suggestion("dining", 2, "HOUSEHOLD_RECENT"), suggestion("groceries", 3, "HOUSEHOLD_RECENT")] },
  { id: "review-2", reviewItemId: "review-item-2", reason: "AMBIGUOUS_CATEGORY", type: "EXPENSE", presentation: "DECISION", proposalStatus: "NEEDS_REVIEW", missingFacts: ["category"], allowedActions: ["CONFIRM_REVIEW", "IGNORE"], sourceType: "TELEGRAM_IMAGE", merchantName: "Super Indo", amount: "92500", transactionAt: "2026-09-13T19:36:00+07:00", suggestedCategories: [suggestion("groceries", 1, "MERCHANT_HISTORY"), suggestion("dining", 2, "HOUSEHOLD_RECENT"), suggestion("coffee", 3, "HOUSEHOLD_RECENT")] },
];
const quickSuggestions = {
  frequent: [
    { description: "Kopi Tuku", amount: "68000", type: "EXPENSE", categoryId: "coffee", accountId: "account-jago" },
    { description: "Grab", amount: "47500", type: "EXPENSE", categoryId: "transport", accountId: "account-jago" },
  ],
  recentCategories: ["coffee", "transport", "groceries"],
  descriptionMap: { kopi: "coffee", grab: "transport", "super indo": "groceries" },
};
const wealthAccounts = [
  { id: "wealth-bank", name: "Tabungan keluarga", institution: "Bank Jago", wealthType: "BANK", usageRole: "SAVINGS", active: true },
  { id: "wealth-rdn", name: "Dana investasi", institution: "Bibit", wealthType: "MUTUAL_FUND", usageRole: "INVESTMENT", active: true },
  { id: "wealth-loan", name: "Cicilan rumah", institution: "Bank", wealthType: "LOAN", usageRole: "OTHER", active: true },
];
const wealthItems = [
  { wealthAccountId: "wealth-bank", name: "Tabungan keluarga", side: "ASSET", valueIdr: "18500000" },
  { wealthAccountId: "wealth-rdn", name: "Dana investasi", side: "ASSET", valueIdr: "34000000" },
  { wealthAccountId: "wealth-loan", name: "Cicilan rumah", side: "LIABILITY", valueIdr: "4250000" },
];
const wealthLatest = { id: "snapshot-2", observedAt: "2026-09-06T10:00:00+07:00", netWorthIdr: "48250000", assetTotalIdr: "52500000", liabilityTotalIdr: "4250000", items: wealthItems };
const wealthPrevious = { id: "snapshot-1", observedAt: "2026-08-06T10:00:00+07:00", netWorthIdr: "43400000" };
const responses = new Map([
  ["/api/v1/auth/me", user],
  ["/api/v1/analytics/overview", { periodKind: "CURRENT_CYCLE", confirmedCount: 38, income: "18500000", expense: "4679500", netCashflow: "13820500", savingsAllocated: "6500000", unallocatedSurplus: "7320500", previousExpense: "4210000", previousDays: 14, expenseDelta: "469500", reviewCount: 2 }],
  ["/api/v1/analytics/cycle-review", cycleFacts()],
  ["/api/v1/analytics/cycle-decisions", { items: [], previous: [], previousCycleStart: "2026-08-26" }],
  ["/api/v1/analytics/cycle", { kind: "CURRENT_CYCLE", start: "2026-09-01", end: "2026-09-30" }],
  ["/api/v1/analytics/cycle/daily", { configured: true, daily, salary: "18500000", spent: "4679500", remaining: "13820500", daysElapsed: 14, daysTotal: 30, cycleStart: "2026-09-01", cycleEnd: "2026-09-30" }],
  ["/api/v1/analytics/categories", categories], ["/api/v1/analytics/cashflow", daily],
  ["/api/v1/analytics/merchants", [{ name: "Sewa Kontrakan", amount: "1000000" }, { name: "Super Indo", amount: "380000" }, { name: "Kopi Tuku", amount: "289500" }]],
  ["/api/v1/analytics/members", [{ name: "Dimas", amount: "1320000" }, { name: "Maya", amount: "774500" }]],
  ["/api/v1/transactions", transactions], ["/api/v1/reviews", reviews], ["/api/v1/household/members", []], ["/api/v1/integration-actions", []], ["/api/v1/accounts", accounts], ["/api/v1/transactions/quick-suggestions", quickSuggestions], ["/api/v1/wealth/observations", []], ["/api/v1/reviews/resolved", []], ["/api/v1/documents", []], ["/api/v1/categories", categories],
  ["/api/v1/wealth/accounts", wealthAccounts], ["/api/v1/wealth/summary", { latest: wealthLatest, previous: wealthPrevious, netWorthChangeIdr: "4850000", confirmedCashflowIdr: "3410000", valuationAndOtherChangeIdr: "1440000" }], ["/api/v1/wealth/snapshots/latest", wealthLatest], ["/api/v1/wealth/history", [wealthLatest, wealthPrevious]], ["/api/v1/wealth/current-cycle-savings", { periodKind: "CURRENT_CYCLE", periodStart: "2026-09-01", periodEnd: "2026-09-30", savingsAllocated: "6500000", savingsByDestination: [{ wealthAccountId: "wealth-rdn", name: "Dana investasi", amountIdr: "6500000" }] }], ["/api/v1/wealth/cycle-recaps", []],
  ["/api/v1/insights", [cycleCommentary]],
]);

const responseHeaders = new Map([
  ["/api/v1/transactions", { "X-Total-Count": String(transactions.length), "X-Voided-Count": "0" }],
  ["/api/v1/reviews", { "X-Decision-Count": String(reviews.length) }],
]);
const freezeStyle = "*,*::before,*::after{animation:none!important;transition:none!important}html{scroll-behavior:auto!important}";
const desktop = { width: 1440, height: 1050 };
const shots = [
  { path: "/", file: "landing.png", viewport: desktop, authenticated: false, ready: page => page.locator(".landing-hero").waitFor() },
  { path: "/login", file: "login.png", viewport: desktop, authenticated: false, ready: page => page.locator(".login-card").waitFor() },
  { path: "/", file: "dashboard.png", viewport: desktop, ready: page => page.locator(".home-hero-amount").first().waitFor() },
  { path: "/inbox?view=transactions", file: "review-inbox.png", viewport: desktop, ready: page => page.getByText("Masuk kategori apa?").first().waitFor() },
  { path: "/analytics", file: "analytics.png", viewport: desktop, ready: page => page.getByText("Ke mana uangnya").first().waitFor() },
  { path: "/wealth", file: "wealth.png", viewport: desktop, ready: page => page.getByText("Kekayaan bersih").first().waitFor() },
  { path: "/", file: "dashboard-mobile.png", viewport: { width: 390, height: 844 }, ready: page => page.locator(".home-hero-amount").first().waitFor() },
  { path: "/transactions", file: "transactions-tablet.png", viewport: { width: 768, height: 1024 }, ready: page => page.locator(".ledger-day").first().waitFor() },
];

await mkdir(outputDirectory, { recursive: true });
const browser = await chromium.launch({ headless: true });
for (const shot of shots) {
  const authenticated = shot.authenticated !== false;
  const page = await browser.newPage({ viewport: shot.viewport, deviceScaleFactor: 1 });
  const failures = [];
  page.on("response", response => { if (response.status() >= 400 && !(response.status() === 401 && !authenticated)) failures.push(`${response.status()} ${response.url()}`); });
  await page.route("**/api/v1/**", route => fixtureRoute(route, authenticated));
  await page.goto(`${baseURL}${shot.path}`, { waitUntil: "networkidle" });
  await shot.ready(page);
  await page.addStyleTag({ content: freezeStyle });
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(300);
  if (failures.length) throw new Error(`${shot.file}: ${failures.join(", ")}`);
  await page.screenshot({ path: `${outputDirectory}/${shot.file}`, fullPage: false });
  await page.close();
}
await browser.close();

async function fixtureRoute(route, authenticated) {
  const url = new URL(route.request().url());
  if (url.pathname === "/api/v1/auth/me" && !authenticated) {
    await route.fulfill({ status: 401, contentType: "application/json", body: JSON.stringify({ error: "unauthorized" }) });
    return;
  }
  if (url.pathname === "/api/v1/telemetry/events") {
    await route.fulfill({ status: 204 });
    return;
  }
  const found = responses.has(url.pathname);
  await route.fulfill({ status: found ? 200 : 404, contentType: "application/json", headers: responseHeaders.get(url.pathname) || {}, body: JSON.stringify(found ? responses.get(url.pathname) : { error: "fixture not found" }) });
}

function transaction(id, type, merchantName, categoryId, categoryName, amount, transactionAt, accountName, sourceType) {
  return { id, type, merchantName, categoryId, categoryName, amount, transactionAt, accountName, memberName: "Keluarga", sourceType, status: "CONFIRMED" };
}
