"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { ArrowRight, CalendarBlank, Camera, Plus, WarningCircle } from "@phosphor-icons/react";
import AppShell from "./components/AppShell";
import { ErrorNotice, Skeleton } from "./components/Feedback";
import { DashboardDailySpendingChart } from "./components/Charts";
import CategoryBars from "./components/CategoryBars";
import { useInboxCounts } from "./components/InboxCountProvider";
import { useQuickRecord } from "./components/QuickRecordProvider";
import TransactionList from "./components/TransactionList";
import LoadingShell from "./components/LoadingShell";
import useAuth from "./components/useAuth";
import { dayLabel, elapsedDaily } from "./lib/chartData";
import { money, dateTime, amountTone } from "./lib/format";
import { cycleRangeLabel } from "./lib/cycleDates";
import { comparisonLine, dayLedgerHref, homeState, spendingShare } from "./lib/home";
import LandingPage from "./components/LandingPage";

// Each dashboard section and the endpoint that feeds it. The response order is
// the order here, and a failed section is reported by this name. The category
// window is the current period, so its total agrees with the hero card.
const sections = [
  ["ringkasan", "/api/v1/analytics/overview"],
  ["pengeluaran harian", "/api/v1/analytics/cycle/daily"],
  ["kategori", "/api/v1/analytics/categories?period=current_cycle"],
  ["transaksi terbaru", "/api/v1/transactions?limit=8&exclude_voided=true"],
  ["periode siklus", "/api/v1/analytics/cycle"],
  ["kekayaan", "/api/v1/wealth/snapshots/latest"],
];

const arrow = direction => (direction === "down" ? "▼" : direction === "up" ? "▲" : "•");

