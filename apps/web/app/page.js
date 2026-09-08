"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { ArrowRight, CalendarBlank, CheckCircle, UploadSimple, WarningCircle } from "@phosphor-icons/react";
import AppShell from "./components/AppShell";
import { ErrorNotice, Skeleton } from "./components/Feedback";
import { CategoryDonutChart, DashboardDailySpendingChart } from "./components/Charts";
import TransactionList from "./components/TransactionList";
import useAuth from "./components/useAuth";
import { elapsedDaily } from "./lib/chartData";
import { money } from "./lib/format";
import LandingPage from "./components/LandingPage";

export default function Home() {
  const user = useAuth(false);
  const [overview, setOverview] = useState(null);
  const [cashflow, setCashflow] = useState([]);
  const [categories, setCategories] = useState([]);
  const [transactions, setTransactions] = useState([]);
  const [cycle, setCycle] = useState(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    if (!user) return;
    setLoading(true);
    try { const responses = await Promise.all([fetch("/api/v1/analytics/overview"), fetch("/api/v1/analytics/cycle/daily"), fetch("/api/v1/analytics/categories?range=3"), fetch("/api/v1/transactions?limit=8"), fetch("/api/v1/analytics/cycle")]);
      if (responses.some(response => !response.ok)) setError("Sebagian ringkasan belum dapat dimuat."); else setError("");
      if (responses[0].ok) setOverview(await responses[0].json()); if (responses[1].ok) { const cycleData = await responses[1].json(); setCashflow(elapsedDaily(cycleData.daily || [], cycleData.daysElapsed)); } if (responses[2].ok) setCategories(await responses[2].json()); if (responses[3].ok) setTransactions(await responses[3].json());
      if (responses[4].ok) setCycle(await responses[4].json());
    } catch { setError("Koneksi terputus saat memuat ringkasan."); } finally { setLoading(false); }
  }, [user]);

  useEffect(() => { load(); }, [load]);

  if (user === null) return <Loading />;
  if (user === false) return <LandingPage />;
  const periodLabel = overview?.periodKind === "CURRENT_CYCLE" ? "siklus ini" : "bulan ini";
  const cards = [[`Pemasukan ${periodLabel}`, overview?.income, "income"], [`Pengeluaran ${periodLabel}`, overview?.expense, "expense"]];
  return <AppShell user={user} eyebrow="Ringkasan" title={`Keuangan keluarga · ${periodLabel}`} actions={<Link className="button secondary" href="/documents"><UploadSimple aria-hidden="true"/> Unggah bukti</Link>}>
    <ErrorNotice message={error} retry={load}/>
    {loading && <Skeleton/>}
    {!loading && <>
    <section className="overview-summary">
      <article className="cashflow-summary"><span>Arus kas bersih · {periodLabel}</span><strong className="net">{money(overview?.netCashflow)}</strong><small><CheckCircle aria-hidden="true" weight="fill"/> Hanya transaksi terkonfirmasi</small></article>
      <div className="kpi-grid">{cards.map(([label, value, tone]) => <article key={label}><span>{label}</span><strong className={tone}>{money(value)}</strong><small>IDR · terkonfirmasi</small></article>)}</div>
      {cycle && <article className="overview-period"><CalendarBlank aria-hidden="true"/><div><span>Periode aktif</span><strong>{cycle.kind === "CURRENT_CYCLE" ? "Siklus gaji" : "Bulan kalender"}</strong><small>{cycle.start}{cycle.end ? ` – ${cycle.end}` : " · masih berjalan"}</small></div></article>}
    </section>
    {overview?.reviewCount > 0 && <Link className="review-alert" href="/inbox?view=transactions"><WarningCircle aria-hidden="true" weight="fill"/><div><b>{overview.reviewCount} transaksi membutuhkan keputusan</b><small>Belum masuk analisis sampai kamu mengonfirmasi interpretasinya.</small></div><strong>Buka Inbox <ArrowRight aria-hidden="true"/></strong></Link>}
    <section className="dashboard-grid"><article className="surface chart-panel"><div className="section-title"><div><span className="eyebrow">{overview?.periodKind === "CURRENT_CYCLE" ? "Siklus gaji · harian" : "Bulan ini · harian"}</span><h2>Pengeluaran harian</h2></div><Link href="/analytics">Lihat analisis <ArrowRight aria-hidden="true"/></Link></div><DashboardDailySpendingChart items={cashflow}/></article><article className="surface category-panel"><div className="section-title"><div><span className="eyebrow">Tiga bulan</span><h2>Ke mana uang pergi</h2></div></div><CategoryDonutChart items={categories}/></article></section>
    <section className="surface recent-panel"><div className="section-title"><div><span className="eyebrow">Ledger</span><h2>Transaksi terbaru</h2></div><Link href="/transactions">Lihat semua <ArrowRight aria-hidden="true"/></Link></div><TransactionList compact items={transactions.slice(0, 8)}/></section></>}
  </AppShell>;
}

function Loading() { return <main className="loading"><div className="spinner"/><span>Memuat Richmod…</span></main>; }
