import { useEffect, useState } from "react";
import { isComparisonTooEarly } from "../lib/cycleKpi";
import { ratioLabel, transactionHref } from "../lib/cycleReview";
import { calendarCategoryView } from "../lib/calendarReview";
import { categoryColorMap, changesView, nonZeroCategoryChanges, OTHER_CATEGORY_COLOR, rankingView } from "../lib/cycleWhere";
import { money } from "../lib/format";
import { ComparisonContext, ChangesTable } from "./CycleSections";
import { SectionTitle } from "./shared";

function DeltaCell({ delta, hasBaseline, early }) {
  if (early) return <span className="cat-delta" data-tone="none">mulai hari ke-4</span>;
  if (!hasBaseline || !delta || delta.empty) return <span className="cat-delta" data-tone="none">—</span>;
  if (delta.zero) return <span className="cat-delta" data-tone="none">sama</span>;
  return <span className="cat-delta" data-tone={delta.tone}>
    {delta.mark && <span aria-hidden="true">{delta.mark} </span>}
    {delta.word && <span className="visually-hidden">{delta.word} </span>}
    {delta.text}{delta.percent && ` ${delta.percent}`}
  </span>;
}

function RowCells({ row, hasBaseline = false, early = false, swatch = true, showDelta = true }) {
  const showSwatch = swatch && Boolean(row.color);
  const barColor = row.color || OTHER_CATEGORY_COLOR;
  return <>
    <span className="cat-name">
      {showSwatch ? <i className="cat-swatch" style={{ "--cat": row.color }} aria-hidden="true"/> : <span className="cat-swatch-gap" aria-hidden="true"/>}
      <span className="cat-name-text">{row.name}{row.expandable && <span className="cat-expand-mark" aria-hidden="true"> ›</span>}</span>
    </span>
    <span className="cat-share-bar" aria-hidden="true"><i style={{ width: `${row.length * 100}%`, background: barColor }}/></span>
    <span className="cat-amount">{money(row.amount)}</span>
    <span className="cat-pct">{ratioLabel(row.shareOfExpense)}</span>
    {showDelta && <DeltaCell delta={row.delta} hasBaseline={hasBaseline} early={early}/>}
  </>;
}

function stackSegments(rows, other = null) {
  const segments = [];
  let otherWeight = other ? Number(other.amount) || 0 : 0;
  for (const row of rows) {
    const weight = Number(row.amount) || 0;
    if (!row.color || row.color === OTHER_CATEGORY_COLOR) otherWeight += weight;
    else if (weight > 0) segments.push({ key: row.key, amount: weight, color: row.color });
  }
  if (otherWeight > 0) segments.push({ key: "other", amount: otherWeight, color: OTHER_CATEGORY_COLOR });
  return segments;
}

