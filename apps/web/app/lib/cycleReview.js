import { minusSign, money } from "./format.js";
import { dayLabel } from "./chartData.js";

import { inclusiveEnd, cycleRangeLabel } from "./cycleDates.js";

export { inclusiveEnd };

export function cycleLabel(period) {
  return cycleRangeLabel({ start: period.start, end: period.end, active: period.state === "ACTIVE" });
}

export function measuredLabel(period) {
  return period ? `${dayLabel(period.start)} – ${dayLabel(inclusiveEnd(period.measuredUntil))}` : "—";
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
  return value == null ? "—" : minusSign(percentage.format(Number(value)));
}

export function transactionHref(period, filters = {}) {
  const query = new URLSearchParams({ from: period.start, to: inclusiveEnd(period.measuredUntil), status: "CONFIRMED", type: "SPENDING" });
  for (const [key, value] of Object.entries(filters)) if (value) query.set(key, value);
  query.set("cycle", period.start);
  if (period.reviewStep) query.set("review", period.reviewStep);
  return `/transactions?${query}`;
}

export function readReviewSelection(search, defaultView = "cycle") {
  const params = new URLSearchParams(search);
  const requested = params.get("view");
  return {
    view: ["cycle", "calendar", "wealth"].includes(requested) ? requested : defaultView,
    cycle: params.get("cycle") || "",
    range: ["3", "6", "12"].includes(params.get("range")) ? params.get("range") : "6",
    from: params.get("from") || "", to: params.get("to") || "",
    category: params.get("category") || "",
    step: reviewSteps.some(([id]) => id === params.get("review")) ? params.get("review") : "",
  };
}

export function selectionHref(selection) {
  const params = new URLSearchParams({ view: selection.view });
  if (selection.view === "wealth") return `/analytics?${params}`;
  if (selection.view === "calendar") {
    params.set("range", selection.range);
    if (selection.from && selection.to) { params.set("from", selection.from); params.set("to", selection.to); }
    if (selection.cycle) params.set("cycle", selection.cycle);
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
  ["quality", "Tindak lanjut"],
  ["discussion", "Pembahasan"], ["decisions", "Keputusan"],
];

export const qualityCopy = {
  OPEN_REVIEWS: ["tinjauan belum selesai", "Buka Inbox", true],
  UNCATEGORIZED_EXPENSE: ["pengeluaran belum dikategorikan", "Lihat transaksi", true],
  PROCESSING_INCOMPLETE: ["sumber belum selesai diproses", "Buka Tindakan", true],
  MISSING_SALARY_ANCHOR: ["Belum ada gaji utama terkonfirmasi untuk menentukan siklus", "Buka pengaturan", true],
  MISSING_WEALTH_SNAPSHOT: ["Belum ada catatan kekayaan untuk periode ini", "Perbarui Kekayaan", false],
  MISSING_PREVIOUS_WEALTH_SNAPSHOT: ["Belum ada catatan kekayaan sebelum siklus ini", "Buka Kekayaan", false],
  WEALTH_SNAPSHOT_BEFORE_CYCLE: ["Belum ada pengamatan kekayaan di siklus ini", "Perbarui Kekayaan", false],
  WEALTH_ACCOUNT_SET_CHANGED: ["Daftar akun kekayaan berubah antar catatan", "Buka Kekayaan", false],
};

export function isBlockingQuality(kind) {
  const entry = qualityCopy[kind];
  return entry ? entry[2] !== false : true;
}

export function splitDataQuality(items = []) {
  const blocking = [];
  const advisory = [];
  for (const item of items || []) {
    if (isBlockingQuality(item.kind)) blocking.push(item);
    else advisory.push(item);
  }
  return { blocking, advisory };
}
