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

export async function get(url, signal) {
  const response = await fetch(url, { signal });
  if (!response.ok) {
    const failure = new Error("Tidak dapat memuat data.");
    failure.status = response.status;
    throw failure;
  }
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

// A request that failed must say so: "Memuat…" forever hides a dead endpoint.
export function Pending({ state, retry, children }) {
  if (state?.error) {
    return (
      <div className="notice error feedback" role="alert">
        <span>{state.error}</span>
        {retry && <button className="secondary" onClick={() => retry()}>Coba lagi</button>}
      </div>
    );
  }
  return <Empty>{children}</Empty>;
}

// Loads `url` with a staleness guard. The returned data belongs to the url it
// was fetched for: while another url loads (a different household's drawer) the
// previous url's data is never handed back (keepPrevious opts in to showing it
// while a filter such as a date range reloads in place). Returns [data, reload, state].
// Starting a load clears the page banner; a failure sets it again.
export function useLoad(url, setError, { keepPrevious = false } = {}) {
  const [result, setResult] = useState({ url: null, data: null });
  const [state, setState] = useState({ error: "", loading: true });
  const generation = useRef(0), controller = useRef(null);
  const load = useCallback(async () => {
    controller.current?.abort();
    const own = new AbortController();
    controller.current = own;
    const id = ++generation.current;
    setError("");
    setState({ error: "", loading: true });
    try {
      const data = await get(url, own.signal);
      if (id !== generation.current) return;
      setResult({ url, data });
      setState({ error: "", loading: false });
    } catch (err) {
      if (id !== generation.current || err?.name === "AbortError") return;
      setError(err.message);
      setState({ error: err.message, loading: false });
    }
  }, [url, setError]);
  useEffect(() => {
    load();
    return () => { generation.current += 1; controller.current?.abort(); };
  }, [load]);
  return [keepPrevious || result.url === url ? result.data : null, load, state];
}

// Paged admin list. A filter change starts a new generation: a "load more" that
// was in flight for the old filter is discarded, and a second click while one is
// in flight does nothing. Returns [data, reload, more, state].
export function useAdminList(path, filters, setError) {
  const query = useMemo(() => new URLSearchParams(filters).toString(), [filters]);
  const [data, setData] = useState(null);
  const [state, setState] = useState({ error: "", loading: true, more: false });
  const generation = useRef(0), controller = useRef(null), moreFlight = useRef(false), cursor = useRef("");
  const load = useCallback(async () => {
    controller.current?.abort();
    const own = new AbortController();
    controller.current = own;
    const id = ++generation.current;
    moreFlight.current = false;
    setError("");
    setState({ error: "", loading: true, more: false });
    try {
      const next = await get(`${path}?${query}`, own.signal);
      if (id !== generation.current) return;
      cursor.current = next.nextCursor || "";
      setData(next);
      setState({ error: "", loading: false, more: false });
    } catch (err) {
      if (id !== generation.current || err?.name === "AbortError") return;
      setError(err.message);
      setState({ error: err.message, loading: false, more: false });
    }
  }, [path, query, setError]);
  const more = useCallback(async () => {
    if (!cursor.current || moreFlight.current) return;
    const id = generation.current, signal = controller.current?.signal;
    moreFlight.current = true;
    setError("");
    setState(current => ({ ...current, more: true }));
    try {
      const next = await get(`${path}?${query}&cursor=${encodeURIComponent(cursor.current)}`, signal);
      if (id !== generation.current) return;
      cursor.current = next.nextCursor || "";
      setData(current => ({ items: [...(current?.items || []), ...next.items], nextCursor: next.nextCursor }));
      setState(current => ({ ...current, more: false }));
    } catch (err) {
      if (id !== generation.current || err?.name === "AbortError") return;
      setError(err.message);
      setState(current => ({ ...current, more: false }));
    } finally {
      if (id === generation.current) moreFlight.current = false;
    }
  }, [path, query, setError]);
  useEffect(() => {
    load();
    return () => { generation.current += 1; controller.current?.abort(); };
  }, [load]);
  return [data, load, more, state];
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
