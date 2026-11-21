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

test("the + Catat sheet is a native modal with a focus trap that returns focus to its opener", () => {
  const dialog = read("app/components/QuickRecordDialog.js");
  assert.match(dialog, /showModal\(\)/);
  assert.match(dialog, /trapTarget\(items, document\.activeElement, event\.shiftKey, dialog\)/);
  assert.match(dialog, /onKeyDown=\{trapTab\}/);
  assert.match(dialog, /role="dialog" aria-modal="true" aria-labelledby="quick-record-title"/);
  const provider = read("app/components/QuickRecordProvider.js");
  assert.match(provider, /if \(target\?\.isConnected\) target\.focus\(\)/);
});

test("the Keluarga tablist is a real roving tablist", () => {
  const hub = read("app/household/page.js");
  assert.match(hub, /role="tablist"/);
  assert.match(hub, /nextTabIndex\(event\.key, index, tabs\.length\)/);
  assert.match(hub, /aria-selected=\{tab === key\}/);
  assert.match(hub, /role="tabpanel" id="settings-panel"/);
});

test("settings jump links point at ids that exist", () => {
  const src = read("app/components/SettingsPanels.js");
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
