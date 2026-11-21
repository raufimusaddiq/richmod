"use client";

import { useEffect, useRef, useState } from "react";
import useDrawerA11y from "../components/useDrawerA11y";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import { LoadError, useDrawerRecord, useGuardedLoad } from "../lib/useLoadState";
import { money, dateTime, displayDate as shortDate, amountTone } from "../lib/format";
import { cycleRangeLabel } from "../lib/cycleDates";
import { NetWorthHistoryChart } from "../components/Charts";
import { usageRoleLabel, wealthTypeLabel, snapshotSourceLabel } from "../lib/labels";

const empty = { accounts: [], snapshots: [], latest: null, previous: null, summary: null, currentCycle: null, cycleRecaps: [], observation: null, observations: [] };
const reviewStatusLabel = { CURRENT: "Perlu direkonsiliasi", RESOLVED: "Sudah direkonsiliasi", STALE: "Perlu diperbarui", LEFT_UNALLOCATED: "Dibiarkan belum dialokasikan", NO_LONGER_APPLICABLE: "Tidak berlaku", NOT_REVIEWED: "Belum ditinjau" };

export function jakartaDateTimeLocal(date = new Date()) {
  const parts = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Jakarta", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).formatToParts(date);
  const value = Object.fromEntries(parts.map(part => [part.type, part.value]));
  return `${value.year}-${value.month}-${value.day}T${value.hour}:${value.minute}`;
}

export function jakartaLocalToRFC3339(value) {
  return `${value}:00+07:00`;
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
    <header><div><span>{title}</span><small>{items.length} akun</small></div><strong className={amountTone(total, liability)}>{money(total)}</strong></header>
    <div className="wealth-account-list">{items.map(item => <div className="wealth-account-row" key={item.wealthAccountId}>
      <div><b>{item.name}</b><small>{accountMeta(item, accountMap) || "Akun kekayaan"}</small></div><strong>{money(item.valueIdr)}</strong>
    </div>)}{!items.length && <p className="empty compact">Belum ada {liability ? "kewajiban" : "aset"} pada catatan ini.</p>}</div>
  </section>;
}

function dropQueryParam(name) {
  const url = new URL(window.location.href);
  if (!url.searchParams.has(name)) return;
  url.searchParams.delete(name);
  window.history.replaceState(null, "", `${url.pathname}${url.search}`);
}

