"use client";

import { useEffect, useState } from "react";
import { CategoryRankingChart, MonthlyCashflowChart } from "../components/Charts";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import { addMonths, calendarErrorMessage, chartCategories, currentMonthKey, defaultRange, firstActiveMonth, hasActivity, hasRefunds, monthRowLabel, nextMonthKey, rangeSummary, validateCustomRange } from "../lib/calendarReview";
import { transactionHref } from "../lib/cycleReview";
import { money, monthLabel } from "../lib/format";
import { SectionTitle, Metric } from "./shared";

export function CalendarReview({ selection, navigate }) {
  const fallback = defaultRange(selection);
  const [from, setFrom] = useState(fallback.from);
  const [to, setTo] = useState(fallback.to);
  const [rangeError, setRangeError] = useState("");
  const now = new Date();
  const running = currentMonthKey(now);
  const selectedFrom = selection.from;
  const selectedTo = selection.to;
  const selectedRange = selection.range;
  useEffect(() => {
    const shown = defaultRange({ from: selectedFrom, to: selectedTo, range: selectedRange });
    setFrom(shown.from); setTo(shown.to); setRangeError("");
  }, [selectedFrom, selectedTo, selectedRange]);
  const query = new URLSearchParams({ period: selection.from && selection.to ? "custom" : "calendar", range: selection.range });
  if (selection.from && selection.to) { query.set("from", selection.from); query.set("to", selection.to); }
  const queryString = query.toString();
  const cashflow = useCalendarSection("cashflow", queryString);
  const categories = useCalendarSection("categories", queryString);
  const merchants = useCalendarSection("merchants", queryString);
  const members = useCalendarSection("members", queryString);
  const sections = [cashflow, categories, merchants, members];
  const allFailed = sections.every(section => section.error);
  const refreshing = sections.some(section => section.loading && section.data);
  const custom = Boolean(selection.from && selection.to);
  const draft = from !== fallback.from || to !== fallback.to;
  const latest = nextMonthKey(now);
  function submitRange(event) {
    event.preventDefault();
    if (!draft) return;
    const problem = validateCustomRange(from, to, new Date());
    setRangeError(problem);
    if (!problem) navigate({ from, to });
  }
  const rangeProps = value => ({ type: "month", max: latest, required: true, "aria-invalid": rangeError ? true : undefined, "aria-describedby": rangeError ? "range-error" : undefined, value });
  return <>
    <div className="range-controls"><div className="range-control-group" data-slide data-slide-panel=".calendar-body" role="group" aria-label="Rentang bulan">{["3", "6", "12"].map(range => <button type="button" key={range} className={selection.range === range && !custom ? "active" : "secondary"} aria-pressed={selection.range === range && !custom} onClick={() => navigate({ range, from: "", to: "" })}>{range} Bulan</button>)}</div>
      <form className="custom-range" aria-label="Rentang bulan kustom" onSubmit={submitRange} noValidate><label>Bulan mulai<input name="from" {...rangeProps(from)} onChange={event => setFrom(event.target.value)}/></label><label>Bulan selesai<input name="to" {...rangeProps(to)} onChange={event => setTo(event.target.value)}/></label><button type="submit" disabled={!draft}>Terapkan</button></form>
    </div>
    {rangeError && <div id="range-error" className="notice error feedback" role="alert"><span>{rangeError}</span></div>}
    <p>{custom ? `Rentang bulan ${rangeSummary(selection.from, selection.to)}` : `${selection.range} bulan terakhir`} · Asia/Jakarta{draft ? " · perubahan belum diterapkan" : ""}</p>
    {allFailed && <ErrorNotice message={cashflow.error} retry={() => sections.forEach(section => section.retry())}/>}
    {refreshing && <p className="ledger-loading" role="status">Memuat rentang…</p>}
    {!allFailed && <div className="calendar-body">
      <CalendarSection state={cashflow} className="surface card analytics-chart" title="Pemasukan vs pengeluaran" about="arus kas bulanan" description="Bagaimana arus kas berubah antar bulan? Semua nilai berasal dari transaksi terkonfirmasi." rows={4}>{items => <><MonthlyCashflowChart items={items} height={280} partialPeriod={running}/>{items.some(item => item.period === running) && <p className="ledger-legend">Bulan berjalan ({monthLabel(running)}) belum penuh.</p>}</>}</CalendarSection>
      <CalendarSection state={cashflow} hideError title="Arus kas per bulan" about="tabel bulanan" description="Pengeluaran sudah dikurangi refund. Bulan sebelum transaksi pertama diringkas menjadi satu baris; bulan kosong di antaranya ditandai, bukan ditulis sebagai Rp0. Kolom refund hanya ada bila rentang ini punya refund." rows={3}>{items => <MonthlyTable items={items} now={now} cycle={selection.cycle}/>}</CalendarSection>
      <CalendarSection state={categories} title="Distribusi kategori" about="distribusi kategori" description="Kategori mana yang menyusun pengeluaran rentang ini? Diurutkan oleh server berdasarkan jumlah, bukan oleh browser. Grafik menampilkan sepuluh kategori terbesar dan Kategori lain; daftar lengkapnya ada di Lihat nilai kategori." rows={3}>{items => <><CategoryRankingChart items={chartCategories(items)} height={160}/><details className="review-daily"><summary>Lihat nilai kategori</summary><ValueList items={items}/></details></>}</CalendarSection>
      <CalendarSection state={merchants} title="Merchant" about="merchant" description="Pengeluaran bersih setelah refund dalam rentang kalender." rows={2}>{items => <ValueList items={items}/>}</CalendarSection>
      <CalendarSection state={members} title="Catatan rumah tangga" about="catatan rumah tangga" description="Atribusi pencatatan ketika diketahui, bukan peringkat anggota." rows={2}>{items => <ValueList items={[...items].sort((a, b) => a.name.localeCompare(b.name, "id"))}/>}</CalendarSection>
      <p>Pembahasan dan tinjauan rumah tangga tersedia pada mode Siklus Gaji.</p>
    </div>}
  </>;
}

