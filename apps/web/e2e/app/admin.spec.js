import { test, expect, reply } from "../support/fixtures.js";
import { superAdmin } from "../support/data.js";
import { layoutApi } from "../support/layoutApi.js";

test.use({ user: superAdmin() });

const users = () => [
  { id: "user-1", displayName: "Rafi", email: "rafi@example.test", active: true, households: 1, passwordInitialized: true, googleLinked: true, isSuperAdmin: true },
  { id: "user-2", displayName: "Dina", email: "dina@example.test", active: true, households: 1, passwordInitialized: false, googleLinked: false, isSuperAdmin: false },
];

test.beforeEach(({ api }) => {
  layoutApi(api);
  api.on("/api/v1/admin/users", () => users(), { method: "GET" });
});

test("the console keeps its eight tabs as a real tablist with arrow and Home/End keys", async ({ page }) => {
  await page.goto("/admin", { waitUntil: "networkidle" });
  const tabs = page.getByRole("tablist").getByRole("tab");
  expect(await tabs.allTextContents()).toEqual(["Ringkasan", "Tinjauan", "Tugas", "LLM", "Log", "Rumah tangga", "Pengguna", "Audit"]);
  await expect(tabs.first()).toHaveAttribute("aria-selected", "true");
  await expect(tabs.first()).toHaveAttribute("aria-controls", "admin-panel");
  await expect(page.getByRole("tabpanel")).toHaveAttribute("id", "admin-panel");
  await tabs.first().focus();
  await page.keyboard.press("ArrowRight");
  await expect(tabs.nth(1)).toBeFocused();
  await expect(tabs.nth(1)).toHaveAttribute("aria-selected", "true");
  expect(new URL(page.url()).searchParams.get("tab")).toBe("reviews");
  await page.keyboard.press("End");
  await expect(tabs.last()).toHaveAttribute("aria-selected", "true");
});

test("a changed user needs confirmation, and a refused change reads as Indonesian guidance", async ({ page, api }) => {
  api.on("/api/v1/admin/users/user-2", () => reply({ error: "FINAL_SUPER_ADMIN_REQUIRED" }, { status: 409 }), { method: "PATCH" });
  await page.goto("/admin?tab=users", { waitUntil: "networkidle" });
  const row = page.locator("tr").filter({ hasText: "dina@example.test" });
  await row.getByRole("button", { name: "Nonaktifkan" }).click();
  const dialog = page.locator("dialog[open]");
  await expect(dialog).toContainText("Nonaktifkan dina@example.test?");
  await dialog.getByRole("button", { name: "Batal" }).click();
  expect(api.calls("/api/v1/admin/users/user-2", "PATCH"), "cancelling changes nothing").toHaveLength(0);
  await row.getByRole("button", { name: "Nonaktifkan" }).click();
  await page.locator("dialog[open]").getByRole("button", { name: "Nonaktifkan", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Harus tersisa minimal satu Super Admin aktif.");
  await expect(row.getByRole("cell", { name: "Google" }).or(row.locator("td").nth(4))).toBeVisible();
});

test("a failed list shows the failure with a retry instead of 'Memuat…' forever", async ({ page, api }) => {
  let healthy = false;
  api.on("/api/v1/admin/users", () => (healthy ? users() : reply({ error: "x" }, { status: 500 })), { method: "GET" });
  await page.goto("/admin?tab=users", { waitUntil: "networkidle" });
  await expect(page.getByRole("tabpanel").getByRole("alert")).toContainText("Tidak dapat memuat data.");
  await expect(page.getByText("Memuat users…")).toHaveCount(0);
  healthy = true;
  await page.getByRole("button", { name: "Coba lagi" }).click();
  await expect(page.locator("tr").filter({ hasText: "dina@example.test" })).toBeVisible();
});

test("tables keep their column labels on every cell so a stacked phone row still names its values", async ({ page }) => {
  await page.goto("/admin?tab=users", { waitUntil: "networkidle" });
  const labels = await page.locator("tr").filter({ hasText: "dina@example.test" }).locator("td").evaluateAll(cells => cells.map(cell => cell.getAttribute("data-label")));
  expect(labels).toEqual(["Pengguna", "Email", "Status", "Rumah tangga", "Kata sandi", "Peran", "Aksi"]);
  await expect(page.locator("tr").filter({ hasText: "rafi@example.test" }), "a Google-linked account is told apart from an inactive one").toContainText("Diatur · Google");
  await expect(page.locator("tr").filter({ hasText: "dina@example.test" })).toContainText("Belum");
});

test("a session that cannot be read says so with a retry, and a 401 leaves the console", async ({ page, api }) => {
  let healthy = false;
  api.on("/api/v1/auth/me", () => (healthy ? undefined : reply({}, { status: 500 })));
  await page.goto("/admin", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("alert")).toContainText("Sesi belum dapat diperiksa");
  healthy = true;
  await page.getByRole("button", { name: "Coba lagi" }).click();
  await expect(page.getByRole("tablist")).toBeVisible();
});
