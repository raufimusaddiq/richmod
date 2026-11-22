import { test, expect, reply } from "../support/fixtures.js";
import { ledgerBasics, transaction } from "../support/data.js";
import { frames } from "../support/sample.js";
import { layoutApi } from "../support/layoutApi.js";

const firstPage = () => ["Pasar", "Grab", "Kopi", "Listrik", "Air", "Gym", "Buku", "Bensin", "Tol", "Parkir"].map((name, index) => transaction(`tx-${index}`, name, { transactionAt: `2026-09-${String(10 - Math.floor(index / 3)).padStart(2, "0")}T09:20:00+07:00` }));
const probeMarkup = '<details id="probe"><summary>Probe</summary><div style="height:200px">isi</div></details>';


test.beforeEach(async ({ api }) => {
  const rows = firstPage();
  api.use(request => {
    if (request.path === "/api/v1/transactions") {
      const second = request.query.get("cursor") === "cursor-2";
      const body = second ? [transaction("tx-90", "Halaman dua", { transactionAt: "2026-09-01T09:20:00+07:00" }), transaction("tx-91", "Halaman dua b", { transactionAt: "2026-09-01T09:20:00+07:00" })] : rows;
      return reply(body, { delay: second ? 0 : 400, headers: second ? {} : { "X-Next-Cursor": "cursor-2" } });
    }
    return ledgerBasics(request.path);
  });
});

