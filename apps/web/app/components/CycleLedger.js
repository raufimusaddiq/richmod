"use client";

import { useEffect, useRef } from "react";
import { buildLedger, buildMatrix, compactMillions, directionMark, signedMillions } from "../lib/cycleLedger";
import { money } from "../lib/format";
import { signedMoney } from "../lib/cycleReview";

// One column per salary cycle. Direction is the sign plus a neutral marker,
// never a colour. All values are served by the API; see lib/cycleLedger.js.
export default function CycleLedger({ history, selected, median, verdict, onSelect, categoryHistory, selectedCategory = "", selectable = [], onSelectCategory }) {
  const ledger = buildLedger(history, selected, median);
  const matrix = buildMatrix(categoryHistory, history, selectedCategory, selectable);
  const frame = useRef(null);
  useEffect(() => {
    const figure = frame.current;
    const active = figure?.querySelector('th[aria-current="true"]');
    if (!figure || !active) return;
    // Start the selected column right after the sticky label (the browser clamps at the end), so no column is cut at the label edge.
    const label = figure.querySelector("thead th:first-child");
    figure.scrollLeft = Math.max(0, active.offsetLeft - (label ? label.offsetWidth : 0));
  }, [selected, history]);
  const anyRunning = ledger.columns.some(column => column.running);
  return <section id="ledger" className="review-section ledger" aria-labelledby="ledger-title">
    <div className="section-title"><h2 id="ledger-title" tabIndex={-1}>Siklus ke siklus</h2></div>
    {verdict.length > 0 && <dl className="ledger-verdict" aria-live="polite">{verdict.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>}
    {ledger.mode === "chart"
      ? <>
        <div ref={frame} className="ledger-figure" tabIndex={0} role="region" aria-label="Perbandingan siklus, geser untuk semua kolom">
          <table className="ledger-table" style={{ "--cols": ledger.columns.length }}>
            <caption className="visually-hidden">Pemasukan, pengeluaran bersih, arus kas bersih, perubahan pengeluaran{matrix.length > 0 && ", dan pengeluaran per kategori"} untuk setiap siklus gaji, dalam jutaan rupiah</caption>
            <thead><tr>
              <th scope="col"><span className="ledger-unit">Rp juta</span></th>
              {ledger.columns.map(column => <th key={column.start} scope="col" aria-current={column.selected ? "true" : undefined} data-selected={column.selected || undefined}>
                <button type="button" aria-pressed={column.selected} onClick={() => onSelect(column.start)}><b>{column.label}</b><small>{column.until}</small></button>
              </th>)}
            </tr></thead>
            <tbody>
              <tr className="ledger-bars">
                <th scope="row">Pemasukan dan pengeluaran</th>
                {ledger.columns.map(column => <td key={column.start} data-selected={column.selected || undefined} data-running={column.running || undefined}>
                  <div className="ledger-pair">
                    <span className="ledger-bar income"><em>{compactMillions(column.income)}</em><i style={{ "--len": column.incomeLength }}/></span>
                    <span className="ledger-bar expense"><em>{compactMillions(column.expense)}</em><i style={{ "--len": column.expenseLength }}/></span>
                  </div>
                  {column.selected && ledger.medianLength != null && <span className="ledger-median" style={{ "--median": ledger.medianLength }}><span className="visually-hidden">Median 3 siklus sebelumnya {compactMillions(median)} juta</span></span>}
                </td>)}
              </tr>
              <tr>
                <th scope="row">Arus kas bersih</th>
                {ledger.columns.map(column => <td key={column.start} data-selected={column.selected || undefined} data-running={column.running || undefined}>{signedMillions(column.net)}</td>)}
              </tr>
              <tr>
                <th scope="row">Selisih pengeluaran dari siklus sebelumnya</th>
                {ledger.columns.map(column => <td key={column.start} data-selected={column.selected || undefined}>{column.running ? "—" : <><span aria-hidden="true">{directionMark(column.delta)} </span>{signedMillions(column.delta)}</>}</td>)}
              </tr>
              <MatrixRows matrix={matrix} columns={ledger.columns} onSelectCategory={onSelectCategory}/>
            </tbody>
          </table>
        </div>
        <p className="ledger-legend">
          <span><i className="income" aria-hidden="true"/>Pemasukan</span>
          <span><i className="expense" aria-hidden="true"/>Pengeluaran bersih</span>
          {ledger.medianLength != null && <span><i className="median" aria-hidden="true"/>Median 3 siklus sebelum siklus terpilih</span>}
          {anyRunning && <span><i className="running" aria-hidden="true"/>Siklus berjalan, belum selesai</span>}
          {matrix.length > 0 && <span>Warna sel = besar relatif dalam satu baris, bukan nilai baik atau buruk</span>}
        </p>
      </>
      : <>
      <ul className="ledger-cards" aria-label="Ringkasan siklus">
        {ledger.columns.map(column => <li key={column.start} data-selected={column.selected || undefined}>
          <button type="button" aria-pressed={column.selected} onClick={() => onSelect(column.start)}><b>{column.label}</b><small>{column.until}</small></button>
          <dl>
            <div><dt>Pemasukan</dt><dd>{money(column.income)}</dd></div>
            <div><dt>Pengeluaran bersih</dt><dd>{money(column.expense)}</dd></div>
            <div><dt>Arus kas bersih</dt><dd>{signedMoney(column.net)}</dd></div>
          </dl>
        </li>)}
      </ul>
      {matrix.length > 0 && <div className="ledger-figure ledger-matrix-only" tabIndex={0} role="region" aria-label="Pengeluaran per kategori, geser untuk semua kolom">
        <table className="ledger-table" style={{ "--cols": ledger.columns.length }}>
          <caption className="visually-hidden">Pengeluaran bersih per kategori untuk setiap siklus gaji, dalam jutaan rupiah</caption>
          <thead><tr><th scope="col"><span className="ledger-unit">Rp juta</span></th>{ledger.columns.map(column => <th key={column.start} scope="col" data-selected={column.selected || undefined}><b>{column.label}</b><small>{column.until}</small></th>)}</tr></thead>
          <tbody><MatrixRows matrix={matrix} columns={ledger.columns} onSelectCategory={onSelectCategory}/></tbody>
        </table>
      </div>}
      </>}
  </section>;
}

// Category rows share the ribbon's columns. Cells are text; the tint only sizes
// each value within its own row. A row opens the category's evidence when the
// served category facts include it.
function MatrixRows({ matrix, columns, onSelectCategory }) {
  return matrix.map((row, index) => <tr key={row.key} className={index === 0 ? "ledger-first-category" : undefined} data-selected-row={row.selected || undefined}>
    <th scope="row">{row.selectable && onSelectCategory
      ? <button type="button" className="ledger-row-button" aria-pressed={row.selected} aria-controls="category-drivers" aria-label={`Buka bukti ${row.name}`} onClick={() => onSelectCategory(row.id)}>{row.name}</button>
      : row.name}</th>
    {columns.map((column, i) => <td key={column.start} data-selected={column.selected || undefined} data-running={column.running || undefined}><span className="ledger-cell" style={{ "--tint": row.tints[i] }}>{compactMillions(row.amounts[i])}</span></td>)}
  </tr>);
}
