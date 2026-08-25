"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import useAuth from "../components/useAuth";
import { dateTime } from "../lib/format";

export default function SettingsPage() {
  const user = useAuth();
  const [data, setData] = useState({ accounts: [], categories: [], aliases: [], known: [], members: [], operations: null });
  const [error, setError] = useState(""); const [working, setWorking] = useState("");
  const owner = user?.memberships?.[0]?.role === "OWNER";
  const load = useCallback(async () => {
    const endpoints = ["accounts", "categories", "merchant-aliases", "known-accounts", "household/members", "operations/status"];
    const responses = await Promise.all(endpoints.map(value => fetch(`/api/v1/${value}`)));
    const values = await Promise.all(responses.map(response => response.ok ? response.json() : null));
    setData({ accounts: values[0] || [], categories: values[1] || [], aliases: values[2] || [], known: values[3] || [], members: values[4] || [], operations: values[5] });
  }, []);
  useEffect(() => { if (user) load(); }, [user, load]);

  async function request(key, url, options) { setWorking(key); setError(""); const response = await fetch(url, options); if (!response.ok) { const body = await response.json().catch(() => ({})); setError(body.error || "Perubahan belum dapat disimpan."); } else await load(); setWorking(""); return response.ok; }
  async function createAccount(event) { event.preventDefault(); const form = new FormData(event.currentTarget); if (await request("account-new", "/api/v1/accounts", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: form.get("name"), accountType: form.get("accountType"), trackingPolicy: form.get("trackingPolicy") }) })) event.currentTarget.reset(); }
  async function createCategory(event) { event.preventDefault(); const form = new FormData(event.currentTarget); if (await request("category-new", "/api/v1/categories", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: form.get("name"), slug: form.get("slug"), parentId: form.get("parentId") || null }) })) event.currentTarget.reset(); }
  async function createKnown(event) { event.preventDefault(); const form = new FormData(event.currentTarget); if (await request("known-new", "/api/v1/known-accounts", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ institution: form.get("institution"), displayName: form.get("displayName"), matchHint: form.get("matchHint"), relationship: form.get("relationship"), userId: form.get("userId") || null }) })) event.currentTarget.reset(); }
  const patch = (key, path, body) => request(key, `/api/v1/${path}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  function rename(item, kind) { const name = window.prompt(`Nama ${kind} baru`, item.name); if (name?.trim() && name.trim() !== item.name) patch(item.id, `${kind === "rekening" ? "accounts" : "categories"}/${item.id}`, { name: name.trim() }); }
  if (!user) return <main className="loading">Memuat…</main>;
  return <AppShell user={user} eyebrow="SETTINGS" title="Konfigurasi household" actions={<span className="header-meta">{owner ? "OWNER access" : "Read only"}</span>}>
    {error && <p className="notice error">{error}</p>}
    <div className="settings-sections">
      <SettingsSection eyebrow="ACCOUNTS" title="Rekening dan tracking policy" description="Jago tetap SPENDING_ONLY; rekening referensi tidak mengubah canonical ledger.">
        {owner && <form className="inline-form" onSubmit={createAccount}><input name="name" placeholder="Nama rekening" required/><select name="accountType" defaultValue="BANK"><option value="BANK">Bank</option><option value="CASH">Cash</option><option value="EWALLET">E-wallet</option><option value="OTHER">Lainnya</option></select><select name="trackingPolicy" defaultValue="SPENDING_ONLY"><option value="SPENDING_ONLY">Spending only</option><option value="FULL_LEDGER">Full ledger</option><option value="REFERENCE_ONLY">Reference only</option></select><button disabled={working === "account-new"}>Tambah</button></form>}
        <div className="settings-list">{data.accounts.map(item => { const jago = item.name.toLowerCase().includes("jago"); return <article key={item.id}><div><b>{item.name}</b><small>{item.accountType}</small></div>{owner ? <select value={item.trackingPolicy} disabled={jago} onChange={event => patch(item.id, `accounts/${item.id}`, { trackingPolicy: event.target.value })}><option value="SPENDING_ONLY">SPENDING_ONLY</option><option value="FULL_LEDGER">FULL_LEDGER</option><option value="REFERENCE_ONLY">REFERENCE_ONLY</option></select> : <span>{item.trackingPolicy}</span>}{owner && <div className="row-actions">{!jago && <button className="secondary" onClick={() => rename(item, "rekening")}>Ubah nama</button>}<button className={item.active ? "danger" : "secondary"} onClick={() => patch(item.id, `accounts/${item.id}`, { active: !item.active })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button></div>}</article>; })}</div>
      </SettingsSection>
      <SettingsSection eyebrow="KNOWN ACCOUNTS" title="Tujuan transfer yang dikenal" description="Masked hint membantu membedakan transfer sendiri, household, dan investasi.">
        {owner && <form className="inline-form known-form" onSubmit={createKnown}><input name="institution" placeholder="Institusi, mis. BCA" required/><input name="displayName" placeholder="Nama tampilan" required/><input name="matchHint" placeholder="4+ digit terakhir" minLength="4" required/><select name="relationship" defaultValue="OWN_ACCOUNT"><option value="OWN_ACCOUNT">Rekening sendiri</option><option value="HOUSEHOLD_ACCOUNT">Household</option><option value="INVESTMENT_ACCOUNT">Investasi / RDN</option><option value="OTHER">Lainnya</option></select><select name="userId" defaultValue=""><option value="">Tanpa pemilik khusus</option>{data.members.filter(item => item.active).map(item => <option value={item.id} key={item.id}>{item.displayName}</option>)}</select><button disabled={working === "known-new"}>Tambah</button></form>}
        <div className="settings-list">{data.known.map(item => <article key={item.id}><div><b>{item.displayName}</b><small>{item.institution} · ••••{item.matchHint}</small></div><span>{item.relationship}</span>{owner && <button className={item.active ? "danger" : "secondary"} onClick={() => patch(item.id, `known-accounts/${item.id}`, { active: !item.active })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button>}</article>)}</div>
      </SettingsSection>
      <SettingsSection eyebrow="CATEGORIES" title="Kategori household" description="Kategori nonaktif tetap tersimpan pada riwayat transaksi.">
        {owner && <form className="inline-form" onSubmit={createCategory}><input name="name" placeholder="Nama kategori" required/><input name="slug" placeholder="slug-kategori" pattern="[a-z0-9]+(?:-[a-z0-9]+)*" required/><select name="parentId" defaultValue=""><option value="">Kategori utama</option>{data.categories.filter(item => !item.parentId && item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><button disabled={working === "category-new"}>Tambah</button></form>}
        <div className="settings-list">{data.categories.map(item => <article key={item.id}><div><b>{item.parentId ? "↳ " : ""}{item.name}</b><small>{item.slug}</small></div><span>{item.active ? "Aktif" : "Nonaktif"}</span>{owner && <div className="row-actions"><button className="secondary" onClick={() => rename(item, "kategori")}>Ubah nama</button><button className={item.active ? "danger" : "secondary"} onClick={() => patch(item.id, `categories/${item.id}`, { active: !item.active })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button></div>}</article>)}</div>
      </SettingsSection>
      <SettingsSection eyebrow="MERCHANTS" title="Aturan kategori otomatis" description="Hanya aturan yang dikonfirmasi eksplisit dapat aktif.">
        <div className="settings-list">{data.aliases.map(item => <article key={item.id}><div><b>{item.normalizedName}</b><small>Alias: {item.rawName}</small></div><span>{item.defaultCategoryName || "Tanpa kategori"}</span>{owner && <button className={item.autoApply ? "danger" : "secondary"} onClick={() => patch(item.id, `merchant-aliases/${item.id}`, { autoApply: !item.autoApply })}>{item.autoApply ? "Nonaktifkan" : "Aktifkan"}</button>}</article>)}{!data.aliases.length && <p className="empty compact">Belum ada aturan merchant.</p>}</div>
      </SettingsSection>
      <SettingsSection eyebrow="INTEGRATIONS" title="Gmail, Telegram, dan gateway" description="Status operasional tidak mengubah financial state.">
        <div className="integration-grid"><article><span className={`health-dot ${data.operations?.gmail?.status === "CONNECTED" ? "ok" : ""}`}/><div><b>Gmail / Bank Jago</b><small>{data.operations?.gmail?.status || "Belum terhubung"}{data.operations?.gmail?.updatedAt ? ` · ${dateTime(data.operations.gmail.updatedAt)}` : ""}</small></div>{owner && <a className="button secondary" href="/api/v1/integrations/gmail/connect">Hubungkan</a>}</article><article><span className={`health-dot ${data.members.some(item => item.telegramConnected) ? "ok" : ""}`}/><div><b>Telegram bot</b><small>{data.members.filter(item => item.telegramConnected).length} anggota terhubung</small></div><a className="button secondary" href="/household">Kelola</a></article><article><span className={`health-dot ${data.operations?.worker?.healthy && data.operations?.llmGateway?.configured ? "ok" : ""}`}/><div><b>Worker & LLM gateway</b><small>Worker {data.operations?.worker?.healthy ? "sehat" : "perlu perhatian"} · gateway {data.operations?.llmGateway?.configured ? "terkonfigurasi" : "tidak dikonfigurasi"}</small></div></article></div>
      </SettingsSection>
      <SettingsSection eyebrow="SYSTEM" title="Status pemrosesan" description={`Diperiksa ${dateTime(data.operations?.checkedAt)}`}><div className="system-metrics"><article><span>Status</span><b>{data.operations?.status || "—"}</b></article><article><span>Job pending</span><b>{data.operations?.jobs?.pending ?? "—"}</b></article><article><span>Job gagal</span><b>{data.operations?.jobs?.failed ?? "—"}</b></article><article><span>Review backlog</span><b>{data.operations?.reviewBacklog ?? "—"}</b></article></div></SettingsSection>
    </div>
  </AppShell>;
}

function SettingsSection({ eyebrow, title, description, children }) { return <section className="surface settings-section"><div className="settings-heading"><div><span className="eyebrow">{eyebrow}</span><h2>{title}</h2><p>{description}</p></div></div>{children}</section>; }
