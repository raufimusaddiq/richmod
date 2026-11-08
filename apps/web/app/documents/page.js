"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import { ErrorNotice, Toast } from "../components/Feedback";
import useAuth from "../components/useAuth";
import useDrawerA11y from "../components/useDrawerA11y";
import { dateTime, percent } from "../lib/format";
import { documentTypeLabels, label } from "../lib/labels";
import { LoadError, useDrawerRecord, useGuardedLoad } from "../lib/useLoadState";

export default function DocumentsPage() {
  const user = useAuth();
  const [items, setItems] = useState([]), [visibleCount, setVisibleCount] = useState(12), [page, setPage] = useState(0), [working, setWorking] = useState(false), [error, setError] = useState(""), [toast, setToast] = useState(""), [fileInfo, setFileInfo] = useState("");
  // The drawer is {closed | loading | error | ready}: "still processing" is only
  // true for a ready drawer with no extraction rows, never for a pending or failed request.
  const drawer = useDrawerRecord(async (id, signal) => {
    const [extractionResponse, pageResponse] = await Promise.all([fetch(`/api/v1/documents/${id}/extraction`, { signal }), fetch(`/api/v1/documents/${id}/pages`, { signal })]);
    if (!extractionResponse.ok) throw new LoadError("Detail dokumen belum dapat dimuat. Coba lagi.");
    const extractions = await extractionResponse.json();
    const pages = pageResponse.ok ? await pageResponse.json() : [];
    return { extractions: Array.isArray(extractions) ? extractions : [], pages: Array.isArray(pages) ? pages : [] };
  }, "Detail dokumen belum dapat dimuat. Coba lagi.");
  const selected = drawer.status === "closed" ? null : (items.find(item => item.id === drawer.id) || { id: drawer.id });
  const extractions = drawer.data?.extractions || [], pages = drawer.data?.pages || [];
  const closeDetail = drawer.close;
  const drawerRef = useDrawerA11y(drawer.status !== "closed", closeDetail);
  // Reloading the list after an upload keeps the grid mounted (refresh, not skeleton).
  const loader = useGuardedLoad(async ({ signal, current }) => {
    const response = await fetch("/api/v1/documents", { signal });
    if (!response.ok) throw new LoadError("Dokumen belum dapat dimuat.");
    const list = await response.json();
    if (current()) setItems(Array.isArray(list) ? list : []);
  }, { fallback: "Dokumen belum dapat dimuat. Periksa koneksi lalu coba lagi." });
  const load = loader.run;
  useEffect(() => { if (user) load(); }, [user, load]);
  // An upload is classified in the background; poll the list while any document is
  // still pending so its status moves without a manual reload.
  const processing = items.some(item => item.status === "PENDING" || item.status === "PROCESSING");
  useEffect(() => {
    if (!processing) return undefined;
    const timer = window.setInterval(() => load(), 5000);
    return () => window.clearInterval(timer);
  }, [processing, load]);
  async function upload(event) {
    event.preventDefault(); const formEl = event.currentTarget; const files = formEl.elements.file.files; const total = [...files].reduce((sum, file) => sum + file.size, 0);
    if (files.length > 10 || total > 10 * 1024 * 1024) { setError("Pilih maksimal 10 gambar dengan ukuran total maksimal 10 MB."); return; }
    if (working) return;
    setWorking(true); setError("");
    try {
      const response = await fetch("/api/v1/documents", { method: "POST", body: new FormData(formEl) });
      if (!response.ok) { const body = await response.json().catch(() => ({})); setError(body.error || "Dokumen belum dapat diunggah."); return; }
      formEl.reset(); setFileInfo(""); setToast("Dokumen diterima dan masuk tahap klasifikasi.");
      await load();
    } catch { setError("Koneksi terputus saat mengunggah. Coba lagi."); }
    finally { setWorking(false); }
  }
  function open(item) { setPage(0); drawer.open(item.id, null); }
  const closeToast = useCallback(() => setToast(""), []);

  if (!user) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  return <AppShell user={user} eyebrow="DOKUMEN" title="Bukti keuangan" actions={<span className="header-meta">JPG atau PNG · maksimal 10 MB total · maksimal 10 gambar</span>}>
    <section className="surface upload-card"><div><span className="eyebrow">PEMROSESAN OTOMATIS</span><h2>Unggah bukti keuangan</h2><p>Beberapa gambar dalam satu kiriman diproses sebagai satu dokumen berurutan.</p></div><form onSubmit={upload}><label>Pilih gambar<input name="file" type="file" multiple accept="image/jpeg,image/png,.jpg,.jpeg,.png" required onChange={event=>{const files=[...event.target.files];const total=files.reduce((sum,file)=>sum+file.size,0);setFileInfo(files.length?`${files.length} gambar · ${(total/1024/1024).toFixed(1)} MB total`:"")}}/></label>{fileInfo&&<small aria-live="polite">{fileInfo}</small>}<button disabled={working}>{working ? "Memproses…" : "Unggah & kenali"}</button></form></section>
    <ErrorNotice message={error || loader.error} retry={error ? undefined : () => load()}/>
    <section className="document-grid">{items.slice(0, visibleCount).map(item => <button key={item.id} className="document-card" onClick={() => open(item)}><img src={`/api/v1/documents/${item.id}/content`} alt="Pratinjau dokumen keuangan" loading="lazy" decoding="async"/><div><span className={`status status-${item.status?.toLowerCase()}`}>{statusLabel(item.status)}</span><h2>{documentTypeLabel(item.documentType) || "Sedang diklasifikasikan"}</h2><p>{sourceLabel(item.sourceType)} · {dateTime(item.createdAt)}</p>{summaryText(item.summary) && <p className="document-summary">{summaryText(item.summary)}</p>}<small>{item.confidence ? `Tingkat keyakinan ${percent(item.confidence)}` : "Tingkat keyakinan belum tersedia"} · {item.linkedTransactionIds?.length || 0} transaksi terhubung</small>{item.needsReview && <b className="needs-review">Perlu ditinjau</b>}</div></button>)}{!items.length && <div className="empty-state"><span>▤</span><h2>Belum ada dokumen</h2><p>Unggah gambar keuangan pertama dari web atau Telegram.</p></div>}</section>
    {items.length > visibleCount && <div className="list-pagination document-more"><button type="button" className="secondary" onClick={() => setVisibleCount(count => count + 12)}>Tampilkan lebih banyak ({items.length - visibleCount} lagi)</button></div>}
    {selected && <div className="drawer-backdrop" onClick={closeDetail}><aside ref={drawerRef} tabIndex={-1} className="detail-drawer document-detail" role="dialog" aria-modal="true" aria-label="Detail dokumen" onClick={event => event.stopPropagation()}><button className="drawer-close" aria-label="Tutup detail" onClick={closeDetail}>×</button><img src={`/api/v1/documents/${selected.id}/pages/${page}/content`} alt={`Dokumen keuangan halaman ${page+1}`}/>{pages.length>1&&<div className="page-thumbnails" aria-label="Halaman dokumen">{pages.map(item=><button key={item.index} aria-current={page===item.index} onClick={()=>setPage(item.index)}><img src={`/api/v1/documents/${selected.id}/pages/${item.index}/content`} alt={`Halaman ${item.index+1}`}/><span>{item.index+1}</span></button>)}</div>}<span className="eyebrow">{documentTypeLabel(selected.documentType) || "DOKUMEN"}</span><h2>Ringkasan ekstraksi</h2>{selected.documentType === "BILL_OR_INVOICE" && <p className="notice">Status pembayaran ditentukan hanya dari bukti yang menyatakan lunas, berhasil, atau selesai. Tagihan yang belum jelas tidak dicatat sebagai pengeluaran.</p>}{drawer.status === "loading" && <p role="status">Memuat ekstraksi…</p>}{drawer.status === "error" && <div role="alert"><p>{drawer.error}</p><button type="button" onClick={drawer.retry}>Coba lagi</button></div>}{extractions.map(item => <article className="extraction" key={`${item.stage}-${item.schemaVersion}`}><div><b>{documentTypeLabel(item.stage) || item.stage}</b><span>{item.validated ? "Diterima" : "Ditolak / perlu ditinjau"}</span></div><p>{summaryText(item.output)||"Tidak ada ringkasan yang dapat ditampilkan."}</p><details><summary>Detail teknis</summary><pre tabIndex={0} role="region" aria-label="Hasil ekstraksi, geser untuk semua isi">{JSON.stringify(item.output, null, 2)}</pre></details></article>)}{drawer.status === "ready" && !extractions.length && <p className="empty compact">Ekstraksi masih diproses atau belum tersedia.</p>}<h3>Transaksi terhubung</h3>{selected.linkedTransactionIds?.map(id => <Link key={id} href={`/transactions?id=${id}`}>Buka transaksi {id.slice(0, 8)}… →</Link>)}</aside></div>}
    <Toast message={toast} onClose={closeToast}/>
  </AppShell>;
}

function summaryText(summary) {
  if (!summary || typeof summary !== "object") return "";
  const status = summary.payment_status === "PAID" ? "Lunas" : summary.payment_status === "UNPAID" ? "Belum Lunas" : summary.payment_status === "UNKNOWN" ? "Status pembayaran belum jelas" : "";
  return [summary.merchant, summary.employer, summary.period, summary.total, summary.net_pay, summary.amount, status, summary.due_date && `Jatuh tempo ${summary.due_date}`].filter(Boolean).slice(0, 3).join(" · ");
}

function statusLabel(value) { return ({ PENDING: "Menunggu", PROCESSING: "Diproses", SUCCEEDED: "Berhasil", EXTRACTED: "Terbaca", FAILED: "Gagal" }[value] || value || "—"); }
function sourceLabel(value) { return ({ WEB_UPLOAD: "Unggahan web", WEB_IMAGE: "Unggahan web", TELEGRAM_IMAGE: "Gambar Telegram", TELEGRAM_TEXT: "Pesan Telegram", EMAIL: "Email" }[value] || value || "—"); }
function documentTypeLabel(value) { return label(documentTypeLabels, value); }
