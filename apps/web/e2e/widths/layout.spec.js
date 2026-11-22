import { test, expect } from "../support/fixtures.js";
import { superAdmin, owner } from "../support/data.js";
import { layoutApi } from "../support/layoutApi.js";

const routes = ["/", "/transactions", "/analytics", "/wealth", "/inbox", "/documents", "/household", "/admin"];
const KEYBOARD = 320;

test.use({ user: superAdmin() });

test.beforeEach(({ api }) => layoutApi(api));

async function expectNoHorizontalOverflow(page, label) {
  const metrics = await page.evaluate(() => {
    const doc = document.documentElement;
    const rightOf = selector => {
      const rect = document.querySelector(selector)?.getBoundingClientRect();
      return rect ? Math.round(rect.right) : 0;
    };
    return { scrollWidth: doc.scrollWidth, clientWidth: doc.clientWidth, frameRight: rightOf(".app-frame"), mainRight: rightOf(".app-main"), fadeRight: rightOf(".route-fade") };
  });
  const note = `${label}: ${JSON.stringify(metrics)}`;
  expect(metrics.scrollWidth, `horizontal overflow ${note}`).toBeLessThanOrEqual(metrics.clientWidth);
  expect(metrics.frameRight, `.app-frame wider than viewport ${note}`).toBeLessThanOrEqual(metrics.clientWidth + 1);
  expect(metrics.mainRight, `.app-main wider than viewport ${note}`).toBeLessThanOrEqual(metrics.clientWidth + 1);
  expect(metrics.fadeRight, `.route-fade wider than viewport ${note}`).toBeLessThanOrEqual(metrics.clientWidth + 1);
}

async function homeCategoryColours(page) {
  return page.evaluate(() => {
    const items = [...document.querySelectorAll(".category-bars > li")];
    const ranked = items.filter(el => !el.hasAttribute("data-other")).map(el => getComputedStyle(el.querySelector(".category-bars-name i")).backgroundColor);
    const other = items.find(el => el.hasAttribute("data-other"));
    const probe = document.createElement("span");
    probe.style.backgroundColor = "var(--chart-category-other)";
    document.body.appendChild(probe);
    const sand = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return {
      ranked,
      otherSwatch: other ? getComputedStyle(other.querySelector(".category-bars-name i")).backgroundColor : null,
      otherBar: other ? getComputedStyle(other.querySelector(".category-bars-track > span")).backgroundColor : null,
      sand,
    };
  });
}

const RETRO_HIDUP = ["rgb(31, 111, 104)", "rgb(176, 125, 18)", "rgb(74, 120, 168)", "rgb(138, 79, 134)", "rgb(184, 96, 42)", "rgb(125, 104, 179)"];