export default function Home() {
  const user = useAuth(false);
  const { open: openQuickRecord } = useQuickRecord();
  const { actionCount = 0 } = useInboxCounts();
  const [overview, setOverview] = useState(null);
  const [cashflow, setCashflow] = useState([]);
  const [categories, setCategories] = useState([]);
  const [categoryWindow, setCategoryWindow] = useState(null);
  const [transactions, setTransactions] = useState([]);
  const [latestWealth, setLatestWealth] = useState(null);
  const [cycle, setCycle] = useState(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    if (!user) return;
    setLoading(true);
    try {
      const responses = await Promise.all(sections.map(([, url]) => fetch(url)));
      // Setters are looked up by section name, so reordering `sections` cannot
      // bind a response to the wrong state.
      const apply = {
        "ringkasan": setOverview,
        "pengeluaran harian": data => setCashflow(elapsedDaily(data.daily || [], data.daysElapsed)),
        "kategori": (data, response) => {
          setCategories(data);
          const h = name => response.headers.get(name);
          setCategoryWindow({ start: h("X-Window-Start"), end: h("X-Window-End") });
        },
        "transaksi terbaru": setTransactions,
        "periode siklus": setCycle,
        "kekayaan": setLatestWealth,
      };
      const failed = [];
      for (const [index, [name]] of sections.entries()) {
        try {
          if (!responses[index].ok) throw new Error();
          apply[name](await responses[index].json(), responses[index]);
        } catch { failed.push(name); }
      }
      setError(failed.length ? `Belum termuat: ${failed.join(", ")}. Bagian lain tetap ditampilkan.` : "");
    } catch { setError("Koneksi terputus saat memuat ringkasan."); } finally { setLoading(false); }
  }, [user]);

  useEffect(() => { load(); }, [load]);
  // A transaction recorded (or undone) from the global "+ Catat" sheet refreshes the figures in place.
  useEffect(() => { window.addEventListener("richmod:transaction-created", load); return () => window.removeEventListener("richmod:transaction-created", load); }, [load]);

  if (user === null) return <Loading />;
  if (user === false) return <LandingPage />;
  const periodLabel = overview?.periodKind === "CURRENT_CYCLE" ? "siklus ini" : "bulan ini";
  const cycleName = cycle?.kind === "CURRENT_CYCLE" ? "Siklus gaji" : "Bulan kalender";
  const cycleDates = cycle ? cycleRangeLabel({ start: cycle.start, end: cycle.end, active: cycle.kind === "CURRENT_CYCLE" }) : "Periode belum tersedia";
  const wealthDate = latestWealth?.observedAt ? dateTime(latestWealth.observedAt) : null;
  const wealthItems = latestWealth?.items || [];
  const wealthAssetCount = wealthItems.filter(item => item.side === "ASSET").length;
  const wealthLiabilityCount = wealthItems.filter(item => item.side === "LIABILITY").length;
  const state = homeState(overview, transactions);
  const comparison = comparisonLine(overview);
  const share = spendingShare(overview);
  const owner = user?.household?.role === "OWNER";
  const compare = comparison && <span className={`home-compare ${comparison.direction}`}>{arrow(comparison.direction)} {comparison.direction !== "same" && <b>{money(comparison.amount)}</b>} {comparison.text}</span>;
  const wealthCard = <Link className="surface home-card overview-wealth" href={latestWealth?.id ? `/wealth?snapshotId=${latestWealth.id}` : "/wealth"}><div><span className="eyebrow">Posisi kekayaan</span><h2>Kekayaan bersih</h2></div><strong>{latestWealth ? money(latestWealth.netWorthIdr) : "—"}</strong>{latestWealth && <div className="wealth-account-list" aria-label="Ringkasan aset dan kewajiban"><div className="wealth-account-row"><div><b>Total aset</b><small>{wealthAssetCount} akun</small></div><strong className={amountTone(latestWealth.assetTotalIdr)}>{money(latestWealth.assetTotalIdr)}</strong></div><div className="wealth-account-row"><div><b>Total kewajiban</b><small>{wealthLiabilityCount} akun</small></div><strong className={amountTone(latestWealth.liabilityTotalIdr, true)}>{money(latestWealth.liabilityTotalIdr)}</strong></div></div>}<small>{wealthDate ? `Diamati ${wealthDate}` : "Kekayaan belum diinisialisasi"}</small><span className="overview-wealth-link">Buka Kekayaan <ArrowRight aria-hidden="true"/></span></Link>;
  return <AppShell user={user} eyebrow="Ringkasan" title="Keuangan keluarga">
    <ErrorNotice message={error} retry={load}/>
    {owner && actionCount > 0 && <Link className="home-notice" href="/household?tab=sumber#tindakan"><WarningCircle aria-hidden="true" weight="fill"/><span>{actionCount} langkah setup atau pemrosesan butuh perhatian</span><strong>Buka <ArrowRight aria-hidden="true"/></strong></Link>}
    {loading && <Skeleton label="Memuat ringkasan keuangan"/>}
    {!loading && state === "empty" && <>
      <section className="surface home-hero home-welcome" aria-labelledby="home-welcome-title">
        <span className="eyebrow">Selamat datang</span>
        <h2 id="home-welcome-title">Mulai catat keuangan keluarga</h2>
        <p>Catat satu transaksi atau foto struk, slip gaji, dan bukti transfer. Ringkasan siklus muncul di sini begitu ada data.</p>
        <div className="home-hero-actions"><button type="button" onClick={() => openQuickRecord()}><Plus aria-hidden="true"/> Catat transaksi pertama</button><button type="button" className="secondary" onClick={() => openQuickRecord({ mode: "photo" })}><Camera aria-hidden="true"/> Foto bukti</button></div>
      </section>
      <section className="overview-lower home-lower-single">{wealthCard}</section>
    </>}
    {!loading && state !== "empty" && <>
    <section className="surface home-hero" aria-labelledby="overview-cashflow-title">
      <header className="home-hero-head">
        <span className="eyebrow">{state === "expenses-only" ? `Pengeluaran ${periodLabel}` : `Sisa ${periodLabel}`}</span>
        <div className="overview-period"><CalendarBlank aria-hidden="true"/><div><span>{cycleName}</span><small>{cycleDates}</small></div></div>
      </header>
      {state === "expenses-only"
        ? <div className="home-hero-main">
          <h2 id="overview-cashflow-title" className="visually-hidden">Pengeluaran {periodLabel}</h2>
          <strong className="home-hero-amount expense">{money(overview?.expense)}</strong>
          {comparison && <p className="home-share-label">{compare}</p>}
          <p className="home-hero-prompt">Belum ada pemasukan tercatat {periodLabel}. Catat gaji atau pemasukan lain supaya sisa uang terlihat.</p>
          <div className="home-hero-actions"><button type="button" onClick={() => openQuickRecord()}><Plus aria-hidden="true"/> Catat pemasukan</button></div>
        </div>
        : <>
          <div className="home-hero-main">
            <h2 id="overview-cashflow-title" className="visually-hidden">Sisa {periodLabel}</h2>
            <strong className={`home-hero-amount ${amountTone(overview?.netCashflow)}`}>{money(overview?.netCashflow)}</strong>
            <div className="home-share" role="img" aria-label={`Pengeluaran ${share}% dari pemasukan`}><span style={{ width: `${share}%` }}/></div>
            <p className="home-share-label">Keluar {share}% dari pemasukan{comparison && <> · {compare}</>}</p>
          </div>
          <dl className="home-figures"><div><dt>Masuk</dt><dd className="income">{money(overview?.income)}</dd></div><div><dt>Keluar</dt><dd className="expense">{money(overview?.expense)}</dd></div><div><dt>Sudah ditabung</dt><dd>{money(overview?.savingsAllocated)}</dd></div><div><dt>Belum dialokasikan</dt><dd>{money(overview?.unallocatedSurplus)}</dd></div></dl>
        </>}
    </section>
    {overview?.reviewCount > 0 && <Link className="review-alert" href="/inbox?view=transactions"><WarningCircle aria-hidden="true" weight="fill"/><div><b>{overview.reviewCount} transaksi membutuhkan keputusan</b><small>Belum masuk analisis sampai kamu mengonfirmasi interpretasinya.</small></div><strong>Buka tinjauan <ArrowRight aria-hidden="true"/></strong></Link>}
    <section className="overview-insights"><article className="surface home-card overview-trend"><div className="section-title"><div><span className="eyebrow">{overview?.periodKind === "CURRENT_CYCLE" ? "Siklus gaji · harian" : "Bulan ini · harian"}</span><h2>Pengeluaran harian</h2></div><Link href="/analytics">Lihat analisis <ArrowRight aria-hidden="true"/></Link></div><DashboardDailySpendingChart items={cashflow}/>{cashflow.length > 0 && <details className="review-daily"><summary>Lihat nilai pengeluaran harian</summary><div className="review-table-wrap" tabIndex={0} role="region" aria-label="Nilai pengeluaran harian, geser untuk semua kolom"><table><caption className="visually-hidden">Pengeluaran bersih harian</caption><thead><tr><th scope="col">Tanggal</th><th scope="col">Pengeluaran</th><th scope="col">Refund</th></tr></thead><tbody>{cashflow.map(item => <tr key={item.period}><th scope="row">{Number(item.expense) > 0 ? <Link href={dayLedgerHref(item.period)}>{dayLabel(item.period)}</Link> : dayLabel(item.period)}</th><td>{money(item.expense)}</td><td>{money(item.refund)}</td></tr>)}</tbody></table></div></details>}</article><article className="surface home-card overview-categories"><div className="section-title"><div><span className="eyebrow">{overview?.periodKind === "CURRENT_CYCLE" ? "Siklus gaji ini" : "Bulan ini"}</span><h2>Ke mana uangnya</h2></div></div><CategoryBars items={categories} window={categoryWindow}/></article></section>
    <section className="overview-lower"><article className="surface home-card overview-activity"><div className="section-title"><div><span className="eyebrow">Catatan transaksi</span><h2>Terbaru</h2></div><Link href="/transactions">Lihat semua <ArrowRight aria-hidden="true"/></Link></div><TransactionList compact items={transactions.slice(0, 8)}/></article>{wealthCard}</section></>}
  </AppShell>;
}

function Loading() { return <LoadingShell />; }
