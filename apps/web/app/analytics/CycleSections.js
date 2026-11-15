"use client";

import { useState } from "react";
import { amountLabel, measuredLabel, ratioLabel, reviewSteps, signedMoney, transactionHref } from "../lib/cycleReview";
import { compactMillions, directionMark } from "../lib/cycleLedger";
import { buildHeatmap, deltaTone, householdRows, merchantBars } from "../lib/cycleSections";
import { money } from "../lib/format";
import { Metric } from "./shared";

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

export function ChangesTable({ items, comparison, selected, select }) {
  if (!items.length) return <p className="empty compact">Belum ada pengeluaran kategori pada periode ini atau pembandingnya.</p>;
  const elapsed = comparison.mode === "ELAPSED_DAYS";
  const deltaLabel = elapsed ? "Selisih vs hari yang sama" : "Selisih vs sebelumnya";
  return <>
    <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Perbandingan kategori lengkap, geser untuk semua kolom"><table className="changes-table">
    <caption>Perubahan kategori. Persentase tanpa pembanding positif ditampilkan sebagai —. Sebelumnya · penuh: {measuredLabel(comparison.previousFullCycle)}.</caption>
    <thead><tr><th scope="col">Kategori / bukti</th><th scope="col">Siklus ini</th><th scope="col">{elapsed ? "Sebelumnya · hari yang sama" : "Sebelumnya · penuh"}</th><th scope="col">{elapsed ? "Median 3 siklus · hari yang sama" : "Median 3 siklus"}</th><th scope="col">{deltaLabel}</th><th scope="col">{elapsed ? "Selisih vs median · hari yang sama" : "Selisih vs median"}</th><th scope="col">{elapsed ? "Kontribusi ke selisih hari yang sama" : "Kontribusi ke selisih total"}</th>{elapsed && <><th scope="col">Sebelumnya · penuh</th><th scope="col">Selisih vs siklus sebelumnya (penuh)</th></>}</tr></thead>
    <tbody>{items.map(item => <tr key={item.id || "uncategorized"} data-selected={selected === (item.id || "uncategorized")}>
      <th scope="row"><button className="change-button" type="button" aria-haspopup="dialog" aria-controls="category-drawer" onClick={event => select(item.id || "uncategorized", event.currentTarget)}>{item.name}</button></th>
      <td>{money(item.amount)}</td><td>{amountLabel(item.previous)}</td><td>{amountLabel(item.median3)}</td>
      <td>{signedMoney(item.deltaVsPrevious)}<small>{ratioLabel(item.relativeDeltaVsPrevious)}</small></td>
      <td>{signedMoney(item.deltaVsMedian3)}<small>{ratioLabel(item.relativeDeltaVsMedian3)}</small></td>
      <td>{ratioLabel(item.contributionToExpenseChange)}</td>
      {elapsed && <><td>{amountLabel(item.previousFullCycle)}</td><td>{signedMoney(item.deltaVsPreviousFullCycle)}<small>{ratioLabel(item.relativeDeltaVsPreviousFullCycle)}</small></td></>}
    </tr>)}</tbody>
  </table></div></>;
}

