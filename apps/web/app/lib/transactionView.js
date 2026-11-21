import { actorText, auditActionText, evidenceTypeText } from "./labels.js";
import { dateTime } from "./format.js";

// The ledger asks the API to hide voided rows unless a status filter is set or ?showVoided=1 is on.
// `showVoided` is a page switch, never sent to the API.
export function withVoidedPolicy(search) {
  const query = new URLSearchParams(search);
  const show = query.get("showVoided") === "1";
  query.delete("showVoided");
  if (!query.get("status") && !show) query.set("exclude_voided", "true");
  return `?${query}`;
}

export const hidesVoided = search => { const query = new URLSearchParams(search); return !query.get("status") && query.get("showVoided") !== "1"; };

// Preset ranges of the "Periode" chip. `cycle` is { start, end? } (end exclusive) or null; `today` is a
// Jakarta YYYY-MM-DD. Presets are derived from dates alone, so the server stays the only calendar.
const shiftDay = (day, offset) => { const d = new Date(`${day}T00:00:00Z`); d.setUTCDate(d.getUTCDate() + offset); return d.toISOString().slice(0, 10); };

export function periodRange(value, { cycle, today }) {
  if (value === "cycle" && cycle?.start) return { from: cycle.start, to: today };
  if (value === "7d") return { from: shiftDay(today, -6), to: today };
  if (value === "30d") return { from: shiftDay(today, -29), to: today };
  return { from: "", to: "" };
}

export function periodValue(search, { cycle, today }) {
  const query = new URLSearchParams(search), from = query.get("from") || "", to = query.get("to") || "";
  if (!from && !to) return "all";
  for (const value of ["cycle", "7d", "30d"]) {
    const range = periodRange(value, { cycle, today });
    if (range.from && range.from === from && range.to === to) return value;
  }
  return "custom";
}

const ORIGIN_ACTION = /CREATE|CONFIRM|LINK_/;

// "Asal-usul & bukti": the evidence the transaction rests on and the events that created or confirmed
// it, oldest first, in plain Indonesian. Every other audit entry goes to the collapsed change history.
export function originTimeline(evidence = [], audit = []) {
  const origin = [], changes = [];
  for (const item of evidence) origin.push({ key: `e-${item.id}`, kind: "evidence", title: evidenceTypeText(item.evidenceType), detail: `Bukti diterima · ${dateTime(item.receivedAt)}`, at: item.receivedAt });
  for (const item of audit) {
    const entry = { key: `a-${item.id}`, kind: "event", title: auditActionText(item.action), detail: `${actorText(item.actorType)} · ${dateTime(item.createdAt)}`, at: item.createdAt };
    (ORIGIN_ACTION.test(String(item.action || "").toUpperCase()) ? origin : changes).push(entry);
  }
  origin.sort((a, b) => String(a.at).localeCompare(String(b.at)));
  return { origin, changes };
}
