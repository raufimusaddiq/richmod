"use client";

import { useEffect, useRef } from "react";
import { CycleSpendingPatternChart } from "../components/Charts";
import { baselineFor } from "../lib/cycleKpi";
import { buildLedger, compactMillions, signedMillions } from "../lib/cycleLedger";
import { amountLabel, ratioLabel, transactionHref } from "../lib/cycleReview";
import { changesView, categoryKey, partialDay, rankingView } from "../lib/cycleWhere";
import { trapKeyDown } from "../lib/focusTrap";
import { dayLabel } from "../lib/chartData";
import { dateTime, money, typeLabel } from "../lib/format";
import { averageDailyText } from "../lib/cycleKpi";
import { SectionTitle } from "./shared";

function Delta({ delta }) {
  return <span className="delta" data-tone={delta.tone}>
    {delta.mark && <span aria-hidden="true">{delta.mark} </span>}{delta.word && <span className="visually-hidden">{delta.word} </span>}{delta.text}{delta.percent && ` ${delta.percent}`}
  </span>;
}

// "Ke mana uangnya": the served amount ranking. A row opens the category drawer.
export function WhereCard({ facts, step, select, openComparison }) {
  const view = rankingView(facts);
  const count = facts.categoryChanges.length;
  return <section id="where" className="dash-card where-card" aria-labelledby="where-title">
    <SectionTitle id="where-title" about="peringkat kategori" title="Ke mana uangnya" description="Pengeluaran bersih setelah refund, diurutkan menurut jumlah oleh server. Selisih dibandingkan dengan siklus lalu pada hari yang sama saat siklus berjalan, atau siklus lalu penuh setelah ditutup."/>
    <p className="dash-sub">Pengeluaran bersih per kategori · {view ? view.baseline : "pembanding belum tersedia"}</p>
    {!view || (!view.rows.length && !view.other) ? <p className="empty compact">Belum ada pengeluaran bersih pada periode ini.</p> : <ol className="where-list" aria-label="Kategori menurut jumlah">
      {view.rows.map(row => <li key={row.key}>
        {row.openable
          ? <button type="button" className="where-row" aria-haspopup="dialog" aria-controls="category-drawer" onClick={event => select(row.key, event.currentTarget)}><RankRow row={row}/></button>
          : <div className="where-row"><RankRow row={row}/></div>}
      </li>)}
      {view.other && <li data-other><div className="where-row"><RankRow row={{ ...view.other, name: `${view.other.name} · ${view.other.categoryCount}` }}/></div></li>}
    </ol>}
    {step === "drivers" && <p className="dash-caption">Pilih kategori untuk membuka rincian merchant dan transaksi.</p>}
    <div className="card-foot">
      <button type="button" className="text-button" onClick={openComparison}>Semua {count} kategori</button>
      <a className="card-link" href={transactionHref(facts.period)}>Semua transaksi →</a>
      <p className="where-total"><span>Total</span><strong>{money(facts.cashflow.expense)}</strong></p>
    </div>
  </section>;
}

function RankRow({ row }) {
  return <>
    <span className="where-name">{row.name}</span>
    <span className="where-amount">{money(row.amount)}</span>
    <span className="where-bar" aria-hidden="true"><i style={{ width: `${row.length * 100}%` }}/></span>
    <span className="where-delta"><Delta delta={row.delta}/></span>
  </>;
}

