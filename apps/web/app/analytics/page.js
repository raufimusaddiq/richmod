"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import { CategoryRankingChart, CycleSpendingPatternChart, MonthlyCashflowChart } from "../components/Charts";
import useAuth from "../components/useAuth";
import { dayLabel, deriveCycleSpendingMetrics } from "../lib/chartData";
import { money } from "../lib/format";

const emptyCycle = { daily: [], salary: "0", spent: "0", remaining: "0", daysElapsed: 0, daysTotal: 0 };

export default function AnalyticsPage() {
  const user = useAuth();
  const [range, setRange] = useState("6");
  const [mode, setMode] = useState("cycle");
  const [data, setData] = useState({ cashflow: [], spending: [], categories: [], merchants: [], members: [] });
  const [dailyCycle, setDailyCycle] = useState(emptyCycle);
  const [error, setError] = useState("");

  const load = useCallback(async query => {
    const suffix = query || `period=${mode === "cycle" ? "current_cycle" : "calendar"}&range=${range}`;
    const responses = await Promise.all(["cashflow", "spending", "categories", "merchants", "members"].map(name => fetch(`/api/v1/analytics/${name}?${suffix}`)));
    const dailyResponse = await fetch("/api/v1/analytics/cycle/daily");
    if (responses.some(item => !item.ok)) {
      setError("Analisis belum dapat dimuat untuk periode ini.");
      return;
    }
    const [cashflow, spending, categories, merchants, members] = await Promise.all(responses.map(item => item.json()));
    const cycle = dailyResponse.ok ? await dailyResponse.json() : emptyCycle;
    setDailyCycle(cycle || emptyCycle);
    if (mode === "cycle") {
      const daily = cycle?.daily || [];
      setData({ cashflow: daily, spending: daily.map(item => ({ period: item.period, expense: item.expense, refund: "0", netSpending: item.expense })), categories, merchants, members });
    } else setData({ cashflow, spending, categories, merchants, members });
    setError("");
  }, [range, mode]);

  useEffect(() => { if (user) load(); }, [user, load]);
  function select(value) { setRange(value); }
  function custom(event) { event.preventDefault(); const form = new FormData(event.currentTarget); load(`period=custom&from=${form.get("from")}&to=${form.get("to")}`); }
  if (!user) return <main className="loading">Memuat…</main>;

  const cycleMetrics = deriveCycleSpendingMetrics(dailyCycle);
  const totalIncome = data.cashflow.reduce((sum, item) => sum + Number(item.income || 0), 0);
  const totalExpense = data.cashflow.reduce((sum, item) => sum + Number(item.expense || 0), 0);
  const totalRefund = data.spending.reduce((sum, item) => sum + Number(item.refund || 0), 0);
  const totalNet = mode === "calendar" ? data.cashflow.reduce((sum, item) => sum + Number(item.netCashflow || 0), 0) : totalIncome - totalExpense;

  const cycleKpis = [["Rata-rata / hari", money(String(Math.round(cycleMetrics.average)))], ["Hari tertinggi", cycleMetrics.peak.period ? `${dayLabel(cycleMetrics.peak.period)} · ${money(String(Math.round(cycleMetrics.peak.expenseValue)))}` : "—"], ["Hari tanpa pengeluaran", `${cycleMetrics.zeroSpendDays} hari`], ["Siklus berjalan", `${cycleMetrics.daysElapsed} / ${cycleMetrics.daysTotal} hari`]];
  const calendarKpis = [["Total Pemasukan", money(String(totalIncome))], ["Total Pengeluaran", money(String(totalExpense))], ["Arus Kas Bersih", money(String(totalNet))], ["Pengembalian Dana", money(String(totalRefund))]];
  const kpis = mode === "cycle" ? cycleKpis : calendarKpis;

  return <AppShell user={user} eyebrow="ANALISIS" title="Pola Keuangan Rumah Tangga">
    <div className="range-controls"><div><button className={mode === "cycle" ? "active" : "secondary"} onClick={() => setMode("cycle")}>Siklus Gaji</button><button className={mode === "calendar" ? "active" : "secondary"} onClick={() => setMode("calendar")}>Kalender</button>{mode === "calendar" && ["3", "6", "12"].map(value => <button className={range === value ? "active" : "secondary"} key={value} onClick={() => select(value)}>{value} Bulan</button>)}</div>{mode === "calendar" && <form onSubmit={custom}><input name="from" type="month" required/><span>—</span><input name="to" type="month" required/><button className="secondary">Kustom</button></form>}</div>
    {error && <p className="notice error">{error}</p>}
    <section className="analytics-kpis">{kpis.map(([label, value]) => <article key={label}><span>{label}</span><b>{value}</b></article>)}</section>
    <section className="surface analytics-chart"><div className="section-title"><div><span className="eyebrow">{mode === "cycle" ? "SIKLUS GAJI · HARIAN" : "TREND BULANAN"}</span><h2>{mode === "cycle" ? "Pola pengeluaran siklus ini" : "Pemasukan vs pengeluaran"}</h2></div></div>{mode === "cycle" ? <CycleSpendingPatternChart items={data.cashflow} spent={dailyCycle.spent} daysElapsed={dailyCycle.daysElapsed} height={360}/> : <MonthlyCashflowChart items={data.cashflow} height={360}/>}</section>
    <section className="analytics-grid"><article className="surface"><div className="section-title"><h2>Peringkat kategori</h2></div><CategoryRankingChart items={data.categories}/></article><Ranked title="Merchant" items={data.merchants}/><Ranked title="Kontribusi anggota" items={data.members}/>{mode === "calendar" && <Ranked title="Pengeluaran bulanan setelah refund" items={data.spending.map(item => ({ name: item.period, amount: item.netSpending }))}/>}</section>
  </AppShell>;
}

function Ranked({ title, items }) { return <article className="surface"><div className="section-title"><h2>{title}</h2></div><div className="ranked">{items.map((item, index) => <div key={item.id || item.name}><span>{String(index + 1).padStart(2, "0")}</span><b>{item.name}</b><strong>{money(item.amount)}</strong></div>)}{!items.length && <p className="empty compact">Belum ada data.</p>}</div></article>; }
