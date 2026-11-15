// Pure helpers for the /transactions page, kept out of the component so they
// can be unit-tested without a browser.

const ISO_DATE = /^\d{4}-\d{2}-\d{2}$/;

// Indonesian message when the range is inverted, else "". Same-day is valid.
export function dateRangeError(from, to) {
  if (!ISO_DATE.test(from || "") || !ISO_DATE.test(to || "")) return "";
  return from > to ? "Tanggal “Dari” tidak boleh setelah tanggal “Sampai”." : "";
}

// Never show API text to the household: map known statuses to Indonesian copy.
export function transactionsErrorMessage(status, body = {}) {
  const text = String(body?.error || "").toLowerCase();
  if (text.includes("from date must not be after to date")) return "Tanggal “Dari” tidak boleh setelah tanggal “Sampai”.";
  if (status === 400 || status === 422) return "Filter tidak valid. Periksa kembali filter lalu coba lagi.";
  if (status === 401 || status === 403) return "Sesi berakhir atau akses ditolak. Masuk kembali.";
  return "Transaksi belum dapat dimuat. Coba lagi.";
}

export function createErrorMessage(status, body = {}) {
  if (status >= 500) return "Transaksi belum dapat disimpan. Coba lagi.";
  if (status === 401 || status === 403) return "Sesi berakhir atau akses ditolak. Masuk kembali.";
  return "Transaksi belum dapat disimpan. Periksa isian lalu coba lagi.";
}

// Params that select the list. `id` only opens the drawer, so it must not narrow
// the list to one row; the window's other params stay untouched.
export function listSearch(search) {
  const query = new URLSearchParams(search);
  query.delete("id");
  if (!query.has("limit")) query.set("limit", "50");
  return `?${query}`;
}

// Everything that narrows the list is cleared, including the cycle hand-off.
export function clearedFilterSearch() { return ""; }

export function hasActiveFilters(search) {
  const query = new URLSearchParams(search);
  for (const key of query.keys()) if (!["limit", "cursor", "id", "record"].includes(key)) return true;
  return false;
}

// Inline validation for the add form. Returns {field: message}.
export function createFormErrors(values, { cycle = false } = {}) {
  const errors = {};
  if (!/^[1-9][0-9]*$/.test(String(values.amount || "").trim())) errors.amount = "Isi jumlah dengan angka bulat lebih dari 0.";
  if (!String(values.description || "").trim()) errors.description = "Deskripsi wajib diisi.";
  if (values.type === "TRANSFER" && !values.accountId) errors.accountId = "Pilih akun sumber.";
  if (values.type === "TRANSFER" && values.purpose && values.purpose !== "INTERNAL_TRANSFER" && !values.relatedWealthAccountId) errors.relatedWealthAccountId = "Pilih akun kekayaan.";
  if (cycle && !values.transactionAt) errors.transactionAt = "Waktu wajib diisi.";
  return errors;
}

// Tab cycling for a modal. `items` are the focusable elements in order, `active`
// the focused one. Returns the element to focus, or null to let the browser move.
export function trapTarget(items, active, shift, container = null) {
  if (!items.length) return container;
  const first = items[0], last = items[items.length - 1];
  if (shift && (active === first || active === container || !items.includes(active))) return last;
  if (!shift && (active === last || !items.includes(active))) return first;
  return null;
}
