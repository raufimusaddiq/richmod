import { useEffect, useState } from "react";
import { CyclePaceChart } from "../components/Charts";
import CountUp from "../components/CountUp";
import { paceCurves, paceReferences } from "../lib/cycleLedger";
import { deltaView, kpiCells, shouldShowLastCycleCard, unsignedPercentText } from "../lib/cycleKpi";
import { cycleRangeLabel } from "../lib/cycleDates";
import { isBlockingQuality, measuredLabel, qualityCopy, splitDataQuality, transactionHref } from "../lib/cycleReview";
import { dayLabel } from "../lib/chartData";
import { money } from "../lib/format";
import { SectionTitle } from "./shared";

const qualityHref = (blocker, period) => blocker.kind === "UNCATEGORIZED_EXPENSE" ? transactionHref(period, { type: "EXPENSE", categoryId: "uncategorized" }) : blocker.action === "/wealth" ? "/analytics?view=wealth" : ["/inbox", "/inbox?view=actions", "/settings"].includes(blocker.action) ? blocker.action : "/inbox";
const counted = ["OPEN_REVIEWS", "UNCATEGORIZED_EXPENSE", "PROCESSING_INCOMPLETE"];

export function NoticeStrip({ facts }) {
  const calendar = facts.period.kind !== "SALARY_CYCLE";
  const { blocking } = splitDataQuality(facts.dataQuality);
  if (!blocking.length && !calendar) return null;
  return <aside className="notice-strip" aria-label="Data belum lengkap">
    <h2>Data belum lengkap</h2>
    <ul>
      {calendar && <li><span>Menampilkan bulan kalender sementara. Belum ada gaji utama terkonfirmasi untuk menentukan siklus.</span></li>}
      {blocking.map(blocker => {
        const [label, action] = qualityCopy[blocker.kind] || ["Data tinjauan perlu dilengkapi", "Buka Inbox"];
        return <li key={blocker.kind}><span>{counted.includes(blocker.kind) ? `${blocker.count} ` : ""}{label}{blocker.amount != null && ` (${money(blocker.amount)})`}</span><a href={qualityHref(blocker, facts.period)}>{action}</a></li>;
      })}
    </ul>
  </aside>;
}

export function QualityFooter({ facts, step, openTrigger = 0 }) {
  const [open, setOpen] = useState(step === "quality");
  const calendar = facts.period.kind !== "SALARY_CYCLE";
  const items = facts.dataQuality || [];
  const { blocking, advisory } = splitDataQuality(items);
  const totalCount = items.length + (calendar ? 1 : 0);

  useEffect(() => {
    setOpen(step === "quality");
  }, [step]);

  useEffect(() => {
    if (openTrigger > 0) setOpen(true);
  }, [openTrigger]);

  useEffect(() => {
    const reveal = () => {
      if (window.location.hash === "#quality") setOpen(true);
    };
    reveal();
    window.addEventListener("hashchange", reveal);
    return () => window.removeEventListener("hashchange", reveal);
  }, []);

  const summaryNote = totalCount === 0
    ? "Tidak ada kendala"
    : blocking.length && advisory.length
      ? `${blocking.length + (calendar ? 1 : 0)} kendala · ${advisory.length} informasi`
      : blocking.length || calendar
        ? `${blocking.length + (calendar ? 1 : 0)} hal · perlu ditinjau`
        : `${advisory.length} hal · informasi`;

  return <section id="quality" className="review-section" aria-labelledby="quality-title">
    <details className="report-disclosure" open={open} onToggle={event => setOpen(event.currentTarget.open)}>
      <summary><h2 id="quality-title" tabIndex={-1}>Kelengkapan data</h2><span>{summaryNote}</span></summary>
      <div className="report-disclosure-body">
        {totalCount === 0
          ? <p>Tidak ada kendala yang tercatat untuk periode ini. Hal yang mengubah angka tetap terlihat di bagian atas; tidak ada skor kepercayaan dari model.</p>
          : <>
            <ul className="review-quality">
              {calendar && <li>
                <div><strong className="quality-tag" data-severity="block">Menghalangi</strong><p>Menampilkan bulan kalender sementara. Belum ada gaji utama terkonfirmasi untuk menentukan siklus.</p></div>
                <a href="/settings">Buka pengaturan</a>
              </li>}
              {items.map(item => {
                const [label, action] = qualityCopy[item.kind] || ["Data tinjauan perlu dilengkapi", "Buka Inbox"];
                const block = isBlockingQuality(item.kind);
                return <li key={item.kind}>
                  <div>
                    <strong className="quality-tag" data-severity={block ? "block" : "info"}>{block ? "Menghalangi" : "Info"}</strong>
                    <p>{counted.includes(item.kind) ? `${item.count} ` : ""}{label}{item.amount != null && ` (${money(item.amount)})`}</p>
                  </div>
                  <a href={qualityHref(item, facts.period)}>{action}</a>
                </li>;
              })}
            </ul>
            <p className="dash-caption">Hal yang mengubah angka (tinjauan terbuka, belum dikategorikan) tampil sebagai banner di atas.</p>
          </>}
      </div>
    </details>
  </section>;
}

