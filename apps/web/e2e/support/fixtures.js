import { test as base, expect } from "@playwright/test";
import { createApi } from "./api.js";
import { owner } from "./data.js";

export const FIXED_NOW = new Date(process.env.E2E_NOW || "2026-09-07T03:00:00Z");

export const test = base.extend({
  user: [owner(), { option: true }],
  api: [
    async ({ page, user }, use) => {
      const api = createApi(user);
      await api.attach(page);
      await use(api);
    },
    { auto: true },
  ],
  clock: [
    async ({ page }, use) => {
      await page.clock.setFixedTime(FIXED_NOW);
      await use();
    },
    { auto: true },
  ],
  pageErrors: [
    async ({ page }, use) => {
      const errors = [];
      page.on("pageerror", error => errors.push(error.message));
      await use(errors);
      expect(errors, "uncaught page errors").toEqual([]);
    },
    { auto: true },
  ],
});

export { expect };
export { reply } from "./api.js";
