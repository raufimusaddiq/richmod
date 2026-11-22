import { test, expect } from "../support/fixtures.js";
import { visualApi, visualUser } from "../support/visualApi.js";

const routes = ["/", "/transactions", "/analytics", "/wealth", "/inbox", "/documents", "/household", "/settings", "/admin"];
const viewports = [
  ["desktop", 1440, 900],
  ["tablet", 1024, 768],
  ["tablet-small", 700, 900],
  ["tablet-portrait", 768, 1024],
  ["mobile", 390, 844],
  ["mobile-360", 360, 800],
  ["mobile-320", 320, 667],
  ["wide", 2560, 1440],
];

const overflows = (page, slack = 1) => page.evaluate(extra => document.documentElement.scrollWidth > document.documentElement.clientWidth + extra, slack);

function trackConsole(page, ignore = []) {
  const errors = [];
  page.on("console", message => {
    if (message.type() !== "error") return;
    const text = message.text();
    if (text.includes("favicon") || ignore.some(part => text.includes(part))) return;
    errors.push(text);
  });
  return errors;
}

async function unreachableScrollers(page) {
  return page.evaluate(() => [...document.querySelectorAll("main *, aside *, dialog *")].filter(element => {
    const style = getComputedStyle(element);
    if (element.clientWidth === 0 || element.clientHeight === 0) return false;
    const scrollsX = (style.overflowX === "auto" || style.overflowX === "scroll") && element.scrollWidth > element.clientWidth + 1;
    const scrollsY = (style.overflowY === "auto" || style.overflowY === "scroll") && element.scrollHeight > element.clientHeight + 1;
    if (!scrollsX && !scrollsY) return false;
    const focusable = element.tabIndex >= 0 || Boolean(element.querySelector('a[href], button, input, select, textarea, [tabindex="0"]'));
    const named = element.tabIndex < 0 || Boolean(element.getAttribute("aria-label") || element.getAttribute("aria-labelledby"));
    return !(focusable && named);
  }).map(element => `${element.tagName.toLowerCase()}.${String(element.className).split(" ")[0]}`));
}

test.use({ user: visualUser() });

test.beforeEach(({ api }) => visualApi(api));

