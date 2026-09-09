"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import useAuth from "../components/useAuth";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import { money, dateTime } from "../lib/format";
import { NetWorthHistoryChart } from "../components/Charts";

const empty = { accounts: [], snapshots: [], latest: null, previous: null, summary: null, currentCycle: null, cycleRecaps: [], observation: null };
const wealthTypeLabel = { BANK: "Bank", CASH: "Tunai", EWALLET: "Dompet digital", MUTUAL_FUND: "Reksa dana", GOLD: "Emas", BROKERAGE: "Brokerage", DEPOSIT: "Deposito", CRYPTO: "Kripto", LOAN: "Pinjaman", OTHER: "Lainnya" };
const usageRoleLabel = { TRANSACTIONAL: "Transaksional", SAVINGS: "Tabungan", INVESTMENT: "Investasi", OTHER: "Lainnya" };
const reviewStatusLabel = { CURRENT: "Perlu direkonsiliasi", RESOLVED: "Sudah direkonsiliasi", STALE: "Perlu diperbarui", LEFT_UNALLOCATED: "Dibiarkan belum dialokasikan", NO_LONGER_APPLICABLE: "Tidak berlaku", NOT_REVIEWED: "Belum ditinjau" };

export function jakartaDateTimeLocal(date = new Date()) {
  const parts = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Jakarta", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).formatToParts(date);
  const value = Object.fromEntries(parts.map(part => [part.type, part.value]));
  return `${value.year}-${value.month}-${value.day}T${value.hour}:${value.minute}`;
}

export function jakartaLocalToRFC3339(value) {
  return `${value}:00+07:00`;
}

function shortDate(value) {
  if (!value) return "—";
  return new Date(`${value}T00:00:00+07:00`).toLocaleDateString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", year: "numeric" });
}

function amountTone(value) {
  try { return BigInt(value || "0") > 0n ? "positive" : BigInt(value || "0") < 0n ? "negative" : ""; } catch { return ""; }
}

function signedMoney(value) {
  try { return BigInt(value || "0") > 0n ? `+${money(value)}` : money(value); } catch { return money(value); }
}

function accountMeta(item, accountMap) {
  const account = accountMap.get(item.wealthAccountId) || item;
  return [account.institution, wealthTypeLabel[account.wealthType], usageRoleLabel[account.usageRole]].filter(Boolean).join(" · ");
}

function WealthAccountList({ title, total, items, accountMap, liability = false }) {
  return <section className={`wealth-account-group ${liability ? "is-liability" : ""}`}>
    <header><div><span>{title}</span><small>{items.length} akun</small></div><strong className={liability ? "negative" : "positive"}>{money(total)}</strong></header>
    <div className="wealth-account-list">{items.map(item => <div className="wealth-account-row" key={item.wealthAccountId}>
      <div><b>{item.name}</b><small>{accountMeta(item, accountMap) || "Akun Wealth"}</small></div><strong>{money(item.valueIdr)}</strong>
    </div>)}{!items.length && <p className="empty compact">Belum ada {liability ? "kewajiban" : "aset"} pada snapshot ini.</p>}</div>
  </section>;
}

