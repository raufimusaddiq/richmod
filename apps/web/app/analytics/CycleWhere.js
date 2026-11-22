import { useEffect, useRef, useState } from "react";
import { CycleSpendingPatternChart } from "../components/Charts";
import { baselineFor } from "../lib/cycleKpi";
import { buildLedger, compactMillions, signedMillions } from "../lib/cycleLedger";
import { amountLabel, ratioLabel, transactionHref } from "../lib/cycleReview";
import { changesView, categoryColorFor, categoryColorMap, categoryKey, OTHER_CATEGORY_COLOR, partialDay, rankingView } from "../lib/cycleWhere";
import { trapKeyDown } from "../lib/focusTrap";
import { dayLabel } from "../lib/chartData";
import { dateTime, money, typeLabel } from "../lib/format";
import { averageDailyText } from "../lib/cycleKpi";
import { SectionTitle } from "./shared";
import { CategoryHeatmap, MerchantRanking } from "./CycleSections";

function Delta({ delta }) {
  return <span className="delta" data-tone={delta.tone}>
    {delta.mark && <span aria-hidden="true">{delta.mark} </span>}{delta.word && <span className="visually-hidden">{delta.word} </span>}{delta.text}{delta.percent && ` ${delta.percent}`}
  </span>;
}

export function WhereCard({ facts, step, select, palette = null }) {
  const view = rankingView(facts, palette);
  const empty = !view || (!view.rows.length && !view.other);
  const segments = [];
  if (!empty) {
    let otherWeight = view.other ? Number(view.other.amount) || 0 : 0;
    for (const row of view.rows) {
      const weight = Number(row.amount) || 0;
      if (!row.color || row.color === OTHER_CATEGORY_COLOR) otherWeight += weight;
      else if (weight > 0) segments.push({ key: row.key, amount: weight, color: row.color });
    }
    if (otherWeight > 0) segments.push({ key: "other", amount: otherWeight, color: OTHER_CATEGORY_COLOR });
  }
  return <section id="where" className="surface dash-card where-card" aria-labelledby="where-title">
    <SectionTitle id="where-title" title="Ke mana uangnya"/>
    {facts.takeaways?.where && <p className="takeaway">{facts.takeaways.where}</p>}
    <p className="dash-sub">Pengeluaran bersih per kategori</p>
    {empty ? <p className="empty compact">Belum ada pengeluaran bersih pada periode ini.</p> : <>
      <div key={segments.map(segment => `${segment.key}:${segment.amount}`).join("|")} className="where-stack" role="img" aria-label="Porsi pengeluaran bersih menurut kategori">{segments.map(segment => <i key={segment.key} style={{ flexGrow: segment.amount, background: segment.color }}/>)}</div>
      <ol className="where-list" aria-label="Kategori menurut jumlah">
        {view.rows.map(row => <li key={row.key} style={row.color ? { "--cat": row.color } : undefined}>
          {row.openable
            ? <button type="button" className="where-row" aria-haspopup="dialog" aria-controls="category-drawer" onClick={event => select(row.key, event.currentTarget)}><RankRow row={row}/></button>
            : <div className="where-row"><RankRow row={row}/></div>}
        </li>)}
        {view.other && <li data-other style={{ "--cat": OTHER_CATEGORY_COLOR }}>
          {view.other.rows?.length > 0
            ? <details className="where-other-details">
              <summary className="where-row where-other-summary"><RankRow row={{ ...view.other, name: `${view.other.name} · ${view.other.categoryCount || view.other.rows.length}`, expandable: true }}/></summary>
              <ol className="where-other-list" aria-label="Rincian kategori lain">
                {view.other.rows.map(sub => <li key={sub.key}>
                  {sub.openable
                    ? <button type="button" className="where-row where-subrow" aria-haspopup="dialog" aria-controls="category-drawer" onClick={event => select(sub.key, event.currentTarget)}><RankRow row={sub} swatch={false}/></button>
                    : <div className="where-row where-subrow"><RankRow row={sub} swatch={false}/></div>}
                </li>)}
              </ol>
            </details>
            : <div className="where-row"><RankRow row={{ ...view.other, name: `${view.other.name} · ${view.other.categoryCount}` }}/></div>}
        </li>}
      </ol>
    </>}
    {step === "drivers" && <p className="dash-caption">Pilih kategori untuk membuka rincian merchant dan transaksi.</p>}
    <details className="review-daily" open={step === "drivers" || undefined}>
      <summary>Per kategori &amp; merchant ›</summary>
      <p className="dash-caption">Pengeluaran bersih setelah refund, diurutkan menurut jumlah oleh server.</p>
      <div id="merchants" className="merchant-detail">
        <h3 id="merchants-title">Merchant pendukung</h3>
        <p className="review-description">{facts.merchantRanking?.rows?.length || 0} teratas · satu baris per penerima. Pengeluaran bersih setelah refund, terbesar dulu.</p>
        <MerchantRanking ranking={facts.merchantRanking} period={facts.period}/>
      </div>
    </details>
    <div className="card-foot">
      <a className="card-link" href={transactionHref(facts.period)}>Semua transaksi →</a>
      <p className="where-total"><span>Total</span><strong>{money(facts.cashflow.expense)}</strong></p>
    </div>
  </section>;
}

