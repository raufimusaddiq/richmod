"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import { ErrorNotice, Toast } from "../components/Feedback";
import useAuth from "../components/useAuth";
import { dateTime, percent } from "../lib/format";

export default function DocumentsPage() {
  const user = useAuth(); const [items, setItems] = useState([]); const [selected, setSelected] = useState(null); const [extractions, setExtractions] = useState([]); const [working, setWorking] = useState(false); const [error, setError] = useState(""); const [toast,setToast]=useState("");
  const load = useCallback(async () => { const response = await fetch("/api/v1/documents"); if (response.ok) setItems(await response.json()); else setError("Dokumen belum dapat dimuat."); }, []);
  useEffect(() => { if (user) load(); }, [user, load]);
  async function upload(event) { event.preventDefault(); setWorking(true); setError(""); const response = await fetch("/api/v1/documents", { method: "POST", body: new FormData(event.currentTarget) }); if (!response.ok) { const body = await response.json().catch(() => ({})); setError(body.error || "Dokumen belum dapat diunggah."); } else { event.currentTarget.reset(); await load(); setToast("Dokumen diterima dan masuk pipeline klasifikasi."); } setWorking(false); }
  async function open(item) { setSelected(item); const response = await fetch(`/api/v1/documents/${item.id}/extraction`); if (response.ok) setExtractions(await response.json()); }
  if (!user) return <main className="loading">Memuat…</main>;
  return <AppShell user={user} eyebrow="DOKUMEN" title="Bukti keuangan" actions={<span className="header-meta">JPEG / PNG · maksimal 10 MB per gambar · maksimal 10 gambar</span>}>
    <section className="surface upload-card"><div><span className="eyebrow">PEMROSESAN OTOMATIS</span><h2>Unggah bukti keuangan</h2><p>Beberapa gambar dalam satu kiriman diproses sebagai satu dokumen berurutan.</p></div><form onSubmit={upload}><input name="file" type="file" multiple accept="image/jpeg,image/png,.jpg,.jpeg,.png" required/><button disabled={working}>{working ? "Memproses…" : "Unggah & kenali"}</button></form></section>
    <ErrorNotice message={error} retry={load}/>
    <section className="document-grid">{items.map(item => <button key={item.id} className="document-card" onClick={() => open(item)}><img src={`/api/v1/documents/${item.id}/content`} alt="Pratinjau dokumen keuangan"/><div><span className={`status status-${item.status?.toLowerCase()}`}>{item.status}</span><h2>{item.documentType || "Sedang diklasifikasikan"}</h2><p>{item.sourceType} · {dateTime(item.createdAt)}</p>{summaryText(item.summary) && <p className="document-summary">{summaryText(item.summary)}</p>}<small>{item.confidence ? `Keyakinan ${percent(item.confidence)}` : "Keyakinan belum tersedia"} · {item.linkedTransactionIds?.length || 0} transaksi terhubung</small>{item.needsReview && <b className="needs-review">Perlu Ditinjau</b>}</div></button>)}{!items.length && <div className="empty-state"><span>▤</span><h2>Belum ada dokumen</h2><p>Unggah gambar keuangan pertama dari web atau Telegram.</p></div>}</section>
    {selected && <div className="drawer-backdrop" onClick={() => setSelected(null)}><aside className="detail-drawer document-detail" role="dialog" aria-modal="true" aria-label="Detail dokumen" onClick={event => event.stopPropagation()}><button className="drawer-close" aria-label="Tutup detail" onClick={() => setSelected(null)}>×</button><img src={`/api/v1/documents/${selected.id}/content`} alt="Dokumen keuangan"/><span className="eyebrow">{selected.documentType || "DOKUMEN"}</span><h2>Detail ekstraksi</h2>{selected.documentType === "BILL_OR_INVOICE" && <p className="notice">Tagihan atau invoice belum menjadi bukti pembayaran. Catat pembayaran hanya setelah ada bukti transaksi.</p>}{extractions.map(item => <article className="extraction" key={`${item.stage}-${item.schemaVersion}`}><div><b>{item.stage}</b><span>{item.validated ? "Tervalidasi" : "Belum tervalidasi"}</span></div><pre>{JSON.stringify(item.output, null, 2)}</pre></article>)}{!extractions.length && <p className="empty compact">Ekstraksi masih diproses atau belum tersedia.</p>}<h3>Transaksi terhubung</h3>{selected.linkedTransactionIds?.map(id => <Link key={id} href={`/transactions?id=${id}`}>Buka transaksi {id.slice(0, 8)}… →</Link>)}</aside></div>}
    <Toast message={toast} onClose={() => setToast("")}/>
  </AppShell>;
}

function summaryText(summary) {
  if (!summary || typeof summary !== "object") return "";
  return [summary.merchant, summary.employer, summary.period, summary.total, summary.net_pay, summary.amount].filter(Boolean).slice(0, 3).join(" · ");
}