// "Yang berubah": first five served changes as diverging bars, merchant movers, and the full comparison on demand.
export function ChangesCard({ facts, step, select, comparisonRef, children }) {
  const view = changesView(facts);
  return <section id="changes" className="dash-card changes-card" aria-labelledby="changes-title">
    <SectionTitle id="changes-title" about="perubahan" title="Yang berubah" description="Lima perubahan kategori terbesar menurut urutan server, bukan penilaian baik atau buruk. Merchant penggerak diurutkan menurut selisih absolut."/>
    <p className="dash-sub">Selisih {view.baseline}</p>
    {!view.hasBaseline ? <p className="empty compact">Belum ada siklus pembanding.</p> : !view.rows.length ? <p className="empty compact">Belum ada perubahan kategori pada periode ini.</p> : <>
      <p className="changes-hint" aria-hidden="true"><span>◀ lebih hemat</span><span>lebih boros ▶</span></p>
      <ul className="diverge" aria-label="Perubahan kategori terbesar">
        {view.rows.map(row => <li key={row.key} data-side={row.side || undefined}>
          <button type="button" className="diverge-row" aria-haspopup="dialog" aria-controls="category-drawer" onClick={event => select(row.key, event.currentTarget)}>
            <span className="diverge-name">{row.name}</span>
            <span className="diverge-track" aria-hidden="true"><i style={{ width: `calc(${row.width} / 2)` }}/></span>
            <span className="diverge-value"><Delta delta={row.delta}/></span>
          </button>
        </li>)}
      </ul>
      {view.merchants.length > 0 && <div className="movers"><h3>Merchant penggerak</h3>
        <ul>{view.merchants.map(item => <li key={item.key}>{item.id ? <a href={transactionHref(facts.period, { merchantId: item.id })}>{item.name}</a> : <span>{item.name}</span>}<Delta delta={item.delta}/></li>)}</ul>
      </div>}
    </>}
    <details className="review-daily" ref={comparisonRef} open={step === "changes" || undefined}>
      <summary>Perbandingan lengkap · median, persentase & kontribusi</summary>
      {children}
    </details>
  </section>;
}

// Daily spending (2/3). Average sits in the header, so the reference line carries no label over the bars.
export function DailyCard({ facts, step, children }) {
  const average = averageDailyText(facts.spendingShape);
  const shape = facts.spendingShape;
  const partial = partialDay(facts.period, facts.daily);
  return <section id="daily" className="dash-card daily-card" aria-labelledby="daily-title">
    <div className="dash-card-head">
      <div>
        <SectionTitle id="daily-title" about="pengeluaran harian" title="Pengeluaran harian" description="Nilai harian sudah dikurangi refund; transfer tidak termasuk. Hari berjalan belum penuh."/>
        <p className="dash-sub">Pengeluaran bersih per hari, Rp juta</p>
      </div>
      <ul className="dash-legend" aria-label="Keterangan grafik">
        <li data-series="current"><i aria-hidden="true"/>Pengeluaran</li>
        {average != null && <li data-series="average"><i aria-hidden="true"/>Rata-rata {money(average)}</li>}
        {partial && <li data-series="partial"><i aria-hidden="true"/>Hari ini, belum penuh</li>}
      </ul>
    </div>
    <CycleSpendingPatternChart items={facts.daily} period={{ ...facts.period, reviewStep: step }} average={average} partialDay={partial} height={190}/>
    <p className="dash-caption">{shape.peakDay ? `Hari tertinggi ${dayLabel(shape.peakDay)} · ${money(shape.peakExpense)} (${ratioLabel(shape.peakShareOfExpense)})` : "Belum ada hari tertinggi"} · {shape.zeroSpendDays} hari tanpa pengeluaran bersih</p>
    {children}
  </section>;
}

// "Siklus ke siklus": one column per served cycle; choosing a column opens that cycle.
export function HistoryCard({ facts, onSelect }) {
  const columns = buildLedger(facts.history, facts.period.start, null).columns;
  if (!columns.length) return null;
  return <section id="history" className="dash-card history-card" aria-labelledby="history-title">
    <SectionTitle id="history-title" about="riwayat siklus" title="Siklus ke siklus" description="Pengeluaran bersih setiap siklus gaji. Siklus berjalan belum penuh dan tidak sebanding dengan siklus yang sudah ditutup."/>
    <p className="dash-sub">Pengeluaran bersih, Rp juta</p>
    <ul className="history-cols" aria-label="Pengeluaran bersih per siklus">
      {columns.map(column => <li key={column.start} data-selected={column.selected || undefined} data-running={column.running || undefined}>
        <button type="button" aria-pressed={column.selected} aria-label={`Buka siklus ${column.label} ${column.until}: pengeluaran ${money(column.expense)}`} onClick={() => onSelect(column.start)}>
          <em>{compactMillions(column.expense)}</em>
          <span className="history-plot" aria-hidden="true"><i style={{ height: `${column.expenseLength * 100}%` }}/></span>
          <b>{column.label}</b>
          <small>{column.running ? "berjalan" : signedMillions(column.net)}</small>
        </button>
      </li>)}
    </ul>
    <p className="dash-caption">Angka di bawah tanggal: arus kas bersih, Rp juta. Siklus berjalan belum penuh.</p>
  </section>;
}

