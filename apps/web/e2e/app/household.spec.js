import { test, expect, reply } from "../support/fixtures.js";
import { owner } from "../support/data.js";
import { layoutApi } from "../support/layoutApi.js";

const TABS = ["Anggota", "Akun & kekayaan", "Kategori & aturan", "Sumber otomatis", "Notifikasi"];

test.beforeEach(({ api }) => layoutApi(api));

test.describe("tabs", () => {
  test("Keluarga has five deep-linkable tabs with a roving keyboard", async ({ page }) => {
    await page.goto("/household", { waitUntil: "networkidle" });
    const tabs = page.getByRole("tablist", { name: "Bagian keluarga" }).getByRole("tab");
    expect(await tabs.allTextContents()).toEqual(TABS);
    await expect(tabs.nth(0)).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("tabpanel")).toHaveAttribute("aria-labelledby", "settings-tab-anggota");
    await tabs.nth(0).focus();
    await page.keyboard.press("ArrowRight");
    await expect(tabs.nth(1)).toBeFocused();
    await expect(tabs.nth(1)).toHaveAttribute("aria-selected", "true");
    expect(new URL(page.url()).searchParams.get("tab")).toBe("akun");
    await page.keyboard.press("End");
    await expect(tabs.nth(4)).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("Home");
    await expect(tabs.nth(0)).toHaveAttribute("aria-selected", "true");
    await page.goto("/household?tab=kategori", { waitUntil: "networkidle" });
    await expect(page.getByRole("tab", { name: "Kategori & aturan" })).toHaveAttribute("aria-selected", "true");
  });

  test("an old anchor opens the tab that holds it, and /settings is the same hub", { tag: "@desktop-only" }, async ({ page }) => {
    await page.goto("/household#akun-kekayaan", { waitUntil: "networkidle" });
    await expect(page.getByRole("tab", { name: "Akun & kekayaan" })).toHaveAttribute("aria-selected", "true");
    await page.goto("/settings#status-pemrosesan", { waitUntil: "networkidle" });
    await expect(page.getByRole("tab", { name: "Sumber otomatis" })).toHaveAttribute("aria-selected", "true");
  });

  test.describe("for a member", () => {
    test.use({ user: owner({ household: { role: "MEMBER" } }) });

    test("only Anggota and Notifikasi exist and a deep link cannot go past them", { tag: "@desktop-only" }, async ({ page }) => {
      await page.goto("/household?tab=sumber", { waitUntil: "networkidle" });
      const tabs = page.getByRole("tablist", { name: "Bagian keluarga" }).getByRole("tab");
      expect(await tabs.allTextContents()).toEqual(["Anggota", "Notifikasi"]);
      await expect(page.getByRole("tab", { name: "Anggota" })).toHaveAttribute("aria-selected", "true");
      await expect(page.locator("#tambah-anggota"), "a member cannot add members").toHaveCount(0);
    });
  });

  test("Sumber otomatis carries a pending-setup indicator for the owner", { tag: "@desktop-only" }, async ({ page, api }) => {
    api.on("/api/v1/integration-actions", () => [{ id: "action-1", status: "PENDING" }, { id: "action-2", status: "PENDING" }]);
    await page.goto("/household", { waitUntil: "networkidle" });
    await expect(page.getByRole("tab", { name: /Sumber otomatis/ }).getByLabel("2 tindakan menunggu")).toBeVisible();
  });
});

