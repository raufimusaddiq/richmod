const integer = new Intl.NumberFormat("id-ID");
const oneDecimal = new Intl.NumberFormat("id-ID", { minimumFractionDigits: 1, maximumFractionDigits: 1 });

export const percent = (v) => (v == null ? "—" : `${oneDecimal.format(v * 100)}%`);

export const ms = (v) =>
  v == null
    ? "—"
    : v < 1000
      ? `${integer.format(Math.round(v))} ms`
      : `${oneDecimal.format(v / 1000)} dtk`;

export function duration(milliseconds) {
  if (milliseconds == null || !Number.isFinite(Number(milliseconds))) return "—";
  const total = Math.max(0, Math.round(Number(milliseconds) / 1000));
  if (total < 60) return `${total} dtk`;
  const days = Math.floor(total / 86400);
  const hours = Math.floor((total % 86400) / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  if (days > 0) return `${days} h${hours ? ` ${hours} j` : ""}`;
  if (hours > 0) return `${hours} j${minutes ? ` ${minutes} m` : ""}`;
  return `${minutes} m${seconds ? ` ${seconds} dtk` : ""}`;
}
