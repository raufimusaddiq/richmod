import { useState } from "react";
import { amountLabel, measuredLabel, ratioLabel, reviewSteps, signedMoney, transactionHref } from "../lib/cycleReview";
import { deltaView } from "../lib/cycleKpi";
import { compactMillions, directionMark } from "../lib/cycleLedger";
import { buildHeatmap, deltaTone, householdRows, merchantBars, nonZeroAmount } from "../lib/cycleSections";
import { OTHER_CATEGORY_COLOR, categoryKey } from "../lib/categoryColors";
import { money } from "../lib/format";
import { Metric, SectionTitle } from "./shared";

const monthShort = new Intl.DateTimeFormat("id-ID", { month: "short", timeZone: "UTC" });
function columnMonth(start) {
  const date = new Date(`${start}T00:00:00Z`);
  return Number.isNaN(date.getTime()) ? "" : monthShort.format(date);
}

function Delta({ value, label }) {
  return <span className="section-delta" data-tone={deltaTone(value)}>
    {label && <span className="visually-hidden">{label}: </span>}
    {value == null ? "—" : <><span aria-hidden="true">{directionMark(value)} </span>{signedMoney(value)}</>}
  </span>;
}

export function MeetingNav({ step, selection, navigate }) {
  const index = reviewSteps.findIndex(([id]) => id === step);
  return <div className="meeting-controls">
    <p role="status">{index + 1} / {reviewSteps.length} · {reviewSteps[index][1]} <small>Sesi tinjauan; keputusan disimpan terpisah.</small></p>
    <nav aria-label="Langkah tinjauan siklus"><ol>{reviewSteps.map(([id, label], i) => <li key={id}><button type="button" className={id === step ? "active" : "secondary"} aria-current={id === step ? "step" : undefined} onClick={() => navigate({ step: id })}>{i + 1}. {label}</button></li>)}</ol></nav>
    <div className="meeting-actions">
      <button type="button" className="secondary" disabled={index === 0} onClick={() => navigate({ step: reviewSteps[index - 1][0] })}>Langkah sebelumnya</button>
      {index < reviewSteps.length - 1 && <button type="button" onClick={() => navigate({ step: reviewSteps[index + 1][0] })}>Langkah berikutnya</button>}
      <button type="button" className="secondary" onClick={() => navigate({ step: "" })}>{index === reviewSteps.length - 1 ? "Selesai meninjau" : "Tampilkan seluruh tinjauan"}</button>
    </div>
  </div>;
}

export function ComparisonContext({ comparison, period }) {
  const elapsed = comparison.mode === "ELAPSED_DAYS";
  const rows = [[elapsed ? "Siklus ini · berjalan" : "Siklus ini", comparison.expense.amount], ["Sebelumnya · penuh", comparison.expense.previousFullCycle], ...(elapsed ? [["Sebelumnya · hari yang sama", comparison.expense.previous]] : []), [elapsed ? "Median 3 siklus · hari yang sama" : "Median 3 siklus", comparison.expense.median3]];
  const maximum = Math.max(0, ...rows.map(([, value]) => Math.abs(Number(value))));
  return <div className="review-baseline">
    <div className="baseline-meta"><span>{measuredLabel(period)}{elapsed && " · berjalan"}</span><span>{comparison.previousFullCycle ? `Sebelumnya · penuh: ${measuredLabel(comparison.previousFullCycle)}` : "Belum ada siklus pembanding"}</span>{elapsed && <span>Hari yang sama, bukan siklus penuh: {measuredLabel(comparison.previous)}</span>}<span>{comparison.median3Available ? "Median 3 siklus tersedia" : `Median belum tersedia · ${comparison.eligibleCycles}/3 siklus`}</span></div>
    <dl className="comparison-bars" aria-label="Perbandingan pengeluaran bersih">
      {rows.map(([label, value]) => <div key={label}><dt>{label}</dt><dd><span aria-hidden="true" className="comparison-track"><i data-negative={String(value).startsWith("-") || undefined} style={{ width: maximum ? `${Math.abs(Number(value)) / maximum * 100}%` : "0%" }}/></span><strong>{amountLabel(value)}</strong></dd></div>)}
    </dl>
    <dl className="comparison-deltas">
      <Metric label="Selisih vs siklus sebelumnya (penuh)" value={`${signedMoney(comparison.expense.deltaVsPreviousFullCycle)} · ${ratioLabel(comparison.expense.relativeDeltaVsPreviousFullCycle)}`}/>
      {elapsed && <Metric label="Selisih vs hari yang sama" value={signedMoney(comparison.expense.deltaVsPrevious)}/>}
      <Metric label={elapsed ? "Selisih vs median 3 siklus · hari yang sama" : "Selisih vs median 3 siklus"} value={signedMoney(comparison.expense.deltaVsMedian3)}/>
    </dl>
  </div>;
}