for (const [name, width, height] of viewports) {
  test.describe(`${name} ${width}x${height}`, () => {
    test.use({ viewport: { width, height } });

    test("every page fits, keeps amounts and headers inside cards, and renders no text below 12px", async ({ page }) => {
      const errors = trackConsole(page);
      for (const path of routes) {
        await page.goto(path, { waitUntil: "networkidle" });
        await page.locator("#main-content").waitFor();
        expect(await overflows(page, 0), `${path} has horizontal overflow`).toBe(false);
        const clippedAmounts = await page.evaluate(() => [...document.querySelectorAll(".transaction-amount")].filter(element => {
          const card = element.closest(".surface");
          return card && element.getBoundingClientRect().right > card.getBoundingClientRect().right + 1;
        }).length);
        expect(clippedAmounts, `${path} clips transaction amounts inside their card`).toBe(0);
        const clippedHeads = await page.evaluate(() => [...document.querySelectorAll(".table-head")].filter(element => {
          const card = element.closest(".surface");
          return element.scrollWidth > element.clientWidth + 1 || (element.lastElementChild && card && element.lastElementChild.getBoundingClientRect().right > card.getBoundingClientRect().right + 1);
        }).length);
        expect(clippedHeads, `${path} clips the transaction table header inside its card`).toBe(0);
        const smallText = await page.evaluate(() => [...document.querySelectorAll("body *")]
          .filter(element => element.children.length === 0 && (element.textContent || "").trim().length > 1)
          .map(element => ({ text: element.textContent.trim().slice(0, 30), size: Number.parseFloat(getComputedStyle(element).fontSize) }))
          .filter(entry => entry.size < 12));
        expect(smallText, `${path} renders text below 12px`).toEqual([]);

        if (path === "/transactions") {
          const row = page.locator(".transaction-row").first();
          await row.hover();
          expect(await row.evaluate(element => getComputedStyle(element).backgroundColor), "transaction row uses primary hover background").not.toBe("rgb(40, 91, 70)");
          await row.focus();
          expect(await row.evaluate(element => getComputedStyle(element).outlineStyle), "transaction row has a keyboard focus ring").not.toBe("none");
        }
        if (path === "/admin") {
          expect(await page.locator(".admin-metrics").evaluate(element => getComputedStyle(element).backgroundColor), "admin metrics expose an empty separator cell").not.toBe("rgb(221, 220, 213)");
          const lane = page.locator(".admin-lane").first();
          await expect(page.locator(".admin-lane").first()).toBeVisible();
          expect(await lane.evaluate(element => getComputedStyle(element).display)).toBe("grid");
          await page.getByRole("tab", { name: "Tugas", exact: true }).click();
          await page.locator("button.admin-link").first().waitFor();
          const link = await page.locator("button.admin-link").first().evaluate(element => {
            const style = getComputedStyle(element);
            return { display: style.display, background: style.backgroundColor, minHeight: style.minHeight };
          });
          expect(link.display, "admin ID inherited button flex layout").not.toBe("flex");
          expect(link.background, "admin ID has button background").toBe("rgba(0, 0, 0, 0)");
          expect(link.minHeight, "admin ID has button minimum height").toBe("0px");
          await page.locator("button.admin-link").first().click();
          await page.locator("aside[role='dialog']").waitFor();
          await expect(page.locator("aside[role='dialog']"), "admin job drawer is a modal dialog").toHaveCount(1);
          await page.keyboard.press("Escape");
          await page.waitForFunction(() => !document.querySelector("aside[role='dialog']"));
          await expect(page.locator("aside[role='dialog']"), "admin drawer closes on Escape").toHaveCount(0);
        }
      }
      expect(errors, "browser console errors").toEqual([]);
    });

    test("calendar, tooltip, transaction drawer and dialog, and document drawer behave", async ({ page }) => {
      const errors = trackConsole(page);
      await page.goto("/analytics", { waitUntil: "networkidle" });
      await page.getByRole("button", { name: "Kalender" }).click();
      await page.getByRole("heading", { name: "Pemasukan vs pengeluaran" }).waitFor();
      expect(await overflows(page), "analytics calendar has horizontal overflow").toBe(false);
      await page.getByRole("button", { name: "Siklus Gaji" }).click();
      await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
      await page.evaluate(() => { document.querySelector("#daily > details").open = true; });
      await page.locator(".cycle-spending-chart .recharts-bar-rectangle").first().hover();
      await page.locator(".chart-tooltip").waitFor();

      await page.goto("/transactions", { waitUntil: "networkidle" });
      await page.locator(".transaction-row").first().click();
      await page.getByRole("button", { name: "Tutup detail" }).waitFor();
      await expect(page.locator("aside[role='dialog'][aria-label='Detail transaksi']"), "transaction drawer is a modal dialog").toHaveCount(1);
      await page.getByRole("button", { name: "Tutup detail" }).click();
      await page.getByRole("button", { name: "Tambah transaksi", exact: true }).click();
      await page.locator("dialog[open]").waitFor();
      await page.locator("dialog[open]").getByRole("button", { name: "Tutup", exact: true }).click();

      await page.goto("/documents", { waitUntil: "networkidle" });
      const card = page.locator(".document-card").first();
      await card.hover();
      const hover = await card.evaluate(element => {
        const style = getComputedStyle(element);
        return { background: style.backgroundColor, color: style.color };
      });
      expect(hover.background, "document card uses primary hover background").not.toBe("rgb(86, 52, 72)");
      expect(hover.color, "document card text stays the brand ink on hover").toBe("rgb(37, 58, 54)");
      await card.click();
      await page.getByRole("dialog", { name: "Detail dokumen" }).waitFor();
      if (name === "mobile") {
        await page.getByRole("button", { name: "Tutup detail" }).click();
        await page.getByRole("button", { name: "Catat transaksi" }).click();
        await page.locator("dialog[open]").waitFor();
      }
      expect(errors, "browser console errors").toEqual([]);
    });
  });
}

