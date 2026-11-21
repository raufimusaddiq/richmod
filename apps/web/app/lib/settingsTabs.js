export const SETTINGS_TABS = [
  ["anggota", "Anggota"],
  ["akun", "Akun & kekayaan"],
  ["kategori", "Kategori & aturan"],
  ["sumber", "Sumber otomatis"],
  ["notifikasi", "Notifikasi"],
];

export const MEMBER_TAB_KEYS = ["anggota", "notifikasi"];

export function tabsForRole(owner) {
  return owner ? SETTINGS_TABS : SETTINGS_TABS.filter(([key]) => MEMBER_TAB_KEYS.includes(key));
}

export const SETTINGS_SECTIONS = [
  ["tambah-anggota", "anggota"],
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
  ["tindakan", "sumber"],
  ["email-masuk-terakhir", "sumber"],
  ["notifikasi", "notifikasi"],
];

const SECTION_TAB = new Map(SETTINGS_SECTIONS);

export function tabFromLocation(search = "", hash = "", allowed = SETTINGS_TABS.map(([key]) => key)) {
  const keys = new Set(allowed);
  const first = allowed[0];
  const anchor = String(hash).replace(/^#/, "");
  if (SECTION_TAB.has(anchor) && keys.has(SECTION_TAB.get(anchor))) return { tab: SECTION_TAB.get(anchor), anchor };
  const requested = new URLSearchParams(search).get("tab");
  return { tab: keys.has(requested) ? requested : first, anchor: "" };
}

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