export function ChangesTable({ items, comparison, selected, select, colors = null }) {
  if (!items.length) return <p className="empty compact">Belum ada pengeluaran kategori pada periode ini atau pembandingnya.</p>;
  const elapsed = comparison.mode === "ELAPSED_DAYS";
  const prevLabel = elapsed ? "Sebelumnya · hari yang sama" : "Sebelumnya · penuh";
  const medianLabel = elapsed ? "Median 3 siklus · hari yang sama" : "Median 3 siklus";
  const deltaLabel = elapsed ? "Selisih vs hari yang sama" : "Selisih vs sebelumnya";
  const medianDeltaLabel = elapsed ? "Selisih vs median · hari yang sama" : "Selisih vs median";
  const contribLabel = elapsed ? "Kontribusi ke selisih hari yang sama" : "Kontribusi ke selisih total";
  return <>
    <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Perbandingan kategori lengkap, geser untuk semua kolom"><table className="changes-table">
    <caption>Perubahan kategori. Persentase tanpa pembanding positif ditampilkan sebagai —. Sebelumnya · penuh: {measuredLabel(comparison.previousFullCycle)}.</caption>
    <thead><tr><th scope="col">Kategori / bukti</th><th scope="col">Siklus ini</th><th scope="col">{prevLabel}</th><th scope="col">{medianLabel}</th><th scope="col">{deltaLabel}</th><th scope="col">{medianDeltaLabel}</th><th scope="col">{contribLabel}</th>{elapsed && <><th scope="col">Sebelumnya · penuh</th><th scope="col">Selisih vs siklus sebelumnya (penuh)</th></>}</tr></thead>
    <tbody>{items.map(item => { const key = categoryKey(item); const color = colors?.get(key); const swatch = color && color !== OTHER_CATEGORY_COLOR ? <i className="heat-swatch" aria-hidden="true" style={{ "--cat": color }}/> : null; return <tr key={key} data-selected={selected === key}>
      <th scope="row"><button className="change-button" type="button" aria-haspopup="dialog" aria-controls="category-drawer" onClick={event => select(key, event.currentTarget)}>{swatch}<span>{item.name}</span></button></th>
      <td data-label="Siklus ini">{money(item.amount)}</td><td data-label={prevLabel}>{amountLabel(item.previous)}</td><td data-label={medianLabel}>{amountLabel(item.median3)}</td>
      <td data-label={deltaLabel}><span>{signedMoney(item.deltaVsPrevious)}<small>{ratioLabel(item.relativeDeltaVsPrevious)}</small></span></td>
      <td data-label={medianDeltaLabel}><span>{signedMoney(item.deltaVsMedian3)}<small>{ratioLabel(item.relativeDeltaVsMedian3)}</small></span></td>
      <td data-label={contribLabel}>{ratioLabel(item.contributionToExpenseChange)}</td>
      {elapsed && <><td data-label="Sebelumnya · penuh">{amountLabel(item.previousFullCycle)}</td><td data-label="Selisih vs siklus sebelumnya (penuh)"><span>{signedMoney(item.deltaVsPreviousFullCycle)}<small>{ratioLabel(item.relativeDeltaVsPreviousFullCycle)}</small></span></td></>}
    </tr>; })}</tbody>
  </table></div></>;
}

