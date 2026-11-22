import { test, expect, reply } from "../support/fixtures.js";
import { owner, superAdmin } from "../support/data.js";

const isPhone = page => page.viewportSize().width <= 600;
const waiting = n => Array.from({ length: n }, (_, index) => ({ id: `review-${index}`, reason: "AMBIGUOUS_CATEGORY", amount: "10000", merchantName: "Warung", transactionAt: "2026-09-05T15:00:00+07:00", sourceType: "BANK_EMAIL", proposalStatus: "NEEDS_REVIEW", missingFacts: ["category"] }));

test("the navigation is four places, and on a phone four tabs around one add button without an overflow menu", async ({ page }) => {
  await page.goto("/", { waitUntil: "networkidle" });
  await page.locator("#main-content").waitFor();
  if (isPhone(page)) {
    const bar = page.getByRole("navigation", { name: "Navigasi utama seluler" });
    await expect(bar).toBeVisible();
    const items = await bar.locator("a, button").evaluateAll(nodes => nodes.map(node => node.getAttribute("aria-label") || node.textContent.trim()));
    expect(items, "Beranda · Transaksi · + · Laporan · Keluarga").toEqual(["Beranda", "Transaksi", "Catat transaksi", "Laporan", "Keluarga"]);
    await expect(page.getByText("Lainnya", { exact: true }), "there is no overflow menu").toHaveCount(0);
    expect(await bar.locator("svg").evaluateAll(icons => icons.every(icon => icon.getAttribute("aria-hidden") === "true")), "icons are decorative").toBe(true);
    const add = await bar.getByRole("button", { name: "Catat transaksi" }).boundingBox();
    expect(add.width, "the add button is an elevated 52px target").toBeGreaterThanOrEqual(52);
  } else {
    const side = page.getByRole("navigation", { name: "Navigasi utama", exact: true });
    expect(await side.getByRole("link").allTextContents()).toEqual(["Beranda", "Transaksi", "Laporan", "Keluarga"]);
    await expect(page.getByRole("navigation", { name: "Navigasi utama seluler" }), "the phone bar is hidden on a wide screen").toBeHidden();
  }
});

test("Perlu dicek is conditional: the link and the phone pill appear only while decisions wait", async ({ page, api }) => {
  await page.goto("/", { waitUntil: "networkidle" });
  await page.locator("#main-content").waitFor();
  await expect(page.getByRole("link", { name: /Perlu dicek/ }), "no decisions waiting, no link").toHaveCount(0);
  await expect(page.getByRole("link", { name: /perlu dicek ›/ })).toHaveCount(0);

  api.on("/api/v1/reviews", () => reply(waiting(3), { headers: { "X-Decision-Count": "3" } }));
  await page.reload({ waitUntil: "networkidle" });
  await page.locator("#main-content").waitFor();
  if (isPhone(page)) {
    await expect(page.getByRole("link", { name: "3 perlu dicek ›" })).toBeVisible();
  } else {
    const link = page.getByRole("link", { name: "Perlu dicek, 3 item menunggu keputusan" });
    await expect(link, "the control carries the accessible name, the badge is decorative").toBeVisible();
    await expect(link.locator(".nav-badge")).toHaveAttribute("aria-hidden", "true");
  }
});

test("Kekayaan highlights Laporan and Pengaturan highlights Keluarga", async ({ page }) => {
  const current = async () => page.locator("nav[aria-label^='Navigasi utama'] a[aria-current='page']:visible").first().textContent();
  await page.goto("/wealth", { waitUntil: "networkidle" });
  await page.locator("#main-content").waitFor();
  expect(await current()).toBe("Laporan");
  await page.goto("/settings", { waitUntil: "networkidle" });
  await page.locator("#main-content").waitFor();
  expect(await current()).toBe("Keluarga");
});

test("the inbox count is fetched once for the shell, not once per navigation", async ({ page, api }) => {
  await page.goto("/", { waitUntil: "networkidle" });
  await page.locator("#main-content").waitFor();
  const before = api.calls("/api/v1/reviews").length;
  const link = isPhone(page) ? page.getByRole("navigation", { name: "Navigasi utama seluler" }) : page.getByRole("navigation", { name: "Navigasi utama", exact: true });
  for (const name of ["Transaksi", "Laporan", "Keluarga", "Beranda"]) {
    await link.getByRole("link", { name }).click();
    await page.locator("#main-content").waitFor();
  }
  expect(api.calls("/api/v1/reviews").length - before, "in-app navigation never re-fetches the review count").toBeLessThanOrEqual(1);
});

