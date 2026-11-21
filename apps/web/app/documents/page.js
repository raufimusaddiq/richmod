"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import AppShell from "../components/AppShell";
import { ErrorNotice, Toast } from "../components/Feedback";
import useAuth from "../components/useAuth";
import useDrawerA11y from "../components/useDrawerA11y";
import { dateTime, money, percent } from "../lib/format";
import { documentTypeLabels, label } from "../lib/labels";
import { mergeFreshDocuments } from "../lib/documentPaging";
import { LOAD, LoadError, useDrawerRecord, useGuardedLoad } from "../lib/useLoadState";

const POLL_MS = 5000, MAX_POLLS = 60;

export default function DocumentsPage() {
  const user = useAuth();
  const [items, setItems] = useState([]), [visibleCount, setVisibleCount] = useState(12), [page, setPage] = useState(0), [working, setWorking] = useState(false), [error, setError] = useState(""), [toast, setToast] = useState(""), [fileInfo, setFileInfo] = useState(""), [processing, setProcessing] = useState(false), [pollStopped, setPollStopped] = useState(false), [filter, setFilter] = useState("ALL"), [retrying, setRetrying] = useState("");
  const polls = useRef(0);
  const [nextCursor, setNextCursor] = useState(""), [loadingMore, setLoadingMore] = useState(false);
  const olderLoaded = useRef(false);
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
  const loader = useGuardedLoad(async ({ signal, current }) => {
    const response = await fetch("/api/v1/documents", { signal });
    if (!response.ok) throw new LoadError("Dokumen belum dapat dimuat.");
    const list = await response.json();
    if (!current()) return;
    const next = Array.isArray(list) ? list : [];
    const cursor = response.headers.get("X-Next-Cursor") || "";
    if (olderLoaded.current) {
      setItems(current => mergeFreshDocuments(next, current));
    } else { setItems(next); setNextCursor(cursor); }
    setProcessing(next.some(item => item.status === "PENDING" || item.status === "PROCESSING" || item.reprocessing));
  }, { fallback: "Dokumen belum dapat dimuat. Periksa koneksi lalu coba lagi." });
  const load = loader.run;
  useEffect(() => { if (user) load(); }, [user, load]);
  useEffect(() => {
    if (!processing || loader.status !== LOAD.READY) return undefined;
    if (polls.current >= MAX_POLLS) { setPollStopped(true); return undefined; }
    const timer = window.setTimeout(() => { polls.current += 1; load(); }, POLL_MS);
    return () => window.clearTimeout(timer);
  }, [processing, loader.status, load]);
  async function loadOlder() {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true); setError("");
    try {
      const response = await fetch(`/api/v1/documents?cursor=${encodeURIComponent(nextCursor)}`);
      if (!response.ok) throw new Error();
      const older = await response.json();
      olderLoaded.current = true;
      setItems(current => [...current, ...(Array.isArray(older) ? older.filter(item => !current.some(existing => existing.id === item.id)) : [])]);
      setNextCursor(response.headers.get("X-Next-Cursor") || "");
    } catch { setError("Dokumen yang lebih lama belum dapat dimuat. Coba lagi."); }
    finally { setLoadingMore(false); }
  }
  function reloadList() { polls.current = 0; setPollStopped(false); load(); }
  async function upload(event) {
    event.preventDefault(); const formEl = event.currentTarget; const files = formEl.elements.file.files; const total = [...files].reduce((sum, file) => sum + file.size, 0);
    if (files.length > 10 || total > 10 * 1024 * 1024) { setError("Pilih maksimal 10 gambar dengan ukuran total maksimal 10 MB."); return; }
    if (working) return;
    setWorking(true); setError("");
    try {
      const response = await fetch("/api/v1/documents", { method: "POST", body: new FormData(formEl) });
      if (!response.ok) { const body = await response.json().catch(() => ({})); setError(body.error || "Dokumen belum dapat diunggah."); return; }
      formEl.reset(); setFileInfo(""); setToast("Dokumen diterima dan masuk tahap klasifikasi.");
      polls.current = 0; setPollStopped(false);
      await load();
    } catch { setError("Koneksi terputus saat mengunggah. Coba lagi."); }
    finally { setWorking(false); }
  }
  async function retry(item) {
    if (retrying) return;
    setRetrying(item.id); setError("");
    try {
      const response = await fetch(`/api/v1/documents/${item.id}/reprocess`, { method: "POST" });
      if (!response.ok) { const body = await response.json().catch(() => ({})); setError(response.status === 409 ? "Hanya dokumen yang gagal yang dapat diproses ulang." : body.error || "Dokumen belum dapat diproses ulang."); return; }
      setToast("Dokumen dijadwalkan untuk diproses ulang.");
      polls.current = 0; setPollStopped(false);
      await load();
      if (drawer.id === item.id) await drawer.open(item.id, drawer.data);
    } catch { setError("Koneksi terputus. Coba proses ulang lagi."); }
    finally { setRetrying(""); }
  }
  const visibleItems = items.filter(item => matchesFilter(item, filter));
  const moreLocal = Math.max(0, visibleItems.length - visibleCount);
  const canLoadOlder = Boolean(nextCursor);
  const shownCount = Math.min(visibleCount, visibleItems.length);
  const filterText = FILTERS.find(([key]) => key === filter)?.[1] || "";
  const moreNote = filter === "ALL"
    ? `Menampilkan ${shownCount} dari ${items.length} dokumen termuat.${canLoadOlder ? " Ada dokumen yang lebih lama." : ""}`
    : `Menampilkan ${shownCount} dari ${visibleItems.length} dokumen "${filterText}" di antara ${items.length} dokumen termuat.${canLoadOlder ? " Muat lebih banyak untuk memeriksa dokumen yang lebih lama." : ""}`;
  function showMore() { if (moreLocal > 0) setVisibleCount(count => count + 12); else loadOlder(); }
  function open(item) { setPage(0); drawer.open(item.id, null); }
  const closeToast = useCallback(() => setToast(""), []);

  if (!user) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  return <AppShell user={user} eyebrow="Dokumen" title="Bukti keuangan">
    <section className="surface upload-card"><div><span className="eyebrow">Pemrosesan otomatis</span><h2>Unggah bukti keuangan</h2><p>Beberapa gambar dalam satu kiriman diproses sebagai satu dokumen berurutan.</p></div><form onSubmit={upload}><label className="file-picker"><span>Pilih gambar</span><span className="file-picker-button" aria-hidden="true">{fileInfo ? "Ganti gambar" : "Pilih berkas"}</span><input name="file" type="file" multiple accept="image/jpeg,image/png,.jpg,.jpeg,.png" required onChange={event=>{const files=[...event.target.files];const total=files.reduce((sum,file)=>sum+file.size,0);setFileInfo(files.length?`${files.length} gambar · ${(total/1024/1024).toFixed(1)} MB total`:"")}}/></label><small>JPG atau PNG · maksimal 10 MB total · maksimal 10 gambar</small><small aria-live="polite">{fileInfo || "Belum ada gambar dipilih"}</small><button disabled={working || !fileInfo}>{working ? "Memproses…" : "Unggah & kenali"}</button></form></section>
    <ErrorNotice message={error || loader.error} retry={error ? undefined : reloadList}/>{pollStopped && <p className="notice" role="status">Status dokumen berhenti diperbarui otomatis. <button type="button" className="secondary" onClick={reloadList}>Muat ulang</button></p>}
    <div className="filter-chips" role="group" aria-label="Filter status dokumen">{FILTERS.map(([key, text]) => <button key={key} type="button" className={filter === key ? "chip active" : "chip secondary"} aria-pressed={filter === key} onClick={() => { setFilter(key); setVisibleCount(12); }}>{text}</button>)}</div>
    <section className="document-grid">{visibleItems.slice(0, visibleCount).map(item => <button key={item.id} className="document-card" onClick={() => open(item)}><img src={`/api/v1/documents/${item.id}/thumbnail`} alt="Pratinjau dokumen keuangan" loading="lazy" decoding="async"/><div><span className={`status status-${item.status?.toLowerCase()}`}>{statusLabel(item.status)}</span><h2 title={documentTitle(item)}>{documentTitle(item)}</h2><p>{documentTypeText(item)} · {dateTime(item.createdAt)}</p>{item.status === "FAILED" ? <p className="document-summary">{item.reprocessing ? "Sedang diproses ulang…" : "Dokumen belum berhasil dibaca."}</p> : summaryText(item.summary) && <p className="document-summary">{summaryText(item.summary)}</p>}<small>{item.status !== "FAILED" && item.confidence ? `Keyakinan ekstraksi ${percent(item.confidence)} · ` : ""}{item.linkedTransactionIds?.length || 0} transaksi terhubung</small>{item.needsReview && item.status !== "NEEDS_REVIEW" && <b className="needs-review">Perlu ditinjau</b>}</div></button>)}{!visibleItems.length && <div className="empty-state"><span aria-hidden="true">▤</span><h2>{items.length ? (canLoadOlder ? "Belum ada pada dokumen yang termuat" : "Tidak ada dokumen pada filter ini") : "Belum ada dokumen"}</h2><p>{items.length ? (canLoadOlder ? `Belum ada dokumen "${filterText}" di antara ${items.length} dokumen termuat. Muat lebih banyak untuk memeriksa dokumen yang lebih lama.` : "Pilih filter lain untuk melihat dokumen.") : "Unggah gambar keuangan pertama dari web atau Telegram."}</p></div>}</section>
    {(moreLocal > 0 || canLoadOlder) && <div className="list-pagination document-more"><p className="muted" role="status">{moreNote}</p><button type="button" className="secondary" disabled={loadingMore} onClick={showMore}>{loadingMore ? "Memuat…" : "Tampilkan lebih banyak"}</button></div>}
    {selected && <div className="drawer-backdrop" onClick={closeDetail}><aside ref={drawerRef} tabIndex={-1} className="detail-drawer document-detail" role="dialog" aria-modal="true" aria-label="Detail dokumen" onClick={event => event.stopPropagation()}><button className="drawer-close" aria-label="Tutup detail" onClick={closeDetail}>×</button><img src={`/api/v1/documents/${selected.id}/pages/${page}/content`} alt={`Dokumen keuangan halaman ${page+1}`}/>{pages.length>1&&<div className="page-thumbnails" aria-label="Halaman dokumen">{pages.map(item=><button key={item.index} aria-current={page===item.index} onClick={()=>setPage(item.index)}><img src={`/api/v1/documents/${selected.id}/pages/${item.index}/content`} alt={`Halaman ${item.index+1}`}/><span>{item.index+1}</span></button>)}</div>}<span className="eyebrow">{documentTypeLabel(selected.documentType) || "Dokumen"}</span><h2>Ringkasan ekstraksi</h2>{selected.status === "FAILED" && <div className="notice" role="status"><p>Ekstraksi dokumen ini gagal. Klasifikasi di bawah hanya menunjukkan jenis dokumen, bukan hasil baca yang berhasil.</p><button type="button" disabled={Boolean(retrying) || selected.reprocessing} onClick={() => retry(selected)}>{selected.reprocessing || retrying === selected.id ? "Sedang diproses ulang…" : "Proses ulang"}</button></div>}{selected.documentType === "BILL_OR_INVOICE" && <p className="notice">Status pembayaran ditentukan hanya dari bukti yang menyatakan lunas, berhasil, atau selesai. Tagihan yang belum jelas tidak dicatat sebagai pengeluaran.</p>}{drawer.status === "loading" && <p role="status">Memuat ekstraksi…</p>}{drawer.status === "error" && <div role="alert"><p>{drawer.error}</p><button type="button" onClick={drawer.retry}>Coba lagi</button></div>}{extractions.map(item => <article className="extraction" key={`${item.stage}-${item.schemaVersion}`}><div><b>{stageLabel(item.stage)}</b><span>{item.validated ? (selected.status === "FAILED" ? "Jenis dokumen dikenali" : "Diterima") : "Ditolak / perlu ditinjau"}</span></div><p>{summaryText(item.output, selected.status === "FAILED") || summaryText(selected.summary, selected.status === "FAILED") || "Tidak ada ringkasan yang dapat ditampilkan."}</p><details><summary>Detail teknis</summary><pre tabIndex={0} role="region" aria-label="Hasil ekstraksi, geser untuk semua isi">{JSON.stringify(item.output, null, 2)}</pre></details></article>)}{drawer.status === "ready" && !extractions.length && <p className="empty compact">Ekstraksi masih diproses atau belum tersedia.</p>}<h3>Transaksi terhubung</h3>{selected.linkedTransactionIds?.map(id => <Link key={id} href={`/transactions?id=${id}`}>Buka transaksi {id.slice(0, 8)}… →</Link>)}</aside></div>}
    <Toast message={toast} onClose={closeToast}/>
  </AppShell>;
}

