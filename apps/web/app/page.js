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
  const [latestWealth, setLatestWealth] = useState(null);
  const [cycle, setCycle] = useState(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    if (!user) return;
    setLoading(true);
    try { const responses = await Promise.all([fetch("/api/v1/analytics/overview"), fetch("/api/v1/analytics/cycle/daily"), fetch("/api/v1/analytics/categories?range=3"), fetch("/api/v1/transactions?limit=8"), fetch("/api/v1/analytics/cycle"), fetch("/api/v1/wealth/snapshots/latest")]);
      if (responses.some(response => !response.ok)) setError("Sebagian ringkasan belum dapat dimuat."); else setError("");
      if (responses[0].ok) setOverview(await responses[0].json()); if (responses[1].ok) { const cycleData = await responses[1].json(); setCashflow(elapsedDaily(cycleData.daily || [], cycleData.daysElapsed)); } if (responses[2].ok) setCategories(await responses[2].json()); if (responses[3].ok) setTransactions(await responses[3].json());
      if (responses[4].ok) setCycle(await responses[4].json());
      if (responses[5].ok) setLatestWealth(await responses[5].json());
    } catch { setError("Koneksi terputus saat memuat ringkasan."); } finally { setLoading(false); }
  }, [user]);

  useEffect(() => { load(); }, [load]);

  if (user === null) return <Loading />;
  if (user === false) return <LandingPage />;
  const periodLabel = overview?.periodKind === "CURRENT_CYCLE" ? "siklus ini" : "bulan ini";
  const cycleName = cycle?.kind === "CURRENT_CYCLE" ? "Siklus gaji" : "Bulan kalender";
  const cycleDates = cycle ? `${cycle.start}${cycle.end ? ` – ${cycle.end}` : " · masih berjalan"}` : "Periode belum tersedia";
  const wealthObservedAt = new Date(latestWealth?.observedAt);
  const wealthDate = latestWealth?.observedAt && !Number.isNaN(wealthObservedAt.valueOf()) ? wealthObservedAt.toLocaleDateString("id-ID") : null;
  const wealthItems = latestWealth?.items || [];
  const wealthAssetCount = wealthItems.filter(item => item.side === "ASSET").length;
  const wealthLiabilityCount = wealthItems.filter(item => item.side === "LIABILITY").length;
  return <AppShell user={user} eyebrow="Ringkasan" title="Keuangan keluarga" actions={<Link className="button secondary" href="/documents"><UploadSimple aria-hidden="true"/> Unggah bukti</Link>}>
    <ErrorNotice message={error} retry={load}/>
    {loading && <Skeleton/>}
    {!loading && <>
    <section className="overview-flow" aria-labelledby="overview-cashflow-title">
      <article className="surface overview-position">
        <header className="overview-position-header">
          <div><span className="eyebrow">Posisi saat ini</span><h2 id="overview-cashflow-title">Arus kas rumah tangga</h2></div>
          <div className="overview-period"><CalendarBlank aria-hidden="true"/><div><span>{cycleName}</span><small>{cycleDates}</small></div></div>
        </header>
        <div className="overview-position-grid">
          <div className="overview-net-cashflow"><span>Arus kas bersih · {periodLabel}</span><strong>{money(overview?.netCashflow)}</strong><small><CheckCircle aria-hidden="true" weight="fill"/> Hanya transaksi terkonfirmasi</small></div>
          <div className="overview-cashflow-parts" aria-label="Pemasukan dan pengeluaran">
            <div><span>Pemasukan</span><strong className="income">{money(overview?.income)}</strong><small>{periodLabel}</small></div>
            <div><span>Pengeluaran</span><strong className="expense">{money(overview?.expense)}</strong><small>{periodLabel}</small></div>
          </div>
          <div className="overview-allocation" aria-label="Alokasi surplus">
            <div><span>Tabungan dialokasikan</span><strong>{money(overview?.savingsAllocated)}</strong><small>Sudah punya tujuan</small></div>
            <div><span>Surplus belum dialokasikan</span><strong>{money(overview?.unallocatedSurplus)}</strong><small>Masih menunggu keputusan</small></div>
          </div>
        </div>
      </article>
    </section>
    {overview?.reviewCount > 0 && <Link className="review-alert" href="/inbox?view=transactions"><WarningCircle aria-hidden="true" weight="fill"/><div><b>{overview.reviewCount} transaksi membutuhkan keputusan</b><small>Belum masuk analisis sampai kamu mengonfirmasi interpretasinya.</small></div><strong>Buka tinjauan <ArrowRight aria-hidden="true"/></strong></Link>}
    <section className="overview-insights"><article className="surface overview-trend"><div className="section-title"><div><span className="eyebrow">{overview?.periodKind === "CURRENT_CYCLE" ? "Siklus gaji · harian" : "Bulan ini · harian"}</span><h2>Pengeluaran harian</h2></div><Link href="/analytics">Lihat analisis <ArrowRight aria-hidden="true"/></Link></div><DashboardDailySpendingChart items={cashflow}/></article><article className="surface overview-categories"><div className="section-title"><div><span className="eyebrow">Tiga bulan</span><h2>Ke mana uang pergi</h2></div></div><CategoryDonutChart items={categories}/></article></section>
    <section className="overview-lower"><article className="surface overview-activity"><div className="section-title"><div><span className="eyebrow">CATATAN TRANSAKSI</span><h2>Transaksi terbaru</h2></div><Link href="/transactions">Lihat semua <ArrowRight aria-hidden="true"/></Link></div><TransactionList compact items={transactions.slice(0, 8)}/></article><Link className="surface overview-wealth" href="/wealth"><div><span className="eyebrow">POSISI KEKAYAAN</span><h2>Kekayaan bersih keluarga</h2></div><strong>{latestWealth ? money(latestWealth.netWorthIdr) : "—"}</strong>{latestWealth && <div className="wealth-account-list" aria-label="Ringkasan aset dan kewajiban"><div className="wealth-account-row"><div><b>Total aset</b><small>{wealthAssetCount} akun</small></div><strong className="positive">{money(latestWealth.assetTotalIdr)}</strong></div><div className="wealth-account-row"><div><b>Total kewajiban</b><small>{wealthLiabilityCount} akun</small></div><strong className="negative">{money(latestWealth.liabilityTotalIdr)}</strong></div></div>}<small>{wealthDate ? `Diamati ${wealthDate}` : "Kekayaan belum diinisialisasi"}</small><span className="overview-wealth-link">Buka Kekayaan <ArrowRight aria-hidden="true"/></span></Link></section></>}
  </AppShell>;
}

function Loading() { return <main className="loading"><div className="spinner"/><span>Memuat Richmod…</span></main>; }
