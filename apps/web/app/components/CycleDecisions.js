"use client";

import { useEffect, useRef, useState } from "react";
import { dayLabel } from "../lib/chartData";
import { dateTime } from "../lib/format";
import { decisionCount } from "../lib/cycleSections";
import { ErrorNotice } from "./Feedback";
import useDialogs from "./useDialogs";

const LIMIT = 2000;

export default function CycleDecisions({ cycleStart, closed, body, onBodyChange, range = "", onSummary }) {
  const { confirm, dialogs } = useDialogs();
  const [data, setData] = useState(null);
  const [error, setError] = useState("");
  const [saveError, setSaveError] = useState("");
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [reload, setReload] = useState(0);
  const field = useRef(null);
  const pending = useRef(false);
  const mounted = useRef(true);
  const summaryRef = useRef(onSummary);
  summaryRef.current = onSummary;
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => {
    const controller = new AbortController();
    setData(null); setError("");
    summaryRef.current?.({ status: "loading", count: 0 });
    fetch(`/api/v1/analytics/cycle-decisions?${new URLSearchParams({ cycle_start: cycleStart })}`, { signal: controller.signal, cache: "no-store" })
      .then(async response => {
        if (!response.ok) throw new Error();
        const result = await response.json();
        if (!Array.isArray(result?.items) || !Array.isArray(result?.previous)) throw new Error();
        if (!controller.signal.aborted) { setData(result); summaryRef.current?.({ status: "ready", count: result.items.length }); }
      }).catch(err => { if (err.name !== "AbortError") { setError("Keputusan belum dapat dimuat. Data keuangan tetap tersedia."); summaryRef.current?.({ status: "error", count: 0 }); } });
    return () => controller.abort();
  }, [cycleStart, reload]);

  async function save(event) {
    event.preventDefault();
    if (pending.current) return;
    const trimmed = body.trim();
    if (!trimmed || decisionCount(trimmed) > LIMIT) {
      setSaveError("Tulis keputusan sepanjang 1–2.000 karakter."); field.current?.focus(); return;
    }
    pending.current = true; setBusy(true); setSaveError(""); setStatus("");
    try {
      const response = await fetch("/api/v1/analytics/cycle-decisions", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ cycleStart, body: trimmed }) });
      if (!response.ok) throw new Error(response.status === 409 ? "Siklus belum ditutup. Keputusan tidak disimpan." : "Keputusan belum dapat disimpan. Periksa daftar sebelum mencoba lagi.");
      await response.json();
      onBodyChange("");
      if (mounted.current) { setStatus("Keputusan disimpan."); setReload(value => value + 1); }
    } catch (err) {
      if (mounted.current) { setSaveError(err.message.startsWith("Keputusan ") || err.message.startsWith("Siklus ") ? err.message : "Koneksi terputus. Periksa daftar sebelum mencoba lagi."); field.current?.focus(); }
    } finally { pending.current = false; if (mounted.current) setBusy(false); }
  }

  async function revoke(id) {
    if (pending.current || !(await confirm("Batalkan keputusan ini? Catatan asli tetap tersimpan dalam riwayat audit.", { confirmLabel: "Ya, batalkan", danger: true }))) return;
    pending.current = true; setBusy(true); setSaveError(""); setStatus("");
    try {
      const response = await fetch(`/api/v1/analytics/cycle-decisions/${encodeURIComponent(id)}/revoke`, { method: "POST" });
      if (!response.ok) throw new Error();
      if (mounted.current) { setStatus("Keputusan dibatalkan. Riwayat tetap tersimpan."); setReload(value => value + 1); }
    } catch { if (mounted.current) setSaveError("Keputusan belum dapat dibatalkan. Muat ulang daftar sebelum mencoba lagi."); }
    finally { pending.current = false; if (mounted.current) setBusy(false); }
  }

  const count = decisionCount(body);
  return <section id="decisions" className="family-talk-part" aria-labelledby="decisions-title">
    {dialogs}
    <h3 id="decisions-title" tabIndex={-1}>Keputusan rumah tangga</h3>
    <p className="review-description">Ditulis dan disimpan anggota rumah tangga. Bukan transaksi, perubahan saldo, atau kesimpulan model.</p>
    <ErrorNotice message={error} retry={() => setReload(value => value + 1)}/>
    {!data && !error && <p role="status">Memuat keputusan…</p>}
    {data && <div className="decision-columns">
      <div className="decision-column">
        <h4>Siklus ini{range ? ` · ${range}` : ""}</h4>
        <DecisionList items={data.items} revoke={revoke} busy={busy}/>
      </div>
      <div className="decision-column decision-previous">
        <h4>Siklus sebelumnya{data.previousCycleStart ? ` · ${dayLabel(data.previousCycleStart)}` : ""}</h4>
        <DecisionList items={data.previous}/>
        <p className="review-description">Konteks yang dicatat keluarga, bukan bukti bahwa keputusan menyebabkan perubahan keuangan.</p>
      </div>
    </div>}
    {closed ? <form className="decision-form" onSubmit={save} aria-busy={busy}>
      <label htmlFor="decision-body">Keputusan baru untuk siklus ini</label>
      <textarea ref={field} id="decision-body" name="body" rows={4} value={body} disabled={busy} aria-describedby={`decision-hint${saveError ? " decision-error" : ""}`} aria-invalid={Boolean(saveError)} onChange={event => { onBodyChange(event.target.value); setSaveError(""); setStatus(""); }} onKeyDown={event => { if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) event.currentTarget.form.requestSubmit(); }}/>
      <p id="decision-hint" className="review-description">Perubahan dibuat sebagai catatan baru; catatan lama dapat dibatalkan dan tetap ada di riwayat audit.</p>
      <div className="decision-form-foot"><button type="submit" disabled={busy}>{busy ? "Menyimpan…" : "Simpan keputusan"}</button><span className="decision-count" aria-live="polite">{count} / 2.000</span></div>
    </form> : <p className="decision-closed">Keputusan dapat disimpan setelah siklus ditutup. Catatan siklus sebelumnya tetap dapat dibaca.</p>}
    {saveError && <p id="decision-error" className="notice error" role="alert">{saveError}</p>}
    <p role="status" aria-live="polite">{status || (body ? "Draf belum disimpan." : "")}</p>
  </section>;
}

function DecisionList({ items, revoke, busy }) {
  if (!items.length) return <p className="empty compact">Belum ada keputusan tercatat.</p>;
  return <ul className="decision-list">{items.map((item, index) => <li key={item.id}>
    <span className="decision-avatar" data-tint={index % 4} aria-hidden="true">{String(item.author || "?").trim().charAt(0).toUpperCase()}</span>
    <div>
      <p>{item.body}</p>
      <small>{item.author} · {dateTime(item.createdAt)}</small>
      {revoke && <button type="button" className="decision-revoke" disabled={busy} onClick={() => revoke(item.id)}>Batalkan keputusan</button>}
    </div>
  </li>)}</ul>;
}