const FILTERS = [["ALL", "Semua"], ["FAILED", "Gagal"], ["REVIEW", "Perlu ditinjau"], ["OK", "Berhasil"]];
function matchesFilter(item, filter) {
  if (filter === "FAILED") return item.status === "FAILED";
  if (filter === "REVIEW") return item.status !== "FAILED" && Boolean(item.needsReview);
  if (filter === "OK") return item.status !== "FAILED" && !item.needsReview && !["PENDING", "PROCESSING", "RECEIVED"].includes(item.status);
  return true;
}
function documentTitle(item) {
  const summary = item.status !== "FAILED" && item.summary && typeof item.summary === "object" ? item.summary : null;
  const rows = Array.isArray(summary?.transactions) ? summary.transactions : [];
  const row = rows[0] || summary;
  const who = row?.merchant || summary?.employer;
  const value = row?.total ?? row?.net_pay ?? row?.amount ?? summary?.net_pay;
  const amount = value !== undefined && value !== null && /^\d+(\.\d+)?$/.test(String(value)) ? money(value) : "";
  const parts = [];
  if (who) parts.push(who);
  if (amount) parts.push(amount);
  if (rows.length > 1) parts.push(`${rows.length} transaksi`);
  if (!parts.length) parts.push(`${sourceLabel(item.sourceType)} ${dateTime(item.createdAt)}`);
  return parts.join(" · ");
}
function documentTypeText(item) {
  return documentTypeLabel(item.documentType) || (item.status === "FAILED" ? "Gagal dikenali" : "Sedang diklasifikasikan");
}
function stageLabel(stage) { return ({ CLASSIFICATION: "Klasifikasi", classification: "Klasifikasi", EXTRACTION: "Ekstraksi", extract: "Ekstraksi" }[stage]) || documentTypeLabel(stage) || stage; }

