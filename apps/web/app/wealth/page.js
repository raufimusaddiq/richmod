"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import useAuth from "../components/useAuth";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import { money, dateTime } from "../lib/format";

const empty = { accounts: [], snapshots: [], latest: null, previous: null, cycleRecaps: [] };

export default function WealthPage() {
  const user = useAuth();
  const [data, setData] = useState(empty), [error, setError] = useState(""), [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState(null), [working, setWorking] = useState(false);
  const load = useCallback(async () => {
    setLoading(true); setError("");
    try { const [accountsResponse, summaryResponse, latestResponse, historyResponse, cycleRecapsResponse] = await Promise.all([fetch("/api/v1/wealth/accounts"), fetch("/api/v1/wealth/summary"), fetch("/api/v1/wealth/snapshots/latest"), fetch("/api/v1/wealth/history"), fetch("/api/v1/wealth/cycle-recaps")]); if (![accountsResponse, summaryResponse, latestResponse, historyResponse, cycleRecapsResponse].every(response => response.ok)) throw new Error(); const [accounts, summary, latest, snapshots, cycleRecaps] = await Promise.all([accountsResponse.json(), summaryResponse.json(), latestResponse.json(), historyResponse.json(), cycleRecapsResponse.json()]); setData({ ...empty, accounts: Array.isArray(accounts) ? accounts : [], snapshots: Array.isArray(snapshots) ? snapshots : [], latest: latest || summary?.latest || null, previous: summary?.previous || null, cycleRecaps: Array.isArray(cycleRecaps) ? cycleRecaps : [] }); }
    catch { setError("Data Wealth belum dapat dimuat. Coba lagi."); } finally { setLoading(false); }
  }, []);
  useEffect(() => { if (user) load(); }, [user, load]);
  async function saveSnapshot(event) { event.preventDefault(); setWorking(true); const form = new FormData(event.currentTarget); const items = data.accounts.filter(item => item.active !== false).map(item => ({ wealthAccountId: item.id, valueIdr: form.get(`value-${item.id}`), source: "MANUAL", note: form.get(`note-${item.id}`) || null })); const observedAt = new Date(form.get("observedAt")).toISOString(); const response = await fetch("/api/v1/wealth/snapshots", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ observedAt, items }) }); if (!response.ok) setError("Snapshot belum dapat disimpan."); else { event.currentTarget.reset(); await load(); } setWorking(false); }
  async function openSnapshot(id) { setError(""); const response = await fetch(`/api/v1/wealth/snapshots/${id}`); if (!response.ok) { setError("Detail snapshot belum dapat dimuat."); return; } setSelected(await response.json()); }
  if (user === null) return <main className="loading">Memuat…</main>;
  if (!user) return null;
  const latest = data.latest || data.snapshots?.[0];
  const total = latest?.netWorthIdr || "0";
  return <AppShell user={user} eyebrow="WEALTH" title="Wealth dan kekayaan bersih" actions={<a className="button secondary" href="/settings#wealth-accounts">Kelola akun Wealth</a>}>
    <ErrorNotice message={error} retry={load}/>{loading ? <Skeleton cards={3} rows={3}/> : <>
      <section className="wealth-kpis"><article><span>Kekayaan bersih terakhir</span><strong>{money(total)}</strong><small>{latest?.observedAt ? `Snapshot ${dateTime(latest.observedAt)}` : "Belum ada snapshot"}</small></article><article><span>Aset</span><strong className="positive">{money(latest?.assetTotalIdr || "0")}</strong><small>Nilai observasi</small></article><article><span>Kewajiban</span><strong className="negative">{money(latest?.liabilityTotalIdr || "0")}</strong><small>Nilai observasi</small></article></section>
      <section className="surface wealth-section"><div className="section-title"><div><span className="eyebrow">SNAPSHOT</span><h2>Catat posisi Wealth</h2></div><span className="header-meta">Jumlah IDR tetap berupa string</span></div><form className="wealth-snapshot-form" onSubmit={saveSnapshot}><label>Waktu observasi<input name="observedAt" type="datetime-local" required defaultValue={new Date().toISOString().slice(0, 16)}/></label>{data.accounts.filter(item => item.active !== false).map(item => <label key={item.id}>{item.name}<small>{item.institution || item.wealthType || item.usageRole}</small><input name={`value-${item.id}`} inputMode="numeric" pattern="[0-9]+" placeholder="Nilai IDR" required/><input name={`note-${item.id}`} placeholder="Catatan (opsional)"/></label>)}{!data.accounts.length && <p className="empty compact">Belum ada Wealth Account. Tambahkan di Pengaturan.</p>}<button disabled={working || !data.accounts.length}>{working ? "Menyimpan…" : "Simpan snapshot"}</button></form></section>
      <section className="surface wealth-section"><div className="section-title"><div><span className="eyebrow">RIWAYAT</span><h2>Snapshot tersimpan</h2></div></div><div className="wealth-history">{data.snapshots.map(item => <button key={item.id} className="wealth-history-row" onClick={() => openSnapshot(item.id)}><span>{dateTime(item.observedAt)}</span><b>{money(item.netWorthIdr || "0")}</b><small>Lihat rincian akun</small></button>)}{!data.snapshots.length && <p className="empty compact">Belum ada snapshot Wealth.</p>}</div></section>
    </>}{selected && <div className="drawer-backdrop" onClick={() => setSelected(null)}><aside className="detail-drawer" onClick={event => event.stopPropagation()}><button className="drawer-close" aria-label="Tutup detail" onClick={() => setSelected(null)}>×</button><span className="eyebrow">DETAIL SNAPSHOT</span><h2>{dateTime(selected.observedAt)}</h2>{(selected.items || []).map(item => <div className="wealth-detail-row" key={item.id || item.wealthAccountId}><span>{item.name || item.accountName}</span><strong>{money(item.valueIdr)}</strong></div>)}</aside></div>}
  </AppShell>;
}