// Right drawer (bottom sheet on phones). Totals are the served row, so they equal the list it was opened from.
export function CategoryDrawer({ item, facts, period, close, triggerRef }) {
  const ref = useRef(null);
  const closeRef = useRef(close);
  closeRef.current = close;
  useEffect(() => {
    const opener = triggerRef?.current instanceof HTMLElement ? triggerRef.current : document.activeElement;
    const drawer = ref.current;
    drawer?.focus();
    const onKeyDown = event => trapKeyDown(event, drawer, () => closeRef.current());
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      const target = opener instanceof HTMLElement && opener.isConnected ? opener : document.getElementById("where-title");
      target?.focus?.();
    };
  }, [triggerRef]);
  const basis = baselineFor(period, facts.spendingShape?.days);
  const key = categoryKey(item);
  return <div id="drivers" className="drawer-backdrop category-backdrop" onClick={() => close()}>
    <aside ref={ref} id="category-drawer" tabIndex={-1} className="detail-drawer category-drawer" role="dialog" aria-modal="true" aria-labelledby="drivers-title" onClick={event => event.stopPropagation()}>
      <button type="button" className="drawer-close" aria-label="Tutup rincian kategori" onClick={() => close()}>×</button>
      <h2 id="drivers-title" tabIndex={-1}>{item.name}</h2>
      <p className="detail-amount">{money(item.amount)}</p>
      <dl className="drawer-facts">
        <div><dt>Porsi pengeluaran</dt><dd>{ratioLabel(item.shareOfExpense)}</dd></div>
        <div><dt>Transaksi</dt><dd>{item.count}</dd></div>
        <div><dt>{basis.label.replace(/^vs /, "Sebelumnya, ")}</dt><dd>{amountLabel(item[basis.field])}</dd></div>
        <div><dt>Selisih</dt><dd>{amountLabel(item[basis.delta])} · {ratioLabel(item[basis.relative])}</dd></div>
      </dl>
      <h3>Merchant</h3>
      {item.merchants.length ? <ul className="drawer-list">{item.merchants.map((merchant, index) => <li key={`${merchant.id}:${index}`}>{merchant.id ? <a href={transactionHref(period, { merchantId: merchant.id, categoryId: key })}>{merchant.name}</a> : <span>{merchant.name}</span>}<strong>{money(merchant.amount)}</strong></li>)}</ul> : <p className="empty compact">Belum ada merchant pendukung.</p>}
      <h3>Transaksi terbesar</h3>
      {item.transactions.length ? <ul className="drawer-list">{item.transactions.map(transaction => <li key={transaction.id}><span>{transaction.merchant}<small>{dateTime(transaction.transactionAt)} · {typeLabel[transaction.type]}</small></span><strong>{money(transaction.amount)}</strong><a className="drawer-open" href={transactionHref(period, { categoryId: key, id: transaction.id })} aria-label={`Buka transaksi ${transaction.merchant}`}>Buka transaksi</a></li>)}</ul> : <p className="empty compact">Tidak ada transaksi terkonfirmasi kategori ini pada siklus terpilih.</p>}
      <a className="card-link" href={transactionHref(period, { categoryId: key })}>Lihat transaksi {item.name} dalam periode ini →</a>
    </aside>
  </div>;
}