export default function WealthWorkspace({ user }) {
  const [data, setData] = useState(empty), [error, setError] = useState("");
  const [working, setWorking] = useState(false), [correcting, setCorrecting] = useState(false);
  const drawer = useDrawerRecord(async (id, signal) => {
    const response = await fetch(`/api/v1/wealth/snapshots/${id}`, { signal });
    if (!response.ok) throw new LoadError("Detail catatan posisi belum dapat dimuat.");
    return response.json();
  }, "Detail catatan posisi belum dapat dimuat.");
  const selected = drawer.status === "ready" ? drawer.data : null;
  const beforeSave = useRef(null);
  const drawerRef = useDrawerA11y(drawer.status !== "closed", () => closeSnapshot());
  const [snapshotOpen, setSnapshotOpen] = useState(false);
  const loader = useGuardedLoad(async ({ signal, current }) => {
    const observationId = new URLSearchParams(window.location.search).get("observationId");
    const [accountsResponse, summaryResponse, latestResponse, historyResponse, currentCycleResponse, cycleRecapsResponse, observationResponse, observationsResponse] = await Promise.all([
      fetch("/api/v1/wealth/accounts", { signal }), fetch("/api/v1/wealth/summary", { signal }), fetch("/api/v1/wealth/snapshots/latest", { signal }), fetch("/api/v1/wealth/history", { signal }), fetch("/api/v1/wealth/current-cycle-savings", { signal }), fetch("/api/v1/wealth/cycle-recaps", { signal }), observationId ? fetch(`/api/v1/wealth/observations/${observationId}`, { signal }) : Promise.resolve(null), fetch("/api/v1/wealth/observations", { signal }),
    ]);
    if (![accountsResponse, summaryResponse, latestResponse, historyResponse, currentCycleResponse, cycleRecapsResponse, observationsResponse].every(response => response.ok) || observationResponse && !observationResponse.ok) throw new LoadError("Data kekayaan belum dapat dimuat. Coba lagi.");
    const [accounts, summary, latest, snapshots, currentCycle, cycleRecaps, observation, observations] = await Promise.all([accountsResponse.json(), summaryResponse.json(), latestResponse.json(), historyResponse.json(), currentCycleResponse.json(), cycleRecapsResponse.json(), observationResponse ? observationResponse.json() : null, observationsResponse.json()]);
    if (!current()) return;
    setData({ ...empty, accounts: Array.isArray(accounts) ? accounts : [], snapshots: Array.isArray(snapshots) ? snapshots : [], latest: latest || summary?.latest || null, previous: summary?.previous || null, summary, currentCycle, cycleRecaps: Array.isArray(cycleRecaps) ? cycleRecaps : [], observation, observations: Array.isArray(observations) ? observations : [] });
  }, { fallback: "Data kekayaan belum dapat dimuat. Coba lagi.", onFailure: () => setData(empty) });
  const load = loader.run;
  const loading = loader.loading || loader.refreshing;
  useEffect(() => { if (user) load(); }, [user, load]);
  useEffect(() => {
    if (!loading && loader.hasLoaded) beforeSave.current = null;
  }, [loading, loader.hasLoaded, data]);
  useEffect(() => {
    const snapshotId = new URLSearchParams(window.location.search).get("snapshotId");
    if (user && snapshotId) openSnapshot(snapshotId);
  }, [user]);

  async function saveSnapshot(event) {
    event.preventDefault();
    if (working) return;
    const formEl = event.currentTarget, form = new FormData(formEl);
    setWorking(true); setError("");
    try {
      const items = data.accounts.filter(item => item.active !== false).map(item => {
        const result = { wealthAccountId: item.id, valueIdr: form.get(`value-${item.id}`), source: "MANUAL", note: form.get(`note-${item.id}`) || null };
        if (data.observation?.resolvedWealthAccountId === item.id) result.source = "DOCUMENT";
        return result;
      });
      const observedAt = jakartaLocalToRFC3339(form.get("observedAt"));
      const response = await fetch("/api/v1/wealth/snapshots", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ observedAt, observationId: data.observation?.id || null, items }) });
      if (!response.ok) { setError("Catatan posisi belum dapat disimpan."); return; }
      beforeSave.current = latest ? [latest.netWorthIdr, latest.assetTotalIdr, latest.liabilityTotalIdr] : null; setSnapshotOpen(false); dropQueryParam("observationId");
      await load();
    } catch { setError("Koneksi terputus. Catatan posisi belum disimpan."); }
    finally { setWorking(false); }
  }

  function openSnapshot(id) {
    setError(""); setCorrecting(false);
    return drawer.open(id);
  }

  function closeSnapshot() {
    drawer.close(); setCorrecting(false); dropQueryParam("snapshotId");
  }

  async function correctSnapshot(event) {
    event.preventDefault();
    if (working || !selected) return;
    const form = new FormData(event.currentTarget);
    setWorking(true); setError("");
    try {
      const items = (selected.items || []).map(item => ({ wealthAccountId: item.wealthAccountId, valueIdr: form.get(`value-${item.wealthAccountId}`), quantity: form.get(`quantity-${item.wealthAccountId}`) || null, unit: form.get(`unit-${item.wealthAccountId}`) || null, unitPriceIdr: form.get(`unitPrice-${item.wealthAccountId}`) || null, source: form.get(`source-${item.wealthAccountId}`) || "MANUAL", note: form.get(`note-${item.wealthAccountId}`) || null }));
      const response = await fetch(`/api/v1/wealth/snapshots/${selected.id}`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ observedAt: selected.observedAt, items }) });
      if (!response.ok) { setError("Koreksi catatan posisi belum dapat disimpan."); return; }
      beforeSave.current = latest ? [latest.netWorthIdr, latest.assetTotalIdr, latest.liabilityTotalIdr] : null; closeSnapshot();
      await load();
    } catch { setError("Koneksi terputus. Koreksi catatan posisi belum disimpan."); }
    finally { setWorking(false); }
  }

  const latest = data.latest || data.snapshots?.[0];
  const moved = (value, index) => Boolean(beforeSave.current) && beforeSave.current[index] !== value;
  const previous = data.summary?.previous || data.previous;
  const previousValues = new Map((latest?.items || []).map(item => [item.wealthAccountId, item.valueIdr]));
  if (data.observation?.resolvedWealthAccountId) previousValues.set(data.observation.resolvedWealthAccountId, data.observation.observedValueIdr);
  const assets = (latest?.items || []).filter(item => item.side === "ASSET");
  const liabilities = (latest?.items || []).filter(item => item.side === "LIABILITY");
  const accountMap = new Map(data.accounts.map(item => [item.id, item]));
  const activeAccounts = data.accounts.filter(item => item.active !== false);
  const showSnapshotForm = !latest || snapshotOpen || Boolean(data.observation);

  return <>
    <div className="wealth-page">
      <div className="wealth-toolbar"><a className="button secondary" href="/household?tab=akun#wealth-accounts">Kelola akun kekayaan</a></div>
      <ErrorNotice message={error || loader.error} retry={error ? undefined : () => load()}/>
      {!loader.hasLoaded && !loader.failed ? <Skeleton cards={3} rows={3}/> : loader.failed ? null : <>
        {!latest ? <section className="surface wealth-empty"><span className="eyebrow">Mulai dari posisi sekarang</span><h2>Kekayaan belum diatur</h2><p>Tambahkan semua akun aset dan kewajiban, lalu simpan satu catatan lengkap untuk mulai melihat kekayaan bersih dan perubahannya.</p><a className="button secondary" href="/household?tab=akun#wealth-accounts">Siapkan akun kekayaan</a></section> : <>
          <section className="surface wealth-position">
            <div className="wealth-position-primary"><span className="eyebrow">Posisi terkini</span><h2>Kekayaan bersih</h2><strong data-updated={moved(latest.netWorthIdr, 0) || undefined}>{money(latest.netWorthIdr)}</strong><p>Diamati {dateTime(latest.observedAt)}</p></div>
            <div className="wealth-balance-summary">
              <article><span>Total aset</span><strong className={amountTone(latest.assetTotalIdr)} data-updated={moved(latest.assetTotalIdr, 1) || undefined}>{money(latest.assetTotalIdr)}</strong><small>{assets.length} akun pada catatan posisi</small></article>
              <article><span>Total kewajiban</span><strong className={amountTone(latest.liabilityTotalIdr, true)} data-updated={moved(latest.liabilityTotalIdr, 2) || undefined}>{money(latest.liabilityTotalIdr)}</strong><small>{liabilities.length} akun pada catatan posisi</small></article>
            </div>
          </section>

          {previous && <section className="surface wealth-change">
            <div className="section-title"><div><span className="eyebrow">Sejak catatan sebelumnya</span><h2>Apa yang mengubah kekayaan bersih</h2></div><strong className={`wealth-change-total ${amountTone(data.summary?.netWorthChangeIdr)}`}>{signedMoney(data.summary?.netWorthChangeIdr)}</strong></div>
            <div className="wealth-change-equation">
              <div><span>Posisi sebelumnya</span><strong>{money(previous.netWorthIdr)}</strong><small>{dateTime(previous.observedAt)}</small></div><i aria-hidden="true">+</i>
              <div><span>Arus kas terkonfirmasi</span><strong className={amountTone(data.summary?.confirmedCashflowIdr)}>{signedMoney(data.summary?.confirmedCashflowIdr)}</strong><small>Dari catatan transaksi rumah tangga</small></div><i aria-hidden="true">+</i>
              <div><span>Valuasi / perubahan lain</span><strong className={amountTone(data.summary?.valuationAndOtherChangeIdr)}>{signedMoney(data.summary?.valuationAndOtherChangeIdr)}</strong><small>Selisih di luar cashflow</small></div><i aria-hidden="true">=</i>
              <div className="is-current"><span>Posisi sekarang</span><strong>{money(latest.netWorthIdr)}</strong><small>{dateTime(latest.observedAt)}</small></div>
            </div>
          </section>}

          <div className="wealth-analysis-grid">
            <section className="surface wealth-composition">
              <div className="section-title"><div><span className="eyebrow">Komposisi kekayaan</span><h2>Di mana kekayaan tersimpan</h2></div></div>
              <div className="wealth-composition-grid"><WealthAccountList title="Aset" total={latest.assetTotalIdr} items={assets} accountMap={accountMap}/><WealthAccountList title="Kewajiban" total={latest.liabilityTotalIdr} items={liabilities} accountMap={accountMap} liability/></div>
            </section>
            {data.snapshots.length > 1 && <section className="surface wealth-trend"><div className="section-title"><div><span className="eyebrow">Riwayat kekayaan bersih</span><h2>Arah kekayaan dari waktu ke waktu</h2></div><span className="header-meta">{data.snapshots.length} catatan</span></div><NetWorthHistoryChart items={data.snapshots} height={280}/></section>}
          </div>
        </>}

        {data.currentCycle && <section className="surface wealth-cycle">
          <div className="wealth-cycle-summary"><span className="eyebrow">{data.currentCycle.periodKind === "CURRENT_CYCLE" ? "Siklus gaji" : "Bulan ini"}</span><h2>{data.currentCycle.periodKind === "CURRENT_CYCLE" ? "Tabungan siklus berjalan" : "Tabungan bulan ini"}</h2><strong className="wealth-cycle-amount">{money(data.currentCycle.savingsAllocated)}</strong><p>{cycleRangeLabel({ start: data.currentCycle.periodStart, end: data.currentCycle.periodEnd, active: data.currentCycle.periodKind === "CURRENT_CYCLE" })}</p></div>
          <div className="wealth-cycle-destinations"><div className="section-title"><h3>Dialokasikan ke</h3><small>Transfer tabungan terkonfirmasi</small></div>{(data.currentCycle.savingsByDestination || []).map(item => <div className="wealth-allocation-row" key={item.wealthAccountId || item.name}><span>{item.name}</span><strong>{money(item.amountIdr)}</strong></div>)}{!data.currentCycle.savingsByDestination?.length && <p className="empty compact">Belum ada tujuan tabungan pada periode ini.</p>}</div>
        </section>}

        <section className="surface wealth-snapshot">
          <div className="section-title"><div><span className="eyebrow">Pemeliharaan posisi</span><h2>{latest ? "Perbarui snapshot kekayaan" : "Buat snapshot awal"}</h2><p className="section-copy">Verifikasi nilai setiap akun sebagai satu posisi lengkap rumah tangga.</p></div><div className="wealth-snapshot-heading-meta"><span className="header-meta">Asia/Jakarta · waktu dipilih pengguna</span>{latest && <button type="button" className="secondary" onClick={() => setSnapshotOpen(value => !value)}>{showSnapshotForm ? "Tutup form" : "Perbarui nilai"}</button>}</div></div>
          {data.observation?.observedDate && <div className="wealth-observation-note"><div><strong>Observasi dokumen siap digunakan</strong><span>{data.observation.institution || data.observation.accountHint || "Dokumen kekayaan"} · bertanggal {data.observation.observedDate}</span></div><b>{money(data.observation.observedValueIdr)}</b><p>Pilih waktu observasi sebelum menyimpan. Richmod tidak menebak waktu dari dokumen.</p></div>}
          {showSnapshotForm && <form key={latest?.id || "initial"} className="wealth-snapshot-form" onSubmit={saveSnapshot}>
            <div className="wealth-snapshot-time"><label><span>Waktu observasi</span><input name="observedAt" type="datetime-local" required defaultValue={data.observation?.observedDate ? "" : jakartaDateTimeLocal()}/></label><p>Gunakan waktu ketika seluruh nilai di bawah benar-benar diamati.</p></div>
            <div className="wealth-snapshot-accounts">{activeAccounts.map(item => <fieldset className="wealth-snapshot-account" key={item.id}><legend><span>{item.name}</span><small>{[item.institution, wealthTypeLabel[item.wealthType], usageRoleLabel[item.usageRole]].filter(Boolean).join(" · ")}</small></legend><label><span>Nilai saat ini</span><input name={`value-${item.id}`} inputMode="numeric" pattern="[0-9]+" enterKeyHint="next" placeholder="Nilai IDR" required defaultValue={previousValues.get(item.id) || ""}/></label><label><span>Catatan</span><input name={`note-${item.id}`} autoCapitalize="sentences" enterKeyHint="next" placeholder="Opsional"/></label></fieldset>)}{!data.accounts.length && <p className="empty compact">Belum ada akun kekayaan. Tambahkan di Pengaturan.</p>}</div>
            <div className="wealth-snapshot-actions"><p>Menyimpan catatan lengkap untuk semua akun aktif.</p><button disabled={working || !data.accounts.length}>{working ? "Menyimpan…" : "Simpan catatan lengkap"}</button></div>
          </form>}
        </section>
        {data.observations.length > 0 && <section className="surface wealth-records"><div className="section-title"><div><span className="eyebrow">Observasi per akun</span><h2>Bukti saldo yang diterima</h2><p className="section-copy">Observasi ini terikat ke akun, tetapi bukan snapshot lengkap dan tidak dihitung ke total kekayaan bersih.</p></div></div><div className="wealth-history">{data.observations.map(item => <article className="wealth-history-row" key={item.id}><span>{item.name}{item.observedDate ? ` · ${shortDate(item.observedDate)}` : ""}</span><strong>{money(item.observedValueIdr)}</strong><small>Diterima {dateTime(item.createdAt)}</small></article>)}</div></section>}

        {data.cycleRecaps.length > 0 && <section className="surface wealth-records"><div className="section-title"><div><span className="eyebrow">Siklus tabungan</span><h2>Rekap siklus tertutup</h2></div></div><div className="wealth-cycle-history">{data.cycleRecaps.map(cycle => <article className="wealth-cycle-row" key={`${cycle.cycleStart}-${cycle.cycleEnd}`}><header><div><span>{cycleRangeLabel({ start: cycle.cycleStart, end: cycle.cycleEnd })}</span><small className={`wealth-review-status status-${String(cycle.residualReviewStatus || "").toLowerCase()}`}>{reviewStatusLabel[cycle.residualReviewStatus] || cycle.residualReviewStatus}</small></div><strong>Surplus {money(cycle.cashflowSurplus)}</strong></header><dl><div><dt>Dialokasikan</dt><dd>{money(cycle.savingsAllocated)}</dd></div><div><dt>Belum dialokasikan</dt><dd>{money(cycle.rawResidual)}</dd></div><div><dt>Tujuan tabungan</dt><dd>{(cycle.savingsByDestination || []).map(item => `${item.name} ${money(item.amountIdr)}`).join(" · ") || "Belum ada"}</dd></div></dl></article>)}</div></section>}

        <section className="surface wealth-records"><div className="section-title"><div><span className="eyebrow">Riwayat</span><h2>Catatan posisi tersimpan</h2></div><span className="header-meta">{data.snapshots.length} catatan</span></div><div className="wealth-history">{data.snapshots.map(item => <button key={item.id} className="wealth-history-row" aria-label={`Buka rincian posisi ${dateTime(item.observedAt)} · ${money(item.netWorthIdr || "0")}`} onClick={() => openSnapshot(item.id)}><span>{dateTime(item.observedAt)}</span><strong>{money(item.netWorthIdr || "0")}</strong><small>Buka rincian akun</small></button>)}{!data.snapshots.length && <p className="empty compact">Belum ada catatan kekayaan.</p>}</div></section>
      </>}
    </div>

    {drawer.status !== "closed" && <div className="drawer-backdrop" onClick={closeSnapshot}><aside ref={drawerRef} tabIndex={-1} className="detail-drawer wealth-drawer" role="dialog" aria-modal="true" aria-labelledby="wealth-snapshot-title" onClick={event => event.stopPropagation()}><button className="drawer-close" aria-label="Tutup detail" onClick={closeSnapshot}>×</button><span className="eyebrow">Detail catatan posisi</span><h2 id="wealth-snapshot-title">{selected ? dateTime(selected.observedAt) : "Memuat catatan"}</h2><p className="muted">Waktu observasi tidak dapat diubah setelah catatan dibuat.</p>
      {drawer.status === "loading" ? <p role="status">Memuat detail…</p> : drawer.status === "error" ? <div role="alert"><p>{drawer.error}</p><button type="button" onClick={drawer.retry}>Coba lagi</button></div> : !correcting ? <><div className="wealth-drawer-summary"><div><span>Kekayaan bersih</span><strong>{money(selected.netWorthIdr)}</strong></div><div><span>Aset</span><strong className={amountTone(selected.assetTotalIdr)}>{money(selected.assetTotalIdr)}</strong></div><div><span>Kewajiban</span><strong className={amountTone(selected.liabilityTotalIdr, true)}>{money(selected.liabilityTotalIdr)}</strong></div></div><div className="wealth-drawer-items">{(selected.items || []).map(item => <div className="wealth-detail-row" key={item.id || item.wealthAccountId}><div><span>{item.name || item.accountName}</span><small>{[wealthTypeLabel[item.wealthType], usageRoleLabel[item.usageRole], snapshotSourceLabel[item.source] || item.source].filter(Boolean).join(" · ")}</small>{item.note && <small>{item.note}</small>}</div><strong>{money(item.valueIdr)}</strong></div>)}</div><button className="button secondary" onClick={() => setCorrecting(true)}>Koreksi catatan</button></> : <form className="wealth-correction-form" onSubmit={correctSnapshot}>{(selected.items || []).map(item => <fieldset className="wealth-correction-account" key={item.id || item.wealthAccountId}><legend>{item.name || item.accountName}</legend><label><span>Nilai</span><input name={`value-${item.wealthAccountId}`} inputMode="numeric" pattern="[0-9]+" required defaultValue={item.valueIdr}/></label><div className="wealth-correction-grid"><label><span>Jumlah unit</span><input name={`quantity-${item.wealthAccountId}`} inputMode="decimal" defaultValue={item.quantity || ""}/></label><label><span>Satuan</span><input name={`unit-${item.wealthAccountId}`} defaultValue={item.unit || ""}/></label><label><span>Harga per unit</span><input name={`unitPrice-${item.wealthAccountId}`} inputMode="numeric" defaultValue={item.unitPriceIdr || ""}/></label><label><span>Sumber</span><input name={`source-${item.wealthAccountId}`} defaultValue={item.source || "MANUAL"}/></label></div><label><span>Catatan</span><input name={`note-${item.wealthAccountId}`} autoCapitalize="sentences" defaultValue={item.note || ""}/></label></fieldset>)}<div className="wealth-drawer-actions"><button type="button" className="secondary" onClick={() => setCorrecting(false)}>Batal</button><button disabled={working}>{working ? "Menyimpan…" : "Simpan koreksi"}</button></div></form>}
    </aside></div>}
  </>;
}