export default function CategoryTable({ facts, step, selected = "", select, palette = null }) {
  const [mode, setMode] = useState("top");
  useEffect(() => { setMode("top"); }, [facts.period.start]);

  const ranking = rankingView(facts, palette);
  const changes = changesView(facts, { limit: 8, palette });
  const early = isComparisonTooEarly(facts);
  const canChange = changes.hasBaseline && !early;
  const activeMode = step === "changes" ? "change" : step === "drivers" ? "top" : (canChange ? mode : "top");
  const isChangeMode = activeMode === "change";
  const colorMap = categoryColorMap(facts, palette);

  const segments = ranking && (ranking.rows.length || ranking.other) ? stackSegments(ranking.rows, ranking.other) : [];

  const rows = isChangeMode ? changes.rows : (ranking?.rows || []);
  const other = isChangeMode ? null : ranking?.other;
  const empty = isChangeMode ? !rows.length : (!ranking || (!rows.length && !other));
  const otherSelected = Boolean(selected && other?.rows?.some(sub => sub.key === selected));
  const takeaway = isChangeMode ? facts.takeaways?.changes : facts.takeaways?.where;
  const baselineHeading = early ? "perbandingan mulai hari ke-4" : changes.hasBaseline ? changes.baseline : "belum ada pembanding";
  const emptyMessage = isChangeMode
    ? (changes.hasBaseline ? "Belum ada perubahan kategori pada periode ini." : "Belum ada siklus pembanding.")
    : "Belum ada pengeluaran bersih pada periode ini.";

  return <section id="where" className="surface dash-card category-card" aria-labelledby="where-title">
    <div className="dash-card-head">
      <div>
        <SectionTitle id="where-title" title="Ke mana uangnya"/>
        {takeaway && <p className="takeaway">{takeaway}</p>}
      </div>
      <div className="segmented" data-slide role="group" aria-label="Urutan kategori">
        <button type="button" className={activeMode === "top" ? "active" : "secondary"} aria-pressed={activeMode === "top"} disabled={step === "changes"} onClick={() => setMode("top")}>Terbesar</button>
        <button type="button" className={activeMode === "change" ? "active" : "secondary"} aria-pressed={activeMode === "change"} disabled={!canChange || step === "drivers"} onClick={() => setMode("change")}>Paling berubah</button>
      </div>
    </div>
    {step === "drivers" && <p className="dash-caption">Pilih kategori untuk membuka rincian merchant dan transaksi.</p>}
    {segments.length > 0 && <div key={segments.map(segment => `${segment.key}:${segment.amount}`).join("|")} className="cat-stack" role="img" aria-label="Porsi pengeluaran bersih menurut kategori">{segments.map(segment => <i key={segment.key} style={{ flexGrow: segment.amount, background: segment.color }}/>)}</div>}
    {empty ? <p className="empty compact">{emptyMessage}</p> : <>
      <div className="cat-row cat-row-head" aria-hidden="true">
        <span>Kategori</span>
        <span>Porsi</span>
        <span className="cat-head-num">Jumlah</span>
        <span className="cat-head-num">%</span>
        <span className="cat-head-num">{baselineHeading}</span>
      </div>
      <ol className="cat-list" aria-label={isChangeMode ? "Kategori paling berubah" : "Kategori menurut jumlah"}>
        {rows.map(row => <li key={row.key}>
          {row.openable
            ? <button type="button" className="cat-row" aria-haspopup="dialog" aria-controls="category-drawer" onClick={event => select(row.key, event.currentTarget)}><RowCells row={row} hasBaseline={changes.hasBaseline} early={early}/></button>
            : <div className="cat-row"><RowCells row={row} hasBaseline={changes.hasBaseline} early={early}/></div>}
        </li>)}
        {other && <li data-other="">
          {other.rows?.length > 0
            ? <details className="cat-other-details" open={otherSelected || undefined}>
              <summary className="cat-row cat-other-summary"><RowCells row={{ ...other, name: `${other.name} · ${other.categoryCount || other.rows.length}`, expandable: true }} hasBaseline={changes.hasBaseline} early={early}/></summary>
              <ol className="cat-other-list" aria-label="Rincian kategori lain">
                {other.rows.map(sub => <li key={sub.key}>
                  {sub.openable
                    ? <button type="button" className="cat-row cat-subrow" aria-haspopup="dialog" aria-controls="category-drawer" onClick={event => select(sub.key, event.currentTarget)}><RowCells row={sub} hasBaseline={changes.hasBaseline} early={early} swatch={false}/></button>
                    : <div className="cat-row cat-subrow"><RowCells row={sub} hasBaseline={changes.hasBaseline} early={early} swatch={false}/></div>}
                </li>)}
              </ol>
            </details>
            : <div className="cat-row"><RowCells row={{ ...other, name: `${other.name} · ${other.categoryCount}` }} hasBaseline={changes.hasBaseline} early={early}/></div>}
        </li>}
      </ol>
    </>}
    <div className="card-foot">
      <a className="card-link" href={transactionHref(facts.period)}>Semua transaksi →</a>
      <p className="cat-total"><span>Total</span><strong>{money(facts.cashflow.expense)}</strong></p>
    </div>
    <details id="changes" className="review-daily" open={step === "changes" || undefined}>
      <summary>Perbandingan kategori yang berubah › <small>median, persentase &amp; kontribusi</small></summary>
      <p className="dash-caption">Urutan dari server, bukan penilaian baik atau buruk. Merah dan hijau hanya menunjukkan arah pengeluaran. Satu pembanding: {changes.baseline}.</p>
      <ComparisonContext comparison={facts.comparison} period={facts.period}/>
      <ChangesTable items={nonZeroCategoryChanges(facts)} comparison={facts.comparison} selected={selected} select={select} colors={colorMap.colors}/>
    </details>
  </section>;
}

export function CalendarCategoryTable({ items = [], total = null, rangeHref = "", palette = null, stale = false }) {
  const { rows } = calendarCategoryView(items, palette);
  const segments = rows.length ? stackSegments(rows) : [];
  return <section id="where" className="surface dash-card category-card calendar-section" data-calendar="" data-stale={stale ? "true" : undefined} aria-busy={stale || undefined} aria-labelledby="where-title">
    <div className="dash-card-head">
      <div>
        <SectionTitle id="where-title" title="Ke mana uangnya"/>
      </div>
    </div>
    {segments.length > 0 && <div key={segments.map(segment => `${segment.key}:${segment.amount}`).join("|")} className="cat-stack" role="img" aria-label="Porsi pengeluaran bersih menurut kategori">{segments.map(segment => <i key={segment.key} style={{ flexGrow: segment.amount, background: segment.color }}/>)}</div>}
    {!rows.length ? <p className="empty compact">Belum ada pengeluaran bersih pada rentang ini.</p> : <>
      <div className="cat-row cat-row-head" aria-hidden="true">
        <span>Kategori</span>
        <span>Porsi</span>
        <span className="cat-head-num">Jumlah</span>
        <span className="cat-head-num">%</span>
      </div>
      <ol className="cat-list" aria-label="Kategori menurut jumlah">
        {rows.map(row => <li key={row.key}><div className="cat-row"><RowCells row={row} showDelta={false}/></div></li>)}
      </ol>
    </>}
    <p className="dash-caption">Pengeluaran bersih setelah refund, diurutkan dari server menurut jumlah.</p>
    <div className="card-foot">
      {rangeHref && <a className="card-link" href={rangeHref}>Semua transaksi →</a>}
      {total != null && <p className="cat-total"><span>Total</span><strong>{money(total)}</strong></p>}
    </div>
  </section>;
}