test.describe("phone cycle review", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("the lead KPI is visible without scrolling, controls are 44px and the page never overflows", async ({ page }) => {
    await page.goto("/analytics", { waitUntil: "networkidle" });
    await page.locator("#main-content").waitFor();
    const lead = await page.locator(".kpi-cell[data-lead]").boundingBox();
    expect(lead && lead.y >= 0 && lead.y + lead.height <= 844, "the lead KPI is fully visible without scrolling at 390x844").toBe(true);
    const targets = await page.evaluate(() => [...document.querySelectorAll(".cycle-picker button, .cycle-picker select, .range-control-group button")].map(element => Math.round(element.getBoundingClientRect().height)));
    expect(targets.filter(height => height < 44), "phone controls are at least 44px tall").toEqual([]);
  });

  for (const [label, scenario] of [["first cycle", { firstCycle: true }], ["no wealth", { noWealth: true }], ["no spending", { noSpending: true }], ["full cycle", { fullCycle: true }]]) {
    test(`${label}: no overflow and the lead KPI is visible`, async ({ page, api }) => {
      visualApi(api, { scenario });
      await page.goto("/analytics", { waitUntil: "networkidle" });
      await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
      expect(await overflows(page)).toBe(false);
      const lead = await page.locator(".kpi-cell[data-lead]").boundingBox();
      expect(lead && lead.y >= 0 && lead.y + lead.height <= 844, "the lead KPI is fully visible without scrolling").toBe(true);
      if (label === "first cycle") await expect(page.locator(".kpi-cell[data-lead]"), "a missing comparison is named, never invented").toContainText("Belum ada pembanding");
    });
  }

  test("every keyboard stop on the dashboard is visible, named and shows a focus ring", async ({ page }) => {
    await page.goto("/analytics", { waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Pengeluaran harian" }).waitFor();
    const stops = [];
    for (let step = 0; step < 40; step += 1) {
      await page.keyboard.press("Tab");
      const stop = await page.evaluate(() => {
        const element = document.activeElement;
        if (!element || element === document.body) return null;
        const rect = element.getBoundingClientRect();
        const tag = element.tagName.toLowerCase();
        const labelledBy = element.getAttribute("aria-labelledby");
        const name = (element.getAttribute("aria-label") || (labelledBy ? labelledBy.split(/\s+/).map(id => document.getElementById(id)?.textContent || "").join(" ") : "") || (tag === "svg" ? "" : element.textContent) || "").trim();
        const signature = () => {
          const style = getComputedStyle(element);
          return [style.outlineStyle, style.outlineWidth, style.boxShadow, style.backgroundColor, style.borderColor].join("|");
        };
        const focused = signature();
        element.blur();
        const unfocused = signature();
        element.focus();
        return { name: name.slice(0, 40), tag, role: element.getAttribute("role") || "", visible: rect.width > 0 && rect.height > 0, ring: focused !== unfocused };
      });
      if (stop) stops.push(stop);
    }
    expect(stops.length, "the dashboard is reachable by keyboard").toBeGreaterThan(5);
    expect(stops.filter(stop => !stop.visible), "no keyboard stop is invisible").toEqual([]);
    expect(stops.filter(stop => !stop.ring), "every keyboard stop shows a focus ring").toEqual([]);
    expect(stops.filter(stop => !stop.name), "every keyboard stop has an accessible name").toEqual([]);
    expect(stops.filter(stop => stop.role === "application"), "no chart is a focusable role=application stop").toEqual([]);
    expect(stops.some(stop => stop.name.includes("Lihat nilai laju pengeluaran")), "the pace chart's data table is reachable by keyboard").toBe(true);
    expect(stops.some(stop => stop.name.startsWith("Pengeluaran harian")), "the daily chart's disclosure is reachable by keyboard").toBe(true);
  });

  test("scrolling regions on the dashboard and the document drawer are keyboard reachable and named", async ({ page }) => {
    await page.route("**/api/v1/documents/*/extraction", route => route.fulfill({ json: [{ stage: "RECEIPT", schemaVersion: 1, validated: true, output: { merchant: "Pasar Minggu", note: "x".repeat(400) } }] }));
    const unreachable = {};
    for (const path of ["/analytics", "/documents"]) {
      await page.goto(path, { waitUntil: "networkidle" });
      await page.locator("#main-content").waitFor();
      if (path === "/documents") {
        await page.locator(".document-card").first().click();
        await page.locator(".detail-drawer").waitFor();
        await page.locator(".detail-drawer pre").first().waitFor({ state: "attached" });
      }
      await page.evaluate(() => document.querySelectorAll("details").forEach(details => { details.open = true; }));
      if (path === "/documents") await page.locator(".detail-drawer pre").first().waitFor();
      unreachable[path] = await unreachableScrollers(page);
    }
    expect(Object.fromEntries(Object.entries(unreachable).filter(([, regions]) => regions.length)), "scrolling regions without keyboard access or a name").toEqual({});
  });
});

test.describe("wide shell", () => {
  test.use({ viewport: { width: 1920, height: 1080 } });

  test("the authenticated shell reaches the viewport edge", async ({ page }) => {
    await page.goto("/", { waitUntil: "networkidle" });
    await page.locator("#main-content").waitFor();
    expect(await page.evaluate(() => Math.abs(document.querySelector(".app-main").getBoundingClientRect().right - window.innerWidth) <= 1)).toBe(true);
  });
});

test.describe("signed out", () => {
  test.beforeEach(({ api }) => visualApi(api, { authenticated: false }));

  test("the login page fits a phone and submits to the existing endpoint", async ({ page, api }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/login", { waitUntil: "networkidle" });
    await page.getByRole("button", { name: "Masuk ke Richmod" }).waitFor();
    expect(await overflows(page), "mobile login has horizontal overflow").toBe(false);
    await page.locator("#login-email").fill("rafi@example.test");
    await page.locator("#login-password").fill("password-example");
    await page.getByRole("button", { name: "Masuk ke Richmod" }).click();
    await expect.poll(() => api.calls("/api/v1/auth/login", "POST").length, "login submits to the existing endpoint").toBeGreaterThan(0);
  });

  for (const [label, path, width, height, heading] of [["landing desktop", "/", 1440, 900, "Keuangan keluarga, tanpa menebak."], ["landing tablet", "/", 1024, 768, "Keuangan keluarga, tanpa menebak."], ["landing mobile", "/", 390, 844, "Keuangan keluarga, tanpa menebak."], ["login desktop", "/login", 1440, 900, "Masuk ke Richmod."], ["login mobile", "/login", 390, 844, "Masuk ke Richmod."]]) {
    test(`${label} renders its heading without overflow or console errors`, async ({ page }) => {
      const errors = trackConsole(page, ["401"]);
      await page.setViewportSize({ width, height });
      await page.goto(path, { waitUntil: "networkidle" });
      await page.getByRole("heading", { name: heading }).waitFor();
      expect(await overflows(page)).toBe(false);
      if (label === "landing desktop") {
        await page.getByRole("link", { name: "Privasi" }).first().click();
        await page.getByRole("heading", { name: "Kebijakan Privasi", level: 1 }).waitFor();
      }
      expect(errors, "browser console errors").toEqual([]);
    });
  }

  test("the privacy and terms pages fit a tablet", async ({ page }) => {
    await page.setViewportSize({ width: 1024, height: 768 });
    for (const [path, heading] of [["/privacy", "Kebijakan Privasi"], ["/terms", "Ketentuan Layanan"]]) {
      await page.goto(path, { waitUntil: "networkidle" });
      await page.getByRole("heading", { name: heading, level: 1 }).waitFor();
      expect(await overflows(page), `${path} has horizontal overflow`).toBe(false);
    }
  });
});

test("a signed-in visitor on /login is sent to Beranda", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/login", { waitUntil: "networkidle" });
  await page.waitForURL(url => url.pathname === "/");
  await page.locator("#main-content").waitFor();
});
