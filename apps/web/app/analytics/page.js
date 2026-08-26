"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import { CashflowChart, CategoryChart, DailyCashflowChart } from "../components/Charts";
import useAuth from "../components/useAuth";
import { money } from "../lib/format";

export default function AnalyticsPage() {
  const user = useAuth();
  const [range, setRange] = useState("6");
  const [data, setData] = useState({ cashflow: [], spending: [], categories: [], merchants: [], members: [] });
  const [dailyCycle, setDailyCycle] = useState([]);
  const [error, setError] = useState("");
  const load = useCallback(async query => {
    const suffix = query || `range=${range}`;
    const responses = await Promise.all(["cashflow", "spending", "categories", "merchants", "members"].map(name => fetch(`/api/v1/analytics/${name}?${suffix}`)));
    const dailyResponse = await fetch("/api/v1/analytics/cycle/daily");
    if (responses.some(item => !item.ok)) { setError("Analytics belum dapat dimuat untuk periode ini."); return; }
    const [cashflow, spending, categories, merchants, members] = await Promise.all(responses.map(item => item.json()));
    setData({ cashflow, spending, categories, merchants, members }); if (dailyResponse.ok) setDailyCycle(await dailyResponse.json()); setError("");
  }, [range]);
  useEffect(() => { if (user) load(); }, [user, load]);
  function select(value) { setRange(value); }
  function custom(event) { event.preventDefault(); const form = new FormData(event.currentTarget); load(`from=${form.get("from")}&to=${form.get("to")}`); }
  if (!user) return <main className="loading">Memuat…</main>;
  const totalExpense = data.spending.reduce((sum, item) => sum + BigInt(item.expense || "0"), 0n).toString();
  const totalRefund = data.spending.reduce((sum, item) => sum + BigInt(item.refund || "0"), 0n).toString();
  return <AppShell user={user} eyebrow="ANALYTICS" title="Pola keuangan household">
    <div className="range-controls"><div>{["3", "6", "12"].map(value => <button className={range === value ? "active" : "secondary"} key={value} onClick={() => select(value)}>{value} bulan</button>)}</div><form onSubmit={custom}><input name="from" type="month" required/><span>—</span><input name="to" type="month" required/><button className="secondary">Custom</button></form></div>
    {error && <p className="notice error">{error}</p>}
    <section className="analytics-kpis"><article><span>Total pengeluaran</span><b>{money(totalExpense)}</b></article><article><span>Refund</span><b>{money(totalRefund)}</b></article><article><span>Merchant terbesar</span><b>{data.merchants[0]?.name || "—"}</b></article><article><span>Kategori terbesar</span><b>{data.categories[0]?.name || "—"}</b></article></section>
    <section className="surface analytics-chart"><div className="section-title"><div><span className="eyebrow">TREND</span><h2>Pemasukan, pengeluaran, dan net</h2></div></div><CashflowChart items={data.cashflow} height={360}/></section>
    <section className="surface analytics-chart cycle-daily-panel"><div className="section-title"><div><span className="eyebrow">SIKLUS GAJI · HARIAN</span><h2>Arus kas harian periode aktif</h2></div></div><DailyCashflowChart items={dailyCycle} height={300}/></section>
    <section className="analytics-grid"><article className="surface"><div className="section-title"><h2>Distribusi kategori</h2></div><CategoryChart items={data.categories}/></article><Ranked title="Merchant" items={data.merchants}/><Ranked title="Kontribusi anggota" items={data.members}/><Ranked title="Pengeluaran bulanan setelah refund" items={data.spending.map(item => ({ name: item.period, amount: item.netSpending }))}/></section>
  </AppShell>;
}

function Ranked({ title, items }) { return <article className="surface"><div className="section-title"><h2>{title}</h2></div><div className="ranked">{items.map((item, index) => <div key={item.id || item.name}><span>{String(index + 1).padStart(2, "0")}</span><b>{item.name}</b><strong>{money(item.amount)}</strong></div>)}{!items.length && <p className="empty compact">Belum ada data.</p>}</div></article>; }
