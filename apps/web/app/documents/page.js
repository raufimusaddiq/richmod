"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import useAuth from "../components/useAuth";
import { dateTime, percent } from "../lib/format";

export default function DocumentsPage() {
  const user = useAuth(); const [items, setItems] = useState([]); const [selected, setSelected] = useState(null); const [extractions, setExtractions] = useState([]); const [working, setWorking] = useState(false); const [error, setError] = useState("");
  const load = useCallback(async () => { const response = await fetch("/api/v1/documents"); if (response.ok) setItems(await response.json()); else setError("Dokumen belum dapat dimuat."); }, []);
  useEffect(() => { if (user) load(); }, [user, load]);
  async function upload(event) { event.preventDefault(); setWorking(true); setError(""); const response = await fetch("/api/v1/documents", { method: "POST", body: new FormData(event.currentTarget) }); if (!response.ok) { const body = await response.json().catch(() => ({})); setError(body.error || "Dokumen belum dapat diunggah."); } else { event.currentTarget.reset(); await load(); } setWorking(false); }
  async function open(item) { setSelected(item); const response = await fetch(`/api/v1/documents/${item.id}/extraction`); if (response.ok) setExtractions(await response.json()); }
  if (!user) return <main className="loading">Memuat…</main>;
  return <AppShell user={user} eyebrow="DOCUMENTS" title="Bukti keuangan" actions={<span className="header-meta">JPEG / PNG · maks. 10 MB</span>}>
    <section className="surface upload-card"><div><span className="eyebrow">GENERIC PIPELINE</span><h2>Unggah bukti, sistem akan mengenalinya</h2><p>Payslip, receipt, screenshot bank/e-wallet, transfer proof, invoice, atau riwayat transaksi.</p></div><form onSubmit={upload}><input name="file" type="file" accept="image/jpeg,image/png,.jpg,.jpeg,.png" required/><button disabled={working}>{working ? "Memproses…" : "Unggah & klasifikasikan"}</button></form></section>
    {error && <p className="notice error">{error}</p>}
    <section className="document-grid">{items.map(item => <button key={item.id} className="document-card" onClick={() => open(item)}><img src={`/api/v1/documents/${item.id}/content`} alt="Pratinjau dokumen keuangan"/><div><span className={`status status-${item.status?.toLowerCase()}`}>{item.status}</span><h2>{item.documentType || "Sedang diklasifikasikan"}</h2><p>{item.sourceType} · {dateTime(item.createdAt)}</p>{summaryText(item.summary) && <p className="document-summary">{summaryText(item.summary)}</p>}<small>{item.confidence ? `Confidence ${percent(item.confidence)}` : "Confidence belum tersedia"} · {item.linkedTransactionIds?.length || 0} transaksi terhubung</small>{item.needsReview && <b className="needs-review">Perlu review</b>}</div></button>)}{!items.length && <div className="empty-state"><span>▤</span><h2>Belum ada dokumen</h2><p>Unggah gambar keuangan pertama dari web atau Telegram.</p></div>}</section>
    {selected && <div className="drawer-backdrop" onClick={() => setSelected(null)}><aside className="detail-drawer document-detail" onClick={event => event.stopPropagation()}><button className="drawer-close" onClick={() => setSelected(null)}>×</button><img src={`/api/v1/documents/${selected.id}/content`} alt="Dokumen keuangan"/><span className="eyebrow">{selected.documentType || "DOCUMENT"}</span><h2>Extraction detail</h2>{extractions.map(item => <article className="extraction" key={`${item.stage}-${item.schemaVersion}`}><div><b>{item.stage}</b><span>{item.validated ? "Tervalidasi" : "Belum tervalidasi"}</span></div><pre>{JSON.stringify(item.output, null, 2)}</pre></article>)}{!extractions.length && <p className="empty compact">Ekstraksi masih diproses atau belum tersedia.</p>}<h3>Linked transactions</h3>{selected.linkedTransactionIds?.map(id => <Link key={id} href={`/transactions?id=${id}`}>Buka transaksi {id.slice(0, 8)}… →</Link>)}</aside></div>}
  </AppShell>;
}

function summaryText(summary) {
  if (!summary || typeof summary !== "object") return "";
  return [summary.merchant, summary.employer, summary.period, summary.total, summary.net_pay, summary.amount].filter(Boolean).slice(0, 3).join(" · ");
}
