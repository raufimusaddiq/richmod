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

// Params that select the list. An evidence link carries ?id=, which scopes the
// ledger to that one row on purpose; the list request keeps it.
export function listSearch(search) {
  const query = new URLSearchParams(search);
  if (!query.has("limit")) query.set("limit", "50");
  return `?${query}`;
}

// True when the URL scopes the ledger to a single linked transaction.
export function isScopedToTransaction(search) {
  return Boolean(new URLSearchParams(search).get("id"));
}

// "25.000" (id-ID thousands dots) -> "25000". Only well-formed grouping is
// stripped, so "2.5" or "1.2.3" still fail validation.
export function normalizeAmount(value) {
  const text = String(value || "").trim();
  return /^[1-9]\d{0,2}(\.\d{3})+$/.test(text) ? text.replace(/\./g, "") : text;
}

export function hasActiveFilters(search) {
  const query = new URLSearchParams(search);
  for (const key of query.keys()) if (!["limit", "cursor", "id", "record"].includes(key)) return true;
  return false;
}

// Inline validation for the add form. Returns {field: message}.
export function createFormErrors(values, { cycle = false } = {}) {
  const errors = {};
  if (!/^[1-9][0-9]*$/.test(normalizeAmount(values.amount))) errors.amount = "Isi jumlah dengan angka bulat lebih dari 0.";
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

// ---- Edit existing transaction (PATCH /api/v1/transactions/{id}) ----

// Statuses the API accepts for an edit; NEEDS_REVIEW is resolved through review and VOIDED is final.
export function canEditTransaction(transaction) {
  return Boolean(transaction?.id) && ["PENDING", "CONFIRMED"].includes(transaction.status);
}

// Transfers keep their account and have no category; only text fields move.
export function editableFields(transaction) {
  const transfer = transaction?.type === "TRANSFER";
  return { category: !transfer, account: !transfer, description: true, note: true };
}

// Ingestion may leave description NULL, so it is only required once the user
// changes it (or when the transaction already has one).
export function editFormErrors(values, transaction = {}) {
  const errors = {};
  const description = String(values.description || "").trim();
  if (!description) {
    if (description !== (transaction?.description || "")) errors.description = "Deskripsi wajib diisi.";
  } else if (description.length > 500) errors.description = "Deskripsi maksimal 500 karakter.";
  if (String(values.note || "").trim().length > 2000) errors.note = "Catatan maksimal 2000 karakter.";
  return errors;
}

// Body for PATCH: only fields the household may change, "" -> null to clear.
export function editPayload(transaction, values) {
  const fields = editableFields(transaction), body = {};
  // Only changed fields are sent, so a since-deactivated category/account never
  // blocks a description-only edit.
  if (fields.category && (values.categoryId || null) !== (transaction.categoryId || null)) body.categoryId = values.categoryId || null;
  if (fields.account && (values.accountId || null) !== (transaction.accountId || null)) body.accountId = values.accountId || null;
  const description = String(values.description || "").trim();
  if (description !== (transaction.description || "")) body.description = description;
  const note = String(values.note || "").trim() || null;
  if (note !== (transaction.note || null)) body.note = note;
  return body;
}

// Never show API text: map the stable error code (then the status) to Indonesian.
export function editErrorMessage(status, body = {}) {
  switch (body?.code) {
    case "REVIEW_OPEN": return "Transaksi ini masih menunggu tinjauan. Selesaikan tinjauan dulu.";
    case "STALE_EDIT": return "Transaksi sudah berubah. Muat ulang detail lalu coba lagi.";
    case "TRANSACTION_VOIDED": return "Transaksi yang dibatalkan tidak bisa diubah.";
    case "TRANSFER_FIELD_LOCKED": return "Kategori dan akun transfer tidak bisa diubah.";
    case "INVALID_CATEGORY": return "Kategori tidak valid atau tidak aktif. Pilih kategori lain.";
    case "INVALID_ACCOUNT": return "Akun tidak valid atau tidak aktif. Pilih akun lain.";
    case "NOT_FOUND": return "Transaksi tidak ditemukan.";
    case "LOCKED_FIELD": case "UNKNOWN_FIELD": case "INVALID_FIELD": case "EMPTY_EDIT": case "INVALID_REQUEST":
      return "Perubahan belum valid. Periksa isian lalu coba lagi.";
    default:
  }
  if (status === 401 || status === 403) return "Sesi berakhir atau akses ditolak. Masuk kembali.";
  if (status === 404) return "Transaksi tidak ditemukan.";
  if (status === 409) return "Transaksi tidak bisa diubah saat ini. Muat ulang lalu coba lagi.";
  if (status >= 500) return "Perubahan belum dapat disimpan. Coba lagi.";
  return "Perubahan belum dapat disimpan. Periksa isian lalu coba lagi.";
}

// ---- Void a transaction (POST /api/v1/transactions/{id}/void) ----

export const VOID_CONFIRM_MESSAGE = "Batalkan transaksi ini? Transaksi keluar dari semua total dan tetap tersimpan di riwayat beserta catatan perubahannya.";
export const VOID_SUCCESS_MESSAGE = "Transaksi dibatalkan. Tidak lagi dihitung dalam total, dan tetap ada di riwayat.";

// Same statuses the API voids. NEEDS_REVIEW is resolved in Tinjauan; VOIDED is already final.
export function canVoidTransaction(transaction) {
  return Boolean(transaction?.id) && ["PENDING", "CONFIRMED"].includes(transaction.status);
}

// What the drawer shows after a refused void. Never raw API text. `review` adds the link to
// Tinjauan; `reload` means the record changed under us, so the drawer is fetched again.
export function voidFailure(status, body = {}) {
  const text = String(body?.error || "").toLowerCase();
  if (status === 409 && text.includes("review endpoint")) return { kind: "review", message: "Transaksi ini masih menunggu keputusan, jadi belum bisa dibatalkan di sini. Selesaikan dulu di Tinjauan." };
  if (status === 409 && body?.code === "MERGE_TARGET") return { kind: "error", reload: true, message: "Transaksi ini menjadi tujuan penggabungan yang masih aktif, jadi belum bisa dibatalkan. Pisahkan penggabungannya dulu lewat tombol “Pisahkan penggabungan” di bawah." };
  if (status === 409) return { kind: "error", message: "Status transaksi sudah berubah. Detail dimuat ulang; coba lagi jika tombol masih tersedia.", reload: true };
  if (status === 401 || status === 403) return { kind: "error", message: "Sesi berakhir atau akses ditolak. Masuk kembali." };
  if (status === 404) return { kind: "error", message: "Transaksi tidak ditemukan. Muat ulang daftar transaksi.", reload: true };
  return { kind: "error", message: "Transaksi belum dibatalkan. Coba lagi." };
}

// ---- Reverse a reconciliation merge (POST /api/v1/reconciliation-merges/{id}/reverse) ----

export const REVERSE_MERGE_SUCCESS_MESSAGE = "Penggabungan dipisahkan. Transaksi yang tadi digabung kembali ke Tinjauan untuk diputuskan lagi.";

// One line about an ACTIVE merge from GET /transactions/{id}: TARGET absorbed the other
// transaction, SOURCE was folded into it. `formatAmount` is the page's money formatter.
export function mergeSummary(merge, formatAmount = value => value) {
  const other = [merge?.otherLabel || "Transaksi lain", merge?.otherAmount ? formatAmount(merge.otherAmount) : ""].filter(Boolean).join(" · ");
  return merge?.role === "TARGET" ? `Menyerap transaksi duplikat: ${other}.` : `Digabung ke transaksi lain: ${other}.`;
}

export function reverseMergeConfirmMessage(merge, formatAmount) {
  return `Pisahkan penggabungan ini? ${mergeSummary(merge, formatAmount)} Transaksi yang digabung kembali ke Tinjauan, dan bukti yang tadi disalin dilepas lagi. Tidak ada transaksi yang dihapus.`;
}

export function reverseMergeFailure(status) {
  if (status === 401 || status === 403) return { kind: "error", message: "Sesi berakhir atau akses ditolak. Masuk kembali." };
  if (status === 404) return { kind: "error", message: "Penggabungan ini sudah dipisahkan atau tidak ditemukan. Detail dimuat ulang.", reload: true };
  if (status === 409) return { kind: "error", message: "Transaksi hasil gabungan sudah berubah, jadi penggabungan belum bisa dipisahkan. Detail dimuat ulang.", reload: true };
  return { kind: "error", message: "Penggabungan belum dipisahkan. Coba lagi." };
}
