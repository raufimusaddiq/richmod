"use client";

import { useEffect, useState } from "react";
import { CategoryRankingChart, MonthlyCashflowChart } from "../components/Charts";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import { calendarErrorMessage, currentMonthKey, hasActivity, monthRowLabel, nextMonthKey, validateCustomRange } from "../lib/calendarReview";
import { money, monthLabel } from "../lib/format";
import { SectionTitle, Metric } from "./shared";

export function CalendarReview({ selection, navigate }) {
  const [data, setData] = useState(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [reload, setReload] = useState(0);
  const [from, setFrom] = useState(selection.from);
  const [to, setTo] = useState(selection.to);
  const [rangeError, setRangeError] = useState("");
  const now = new Date(); // labels and picker bounds only; the API decides which months exist
  const running = currentMonthKey(now);
  useEffect(() => { setFrom(selection.from); setTo(selection.to); setRangeError(""); }, [selection.from, selection.to]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError("");
    const query = new URLSearchParams({ period: selection.from && selection.to ? "custom" : "calendar", range: selection.range });
    if (selection.from && selection.to) { query.set("from", selection.from); query.set("to", selection.to); }
    Promise.all(["cashflow", "categories", "merchants", "members"].map(async name => {
      const response = await fetch(`/api/v1/analytics/${name}?${query}`, { signal: controller.signal });
      if (!response.ok) {
        const body = await response.json().catch(() => ({}));
        throw new Error(calendarErrorMessage(response.status, body?.error));
      }
      return [name, await response.json()];
    })).then(entries => { if (!controller.signal.aborted) setData(Object.fromEntries(entries)); })
      .catch(err => { if (err.name !== "AbortError") { setData(null); setError(err.message); } })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [selection.range, selection.from, selection.to, reload]);
  const custom = Boolean(selection.from && selection.to);
  const refreshing = loading && Boolean(data);
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
    <ErrorNotice message={error} retry={() => setReload(value => value + 1)}/>
    {loading && !data && !error && <Skeleton cards={1} rows={4}/>}
    {refreshing && <p className="ledger-loading" role="status">Memuat rentang…</p>}
    {data && <div className="calendar-body" data-stale={refreshing ? "true" : undefined} aria-busy={refreshing || undefined}>
      <section className="review-section analytics-chart"><SectionTitle title="Pemasukan vs pengeluaran" description="Bagaimana arus kas berubah antar bulan? Semua nilai berasal dari transaksi terkonfirmasi."/><MonthlyCashflowChart items={data.cashflow} height={280} partialPeriod={running}/>{data.cashflow.some(item => item.period === running) && <p className="ledger-legend">Bulan berjalan ({monthLabel(running)}) belum penuh.</p>}</section>
      <section className="review-section"><SectionTitle title="Arus kas per bulan" description="Pengeluaran sudah dikurangi refund. Bulan tanpa transaksi terkonfirmasi ditandai, bukan ditulis sebagai Rp0."/><div className="review-table-wrap"><table><thead><tr><th scope="col">Bulan</th><th scope="col">Pemasukan</th><th scope="col">Pengeluaran bersih</th><th scope="col">Refund</th><th scope="col">Arus kas bersih</th></tr></thead><tbody>{data.cashflow.map(item => { const active = hasActivity(item); return <tr key={item.period}><th scope="row">{monthRowLabel(item.period, now)}{!active && <small>belum ada transaksi</small>}</th>{active ? <><td>{money(item.income)}</td><td>{money(item.expense)}</td><td>{money(item.refund)}</td><td>{money(item.netCashflow)}</td></> : <td colSpan={4}>—</td>}</tr>; })}</tbody></table></div></section>
      <section className="review-section"><SectionTitle title="Distribusi kategori" description="Kategori mana yang menyusun pengeluaran rentang ini?"/><CategoryRankingChart items={data.categories}/></section>
      <section className="review-section"><SectionTitle title="Merchant" description="Pengeluaran bersih setelah refund dalam rentang kalender."/><ValueList items={data.merchants}/></section>
      <section className="review-section"><SectionTitle title="Catatan rumah tangga" description="Atribusi pencatatan ketika diketahui, bukan peringkat anggota."/><ValueList items={[...data.members].sort((a, b) => a.name.localeCompare(b.name, "id"))}/></section>
      <p>Pembahasan dan tinjauan rumah tangga tersedia pada mode Siklus Gaji.</p>
    </div>}
  </>;
}

export function ValueList({ items }) {
  return items.length ? <dl className="review-values">{items.map((item, index) => <Metric key={item.id || `${item.name}:${index}`} label={item.name} value={money(item.amount)}/>)}</dl> : <p className="empty compact">Belum ada data pada rentang ini.</p>;
}