test.describe("with motion", () => {
  test.use({ reducedMotion: "no-preference" });

  test("first-load rows reveal with the route, a started list never staggers late items, pagination does not stagger", async ({ page }) => {
    await page.addInitScript(() => {
      window.__stagger = 0;
      new MutationObserver(records => {
        for (const record of records) if (record.target.hasAttribute("data-stagger-item")) window.__stagger++;
      }).observe(document, { subtree: true, attributes: true, attributeFilter: ["data-stagger-item"] });
    });
    await page.goto("/transactions", { waitUntil: "domcontentloaded" });
    await page.waitForSelector(".transaction-row");
    const revealed = await page.evaluate(() => {
      const rows = [...document.querySelectorAll(".transaction-row")];
      const running = rows.flatMap(row => row.getAnimations().map(animation => animation.animationName));
      return { rows: rows.length, inside: rows.filter(row => row.closest("[data-reveal]")).length, stagger: running.filter(name => name === "stagger-in").length, reveal: document.querySelectorAll("[data-reveal]").length };
    });
    expect(revealed.reveal, "skeleton to content reveals the list").toBeGreaterThan(0);
    expect(revealed.inside, "every row sits inside the reveal").toBe(revealed.rows);
    expect(revealed.stagger, "no row staggers inside a route reveal").toBe(0);
    await page.waitForFunction(() => !document.querySelector("[data-stagger-item]"), null, { timeout: 3000 });

    await page.evaluate(() => {
      const host = document.createElement("div");
      host.id = "stagger-probe";
      host.setAttribute("data-stagger", ".probe-item");
      host.innerHTML = Array.from({ length: 12 }, (_, index) => `<p class="probe-item">Item ${index}</p>`).join("");
      document.querySelector("main").appendChild(host);
    });
    const probe = await page.evaluate(() => {
      const items = [...document.querySelectorAll("#stagger-probe .probe-item")];
      return { marked: items.filter(item => item.hasAttribute("data-stagger-item")).length, running: items.filter(item => item.getAnimations().some(animation => animation.animationName === "stagger-in")).length, delays: items.map(item => getComputedStyle(item).animationDelay) };
    });
    const delays = probe.delays.map(value => Number.parseFloat(value) * (value.endsWith("ms") ? 1 : 1000));
    expect({ marked: probe.marked, running: probe.running }, "first appearance stamps and animates every item").toEqual({ marked: 12, running: 12 });
    expect(Math.max(...delays), "stagger is capped").toBeLessThanOrEqual(400);
    expect(delays[11], "items past the cap share the last delay").toBe(delays[7]);
    expect(delays[1] > delays[0] && delays[3] > delays[2], `items are staggered apart: ${delays.join(",")}`).toBe(true);
    await page.evaluate(() => document.querySelector("#stagger-probe").insertAdjacentHTML("beforeend", '<p class="probe-item">Late</p>'));
    expect(await page.evaluate(() => document.querySelector("#stagger-probe").lastElementChild.hasAttribute("data-stagger-item")), "an item appended to a started list is not staggered").toBe(false);
    await page.waitForFunction(() => !document.querySelector("[data-stagger-item]"), null, { timeout: 3000 });
    await page.evaluate(() => document.querySelector("#stagger-probe").remove());
    await page.evaluate(() => { window.__stagger = 0; });
    await page.getByRole("button", { name: "Berikutnya", exact: true }).click();
    await page.waitForSelector(".transaction-row:has-text('Halaman dua')");
    await page.waitForTimeout(700);
    expect(await page.evaluate(() => window.__stagger), "pagination staggers no rows").toBe(0);
  });

  test("the filter panel and a details disclosure animate open and closed", async ({ page }) => {
    await page.goto("/transactions", { waitUntil: "networkidle" });
    await page.waitForSelector(".transaction-row");
    const wasHidden = await page.evaluate(() => document.querySelector(".filter-more").hidden);
    const opening = await frames(page, ".filter-toggle", () => ({ h: document.querySelector(".filter-more").getBoundingClientRect().height }));
    const openHeights = opening.map(row => row.h);
    const openFinal = openHeights.at(-1);
    expect(wasHidden && openFinal >= 40, `filter panel opens: ${openFinal}`).toBe(true);
    expect(openHeights.some(value => value > 0 && value < openFinal), `filter panel animates open: ${openHeights.map(Math.round)}`).toBe(true);
    const closing = await frames(page, ".filter-toggle", () => ({ h: document.querySelector(".filter-more").getBoundingClientRect().height }));
    const closeHeights = closing.map(row => row.h);
    expect(closeHeights.some(value => value > 0 && value < openFinal) && closeHeights.at(-1) === 0, `filter panel animates closed: ${closeHeights.map(Math.round)}`).toBe(true);

    const shut = await page.evaluate(markup => {
      document.querySelector("main").insertAdjacentHTML("beforeend", markup);
      return document.querySelector("#probe").getBoundingClientRect().height;
    }, probeMarkup);
    const detailsOpening = (await frames(page, "#probe summary", () => ({ h: document.querySelector("#probe").getBoundingClientRect().height }))).map(row => row.h);
    const openedHeight = detailsOpening.at(-1);
    expect(detailsOpening.some(value => value > shut && value < openedHeight - 10), `details animate open: ${shut},${detailsOpening.map(Math.round)}`).toBe(true);
    const detailsClosing = (await frames(page, "#probe summary", () => ({ h: document.querySelector("#probe").getBoundingClientRect().height }))).map(row => row.h);
    expect(detailsClosing.some(value => value > shut && value < openedHeight - 10) && Math.abs(detailsClosing.at(-1) - shut) < 2, `details animate closed: ${detailsClosing.map(Math.round)}`).toBe(true);
  });

  test("the tab pill slides to the chosen tab and the panel re-enters", async ({ page }) => {
    await page.goto("/inbox", { waitUntil: "networkidle" });
    await page.waitForSelector(".inbox-tabs[data-slide-ready]");
    const startX = await page.evaluate(() => Number.parseFloat(document.querySelector(".inbox-tabs").style.getPropertyValue("--slide-x")));
    const slide = await frames(page, ".inbox-tabs > button:nth-child(2)", () => {
      const container = document.querySelector(".inbox-tabs");
      const panel = document.querySelector("#inbox-panel");
      return { x: new DOMMatrix(getComputedStyle(container, "::before").transform).m41, goal: container.querySelector('[aria-selected="true"]').offsetLeft, entering: panel.hasAttribute("data-panel-enter"), opacity: Number(getComputedStyle(panel).opacity) };
    }, 1000);
    const goal = slide.at(-1).goal;
    expect(slide.some(row => row.x > startX && row.x < goal - 2), `pill does not jump: start ${startX}, goal ${goal}, samples ${slide.map(row => Math.round(row.x))}`).toBe(true);
    expect(Math.abs(slide.at(-1).x - goal), `pill lands: ${slide.at(-1).x} vs ${goal}`).toBeLessThanOrEqual(1);
    const fit = await page.evaluate(() => {
      const container = document.querySelector(".inbox-tabs");
      const pill = getComputedStyle(container, "::before");
      const box = container.querySelector('[aria-selected="true"]').getBoundingClientRect();
      return Math.abs(Number.parseFloat(pill.width) - box.width) + Math.abs(Number.parseFloat(pill.height) - box.height);
    });
    expect(fit, "pill box matches the active button").toBeLessThanOrEqual(0.5);
    expect(slide.some(row => row.entering && row.opacity < 1), "panel re-enters on tab change").toBe(true);
    expect(slide.at(-1).entering, "panel marker is consumed").toBe(false);
    const roles = await page.evaluate(() => [...document.querySelectorAll(".inbox-tabs > button")].map(button => `${button.getAttribute("role")}:${button.getAttribute("aria-selected")}:${button.tabIndex}`));
    expect(roles.join(), "tab semantics are untouched").toBe("tab:false:-1,tab:true:0");
  });
});

