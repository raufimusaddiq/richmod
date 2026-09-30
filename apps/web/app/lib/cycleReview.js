import { money } from "./format.js";
import { dayLabel } from "./chartData.js";

export function inclusiveEnd(exclusive) {
  if (!exclusive) return "";
  const date = new Date(`${exclusive}T00:00:00Z`);
  if (Number.isNaN(date.getTime())) return "";
  date.setUTCDate(date.getUTCDate() - 1);
  return date.toISOString().slice(0, 10);
}

export function cycleLabel(period) {
  return `${dayLabel(period.start)} – ${period.state === "ACTIVE" ? "berjalan" : dayLabel(inclusiveEnd(period.end))}`;
}

export function amountLabel(value) {
  return value == null ? "—" : money(value);
}

export function signedMoney(value) {
  if (value == null) return "—";
  return `${String(value).startsWith("-") ? "−" : value === "0" ? "" : "+"}${money(String(value).replace(/^-/, ""))}`;
}

const percentage = new Intl.NumberFormat("id-ID", { style: "percent", maximumFractionDigits: 1 });
export function ratioLabel(value) {
  return value == null ? "—" : percentage.format(Number(value));
}

// Only visual length: exact values/order remain server-owned. Equal largest
// magnitudes share the same scale; no semantic importance threshold.
export function changeWidth(item, items) {
  const magnitude = row => Math.abs(Number(row.deltaVsPrevious ?? row.amount));
  const max = Math.max(0, ...items.map(magnitude));
  return max > 0 ? `${magnitude(item) / max * 100}%` : "0%";
}

export function transactionHref(period, filters = {}) {
  const query = new URLSearchParams({ from: period.start, to: inclusiveEnd(period.measuredUntil), status: "CONFIRMED", type: "SPENDING" });
  for (const [key, value] of Object.entries(filters)) if (value) query.set(key, value);
  query.set("cycle", period.start);
  if (period.reviewStep) query.set("review", period.reviewStep);
  return `/transactions?${query}`;
}

export function readReviewSelection(search) {
  const params = new URLSearchParams(search);
  return {
    view: params.get("view") === "calendar" ? "calendar" : "cycle",
    cycle: params.get("cycle") || "",
    range: ["3", "6", "12"].includes(params.get("range")) ? params.get("range") : "6",
    from: params.get("from") || "", to: params.get("to") || "",
    category: params.get("category") || "",
    step: reviewSteps.some(([id]) => id === params.get("review")) ? params.get("review") : "",
  };
}

export function selectionHref(selection) {
  const params = new URLSearchParams({ view: selection.view });
  if (selection.view === "calendar") {
    params.set("range", selection.range);
    if (selection.from && selection.to) { params.set("from", selection.from); params.set("to", selection.to); }
  } else {
    if (selection.cycle) params.set("cycle", selection.cycle);
    if (selection.category) params.set("category", selection.category);
    if (reviewSteps.some(([id]) => id === selection.step)) params.set("review", selection.step);
  }
  return `/analytics?${params}`;
}

export const reviewSteps = [
  ["position", "Posisi siklus"], ["spending-shape", "Pola pengeluaran"],
  ["changes", "Perubahan"], ["drivers", "Bukti pendukung"],
  ["savings-wealth", "Tabungan & kekayaan"], ["quality", "Tindak lanjut"],
  ["discussion", "Pembahasan"], ["decisions", "Keputusan"],
];

export const qualityCopy = {
  OPEN_REVIEWS: ["tinjauan belum selesai", "Buka Inbox"],
  UNCATEGORIZED_EXPENSE: ["pengeluaran belum dikategorikan", "Lihat transaksi"],
  PROCESSING_INCOMPLETE: ["sumber belum selesai diproses", "Buka Inbox"],
  MISSING_SALARY_ANCHOR: ["Belum ada gaji utama terkonfirmasi untuk menentukan siklus", "Buka pengaturan"],
  MISSING_WEALTH_SNAPSHOT: ["Belum ada catatan kekayaan untuk periode ini", "Perbarui Kekayaan"],
  MISSING_PREVIOUS_WEALTH_SNAPSHOT: ["Belum ada catatan kekayaan sebelum siklus ini", "Buka Kekayaan"],
  WEALTH_SNAPSHOT_BEFORE_CYCLE: ["Catatan kekayaan terbaru mendahului siklus ini", "Perbarui Kekayaan"],
  WEALTH_SNAPSHOT_UNCHANGED: ["Belum ada pengamatan kekayaan baru dalam siklus ini", "Perbarui Kekayaan"],
  WEALTH_ACCOUNT_SET_CHANGED: ["Daftar akun kekayaan berubah antar catatan", "Buka Kekayaan"],
};
