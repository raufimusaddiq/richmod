"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import AppShell from "../components/AppShell";
import { CategoryRankingChart, CycleSpendingPatternChart, MonthlyCashflowChart } from "../components/Charts";
import InsightCard from "../components/InsightCard";
import useAuth from "../components/useAuth";
import { cycleProgressLabel, dayLabel, deriveCycleSpendingMetrics, elapsedDaily } from "../lib/chartData";
import { money } from "../lib/format";
import { pollInsight, selectCycleInsight } from "../lib/insightData";

const emptyCycle = { daily: [], salary: "0", spent: "0", remaining: "0", daysElapsed: 0, daysTotal: 0 };

export default function AnalyticsPage() {
  const user = useAuth();
  const [range, setRange] = useState("6");
  const [mode, setMode] = useState("cycle");
  const [data, setData] = useState({ cashflow: [], spending: [], categories: [], merchants: [], members: [] });
  const [dailyCycle, setDailyCycle] = useState(emptyCycle);
  const [error, setError] = useState("");
  const [insight, setInsight] = useState(null);
  const [insightLoading, setInsightLoading] = useState(false);
  const [insightError, setInsightError] = useState("");
  const insightAbort = useRef(null);

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
      const daily = elapsedDaily(cycle?.daily || [], cycle?.daysElapsed);
      setData({ cashflow: daily, spending: daily.map(item => ({ period: item.period, expense: item.expense, refund: "0", netSpending: item.expense })), categories, merchants, members });
    } else setData({ cashflow, spending, categories, merchants, members });
    setError("");
  }, [range, mode]);

  useEffect(() => { if (user) load(); }, [user, load]);
  useEffect(() => {
    if (!user || mode !== "cycle") { insightAbort.current?.abort(); setInsight(null); setInsightLoading(false); setInsightError(""); return undefined; }
    const controller = new AbortController();
    insightAbort.current?.abort(); insightAbort.current = controller;
    setInsight(null); setInsightError("");
    const loadList = async signal => { const response = await fetch("/api/v1/insights", { signal }); if (!response.ok) throw new Error("load"); return response.json(); };
    loadList(controller.signal).then(async items => {
      const selected = selectCycleInsight(items, dailyCycle);
      if (controller.signal.aborted) return;
      setInsight(selected);
      if (selected?.status === "PENDING") await pollInsight({ insightId: selected.id, load: loadList, onUpdate: setInsight, signal: controller.signal });
    }).catch(error => { if (error.name !== "AbortError") setInsightError(error.message === "insight polling timeout" ? "Analisis belum selesai. Coba perbarui lagi." : "Analisis Richmod belum dapat dimuat."); });
    return () => controller.abort();
  }, [user, mode, dailyCycle.cycleStart]);

  const generateInsight = useCallback(async () => {
    if (!dailyCycle.cycleStart) return;
    insightAbort.current?.abort();
    const controller = new AbortController();
    insightAbort.current = controller;
    setInsightLoading(true); setInsightError("");
    try {
      const response = await fetch("/api/v1/insights/generate?period=cycle", { method: "POST", signal: controller.signal });
      if (!response.ok) throw new Error("generate");
      const requested = await response.json();
      await pollInsight({ insightId: requested.id, signal: controller.signal, onUpdate: setInsight, load: async signal => { const listResponse = await fetch("/api/v1/insights", { signal }); if (!listResponse.ok) throw new Error("poll"); return listResponse.json(); } });
    } catch (error) {
      if (error.name !== "AbortError") setInsightError(error.message === "insight polling timeout" ? "Analisis belum selesai. Coba perbarui lagi." : "Analisis Richmod belum dapat dibuat.");
    } finally { if (!controller.signal.aborted) setInsightLoading(false); }
  }, [dailyCycle.cycleStart]);

  useEffect(() => () => insightAbort.current?.abort(), []);
  function select(value) { setRange(value); }
  function custom(event) { event.preventDefault(); const form = new FormData(event.currentTarget); load(`period=custom&from=${form.get("from")}&to=${form.get("to")}`); }
  if (!user) return <main className="loading">Memuat…</main>;

  const cycleMetrics = deriveCycleSpendingMetrics(dailyCycle);
  const totalIncome = data.cashflow.reduce((sum, item) => sum + Number(item.income || 0), 0);
  const totalExpense = data.cashflow.reduce((sum, item) => sum + Number(item.expense || 0), 0);
  const totalRefund = data.spending.reduce((sum, item) => sum + Number(item.refund || 0), 0);
  const totalNet = mode === "calendar" ? data.cashflow.reduce((sum, item) => sum + Number(item.netCashflow || 0), 0) : totalIncome - totalExpense;

  const cycleKpis = [["Rata-rata / hari", money(String(Math.round(cycleMetrics.average)))], ["Hari tertinggi", cycleMetrics.peak.period ? `${dayLabel(cycleMetrics.peak.period)} · ${money(String(Math.round(cycleMetrics.peak.expenseValue)))}` : "—"], ["Hari tanpa pengeluaran", `${cycleMetrics.zeroSpendDays} hari`], ["Hari siklus", cycleProgressLabel(cycleMetrics.daysElapsed)]];
  const calendarKpis = [["Total Pemasukan", money(String(totalIncome))], ["Total Pengeluaran", money(String(totalExpense))], ["Arus Kas Bersih", money(String(totalNet))], ["Pengembalian Dana", money(String(totalRefund))]];
  const kpis = mode === "cycle" ? cycleKpis : calendarKpis;

  return <AppShell user={user} eyebrow="ANALISIS" title="Pola Keuangan Rumah Tangga">
    <div className="range-controls"><div><button className={mode === "cycle" ? "active" : "secondary"} onClick={() => setMode("cycle")}>Siklus Gaji</button><button className={mode === "calendar" ? "active" : "secondary"} onClick={() => setMode("calendar")}>Kalender</button>{mode === "calendar" && ["3", "6", "12"].map(value => <button className={range === value ? "active" : "secondary"} key={value} onClick={() => select(value)}>{value} Bulan</button>)}</div>{mode === "calendar" && <form onSubmit={custom}><input name="from" type="month" required/><span>—</span><input name="to" type="month" required/><button className="secondary">Kustom</button></form>}</div>
    {error && <p className="notice error">{error}</p>}
    <section className="analytics-kpis">{kpis.map(([label, value]) => <article key={label}><span>{label}</span><b>{value}</b></article>)}</section>
    <section className="surface analytics-chart"><div className="section-title"><div><span className="eyebrow">{mode === "cycle" ? "SIKLUS GAJI · HARIAN" : "TREND BULANAN"}</span><h2>{mode === "cycle" ? "Pola pengeluaran siklus ini" : "Pemasukan vs pengeluaran"}</h2></div></div>{mode === "cycle" ? <CycleSpendingPatternChart items={data.cashflow} spent={dailyCycle.spent} daysElapsed={dailyCycle.daysElapsed} height={310}/> : <MonthlyCashflowChart items={data.cashflow} height={340}/>}</section>
    <InsightCard insight={insight} loading={insightLoading} error={insightError} unsupported={mode === "calendar"} canGenerate={Boolean(dailyCycle.cycleStart)} onGenerate={generateInsight}/>
    <section className="analytics-detail-layout"><article className="surface analytics-category-panel"><div className="section-title"><h2>Peringkat kategori</h2></div><CategoryRankingChart items={data.categories}/></article><div className="analytics-detail-side"><Ranked title="Merchant" items={data.merchants}/><Ranked title="Kontribusi anggota" items={data.members}/></div></section>
    {mode === "calendar" && <div className="analytics-calendar-detail"><Ranked title="Pengeluaran bulanan setelah refund" items={data.spending.map(item => ({ name: item.period, amount: item.netSpending }))}/></div>}
  </AppShell>;
}

function Ranked({ title, items }) { return <article className="surface"><div className="section-title"><h2>{title}</h2></div><div className="ranked">{items.map((item, index) => <div key={item.id || item.name}><span>{String(index + 1).padStart(2, "0")}</span><b>{item.name}</b><strong>{money(item.amount)}</strong></div>)}{!items.length && <p className="empty compact">Belum ada data.</p>}</div></article>; }
