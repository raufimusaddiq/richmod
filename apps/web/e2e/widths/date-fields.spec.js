import { test, expect } from "../support/fixtures.js";

async function openFilters(page) {
  const toggle = page.locator(".filter-toggle");
  await toggle.waitFor({ state: "visible" });
  if ((await toggle.getAttribute("aria-expanded")) !== "true") await toggle.click();
  await page.locator("#transaction-filters-more").waitFor({ state: "visible" });
}

async function measureFit(input) {
  await input.waitFor({ state: "visible" });
  await input.evaluate(async () => {
    await document.fonts?.ready;
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  });
  const box = await input.evaluate(element => {
    const rect = element.getBoundingClientRect();
    const parent = element.parentElement.getBoundingClientRect();
    return { left: rect.left, right: rect.right, width: rect.width, height: rect.height, parentLeft: parent.left, parentRight: parent.right, overflow: element.scrollWidth > element.clientWidth + 1, fontSize: Number.parseFloat(getComputedStyle(element).fontSize) };
  });
  expect(box.left, "input starts inside its container").toBeGreaterThanOrEqual(box.parentLeft - 1);
  expect(box.right, "input ends inside its container").toBeLessThanOrEqual(box.parentRight + 1);
  expect(box.overflow, "date text does not overflow").toBe(false);
  expect(box.height, "date input stays touch accessible").toBeGreaterThanOrEqual(44);
  expect(box.width, "date input stays touch accessible").toBeGreaterThanOrEqual(44);
  await input.focus();
  expect(await input.evaluate(element => getComputedStyle(element).outlineStyle), "date input has visible keyboard focus").not.toBe("none");
  return box;
}

const overflowsPage = page => page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1);

for (const width of [320, 390, 600, 1440]) {
  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height: 844 } });

    test("date filters and the manual entry time field fit and stay usable", async ({ page, api }) => {
      await page.goto("/transactions", { waitUntil: "networkidle" });
      await openFilters(page);
      for (const [label, value] of [["Dari tanggal", "2026-09-25"], ["Sampai tanggal", "2026-10-01"]]) {
        const input = page.getByLabel(label, { exact: true });
        await measureFit(input);
        await input.fill(value);
        const box = await measureFit(input);
        if (width <= 680) expect(box.fontSize, "mobile date text prevents focus zoom").toBeGreaterThanOrEqual(16);
      }
      await page.getByRole("button", { name: "Terapkan filter", exact: true }).click();
      expect(new URL(page.url()).searchParams.get("from")).toBe("2026-09-25");
      expect(new URL(page.url()).searchParams.get("to")).toBe("2026-10-01");
      expect(await overflowsPage(page), "ledger fits viewport").toBe(false);

      await page.getByRole("button", { name: "Tambah transaksi", exact: true }).click();
      const dialog = page.locator("dialog[open]");
      await dialog.getByText("Waktu, akun, dan catatan", { exact: true }).click();
      const time = dialog.getByLabel("Waktu", { exact: true });
      await measureFit(time);
      await time.fill("2026-09-25T12:30");
      await measureFit(time);
      await expect(time).toHaveValue("2026-09-25T12:30");
      expect(await dialog.evaluate(element => element.scrollWidth > element.clientWidth + 1), "transaction dialog fits viewport").toBe(false);
      await page.keyboard.press("Escape");
      await expect(dialog, "native dialog closes with Escape").toHaveCount(0);
      expect(api.calls("/api/v1/transactions", "POST")).toHaveLength(0);
    });
  });
}
