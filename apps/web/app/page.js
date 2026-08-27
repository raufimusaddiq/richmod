"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import AppShell from "./components/AppShell";
import { ErrorNotice, Skeleton } from "./components/Feedback";
import { CashflowChart, CategoryChart } from "./components/Charts";
import TransactionList from "./components/TransactionList";
import useAuth from "./components/useAuth";
import { money } from "./lib/format";

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
    try { const responses = await Promise.all([fetch("/api/v1/analytics/overview"), fetch("/api/v1/analytics/cycle/daily"), fetch("/api/v1/analytics/categories?range=3"), fetch("/api/v1/transactions"), fetch("/api/v1/analytics/cycle")]);
      if (responses.some(response => !response.ok)) setError("Sebagian ringkasan belum dapat dimuat."); else setError("");
      if (responses[0].ok) setOverview(await responses[0].json()); if (responses[1].ok) { const cycleData = await responses[1].json(); setCashflow(cycleData.daily || []); } if (responses[2].ok) setCategories(await responses[2].json()); if (responses[3].ok) setTransactions(await responses[3].json());
      if (responses[4].ok) setCycle(await responses[4].json());
    } catch { setError("Koneksi terputus saat memuat ringkasan."); } finally { setLoading(false); }
  }, [user]);

  useEffect(() => { load(); }, [load]);

  if (user === null) return <Loading />;
  if (user === false) return <Login onSuccess={() => window.location.reload()} />;
  const periodLabel = overview?.periodKind === "CURRENT_CYCLE" ? "siklus ini" : "bulan ini";
  const cards = [[`Pemasukan ${periodLabel}`, overview?.income, "income"], [`Pengeluaran ${periodLabel}`, overview?.expense, "expense"], ["Arus kas bersih", overview?.netCashflow, "net"]];
  return <AppShell user={user} eyebrow="RINGKASAN" title={`Keuangan keluarga · ${periodLabel}`} actions={<Link className="button secondary" href="/documents">＋ Unggah dokumen</Link>}>
    <ErrorNotice message={error} retry={load}/>
    {loading && <Skeleton/>}
    {!loading && <>
    {cycle && <section className="surface" style={{padding:"14px 17px",marginBottom:14,display:"flex",justifyContent:"space-between",alignItems:"center",gap:12}}><div><span className="eyebrow">PERIODE AKTIF</span><strong style={{display:"block",marginTop:5}}>{cycle.kind === "CURRENT_CYCLE" ? "Siklus Gaji" : "Bulan Kalender"}</strong></div><small style={{color:"#6d776f"}}>{cycle.start}{cycle.end ? ` – ${cycle.end}` : " · masih berjalan"}</small></section>}
    {overview?.reviewCount > 0 && <Link className="review-alert" href="/reviews"><span>!</span><div><b>{overview.reviewCount} transaksi butuh bantuanmu</b><small>Selesaikan keputusan agar ledger tetap akurat.</small></div><strong>Buka Review Inbox →</strong></Link>}
    <section className="kpi-grid">{cards.map(([label, value, tone]) => <article key={label}><span>{label}</span><strong className={tone}>{money(value)}</strong><small>Data terkonfirmasi · IDR</small></article>)}<article><span>Review Inbox</span><strong>{overview?.reviewCount ?? "—"}</strong><small>Belum masuk analytics</small></article></section>
    <section className="dashboard-grid"><article className="surface chart-panel"><div className="section-title"><div><span className="eyebrow">SIKLUS GAJI · HARIAN</span><h2>Arus kas</h2></div><Link href="/analytics">Lihat analisis →</Link></div><CashflowChart items={cashflow}/></article><article className="surface category-panel"><div className="section-title"><div><span className="eyebrow">3 BULAN</span><h2>Ke mana uang pergi</h2></div></div><CategoryChart items={categories}/></article></section>
    <section className="surface recent-panel"><div className="section-title"><div><span className="eyebrow">LEDGER</span><h2>Transaksi terbaru</h2></div><Link href="/transactions">Lihat semua →</Link></div><TransactionList compact items={transactions.slice(0, 8)}/></section></>}
  </AppShell>;
}

function Login({ onSuccess }) {
  const [error, setError] = useState("");
  async function login(event) {
    event.preventDefault(); setError(""); const form = new FormData(event.currentTarget);
    const response = await fetch("/api/v1/auth/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ email: form.get("email"), password: form.get("password") }) });
    if (!response.ok) { setError("Email atau kata sandi tidak cocok."); return; }
    onSuccess();
  }
  return <main className="login"><section><div className="login-brand"><span>R</span><b>Richmod</b></div><span className="eyebrow">FAMILY FINANCE</span><h1>Uang keluarga,<br/>lebih mudah dipahami.</h1><p>Ledger household yang menyatukan Jago, Telegram, dan dokumen keuangan—tanpa menebak.</p><form onSubmit={login}><label>Email<input name="email" type="email" autoComplete="email" required /></label><label>Kata sandi<input name="password" type="password" autoComplete="current-password" required /></label>{error && <p className="error">{error}</p>}<button>Masuk ke Richmod</button></form><small>IDR · Waktu Indonesia Barat (GMT+7)</small></section></main>;
}

function Loading() { return <main className="loading"><div className="spinner"/><span>Memuat Richmod…</span></main>; }
