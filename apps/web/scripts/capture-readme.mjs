import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const baseURL = process.env.RICHMOD_SCREENSHOT_URL || "http://127.0.0.1:3000";
const outputDirectory = fileURLToPath(new URL("../../../docs/assets/", import.meta.url));
const user = { userId: "demo-user", email: "demo@richmod.local", displayName: "Keluarga Richmod", isSuperAdmin: false, household: { id: "demo-household", role: "OWNER" } };
const categories = [
  { id: "food", name: "Makan di Luar", amount: "2475000", active: true },
  { id: "groceries", name: "Belanja Bulanan", amount: "1850000", active: true },
  { id: "transport", name: "Transportasi", amount: "960000", active: true },
  { id: "utilities", name: "Tagihan", amount: "725000", active: true },
  { id: "health", name: "Kesehatan", amount: "430000", active: true },
];
const transactions = [
  transaction("tx-1", "EXPENSE", "Kopi Tuku", "food", "Makan di Luar", "68000", "2026-09-06T09:14:00+07:00"),
  transaction("tx-2", "EXPENSE", "Super Indo", "groceries", "Belanja Bulanan", "438500", "2026-09-05T18:42:00+07:00"),
  transaction("tx-3", "EXPENSE", "Grab", "transport", "Transportasi", "47500", "2026-09-05T08:10:00+07:00"),
  transaction("tx-4", "INCOME", "Gaji Bulanan", "salary", "Gaji", "18500000", "2026-09-01T08:00:00+07:00"),
  transaction("tx-5", "EXPENSE", "PLN", "utilities", "Tagihan", "612400", "2026-09-02T20:05:00+07:00"),
];
const daily = [420000, 275000, 0, 510000, 335000, 554500].map((expense, index) => ({ period: `2026-09-${String(index + 1).padStart(2, "0")}`, income: index === 0 ? "18500000" : "0", expense: String(expense), netCashflow: String((index === 0 ? 18500000 : 0) - expense) }));
const reviews = [
  { id: "review-1", reason: "AMBIGUOUS_CATEGORY", amount: "186500", merchantName: "INDOMARET POINT", transactionAt: "2026-09-06T12:21:00+07:00", sourceType: "Bank email", type: "EXPENSE", proposalStatus: "NEEDS_REVIEW", categoryId: "groceries" },
  { id: "review-2", reason: "UNKNOWN_MERCHANT", amount: "92500", description: "Merchant perlu dikonfirmasi", transactionAt: "2026-09-05T19:36:00+07:00", sourceType: "Telegram", type: "EXPENSE", proposalStatus: "NEEDS_REVIEW", missingFields: ["merchant"] },
];
const actions = [{ id: "action-1", integrationType: "EMAIL_FORWARDING", actionType: "VERIFY_FORWARDING", status: "OPEN", title: "Verifikasi penerusan email", description: "Google memerlukan konfirmasi sebelum notifikasi finansial dapat diteruskan.", createdAt: "2026-09-06T03:00:00Z" }];
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
  ["/api/v1/analytics/overview", { periodKind: "CURRENT_CYCLE", income: "18500000", expense: "2094500", netCashflow: "16405500", savingsAllocated: "6500000", unallocatedSurplus: "9905500", reviewCount: 2 }],
  ["/api/v1/analytics/cycle", { kind: "CURRENT_CYCLE", start: "1 Sep 2026", end: "30 Sep 2026" }],
  ["/api/v1/analytics/cycle/daily", { configured: true, daily, salary: "18500000", spent: "2094500", remaining: "16405500", daysElapsed: 6, daysTotal: 30, cycleStart: "2026-09-01", cycleEnd: "2026-09-30" }],
  ["/api/v1/analytics/categories", categories], ["/api/v1/analytics/cashflow", daily],
  ["/api/v1/analytics/spending", daily.map(item => ({ ...item, refund: "0", netSpending: item.expense }))],
  ["/api/v1/analytics/merchants", [{ name: "Super Indo", amount: "438500" }, { name: "Kopi Tuku", amount: "332000" }, { name: "Grab", amount: "274500" }]],
  ["/api/v1/analytics/members", [{ name: "Dimas", amount: "1320000" }, { name: "Maya", amount: "774500" }]],
  ["/api/v1/transactions", transactions], ["/api/v1/reviews", reviews], ["/api/v1/integration-actions", actions], ["/api/v1/categories", categories],
  ["/api/v1/wealth/accounts", wealthAccounts], ["/api/v1/wealth/summary", { latest: wealthLatest, previous: wealthPrevious, netWorthChangeIdr: "4850000", confirmedCashflowIdr: "3410000", valuationAndOtherChangeIdr: "1440000" }], ["/api/v1/wealth/snapshots/latest", wealthLatest], ["/api/v1/wealth/history", [wealthLatest, wealthPrevious]], ["/api/v1/wealth/current-cycle-savings", { periodKind: "CURRENT_CYCLE", periodStart: "2026-09-01", periodEnd: "2026-09-30", savingsAllocated: "6500000", savingsByDestination: [{ wealthAccountId: "wealth-rdn", name: "Dana investasi", amountIdr: "6500000" }] }], ["/api/v1/wealth/cycle-recaps", []],
  ["/api/v1/insights", [{ id: "insight-1", status: "SUCCEEDED", text: "Pengeluaran enam hari pertama masih terkendali terhadap pemasukan siklus ini. Makan di luar menjadi kategori terbesar; tetapkan batas mingguan agar ruang untuk kebutuhan rutin tetap terjaga.", dataCompleteness: 0.94, completedAt: "2026-09-06T04:15:00Z", metrics: { period_kind: "CURRENT_CYCLE", period_start: "2026-09-01" } }]],
]);

await mkdir(outputDirectory, { recursive: true });
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1050 }, deviceScaleFactor: 1 });
let authenticated = true;
await page.route("**/api/v1/**", async route => {
  const url = new URL(route.request().url());
  if (url.pathname === "/api/v1/auth/me" && !authenticated) {
    await route.fulfill({ status: 401, contentType: "application/json", body: JSON.stringify({ error: "unauthorized" }) });
    return;
  }
  const key = [...responses.keys()].find(candidate => url.pathname === candidate);
  await route.fulfill({ status: key ? 200 : 404, contentType: "application/json", body: JSON.stringify(key ? responses.get(key) : { error: "fixture not found" }) });
});
for (const [path, file, selector, isAuthenticated] of [
  ["/", "landing.png", ".landing-hero", false],
  ["/login", "login.png", ".login-card", false],
  ["/", "dashboard.png", ".app-frame", true],
  ["/wealth", "wealth.png", ".app-frame", true],
  ["/inbox?view=transactions", "review-inbox.png", ".app-frame", true],
  ["/analytics", "analytics.png", ".app-frame", true],
]) {
  authenticated = isAuthenticated;
  await page.goto(`${baseURL}${path}`, { waitUntil: "networkidle" });
  await page.locator(selector).waitFor();
  await page.addStyleTag({ content: "*,*::before,*::after{animation:none!important;transition:none!important}html{scroll-behavior:auto!important}" });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: `${outputDirectory}/${file}`, fullPage: false });
}
await browser.close();

function transaction(id, type, merchantName, categoryId, categoryName, amount, transactionAt) {
  return { id, type, merchantName, categoryId, categoryName, amount, transactionAt, accountName: "Rekening Utama", memberName: "Keluarga", sourceType: "Bank email", status: "CONFIRMED" };
}
