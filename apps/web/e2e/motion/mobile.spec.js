import { test, expect, reply } from "../support/fixtures.js";
import { ledgerBasics, transaction } from "../support/data.js";
import { cycleFacts } from "../../tests/fixtures/cycle-review.mjs";

test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, reducedMotion: "no-preference" });

test.beforeEach(async ({ api }) => {
  api.use(request => {
    const { path } = request;
    if (path === "/api/v1/transactions") return reply([transaction("tx-1")], { delay: 400 });
    if (path.startsWith("/api/v1/transactions/tx-")) return path.endsWith("/evidence") || path.endsWith("/audit") ? [] : transaction("tx-1");
    if (path === "/api/v1/analytics/cycle-review") return cycleFacts();
    if (path === "/api/v1/analytics/cycle-decisions") return { items: [], previous: [], previousCycleStart: "2026-08-26" };
    return ledgerBasics(path);
  });
});

const nav = ".mobile-nav";
const slideX = page => page.locator(nav).evaluate(node => Number.parseFloat(node.style.getPropertyValue("--slide-x")));

async function tallPage(page, url) {
  await page.goto(url, { waitUntil: "networkidle" });
  await page.locator("#main-content").waitFor();
  await page.evaluate(() => {
    const spacer = document.createElement("div");
    spacer.style.height = "3000px";
    document.body.appendChild(spacer);
  });
}

