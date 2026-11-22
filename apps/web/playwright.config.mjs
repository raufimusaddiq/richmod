import { defineConfig } from "@playwright/test";

const port = Number(process.env.E2E_PORT || 4173);
const baseURL = `http://127.0.0.1:${port}`;
const ci = Boolean(process.env.CI);
const visualEnabled = process.platform === "linux" || process.env.PLAYWRIGHT_VISUAL === "1";

const phones = { mobile: { width: 390, height: 844 }, "small-mobile": { width: 320, height: 740 } };
const wide = { desktop: { width: 1440, height: 1050 }, tablet: { width: 1024, height: 768 } };
const viewports = { ...wide, ...phones };
const appProjects = new Set(["desktop", "mobile"]);
const desktopOnly = /@desktop-only/;
const shardOrder = ["widths-webkit", "desktop", "widths", "mobile", "tablet", "small-mobile", "motion", "visual"];

const responsive = Object.entries(viewports).map(([name, viewport]) => ({
  name,
  testMatch: appProjects.has(name) ? ["responsive/**/*.spec.js", "app/**/*.spec.js"] : ["responsive/**/*.spec.js"],
  ...(name === "mobile" ? { grepInvert: desktopOnly } : {}),
  use: { viewport },
}));

export default defineConfig({
  testDir: "./e2e",
  outputDir: "./test-results",
  snapshotPathTemplate: "{testDir}/visual/baselines/{arg}{ext}",
  fullyParallel: true,
  forbidOnly: ci,
  retries: ci ? 1 : 0,
  workers: ci ? 4 : undefined,
  timeout: 60_000,
  expect: {
    timeout: 10_000,
    toHaveScreenshot: { animations: "disabled", caret: "hide", maxDiffPixelRatio: 0.005 },
  },
  reporter: ci ? [["list"], ["blob"], ["github"]] : [["list"]],
  use: {
    baseURL,
    locale: "id-ID",
    timezoneId: "Asia/Jakarta",
    reducedMotion: "reduce",
    serviceWorkers: "block",
    actionTimeout: 15_000,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: {
    command: "node scripts/static-server.mjs",
    env: { PORT: String(port) },
    url: baseURL,
    reuseExistingServer: false,
    timeout: 30_000,
  },
  projects: [
    ...responsive,
    { name: "widths", testMatch: ["widths/**/*.spec.js"], use: { browserName: "chromium" } },
    { name: "widths-webkit", testMatch: ["widths/date-fields.spec.js"], use: { browserName: "webkit" } },
    { name: "motion", testMatch: ["motion/**/*.spec.js"], use: { browserName: "chromium", viewport: { width: 1280, height: 900 }, reducedMotion: "no-preference" } },
    ...(visualEnabled ? [{ name: "visual", testMatch: ["visual/**/*.spec.js"], use: { browserName: "chromium", viewport: { width: 1440, height: 900 } } }] : []),
  ].sort((a, b) => shardOrder.indexOf(a.name) - shardOrder.indexOf(b.name)),
});
