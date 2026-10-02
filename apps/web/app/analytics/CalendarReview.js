"use client";

import { useEffect, useState } from "react";
import { CategoryRankingChart, MonthlyCashflowChart } from "../components/Charts";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import { calendarErrorMessage, currentMonthKey, hasActivity, monthRowLabel, nextMonthKey, validateCustomRange } from "../lib/calendarReview";
import { money, monthLabel } from "../lib/format";
import { SectionTitle, Metric } from "./shared";

export function CalendarReview({ selection, navigate }) {
  const [from, setFrom] = useState(selection.from);
  const [to, setTo] = useState(selection.to);
  const [rangeError, setRangeError] = useState("");
  const now = new Date(); // labels and picker bounds only; the API decides which months exist
  const running = currentMonthKey(now);
  useEffect(() => { setFrom(selection.from); setTo(selection.to); setRangeError(""); }, [selection.from, selection.to]);
  const query = new URLSearchParams({ period: selection.from && selection.to ? "custom" : "calendar", range: selection.range });
  if (selection.from && selection.to) { query.set("from", selection.from); query.set("to", selection.to); }
  const queryString = query.toString();
  // Each section loads, fails and retries on its own: one failing request no longer blanks the view.
  const cashflow = useCalendarSection("cashflow", queryString);
  const categories = useCalendarSection("categories", queryString);
  const merchants = useCalendarSection("merchants", queryString);
  const members = useCalendarSection("members", queryString);
  const sections = [cashflow, categories, merchants, members];
  const allFailed = sections.every(section => section.error);
  const refreshing = sections.some(section => section.loading && section.data);
  const custom = Boolean(selection.from && selection.to);
  const latest = nextMonthKey(now);
  function submitRange(event) {
    event.preventDefault();
    const problem = validateCustomRange(from, to, new Date());
    setRangeError(problem);
    if (!problem) navigate({ from, to });
  }
  const rangeProps = value => ({ type: "month", max: latest, required: true, "aria-invalid": rangeError ? true : undefined, "aria-describedby": rangeError ? "range-error" : undefined, value });
  return <>
    <div className="range-controls"><div className="range-control-group">{["3", "6", "12"].map(range => <button type="button" key={range} className={selection.range === range && !custom ? "active" : "secondary"} aria-pressed={selection.range === range && !custom} onClick={() => navigate({ range, from: "", to: "" })}>{range} Bulan</button>)}</div>
      <form className="custom-range" onSubmit={submitRange} noValidate><label>Bulan mulai<input name="from" {...rangeProps(from)} onChange={event => setFrom(event.target.value)}/></label><label>Bulan selesai<input name="to" {...rangeProps(to)} onChange={event => setTo(event.target.value)}/></label><button className={custom ? "active" : "secondary"} aria-pressed={custom}>Kustom</button></form>
    </div>
    {rangeError && <div id="range-error" className="notice error feedback" role="alert"><span>{rangeError}</span></div>}
    <p>{custom ? `Rentang bulan ${monthLabel(selection.from)} sampai ${monthLabel(selection.to)}` : `${selection.range} bulan terakhir`} · Asia/Jakarta</p>
    {allFailed && <ErrorNotice message={cashflow.error} retry={() => sections.forEach(section => section.retry())}/>}
    {refreshing && <p className="ledger-loading" role="status">Memuat rentang…</p>}
    {!allFailed && <div className="calendar-body">
      <CalendarSection state={cashflow} className="review-section analytics-chart" title="Pemasukan vs pengeluaran" about="arus kas bulanan" description="Bagaimana arus kas berubah antar bulan? Semua nilai berasal dari transaksi terkonfirmasi." rows={4}>{items => <><MonthlyCashflowChart items={items} height={280} partialPeriod={running}/>{items.some(item => item.period === running) && <p className="ledger-legend">Bulan berjalan ({monthLabel(running)}) belum penuh.</p>}</>}</CalendarSection>
      <CalendarSection state={cashflow} hideError title="Arus kas per bulan" about="tabel bulanan" description="Pengeluaran sudah dikurangi refund. Bulan tanpa transaksi terkonfirmasi ditandai, bukan ditulis sebagai Rp0." rows={3}>{items => <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Arus kas per bulan, geser untuk semua kolom"><table><thead><tr><th scope="col">Bulan</th><th scope="col">Pemasukan</th><th scope="col">Pengeluaran bersih</th><th scope="col">Refund</th><th scope="col">Arus kas bersih</th></tr></thead><tbody>{items.map(item => { const active = hasActivity(item); return <tr key={item.period}><th scope="row">{monthRowLabel(item.period, now)}{!active && <small>belum ada transaksi</small>}</th>{active ? <><td>{money(item.income)}</td><td>{money(item.expense)}</td><td>{money(item.refund)}</td><td>{money(item.netCashflow)}</td></> : <td colSpan={4}>—</td>}</tr>; })}</tbody></table></div>}</CalendarSection>
      <CalendarSection state={categories} title="Distribusi kategori" about="distribusi kategori" description="Kategori mana yang menyusun pengeluaran rentang ini?" rows={3}>{items => <CategoryRankingChart items={items}/>}</CalendarSection>
      <CalendarSection state={merchants} title="Merchant" about="merchant" description="Pengeluaran bersih setelah refund dalam rentang kalender." rows={2}>{items => <ValueList items={items}/>}</CalendarSection>
      <CalendarSection state={members} title="Catatan rumah tangga" about="catatan rumah tangga" description="Atribusi pencatatan ketika diketahui, bukan peringkat anggota." rows={2}>{items => <ValueList items={[...items].sort((a, b) => a.name.localeCompare(b.name, "id"))}/>}</CalendarSection>
      <p>Pembahasan dan tinjauan rumah tangga tersedia pada mode Siklus Gaji.</p>
    </div>}
  </>;
}

// One analytics request. A new range keeps the previous data (dimmed) until it
// arrives; a failure clears that section only and is retried from its own notice.
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

function CalendarSection({ state, title, description, about, rows, className = "review-section", hideError = false, children }) {
  return <section className={`${className} calendar-section`} data-stale={state.loading && state.data ? "true" : undefined} aria-busy={state.loading || undefined}>
    <SectionTitle title={title} description={description} about={about}/>
    {state.error ? (hideError ? null : <ErrorNotice message={state.error} retry={state.retry}/>) : state.data ? children(state.data) : <Skeleton cards={1} rows={rows} label={`Memuat ${title.toLowerCase()}`}/>}
  </section>;
}

export function ValueList({ items }) {
  return items.length ? <dl className="review-values">{items.map((item, index) => <Metric key={item.id || `${item.name}:${index}`} label={item.name} value={money(item.amount)}/>)}</dl> : <p className="empty compact">Belum ada data pada rentang ini.</p>;
}
