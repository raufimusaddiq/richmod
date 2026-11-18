// Settings structure locks: the page is split into deep-linkable tabs, an old
// #anchor opens the tab that holds it, and a long list filters in place.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { SETTINGS_SECTIONS, SETTINGS_TABS, filterItems, tabFromLocation } from "../app/lib/settingsTabs.js";

const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const settings = read("app/settings/page.js");

test("settings is split into a small set of deep-linkable tabs", () => {
  assert.ok(SETTINGS_TABS.length >= 3 && SETTINGS_TABS.length <= 5, `expected a few tabs, found ${SETTINGS_TABS.length}`);
  assert.match(settings, /role="tablist"/);
  assert.match(settings, /role="tab"/);
  assert.match(settings, /aria-selected=\{tab === key\}/);
  assert.match(settings, /aria-current=\{tab === key \? "true" : undefined\}/);
  assert.match(settings, /role="tabpanel" id="settings-panel"/);
  assert.match(settings, /aria-labelledby=\{`settings-tab-\$\{tab\}`\}/);
  // The real roving tablist: arrows/Home/End move selection and focus together.
  assert.match(settings, /nextTabIndex\(event\.key, index, SETTINGS_TABS\.length\)/);
  assert.match(settings, /window\.history\.pushState\(\{\}, "", `\/settings\?tab=\$\{key\}`\)/);
});

test("an existing anchor opens the tab that holds it", () => {
  assert.deepEqual(tabFromLocation("", "#akun-kekayaan"), { tab: "akun", anchor: "akun-kekayaan" });
  assert.deepEqual(tabFromLocation("", "#status-pemrosesan"), { tab: "sumber", anchor: "status-pemrosesan" });
  assert.deepEqual(tabFromLocation("?tab=kategori", ""), { tab: "kategori", anchor: "" });
  assert.deepEqual(tabFromLocation("?tab=nope", ""), { tab: SETTINGS_TABS[0][0], anchor: "" });
  // An anchor wins over a conflicting ?tab=.
  assert.equal(tabFromLocation("?tab=notifikasi", "#kategori-keluarga").tab, "kategori");
  // Every id the page has ever exposed still maps to a tab.
  for (const id of ["rekening-manual", "akun-kekayaan", "wealth-accounts", "sumber-slip-gaji-utama", "tujuan-transfer-yang-dikenal", "notifikasi-bank", "provider-finansial", "kategori-keluarga", "aturan-kategori-otomatis", "email-telegram-dan-pemrosesan-otomatis", "status-pemrosesan", "notifikasi"]) {
    assert.ok(SETTINGS_SECTIONS.some(([section]) => section === id), id);
  }
  assert.match(settings, /window\.addEventListener\("hashchange", apply\)/);
  assert.match(settings, /scrollIntoView\(\{ block: "start" \}\)/);
});

test("long lists filter with a result count and an empty state", () => {
  const items = [{ name: "Belanja rumah" }, { name: "Transportasi" }, { normalizedName: "Pasar Minggu", rawName: "PASAR MINGGU" }];
  assert.deepEqual(filterItems(items, "rumah").map(item => item.name), ["Belanja rumah"]);
  assert.deepEqual(filterItems(items, "pasar").map(item => item.normalizedName), ["Pasar Minggu"]);
  assert.equal(filterItems(items, "  ").length, 3);
  assert.equal(filterItems(items, "tidak ada").length, 0);
  assert.match(settings, /items\.length > 10/);
  assert.match(settings, /type="search"/);
  assert.match(settings, /role="status"/);
  assert.match(settings, /Tidak ada yang cocok dengan pencarian\./);
});

test("the sticky bar is the tab bar and a section title clears it", () => {
  const css = read("app/styles/08-insights-settings-admin.css");
  assert.match(css, /\.settings-tabs-wrap \{ position: sticky; top: 0;/);
  assert.match(css, /\.settings-section \{ scroll-margin-top: 64px; \}/);
  // The destructive row action is a text-style button, not a full-width block.
  assert.match(css, /\.danger-quiet \{ border-color: transparent; background: transparent; color: var\(--danger\);/);
  assert.match(css, /\.settings-list article > button \{ justify-self: start; \}/);
  assert.doesNotMatch(settings, /className=\{item\.active \? "danger" : "secondary"\}/);
});
