"use client";

import { Children, cloneElement, isValidElement, useCallback, useEffect, useMemo, useRef, useState } from "react";

export const time = (v) =>
  v
    ? new Intl.DateTimeFormat("id-ID", {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(new Date(v))
    : "—";

export const number = (v) => new Intl.NumberFormat("id-ID").format(v || 0);

export const percent = (v) => (v == null ? "—" : `${(v * 100).toFixed(1)}%`);

export const ms = (v) =>
  v == null
    ? "—"
    : v < 1000
      ? `${Math.round(v)} ms`
      : `${(v / 1000).toFixed(1)} dtk`;

export async function get(url) {
  const response = await fetch(url);
  if (!response.ok) throw new Error("Tidak dapat memuat data.");
  return response.json();
}

export function Badge({ value }) {
  return (
    <span className={`admin-badge admin-${String(value || "").toLowerCase()}`}>
      {badgeLabel[value] || value || "—"}
    </span>
  );
}

const badgeLabel = { ACTIVE: "Aktif", INACTIVE: "Nonaktif", DISABLED: "Dinonaktifkan", PENDING: "Menunggu", RUNNING: "Berjalan", SUCCEEDED: "Berhasil", FAILED: "Gagal", HEALTHY: "Sehat", WARN: "Peringatan", ERROR: "Galat" };

export function Metric({ label, value, note }) {
  return (
    <article className="admin-metric">
      <span>{label}</span>
      <b>{value}</b>
      {note && <small>{note}</small>}
    </article>
  );
}

export function Empty({ children }) {
  return <p className="empty compact">{children}</p>;
}

export function useDrawerA11y(close) {
  const ref = useRef(null);
  useEffect(() => {
    ref.current?.focus();
    const onKeyDown = (event) => { if (event.key === "Escape") close(); };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [close]);
  return ref;
}

export function useLoad(url, setError) {
  const [data, setData] = useState(null);
  const load = useCallback(
    () =>
      get(url)
        .then(setData)
        .catch((err) => setError(err.message)),
    [url, setError],
  );
  useEffect(() => {
    load();
  }, [load]);
  return [data, load];
}

export function useAdminList(path, filters, setError) {
  const query = useMemo(() => new URLSearchParams(filters).toString(), [filters]);
  const [data, setData] = useState(null);
  const load = useCallback(() => get(`${path}?${query}`).then(setData).catch((e) => setError(e.message)), [path, query, setError]);
  const more = useCallback(() => {
    if (!data?.nextCursor) return;
    get(`${path}?${query}&cursor=${encodeURIComponent(data.nextCursor)}`).then((next) => setData({ items: [...data.items, ...next.items], nextCursor: next.nextCursor })).catch((e) => setError(e.message));
  }, [data, path, query, setError]);
  useEffect(() => { load(); }, [load]);
  return [data, load, more];
}

export function Table({ headers, children }) {
  const rows = Children.map(children, (row) => {
    if (!isValidElement(row)) return row;
    const cells = Children.map(row.props.children, (cell, index) => {
      if (!isValidElement(cell) || cell.props.colSpan) return cell;
      return cloneElement(cell, { "data-label": headers[index] || "" });
    });
    return cloneElement(row, undefined, cells);
  });
  return (
    <div className="admin-table-wrap">
      <table className="admin-table">
        <thead>
          <tr>
            {headers.map((x) => (
              <th key={x}>{x}</th>
            ))}
          </tr>
        </thead>
        <tbody>{rows}</tbody>
      </table>
    </div>
  );
}