export function CategoryHeatmap({ facts, mode: controlledMode = null, colors = null, selectedCategory = "", onSelectCategory }) {
  const sameDay = facts.categoryHistory?.sameDay;
  const [localMode, setLocalMode] = useState(sameDay ? "SAME_DAY" : "FULL");
  const mode = controlledMode ?? localMode;
  const heat = buildHeatmap(facts.categoryHistory, facts.history, mode, { selectedCategory, selectable: facts.categoryChanges.map(item => item.id || "uncategorized") });
  if (!heat) return <p className="empty compact">Matriks kategori belum tersedia untuk siklus ini.</p>;
  const deltaLabel = heat.mode === "SAME_DAY" ? "Selisih vs siklus lalu, hari yang sama" : "Selisih vs siklus lalu";
  return <div className="heat-section">
    {sameDay && controlledMode == null && <div className="segmented" data-slide role="group" aria-label="Rentang kolom matriks">
      <button type="button" aria-pressed={mode === "SAME_DAY"} className={mode === "SAME_DAY" ? "active" : "secondary"} onClick={() => setLocalMode("SAME_DAY")}>Hari ke-{sameDay.elapsedDays} yang sama</button>
      <button type="button" aria-pressed={mode === "FULL"} className={mode === "FULL" ? "active" : "secondary"} onClick={() => setLocalMode("FULL")}>Siklus penuh</button>
    </div>}
    <p className="review-description">{heat.mode === "SAME_DAY" ? `Setiap siklus dihitung sampai hari ke-${sameDay.elapsedDays} agar siklus berjalan setara.` : "Total akhir tiap siklus. Kolom berjalan ditandai garis putus-putus dan tidak ikut pewarnaan karena belum selesai."}</p>
    <div className="heat-wrap" tabIndex={0} role="region" aria-label="Pengeluaran per kategori dan siklus, geser untuk semua kolom">
      <table className="heat-table">
        <caption className="visually-hidden">Pengeluaran bersih per kategori untuk setiap siklus gaji, dalam jutaan rupiah. {deltaLabel}.</caption>
        <thead><tr>
          <th scope="col" className="heat-unit">Rp juta · {heat.mode === "SAME_DAY" ? `hari 1–${sameDay.elapsedDays}` : "total siklus"}</th>
          {heat.columns.map(column => <th key={column.start} scope="col" data-selected={column.selected || undefined} data-running={column.untinted || undefined} aria-current={column.selected ? "true" : undefined}>
            <b>{columnMonth(column.start)}</b><small>{column.subhead}</small>
          </th>)}
          <th scope="col" className="heat-delta-head">{deltaLabel}</th>
        </tr></thead>
        <tbody>
          {heat.rows.map(row => {
            const color = row.muted ? OTHER_CATEGORY_COLOR : colors?.get(row.key);
            const swatch = row.muted || (color && color !== OTHER_CATEGORY_COLOR) ? <i className="heat-swatch" aria-hidden="true" style={{ "--cat": row.muted ? OTHER_CATEGORY_COLOR : color }}/> : null;
            return <tr key={row.key} data-selected-row={row.selected || undefined}>
              <th scope="row">
                {row.selectable && onSelectCategory
                  ? <button type="button" className="heat-row-button" aria-pressed={row.selected} aria-label={`Buka bukti ${row.name}`} onClick={() => onSelectCategory(row.id)}>{swatch}<span>{row.name}</span></button>
                  : <span className="heat-name" data-muted={row.muted || undefined}>{swatch}{row.name}</span>}
                <small className="heat-row-delta"><Delta value={row.delta} label={deltaLabel}/></small>
              </th>
              {heat.columns.map((column, index) => <td key={column.start} className="heat-amount" data-selected={column.selected || undefined} data-running={column.untinted || undefined}>
                <span className="heat-cell" title={money(row.amounts[index])} style={{ "--tint": row.tints[index] }}>{compactMillions(row.amounts[index])}</span>
              </td>)}
              <td className="heat-delta"><Delta value={row.delta}/></td>
            </tr>;
          })}
        </tbody>
        <tfoot><tr>
          <th scope="row">Total pengeluaran bersih</th>
          {heat.columns.map((column, index) => <td key={column.start} className="heat-amount" data-selected={column.selected || undefined} data-running={column.untinted || undefined} title={money(heat.total.amounts[index])}>{compactMillions(heat.total.amounts[index])}</td>)}
          <td className="heat-delta"><Delta value={heat.total.delta}/></td>
        </tr></tfoot>
      </table>
    </div>
    <p className="heat-legend">Warna sel: besar relatif dalam satu baris (lebih gelap = lebih besar), bukan baik atau buruk. Klik baris untuk membuka kategori (merchant &amp; transaksi).</p>
  </div>;
}