function summaryText(summary, hidePayment = false) {
  if (!summary || typeof summary !== "object") return "";
  const rows = Array.isArray(summary.transactions) ? summary.transactions : [];
  const row = rows[0] || summary;
  const status = hidePayment ? "" : summary.payment_status === "PAID" ? "Lunas" : summary.payment_status === "UNPAID" ? "Belum Lunas" : summary.payment_status === "UNKNOWN" ? "Status pembayaran belum jelas" : "";
  const rupiah = value => value !== undefined && value !== null && /^\d+(\.\d+)?$/.test(String(value)) ? money(value) : value;
  return [row.merchant || summary.merchant, summary.employer, summary.period, rupiah(row.total ?? summary.total), rupiah(summary.net_pay), rupiah(row.amount ?? summary.amount), rows.length > 1 && `${rows.length} transaksi`, status, summary.due_date && `Jatuh tempo ${summary.due_date}`].filter(Boolean).slice(0, 3).join(" · ");
}

function statusLabel(value) { return ({ PENDING: "Menunggu", PROCESSING: "Diproses", SUCCEEDED: "Berhasil", EXTRACTED: "Terbaca", NEEDS_REVIEW: "Perlu ditinjau", RECEIVED: "Diterima", CLASSIFIED: "Terklasifikasi", FAILED: "Gagal" }[value] || value || "—"); }
function sourceLabel(value) { return ({ WEB_UPLOAD: "Unggahan web", WEB_IMAGE: "Unggahan web", TELEGRAM_IMAGE: "Gambar Telegram", TELEGRAM_TEXT: "Pesan Telegram", EMAIL: "Email" }[value] || value || "—"); }
function documentTypeLabel(value) { return label(documentTypeLabels, value); }