test("a tab tap resets the scroll instantly inside the page transition, not as a smooth scroll before it", async ({ page }) => {
  await tallPage(page, "/");
  await page.evaluate(() => window.scrollTo({ top: 900, behavior: "instant" }));
  expect(await page.evaluate(() => window.scrollY)).toBe(900);
  await page.evaluate(() => {
    const record = { samples: [], atEnter: null, enter: false };
    window.__scroll = record;
    const sample = () => {
      record.samples.push(window.scrollY);
      if (!record.enter && document.getAnimations().some(animation => animation.animationName === "page-enter")) {
        record.enter = true;
        record.atEnter = window.scrollY;
      }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
  await page.tap(`${nav} a[href='/analytics']`);
  await page.waitForFunction(() => window.__scroll.enter, null, { timeout: 3000 });
  await page.waitForTimeout(800);
  const record = await page.evaluate(() => window.__scroll);
  expect(record.atEnter, "the new page starts at the top when its enter animation starts").toBe(0);
  expect(record.samples.filter(value => value !== 0 && value !== 900), "the scroll jumps from the old position to the top and is never in between").toEqual([]);
  expect(record.samples.at(-1)).toBe(0);
});

test("the bottom bar is its own transition group, so the page never paints over the tabs", async ({ page }) => {
  await page.goto("/", { waitUntil: "networkidle" });
  await page.locator("#main-content").waitFor();
  expect(await page.locator(nav).evaluate(node => getComputedStyle(node).viewTransitionName)).toBe("mobile-nav");
  const add = page.locator(`${nav} .mobile-add`);
  const box = await add.boundingBox();
  const bar = await page.locator(nav).boundingBox();
  const clip = { x: box.x - 6, y: bar.y + 2, width: box.width + 12, height: bar.height - 2 };
  const rest = await page.screenshot({ clip });
  await page.evaluate(() => {
    const record = { ready: false, styles: null };
    window.__transition = record;
    const original = document.startViewTransition.bind(document);
    document.startViewTransition = (...args) => {
      const transition = original(...args);
      transition.ready.then(() => {
        const root = document.documentElement;
        record.styles = {
          oldDisplay: getComputedStyle(root, "::view-transition-old(mobile-nav)").display,
          newAnimation: getComputedStyle(root, "::view-transition-new(mobile-nav)").animationName,
          groupAnimation: getComputedStyle(root, "::view-transition-group(mobile-nav)").animationName,
          pageSwap: getComputedStyle(root, "::view-transition-new(app-main)").animationName,
        };
        document.getAnimations().forEach(animation => {
          animation.pause();
          animation.currentTime = 120;
        });
        record.ready = true;
      }, () => {});
      return transition;
    };
  });
  await page.tap(`${nav} a[href='/transactions']`);
  await page.waitForFunction(() => window.__transition.ready, null, { timeout: 3000 });
  const frozen = await page.evaluate(() => window.__transition.styles);
  expect(frozen, "the nav group carries no animation, hides the old image and shows the live one").toEqual({ oldDisplay: "none", newAnimation: "none", groupAnimation: "none", pageSwap: "page-enter" });
  const during = await page.screenshot({ clip });
  expect(during.equals(rest), "the add button is untouched while the page animates beneath it").toBe(true);
});

test("the active pill slides to the tapped tab and never lands on the add button", async ({ page }) => {
  await page.goto("/", { waitUntil: "networkidle" });
  await page.locator(`${nav}[data-slide-ready]`).waitFor();
  const look = await page.locator(nav).evaluate(node => {
    const probe = document.createElement("i");
    probe.style.cssText = "background: var(--accent-soft); border-radius: var(--radius-sm)";
    document.body.appendChild(probe);
    const expected = { background: getComputedStyle(probe).backgroundColor, radius: getComputedStyle(probe).borderTopLeftRadius };
    probe.remove();
    const pill = getComputedStyle(node, "::before");
    const active = getComputedStyle(node.querySelector("a.active"));
    return { position: getComputedStyle(node).position, expected, pill: { background: pill.backgroundColor, radius: pill.borderTopLeftRadius, border: pill.borderTopWidth, shadow: pill.boxShadow, opacity: pill.opacity }, activeBackground: active.backgroundColor };
  });
  expect(look.position, "the bar stays fixed").toBe("fixed");
  expect(look.pill, "the pill looks like the old active tab").toEqual({ background: look.expected.background, radius: look.expected.radius, border: "0px", shadow: "none", opacity: "1" });
  expect(look.activeBackground, "the active link is transparent over the pill").toBe("rgba(0, 0, 0, 0)");

  const before = await slideX(page);
  await page.evaluate(() => {
    const record = [];
    window.__pill = record;
    document.addEventListener("transitionrun", event => { if (event.target.matches(".mobile-nav")) record.push(`${event.pseudoElement}:${event.propertyName}`); }, true);
  });
  await page.tap(`${nav} a[href='/analytics']`);
  await page.waitForFunction(previous => Number.parseFloat(document.querySelector(".mobile-nav").style.getPropertyValue("--slide-x")) !== previous, before, { timeout: 3000 });
  const geometry = await page.evaluate(() => {
    const bar = document.querySelector(".mobile-nav");
    const link = bar.querySelector("a.active");
    return { x: Number.parseFloat(bar.style.getPropertyValue("--slide-x")), w: Number.parseFloat(bar.style.getPropertyValue("--slide-w")), link: link.getAttribute("href"), left: link.getBoundingClientRect().left - bar.getBoundingClientRect().left, width: link.getBoundingClientRect().width };
  });
  expect(geometry.link).toBe("/analytics");
  expect(Math.abs(geometry.x - geometry.left), "the pill sits on the tapped tab").toBeLessThan(1.5);
  expect(Math.abs(geometry.w - geometry.width)).toBeLessThan(1.5);
  expect(await page.evaluate(() => window.__pill), "the pill moves by a transition, not a jump").toContain("::before:transform");
  const add = await page.locator(`${nav} .mobile-add`).boundingBox();
  const navBox = await page.locator(nav).boundingBox();
  expect(geometry.x + navBox.x + geometry.w <= add.x || geometry.x + navBox.x >= add.x + add.width, "the pill is not under the add button").toBe(true);
});

test("a page with no active tab hides the pill", async ({ page }) => {
  await page.goto("/inbox", { waitUntil: "networkidle" });
  await page.locator(`${nav}[data-slide-ready]`).waitFor();
  expect(await page.locator(nav).evaluate(node => ({ none: node.hasAttribute("data-slide-none"), active: node.querySelectorAll("a.active").length, opacity: getComputedStyle(node, "::before").opacity }))).toEqual({ none: true, active: 0, opacity: "0" });
});

test("a phone sheet stays opaque while it moves", async ({ page }) => {
  await page.goto("/", { waitUntil: "networkidle" });
  await page.locator(`${nav}[data-slide-ready]`).waitFor();
  await page.evaluate(() => {
    const record = { properties: [], frozen: null };
    window.__sheet = record;
    document.addEventListener("transitionrun", event => {
      if (!event.target.matches("dialog") || event.pseudoElement) return;
      record.properties.push(event.propertyName);
      event.target.getAnimations().forEach(animation => {
        animation.pause();
        animation.currentTime = 80;
      });
      const style = getComputedStyle(event.target);
      record.frozen = { opacity: style.opacity, transform: style.transform };
    }, true);
  });
  await page.tap(`${nav} .mobile-add`);
  await page.waitForSelector("dialog.quick-record[open]");
  const record = await page.evaluate(() => window.__sheet);
  expect(record.properties, "the sheet moves by transform").toContain("transform");
  expect(record.properties, "no opacity transition runs on a phone sheet").not.toContain("opacity");
  expect(record.frozen.opacity, "the page does not show through the sheet mid-travel").toBe("1");
  expect(record.frozen.transform, "the sheet is mid-travel").not.toBe("none");
});

test("a phone drawer stays opaque while its dim fades in", async ({ page }) => {
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.waitForSelector(".ledger-panel");
  await page.evaluate(() => {
    const record = { names: [], frozen: null };
    window.__drawer = record;
    document.addEventListener("animationstart", event => {
      if (!event.target.matches(".drawer-backdrop")) return;
      record.names.push(event.animationName);
      event.target.getAnimations().forEach(animation => {
        animation.pause();
        animation.currentTime = 100;
      });
      const backdrop = getComputedStyle(event.target);
      const drawer = getComputedStyle(event.target.querySelector(".detail-drawer"));
      record.frozen = { backdropOpacity: backdrop.opacity, backdropColor: backdrop.backgroundColor, backdropFilter: backdrop.backdropFilter, drawerOpacity: drawer.opacity };
    }, true);
  });
  await page.locator(".ledger-panel button").first().tap();
  await page.waitForSelector(".detail-drawer");
  const record = await page.evaluate(() => window.__drawer);
  expect(record.names).toContain("backdrop-in");
  expect(record.frozen.backdropOpacity, "the backdrop element itself is never faded").toBe("1");
  expect(record.frozen.drawerOpacity, "the drawer is opaque while the backdrop dims").toBe("1");
  const alpha = Number.parseFloat(record.frozen.backdropColor.split(",")[3]);
  expect(alpha, `the dim is mid-fade: ${record.frozen.backdropColor}`).toBeGreaterThan(0);
  expect(alpha).toBeLessThan(0.42);
});

test("no <option> is tagged for the route reveal", async ({ page }) => {
  await page.goto("/transactions", { waitUntil: "domcontentloaded" });
  await page.waitForSelector(".ledger-panel");
  const counts = await page.evaluate(() => ({ options: document.querySelectorAll("select option").length, revealed: document.querySelectorAll("option[data-reveal], optgroup[data-reveal], select [data-reveal]").length, tagged: document.querySelectorAll("[data-reveal]").length }));
  expect(counts.options, "the page has select options to check").toBeGreaterThan(0);
  expect(counts.tagged, "the reveal still tags real content").toBeGreaterThan(0);
  expect(counts.revealed, "options inside a select are not tagged").toBe(0);
});
