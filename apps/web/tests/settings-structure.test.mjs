import assert from "node:assert/strict";
import test from "node:test";
import { MEMBER_TAB_KEYS, SETTINGS_SECTIONS, SETTINGS_TABS, filterItems, tabFromLocation, tabsForRole } from "../app/lib/settingsTabs.js";

test("Keluarga has exactly the five deep-linkable tabs", () => {
  assert.deepEqual(SETTINGS_TABS.map(([key]) => key), ["anggota", "akun", "kategori", "sumber", "notifikasi"]);
  assert.deepEqual(SETTINGS_TABS.map(([, label]) => label), ["Anggota", "Akun & kekayaan", "Kategori & aturan", "Sumber otomatis", "Notifikasi"]);
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
});

test("long lists filter with a result count and an empty state", () => {
  const items = [{ name: "Belanja rumah" }, { name: "Transportasi" }, { normalizedName: "Pasar Minggu", rawName: "PASAR MINGGU" }];
  assert.deepEqual(filterItems(items, "rumah").map(item => item.name), ["Belanja rumah"]);
  assert.deepEqual(filterItems(items, "pasar").map(item => item.normalizedName), ["Pasar Minggu"]);
  assert.equal(filterItems(items, "  ").length, 3);
  assert.equal(filterItems(items, "tidak ada").length, 0);
});