function useCalendarSection(name, queryString) {
  const [state, setState] = useState({ data: null, error: "", loading: true });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setState(current => ({ ...current, error: "", loading: true }));
    fetch(`/api/v1/analytics/${name}?${queryString}`, { signal: controller.signal })
      .then(async response => {
        if (!response.ok) {
          const body = await response.json().catch(() => ({}));
          throw new Error(calendarErrorMessage(response.status, body?.error));
        }
        return response.json();
      })
      .then(data => { if (!controller.signal.aborted) setState({ data, error: "", loading: false }); })
      .catch(err => { if (err.name !== "AbortError" && !controller.signal.aborted) setState({ data: null, error: err.message, loading: false }); });
    return () => controller.abort();
  }, [name, queryString, attempt]);
  return { ...state, retry: () => setAttempt(value => value + 1) };
}

function CalendarSection({ state, title, description, about, rows, className = "surface card", hideError = false, children }) {
  return <section className={`${className} calendar-section`} data-stale={state.loading && state.data ? "true" : undefined} aria-busy={state.loading || undefined}>
    <SectionTitle title={title} description={description} about={about}/>
    {state.error ? (hideError ? null : <ErrorNotice message={state.error} retry={state.retry}/>) : state.data ? children(state.data) : <Skeleton cards={1} rows={rows} label={`Memuat ${title.toLowerCase()}`}/>}
  </section>;
}

function ValueList({ items }) {
  return items.length ? <dl className="review-values">{items.map((item, index) => <Metric key={item.id || `${item.name}:${index}`} label={item.name} value={money(item.amount)}/>)}</dl> : <p className="empty compact">Belum ada data pada rentang ini.</p>;
}

function MonthlyTable({ items, now, cycle }) {
  const start = firstActiveMonth(items);
  const showRefund = hasRefunds(items);
  const columns = showRefund ? 4 : 3;
  const labels = ["Pemasukan", "Pengeluaran bersih", ...(showRefund ? ["Refund"] : []), "Arus kas bersih"];
  const rows = start < 0 ? [] : items.slice(start);
  return <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Arus kas per bulan, geser untuk semua kolom">
    <table className="monthly-table">
      <caption className="visually-hidden">Arus kas per bulan pada rentang kalender</caption>
      <thead><tr><th scope="col">Bulan</th>{labels.map(label => <th key={label} scope="col">{label}</th>)}</tr></thead>
      <tbody>
        {start > 0 && <tr className="month-start"><td colSpan={columns + 1}>Data mulai {monthLabel(items[start].period)}</td></tr>}
        {start < 0 && <tr className="month-start"><td colSpan={columns + 1}>Belum ada transaksi terkonfirmasi pada rentang ini.</td></tr>}
        {rows.map(item => { const active = hasActivity(item); return <tr key={item.period}>
          <th scope="row">{active ? <a href={transactionHref({ start: cycle, measuredUntil: `${addMonths(item.period, 1)}-01` }, { from: `${item.period}-01` })} aria-label={`Lihat pengeluaran ${monthRowLabel(item.period, now)}`}>{monthRowLabel(item.period, now)}</a> : <>{monthRowLabel(item.period, now)}<small>belum ada transaksi</small></>}</th>
          {active ? <>
            <td data-label={labels[0]}>{money(item.income)}</td>
            <td data-label={labels[1]}>{money(item.expense)}</td>
            {showRefund && <td data-label={labels[2]}>{money(item.refund)}</td>}
            <td data-label={labels[showRefund ? 3 : 2]}>{money(item.netCashflow)}</td>
          </> : <td colSpan={columns}>—</td>}
        </tr>; })}
      </tbody>
    </table>
  </div>;
}

