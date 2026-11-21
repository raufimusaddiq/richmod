import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { screenForPath, trackEvent, trackScreenView } from "../app/lib/telemetry.js";

test("routes map only to bounded screen keys, never paths or ids", () => {
  assert.equal(screenForPath("/"), "home");
  assert.equal(screenForPath("/analytics"), "analytics");
  assert.equal(screenForPath("/transactions/123e4567?x=1"), "transactions");
  assert.equal(screenForPath("/settings/anything"), "settings");
  assert.equal(screenForPath("/admin/users"), "admin");
  assert.equal(screenForPath("/login"), null);
  assert.equal(screenForPath("/home"), null);
  assert.equal(screenForPath(undefined), null);
});

test("trackEvent posts only the allow-listed pair and swallows every failure", async () => {
  const sent = [];
  const original = globalThis.fetch;
  try {
    globalThis.fetch = (url, init) => { sent.push([url, JSON.parse(init.body)]); return Promise.reject(new Error("offline")); };
    trackEvent("analytics");
    trackEvent("nope");
    trackEvent("home", "EXFILTRATE");
    trackScreenView("/login");
    trackScreenView("/wealth");
    await new Promise(resolve => setTimeout(resolve, 0));
    assert.deepEqual(sent, [
      ["/api/v1/telemetry/events", { screen: "analytics", action: "SCREEN_VIEW" }],
      ["/api/v1/telemetry/events", { screen: "wealth", action: "SCREEN_VIEW" }],
    ]);
    globalThis.fetch = () => { throw new Error("sync failure"); };
    assert.doesNotThrow(() => trackEvent("home"));
  } finally {
    globalThis.fetch = original;
  }
});

test("AppShell reports a screen view on route change", () => {
  const shell = readFileSync(new URL("../app/components/AppShell.js", import.meta.url), "utf8");
  assert.match(shell, /trackScreenView\(pathname\)/);
});
