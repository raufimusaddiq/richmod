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

export function cycleRangeLabel({ start, end, active = false }) {
  const from = formatDay(start);
  if (!from) return "—";
  if (active || !end) return `${from} – berjalan`;
  return `${from} – ${formatDay(inclusiveEnd(end)) || "—"}`;
}

export function windowLabel({ kind, cycles, start, end }) {
  const label = kind === "SALARY_CYCLES" && cycles > 0 ? `${cycles} siklus gaji` : "3 bulan kalender";
  const from = formatDay(start);
  const to = formatDay(end);
  return from && to ? `${label} · ${from} – ${to}` : label;
}