function RankRow({ row, swatch = true }) {
  const showSwatch = swatch && Boolean(row.color);
  return <>
    {showSwatch ? <i className="where-swatch" aria-hidden="true"/> : <span className="where-swatch-gap" aria-hidden="true"/>}
    <span className="where-name">{row.name}{row.expandable && <span className="where-expand-mark" aria-hidden="true"> ›</span>}</span>
    <span className="where-amount">{money(row.amount)}</span>
    <span className="where-share">{ratioLabel(row.shareOfExpense)}</span>
  </>;
}

export function ChangesCard({ facts, step, select, children }) {
  const view = changesView(facts);
  return <section id="changes" className="surface dash-card changes-card" aria-labelledby="changes-title">
    <SectionTitle id="changes-title" title="Yang berubah"/>
    {facts.takeaways?.changes && <p className="takeaway">{facts.takeaways.changes}</p>}
    <p className="dash-sub">Selisih {view.baseline}</p>
    {!view.hasBaseline ? <p className="empty compact">Belum ada siklus pembanding.</p> : !view.rows.length ? <p className="empty compact">Belum ada perubahan kategori pada periode ini.</p> : <>
      <p className="changes-hint" aria-hidden="true"><span>◀ lebih rendah</span><span>lebih tinggi ▶</span></p>
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
    <details className="review-daily" open={step === "changes" || undefined}>
      <summary>Detail ›<span className="visually-hidden"> Perbandingan lengkap · median, persentase &amp; kontribusi</span></summary>
      <p className="dash-caption">Lima perubahan kategori terbesar menurut urutan server, bukan penilaian baik atau buruk. Merah dan hijau hanya menunjukkan arah pengeluaran. Merchant penggerak diurutkan menurut selisih absolut.</p>
      {children}
    </details>
  </section>;
}

export function DailyCard({ facts, step, children }) {
  const average = averageDailyText(facts.spendingShape);
  const shape = facts.spendingShape;
  const partial = partialDay(facts.period, facts.daily);
  const [open, setOpen] = useState(step === "spending-shape");
  useEffect(() => { if (step === "spending-shape") setOpen(true); }, [step]);
  return <section id="daily" className="review-section" aria-labelledby="daily-title">
    <details className="report-disclosure" open={open} onToggle={event => setOpen(event.currentTarget.open)}>
      <summary><h2 id="daily-title" tabIndex={-1}>Pengeluaran harian</h2><span>{average != null ? `rata-rata ${money(average)} · ` : ""}{shape.peakDay ? `hari terbesar ${dayLabel(shape.peakDay)}` : "belum ada hari tertinggi"}</span></summary>
      {open && <div className="report-disclosure-body">
        <p className="review-description">Pengeluaran bersih per hari, Rp juta. Nilai harian sudah dikurangi refund; transfer tidak termasuk. Hari berjalan belum penuh.</p>
        <ul className="dash-legend" aria-label="Keterangan grafik">
          <li data-series="current"><i aria-hidden="true"/>Pengeluaran</li>
          {average != null && <li data-series="average"><i aria-hidden="true"/>Rata-rata {money(average)}</li>}
          {partial && <li data-series="partial"><i aria-hidden="true"/>Hari ini, belum penuh</li>}
        </ul>
        <CycleSpendingPatternChart items={facts.daily} period={{ ...facts.period, reviewStep: step }} average={average} partialDay={partial} height={190}/>
        <p className="dash-caption">{shape.peakDay ? `Hari tertinggi ${dayLabel(shape.peakDay)} · ${money(shape.peakExpense)} (${ratioLabel(shape.peakShareOfExpense)})` : "Belum ada hari tertinggi"} · {shape.zeroSpendDays} hari tanpa pengeluaran bersih</p>
        {children}
      </div>}
    </details>
  </section>;
}

export function HistoryCard({ facts, step, selectedCategory, onSelect, onSelectCategory, palette = null }) {
  const colorMap = categoryColorMap(facts, palette);
  const columns = buildLedger(facts.history, facts.period.start, facts.categoryHistory, colorMap).columns;
  const detail = useRef(null);
  useEffect(() => {
    const reveal = () => { if (detail.current && ["#history", "#matrix"].includes(window.location.hash)) detail.current.open = true; };
    reveal();
    window.addEventListener("hashchange", reveal);
    return () => window.removeEventListener("hashchange", reveal);
  }, []);
  if (!columns.length) return null;
  return <section id="history" className="surface dash-card history-card" aria-labelledby="history-title">
    <SectionTitle id="history-title" title="Siklus ke siklus"/>
    {facts.takeaways?.history && <p className="takeaway">{facts.takeaways.history}</p>}
    <p className="dash-sub">Pengeluaran bersih per kategori, Rp juta</p>
    <ul className="history-cols" aria-label="Pengeluaran bersih per siklus">
      {columns.map(column => <li key={column.start} data-selected={column.selected || undefined} data-running={column.running || undefined}>
        <button type="button" aria-pressed={column.selected} aria-label={`Buka siklus ${column.label} ${column.until}: pengeluaran ${money(column.expense)}`} onClick={() => onSelect(column.start)}>
          <em>{compactMillions(column.expense)}</em>
          <span className="history-plot" aria-hidden="true">
            <span key={`${column.start}:${column.expense}:${column.expenseLength}`} className="history-bar" style={{ height: `${column.expenseLength * 100}%` }}>
              {column.segments.length
                ? column.segments.map(segment => <i key={segment.key} className="history-seg" style={{ flexGrow: segment.weight, background: segment.color }}/>)
                : <i className="history-seg" style={{ flexGrow: 1 }}/>}
            </span>
          </span>
          <b>{column.label}</b>
          <small>{column.running ? "berjalan" : signedMillions(column.net)}</small>
        </button>
      </li>)}
    </ul>
    <p className="dash-caption">Warna kategori sama dengan Ke mana uangnya. Angka di bawah tanggal: arus kas bersih, Rp juta.</p>
    <details className="review-daily" ref={detail} open={step === "position" || undefined}>
      <summary>Angka per kategori ›</summary>
      <div id="matrix" className="matrix-detail">
        <h3 id="matrix-title">Angka per kategori</h3>
        <p className="review-description">{facts.history.length} siklus · {facts.categoryHistory?.rows?.length || 0} kategori + kategori lain</p>
        <CategoryHeatmap key={facts.period.start} facts={facts} colors={colorMap.colors} selectedCategory={selectedCategory} onSelectCategory={onSelectCategory}/>
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
      <h2 id="drivers-title" tabIndex={-1} className="category-drawer-title">{swatchColor && <i className="where-swatch" aria-hidden="true" style={{ "--cat": swatchColor }}/>}{item.name}</h2>
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