test("a screen view is reported as a bounded screen key, never a path or id", async ({ page, api }) => {
  await page.goto("/transactions?q=secret", { waitUntil: "networkidle" });
  await page.locator("#main-content").waitFor();
  await expect.poll(() => api.telemetry.length).toBeGreaterThan(0);
  expect(api.telemetry[0]).toEqual({ screen: "transactions", action: "SCREEN_VIEW" });
});

test("the page extends under the notch", async ({ page }) => {
  await page.goto("/", { waitUntil: "networkidle" });
  await expect(page.locator('meta[name="viewport"]')).toHaveAttribute("content", /viewport-fit=cover/);
});

test("a platform admin sees the Admin link in the sidebar and a member does not", async ({ page, api }) => {
  test.skip(isPhone(page), "the sidebar is a wide-screen control");
  api.user = superAdmin();
  await page.goto("/", { waitUntil: "networkidle" });
  await expect(page.locator(".sidebar a.admin-link[href='/admin']")).toBeVisible();
  api.user = owner();
  await page.reload({ waitUntil: "networkidle" });
  await page.locator("#main-content").waitFor();
  await expect(page.locator(".sidebar a.admin-link")).toHaveCount(0);
});

test.describe("the session", () => {
  test("a session that cannot be checked says so and retries, instead of signing the user out", async ({ page, api }) => {
    let healthy = false;
    api.on("/api/v1/auth/me", () => (healthy ? undefined : reply({}, { status: 503 })));
    await page.goto("/", { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("alert")).toContainText("Sesi belum dapat diperiksa");
    await expect(page.getByRole("heading", { name: "Keuangan keluarga, tanpa menebak." }), "an outage is not a logout").toHaveCount(0);
    healthy = true;
    await page.getByRole("button", { name: "Coba lagi" }).click();
    await page.locator("#main-content").waitFor();
  });

  test("only a 401 shows the landing page", async ({ page, api }) => {
    api.on("/api/v1/auth/me", () => reply({}, { status: 401 }));
    await page.goto("/", { waitUntil: "networkidle" });
    await expect(page.getByRole("heading", { name: "Keuangan keluarga, tanpa menebak." })).toBeVisible();
  });

  test("an account without a household sees the waiting screen, can check again and sign out", async ({ page, api }) => {
    api.user = superAdmin();
    api.user = { ...api.user, household: null };
    let activated = false;
    api.on("/api/v1/auth/me", () => (activated ? { ...superAdmin(), email: "rafi@example.test" } : undefined));
    api.on("/api/v1/auth/logout", () => ({ ok: true }), { method: "POST" });
    await page.goto("/", { waitUntil: "networkidle" });
    await expect(page.getByText("RUANG KELUARGA BELUM AKTIF")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Akun kamu sudah terdaftar." })).toBeVisible();
    await expect(page.getByText("Rafi", { exact: true })).toBeVisible();
    await expect(page.getByText("rafi@example.test")).toBeVisible();
    await expect(page.getByRole("link", { name: "Buka Konsol Admin" }), "a platform admin keeps a way into the console").toBeVisible();
    await expect(page.getByRole("button", { name: "Keluar" })).toBeVisible();
    activated = true;
    await page.getByRole("button", { name: "Periksa status" }).click();
    await page.locator("#main-content").waitFor();
  });
});

test("an unknown address shows the Indonesian not-found page with a way back to Beranda", async ({ page }) => {
  await page.goto("/tidak-ada-halaman", { waitUntil: "networkidle" });
  await expect(page.getByText("Halaman tidak ditemukan").first()).toBeVisible();
  await expect(page.getByRole("link", { name: /Kembali ke Beranda/ })).toHaveAttribute("href", "/");
  await expect(page).toHaveTitle("Halaman tidak ditemukan · Richmod");
});

test("a household-less account makes no household-scoped requests", async ({ page, api }) => {
  api.user = { ...superAdmin(), household: null };
  await page.goto("/", { waitUntil: "networkidle" });
  await expect(page.getByText("RUANG KELUARGA BELUM AKTIF")).toBeVisible();
  expect(api.requests.filter(request => /\/(reviews|analytics|transactions|categories)/.test(request.path)), "no household data is fetched before a household exists").toEqual([]);
});