export function LastCycleCard({ facts, meeting = false, onReviewPrevious }) {
  if (meeting || !shouldShowLastCycleCard(facts)) return null;
  const prev = facts.previousCycle;
  const delta = deltaView(prev.deltaVsPreviousFullCycle ?? null, { relative: prev.relativeDeltaVsPreviousFullCycle ?? null, lowerIsBetter: true });
  const percent = unsignedPercentText(prev.relativeDeltaVsPreviousFullCycle);
  const range = cycleRangeLabel({ start: prev.start, end: prev.end });
  return <section id="last-cycle" className="surface dash-card last-cycle-card" aria-labelledby="last-cycle-title">
    <div className="dash-card-head">
      <div>
        <SectionTitle id="last-cycle-title" title="Siklus lalu sudah ditutup"/>
        <p className="takeaway">Minggu pertama siklus baru: tutup dulu siklus {range}.</p>
      </div>
    </div>
    <dl className="last-cycle-facts">
      <div>
        <dt>Pengeluaran bersih</dt>
        <dd>{money(prev.expense)}</dd>
        {prev.days != null && <small>{prev.days} hari</small>}
      </div>
      <div>
        <dt>vs siklus sebelumnya</dt>
        <dd data-tone={delta.tone}>
          {delta.empty ? "—" : delta.zero ? "Sama" : <>{delta.mark && <span aria-hidden="true">{delta.mark} </span>}{delta.word && <span className="visually-hidden">{delta.word} </span>}{percent || delta.text}</>}
        </dd>
        {!delta.empty && !delta.zero && percent && <small>{delta.text} (total)</small>}
      </div>
      <div>
        <dt>Ditabung</dt>
        <dd>{money(prev.savingsAllocated)}</dd>
      </div>
      <div>
        <dt>Sisa belum dialokasikan</dt>
        <dd>{money(prev.unallocatedSurplus)}</dd>
        <small><a className="last-cycle-link" href="/inbox">Putuskan di Inbox →</a></small>
      </div>
    </dl>
    <div className="last-cycle-actions">
      <button type="button" onClick={() => onReviewPrevious(prev.start)}>Tinjau siklus lalu · 7 langkah</button>
      <span className="kpi-sub">Kartu ini tampil di hari ke-1 sampai ke-7.</span>
    </div>
  </section>;
}

export function KpiStrip({ facts, updated = 0, meeting = false, onStartReview }) {
  const cells = kpiCells(facts);
  const showReviewCta = facts.period.kind === "SALARY_CYCLE" && facts.period.state === "CLOSED" && !meeting && Boolean(onStartReview);
  return <section id="position" className="kpi-strip" aria-labelledby="position-title" data-updated={updated || undefined}>
    <h2 id="position-title" className="visually-hidden" tabIndex={-1}>Posisi siklus</h2>
    <dl key={updated} className="surface">
      {cells.map(cell => <div key={cell.key} className="kpi-cell" data-lead={cell.lead || undefined}>
        <dt>{cell.label}</dt>
        <dd><CountUp text={cell.value}/></dd>
        {cell.verdict && <p className="kpi-verdict">{cell.verdict}</p>}
        {cell.chips?.length > 0 && <div className="kpi-chips">
          {cell.chips.map(chip => <span key={chip.key} className="kpi-chip" data-tone={chip.tone}>
            {chip.mark && <span aria-hidden="true">{chip.mark} </span>}
            {chip.word && <span className="visually-hidden">{chip.word} </span>}
            {chip.text}
          </span>)}
        </div>}
        {cell.emptyComparison && <p className="kpi-sub">{cell.emptyComparison}</p>}
        {cell.sub && <p className="kpi-sub">{cell.sub}</p>}
        {cell.footnote && <p className="kpi-sub">{cell.footnote}</p>}
        {cell.lead && showReviewCta && <div className="kpi-cta"><button type="button" onClick={onStartReview}>Mulai tinjauan · 7 langkah</button></div>}
      </div>)}
    </dl>
  </section>;
}

export function PaceCard({ facts, step }) {
  const { comparison, period, pace, spendingShape } = facts;
  const median = pace?.median3 != null;
  const curves = paceCurves(pace);
  const previousCurve = curves.previous ?? [];
  const medianCurve = curves.median ?? [];
  const closed = period.state === "CLOSED";
  const prevStart = comparison?.previousFullCycle?.start;
  const prevDays = prevStart ? facts.history?.find(item => item.start === prevStart)?.days ?? null : null;
  return <section id="spending-shape" className="surface dash-card pace-card" aria-labelledby="shape-title">
    <div className="dash-card-head">
      <div>
        <SectionTitle id="shape-title" title="Laju pengeluaran"/>
        {facts.takeaways?.pace && <p className="takeaway">{facts.takeaways.pace}</p>}
      </div>
      <ul className="dash-legend" aria-label="Keterangan grafik">
        <li data-series="current"><i aria-hidden="true"/>Siklus ini{closed && spendingShape?.days ? ` (${spendingShape.days} hari)` : ""}</li>
        {comparison.previousFullCycle && <li data-series="previous"><i aria-hidden="true"/>Siklus lalu ({prevDays ? `${prevDays} hari` : measuredLabel(comparison.previousFullCycle)})</li>}
        {median && <li data-series="median"><i aria-hidden="true"/>Median 3 siklus</li>}
      </ul>
    </div>
    <CyclePaceChart items={facts.daily} references={paceReferences(facts)} pace={pace} period={period} height={210}/>
    <details className="review-daily" open={step === "spending-shape" || undefined}><summary>Tabel laju › <small>kumulatif per hari</small></summary>
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
