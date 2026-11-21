import { CyclePaceChart } from "../components/Charts";
import { paceCurves, paceReferences } from "../lib/cycleLedger";
import { kpiCells } from "../lib/cycleKpi";
import { measuredLabel, qualityCopy, transactionHref } from "../lib/cycleReview";
import { dayLabel } from "../lib/chartData";
import { money } from "../lib/format";
import { SectionTitle } from "./shared";

const qualityHref = (blocker, period) => blocker.kind === "UNCATEGORIZED_EXPENSE" ? transactionHref(period, { type: "EXPENSE", categoryId: "uncategorized" }) : blocker.action === "/wealth" ? "/analytics?view=wealth" : ["/inbox", "/inbox?view=actions", "/settings"].includes(blocker.action) ? blocker.action : "/inbox";
const counted = ["OPEN_REVIEWS", "UNCATEGORIZED_EXPENSE", "PROCESSING_INCOMPLETE"];

export function NoticeStrip({ facts }) {
  const calendar = facts.period.kind !== "SALARY_CYCLE";
  if (!facts.dataQuality.length && !calendar) return null;
  return <aside id="quality" className="notice-strip" aria-labelledby="quality-title">
    <h2 id="quality-title" tabIndex={-1}>Data belum lengkap</h2>
    <ul>
      {calendar && <li><span>Menampilkan bulan kalender sementara. Belum ada gaji utama terkonfirmasi untuk menentukan siklus.</span></li>}
      {facts.dataQuality.map(blocker => {
        const [label, action] = qualityCopy[blocker.kind] || ["Data tinjauan perlu dilengkapi", "Buka Inbox"];
        return <li key={blocker.kind}><span>{counted.includes(blocker.kind) ? `${blocker.count} ` : ""}{label}{blocker.amount != null && ` (${money(blocker.amount)})`}</span><a href={qualityHref(blocker, facts.period)}>{action}</a></li>;
      })}
    </ul>
  </aside>;
}

export function QualityFooter({ facts, step }) {
  if (facts.dataQuality.length || facts.period.kind !== "SALARY_CYCLE") return null;
  return <section id="quality" className="review-section" aria-labelledby="quality-title">
    <details className="report-disclosure" open={step === "quality"}><summary><h2 id="quality-title" tabIndex={-1}>Kelengkapan data</h2><span>Tidak ada kendala</span></summary>
      <p>Tidak ada kendala yang tercatat untuk periode ini. Hal yang belum lengkap tetap terlihat di bagian atas; tidak ada skor kepercayaan dari model.</p>
    </details>
  </section>;
}

function Delta({ delta }) {
  if (!delta || (delta.empty && !delta.note)) return null;
  return <p className="kpi-delta" data-tone={delta.tone}>
    {!delta.empty && <strong>{delta.mark && <span aria-hidden="true">{delta.mark} </span>}{delta.word && <span className="visually-hidden">{delta.word} </span>}{delta.text}{delta.percent && ` ${delta.percent}`}</strong>}
    {delta.note && <small>{delta.note}</small>}
  </p>;
}

export function KpiStrip({ facts, updated = 0 }) {
  const cells = kpiCells(facts);
  return <section id="position" className="kpi-strip" aria-labelledby="position-title" data-updated={updated || undefined}>
    <h2 id="position-title" className="visually-hidden" tabIndex={-1}>Posisi siklus</h2>
    <dl key={updated} className="surface">
      {cells.map(cell => <div key={cell.key} className="kpi-cell" data-lead={cell.lead || undefined}>
        <dt>{cell.label}</dt>
        <dd>{cell.value}</dd>
        <Delta delta={cell.delta}/>
        {cell.sub && <p className="kpi-sub">{cell.sub}</p>}
        {cell.footnote && <p className="kpi-sub">{cell.footnote}</p>}
      </div>)}
    </dl>
    <div className="kpi-note"><span>Refund <strong>{money(facts.cashflow.refund)}</strong></span><details className="review-explainer"><summary>Tentang posisi siklus</summary><p>Hanya transaksi terkonfirmasi. Pengeluaran bersih setelah refund; transfer bukan pengeluaran.{facts.period.state === "ACTIVE" && " Selama siklus berjalan, arus kas bersih belum final karena pengeluaran masih bertambah."} Belum dialokasikan adalah arus kas bersih dikurangi transfer tabungan terkonfirmasi, bukan transaksi tambahan.</p></details></div>
  </section>;
}

export function PaceCard({ facts }) {
  const { comparison, period, pace } = facts;
  const median = pace?.median3 != null;
  const curves = paceCurves(pace);
  const previousCurve = curves.previous ?? [];
  const medianCurve = curves.median ?? [];
  return <section id="spending-shape" className="surface dash-card pace-card" aria-labelledby="shape-title">
    <div className="dash-card-head">
      <div>
        <SectionTitle id="shape-title" title="Laju pengeluaran"/>
        {facts.takeaways?.pace && <p className="takeaway">{facts.takeaways.pace}</p>}
      </div>
      <ul className="dash-legend" aria-label="Keterangan grafik">
        <li data-series="current"><i aria-hidden="true"/>Siklus ini</li>
        {comparison.previousFullCycle && <li data-series="previous"><i aria-hidden="true"/>Siklus lalu ({measuredLabel(comparison.previousFullCycle)})</li>}
        {median && <li data-series="median"><i aria-hidden="true"/>Median 3 siklus</li>}
      </ul>
    </div>
    <CyclePaceChart items={facts.daily} references={paceReferences(facts)} pace={pace} period={period} height={176}/>
    <details className="review-daily"><summary>Detail ›<span className="visually-hidden"> Lihat nilai laju pengeluaran</span></summary>
      <p className="dash-caption">Total pengeluaran bersih kumulatif sejak awal siklus, setelah refund, dalam Rp juta; transfer tidak termasuk. Garis berhenti di data terakhir dan tidak ada proyeksi.{!comparison.median3Available && ` Median 3 siklus tampil setelah 3 siklus penuh (${comparison.eligibleCycles}/3).`}</p>
      <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Nilai laju pengeluaran, geser untuk semua kolom"><table>
        <caption className="visually-hidden">Total pengeluaran bersih sampai setiap hari siklus</caption>
        <thead><tr><th scope="col">Hari</th><th scope="col">Tanggal</th><th scope="col">Siklus ini</th>{previousCurve.length > 0 && <th scope="col">Siklus lalu</th>}{medianCurve.length > 0 && <th scope="col">Median 3 siklus</th>}</tr></thead>
        <tbody>{facts.daily.map((day, index) => <tr key={day.period}>
          <th scope="row" data-row-prefix="Hari ">{index + 1}</th>
          <td data-label="Tanggal">{dayLabel(day.period)}</td>
          <td data-label="Siklus ini">{day.cumulativeExpense == null ? "—" : money(day.cumulativeExpense)}</td>
          {previousCurve.length > 0 && <td data-label="Siklus lalu">{previousCurve[index] == null ? "—" : money(previousCurve[index])}</td>}
          {medianCurve.length > 0 && <td data-label="Median 3 siklus">{medianCurve[index] == null ? "—" : money(medianCurve[index])}</td>}
        </tr>)}</tbody>
      </table></div>
    </details>
  </section>;
}
