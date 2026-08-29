"use client";

import { insightQuality } from "../lib/insightData";

export default function InsightCard({ insight, loading = false, error = "", unsupported = false, canGenerate = true, onGenerate }) {
  const status = insight?.status;
  const quality = insightQuality(insight);
  const isPending = !error && (loading || status === "PENDING");
  const actionLabel = status === "SUCCEEDED" ? "Perbarui" : "Buat analisis";

  return <section className="surface insight-card" aria-labelledby="insight-title">
    <div className="section-title insight-header"><div><span className="eyebrow">✦ ANALISIS RICHMOD</span><h2 id="insight-title">Ringkasan pola keuangan</h2></div>{!unsupported && status === "SUCCEEDED" && <button className="secondary" type="button" onClick={onGenerate} disabled={isPending}>{isPending ? "Menganalisis…" : actionLabel}</button>}</div>
    {unsupported && <p className="insight-muted">Analisis Richmod untuk rentang beberapa bulan belum tersedia.</p>}
    {!unsupported && error && <div className="insight-state" role="alert"><p>{error}</p><small>Grafik dan data keuangan tetap tersedia.</small>{canGenerate && <button className="secondary" type="button" onClick={onGenerate}>Coba lagi</button>}</div>}
    {!unsupported && isPending && <div className="insight-status" aria-live="polite">Sedang membaca pola keuangan siklus ini…</div>}
    {!unsupported && !error && !isPending && status === "FAILED" && <div className="insight-state"><p>Analisis belum berhasil dibuat.</p><small>Grafik dan data keuangan tetap tersedia.</small><button className="secondary" type="button" onClick={onGenerate}>Coba lagi</button></div>}
    {!unsupported && !error && !isPending && !status && <div className="insight-state"><p>{canGenerate ? "Belum ada analisis untuk siklus ini." : "Belum ada siklus gaji aktif."}</p><small>{canGenerate ? "Richmod dapat membaca pola dari data keuangan yang sudah terkonfirmasi." : "Pilih sumber gaji utama agar periode analisis dapat ditentukan dengan tepat."}</small>{canGenerate && <button className="secondary" type="button" onClick={onGenerate}>Buat analisis</button>}</div>}
    {!unsupported && !error && !isPending && status === "SUCCEEDED" && <><div className="insight-text">{String(insight.text || "").split(/\n{2,}/).filter(Boolean).map((paragraph, index) => <p key={index}>{paragraph}</p>)}</div><div className="insight-meta"><span>Kualitas data · {quality.label} · {Math.round(quality.value * 100)}%</span>{insight.completedAt && <span>Diperbarui {formatDate(insight.completedAt)}</span>}</div></>}
  </section>;
}

function formatDate(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
}
