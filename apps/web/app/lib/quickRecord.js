// Pure helpers for the global "+ Catat" sheet, kept out of the component so
// they can be unit-tested without a browser.

// Live Indonesian thousands grouping while typing: "60000" -> "60.000".
// Anything that is not a digit is dropped; leading zeros never survive.
export function formatRupiahInput(value) {
  const digits = String(value ?? "").replace(/\D/g, "").replace(/^0+/, "");
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, ".");
}

// "60.000" -> "60000", the whole-IDR string the API expects.
export function rupiahDigits(value) {
  return String(value ?? "").replace(/\D/g, "").replace(/^0+/, "");
}

// The caret position after reformatting: keep it after the same number of digits.
export function caretAfterFormat(formatted, digitsBeforeCaret) {
  if (digitsBeforeCaret <= 0) return 0;
  let seen = 0;
  for (let index = 0; index < formatted.length; index += 1) {
    if (/\d/.test(formatted[index])) seen += 1;
    if (seen === digitsBeforeCaret) return index + 1;
  }
  return formatted.length;
}

// Same key the API uses for descriptionMap: lower-case, whitespace collapsed.
export function normalizeDescription(value) {
  return String(value ?? "").toLowerCase().split(/\s+/).filter(Boolean).join(" ");
}

// The category a description suggests. An exact match wins; otherwise the typed
// text may be the start of a known description (3+ characters). The category must
// still be active. Returns a category id or "".
export function suggestCategory(descriptionMap, text, activeIds) {
  const typed = normalizeDescription(text);
  if (!typed || !descriptionMap) return "";
  const allowed = activeIds instanceof Set ? activeIds : new Set(activeIds || []);
  const exact = descriptionMap[typed];
  if (exact && allowed.has(exact)) return exact;
  if (typed.length < 3) return "";
  for (const key of Object.keys(descriptionMap)) {
    if (key.startsWith(typed) && allowed.has(descriptionMap[key])) return descriptionMap[key];
  }
  return "";
}

// The 1-tap chips: the household's recent categories that are still active, then
// the rest stay behind "Lainnya…". Never more than `limit`.
export function categoryChips(recentIds, categories, limit = 6) {
  const byId = new Map((categories || []).filter(item => item.active !== false).map(item => [item.id, item]));
  const chips = [];
  for (const id of recentIds || []) {
    if (byId.has(id) && !chips.some(item => item.id === id)) chips.push(byId.get(id));
    if (chips.length === limit) break;
  }
  return chips;
}

// A "Ulangi cepat" chip label.
export function repeatLabel(item, formatAmount = value => value) {
  return `${item.description} · ${formatAmount(item.amount)}`;
}

// Only usable entries survive: a suggestion for an inactive category or account
// has that field cleared, never the whole chip.
export function sanitizeSuggestions(raw, { categories = [], accounts = [] } = {}) {
  const activeCategories = new Set(categories.filter(item => item.active !== false).map(item => item.id));
  const activeAccounts = new Set(accounts.filter(item => item.active !== false).map(item => item.id));
  const data = raw && typeof raw === "object" ? raw : {};
  const frequent = (Array.isArray(data.frequent) ? data.frequent : [])
    .filter(item => item && item.description && /^[1-9]\d*$/.test(String(item.amount)) && (item.type === "EXPENSE" || item.type === "INCOME"))
    .slice(0, 5)
    .map(item => ({ ...item, categoryId: activeCategories.has(item.categoryId) ? item.categoryId : null, accountId: activeAccounts.has(item.accountId) ? item.accountId : null }));
  return {
    frequent,
    recentCategories: (Array.isArray(data.recentCategories) ? data.recentCategories : []).filter(id => activeCategories.has(id)).slice(0, 6),
    descriptionMap: data.descriptionMap && typeof data.descriptionMap === "object" ? data.descriptionMap : {},
  };
}

// The payload POSTed to /api/v1/transactions from the sheet's form state.
export function buildQuickPayload(form) {
  const body = { type: form.type, amount: rupiahDigits(form.amount), description: String(form.description || "").trim() };
  if (form.accountId) body.accountId = form.accountId;
  if (form.type === "TRANSFER") {
    body.purpose = form.purpose || "INTERNAL_TRANSFER";
    if (body.purpose !== "INTERNAL_TRANSFER" && form.relatedWealthAccountId) body.relatedWealthAccountId = form.relatedWealthAccountId;
  } else if (form.categoryId) body.categoryId = form.categoryId;
  if (form.note) body.note = form.note;
  if (form.transactionAt) body.transactionAt = new Date(form.transactionAt).toISOString();
  return body;
}

export function quickFormErrors(form, { cycle = false } = {}) {
  const errors = {};
  if (!/^[1-9][0-9]*$/.test(rupiahDigits(form.amount))) errors.amount = "Isi jumlah dengan angka lebih dari 0.";
  if (!String(form.description || "").trim()) errors.description = "Deskripsi wajib diisi.";
  if (form.type === "TRANSFER" && !form.accountId) errors.accountId = "Pilih akun sumber.";
  if (form.type === "TRANSFER" && form.purpose && form.purpose !== "INTERNAL_TRANSFER" && !form.relatedWealthAccountId) errors.relatedWealthAccountId = "Pilih akun kekayaan.";
  if (cycle && !form.transactionAt) errors.transactionAt = "Waktu wajib diisi.";
  return errors;
}

export const QUICK_EVENTS = { open: "richmod:open-quick-record", created: "richmod:transaction-created" };

export const MAX_PHOTOS = 10, MAX_PHOTO_BYTES = 10 * 1024 * 1024;

// "" when the picked files are acceptable, else the Indonesian reason.
export function photoSelectionError(files) {
  const list = [...(files || [])];
  if (!list.length) return "Pilih minimal satu gambar.";
  if (list.some(file => !/^image\/(jpeg|png)$/.test(file.type) && !/\.(jpe?g|png)$/i.test(file.name || ""))) return "Hanya gambar JPG atau PNG.";
  if (list.length > MAX_PHOTOS || list.reduce((sum, file) => sum + file.size, 0) > MAX_PHOTO_BYTES) return "Pilih maksimal 10 gambar dengan ukuran total maksimal 10 MB.";
  return "";
}

// The recent-upload status line. Only FAILED can be retried.
export function photoStatus(item) {
  if (item?.reprocessing || ["PENDING", "PROCESSING", "RECEIVED"].includes(item?.status)) return { text: "Sedang diproses…", tone: "pending", retry: false };
  if (item?.status === "FAILED") return { text: "Gagal diproses", tone: "failed", retry: true };
  return { text: "Selesai", tone: "done", retry: false };
}
