import { CyclePaceChart } from "../components/Charts";
import { paceCurves, paceReferences } from "../lib/cycleLedger";
import { kpiCells } from "../lib/cycleKpi";
import { amountLabel, measuredLabel, qualityCopy, signedMoney, transactionHref } from "../lib/cycleReview";
import { dayLabel } from "../lib/chartData";
import { displayDate, money } from "../lib/format";
import { SectionTitle, Metric } from "./shared";

const qualityHref = (blocker, period) => blocker.kind === "UNCATEGORIZED_EXPENSE" ? transactionHref(period, { type: "EXPENSE", categoryId: "uncategorized" }) : ["/inbox", "/inbox?view=actions", "/wealth", "/settings"].includes(blocker.action) ? blocker.action : "/inbox";
const counted = ["OPEN_REVIEWS", "UNCATEGORIZED_EXPENSE", "PROCESSING_INCOMPLETE"];

// Never collapsed: whatever is incomplete stays visible above the figures it qualifies.
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

// With nothing to report the strip is absent; the positive statement lives in a collapsed footer.
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
  // A missing comparison is named in words; it does not also draw a second grey dash.
  return <p className="kpi-delta" data-tone={delta.tone}>
    {!delta.empty && <strong>{delta.mark && <span aria-hidden="true">{delta.mark} </span>}{delta.word && <span className="visually-hidden">{delta.word} </span>}{delta.text}{delta.percent && ` ${delta.percent}`}</strong>}
    {delta.note && <small>{delta.note}</small>}
  </p>;
}

export function KpiStrip({ facts, updated = 0 }) {
  const cells = kpiCells(facts);
  return <section id="position" className="kpi-strip" aria-labelledby="position-title" data-updated={updated || undefined}>
    <h2 id="position-title" className="visually-hidden" tabIndex={-1}>Posisi siklus</h2>
    <dl key={updated}>
      {cells.map(cell => <div key={cell.key} className="kpi-cell" data-lead={cell.lead || undefined}>
        <dt>{cell.label}</dt>
        <dd>{cell.value}</dd>
        <Delta delta={cell.delta}/>
        {cell.extra && <Delta delta={cell.extra}/>}
        {cell.sub && <p className="kpi-sub">{cell.sub}</p>}
        {cell.observed && <p className="kpi-sub">{cell.observed}</p>}
        {cell.footnote && <p className="kpi-sub">{cell.footnote}</p>}
      </div>)}
    </dl>
    <div className="kpi-note"><span>Refund <strong>{money(facts.cashflow.refund)}</strong></span><details className="review-explainer"><summary>Tentang posisi siklus</summary><p>Hanya transaksi terkonfirmasi. Pengeluaran bersih setelah refund; transfer bukan pengeluaran.{facts.period.state === "ACTIVE" && " Selama siklus berjalan, arus kas bersih belum final karena pengeluaran masih bertambah."} Surplus belum dialokasikan adalah arus kas bersih dikurangi transfer tabungan terkonfirmasi, bukan transaksi tambahan.</p></details></div>
  </section>;
}

export function PaceCard({ facts }) {
  const { comparison, period, pace } = facts;
  const median = pace?.median3 != null;
  const curves = paceCurves(pace);
  const previousCurve = curves.previous ?? [];
  const medianCurve = curves.median ?? [];
  return <section id="spending-shape" className="dash-card pace-card" aria-labelledby="shape-title">
    <div className="dash-card-head">
      <div>
        <SectionTitle id="shape-title" about="laju pengeluaran" title="Laju pengeluaran" description="Total pengeluaran bersih kumulatif sejak awal siklus, setelah refund; transfer tidak termasuk. Garis berhenti di data terakhir dan tidak ada proyeksi."/>
        <p className="dash-sub">Total pengeluaran bersih sejak gaji, Rp juta</p>
      </div>
      <ul className="dash-legend" aria-label="Keterangan grafik">
        <li data-series="current"><i aria-hidden="true"/>Siklus ini</li>
        {comparison.previousFullCycle && <li data-series="previous"><i aria-hidden="true"/>Siklus lalu ({measuredLabel(comparison.previousFullCycle)})</li>}
        {median && <li data-series="median"><i aria-hidden="true"/>Median 3 siklus</li>}
      </ul>
    </div>
    <CyclePaceChart items={facts.daily} references={paceReferences(facts)} pace={pace} period={period} height={176}/>
    <p className="dash-caption">Garis berhenti di data terakhir; tidak ada proyeksi.{!comparison.median3Available && ` Median 3 siklus tampil setelah 3 siklus penuh (${comparison.eligibleCycles}/3).`}</p>
    {/* The chart is an image to assistive tech; the same served totals are here as a table. */}
    <details className="review-daily"><summary>Lihat nilai laju pengeluaran</summary>
      <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Nilai laju pengeluaran, geser untuk semua kolom"><table>
        <caption className="visually-hidden">Total pengeluaran bersih sampai setiap hari siklus</caption>
        <thead><tr><th scope="col">Hari</th><th scope="col">Tanggal</th><th scope="col">Siklus ini</th>{previousCurve.length > 0 && <th scope="col">Siklus lalu</th>}{medianCurve.length > 0 && <th scope="col">Median 3 siklus</th>}</tr></thead>
        <tbody>{facts.daily.map((day, index) => <tr key={day.period}>
          <th scope="row">{index + 1}</th>
          <td>{dayLabel(day.period)}</td>
          <td>{day.cumulativeExpense == null ? "—" : money(day.cumulativeExpense)}</td>
          {previousCurve.length > 0 && <td>{previousCurve[index] == null ? "—" : money(previousCurve[index])}</td>}
          {medianCurve.length > 0 && <td>{medianCurve[index] == null ? "—" : money(medianCurve[index])}</td>}
        </tr>)}</tbody>
      </table></div>
    </details>
  </section>;
}

