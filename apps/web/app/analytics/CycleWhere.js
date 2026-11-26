import { useEffect, useRef, useState } from "react";
import { CycleSpendingPatternChart } from "../components/Charts";
import { averageDailyText, baselineFor } from "../lib/cycleKpi";
import { buildLedger, compactMillions, signedMillions } from "../lib/cycleLedger";
import { amountLabel, ratioLabel, transactionHref } from "../lib/cycleReview";
import { categoryColorFor, categoryColorMap, categoryKey, OTHER_CATEGORY_COLOR, partialDay } from "../lib/cycleWhere";
import { trapKeyDown } from "../lib/focusTrap";
import { dayLabel } from "../lib/chartData";
import { dateTime, money, typeLabel } from "../lib/format";
import { SectionTitle } from "./shared";
import { CategoryHeatmap } from "./CycleSections";

export function DailyCard({ facts, step }) {
  const average = averageDailyText(facts.spendingShape);
  const shape = facts.spendingShape;
  const partial = partialDay(facts.period, facts.daily);
  const parts = [];
  if (average != null) parts.push(`Rata-rata ${money(average)} per hari`);
  if (shape.peakDay) parts.push(`hari terbesar ${dayLabel(shape.peakDay)}, ${money(shape.peakExpense)} (${ratioLabel(shape.peakShareOfExpense)})`);
  parts.push(`${shape.zeroSpendDays} hari tanpa pengeluaran bersih`);

  return <section id="daily" className="surface dash-card daily-card" aria-labelledby="daily-title">
    <div className="dash-card-head">
      <div>
        <SectionTitle id="daily-title" title="Ritme harian"/>
        <p className="takeaway">{parts.join(" · ")}</p>
      </div>
      <ul className="dash-legend" aria-label="Keterangan grafik">
        <li data-series="current"><i aria-hidden="true"/>Pengeluaran</li>
        {shape.peakDay && <li data-series="peak"><i aria-hidden="true"/>Hari tertinggi</li>}
        {average != null && <li data-series="average"><i aria-hidden="true"/>Rata-rata</li>}
        {partial && <li data-series="partial"><i aria-hidden="true"/>Hari ini, belum penuh</li>}
      </ul>
    </div>
    <CycleSpendingPatternChart items={facts.daily} period={{ ...facts.period, reviewStep: step }} average={average} partialDay={partial} peakDay={shape.peakDay} height={150}/>
    <details className="review-daily" open={step === "spending-shape" || undefined}>
      <summary>Nilai harian › <small>pengeluaran, refund, total sampai hari itu</small></summary>
      <p className="dash-caption">Pengeluaran bersih per hari setelah refund; transfer tidak termasuk. Hari berjalan belum penuh.</p>
      <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Nilai pengeluaran harian, geser untuk semua kolom"><table>
        <caption className="visually-hidden">Pengeluaran bersih harian</caption>
        <thead><tr><th scope="col">Tanggal</th><th scope="col">Pengeluaran</th><th scope="col">Refund</th><th scope="col">Total sampai hari ini</th></tr></thead>
        <tbody>{facts.daily.map(item => <tr key={item.period}>
          <th scope="row">{Number(item.expense) !== 0 ? <a href={transactionHref({ ...facts.period, reviewStep: step }, { from: item.period, to: item.period })} aria-label={`Lihat pengeluaran ${dayLabel(item.period)}`}>{dayLabel(item.period)}</a> : dayLabel(item.period)}</th>
          <td data-label="Pengeluaran">{money(item.expense)}</td>
          <td data-label="Refund">{money(item.refund)}</td>
          <td data-label="Total sampai hari ini">{item.cumulativeExpense == null ? "—" : money(item.cumulativeExpense)}</td>
        </tr>)}</tbody>
      </table></div>
    </details>
  </section>;
}

