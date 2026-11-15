// The one place a salary cycle is turned into a label. The API stores a cycle's
// `end` as exclusive (the next cycle's first day), so a closed cycle is shown up
// to the day before it and the raw end is never printed. An active cycle has no
// end yet and reads "berjalan".
const dayFormat = new Intl.DateTimeFormat("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", year: "numeric" });

export function formatDay(value) {
  if (!value) return "";
  const date = new Date(`${value}T00:00:00+07:00`);
  return Number.isNaN(date.getTime()) ? "" : dayFormat.format(date);
}

export function inclusiveEnd(exclusive) {
  if (!exclusive) return "";
  const date = new Date(`${exclusive}T00:00:00Z`);
  if (Number.isNaN(date.getTime())) return "";
  date.setUTCDate(date.getUTCDate() - 1);
  return date.toISOString().slice(0, 10);
}

// `active` wins over `end`: a running cycle may carry a provisional end.
export function cycleRangeLabel({ start, end, active = false }) {
  const from = formatDay(start);
  if (!from) return "—";
  if (active || !end) return `${from} – berjalan`;
  return `${from} – ${formatDay(inclusiveEnd(end)) || "—"}`;
}

// Eyebrow for a spending window. `end` is inclusive here (already the last day).
// `kind` and `cycles` are what the server actually resolved, not what was asked
// for. The window is always spelled out so the donut never silently answers a
// different question than the cycle-scoped page around it.
export function windowLabel({ kind, cycles, start, end }) {
  const label = kind === "SALARY_CYCLES" && cycles > 0 ? `${cycles} siklus gaji` : "3 bulan kalender";
  const from = formatDay(start);
  const to = formatDay(end);
  return from && to ? `${label} · ${from} – ${to}` : label;
}
