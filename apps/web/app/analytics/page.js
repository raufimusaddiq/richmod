"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import { CashflowChart, CategoryChart, DailyCashflowChart } from "../components/Charts";
import useAuth from "../components/useAuth";
import { money } from "../lib/format";

export default function AnalyticsPage() {
  const user = useAuth();
  const [range, setRange] = useState("6");
  const [mode, setMode] = useState("cycle");
  const [data, setData] = useState({ cashflow: [], spending: [], categories: [], merchants: [], members: [] });
  const [dailyCycle, setDailyCycle] = useState({ daily: [], salary: "0", spent: "0", remaining: "0", daysElapsed: 0, daysTotal: 0 });
  const [error, setError] = useState("");
  const load = useCallback(async query => {
    const suffix = query || `range=${range}`;
    const responses = await Promise.all(["cashflow", "spending", "categories", "merchants", "members"].map(name => fetch(`/api/v1/analytics/${name}?${suffix}`)));
    const dailyResponse = await fetch("/api/v1/analytics/cycle/daily");
    if (responses.some(item => !item.ok)) { setError("Analisis belum dapat dimuat untuk periode ini."); return; }
    const [cashflow, spending, categories, merchants, members] = await Promise.all(responses.map(item => item.json()));
    const cycle = dailyResponse.ok ? await dailyResponse.json() : null;
    setDailyCycle(cycle || { daily: [], salary: "0", spent: "0", remaining: "0", daysElapsed: 0, daysTotal: 0 });
    if (mode === "cycle" && cycle) {
      const daily = cycle.daily || [];
      setData({ cashflow: daily, spending: daily.map(item => ({ period: item.period, expense: item.expense, refund: "0", netSpending: item.expense })), categories, merchants, members });
    } else setData({ cashflow, spending, categories, merchants, members });
    setError("");
  }, [range, mode]);
  useEffect(() => { if (user) load(); }, [user, load]);
  function select(value) { setRange(value); }
  function custom(event) { event.preventDefault(); const form = new FormData(event.currentTarget); load(`from=${form.get("from")}&to=${form.get("to")}`); }
  if (!user) return <main className="loading">Memuat…</main>;
  const totalExpense = data.spending.reduce((sum, item) => sum + BigInt(item.expense || "0"), 0n).toString();
  const totalRefund = data.spending.reduce((sum, item) => sum + BigInt(item.refund || "0"), 0n).toString();
  return <AppShell user={user} eyebrow="ANALISIS" title="Pola Keuangan Rumah Tangga">
    <div className="range-controls"><div><button className={mode === "cycle" ? "active" : "secondary"} onClick={() => setMode("cycle")}>Siklus Gaji</button><button className={mode === "calendar" ? "active" : "secondary"} onClick={() => setMode("calendar")}>Kalender</button>{mode === "calendar" && ["3", "6", "12"].map(value => <button className={range === value ? "active" : "secondary"} key={value} onClick={() => select(value)}>{value} Bulan</button>)}</div><form onSubmit={custom}><input name="from" type="month" required/><span>—</span><input name="to" type="month" required/><button className="secondary">Kustom</button></form></div>
    {error && <p className="notice error">{error}</p>}
    <section className="analytics-kpis"><article><span>Total Pengeluaran</span><b>{money(totalExpense)}</b></article><article><span>Pengembalian Dana</span><b>{money(totalRefund)}</b></article><article><span>Merchant Terbesar</span><b>{data.merchants[0]?.name || "—"}</b></article><article><span>Kategori Terbesar</span><b>{data.categories[0]?.name || "—"}</b></article></section>
    <section className="surface analytics-chart"><div className="section-title"><div><span className="eyebrow">{mode === "cycle" ? "SIKLUS GAJI · HARIAN" : "TREND"}</span><h2>{mode === "cycle" ? "Arus kas siklus berjalan" : "Pemasukan, pengeluaran, dan net"}</h2></div></div>{mode === "cycle" ? <DailyCashflowChart items={data.cashflow} height={360}/> : <CashflowChart items={data.cashflow} height={360}/>}</section>
    {mode === "calendar" && <section className="surface analytics-chart cycle-daily-panel"><div className="section-title"><div><span className="eyebrow">SIKLUS GAJI · HARIAN</span><h2>Kecepatan pengeluaran siklus aktif</h2></div></div><div className="analytics-kpis cycle-kpis"><article><span>Gaji acuan</span><b>{money(dailyCycle.salary)}</b></article><article><span>Sudah dibelanjakan</span><b>{money(dailyCycle.spent)}</b></article><article><span>Sisa</span><b>{money(dailyCycle.remaining)}</b></article><article><span>Hari berjalan</span><b>{dailyCycle.daysElapsed} / {dailyCycle.daysTotal}</b></article></div><DailyCashflowChart items={dailyCycle.daily} height={300}/></section>}
    <section className="analytics-grid"><article className="surface"><div className="section-title"><h2>Distribusi kategori</h2></div><CategoryChart items={data.categories}/></article><Ranked title="Merchant" items={data.merchants}/><Ranked title="Kontribusi anggota" items={data.members}/><Ranked title="Pengeluaran bulanan setelah refund" items={data.spending.map(item => ({ name: item.period, amount: item.netSpending }))}/></section>
  </AppShell>;
}

function Ranked({ title, items }) { return <article className="surface"><div className="section-title"><h2>{title}</h2></div><div className="ranked">{items.map((item, index) => <div key={item.id || item.name}><span>{String(index + 1).padStart(2, "0")}</span><b>{item.name}</b><strong>{money(item.amount)}</strong></div>)}{!items.length && <p className="empty compact">Belum ada data.</p>}</div></article>; }