test.describe("with reduced motion", () => {
  test.use({ reducedMotion: "reduce" });

  test("the pill, rows and filter panel are instant", async ({ page }) => {
    await page.goto("/inbox", { waitUntil: "networkidle" });
    await page.waitForSelector(".inbox-tabs[data-slide-ready]");
    const calm = await frames(page, ".inbox-tabs > button:nth-child(2)", () => {
      const container = document.querySelector(".inbox-tabs");
      return { x: new DOMMatrix(getComputedStyle(container, "::before").transform).m41, goal: container.querySelector('[aria-selected="true"]').offsetLeft };
    }, 300);
    const settled = calm.filter(row => row.t > 40);
    expect(settled.every(row => Math.abs(row.x - row.goal) <= 1), `reduced motion does not slide: ${calm.map(row => Math.round(row.x))}`).toBe(true);
    await page.goto("/transactions", { waitUntil: "networkidle" });
    await page.waitForSelector(".transaction-row");
    const animatedRows = await page.evaluate(() => [...document.querySelectorAll(".transaction-row")].filter(row => row.getAnimations().length).length);
    expect(animatedRows, "reduced motion animates no rows").toBe(0);
    const instant = await frames(page, ".filter-toggle", () => ({ h: document.querySelector(".filter-more").getBoundingClientRect().height }), 300);
    expect(instant.filter(row => row.t > 40).every(row => row.h >= 40), `reduced motion does not delay the filter panel: ${instant.map(row => Math.round(row.h))}`).toBe(true);
  });
});

test.describe("coverage of the shared mechanisms", () => {
  test.use({ reducedMotion: "no-preference" });

  const strips = [
    ["/analytics", ".report-switch"],
    ["/analytics", ".range-control-group"],
    ["/household", ".settings-tabs"],
    ["/documents", ".filter-chips"],
    ["/inbox", ".inbox-tabs"],
  ];

  for (const [path, selector] of strips) {
    test(`${selector} on ${path} gets the sliding pill`, async ({ page, api }) => {
      layoutApi(api);
      await page.goto(path, { waitUntil: "networkidle" });
      await expect(page.locator(`${selector}[data-slide]`).first()).toBeVisible();
      await expect(page.locator(`${selector}[data-slide-ready]`).first()).toBeAttached();
    });
  }

  test("first-load lists opt into the stagger", async ({ page, api }) => {
    layoutApi(api);
    for (const [path, selector] of [["/transactions", "[data-stagger='.transaction-row']"], ["/documents", "[data-stagger='.document-card']"], ["/", "[data-stagger=':scope > li']"], ["/household", "[data-stagger=':scope > article']"]]) {
      await page.goto(path, { waitUntil: "networkidle" });
      await expect(page.locator(selector).first(), `${path} lists stagger on first load`).toBeAttached();
    }
  });

  test("in-app navigation runs inside a view transition", async ({ page, api }) => {
    layoutApi(api);
    await page.addInitScript(() => {
      window.__transitions = 0;
      const original = document.startViewTransition?.bind(document);
      if (original) document.startViewTransition = (...args) => { window.__transitions += 1; return original(...args); };
    });
    await page.goto("/", { waitUntil: "networkidle" });
    await page.locator("#main-content").waitFor();
    await page.getByRole("navigation", { name: "Navigasi utama", exact: true }).getByRole("link", { name: "Transaksi" }).click();
    await page.locator(".transaction-row").first().waitFor();
    expect(await page.evaluate(() => window.__transitions), "navigation starts a view transition").toBeGreaterThan(0);
  });
});