for (const width of [320, 360, 390, 1366]) {
  const height = width >= 1100 ? 900 : 740;
  const phone = width <= 600;

  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height } });

    test("every route fits the viewport and phone fields are 16px or larger", async ({ page }) => {
      for (const path of routes) {
        await page.goto(path, { waitUntil: "networkidle" });
        await page.locator("#main-content").waitFor();
        await expectNoHorizontalOverflow(page, `${width}px ${path}`);
        if (phone) {
          const smallInputs = await page.evaluate(() => [...document.querySelectorAll("input, select, textarea")].filter(el => {
            if (el.tagName === "INPUT" && ["hidden", "checkbox", "radio", "file", "button", "submit", "reset"].includes(el.type)) return false;
            const rect = el.getBoundingClientRect();
            return rect.width > 0 && rect.height > 0 && getComputedStyle(el).visibility !== "hidden";
          }).map(el => ({ name: el.name || el.getAttribute("aria-label") || el.tagName, fontSize: Number.parseFloat(getComputedStyle(el).fontSize) })).filter(item => item.fontSize < 16));
          expect(smallInputs, `${path} has inputs below 16px`).toEqual([]);
        }
      }
    });

    test("Beranda: category colours, ink hero figures, and the phone and desktop card layout", async ({ page }) => {
      await page.goto("/", { waitUntil: "networkidle" });
      const colours = await homeCategoryColours(page);
      expect(colours.ranked, "Beranda category bars use the six Retro hidup colours in rank order").toEqual(RETRO_HIDUP);
      expect(new Set(colours.ranked).size, `Beranda category bars repeat no colour: ${JSON.stringify(colours.ranked)}`).toBe(colours.ranked.length);
      expect(colours.otherSwatch, "Beranda Kategori lain swatch uses sand").toBe(colours.sand);
      expect(colours.otherBar, "Beranda Kategori lain bar uses sand").toBe(colours.sand);
      const heroColors = await page.evaluate(() => ({
        ink: getComputedStyle(document.body).color,
        amount: getComputedStyle(document.querySelector(".home-hero-amount")).color,
        figures: [...document.querySelectorAll(".home-figures dd")].map(el => getComputedStyle(el).color),
      }));
      expect(heroColors.amount, "Beranda hero Sisa renders in ink").toBe(heroColors.ink);
      expect(heroColors.figures.every(color => color === heroColors.ink), `Beranda hero figures render in ink: ${JSON.stringify(heroColors)}`).toBe(true);
      if (phone) {
        await expect(page.locator(".overview-trend"), "phone Beranda hides daily chart").toBeHidden();
        await expect(page.locator(".overview-wealth"), "phone Beranda hides Kekayaan card").toBeHidden();
        return;
      }
      await expect(page.locator(".overview-trend"), "desktop Beranda shows daily chart").toBeVisible();
      await expect(page.locator(".overview-wealth"), "desktop Beranda shows Kekayaan card").toBeVisible();
      const lowerBoxes = await page.evaluate(() => ({
        activity: document.querySelector(".overview-activity").getBoundingClientRect().height,
        wealth: document.querySelector(".overview-wealth").getBoundingClientRect().height,
        trendBottom: document.querySelector(".overview-trend").getBoundingClientRect().bottom,
        detailsBottom: document.querySelector(".overview-trend .review-daily").getBoundingClientRect().bottom,
        dailyWrapHeight: document.querySelector(".daily-spend .chart-wrap").getBoundingClientRect().height,
      }));
      const note = JSON.stringify(lowerBoxes);
      expect(lowerBoxes.wealth, `desktop Kekayaan card must not stretch to match Terbaru: ${note}`).toBeLessThan(lowerBoxes.activity - 40);
      expect(lowerBoxes.dailyWrapHeight, `desktop Pengeluaran harian chart grows to fill card: ${note}`).toBeGreaterThan(240);
      expect(lowerBoxes.trendBottom - lowerBoxes.detailsBottom, `desktop Pengeluaran harian leaves no dead space: ${note}`).toBeLessThanOrEqual(32);
      const fills = await page.evaluate(() => [...document.querySelectorAll(".daily-spend .recharts-bar-rectangle rect")].map(el => getComputedStyle(el).fill));
      expect(fills.length > 0 && fills.every(fill => fill === "rgb(47, 74, 109)"), `Beranda daily bars all render in single slate navy #2f4a6d: ${JSON.stringify(fills)}`).toBe(true);
      await page.getByText("Lihat nilai pengeluaran harian", { exact: true }).click();
      await expectNoHorizontalOverflow(page, `${width}px / expanded daily table`);
    });

    test("Laporan: shared category colours, drawer without sand swatch, chart polish and uniform disclosure rows", async ({ page }) => {
      await page.goto("/", { waitUntil: "networkidle" });
      const home = await homeCategoryColours(page);
      await page.goto("/analytics", { waitUntil: "networkidle" });
      await page.getByRole("heading", { name: "Posisi siklus" }).waitFor();
      await expect(page.locator(".kpi-note"), "Laporan omits Refund/Tentang posisi siklus line").toHaveCount(0);
      await expect(page.locator("#where .where-delta"), "Ke mana uangnya omits delta column").toHaveCount(0);
      expect(await page.locator("#where .where-share").count(), "Ke mana uangnya shows share percentages").toBeGreaterThanOrEqual(1);
      expect(await page.locator("#history .history-bar .history-seg").count(), "Siklus ke siklus draws category-stacked bars").toBeGreaterThanOrEqual(1);
      const colours = await page.evaluate(() => ({
        where: [...document.querySelectorAll("#where .where-list > li:not([data-other]) .where-swatch")].map(el => getComputedStyle(el).backgroundColor),
        history: [...document.querySelectorAll("#history .history-cols > li:last-child .history-seg")].map(el => getComputedStyle(el).backgroundColor),
      }));
      expect(colours.where, "Ke mana uangnya shares exact Retro hidup colours with Beranda").toEqual(home.ranked.slice(0, 3));
      expect(colours.history, "Siklus ke siklus shares exact Retro hidup colours with Beranda").toEqual(home.ranked.slice(0, 3));

      await page.locator("#where .where-other-summary").click();
      const otherExpanded = await page.evaluate(() => {
        const subRows = [...document.querySelectorAll("#where .where-other-list > li")];
        return { count: subRows.length, names: subRows.map(el => el.querySelector(".where-name")?.textContent?.trim()), swatches: subRows.reduce((sum, el) => sum + el.querySelectorAll(".where-swatch").length, 0) };
      });
      expect(otherExpanded, "Kategori lain expands to list remaining categories with no swatches").toEqual({ count: 1, names: ["Perjalanan"], swatches: 0 });
      await page.locator("#where .where-other-list button", { hasText: "Perjalanan" }).click();
      await page.locator("#category-drawer").waitFor();
      await expect(page.locator("#category-drawer .where-swatch"), "category drawer omits sand swatch for non-top-6 category").toHaveCount(0);
      await page.keyboard.press("Escape");
      await page.locator("#category-drawer").waitFor({ state: "detached" });

      const polish = await page.evaluate(() => {
        const barRects = [...document.querySelectorAll("#history .history-bar")].map(el => ({ width: el.getBoundingClientRect().width, buttonWidth: el.closest("button").getBoundingClientRect().width }));
        const plotHeight = document.querySelector("#history .history-plot")?.getBoundingClientRect().height || 0;
        const selectedBg = getComputedStyle(document.querySelector("#history .history-cols > li[data-selected]")).backgroundColor;
        const paceGrid = document.querySelector("#spending-shape .recharts-cartesian-grid")?.getBoundingClientRect();
        const endLabels = [...document.querySelectorAll("#spending-shape [data-pace-end]")].map(el => {
          const rect = el.getBoundingClientRect();
          return { key: el.getAttribute("data-pace-end"), text: el.textContent, left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom };
        });
        return { barRects, plotHeight, selectedBg, endLabels, paceGrid: paceGrid ? { top: paceGrid.top, bottom: paceGrid.bottom } : null };
      });
      if (width >= 1366) {
        expect(polish.barRects.every(item => item.width >= 56 && item.width <= 76), `Siklus ke siklus bars should be ~56-72px wide: ${JSON.stringify(polish.barRects)}`).toBe(true);
        expect(polish.plotHeight, "Siklus ke siklus plot grows to card height").toBeGreaterThanOrEqual(180);
      } else if (phone) {
        expect(polish.barRects.every(item => item.width >= item.buttonWidth - 8), `phone Siklus ke siklus bars fill column width: ${JSON.stringify(polish.barRects)}`).toBe(true);
      }
      expect(polish.selectedBg, "Siklus ke siklus keeps current-cycle highlight").not.toBe("rgba(0, 0, 0, 0)");
      expect(new Set(polish.endLabels.map(item => item.key)).size, `Laju pengeluaran has one end label per series: ${JSON.stringify(polish.endLabels)}`).toBe(polish.endLabels.length);
      for (const label of polish.endLabels) {
        if (polish.paceGrid) expect(label.top >= polish.paceGrid.top - 2 && label.bottom <= polish.paceGrid.bottom + 2, `Laju pengeluaran end label ${label.text} stays inside plot area`).toBe(true);
      }
      for (let i = 0; i < polish.endLabels.length; i += 1) {
        for (let j = i + 1; j < polish.endLabels.length; j += 1) {
          const a = polish.endLabels[i];
          const b = polish.endLabels[j];
          const overlapX = a.left < b.right && b.left < a.right;
          const verticalGap = Math.max(a.top - b.bottom, b.top - a.bottom);
          expect(overlapX && verticalGap < 4, `Laju pengeluaran end labels collide: ${JSON.stringify([a, b])}`).toBe(false);
        }
      }

      const stackRows = await page.evaluate(() => [...document.querySelectorAll(".review-stack .report-disclosure > summary")].map(summary => {
        const style = getComputedStyle(summary);
        const h2 = summary.querySelector("h2");
        const h2Style = getComputedStyle(h2);
        const h2Rect = h2.getBoundingClientRect();
        const span = summary.querySelector(":scope > span");
        return {
          title: h2.textContent,
          pad: `${style.paddingTop} ${style.paddingRight} ${style.paddingBottom} ${style.paddingLeft}`,
          titleSize: h2Style.fontSize,
          titleColor: h2Style.color,
          titleFont: h2Style.fontFamily,
          summarySize: getComputedStyle(span).fontSize,
          summaryBelow: span.getBoundingClientRect().top >= h2Rect.bottom - 1,
        };
      }));
      expect(stackRows.length, "Laporan bottom list has uniform rows").toBeGreaterThanOrEqual(2);
      const ink = await page.evaluate(() => getComputedStyle(document.body).color);
      for (const row of stackRows) {
        expect(row.pad, `${row.title} equal padding`).toBe("14px 16px 14px 16px");
        expect(row.titleSize, `${row.title} title 17px`).toBe("17px");
        expect(row.titleColor, `${row.title} title colour matches ink`).toBe(ink);
        expect(row.titleFont, `${row.title} uses Fraunces`).toMatch(/Fraunces/);
        expect(row.summarySize, `${row.title} summary 13px`).toBe("13px");
        expect(row.summaryBelow, `${row.title} summary sits on its own line below title`).toBe(true);
      }

      await page.evaluate(() => document.querySelectorAll("details").forEach(details => { details.open = true; }));
      await page.waitForTimeout(100);
      await expectNoHorizontalOverflow(page, `${width}px /analytics all disclosures open`);
      const audit = await page.evaluate(() => {
        const probe = document.createElement("span");
        probe.style.backgroundColor = "var(--chart-category-other)";
        document.body.appendChild(probe);
        const sand = getComputedStyle(probe).backgroundColor;
        probe.remove();
        const rogueSand = [...document.querySelectorAll("#where .where-swatch, #changes .heat-swatch, #matrix .heat-swatch")]
          .filter(el => getComputedStyle(el).backgroundColor === sand)
          .filter(el => !el.closest("li[data-other]") && !el.closest("span[data-muted]")).length;
        const dailyBars = [...document.querySelectorAll("#daily .cycle-spending-chart .recharts-bar-rectangle path, #daily .cycle-spending-chart .recharts-bar-rectangle rect")]
          .map(el => getComputedStyle(el).fill)
          .filter(fill => fill !== "none" && fill !== "rgba(0, 0, 0, 0)");
        return { rogueSand, dailyBars };
      });
      expect(audit.rogueSand, "no sand swatch appears on any row except Kategori lain").toBe(0);
      expect(audit.dailyBars.some(fill => fill === "rgb(47, 74, 109)"), `Laporan daily bars use slate navy #2f4a6d: ${JSON.stringify(audit.dailyBars)}`).toBe(true);
      if (phone) {
        const wideTables = await page.evaluate(() => [...document.querySelectorAll(".review-table-wrap, .merchant-rank-scroll")].map(el => ({ cls: el.className, label: el.getAttribute("aria-label"), scrollWidth: el.scrollWidth, clientWidth: el.clientWidth })).filter(item => item.scrollWidth > item.clientWidth));
        expect(wideTables, "Laporan tables never scroll sideways on a phone").toEqual([]);
      }
    });

    test("Keluarga: members above the add form, category list fits, Admin Richmod entry per width", async ({ page }) => {
      await page.goto("/household?tab=anggota", { waitUntil: "networkidle" });
      await page.locator("#tambah-anggota").waitFor();
      const order = await page.evaluate(() => {
        const list = [...document.querySelectorAll(".admin-stack > section")].find(section => section.querySelector("h2")?.textContent === "Anggota");
        return { listTop: list.getBoundingClientRect().top, formTop: document.querySelector("#tambah-anggota").getBoundingClientRect().top };
      });
      expect(order.listTop, `Anggota list sits above Tambah anggota form: ${JSON.stringify(order)}`).toBeLessThan(order.formTop);

      await page.goto("/household?tab=kategori", { waitUntil: "networkidle" });
      await page.getByRole("heading", { name: "Kategori keluarga", exact: true }).waitFor();
      await expectNoHorizontalOverflow(page, `${width}px /household?tab=kategori`);
      const section = await page.locator("section#kategori-keluarga").evaluate(el => ({ scrollWidth: el.scrollWidth, clientWidth: el.clientWidth, right: el.getBoundingClientRect().right, viewport: document.documentElement.clientWidth }));
      expect(section.scrollWidth <= section.clientWidth + 1 && section.right <= section.viewport + 1, `Kategori keluarga overflows: ${JSON.stringify(section)}`).toBe(true);

      const adminRow = page.locator("a.mobile-admin-entry[href='/admin']");
      if (width <= 1100) {
        await expect(adminRow, "superAdmin sees Admin Richmod row in Keluarga").toBeVisible();
        await expect(adminRow).toHaveText(/Admin Richmod/);
      } else {
        await expect(adminRow, "desktop hides mobile Admin Richmod row").toBeHidden();
        await expect(page.locator(".sidebar a.admin-link[href='/admin']"), "desktop sidebar keeps Admin link").toBeVisible();
      }
    });

    test("Transaksi: icon tones, filter bar, the Catat sheet and the detail drawer fit and lock scroll", async ({ page }) => {
      await page.goto("/transactions", { waitUntil: "networkidle" });
      await page.locator(".transaction-row").first().waitFor();
      const tones = await page.evaluate(() => {
        const toneFor = label => {
          const row = [...document.querySelectorAll(".transaction-row")].find(el => el.textContent.includes(label));
          return row?.querySelector(".category-icon")?.className || "";
        };
        return { childOfTop1: toneFor("Kategori Keluarga 8"), parentOfTop2AndTop5: toneFor("Kategori Keluarga 10"), siblingOfTop2AndTop5: toneFor("Kategori Keluarga 11"), unparentedOther: toneFor("Kategori Keluarga 9") };
      });
      const note = JSON.stringify(tones);
      expect(tones.childOfTop1, `child of top-6 parent inherits parent tone: ${note}`).toMatch(/\btone-1\b/);
      expect(tones.parentOfTop2AndTop5, `unranked parent inherits highest-ranked child tone: ${note}`).toMatch(/\btone-2\b/);
      expect(tones.siblingOfTop2AndTop5, `unranked sibling inherits highest-ranked family tone: ${note}`).toMatch(/\btone-2\b/);
      expect(tones.unparentedOther, `unparented non-top-6 stays sand: ${note}`).toMatch(/\btone-other\b/);
      await expectNoHorizontalOverflow(page, `${width}px /transactions`);
      const filterPrimary = await page.locator("div.filter-primary").evaluate(el => ({ scrollWidth: el.scrollWidth, clientWidth: el.clientWidth, right: el.getBoundingClientRect().right, viewport: document.documentElement.clientWidth }));
      expect(filterPrimary.scrollWidth <= filterPrimary.clientWidth + 1 && filterPrimary.right <= filterPrimary.viewport + 1, `filter-primary overflows: ${JSON.stringify(filterPrimary)}`).toBe(true);

      const trigger = width <= 1100 ? page.getByRole("button", { name: "Catat transaksi", exact: true }) : page.getByRole("button", { name: "Catat", exact: true });
      await trigger.click();
      const quick = page.locator("dialog.quick-record[open]");
      await quick.waitFor();
      const metrics = await quick.evaluate(dialog => {
        const body = dialog.querySelector(".quick-body");
        const seg = dialog.querySelector(".quick-body .segmented");
        return { dialogOverflow: dialog.scrollWidth > dialog.clientWidth, bodyScrollWidth: body.scrollWidth, bodyClientWidth: body.clientWidth, segRight: seg.getBoundingClientRect().right, bodyRight: body.getBoundingClientRect().right, htmlOverflowY: getComputedStyle(document.documentElement).overflowY };
      });
      const quickNote = JSON.stringify(metrics);
      expect(metrics.dialogOverflow, `Catat dialog overflows: ${quickNote}`).toBe(false);
      expect(metrics.bodyScrollWidth, `.quick-body scrolls sideways: ${quickNote}`).toBeLessThanOrEqual(metrics.bodyClientWidth);
      expect(metrics.segRight, `segmented control overflows .quick-body: ${quickNote}`).toBeLessThanOrEqual(metrics.bodyRight + 1);
      expect(metrics.htmlOverflowY, "background scroll locks while Catat dialog is open").toBe("hidden");

      if (width === 360 || width === 390) {
        const visibleHeight = height - KEYBOARD;
        const before = await quick.evaluate(dialog => {
          const amount = dialog.querySelector('input[name="amount"]');
          const repeat = dialog.querySelector('[aria-label="Ulangi cepat"]');
          const repeatRow = repeat?.querySelector("div");
          const desc = dialog.querySelector('input[name="description"]');
          return {
            focusedName: document.activeElement?.getAttribute("name") || "",
            amountHint: amount.getAttribute("enterkeyhint"),
            descHint: desc.getAttribute("enterkeyhint"),
            amountBottom: amount.getBoundingClientRect().bottom,
            repeatTop: repeat.getBoundingClientRect().top,
            repeatBottom: repeat.getBoundingClientRect().bottom,
            descTop: desc.getBoundingClientRect().top,
            repeatScrollsX: repeatRow.scrollWidth > repeatRow.clientWidth,
          };
        });
        expect(before.focusedName, "Catat autofocuses amount").toBe("amount");
        expect(before.amountHint, "Catat amount uses enterKeyHint=next").toBe("next");
        expect(before.descHint, "Catat description uses enterKeyHint=done").toBe("done");
        expect(before.repeatTop >= before.amountBottom - 2 && before.repeatBottom <= before.descTop + 2, `Ulangi cepat sits directly between amount and description: ${JSON.stringify(before)}`).toBe(true);
        expect(before.repeatScrollsX, "Ulangi cepat chip row scrolls horizontally without widening the sheet").toBe(true);

        await page.evaluate(simulated => {
          Object.defineProperty(window.visualViewport, "height", { configurable: true, get: () => simulated });
          window.visualViewport.dispatchEvent(new Event("resize"));
        }, visibleHeight);
        await expect.poll(() => page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--kb-inset").trim())).toBe(`${KEYBOARD}px`);
        const during = await quick.evaluate(dialog => {
          const docStyle = getComputedStyle(document.documentElement);
          const nav = document.querySelector(".mobile-nav");
          return {
            vvHeight: docStyle.getPropertyValue("--vv-height").trim(),
            navDisplay: nav ? getComputedStyle(nav).display : "none",
            submitRect: dialog.querySelector('.quick-savebar button[type="submit"]').getBoundingClientRect().toJSON(),
            amountRect: dialog.querySelector('input[name="amount"]').getBoundingClientRect().toJSON(),
            repeatRect: dialog.querySelector('[aria-label="Ulangi cepat"]').getBoundingClientRect().toJSON(),
            dialogOverflow: dialog.scrollWidth > dialog.clientWidth,
          };
        });
        expect(during.vvHeight, "--vv-height matches visual viewport height").toBe(`${visibleHeight}px`);
        expect(during.navDisplay, ".mobile-nav hides while typing").toBe("none");
        expect(during.dialogOverflow, "Catat dialog has no horizontal overflow with keyboard open").toBe(false);
        expect(during.submitRect.top >= 0 && during.submitRect.bottom <= visibleHeight + 1, `Catat button stays inside visual viewport (${visibleHeight}px): ${JSON.stringify(during.submitRect)}`).toBe(true);
        expect(during.amountRect.top >= 0 && during.amountRect.bottom <= visibleHeight + 1, `focused amount stays inside visual viewport: ${JSON.stringify(during.amountRect)}`).toBe(true);
        expect(during.repeatRect.top >= 0 && during.repeatRect.bottom <= visibleHeight + 1, `Ulangi cepat row stays visible above keyboard: ${JSON.stringify(during.repeatRect)}`).toBe(true);

        await page.keyboard.press("Enter");
        const afterEnter = await page.evaluate(() => ({ focusedName: document.activeElement?.getAttribute("name") || "", dialogOpen: Boolean(document.querySelector("dialog.quick-record[open]")) }));
        expect(afterEnter.focusedName, "Enter on amount moves focus to description").toBe("description");
        expect(afterEnter.dialogOpen, "Enter on amount never submits half-filled form").toBe(true);

        await page.evaluate(() => {
          delete window.visualViewport.height;
          window.visualViewport.dispatchEvent(new Event("resize"));
        });
        await expect.poll(() => page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--kb-inset").trim())).not.toBe(`${KEYBOARD}px`);
      }
      await page.keyboard.press("Escape");
      await quick.waitFor({ state: "detached" });

      await page.locator(".transaction-row").nth(4).click();
      const drawer = page.locator("aside[role='dialog'].detail-drawer");
      await drawer.waitFor();
      await page.waitForFunction(() => {
        const el = document.querySelector("aside[role='dialog'].detail-drawer");
        return el && getComputedStyle(el).transform === "none";
      });
      const scrollBefore = await page.evaluate(() => window.scrollY);
      const state = await drawer.evaluate(el => {
        const rect = el.getBoundingClientRect();
        return {
          htmlOverflowY: getComputedStyle(document.documentElement).overflowY,
          overscrollY: getComputedStyle(el).overscrollBehaviorY,
          left: Math.round(rect.left),
          top: Math.round(rect.top),
          width: Math.round(rect.width),
          height: Math.round(rect.height),
          viewportWidth: document.documentElement.clientWidth,
          viewportHeight: window.innerHeight,
          drawerOverflowX: el.scrollWidth > el.clientWidth,
        };
      });
      expect(state.htmlOverflowY, "html locks scroll while drawer is open").toBe("hidden");
      expect(state.overscrollY, "drawer contains overscroll").toBe("contain");
      expect(state.drawerOverflowX, "drawer does not scroll sideways").toBe(false);
      if (phone) {
        expect(state.left, "phone drawer fills full width").toBe(0);
        expect(state.top, "phone drawer starts at top").toBe(0);
        expect(Math.abs(state.width - state.viewportWidth), `phone drawer width matches viewport: ${JSON.stringify(state)}`).toBeLessThanOrEqual(1);
        expect(Math.abs(state.height - state.viewportHeight), `phone drawer height fills screen: ${JSON.stringify(state)}`).toBeLessThanOrEqual(1);
      }
      await page.mouse.move(10, 10);
      await page.mouse.wheel(0, 600);
      await page.waitForTimeout(80);
      expect(await page.evaluate(() => window.scrollY), "background page does not scroll while drawer is open").toBe(scrollBefore);

      if (width === 360 || width === 390) {
        const visibleHeight = height - KEYBOARD;
        await drawer.getByRole("button", { name: "Ubah", exact: true }).click();
        const edit = page.locator("dialog.create-dialog[open]");
        await edit.waitFor();
        await edit.locator('input[name="description"]').focus();
        await page.evaluate(simulated => {
          Object.defineProperty(window.visualViewport, "height", { configurable: true, get: () => simulated });
          window.visualViewport.dispatchEvent(new Event("resize"));
        }, visibleHeight);
        await expect.poll(() => page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--kb-inset").trim())).toBe(`${KEYBOARD}px`);
        const editKb = await edit.evaluate(dialog => {
          const span = dialog.querySelector('label:has(input[name="description"]) > span');
          const mark = span?.querySelector(".required-mark");
          return {
            saveRect: dialog.querySelector('.dialog-actions button:last-child').getBoundingClientRect().toJSON(),
            dialogOverflow: dialog.scrollWidth > dialog.clientWidth,
            descSpanRect: span?.getBoundingClientRect().toJSON() || null,
            markRect: mark?.getBoundingClientRect().toJSON() || null,
          };
        });
        expect(editKb.dialogOverflow, "Ubah dialog does not overflow horizontally").toBe(false);
        expect(editKb.descSpanRect && editKb.markRect, "Ubah Deskripsi renders inline required mark inside label span").toBeTruthy();
        expect(editKb.descSpanRect.height < 28 && Math.abs(editKb.markRect.top - editKb.descSpanRect.top) < 6, `Ubah Deskripsi required mark stays inline: ${JSON.stringify(editKb)}`).toBe(true);
        expect(editKb.saveRect.top >= 0 && editKb.saveRect.bottom <= visibleHeight + 1, `Ubah Simpan button stays inside visual viewport (${visibleHeight}px): ${JSON.stringify(editKb.saveRect)}`).toBe(true);
        await page.evaluate(() => {
          delete window.visualViewport.height;
          window.visualViewport.dispatchEvent(new Event("resize"));
        });
        await page.keyboard.press("Escape");
        await edit.waitFor({ state: "detached" });
      }
      await page.keyboard.press("Escape");
      await drawer.waitFor({ state: "detached" });
      expect(await page.evaluate(() => getComputedStyle(document.documentElement).overflowY), "scroll unlocks when drawer closes").not.toBe("hidden");
    });
  });
}

test.describe("a household member who is not a platform admin", () => {
  test.use({ user: owner(), viewport: { width: 390, height: 844 } });

  test("never sees the Admin Richmod entry", async ({ page }) => {
    await page.goto("/household", { waitUntil: "networkidle" });
    await page.locator("#main-content").waitFor();
    await expect(page.locator("a.mobile-admin-entry[href='/admin']")).toHaveCount(0);
  });
});