test.describe("members", () => {
  test("a member row names its Telegram and account state, and the add form marks its required fields", { tag: "@desktop-only" }, async ({ page, api }) => {
    api.on("/api/v1/household/members", () => [
      { id: "member-1", displayName: "Rafi", email: "rafi@example.test", role: "OWNER", active: true, telegramConnected: true, passwordInitialized: true },
      { id: "member-2", displayName: "Dina", email: "dina@example.test", role: "MEMBER", active: true, telegramConnected: false, passwordInitialized: false, googleLinked: true },
      { id: "member-3", displayName: "Budi", email: "budi@example.test", role: "MEMBER", active: true, telegramConnected: false, passwordInitialized: false },
    ], { method: "GET" });
    await page.goto("/household", { waitUntil: "networkidle" });
    const row = name => page.locator("#main-content article").filter({ hasText: name });
    await expect(row("Rafi")).toContainText("Administrator · Telegram terhubung · Aktif");
    await expect(row("Dina")).toContainText("Terhubung dengan Google");
    await expect(row("Budi")).toContainText("Belum mengaktifkan akun");
    await expect(page.locator("#tambah-anggota .form-legend")).toContainText("wajib diisi");
    await expect(page.getByLabel("Nama anggota")).toBeVisible();
    await expect(page.getByLabel("Email anggota")).toHaveAttribute("type", "email");
  });

  test("adding a member confirms with a toast and returns focus to the form", { tag: "@desktop-only" }, async ({ page, api }) => {
    let created = false;
    api.on("/api/v1/household/members", request => {
      if (request.method === "POST") { created = true; return { id: "member-9" }; }
      return created ? [{ id: "member-1", displayName: "Rafi", role: "OWNER", active: true }, { id: "member-9", displayName: "Sari", email: "sari@example.test", role: "MEMBER", active: true }] : undefined;
    });
    await page.goto("/household", { waitUntil: "networkidle" });
    await page.getByLabel("Nama anggota").fill("Sari");
    await page.getByLabel("Email anggota").fill("sari@example.test");
    await page.getByRole("button", { name: /^Tambah/ }).click();
    await expect(page.getByRole("status").filter({ hasText: "Anggota Sari ditambahkan." })).toBeVisible();
    await expect(page.getByLabel("Nama anggota"), "focus returns to the first field").toBeFocused();
    expect(api.calls("/api/v1/household/members", "POST")[0].body).toEqual({ displayName: "Sari", email: "sari@example.test" });
  });

  test("deactivating asks first, names the consequence, then confirms and returns focus to the row", { tag: "@desktop-only" }, async ({ page, api }) => {
    let active = true;
    api.on("/api/v1/household/members", () => [{ id: "member-1", displayName: "Rafi", role: "OWNER", active: true }, { id: "member-2", displayName: "Dina", email: "dina@example.test", role: "MEMBER", active }], { method: "GET" });
    api.on("/api/v1/household/members/member-2", () => { active = false; return {}; }, { method: "PATCH" });
    await page.goto("/household", { waitUntil: "networkidle" });
    await page.getByRole("button", { name: "Nonaktifkan Dina" }).click();
    const dialog = page.locator("dialog[open]");
    await expect(dialog).toContainText("Riwayat transaksi tetap disimpan");
    await dialog.getByRole("button", { name: "Batal" }).click();
    expect(api.calls("/api/v1/household/members/member-2", "PATCH"), "cancelling the dialog changes nothing").toHaveLength(0);
    await page.getByRole("button", { name: "Nonaktifkan Dina" }).click();
    await page.locator("dialog[open]").getByRole("button", { name: "Nonaktifkan", exact: true }).click();
    await expect(page.getByRole("status").filter({ hasText: "Anggota Dina dinonaktifkan." })).toBeVisible();
    await expect(page.locator("#member-member-2")).toBeFocused();
  });

  test("a login invite shows a copyable link with its expiry, and can be cancelled", { tag: "@desktop-only" }, async ({ page, api, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    const soon = new Date("2026-09-08T03:00:00Z").toISOString();
    let pending = false;
    api.on("/api/v1/household/members", () => [{ id: "member-1", displayName: "Rafi", role: "OWNER", active: true, passwordInitialized: true }, { id: "member-2", displayName: "Dina", email: "dina@example.test", role: "MEMBER", active: true, passwordInitialized: false, ...(pending ? { dashboardInviteStatus: "PENDING", dashboardInviteExpiresAt: soon } : {}) }], { method: "GET" });
    api.on("/api/v1/household/members/member-2/dashboard-invite", request => {
      if (request.method === "POST") { pending = true; return { link: "/invite#token-abc", expiresAt: soon, purpose: "ACTIVATE" }; }
      pending = false;
      return {};
    });
    await page.goto("/household", { waitUntil: "networkidle" });
    await page.getByRole("button", { name: "Undang masuk ke Richmod Dina" }).click();
    await expect(page.getByRole("heading", { name: /Berlaku sampai .* · sekali pakai/ })).toBeVisible();
    await page.getByRole("button", { name: "Salin tautan" }).click();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toMatch(/\/invite#token-abc$/);
    await page.getByRole("button", { name: "Batalkan undangan masuk Dina" }).click();
    await expect(page.getByRole("status").filter({ hasText: "Undangan Dina dibatalkan." })).toBeVisible();
    await expect(page.getByRole("heading", { name: /Berlaku sampai/ })).toHaveCount(0);
  });

  test("a failed member list says so instead of showing zero accounts", { tag: "@desktop-only" }, async ({ page, api }) => {
    api.on("/api/v1/household/members", () => reply({}, { status: 500 }), { method: "GET" });
    await page.goto("/household", { waitUntil: "networkidle" });
    await expect(page.getByText("Daftar anggota belum termuat.")).toBeVisible();
    await expect(page.locator("#main-content")).not.toContainText("0 akun");
  });
});

test.describe("settings lists", () => {
  test("a long list filters with a result count and an empty state; rows show labels, not enum values", { tag: "@desktop-only" }, async ({ page }) => {
    await page.goto("/household?tab=kategori", { waitUntil: "networkidle" });
    const search = page.getByRole("searchbox").first();
    await expect(search).toBeVisible();
    await search.fill("Keluarga 12");
    await expect(page.getByRole("status").filter({ hasText: /\d/ }).first()).toBeVisible();
    await search.fill("tidak ada yang begini");
    await expect(page.getByText("Tidak ada yang cocok dengan pencarian.")).toBeVisible();
    await page.goto("/household?tab=akun", { waitUntil: "networkidle" });
    await expect(page.locator("#main-content")).not.toContainText(/SPENDING_ONLY|FULL_LEDGER|MUTUAL_FUND|BANK_EMAIL|INVESTMENT\b/);
  });

  test("deactivating an account asks first and names the consequence; a failure is announced in its row", { tag: "@desktop-only" }, async ({ page, api }) => {
    api.on(/^\/api\/v1\/accounts\/acc-1$/, () => reply({ error: "boom" }, { status: 500 }), { method: "PATCH" });
    await page.goto("/household?tab=akun", { waitUntil: "networkidle" });
    await page.getByRole("button", { name: /^Nonaktifkan Rekening Bank Jago/ }).click();
    const dialog = page.locator("dialog[open]");
    await expect(dialog).toContainText("Nonaktifkan");
    await dialog.getByRole("button", { name: "Nonaktifkan", exact: true }).click();
    const alert = page.locator("#main-content article").filter({ hasText: "Rekening Bank Jago" }).getByRole("alert");
    await expect(alert, "the failure appears in the row that failed").toContainText("boom");
  });

  test("every repeated row action carries the item in its accessible name", { tag: "@desktop-only" }, async ({ page }) => {
    await page.goto("/household?tab=akun", { waitUntil: "networkidle" });
    const unnamed = await page.locator(".settings-list article button").evaluateAll(buttons => buttons.filter(button => !button.getAttribute("aria-label")).map(button => button.textContent.trim()));
    expect(unnamed, "row buttons without an aria-label").toEqual([]);
  });
});

test.describe("more member and settings details", () => {
  test("a member with a password offers a reset link, one without offers an invite", { tag: "@desktop-only" }, async ({ page, api }) => {
    api.on("/api/v1/household/members", () => [
      { id: "member-1", displayName: "Rafi", role: "OWNER", active: true, passwordInitialized: true },
      { id: "member-2", displayName: "Dina", email: "dina@example.test", role: "MEMBER", active: true, passwordInitialized: true },
      { id: "member-3", displayName: "Budi", email: "budi@example.test", role: "MEMBER", active: true, passwordInitialized: false },
    ], { method: "GET" });
    await page.goto("/household", { waitUntil: "networkidle" });
    await expect(page.getByRole("button", { name: "Atur ulang kata sandi Dina" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Undang masuk ke Richmod Budi" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Hubungkan Telegram Rafi" }), "an owner connects their own Telegram").toBeVisible();
    await expect(page.getByRole("button", { name: "Undang Telegram Dina" })).toBeVisible();
  });

  test("a toast stays clear of the phone tab bar", async ({ page, api }) => {
    test.skip((page.viewportSize()?.width ?? 1000) > 680, "the phone tab bar only exists on a phone");
    api.on("/api/v1/household/members", request => (request.method === "POST" ? { id: "member-9" } : undefined));
    await page.goto("/household", { waitUntil: "networkidle" });
    await page.getByLabel("Nama anggota").fill("Sari");
    await page.getByLabel("Email anggota").fill("sari@example.test");
    await page.getByRole("button", { name: /^Tambah/ }).click();
    const toast = page.locator(".toast");
    await toast.waitFor();
    await page.evaluate(() => document.activeElement?.blur());
    const toastBox = await toast.boundingBox();
    const navBox = await page.locator(".mobile-nav").boundingBox();
    expect(toastBox.y + toastBox.height, "the toast sits above the tab bar").toBeLessThanOrEqual(navBox.y);
  });

  test("a pending financial source offers Ubah, Uji and Aktifkan with the provider in each name", { tag: "@desktop-only" }, async ({ page, api }) => {
    api.on("/api/v1/financial-email-sources", () => [{ id: "fs-1", providerName: "Bibit", status: "PENDING", senderAddress: "no-reply@bibit.id", cashMovement: "NONE", active: true }]);
    await page.goto("/household?tab=sumber", { waitUntil: "networkidle" });
    await expect(page.getByRole("button", { name: "Uji Bibit" })).toHaveText("Uji");
    await expect(page.getByRole("button", { name: "Ubah Bibit" })).toHaveText("Ubah");
    await expect(page.getByRole("button", { name: "Aktifkan Bibit" })).toBeVisible();
    await expect(page.locator("#main-content")).not.toContainText(/>Test</);
  });
});

test.describe("Sumber otomatis", () => {
  const ingress = (steps = { address: "done", sender: "done", forwarding: "done", activated: "current" }) => ({ address: "h_abc@richmod.link", status: "PROVISIONED", forwardingAction: null, steps });

  test("rotating the address asks first and warns that forwarding rules must be redone", { tag: "@desktop-only" }, async ({ page, api }) => {
    api.on("/api/v1/integrations/email-ingress", () => ingress());
    api.on("/api/v1/integrations/email-ingress/rotate", () => ({}), { method: "POST" });
    await page.goto("/household?tab=sumber", { waitUntil: "networkidle" });
    await page.getByRole("button", { name: "Rotasi", exact: true }).click();
    const dialog = page.locator("dialog[open]");
    await expect(dialog).toContainText("Aturan penerusan");
    await expect(dialog).toContainText("dibuat ulang");
    await dialog.getByRole("button", { name: "Batal" }).click();
    expect(api.calls("/api/v1/integrations/email-ingress/rotate", "POST"), "cancelling rotates nothing").toHaveLength(0);
    await page.getByRole("button", { name: "Rotasi", exact: true }).click();
    await page.locator("dialog[open]").getByRole("button", { name: "Rotasi alamat" }).click();
    await expect.poll(() => api.calls("/api/v1/integrations/email-ingress/rotate", "POST").length).toBe(1);
    await expect(page.getByRole("status").filter({ hasText: "Aturan penerusan perlu dibuat ulang." })).toBeVisible();
  });

  test("an activation the server refuses says what to do next", { tag: "@desktop-only" }, async ({ page, api }) => {
    api.on("/api/v1/integrations/email-ingress", () => ingress());
    api.on("/api/v1/integrations/email-ingress/activate", () => reply({ code: "SENDER_REQUIRED" }, { status: 409 }), { method: "POST" });
    await page.goto("/household?tab=sumber", { waitUntil: "networkidle" });
    await page.getByRole("button", { name: "Aktifkan", exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: "Tambahkan minimal satu pengirim bank atau provider yang aktif sebelum mengaktifkan email." })).toBeVisible();
  });

  test("the owner sees the delivery log and pending actions; a member does not", { tag: "@desktop-only" }, async ({ page, api }) => {
    api.on("/api/v1/integrations/email-ingress", () => ingress());
    await page.goto("/household?tab=sumber", { waitUntil: "networkidle" });
    await expect(page.getByRole("heading", { name: "Email masuk terakhir" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Tindakan" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Status pemrosesan otomatis" }).or(page.getByText("Status pemrosesan otomatis"))).toBeVisible();
  });

  test("the old ?view=email inbox link lands on the delivery log", { tag: "@desktop-only" }, async ({ page }) => {
    await page.goto("/inbox?view=email", { waitUntil: "networkidle" });
    await expect.poll(() => new URL(page.url()).pathname + new URL(page.url()).search + new URL(page.url()).hash).toBe("/household?tab=sumber#email-masuk-terakhir");
  });
});
