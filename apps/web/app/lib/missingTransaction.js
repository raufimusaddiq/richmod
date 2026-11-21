import { inclusiveEnd } from "./cycleReview.js";

const day = /^\d{4}-\d{2}-\d{2}$/;

export function missingTransactionHref(result) {
  const start = result?.cycleStart || "";
  const end = inclusiveEnd(result?.cycleEnd || "");
  if (!day.test(start) || !day.test(end) || end < start) return "/transactions";
  return `/transactions?${new URLSearchParams({ record: "missing", from: start, to: end })}`;
}

function localMinute(date) {
  const pad = value => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function cycleEntryWindow(search, now = new Date()) {
  const query = new URLSearchParams(search || "");
  const from = query.get("from") || "", to = query.get("to") || "";
  if (query.get("record") !== "missing" || !day.test(from) || !day.test(to) || to < from) return null;
  const min = `${from}T00:00`, max = `${to}T23:59`;
  const current = localMinute(now);
  return { from, to, min, max, value: current >= min && current <= max ? current : `${to}T12:00` };
}

export function withinCycleWindow(value, window) {
  if (!window) return true;
  return typeof value === "string" && value.length >= 16 && value.slice(0, 16) >= window.min && value.slice(0, 16) <= window.max;
}
