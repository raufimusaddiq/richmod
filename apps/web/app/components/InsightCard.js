"use client";

import { dateTime } from "../lib/format";
import { measuredLabel } from "../lib/cycleReview";
import { insightPeriod } from "../lib/insightData";

const dataLinks = [
  ["Laju pengeluaran", "#spending-shape"],
  ["Yang berubah", "#changes"],
  ["Angka per kategori", "#history"],
  ["Kelengkapan data", "#quality"],
];

export default function InsightCard({ insight, cycle, days, loading = false, error = "", unsupported = false, canGenerate = true, onGenerate }) {
  const status = insight?.status;
  const isPending = !error && (loading || status === "PENDING");
  const succeeded = !unsupported && !isPending && status === "SUCCEEDED";
  const period = insightPeriod(insight);
  const older = cycle?.state === "ACTIVE" && period.measuredUntil !== cycle.measuredUntil;
  const compact = !succeeded;

  return <section className={`surface insight-card${compact ? " insight-card-compact" : ""}`} aria-labelledby={succeeded ? "insight-title" : undefined} aria-label={compact ? "Pembahasan siklus" : undefined}>
    <p className="discussion-banner">Opsional. Bukan saran keuangan atau keputusan rumah tangga. Ditulis model dari data terkonfirmasi; angka di dasbor tetap acuan.</p>
    <div className="section-title insight-header"><div>{succeeded && <h3 id="insight-title">Pembahasan siklus terpilih</h3>}</div>{succeeded && !error && canGenerate && <button className="secondary" type="button" onClick={onGenerate}>Perbarui pembahasan</button>}</div>
    {unsupported && <p className="insight-muted">Pembahasan Richmod untuk rentang beberapa bulan belum tersedia.</p>}
    {!unsupported && error && <div className="insight-state" role="alert"><div className="insight-state-copy"><p>{error}</p><small>Grafik dan data keuangan tetap tersedia.</small></div>{canGenerate && <button className="secondary" type="button" onClick={onGenerate}>Coba lagi</button>}</div>}
    {!unsupported && isPending && <div className="insight-state" aria-live="polite"><div className="insight-state-copy"><p>Sedang membaca pola keuangan siklus ini…</p><small>Halaman memeriksa hasilnya tiap 5 detik hingga sekitar 7 menit. Bagian lain tetap dapat dibaca.</small></div><button className="secondary" type="button" disabled>Menyusun pembahasan…</button></div>}
    {!unsupported && !error && !isPending && status === "FAILED" && <div className="insight-state"><div className="insight-state-copy"><p>Pembahasan belum berhasil dibuat.</p><small>Periksa kelengkapan data sebelum mencoba lagi. Grafik dan bukti tetap tersedia.</small></div>{canGenerate && <button className="secondary" type="button" onClick={onGenerate}>Coba lagi</button>}</div>}
    {!unsupported && !error && !isPending && !status && <div className="insight-state"><div className="insight-state-copy"><p>{canGenerate ? "Belum ada pembahasan untuk siklus ini." : "Belum ada siklus gaji terkonfirmasi."}</p><small>{canGenerate ? "Richmod dapat membaca pola dari data keuangan yang sudah terkonfirmasi. Dibatasi satu pembahasan per jam." : "Gaji utama terkonfirmasi diperlukan untuk menentukan periode pembahasan."}</small></div>{canGenerate && <button className="secondary" type="button" onClick={onGenerate}>Buat pembahasan</button>}</div>}
    {succeeded && <div className="discussion-body">
      <div>
        {period.measuredUntil && <p className="insight-muted">Data pembahasan: {measuredLabel(period)}{!older && days ? ` (hari ke-${days})` : ""} · dibuat {insight.completedAt ? dateTime(insight.completedAt) : "—"}{older && " · bukan data terkini"}.</p>}
        <div className="insight-text">{String(insight.text || "").split(/\n{2,}/).filter(Boolean).map((paragraph, index) => <p key={index}>{paragraph}</p>)}</div>
        <div className="insight-meta"><span>Pembahasan tambahan, bukan catatan finansial.</span>{insight.completedAt && <span>Diperbarui {dateTime(insight.completedAt)}</span>}</div>
      </div>
      <aside className="discussion-links" aria-labelledby="discussion-links-title">
        <h4 id="discussion-links-title">Cek dengan data</h4>
        <ul>{dataLinks.map(([label, href]) => <li key={href}><a href={href}>{label} →</a></li>)}</ul>
        <p className="insight-muted">Tautan tetap, bukan dari model. Maks. 1 pembahasan per jam.</p>
      </aside>
    </div>}
  </section>;
}