export default function WealthPage() {
  const user = useAuth();
  const [data, setData] = useState(empty), [error, setError] = useState(""), [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState(null), [working, setWorking] = useState(false), [correcting, setCorrecting] = useState(false);
  const [snapshotOpen, setSnapshotOpen] = useState(false);
  const load = useCallback(async () => {
    setLoading(true); setError("");
    try {
      const observationId = new URLSearchParams(window.location.search).get("observationId");
      const [accountsResponse, summaryResponse, latestResponse, historyResponse, currentCycleResponse, cycleRecapsResponse, observationResponse] = await Promise.all([
        fetch("/api/v1/wealth/accounts"), fetch("/api/v1/wealth/summary"), fetch("/api/v1/wealth/snapshots/latest"), fetch("/api/v1/wealth/history"), fetch("/api/v1/wealth/current-cycle-savings"), fetch("/api/v1/wealth/cycle-recaps"), observationId ? fetch(`/api/v1/wealth/observations/${observationId}`) : Promise.resolve(null),
      ]);
      if (![accountsResponse, summaryResponse, latestResponse, historyResponse, currentCycleResponse, cycleRecapsResponse].every(response => response.ok) || observationResponse && !observationResponse.ok) throw new Error();
      const [accounts, summary, latest, snapshots, currentCycle, cycleRecaps, observation] = await Promise.all([accountsResponse.json(), summaryResponse.json(), latestResponse.json(), historyResponse.json(), currentCycleResponse.json(), cycleRecapsResponse.json(), observationResponse ? observationResponse.json() : null]);
      setData({ ...empty, accounts: Array.isArray(accounts) ? accounts : [], snapshots: Array.isArray(snapshots) ? snapshots : [], latest: latest || summary?.latest || null, previous: summary?.previous || null, summary, currentCycle, cycleRecaps: Array.isArray(cycleRecaps) ? cycleRecaps : [], observation });
    } catch { setError("Data Wealth belum dapat dimuat. Coba lagi."); } finally { setLoading(false); }
  }, []);
  useEffect(() => { if (user) load(); }, [user, load]);

  async function saveSnapshot(event) {
    event.preventDefault(); setWorking(true);
    const form = new FormData(event.currentTarget);
    const items = data.accounts.filter(item => item.active !== false).map(item => {
      const result = { wealthAccountId: item.id, valueIdr: form.get(`value-${item.id}`), source: "MANUAL", note: form.get(`note-${item.id}`) || null };
      if (data.observation?.resolvedWealthAccountId === item.id) result.source = "DOCUMENT";
      return result;
    });
    const observedAt = jakartaLocalToRFC3339(form.get("observedAt"));
    const response = await fetch("/api/v1/wealth/snapshots", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ observedAt, observationId: data.observation?.id || null, items }) });
    if (!response.ok) setError("Snapshot belum dapat disimpan."); else { setSnapshotOpen(false); window.history.replaceState({}, "", "/wealth"); await load(); }
    setWorking(false);
  }

  async function openSnapshot(id) {
    setError(""); setCorrecting(false);
    const response = await fetch(`/api/v1/wealth/snapshots/${id}`);
    if (!response.ok) { setError("Detail snapshot belum dapat dimuat."); return; }
    setSelected(await response.json());
  }

  function closeSnapshot() {
    setSelected(null); setCorrecting(false);
  }

  async function correctSnapshot(event) {
    event.preventDefault(); setWorking(true);
    const form = new FormData(event.currentTarget);
    const items = (selected.items || []).map(item => ({ wealthAccountId: item.wealthAccountId, valueIdr: form.get(`value-${item.wealthAccountId}`), quantity: form.get(`quantity-${item.wealthAccountId}`) || null, unit: form.get(`unit-${item.wealthAccountId}`) || null, unitPriceIdr: form.get(`unitPrice-${item.wealthAccountId}`) || null, source: form.get(`source-${item.wealthAccountId}`) || "MANUAL", note: form.get(`note-${item.wealthAccountId}`) || null }));
    const response = await fetch(`/api/v1/wealth/snapshots/${selected.id}`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ observedAt: selected.observedAt, items }) });
    if (!response.ok) setError("Koreksi snapshot belum dapat disimpan."); else { closeSnapshot(); await load(); }
    setWorking(false);
  }

  if (user === null) return <main className="loading">Memuat…</main>;
  if (!user) return null;

  const latest = data.latest || data.snapshots?.[0];
  const previous = data.summary?.previous || data.previous;
  const previousValues = new Map((latest?.items || []).map(item => [item.wealthAccountId, item.valueIdr]));
  if (data.observation?.resolvedWealthAccountId) previousValues.set(data.observation.resolvedWealthAccountId, data.observation.observedValueIdr);
  const assets = (latest?.items || []).filter(item => item.side === "ASSET");
  const liabilities = (latest?.items || []).filter(item => item.side === "LIABILITY");
  const accountMap = new Map(data.accounts.map(item => [item.id, item]));
  const activeAccounts = data.accounts.filter(item => item.active !== false);
  const showSnapshotForm = !latest || snapshotOpen || Boolean(data.observation);

  return <AppShell user={user} eyebrow="WEALTH" title="Kekayaan bersih rumah tangga" actions={<a className="button secondary" href="/settings#wealth-accounts">Kelola akun Wealth</a>}>
    <div className="wealth-page">
      <ErrorNotice message={error} retry={load}/>
      {loading ? <Skeleton cards={3} rows={3}/> : <>
        {!latest ? <section className="surface wealth-empty"><span className="eyebrow">MULAI DARI POSISI SEKARANG</span><h2>Kekayaan belum diatur</h2><p>Tambahkan semua akun aset dan kewajiban, lalu simpan satu snapshot lengkap untuk mulai melihat Net Worth dan perubahannya.</p><a className="button secondary" href="/settings#wealth-accounts">Siapkan akun Wealth</a></section> : <>
          <section className="surface wealth-position">
            <div className="wealth-position-primary"><span className="eyebrow">POSISI TERKINI</span><h2>Net Worth</h2><strong>{money(latest.netWorthIdr)}</strong><p>Diamati {dateTime(latest.observedAt)}</p></div>
            <div className="wealth-balance-summary">
              <article><span>Total aset</span><strong className="positive">{money(latest.assetTotalIdr)}</strong><small>{assets.length} akun pada snapshot</small></article>
              <article><span>Total kewajiban</span><strong className="negative">{money(latest.liabilityTotalIdr)}</strong><small>{liabilities.length} akun pada snapshot</small></article>
            </div>
          </section>

          {previous && <section className="surface wealth-change">
            <div className="section-title"><div><span className="eyebrow">SEJAK SNAPSHOT SEBELUMNYA</span><h2>Apa yang mengubah Net Worth</h2></div><strong className={`wealth-change-total ${amountTone(data.summary?.netWorthChangeIdr)}`}>{signedMoney(data.summary?.netWorthChangeIdr)}</strong></div>
            <div className="wealth-change-equation">
              <div><span>Posisi sebelumnya</span><strong>{money(previous.netWorthIdr)}</strong><small>{dateTime(previous.observedAt)}</small></div><i aria-hidden="true">+</i>
              <div><span>Cashflow terkonfirmasi</span><strong className={amountTone(data.summary?.confirmedCashflowIdr)}>{signedMoney(data.summary?.confirmedCashflowIdr)}</strong><small>Dari ledger rumah tangga</small></div><i aria-hidden="true">+</i>
              <div><span>Valuasi / perubahan lain</span><strong className={amountTone(data.summary?.valuationAndOtherChangeIdr)}>{signedMoney(data.summary?.valuationAndOtherChangeIdr)}</strong><small>Selisih di luar cashflow</small></div><i aria-hidden="true">=</i>
              <div className="is-current"><span>Posisi sekarang</span><strong>{money(latest.netWorthIdr)}</strong><small>{dateTime(latest.observedAt)}</small></div>
            </div>
          </section>}

          <div className="wealth-analysis-grid">
            <section className="surface wealth-composition">
              <div className="section-title"><div><span className="eyebrow">KOMPOSISI WEALTH</span><h2>Di mana kekayaan tersimpan</h2></div></div>
              <div className="wealth-composition-grid"><WealthAccountList title="Aset" total={latest.assetTotalIdr} items={assets} accountMap={accountMap}/><WealthAccountList title="Kewajiban" total={latest.liabilityTotalIdr} items={liabilities} accountMap={accountMap} liability/></div>
            </section>
            {data.snapshots.length > 1 && <section className="surface wealth-trend"><div className="section-title"><div><span className="eyebrow">RIWAYAT NET WORTH</span><h2>Arah kekayaan dari waktu ke waktu</h2></div><span className="header-meta">{data.snapshots.length} snapshot</span></div><NetWorthHistoryChart items={data.snapshots} height={280}/></section>}
          </div>
        </>}

        {data.currentCycle && <section className="surface wealth-cycle">
          <div className="wealth-cycle-summary"><span className="eyebrow">{data.currentCycle.periodKind === "CURRENT_CYCLE" ? "TABUNGAN SIKLUS BERJALAN" : "TABUNGAN BULAN INI"}</span><h2>{money(data.currentCycle.savingsAllocated)}</h2><p>{shortDate(data.currentCycle.periodStart)} – {shortDate(data.currentCycle.periodEnd)}</p></div>
          <div className="wealth-cycle-destinations"><div className="section-title"><h3>Dialokasikan ke</h3><small>Transfer tabungan terkonfirmasi</small></div>{(data.currentCycle.savingsByDestination || []).map(item => <div className="wealth-allocation-row" key={item.wealthAccountId || item.name}><span>{item.name}</span><strong>{money(item.amountIdr)}</strong></div>)}{!data.currentCycle.savingsByDestination?.length && <p className="empty compact">Belum ada tujuan tabungan pada periode ini.</p>}</div>
        </section>}

        <section className="surface wealth-snapshot">
          <div className="section-title"><div><span className="eyebrow">PEMELIHARAAN POSISI</span><h2>{latest ? "Perbarui snapshot Wealth" : "Buat snapshot awal"}</h2><p className="section-copy">Verifikasi nilai setiap akun sebagai satu posisi lengkap rumah tangga.</p></div><div className="wealth-snapshot-heading-meta"><span className="header-meta">Asia/Jakarta · waktu dipilih pengguna</span>{latest && <button type="button" className="secondary" onClick={() => setSnapshotOpen(value => !value)}>{showSnapshotForm ? "Tutup form" : "Perbarui nilai"}</button>}</div></div>
          {data.observation?.observedDate && <div className="wealth-observation-note"><div><strong>Observasi dokumen siap digunakan</strong><span>{data.observation.institution || data.observation.accountHint || "Dokumen Wealth"} · bertanggal {data.observation.observedDate}</span></div><b>{money(data.observation.observedValueIdr)}</b><p>Pilih waktu observasi sebelum menyimpan. Richmod tidak menebak waktu dari dokumen.</p></div>}
          {showSnapshotForm && <form key={latest?.id || "initial"} className="wealth-snapshot-form" onSubmit={saveSnapshot}>
            <div className="wealth-snapshot-time"><label><span>Waktu observasi</span><input name="observedAt" type="datetime-local" required defaultValue={data.observation?.observedDate ? "" : jakartaDateTimeLocal()}/></label><p>Gunakan waktu ketika seluruh nilai di bawah benar-benar diamati.</p></div>
            <div className="wealth-snapshot-accounts">{activeAccounts.map(item => <fieldset className="wealth-snapshot-account" key={item.id}><legend><span>{item.name}</span><small>{[item.institution, wealthTypeLabel[item.wealthType], usageRoleLabel[item.usageRole]].filter(Boolean).join(" · ")}</small></legend><label><span>Nilai saat ini</span><input name={`value-${item.id}`} inputMode="numeric" pattern="[0-9]+" placeholder="Nilai IDR" required defaultValue={previousValues.get(item.id) || ""}/></label><label><span>Catatan</span><input name={`note-${item.id}`} placeholder="Opsional"/></label></fieldset>)}{!data.accounts.length && <p className="empty compact">Belum ada Wealth Account. Tambahkan di Pengaturan.</p>}</div>
            <div className="wealth-snapshot-actions"><p>Menyimpan snapshot lengkap untuk semua akun aktif.</p><button disabled={working || !data.accounts.length}>{working ? "Menyimpan…" : "Simpan snapshot lengkap"}</button></div>
          </form>}
        </section>

        {data.cycleRecaps.length > 0 && <section className="surface wealth-records"><div className="section-title"><div><span className="eyebrow">SIKLUS TABUNGAN</span><h2>Rekap siklus tertutup</h2></div></div><div className="wealth-cycle-history">{data.cycleRecaps.map(cycle => <article className="wealth-cycle-row" key={`${cycle.cycleStart}-${cycle.cycleEnd}`}><header><div><span>{shortDate(cycle.cycleStart)} – {shortDate(cycle.cycleEnd)}</span><small className={`wealth-review-status status-${String(cycle.residualReviewStatus || "").toLowerCase()}`}>{reviewStatusLabel[cycle.residualReviewStatus] || cycle.residualReviewStatus}</small></div><strong>Surplus {money(cycle.cashflowSurplus)}</strong></header><dl><div><dt>Dialokasikan</dt><dd>{money(cycle.savingsAllocated)}</dd></div><div><dt>Belum dialokasikan</dt><dd>{money(cycle.rawResidual)}</dd></div><div><dt>Tujuan tabungan</dt><dd>{(cycle.savingsByDestination || []).map(item => `${item.name} ${money(item.amountIdr)}`).join(" · ") || "Belum ada"}</dd></div></dl></article>)}</div></section>}

        <section className="surface wealth-records"><div className="section-title"><div><span className="eyebrow">RIWAYAT</span><h2>Snapshot tersimpan</h2></div><span className="header-meta">{data.snapshots.length} catatan</span></div><div className="wealth-history">{data.snapshots.map(item => <button key={item.id} className="wealth-history-row" onClick={() => openSnapshot(item.id)}><span>{dateTime(item.observedAt)}</span><strong>{money(item.netWorthIdr || "0")}</strong><small>Buka rincian akun</small></button>)}{!data.snapshots.length && <p className="empty compact">Belum ada snapshot Wealth.</p>}</div></section>
      </>}
    </div>

    {selected && <div className="drawer-backdrop" onClick={closeSnapshot}><aside className="detail-drawer wealth-drawer" role="dialog" aria-modal="true" aria-labelledby="wealth-snapshot-title" onClick={event => event.stopPropagation()}><button className="drawer-close" aria-label="Tutup detail" onClick={closeSnapshot}>×</button><span className="eyebrow">DETAIL SNAPSHOT</span><h2 id="wealth-snapshot-title">{dateTime(selected.observedAt)}</h2><p className="muted">Waktu observasi tidak dapat diubah setelah snapshot dibuat.</p>
      {!correcting ? <><div className="wealth-drawer-summary"><div><span>Net Worth</span><strong>{money(selected.netWorthIdr)}</strong></div><div><span>Aset</span><strong className="positive">{money(selected.assetTotalIdr)}</strong></div><div><span>Kewajiban</span><strong className="negative">{money(selected.liabilityTotalIdr)}</strong></div></div><div className="wealth-drawer-items">{(selected.items || []).map(item => <div className="wealth-detail-row" key={item.id || item.wealthAccountId}><div><span>{item.name || item.accountName}</span><small>{[wealthTypeLabel[item.wealthType], usageRoleLabel[item.usageRole], item.source].filter(Boolean).join(" · ")}</small>{item.note && <small>{item.note}</small>}</div><strong>{money(item.valueIdr)}</strong></div>)}</div><button className="button secondary" onClick={() => setCorrecting(true)}>Koreksi snapshot</button></> : <form className="wealth-correction-form" onSubmit={correctSnapshot}>{(selected.items || []).map(item => <fieldset className="wealth-correction-account" key={item.id || item.wealthAccountId}><legend>{item.name || item.accountName}</legend><label><span>Nilai</span><input name={`value-${item.wealthAccountId}`} inputMode="numeric" pattern="[0-9]+" required defaultValue={item.valueIdr}/></label><div className="wealth-correction-grid"><label><span>Quantity</span><input name={`quantity-${item.wealthAccountId}`} inputMode="decimal" defaultValue={item.quantity || ""}/></label><label><span>Unit</span><input name={`unit-${item.wealthAccountId}`} defaultValue={item.unit || ""}/></label><label><span>Harga per unit</span><input name={`unitPrice-${item.wealthAccountId}`} inputMode="numeric" defaultValue={item.unitPriceIdr || ""}/></label><label><span>Sumber</span><input name={`source-${item.wealthAccountId}`} defaultValue={item.source || "MANUAL"}/></label></div><label><span>Catatan</span><input name={`note-${item.wealthAccountId}`} defaultValue={item.note || ""}/></label></fieldset>)}<div className="wealth-drawer-actions"><button type="button" className="secondary" onClick={() => setCorrecting(false)}>Batal</button><button disabled={working}>{working ? "Menyimpan…" : "Simpan koreksi"}</button></div></form>}
    </aside></div>}
  </AppShell>;
}
