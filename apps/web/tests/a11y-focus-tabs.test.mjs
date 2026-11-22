import test from "node:test";
import assert from "node:assert/strict";
import { trapTarget, nextTabIndex } from "../app/lib/focusTrap.js";
import { SETTINGS_SECTIONS } from "../app/lib/settingsTabs.js";

test("trapTarget wraps Tab and Shift+Tab inside the container", () => {
  const items = ["a", "b", "c"];
  assert.equal(trapTarget(items, "c", false), "a");
  assert.equal(trapTarget(items, "a", true), "c");
  assert.equal(trapTarget(items, "b", false), null);
  assert.equal(trapTarget(items, "b", true), null);
  assert.equal(trapTarget(items, "outside", false), "a");
  assert.equal(trapTarget(items, "outside", true), "c");
  assert.equal(trapTarget([], "x", false, "box"), "box");
});

test("nextTabIndex implements the tablist arrow/Home/End keys", () => {
  assert.equal(nextTabIndex("ArrowRight", 3, 4), 0);
  assert.equal(nextTabIndex("ArrowLeft", 0, 4), 3);
  assert.equal(nextTabIndex("Home", 2, 4), 0);
  assert.equal(nextTabIndex("End", 0, 4), 3);
  assert.equal(nextTabIndex("a", 0, 4), null);
});

test("settings jump links point at ids that exist", () => {
  const links = SETTINGS_SECTIONS.map(([id]) => id);
  assert.ok(links.includes("akun-kekayaan") && links.includes("status-pemrosesan"));
  assert.ok(!links.includes("akun-wealth"));
});