function SplitBar({ cashflow }) {
  const positive = value => value != null && !String(value).startsWith("-") && !/^0+(\.0+)?$/.test(String(value));
  const parts = [cashflow.savingsAllocated, cashflow.unallocatedSurplus];
  if (!positive(cashflow.netCashflow) || parts.some(value => value == null || String(value).startsWith("-")) || !parts.some(positive)) return null;
  return <div className="split-bar" role="img" aria-label="Pembagian arus kas bersih: ditabung dan belum dialokasikan"><i data-part="saved" style={{ flexGrow: Number(parts[0]) }}/><i data-part="open" style={{ flexGrow: Number(parts[1]) }}/></div>;
}

export function SavingsWealthCard({ facts }) {
  const { wealth, cashflow } = facts;
  const snapshot = wealth.current ?? wealth.previous;
  return <section id="savings-wealth" className="dash-card savings-card" aria-labelledby="savings-title">
    <SectionTitle id="savings-title" about="tabungan dan kekayaan" title="Tabungan & kekayaan" description="Alokasi tabungan adalah transfer terkonfirmasi; kekayaan adalah pengamatan saldo, bukan transaksi."/>
    <p className="dash-sub">Arus kas bersih {facts.period.state === "ACTIVE" ? "sejauh ini " : ""}{amountLabel(cashflow.netCashflow)}</p>
    <SplitBar cashflow={cashflow}/>
    <dl className="savings-lines">
      <div data-part="saved"><dt><i aria-hidden="true"/>Ditabung</dt><dd>{money(cashflow.savingsAllocated)}</dd></div>
      <div data-part="open"><dt><i aria-hidden="true"/>Belum dialokasikan</dt><dd>{money(cashflow.unallocatedSurplus)}</dd></div>
      {facts.savingsDestinations.map(item => <div key={item.id || item.name} className="destination"><dt>{item.name}</dt><dd>{money(item.amount)}</dd></div>)}
    </dl>
    {!facts.savingsDestinations.length && <p className="dash-caption">Belum ada alokasi tabungan terkonfirmasi.</p>}
    <div className="worth-block">
      <h3>Kekayaan bersih</h3>
      <p className="worth-value">{snapshot ? money(snapshot.netWorth) : "—"}</p>
      <p className="snapshot-context">{wealth.previous && wealth.current ? <>Dibandingkan: <a href={`/wealth?snapshotId=${encodeURIComponent(wealth.previous.id)}`}>{displayDate(wealth.previous.observedAt)}</a> → <a href={`/wealth?snapshotId=${encodeURIComponent(wealth.current.id)}`}>{displayDate(wealth.current.observedAt)}</a>. Perubahan {signedMoney(wealth.netWorthChange)}.</> : wealth.previous ? <>Pengamatan terakhir <a href={`/wealth?snapshotId=${encodeURIComponent(wealth.previous.id)}`}>{displayDate(wealth.previous.observedAt)}</a>, sebelum siklus ini. Perubahan siklus ini belum dapat dihitung.</> : "Belum ada pengamatan kekayaan."}</p>
      {wealth.current && wealth.netWorthChange == null && <p className="snapshot-context">{!wealth.previous ? "Belum ada catatan kekayaan sebelum siklus ini untuk dibandingkan." : "Belum dapat direkonsiliasi."}</p>}
    </div>
    <details className="review-daily"><summary>Rincian saldo & rekonsiliasi</summary><dl className="review-context">
      {wealth.current ? <><Metric label="Kekayaan bersih sebelumnya" value={amountLabel(wealth.previous?.netWorth)}/><Metric label="Kekayaan bersih terbaru" value={amountLabel(wealth.current.netWorth)}/></> : <Metric label="Kekayaan bersih pengamatan terakhir" value={amountLabel(wealth.previous?.netWorth)}/>}
      <Metric label="Perubahan kekayaan bersih" value={signedMoney(wealth.netWorthChange)}/>
      <Metric label="Kontribusi arus kas terkonfirmasi" value={amountLabel(wealth.confirmedCashflow)}/>
      <Metric label="Valuasi & perubahan lain" value={signedMoney(wealth.valuationAndOtherChange)}/>
    </dl>
    <details className="review-explainer"><summary>Tentang rekonsiliasi</summary><p>Rekonsiliasi mengikuti selang waktu pengamatan, bukan saldo akhir siklus yang diperkirakan. Selisih lainnya bukan laba investasi atau transfer tabungan.</p></details>
    {(wealth.current ? [["Sebelumnya", wealth.previous], ["Terbaru", wealth.current]] : [["Pengamatan terakhir", wealth.previous]]).map(([label, item]) => <p className="snapshot-context" key={label}>{label}: {item ? <>usia {item.ageDays} hari pada batas pengukuran</> : "belum tersedia"}</p>)}
    </details>
    <a className="card-link" href="/wealth">Buka Kekayaan →</a>
  </section>;
}
