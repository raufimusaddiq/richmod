// The settings page is split into tabs. Every section id the page has ever used
// still works as an anchor: /settings#<id> opens the tab that holds it.
export const SETTINGS_TABS = [
  ["akun", "Akun & kekayaan"],
  ["kategori", "Kategori & aturan"],
  ["sumber", "Sumber otomatis"],
  ["notifikasi", "Notifikasi"],
];

// [anchor id, tab key]. `wealth-accounts` and `status-pemrosesan` are inner
// anchors other pages link to, not section titles.
export const SETTINGS_SECTIONS = [
  ["rekening-manual", "akun"],
  ["akun-kekayaan", "akun"],
  ["wealth-accounts", "akun"],
  ["sumber-slip-gaji-utama", "akun"],
  ["tujuan-transfer-yang-dikenal", "akun"],
  ["kategori-keluarga", "kategori"],
  ["aturan-kategori-otomatis", "kategori"],
  ["notifikasi-bank", "sumber"],
  ["provider-finansial", "sumber"],
  ["email-telegram-dan-pemrosesan-otomatis", "sumber"],
  ["status-pemrosesan", "sumber"],
  ["notifikasi", "notifikasi"],
];

const TAB_KEYS = new Set(SETTINGS_TABS.map(([key]) => key));
const SECTION_TAB = new Map(SETTINGS_SECTIONS);

// Which tab a location opens. An anchor wins over ?tab=, and an unknown value
// falls back to the first tab.
export function tabFromLocation(search = "", hash = "") {
  const anchor = String(hash).replace(/^#/, "");
  if (SECTION_TAB.has(anchor)) return { tab: SECTION_TAB.get(anchor), anchor };
  const requested = new URLSearchParams(search).get("tab");
  return { tab: TAB_KEYS.has(requested) ? requested : SETTINGS_TABS[0][0], anchor: "" };
}

// The text a list row is searched by: only the fields a family sees.
export function searchText(item) {
  return [item.name, item.displayName, item.normalizedName, item.rawName, item.bankName, item.providerName, item.employer, item.defaultCategoryName, item.institution]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

export function filterItems(items, query) {
  const needle = String(query || "").trim().toLowerCase();
  return needle ? items.filter(item => searchText(item).includes(needle)) : items;
}
