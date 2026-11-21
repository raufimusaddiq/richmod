import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { MEMBER_TAB_KEYS, SETTINGS_SECTIONS, SETTINGS_TABS, filterItems, tabFromLocation, tabsForRole } from "../app/lib/settingsTabs.js";

const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const hub = read("app/household/page.js");
const settings = read("app/components/SettingsPanels.js");

test("Keluarga has exactly the five deep-linkable tabs", () => {
  assert.deepEqual(SETTINGS_TABS.map(([key]) => key), ["anggota", "akun", "kategori", "sumber", "notifikasi"]);
  assert.deepEqual(SETTINGS_TABS.map(([, label]) => label), ["Anggota", "Akun & kekayaan", "Kategori & aturan", "Sumber otomatis", "Notifikasi"]);
  assert.match(hub, /role="tablist"/);
  assert.match(hub, /role="tab"/);
  assert.match(hub, /aria-selected=\{tab === key\}/);
  assert.match(hub, /aria-current=\{tab === key \? "true" : undefined\}/);
  assert.match(hub, /role="tabpanel" id="settings-panel"/);
  assert.match(hub, /aria-labelledby=\{`settings-tab-\$\{tab\}`\}/);
  assert.match(hub, /nextTabIndex\(event\.key, index, tabs\.length\)/);
  assert.match(hub, /window\.history\.pushState\(\{\}, "", `\/household\?tab=\$\{key\}`\)/);
});

test("/settings renders the same hub, so old links and bookmarks keep working", () => {
  const page = read("app/settings/page.js");
  assert.match(page, /export \{ default \} from "\.\.\/household\/page"/);
  assert.match(hub, /window\.addEventListener\("hashchange", apply\)/);
  assert.match(hub, /window\.addEventListener\("popstate", apply\)/);
  assert.match(settings, /scrollIntoView\(\{ block: "start" \}\)/);
});

test("an existing anchor opens the tab that holds it", () => {
  assert.deepEqual(tabFromLocation("", "#akun-kekayaan"), { tab: "akun", anchor: "akun-kekayaan" });
  assert.deepEqual(tabFromLocation("", "#status-pemrosesan"), { tab: "sumber", anchor: "status-pemrosesan" });
  assert.deepEqual(tabFromLocation("", "#tindakan"), { tab: "sumber", anchor: "tindakan" });
  assert.deepEqual(tabFromLocation("?tab=kategori", ""), { tab: "kategori", anchor: "" });
  assert.deepEqual(tabFromLocation("?tab=anggota", ""), { tab: "anggota", anchor: "" });
  assert.deepEqual(tabFromLocation("?tab=nope", ""), { tab: "anggota", anchor: "" });
  assert.deepEqual(tabFromLocation("", ""), { tab: "anggota", anchor: "" });
  assert.equal(tabFromLocation("?tab=notifikasi", "#kategori-keluarga").tab, "kategori");
  for (const id of ["rekening-manual", "akun-kekayaan", "wealth-accounts", "sumber-slip-gaji-utama", "tujuan-transfer-yang-dikenal", "notifikasi-bank", "provider-finansial", "kategori-keluarga", "aturan-kategori-otomatis", "email-telegram-dan-pemrosesan-otomatis", "status-pemrosesan", "notifikasi"]) {
    assert.ok(SETTINGS_SECTIONS.some(([section]) => section === id), id);
  }
});

test("a member sees only Anggota and Notifikasi, and cannot deep-link past that", () => {
  assert.deepEqual(MEMBER_TAB_KEYS, ["anggota", "notifikasi"]);
  assert.deepEqual(tabsForRole(false).map(([key]) => key), ["anggota", "notifikasi"]);
  assert.equal(tabsForRole(true).length, 5);
  const allowed = tabsForRole(false).map(([key]) => key);
  assert.deepEqual(tabFromLocation("?tab=sumber", "", allowed), { tab: "anggota", anchor: "" });
  assert.deepEqual(tabFromLocation("", "#notifikasi-bank", allowed), { tab: "anggota", anchor: "" });
  assert.deepEqual(tabFromLocation("?tab=notifikasi", "", allowed), { tab: "notifikasi", anchor: "" });
  assert.match(settings, /if \(user && owner\) load\(\)/);
  assert.match(settings, /if \(!owner\) return <NotificationsSection/);
});

test("Sumber otomatis carries a pending-setup indicator for the owner", () => {
  assert.match(hub, /key === "sumber" && owner && actionCount > 0/);
  assert.match(hub, /useInboxCounts\(\)/);
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
  assert.match(css, /\.danger-quiet \{ border-color: transparent; background: transparent; color: var\(--danger\);/);
  assert.match(css, /\.settings-list article > button \{ justify-self: start; \}/);
  assert.doesNotMatch(settings, /className=\{item\.active \? "danger" : "secondary"\}/);
});

test("Keluarga Anggota places the member list above the add-member form", () => {
  const listIndex = hub.indexOf("<h2>Anggota</h2>");
  const formIndex = hub.indexOf('id="tambah-anggota"');
  assert.ok(listIndex > 0 && formIndex > 0 && listIndex < formIndex, "member list renders above Tambah anggota");
});
