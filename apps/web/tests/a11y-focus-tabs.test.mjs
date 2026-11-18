import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { trapTarget, nextTabIndex } from "../app/lib/focusTrap.js";
import { SETTINGS_SECTIONS } from "../app/lib/settingsTabs.js";

const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

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

test("mobile sheet uses the focus trap and returns focus to the trigger", () => {
  const shell = read("app/components/AppShell.js");
  assert.match(shell, /trapKeyDown\(event, morePanel\.current/);
  assert.match(shell, /ref=\{morePanel\}/);
  assert.match(shell, /moreButton\.current\?\.focus\(\)/);
});

test("settings jump links point at ids that exist", () => {
  const src = read("app/settings/page.js");
  const links = SETTINGS_SECTIONS.map(([id]) => id);
  assert.ok(links.includes("akun-kekayaan") && links.includes("status-pemrosesan"));
  assert.ok(!links.includes("akun-wealth"));
  assert.match(src, /title="Akun kekayaan"/);
  assert.match(src, /id="status-pemrosesan"/);
});

test("inbox tabs, admin tabs and document chips expose state", () => {
  const inbox = read("app/inbox/page.js");
  assert.match(inbox, /role="tablist"/);
  assert.match(inbox, /aria-controls="inbox-panel"/);
  assert.match(inbox, /role="tabpanel" id="inbox-panel"/);
  assert.match(inbox, /event.key === "ArrowRight"/);
  assert.match(inbox, /event.key === "Home"/);
  const admin = read("app/admin/page.js");
  assert.match(admin, /role="tablist"/);
  assert.match(admin, /role="tab"/);
  assert.match(admin, /aria-selected=\{tab === key\}/);
  assert.match(admin, /aria-controls="admin-panel"/);
  assert.match(admin, /role="tabpanel" id="admin-panel"/);
  assert.match(admin, /nextTabIndex\(event\.key, index, tabs\.length\)/);
  assert.match(read("app/documents/page.js"), /aria-pressed=\{filter === key\}/);
});
