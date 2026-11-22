import { test, expect, reply } from "../support/fixtures.js";
import { ledgerBasics, transaction } from "../support/data.js";

const wealthAccounts = () => [
  { id: "asset-1", name: "Tabungan", institution: "Bank", wealthType: "BANK", usageRole: "SPENDING", active: true },
  { id: "liability-1", name: "Kartu kredit", institution: "Bank", wealthType: "CREDIT_CARD", usageRole: "SPENDING", active: true },
];
const snapshot = (id, value) => ({ id, netWorthIdr: value, assetTotalIdr: value, liabilityTotalIdr: "0", observedAt: "2026-11-03T10:00:00+07:00", items: [{ wealthAccountId: "asset-1", valueIdr: value }, { wealthAccountId: "liability-1", valueIdr: "0" }] });

function mockLedgerAndWealth(api) {
  let latest = snapshot("wealth-1", "1000000");
  let saves = 0;
  api.use(request => {
    const { path, query, method } = request;
    if (path === "/api/v1/transactions") {
      const second = query.get("cursor") === "cursor-2";
      return reply([second ? transaction("tx-3", "Grab") : transaction("tx-1", "Pasar Minggu")], { headers: second ? {} : { "X-Next-Cursor": "cursor-2" } });
    }
    if (path === "/api/v1/wealth/accounts") return wealthAccounts();
    if (path === "/api/v1/wealth/summary") return { latest };
    if (path === "/api/v1/wealth/snapshots/latest") return latest;
    if (path === "/api/v1/wealth/history") return [latest];
    if (path === "/api/v1/wealth/current-cycle-savings") return { periodKind: "CURRENT_CYCLE", savingsAllocated: "0", periodStart: "2026-11-01", periodEnd: "2026-11-30", savingsByDestination: [] };
    if (path === "/api/v1/wealth/snapshots" && method === "POST") {
      saves += 1;
      if (saves > 1) return reply({ error: "synthetic failure" }, { status: 500 });
      latest = snapshot("wealth-2", "1500000");
      return {};
    }
    return ledgerBasics(path);
  });
}

const animated = (page, selector, attribute, animation) => page.waitForFunction(([selector, attribute, animation]) => {
  const element = document.querySelector(selector);
  return Boolean(element) && element.hasAttribute(attribute) && getComputedStyle(element).animationName.includes(animation);
}, [selector, attribute, animation], { timeout: 3000 });

const settled = (page, selector, attribute) => page.waitForFunction(([selector, attribute]) => {
  const element = document.querySelector(selector);
  return Boolean(element) && !element.hasAttribute(attribute);
}, [selector, attribute], { timeout: 1500 }).then(() => true, () => false);

for (const [label, width] of [["desktop", 1440], ["mobile", 390]]) {
  test.describe(label, () => {
    test.use({ viewport: { width, height: 900 } });

    test.beforeEach(({ api }) => mockLedgerAndWealth(api));

    test("the ledger fades on a filter but not on pagination", async ({ page }) => {
      await page.goto("/transactions", { waitUntil: "networkidle" });
      await page.waitForSelector(".ledger-panel:not([data-refreshed])");
      const toggle = page.locator(".filter-toggle");
      if (await toggle.isVisible()) await toggle.click();
      await page.getByRole("button", { name: "Terapkan filter", exact: true }).click();
      await animated(page, ".ledger-panel", "data-refreshed", "list-in");
      await page.getByRole("button", { name: "Berikutnya", exact: true }).click();
      expect(await settled(page, ".ledger-panel", "data-refreshed"), "pagination replayed the filter fade").toBe(true);
    });

    test("a wealth save flashes once and a retry does not replay it", async ({ page }) => {
      await page.goto("/wealth", { waitUntil: "networkidle" });
      await page.waitForSelector(".wealth-position-primary > strong:not([data-updated])");
      await page.getByRole("button", { name: "Perbarui nilai", exact: true }).click();
      await page.locator('input[name="value-asset-1"]').fill("1500000");
      await page.getByRole("button", { name: "Simpan catatan lengkap", exact: true }).click();
      await animated(page, ".wealth-position-primary > strong", "data-updated", "value-updated");

      await page.getByRole("button", { name: "Perbarui nilai", exact: true }).click();
      await page.getByRole("button", { name: "Simpan catatan lengkap", exact: true }).click();
      await expect(page.locator(".notice.error")).toContainText("belum dapat disimpan");
      await page.getByRole("button", { name: "Simpan catatan lengkap", exact: true }).click();
      await expect(page.locator(".notice.error")).toContainText("belum dapat disimpan");
      expect(await settled(page, ".wealth-position-primary > strong", "data-updated"), "a retry replayed the save flash").toBe(true);
    });
  });
}
