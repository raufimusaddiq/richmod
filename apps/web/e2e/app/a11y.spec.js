import { test, expect, reply } from "../support/fixtures.js";
import { transaction } from "../support/data.js";
import { layoutApi } from "../support/layoutApi.js";

test("a loading screen announces itself with a name and busy state, and its note is decorative", { tag: "@desktop-only" }, async ({ page, api }) => {
  layoutApi(api);
  api.on("/api/v1/transactions", () => reply([transaction("tx-1")], { delay: 600 }));
  await page.goto("/transactions", { waitUntil: "domcontentloaded" });
  const loading = page.getByRole("status", { name: "Memuat data" });
  await expect(loading).toHaveAttribute("aria-live", "polite");
  await expect(loading).toHaveAttribute("aria-busy", "true");
  await expect(loading.locator(".skeleton-note")).toHaveAttribute("aria-hidden", "true");
  await expect(page.locator(".transaction-row").first()).toBeVisible();
  await expect(loading).toHaveCount(0);
});

test("Perlu dicek has two tabs with the full tab keyboard and panel wiring", { tag: "@desktop-only" }, async ({ page, api }) => {
  layoutApi(api);
  await page.goto("/inbox", { waitUntil: "networkidle" });
  const tabs = page.getByRole("tablist").getByRole("tab");
  await expect(tabs).toHaveCount(2);
  await expect(tabs.first()).toHaveAttribute("aria-controls", "inbox-panel");
  await expect(page.getByRole("tabpanel")).toHaveAttribute("id", "inbox-panel");
  await tabs.first().focus();
  await page.keyboard.press("ArrowRight");
  await expect(tabs.nth(1)).toBeFocused();
  await expect(tabs.nth(1)).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Home");
  await expect(tabs.first()).toHaveAttribute("aria-selected", "true");
  await expect(tabs.first()).toHaveAttribute("tabindex", "0");
  await expect(tabs.nth(1)).toHaveAttribute("tabindex", "-1");
});

test("the old /reviews and /actions links keep working", { tag: "@desktop-only" }, async ({ page, api }) => {
  layoutApi(api);
  await page.goto("/reviews", { waitUntil: "networkidle" });
  await expect.poll(() => new URL(page.url()).pathname + new URL(page.url()).search).toBe("/inbox?view=transactions");
  await page.goto("/actions", { waitUntil: "networkidle" });
  await expect.poll(() => new URL(page.url()).pathname).toBe("/household");
  expect(new URL(page.url()).searchParams.get("tab")).toBe("sumber");
});

test("a failed review list offers a retry, and a failed optional list never hides the reviews", { tag: "@desktop-only" }, async ({ page, api }) => {
  layoutApi(api);
  let healthy = false;
  api.on("/api/v1/reviews", () => (healthy ? undefined : reply({}, { status: 500 })));
  await page.goto("/inbox", { waitUntil: "networkidle" });
  await expect(page.getByRole("alert").filter({ hasText: "Data belum dapat dimuat. Coba lagi." })).toBeVisible();
  healthy = true;
  await page.getByRole("button", { name: "Coba lagi" }).click();
  await expect(page.locator("#inbox-panel")).toContainText("Transfer ke rekening lain");
  api.on("/api/v1/categories", () => reply({ error: "x" }, { status: 500 }));
  await page.reload({ waitUntil: "networkidle" });
  await expect(page.locator("#inbox-panel"), "the reviews still render").toContainText("Transfer ke rekening lain");
});

test("toasts and card glyphs are decorative; a toast is a polite status with a named close button", { tag: "@desktop-only" }, async ({ page, api }) => {
  layoutApi(api);
  api.on("/api/v1/household/members", request => (request.method === "POST" ? { id: "member-9" } : undefined));
  await page.goto("/household", { waitUntil: "networkidle" });
  await page.getByLabel("Nama anggota").fill("Sari");
  await page.getByLabel("Email anggota").fill("sari@example.test");
  await page.getByRole("button", { name: /^Tambah/ }).click();
  const toast = page.locator(".toast");
  await expect(toast).toHaveAttribute("role", "status");
  await expect(toast).toHaveAttribute("aria-live", "polite");
  await expect(toast.locator("span").first()).toHaveAttribute("aria-hidden", "true");
  await expect(toast.getByRole("button", { name: "Tutup notifikasi" })).toBeVisible();
});

test("a transaction drawer is a modal that traps Tab, closes with Escape and gives focus back", async ({ page, api }) => {
  layoutApi(api);
  await page.goto("/transactions", { waitUntil: "networkidle" });
  const row = page.locator(".transaction-row").first();
  await row.click();
  const drawer = page.getByRole("dialog", { name: "Detail transaksi" });
  await drawer.waitFor();
  expect(await drawer.evaluate(element => element.contains(document.activeElement)), "focus moves into the drawer").toBe(true);
  for (let index = 0; index < 30; index += 1) await page.keyboard.press("Tab");
  expect(await drawer.evaluate(element => element.contains(document.activeElement)), "Tab stays inside the drawer").toBe(true);
  for (let index = 0; index < 30; index += 1) await page.keyboard.press("Shift+Tab");
  expect(await drawer.evaluate(element => element.contains(document.activeElement)), "Shift+Tab stays inside the drawer").toBe(true);
  await page.keyboard.press("Escape");
  await drawer.waitFor({ state: "detached" });
  await expect(row).toBeFocused();
});

test("every scrolling table wrapper is focusable, a named region", async ({ page, api }) => {
  layoutApi(api);
  await page.goto("/analytics", { waitUntil: "networkidle" });
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  await page.evaluate(() => document.querySelectorAll("details").forEach(details => { details.open = true; }));
  const wrappers = await page.locator(".review-table-wrap, .heat-wrap, .merchant-rank-scroll").evaluateAll(nodes => nodes.map(node => ({ tabindex: node.getAttribute("tabindex"), role: node.getAttribute("role"), label: node.getAttribute("aria-label") })));
  expect(wrappers.length).toBeGreaterThanOrEqual(3);
  for (const wrapper of wrappers) {
    expect(wrapper.tabindex).toBe("0");
    expect(wrapper.role).toBe("region");
    expect(wrapper.label).toMatch(/geser untuk semua kolom$/);
  }
});

test("the report charts are named images or links, never focusable applications", async ({ page, api }) => {
  layoutApi(api);
  await page.goto("/analytics", { waitUntil: "networkidle" });
  await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
  await page.evaluate(() => document.querySelectorAll("details").forEach(details => { details.open = true; }));
  expect(await page.locator("[role='application']").count()).toBe(0);
  const unnamed = await page.locator(".recharts-wrapper").evaluateAll(nodes => nodes.filter(node => !node.closest("[role='img'][aria-label], [aria-label]")).length);
  expect(unnamed, "every chart sits inside a named container").toBe(0);
});