// Kategori × siklus. Same-day mode is the default while a cycle runs, so every
// column is cut at the same elapsed day; full mode marks the running column and
// leaves it out of the tint. Values, deltas and totals are served.
export function CategoryHeatmap({ facts, selectedCategory = "", onSelectCategory }) {
  const sameDay = facts.categoryHistory?.sameDay;
  const [mode, setMode] = useState(sameDay ? "SAME_DAY" : "FULL");
  const heat = buildHeatmap(facts.categoryHistory, facts.history, mode, { selectedCategory, selectable: facts.categoryChanges.map(item => item.id || "uncategorized") });
  if (!heat) return <p className="empty compact">Matriks kategori belum tersedia untuk siklus ini.</p>;
  const deltaLabel = heat.mode === "SAME_DAY" ? "Selisih vs siklus lalu, hari yang sama" : "Selisih vs siklus lalu";
  return <div className="heat-section">
    {sameDay && <div className="segmented" role="group" aria-label="Rentang kolom matriks">
      <button type="button" aria-pressed={mode === "SAME_DAY"} className={mode === "SAME_DAY" ? "active" : "secondary"} onClick={() => setMode("SAME_DAY")}>Hari ke-{sameDay.elapsedDays} yang sama</button>
      <button type="button" aria-pressed={mode === "FULL"} className={mode === "FULL" ? "active" : "secondary"} onClick={() => setMode("FULL")}>Siklus penuh</button>
    </div>}
    <p className="review-description">{heat.mode === "SAME_DAY" ? `Setiap siklus dihitung sampai hari ke-${sameDay.elapsedDays} agar siklus berjalan setara.` : "Total akhir tiap siklus. Kolom berjalan ditandai garis putus-putus dan tidak ikut pewarnaan karena belum selesai."}</p>
    <div className="heat-wrap" tabIndex={0} role="region" aria-label="Pengeluaran per kategori dan siklus, geser untuk semua kolom">
      <table className="heat-table" style={{ "--heat-cols": heat.columns.length }}>
        <caption className="visually-hidden">Pengeluaran bersih per kategori untuk setiap siklus gaji, dalam jutaan rupiah. {deltaLabel}.</caption>
        <thead><tr>
          <th scope="col" className="heat-unit">Rp juta · {heat.mode === "SAME_DAY" ? `hari 1–${sameDay.elapsedDays}` : "total siklus"}</th>
          {heat.columns.map(column => <th key={column.start} scope="col" data-selected={column.selected || undefined} data-running={column.untinted || undefined} aria-current={column.selected ? "true" : undefined}>
            <b>{columnMonth(column.start)}</b><small>{column.subhead}</small>
          </th>)}
          <th scope="col" className="heat-delta-head">{deltaLabel}</th>
        </tr></thead>
        <tbody>
          {heat.rows.map(row => <tr key={row.key} data-selected-row={row.selected || undefined}>
            <th scope="row">
              {row.selectable && onSelectCategory
                ? <button type="button" className="heat-row-button" aria-pressed={row.selected} aria-controls="category-drivers" aria-label={`Buka bukti ${row.name}`} onClick={() => onSelectCategory(row.id)}>{row.name}</button>
                : <span data-muted={row.muted || undefined}>{row.name}</span>}
              <small className="heat-row-delta"><Delta value={row.delta} label={deltaLabel}/></small>
            </th>
            {heat.columns.map((column, index) => <td key={column.start} className="heat-amount" data-selected={column.selected || undefined} data-running={column.untinted || undefined}>
              <span className="heat-cell" style={{ "--tint": row.tints[index] }}>{compactMillions(row.amounts[index])}</span>
            </td>)}
            <td className="heat-delta"><Delta value={row.delta}/></td>
          </tr>)}
        </tbody>
        <tfoot><tr>
          <th scope="row">Total pengeluaran bersih</th>
          {heat.columns.map((column, index) => <td key={column.start} className="heat-amount" data-selected={column.selected || undefined} data-running={column.untinted || undefined}>{compactMillions(heat.total.amounts[index])}</td>)}
          <td className="heat-delta"><Delta value={heat.total.delta}/></td>
        </tr></tfoot>
      </table>
    </div>
    <p className="heat-legend">Warna sel: besar relatif dalam satu baris (lebih gelap = lebih besar), bukan baik atau buruk. Klik baris untuk membuka kategori (merchant &amp; transaksi).</p>
  </div>;
}

// Merchant pendukung: one row per payee, largest net amount first, as served.
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
            <td>{item.count ?? "—"}</td>
            <td>{money(item.amount)}</td>
            <td>{amountLabel(item.previous)}</td>
            <td><Delta value={item.deltaVsPrevious}/></td>
          </tr>)}
          {list.other && <tr data-other="true">
            <th scope="row">{name({ ...list.other, name: `Merchant lain · ${list.other.merchantCount}` }, true)}{bar(list.other)}</th>
            <td>{list.other.count ?? "—"}</td>
            <td>{money(list.other.amount)}</td>
            <td>{amountLabel(list.other.previous)}</td>
            <td><Delta value={list.other.deltaVsPrevious}/></td>
          </tr>}
        </tbody>
        {list.total && <tfoot><tr>
          <th scope="row">Total</th>
          <td>{list.total.count ?? "—"}</td>
          <td>{money(list.total.amount)}</td>
          <td>{amountLabel(list.total.previous)}</td>
          <td><Delta value={list.total.deltaVsPrevious}/></td>
        </tr></tfoot>}
      </table>
    </div>
    <a className="card-link" href={transactionHref(period)}>Semua transaksi siklus ini →</a>
  </div>;
}

// Catatan rumah tangga: a 100% bar and a fixed-order list. Never sorted by amount,
// so it cannot read as a ranking; the hues are identity, not data.
export function HouseholdSection({ members = [] }) {
  const rows = householdRows(members);
  return <section id="household" className="review-section" aria-labelledby="household-title">
    <details className="report-disclosure">
      <summary><h2 id="household-title">Catatan rumah tangga</h2><span>{rows.length ? `${rows.length} pencatat` : "Belum ada atribusi"}</span></summary>
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
    </details>
  </section>;
}
