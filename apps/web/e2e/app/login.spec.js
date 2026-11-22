import { test, expect, reply } from "../support/fixtures.js";

test.describe("signed out", () => {
  test.beforeEach(({ api }) => {
    api.on("/api/v1/auth/me", () => reply({}, { status: 401 }));
  });

  test("login offers Google beside the password form, with keyboard hints on the fields", async ({ page }) => {
    await page.goto("/login", { waitUntil: "networkidle" });
    await expect(page.getByRole("heading", { name: "Masuk ke Richmod." })).toBeVisible();
    await expect(page.getByRole("link", { name: /Masuk dengan Google/ })).toHaveAttribute("href", "/api/v1/auth/google/start");
    await expect(page.getByText("atau dengan email")).toBeVisible();
    await expect(page.locator("#login-email")).toHaveAttribute("enterkeyhint", "next");
    await expect(page.locator("#login-password")).toHaveAttribute("enterkeyhint", "go");
    await expect(page.getByRole("button", { name: "Masuk ke Richmod" })).toBeVisible();
  });

  test("a Google error code from the redirect is shown in Indonesian, and an unknown code gets a generic line", async ({ page }) => {
    await page.goto("/login?error=google_email_mismatch", { waitUntil: "networkidle" });
    await expect(page.getByText("Email Google tidak cocok dengan email pada undangan.")).toBeVisible();
    await page.goto("/login?error=something_new", { waitUntil: "networkidle" });
    await expect(page.getByText("Tidak dapat masuk saat ini. Coba lagi.")).toBeVisible();
  });

  test("a wrong password shows one calm message and stays on the page", async ({ page, api }) => {
    api.on("/api/v1/auth/login", () => reply({ error: "invalid" }, { status: 401 }), { method: "POST" });
    await page.goto("/login", { waitUntil: "networkidle" });
    await page.locator("#login-email").fill("rafi@example.test");
    await page.locator("#login-password").fill("salah");
    await page.getByRole("button", { name: "Masuk ke Richmod" }).click();
    await expect(page.getByText("Email atau kata sandi tidak cocok.")).toBeVisible();
    expect(new URL(page.url()).pathname).toBe("/login");
  });

  test("the decorative login picture is hidden from assistive technology", async ({ page }) => {
    await page.goto("/login", { waitUntil: "networkidle" });
    const images = await page.locator(".login-context img").evaluateAll(nodes => nodes.map(node => ({ alt: node.getAttribute("alt"), hidden: node.getAttribute("aria-hidden") })));
    expect(images.length).toBeGreaterThan(0);
    for (const image of images) expect(image).toEqual({ alt: "", hidden: "true" });
  });

  test.describe("invite", () => {
    test("Google activation posts the hash token, clears it, then leaves for the Google start URL", async ({ page, api }) => {
      api.on("/api/v1/auth/google/invite", () => ({}), { method: "POST" });
      await page.route("**/api/v1/auth/google/start", route => route.fulfill({ status: 200, contentType: "text/html", body: "<html><body>google</body></html>" }));
      await page.goto("/invite#token-abc", { waitUntil: "networkidle" });
      await expect(page.getByText("atau buat kata sandi", { exact: true })).toBeVisible();
      await page.getByRole("button", { name: /Aktifkan dengan Google/ }).click();
      await page.waitForURL(/\/api\/v1\/auth\/google\/start$/);
      expect(api.calls("/api/v1/auth/google/invite", "POST")[0].body).toEqual({ token: "token-abc" });
    });

    test("a missing or bad token and mismatched passwords are refused with one message", async ({ page, api }) => {
      await page.goto("/invite", { waitUntil: "networkidle" });
      await page.getByRole("button", { name: /Aktifkan dengan Google/ }).click();
      await expect(page.getByText("Tautan atau kata sandi tidak valid.")).toBeVisible();
      expect(api.calls("/api/v1/auth/google/invite")).toHaveLength(0);
      await page.goto("/invite#token-abc", { waitUntil: "networkidle" });
      const fields = page.locator('input[type="password"]');
      await expect(fields.first()).toHaveAttribute("minlength", "12");
      await fields.nth(0).fill("kata-sandi-panjang-1");
      await fields.nth(1).fill("kata-sandi-lain-123");
      await page.locator("form").getByRole("button").last().click();
      await expect(page.getByText("Tautan atau kata sandi tidak valid.")).toBeVisible();
      expect(api.calls("/api/v1/auth/dashboard-invites/accept")).toHaveLength(0);
    });

    test("a network failure is reported, then the message clears when the field is edited", async ({ page, api }) => {
      await page.route("**/api/v1/auth/dashboard-invites/accept", route => route.abort());
      await page.goto("/invite#token-abc", { waitUntil: "networkidle" });
      const fields = page.locator('input[type="password"]');
      await fields.nth(0).fill("kata-sandi-panjang-1");
      await fields.nth(1).fill("kata-sandi-panjang-1");
      await page.locator("form").getByRole("button").last().click();
      await expect(page.getByText(/Koneksi terputus/)).toBeVisible();
      await fields.nth(0).fill("kata-sandi-panjang-2");
      await expect(page.getByText(/Koneksi terputus/)).toHaveCount(0);
      expect(api.calls("/api/v1/auth/dashboard-invites/accept")).toHaveLength(0);
    });
  });
});