function MerchantDelta({ value }) {
  const delta = deltaView(value ?? null, { lowerIsBetter: true });
  if (delta.empty) return <span className="merchant-row-delta" data-tone="none"/>;
  if (delta.zero) return <span className="merchant-row-delta" data-tone="none">sama</span>;
  return <span className="merchant-row-delta" data-tone={delta.tone}>
    {delta.mark && <span aria-hidden="true">{delta.mark} </span>}
    {delta.word && <span className="visually-hidden">{delta.word} </span>}
    {delta.text}
  </span>;
}

export function MerchantCard({ facts, step }) {
  const list = merchantBars(facts.merchantRanking);
  const rows = list?.rows || [];
  const other = list?.other && nonZeroAmount(list.other.amount) ? list.other : null;
  const empty = !list || !rows.length;
  return <section id="merchants" className="surface dash-card" aria-labelledby="merchants-title">
    <div className="dash-card-head">
      <div>
        <SectionTitle id="merchants-title" title="Merchant teratas"/>
        {facts.takeaways?.merchants && <p className="takeaway">{facts.takeaways.merchants}</p>}
      </div>
    </div>
    {empty ? <p className="empty compact">Belum ada merchant pendukung pada siklus ini.</p> : <>
      <ul className="merchant-list" aria-label="Merchant teratas">
        {rows.map(item => <li key={item.key} className="merchant-item">
          <span className="merchant-item-name">
            {item.id ? <a href={transactionHref(facts.period, { merchantId: item.id })}>{item.name}</a> : <span>{item.name}</span>}
            {item.notNamed && <small>tanpa nama penerima</small>}
          </span>
          <span className="merchant-item-amount">{money(item.amount)}</span>
          <span className="merchant-item-bar" aria-hidden="true"><i style={{ width: `${item.length * 100}%` }}/></span>
          <MerchantDelta value={item.deltaVsPrevious}/>
        </li>)}
        {other && <li className="merchant-item" data-other="">
          <span className="merchant-item-name"><span>Merchant lain · {other.merchantCount}</span></span>
          <span className="merchant-item-amount">{money(other.amount)}</span>
          <span aria-hidden="true"/>
          <MerchantDelta value={other.deltaVsPrevious}/>
        </li>}
      </ul>
      <details className="review-daily" open={step === "drivers" || undefined}>
        <summary>Semua merchant › <small>{list.rows.length} teratas, jumlah transaksi, total</small></summary>
        <MerchantRanking ranking={facts.merchantRanking} period={facts.period}/>
      </details>
    </>}
  </section>;
}