export function HistoryCard({ facts, step, selectedCategory, onSelect, onSelectCategory, palette = null }) {
  const sameDay = facts.categoryHistory?.sameDay;
  const early = facts.period?.state === "ACTIVE" && facts.spendingShape?.days <= 3;
  const defaultMode = sameDay && !early ? "SAME_DAY" : "FULL";
  const [mode, setMode] = useState(defaultMode);
  useEffect(() => { setMode(defaultMode); }, [facts.period.start, defaultMode]);

  const effectiveMode = sameDay && !early ? mode : "FULL";
  const colorMap = categoryColorMap(facts, palette);
  const ledger = buildLedger(facts.history, facts.period.start, facts.categoryHistory, colorMap, effectiveMode);
  const columns = ledger.columns;
  const detail = useRef(null);
  useEffect(() => {
    const reveal = () => { if (detail.current && ["#history", "#matrix"].includes(window.location.hash)) detail.current.open = true; };
    reveal();
    window.addEventListener("hashchange", reveal);
    return () => window.removeEventListener("hashchange", reveal);
  }, []);
  if (!columns.length) return null;
  const isSameDay = ledger.mode === "SAME_DAY";
  return <section id="history" className="surface dash-card history-card" aria-labelledby="history-title">
    <div className="dash-card-head">
      <div>
        <SectionTitle id="history-title" title="Siklus ke siklus"/>
        {facts.takeaways?.history && <p className="takeaway">{facts.takeaways.history}</p>}
      </div>
      {sameDay && <div className="segmented history-mode-toggle" data-slide role="group" aria-label="Rentang siklus ke siklus">
        <button type="button" className={isSameDay ? "active" : "secondary"} aria-pressed={isSameDay} disabled={early} onClick={() => setMode("SAME_DAY")}>Hari 1–{sameDay.elapsedDays}</button>
        <button type="button" className={!isSameDay ? "active" : "secondary"} aria-pressed={!isSameDay} onClick={() => setMode("FULL")}>Siklus penuh</button>
      </div>}
    </div>
    <div className="history-scroll" tabIndex={0} role="region" aria-label="Pengeluaran bersih per siklus, geser untuk semua siklus">
      <ul className="history-cols" aria-label="Pengeluaran bersih per siklus">
        {columns.map(column => <li key={column.start} data-selected={column.selected || undefined} data-running={column.running || undefined}>
          <button type="button" aria-pressed={column.selected} aria-label={`Buka siklus ${column.label} ${column.until}: pengeluaran ${money(column.expense)}`} onClick={() => onSelect(column.start)}>
            <em>{compactMillions(column.expense)}</em>
            <span className="history-plot" aria-hidden="true">
              <span key={`${column.start}:${ledger.mode}:${column.expense}:${column.expenseLength}`} className="history-bar" style={{ height: `${column.expenseLength * 100}%` }}>
                {column.segments.length
                  ? column.segments.map(segment => <i key={segment.key} className="history-seg" style={{ flexGrow: segment.weight, background: segment.color }}/>)
                  : <i className="history-seg" style={{ flexGrow: 1 }}/>}
              </span>
            </span>
            <b>{column.label}</b>
            <span className="history-meta">
              {column.running
                ? <small>berjalan</small>
                : isSameDay
                  ? <small>hari 1–{column.elapsedDays}</small>
                  : <>
                    {column.averageDailyExpenseRounded != null && <small>{money(column.averageDailyExpenseRounded)}/hr</small>}
                    <small>arus kas {signedMillions(column.net)}</small>
                  </>}
            </span>
          </button>
        </li>)}
      </ul>
    </div>
    <p className="dash-caption">Pengeluaran bersih, Rp juta · warna kategori sama dengan Ke mana uangnya</p>
    <details className="review-daily" ref={detail} open={step === "position" || undefined}>
      <summary>Angka per kategori › <small>kategori × siklus, Rp juta</small></summary>
      <div id="matrix" className="matrix-detail">
        <h3 id="matrix-title">Angka per kategori</h3>
        <p className="review-description">{facts.history.length} siklus · {facts.categoryHistory?.rows?.length || 0} kategori + kategori lain</p>
        <CategoryHeatmap key={facts.period.start} facts={facts} mode={effectiveMode} colors={colorMap.colors} selectedCategory={selectedCategory} onSelectCategory={onSelectCategory}/>
      </div>
    </details>
  </section>;
}

export function CategoryDrawer({ item, facts, period, close, triggerRef, palette = null }) {
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
      const active = document.activeElement;
      if (active && active !== document.body && !drawer?.contains(active)) return;
      const target = opener instanceof HTMLElement && opener.isConnected ? opener : document.getElementById("where-title");
      target?.focus?.();
    };
  }, [triggerRef]);
  const basis = baselineFor(period, facts.spendingShape?.days);
  const key = categoryKey(item);
  const resolvedColor = categoryColorMap(facts, palette).colors.get(key) || categoryColorFor(item, palette);
  const swatchColor = resolvedColor && resolvedColor !== OTHER_CATEGORY_COLOR ? resolvedColor : "";
  return <div id="drivers" className="drawer-backdrop category-backdrop" onClick={() => close()}>
    <aside ref={ref} id="category-drawer" tabIndex={-1} className="detail-drawer category-drawer" role="dialog" aria-modal="true" aria-labelledby="drivers-title" onClick={event => event.stopPropagation()}>
      <button type="button" className="drawer-close" aria-label="Tutup rincian kategori" onClick={() => close()}>×</button>
      <h2 id="drivers-title" tabIndex={-1} className="category-drawer-title">{swatchColor && <i className="cat-swatch" aria-hidden="true" style={{ "--cat": swatchColor }}/>}{item.name}</h2>
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
