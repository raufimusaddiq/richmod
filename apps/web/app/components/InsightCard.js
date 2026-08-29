"use client";

import { insightQuality } from "../lib/insightData";

export default function InsightCard({ insight, loading = false, error = "", unsupported = false, canGenerate = true, onGenerate }) {
  const status = insight?.status;
  const quality = insightQuality(insight);
  const isPending = !error && (loading || status === "PENDING");
  const succeeded = !unsupported && !error && !isPending && status === "SUCCEEDED";
  const compact = !succeeded;

  return <section className={`surface insight-card${compact ? " insight-card-compact" : ""}`} aria-labelledby={succeeded ? "insight-title" : undefined} aria-label={compact ? "Analisis Richmod" : undefined}>
    <div className="section-title insight-header"><div><span className="eyebrow">✦ ANALISIS RICHMOD</span>{succeeded && <h2 id="insight-title">Ringkasan pola keuangan</h2>}</div>{succeeded && <button className="secondary" type="button" onClick={onGenerate}>Perbarui</button>}</div>
    {unsupported && <p className="insight-muted">Analisis Richmod untuk rentang beberapa bulan belum tersedia.</p>}
    {!unsupported && error && <div className="insight-state" role="alert"><div className="insight-state-copy"><p>{error}</p><small>Grafik dan data keuangan tetap tersedia.</small></div>{canGenerate && <button className="secondary" type="button" onClick={onGenerate}>Coba lagi</button>}</div>}
    {!unsupported && isPending && <div className="insight-state" aria-live="polite"><div className="insight-state-copy"><p>Sedang membaca pola keuangan siklus ini…</p></div><button className="secondary" type="button" disabled>Menganalisis…</button></div>}
    {!unsupported && !error && !isPending && status === "FAILED" && <div className="insight-state"><div className="insight-state-copy"><p>Analisis belum berhasil dibuat.</p><small>Grafik dan data keuangan tetap tersedia.</small></div><button className="secondary" type="button" onClick={onGenerate}>Coba lagi</button></div>}
    {!unsupported && !error && !isPending && !status && <div className="insight-state"><div className="insight-state-copy"><p>{canGenerate ? "Belum ada analisis untuk siklus ini." : "Belum ada siklus gaji aktif."}</p><small>{canGenerate ? "Richmod dapat membaca pola dari data keuangan yang sudah terkonfirmasi." : "Pilih sumber gaji utama agar periode analisis dapat ditentukan dengan tepat."}</small></div>{canGenerate && <button className="secondary" type="button" onClick={onGenerate}>Buat analisis</button>}</div>}
    {succeeded && <><div className="insight-text">{String(insight.text || "").split(/\n{2,}/).filter(Boolean).map((paragraph, index) => <p key={index}>{paragraph}</p>)}</div><div className="insight-meta"><span>Kualitas data · {quality.label} · {Math.round(quality.value * 100)}%</span>{insight.completedAt && <span>Diperbarui {formatDate(insight.completedAt)}</span>}</div></>}
  </section>;
}

function formatDate(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
}