export function MerchantRanking({ ranking, period }) {
  const list = merchantBars(ranking);
  if (!list || !list.rows.length) return <p className="empty compact">Belum ada merchant pendukung pada siklus ini.</p>;
  const name = (item, other) => item.id
    ? <a href={transactionHref(period, { merchantId: item.id })}>{item.name}</a>
    : <span data-muted={other || undefined}>{item.name}</span>;
  const bar = item => <span className="merchant-bar" aria-hidden="true"><i style={{ width: `${item.length * 100}%` }}/></span>;
  return <div className="merchant-rank-wrap">
    <div className="merchant-rank-scroll" tabIndex={0} role="region" aria-label="Merchant pendukung, geser untuk semua kolom">
      <table className="merchant-rank">
        <caption className="visually-hidden">Merchant pendukung, terbesar dulu. Pembanding: {list.compareLabel.toLowerCase()}.</caption>
        <thead><tr><th scope="col">Merchant</th><th scope="col">Transaksi</th><th scope="col">Siklus ini</th><th scope="col">{list.compareLabel}</th><th scope="col">Selisih</th></tr></thead>
        <tbody>
          {list.rows.map(item => <tr key={item.key}>
            <th scope="row">{name(item)}{item.notNamed && <small>transaksi tanpa nama penerima</small>}{bar(item)}</th>
            <td data-label="Transaksi">{item.count ?? "—"}</td>
            <td data-label="Siklus ini">{money(item.amount)}</td>
            <td data-label={list.compareLabel}>{amountLabel(item.previous)}</td>
            <td data-label="Selisih"><Delta value={item.deltaVsPrevious}/></td>
          </tr>)}
          {list.other && nonZeroAmount(list.other.amount) && <tr data-other="true">
            <th scope="row">{name({ ...list.other, name: `Merchant lain · ${list.other.merchantCount}` }, true)}{bar(list.other)}</th>
            <td data-label="Transaksi">{list.other.count ?? "—"}</td>
            <td data-label="Siklus ini">{money(list.other.amount)}</td>
            <td data-label={list.compareLabel}>{amountLabel(list.other.previous)}</td>
            <td data-label="Selisih"><Delta value={list.other.deltaVsPrevious}/></td>
          </tr>}
        </tbody>
        {list.total && <tfoot><tr>
          <th scope="row">Total</th>
          <td data-label="Transaksi">{list.total.count ?? "—"}</td>
          <td data-label="Siklus ini">{money(list.total.amount)}</td>
          <td data-label={list.compareLabel}>{amountLabel(list.total.previous)}</td>
          <td data-label="Selisih"><Delta value={list.total.deltaVsPrevious}/></td>
        </tr></tfoot>}
      </table>
    </div>
    <a className="card-link" href={transactionHref(period)}>Semua transaksi siklus ini →</a>
  </div>;
}

export function HouseholdSection({ members = [] }) {
  const rows = householdRows(members);
  return <section id="household" className="family-talk-part" aria-labelledby="household-title">
    <h3 id="household-title">Catatan rumah tangga</h3>
    <p className="review-description">Siapa yang memulai pencatatan, ketika diketahui. Bukan peringkat tanggung jawab atau perbandingan kebiasaan.</p>
    {rows.length ? <>
      <div className="household-bar" role="img" aria-label="Porsi pengeluaran bersih menurut pencatat">
        {rows.map(item => <i key={item.key} data-tint={item.tint} style={{ flexGrow: item.share ?? 0 }}/>)}
      </div>
      <ul className="household-list">
        {rows.map(item => <li key={item.key}>
          <span className="household-avatar" data-tint={item.tint} aria-hidden="true">{item.initial}</span>
          <span className="household-name">{item.name}{item.automatic && <small>otomatis</small>}</span>
          <span className="household-count">{item.count} transaksi</span>
          <span className="household-amount">{money(item.amount)}</span>
          <span className="household-share">{ratioLabel(item.shareOfExpense)}</span>
        </li>)}
      </ul>
      <p className="heat-legend">Urutan tetap: otomatis dulu, lalu anggota menurut nama. Tanpa perbandingan antar siklus.</p>
    </> : <p className="empty compact">Belum ada pengeluaran yang dapat diatribusikan.</p>}
  </section>;
}
