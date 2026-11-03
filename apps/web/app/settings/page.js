"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import useAuth from "../components/useAuth";
import { capabilityLabel, financialSourceStatusLabel, label, labels, relationshipLabel, sideLabel, usageRoleLabel, wealthTypeLabel } from "../lib/labels";
import useDialogs from "../components/useDialogs";
import { dateTime } from "../lib/format";

const capabilityCheckStyle = { display: "flex", minHeight: 42, alignItems: "center", gap: 8, color: "var(--ink-soft)", whiteSpace: "nowrap", cursor: "pointer" };
const capabilityInputStyle = { width: 24, height: 24, minHeight: 24, padding: 0, margin: 0, flex: "0 0 auto", accentColor: "var(--accent)", boxShadow: "none", cursor: "pointer" };

export default function SettingsPage() {
  const { confirm, prompt, dialogs } = useDialogs();
  const user = useAuth();
  const [data, setData] = useState({ accounts: [], wealthAccounts: [], categories: [], aliases: [], known: [], members: [], operations: null, salarySources: [], listeners: [], financialSources: [], emailIngress: null });
  const [error, setError] = useState(""); const [working, setWorking] = useState(""); const [financialPreview, setFinancialPreview] = useState(null); const [editingFinancialSource, setEditingFinancialSource] = useState(null);
  const owner = user?.household?.role === "OWNER";
  const load = useCallback(async () => {
    const endpoints = ["accounts", "wealth/accounts", "categories", "merchant-aliases", "known-accounts", "household/members", "operations/status", "salary/sources", "bank-email-listeners", "financial-email-sources", "integrations/email-ingress"];
    const responses = await Promise.all(endpoints.map(value => fetch(`/api/v1/${value}`)));
    const values = await Promise.all(responses.map(response => response.ok ? response.json() : null));
    setData({ accounts: values[0] || [], wealthAccounts: values[1] || [], categories: values[2] || [], aliases: values[3] || [], known: values[4] || [], members: values[5] || [], operations: values[6], salarySources: values[7] || [], listeners: values[8] || [], financialSources: values[9] || [], emailIngress: values[10] });
  }, []);
  useEffect(() => { if (user) load(); }, [user, load]);

  async function request(key, url, options) { setWorking(key); setError(""); const response = await fetch(url, options); if (!response.ok) { const body = await response.json().catch(() => ({})); setError(body.error || "Perubahan belum dapat disimpan."); } else await load(); setWorking(""); return response.ok; }
  async function createAccount(event) { event.preventDefault(); const form = new FormData(event.currentTarget); if (await request("account-new", "/api/v1/accounts", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: form.get("name"), accountType: form.get("accountType"), trackingPolicy: form.get("trackingPolicy") }) })) event.currentTarget.reset(); }
  async function createWealthAccount(event) { event.preventDefault(); const form = new FormData(event.currentTarget); const body = Object.fromEntries(form); body.linkedAccountId = body.linkedAccountId || null; body.ownerUserId = body.ownerUserId || null; if (await request("wealth-new", "/api/v1/wealth/accounts", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })) event.currentTarget.reset(); }
  async function createCategory(event) { event.preventDefault(); const form = new FormData(event.currentTarget); if (await request("category-new", "/api/v1/categories", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: form.get("name"), slug: form.get("slug"), parentId: form.get("parentId") || null }) })) event.currentTarget.reset(); }
  async function createKnown(event) { event.preventDefault(); const form = new FormData(event.currentTarget); if (await request("known-new", "/api/v1/known-accounts", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ institution: form.get("institution"), displayName: form.get("displayName"), matchHint: form.get("matchHint"), relationship: form.get("relationship"), userId: form.get("userId") || null, wealthAccountId: form.get("wealthAccountId") || null }) })) event.currentTarget.reset(); }
  function selectSalary(event) { request("salary-source", "/api/v1/salary/sources", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ sourceId: event.target.value }) }); }
  async function createListener(event) { event.preventDefault(); const form = new FormData(event.currentTarget); if (await request("listener-new", "/api/v1/bank-email-listeners", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ bankName: form.get("bankName"), senderAddress: form.get("senderAddress") }) })) event.currentTarget.reset(); }
  async function createFinancialSource(event) { event.preventDefault(); const form = new FormData(event.currentTarget); const capabilities = [form.get("cashMovement") && "CASH_MOVEMENT", form.get("wealthValue") && "WEALTH_VALUE"].filter(Boolean); if (!capabilities.length) { setError("Pilih minimal satu jenis deteksi."); return; } if (await request("financial-source-new", "/api/v1/financial-email-sources", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ providerName: form.get("providerName"), senderAddress: form.get("senderAddress"), capabilities, defaultWealthAccountId: form.get("defaultWealthAccountId") || null, status: "DRAFT" }) })) event.currentTarget.reset(); }
  function editFinancialSource(item) { setEditingFinancialSource(item); }
  async function saveFinancialSource(event) { event.preventDefault(); const form = new FormData(event.currentTarget); const capabilities = [form.get("cashMovement") && "CASH_MOVEMENT", form.get("wealthValue") && "WEALTH_VALUE"].filter(Boolean); if (!capabilities.length) { setError("Pilih minimal satu jenis deteksi."); return; } if (await patch(editingFinancialSource.id, `financial-email-sources/${editingFinancialSource.id}`, { providerName: form.get("providerName"), senderAddress: form.get("senderAddress"), capabilities, defaultWealthAccountId: form.get("defaultWealthAccountId") || null })) setEditingFinancialSource(null); }
  async function testFinancialSource(item) { const sample = await prompt("Uji email contoh", [{ name: "subject", label: "Subjek email contoh", defaultValue: "Konfirmasi transaksi", required: true }, { name: "body", label: "Isi email contoh", multiline: true, required: true }], { confirmLabel: "Jalankan uji" }); if (!sample || !sample.body.trim()) return; const { subject, body } = sample; setWorking(`test-${item.id}`); setFinancialPreview(null); const response = await fetch(`/api/v1/financial-email-sources/${item.id}/test`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ subject, body }) }); const created = await response.json().catch(() => ({})); if (!response.ok) { setError(created.error || "Preview gagal dimulai."); setWorking(""); return; } for (let attempt = 0; attempt < 30; attempt += 1) { await new Promise(resolve => setTimeout(resolve, 1000)); const current = await fetch(`/api/v1/financial-email-previews/${created.id}`).then(value => value.json()); if (current.status === "SUCCEEDED" || current.status === "FAILED") { setFinancialPreview(current); break; } } setWorking(""); }
  const emailIngressAction = (action = "") => request(`email-ingress-${action || "create"}`, `/api/v1/integrations/email-ingress${action ? `/${action}` : ""}`, { method: "POST" });
  async function copyEmailIngress() { try { await navigator.clipboard.writeText(data.emailIngress.address); } catch { setError("Alamat email tidak dapat disalin. Salin manual dari teks di atas."); } }
  const patch = (key, path, body) => request(key, `/api/v1/${path}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  async function rename(item, kind) { const result = await prompt(`Ganti nama ${kind}`, [{ name: "name", label: `Nama ${kind} baru`, defaultValue: item.name, required: true }]); const name = result?.name; if (name?.trim() && name.trim() !== item.name) patch(item.id, `${kind === "rekening" ? "accounts" : "categories"}/${item.id}`, { name: name.trim() }); }
  if (!user) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  return <AppShell user={user} eyebrow="PENGATURAN" title="Pengaturan keluarga" actions={<span className="header-meta">{owner ? "Akses pemilik" : "Hanya baca"}</span>}>
    {dialogs}
    {error && <p className="notice error">{error}</p>}
    <nav className="settings-index" aria-label="Bagian pengaturan">{[["rekening-manual", "Rekening"], ["akun-wealth", "Wealth"], ["sumber-slip-gaji-utama", "Gaji"], ["tujuan-transfer-yang-dikenal", "Transfer"], ["notifikasi-bank", "Email bank"], ["provider-finansial", "Provider"], ["kategori-keluarga", "Kategori"], ["aturan-kategori-otomatis", "Merchant"], ["email-telegram-dan-gateway", "Integrasi"], ["status-pemrosesan", "Sistem"]].map(([href, label]) => <a key={href} href={`#${href}`}>{label}</a>)}</nav>
    <div className="settings-sections">
      <SettingsSection title="Rekening manual" description="Kelola rekening yang tidak terhubung ke notifikasi email bank. Atur kebijakan pencatatannya untuk buku keluarga.">
        {owner && <form className="inline-form" onSubmit={createAccount}><input aria-label="Nama rekening" name="name" placeholder="Nama rekening" required/><select aria-label="Jenis rekening" name="accountType" defaultValue="BANK"><option value="BANK">Bank</option><option value="CASH">Tunai</option><option value="EWALLET">Dompet Digital</option><option value="OTHER">Lainnya</option></select><select aria-label="Kebijakan pencatatan" name="trackingPolicy" defaultValue="SPENDING_ONLY"><option value="SPENDING_ONLY">Pengeluaran Saja</option><option value="FULL_LEDGER">Buku Besar Lengkap</option><option value="REFERENCE_ONLY">Referensi Saja</option></select><button disabled={working === "account-new"}>Tambah</button></form>}
        <BoundedList items={data.accounts} renderItem={item => (<article key={item.id}><div><b>{item.name}</b><small>{label(wealthTypeLabel, item.accountType)}{item.systemManaged ? " · Dikelola email" : " · Manual"}</small></div><span>{({SPENDING_ONLY: "Pengeluaran Saja", FULL_LEDGER: "Buku Besar Lengkap", REFERENCE_ONLY: "Referensi Saja"})[item.trackingPolicy] || item.trackingPolicy} · Tidak dapat diubah</span>{owner && !item.systemManaged && <div className="row-actions"><button className="secondary" onClick={() => rename(item, "rekening")}>Ubah nama</button><button className={item.active ? "danger" : "secondary"} onClick={() => patch(item.id, `accounts/${item.id}`, { active: !item.active })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button></div>}</article>)} emptyMessage="Belum ada rekening. Tambahkan rekening manual atau hubungkan melalui Email Bank." emptyClassName="empty compact" />
      </SettingsSection>
      <SettingsSection title="Akun kekayaan" description="Daftarkan aset dan kewajiban untuk snapshot kekayaan bersih."><div id="wealth-accounts"/>{owner && <form className="inline-form known-form" onSubmit={createWealthAccount}><input aria-label="Nama akun" name="name" placeholder="Nama akun" required/><input aria-label="Institusi (opsional)" name="institution" placeholder="Institusi (opsional)"/><select aria-label="Aset atau kewajiban" name="side" defaultValue="ASSET"><option value="ASSET">Aset</option><option value="LIABILITY">Kewajiban</option></select><select aria-label="Jenis akun kekayaan" name="wealthType" defaultValue="BANK"><option value="BANK">Bank</option><option value="CASH">Tunai</option><option value="EWALLET">Dompet digital</option><option value="MUTUAL_FUND">Reksa dana</option><option value="GOLD">Emas</option><option value="BROKERAGE">Brokerage</option><option value="DEPOSIT">Deposito</option><option value="CRYPTO">Kripto</option><option value="LOAN">Pinjaman</option><option value="OTHER">Lainnya</option></select><select aria-label="Peran akun" name="usageRole" defaultValue="SAVINGS"><option value="TRANSACTIONAL">Transaksional</option><option value="SAVINGS">Tabungan</option><option value="INVESTMENT">Investasi</option><option value="OTHER">Lainnya</option></select><select aria-label="Pemilik" name="ownerUserId" defaultValue=""><option value="">Bersama / keluarga</option>{data.members.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.displayName}</option>)}</select><select aria-label="Rekening sumber" name="linkedAccountId" defaultValue=""><option value="">Tidak ditautkan</option>{data.accounts.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><button disabled={working === "wealth-new"}>Tambah</button></form>}<BoundedList items={data.wealthAccounts} renderItem={item => (<article key={item.id}><div><b>{item.name}</b><small>{item.institution || "Tanpa institusi"} · {label(wealthTypeLabel, item.wealthType)} · {item.ownerUserId ? "Pemilik khusus" : "Bersama"}</small></div><span>{label(sideLabel, item.side)} · {label(usageRoleLabel, item.usageRole)}</span>{owner && <><select aria-label={`Pemilik ${item.name}`} value={item.ownerUserId || ""} onChange={event => patch(item.id, `wealth/accounts/${item.id}`, { ownerUserId: event.target.value || null })}><option value="">Bersama / keluarga</option>{data.members.filter(member => member.active).map(member => <option key={member.id} value={member.id}>{member.displayName}</option>)}</select><select aria-label={`Rekening sumber ${item.name}`} value={item.linkedAccountId || ""} onChange={event => patch(item.id, `wealth/accounts/${item.id}`, { linkedAccountId: event.target.value || null })}><option value="">Tidak ditautkan</option>{data.accounts.filter(account => account.active).map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select><button className={item.active ? "danger" : "secondary"} onClick={() => patch(item.id, `wealth/accounts/${item.id}`, { active: !item.active })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button></>}</article>)} emptyMessage="Belum ada akun kekayaan." emptyClassName="empty compact" /></SettingsSection>
      <SettingsSection title="Sumber Slip Gaji Utama" description="Sumber utama menentukan batas Siklus Gaji. Pilih hanya sumber yang benar-benar menjadi pemasukan utama rumah tangga."><select aria-label="Sumber Slip Gaji Utama" value={data.salarySources.find(item => item.isPrimary)?.id || ""} onChange={selectSalary} disabled={!owner || working === "salary-source"}><option value="" disabled>Belum Dipilih</option>{data.salarySources.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.employer}</option>)}</select>{data.salarySources.length > 0 && <BoundedList items={data.salarySources} renderItem={item => (<article key={item.id}><div><b>{item.employer}</b><small>{item.active ? "Aktif" : "Nonaktif"}</small></div><span>{item.isPrimary ? "Utama" : "Cadangan"}</span></article>)} />}{!data.salarySources.length && <p className="muted">Belum ada sumber gaji. Konfirmasi slip gaji terlebih dahulu.</p>}</SettingsSection>
      <SettingsSection title="Tujuan transfer yang dikenal" description="Petunjuk tersamar membantu membedakan transfer sendiri, keluarga, dan investasi.">
        {owner && <form className="inline-form known-form" onSubmit={createKnown}><input aria-label="Institusi, mis. BCA" name="institution" placeholder="Institusi, mis. BCA" required/><input aria-label="Nama tampilan" name="displayName" placeholder="Nama tampilan" required/><input aria-label="4+ digit terakhir" name="matchHint" placeholder="4+ digit terakhir" minLength="4" required/><select aria-label="Hubungan" name="relationship" defaultValue="OWN_ACCOUNT"><option value="OWN_ACCOUNT">Rekening sendiri</option><option value="HOUSEHOLD_ACCOUNT">Rekening keluarga</option><option value="INVESTMENT_ACCOUNT">Investasi / RDN</option><option value="OTHER">Lainnya</option></select><select aria-label="Akun kekayaan" name="wealthAccountId" defaultValue=""><option value="">Tanpa akun kekayaan</option>{data.wealthAccounts.filter(item => item.active).map(item => <option value={item.id} key={item.id}>{item.name}</option>)}</select><select aria-label="Anggota" name="userId" defaultValue=""><option value="">Tanpa pemilik khusus</option>{data.members.filter(item => item.active).map(item => <option value={item.id} key={item.id}>{item.displayName}</option>)}</select><button disabled={working === "known-new"}>Tambah</button></form>}
        <BoundedList items={data.known} renderItem={item => (<article key={item.id}><div><b>{item.displayName}</b><small>{item.institution} · ••••{item.matchHint}</small></div><span>{label(relationshipLabel, item.relationship)}</span>{owner && <><select aria-label={`Akun kekayaan ${item.displayName}`} value={item.wealthAccountId || ""} onChange={event => patch(item.id, `known-accounts/${item.id}`, { wealthAccountId: event.target.value || null })}><option value="">Tanpa akun kekayaan</option>{data.wealthAccounts.filter(account => account.active).map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select><button className={item.active ? "danger" : "secondary"} onClick={() => patch(item.id, `known-accounts/${item.id}`, { active: !item.active })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button></>}</article>)} emptyMessage="Belum ada tujuan transfer yang dikenal." />
      </SettingsSection>
      <SettingsSection title="Notifikasi bank" description="Tambahkan pengirim notifikasi yang dipercaya. Kebijakan semua sumber: Pengeluaran Saja.">
        {owner && <form className="inline-form" onSubmit={createListener}><input aria-label="Nama bank" name="bankName" placeholder="Nama bank" required/><input aria-label="Alamat pengirim" name="senderAddress" type="email" placeholder="Alamat pengirim" required/><button disabled={working === "listener-new"}>Tambah</button></form>}
        <BoundedList items={data.listeners} renderItem={item => (<article key={item.id}><div><b>{item.bankName}</b><small>{item.senderAddress}</small></div><span>{item.active ? "Aktif" : "Nonaktif"} · Pengeluaran Saja</span>{owner && <button className={item.active ? "danger" : "secondary"} onClick={() => patch(item.id, `bank-email-listeners/${item.id}`, { active: !item.active })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button>}</article>)} emptyMessage="Belum ada notifikasi bank yang dipercaya." />
      </SettingsSection>
      <SettingsSection title="Provider finansial" description="Sumber baru dimulai sebagai draft. Tidak membuat rekening Bank atau mengubah transaksi sampai diaktifkan.">
        {owner && <form className="inline-form" onSubmit={createFinancialSource}><input aria-label="Nama provider" name="providerName" placeholder="Nama provider" required/><input aria-label="Alamat pengirim" name="senderAddress" type="email" placeholder="Alamat pengirim" required/><label className="check" style={capabilityCheckStyle}><input name="cashMovement" type="checkbox" defaultChecked style={capabilityInputStyle}/> Pergerakan dana</label><label className="check" style={capabilityCheckStyle}><input name="wealthValue" type="checkbox" defaultChecked style={capabilityInputStyle}/> Nilai aset / portfolio</label><select aria-label="Akun kekayaan bawaan" name="defaultWealthAccountId" defaultValue=""><option value="">Wealth default (opsional)</option>{data.wealthAccounts.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><button disabled={working === "financial-source-new"}>Tambah sebagai draft</button></form>}
        {owner && editingFinancialSource && <form className="inline-form" onSubmit={saveFinancialSource}><input aria-label="Nama provider" name="providerName" defaultValue={editingFinancialSource.providerName} placeholder="Nama provider" required/><input aria-label="Alamat pengirim" name="senderAddress" type="email" defaultValue={editingFinancialSource.senderAddress} placeholder="Alamat pengirim" required/><label className="check" style={capabilityCheckStyle}><input name="cashMovement" type="checkbox" defaultChecked={editingFinancialSource.capabilities.includes("CASH_MOVEMENT")} style={capabilityInputStyle}/> Pergerakan dana</label><label className="check" style={capabilityCheckStyle}><input name="wealthValue" type="checkbox" defaultChecked={editingFinancialSource.capabilities.includes("WEALTH_VALUE")} style={capabilityInputStyle}/> Nilai aset / portfolio</label><select aria-label="Akun kekayaan bawaan" name="defaultWealthAccountId" defaultValue={editingFinancialSource.defaultWealthAccountId || ""}><option value="">Tanpa Wealth default</option>{data.wealthAccounts.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><button disabled={working === editingFinancialSource.id}>Simpan</button><button type="button" className="secondary" onClick={() => setEditingFinancialSource(null)}>Batal</button></form>}
        <BoundedList items={data.financialSources} renderItem={item => (<article key={item.id}><div><b>{item.providerName}</b><small>{item.senderAddress}</small></div><span>{label(financialSourceStatusLabel, item.status)} · {labels(capabilityLabel, item.capabilities)}{item.defaultWealthAccountId ? " · akun Wealth bawaan terpasang" : " · ⚠ Perlu konfigurasi Wealth"}</span>{owner && item.status !== "ACTIVE" && <button className="secondary" onClick={() => editFinancialSource(item)}>Edit</button>}{owner && <button className="secondary" disabled={working === `test-${item.id}`} onClick={() => testFinancialSource(item)}>Test</button>}{owner && item.status !== "ACTIVE" && <button onClick={() => patch(item.id, `financial-email-sources/${item.id}`, { status: "ACTIVE" })}>Aktifkan</button>}{owner && item.status === "ACTIVE" && <button className="danger" onClick={() => patch(item.id, `financial-email-sources/${item.id}`, { status: "DISABLED" })}>Nonaktifkan</button>}</article>)} emptyMessage="Belum ada sumber email finansial." emptyClassName="empty compact" />
        {financialPreview && <pre className="notice">Preview tanpa mutasi canonical: {JSON.stringify(financialPreview.result || financialPreview.error, null, 2)}</pre>}
      </SettingsSection>
      <SettingsSection title="Kategori keluarga" description="Kategori nonaktif tetap tersimpan pada riwayat transaksi.">
        {owner && <form className="inline-form" onSubmit={createCategory}><input aria-label="Nama kategori" name="name" placeholder="Nama kategori" required/><input aria-label="Slug kategori" name="slug" placeholder="slug-kategori" pattern="[a-z0-9]+(?:-[a-z0-9]+)*" required/><select aria-label="Kategori induk" name="parentId" defaultValue=""><option value="">Kategori utama</option>{data.categories.filter(item => !item.parentId && item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><button disabled={working === "category-new"}>Tambah</button></form>}
        <BoundedList items={data.categories} renderItem={item => (<article key={item.id}><div><b>{item.parentId ? "↳ " : ""}{item.name}</b><small>{item.slug}</small></div><span>{item.active ? "Aktif" : "Nonaktif"}</span>{owner && <div className="row-actions"><button className="secondary" onClick={() => rename(item, "kategori")}>Ubah nama</button><button className={item.active ? "danger" : "secondary"} onClick={() => patch(item.id, `categories/${item.id}`, { active: !item.active })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button></div>}</article>)} emptyMessage="Belum ada kategori keluarga." />
      </SettingsSection>
      <SettingsSection title="Aturan kategori otomatis" description="Hanya aturan yang dikonfirmasi secara eksplisit yang dapat aktif.">
        <BoundedList items={data.aliases} renderItem={item => (<article key={item.id}><div><b>{item.normalizedName}</b><small>Alias: {item.rawName}</small></div><span>{item.defaultCategoryName || "Tanpa kategori"}</span>{owner && <button className={item.autoApply ? "danger" : "secondary"} onClick={() => patch(item.id, `merchant-aliases/${item.id}`, { autoApply: !item.autoApply })}>{item.autoApply ? "Nonaktifkan" : "Aktifkan"}</button>}</article>)} emptyMessage="Belum ada aturan merchant." emptyClassName="empty compact" />
      </SettingsSection>
      <SettingsSection title="Email, Telegram, dan gateway" description="Alamat email dibuat Richmod dan aktif hanya setelah pemilik melakukan cutover.">
        <div className="integration-grid"><article><span className={`health-dot ${data.emailIngress?.status === "ACTIVE" ? "ok" : ""}`}/><div><b>Email notifikasi bank</b><small>{data.emailIngress?.address || "Alamat belum dibuat"}{data.emailIngress?.status ? ` · ${data.emailIngress.status}` : ""}{data.emailIngress?.lastReceivedAt ? ` · diterima ${dateTime(data.emailIngress.lastReceivedAt)}` : ""}</small></div><div className="integration-actions">{owner && !data.emailIngress?.address && <button className="secondary" disabled={working === "email-ingress-create"} onClick={() => emailIngressAction()}>Buat alamat</button>}{data.emailIngress?.address && <button className="secondary" onClick={copyEmailIngress}>Salin</button>}{owner && data.emailIngress?.status === "PROVISIONED" && <button disabled={working === "email-ingress-activate"} onClick={() => emailIngressAction("activate")}>Aktifkan</button>}{owner && data.emailIngress?.address && <button className="secondary" disabled={working === "email-ingress-rotate"} onClick={async () => { if (await confirm("Alamat lama akan dinonaktifkan. Lanjutkan?", { confirmLabel: "Rotasi alamat", danger: true })) emailIngressAction("rotate"); }}>Rotasi</button>}</div></article><article><span className={`health-dot ${data.members.some(item => item.telegramConnected) ? "ok" : ""}`}/><div><b>Telegram bot</b><small>{data.members.filter(item => item.telegramConnected).length} anggota terhubung</small></div><a className="button secondary" href="/household">Kelola</a></article><article><span className={`health-dot ${data.operations?.worker?.healthy && data.operations?.llmGateway?.configured ? "ok" : ""}`}/><div><b>Worker & LLM gateway</b><small>Worker {data.operations?.worker?.healthy ? "sehat" : "perlu perhatian"} · gateway {data.operations?.llmGateway?.configured ? "terkonfigurasi" : "tidak dikonfigurasi"}</small></div></article></div>
      </SettingsSection>
      <NotificationsSection />
    </div>
  </AppShell>;
}

// The browser owns the truth: the card is a view of Notification.permission
// and PushManager.getSubscription(), never of what the app last did.
export function notificationState({ supported, ios, installed, serverEnabled, permission, subscribed }) {
  if (!supported || !serverEnabled || (ios && !installed)) return "unsupported";
  if (permission === "denied") return "denied";
  if (permission === "granted") return subscribed ? "subscribed" : "granted";
  return "not-asked";
}

function base64Key(value) {
  const padded = value.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(value.length / 4) * 4, "=");
  return Uint8Array.from(atob(padded), character => character.charCodeAt(0));
}

// Unsubscribing locally and deleting the server row are one action; either
// alone leaves a subscription the sender keeps trying to reach.
export async function disableNotifications(serviceWorker, request = fetch) {
  const registration = await serviceWorker.getRegistration();
  const subscription = await registration?.pushManager?.getSubscription();
  if (!subscription) return "missing";
  const response = await request("/api/v1/push/subscriptions", {
    method: "DELETE",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ endpoint: subscription.endpoint }),
  });
  await subscription.unsubscribe();
  return response.ok ? "off" : "unconfirmed";
}

const PUSH_COPY = {
  unsupported: ["Tidak didukung di perangkat ini", "Peramban ini belum mendukung notifikasi push."],
  "not-asked": ["Belum diaktifkan", "Aktifkan untuk menerima pemberitahuan saat ada tinjauan atau tindakan yang menunggu."],
  denied: ["Diblokir", "Notifikasi diblokir untuk situs ini. Buka pengaturan situs di peramban, izinkan notifikasi, lalu muat ulang halaman ini. Richmod tidak dapat meminta izin lagi."],
  granted: ["Belum terdaftar", "Izin notifikasi sudah diberikan, tetapi perangkat ini belum terdaftar. Coba daftarkan ulang."],
  subscribed: ["Aktif", "Notifikasi aktif di perangkat ini. Pemberitahuan muncul saat ada tinjauan atau tindakan baru."],
};

export function NotificationsView({ state, iosNotInstalled = false, serverDisabled = false, working = false, status = "", onEnable, onDisable }) {
  const [fallbackTitle, fallbackBody] = PUSH_COPY[state] || ["Memeriksa status notifikasi", "Richmod memeriksa dukungan notifikasi di peramban ini."];
  const title = state === "unsupported" && iosNotInstalled ? "Perlu dipasang ke Layar Utama" : fallbackTitle;
  const body = state === "unsupported" && iosNotInstalled
    ? "Safari di iPhone dan iPad hanya menerima notifikasi setelah Richmod ditambahkan ke Layar Utama. Buka menu Bagikan lalu pilih Tambahkan ke Layar Utama."
    : state === "unsupported" && serverDisabled
      ? "Server ini belum mengaktifkan notifikasi push, jadi perangkat belum dapat mendaftar."
      : fallbackBody;
  return <>
    <div className="notification-state">
      <div className="notification-copy"><b>{title}</b><p>{body}</p></div>
      {(state === "not-asked" || state === "granted") && <button type="button" disabled={working} onClick={onEnable}>{working ? "Memproses…" : state === "granted" ? "Coba lagi" : "Aktifkan notifikasi"}</button>}
      {state === "subscribed" && <button type="button" className="secondary" disabled={working} onClick={onDisable}>{working ? "Mematikan…" : "Matikan notifikasi"}</button>}
    </div>
    <p className="notification-status" role="status" aria-live="polite">{status}</p>
  </>;
}

function NotificationsSection() {
  const [view, setView] = useState({ state: "checking", iosNotInstalled: false, serverDisabled: false, publicKey: "" });
  const [working, setWorking] = useState(false);
  const [status, setStatus] = useState("");

  const refresh = useCallback(async () => {
    const supported = typeof window !== "undefined" && "PushManager" in window && "serviceWorker" in navigator && "Notification" in window;
    const ios = typeof navigator !== "undefined" && (/iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1));
    const installed = typeof navigator !== "undefined" && (navigator.standalone === true || window.matchMedia?.("(display-mode: standalone)")?.matches === true);
    let server = null;
    try {
      const response = await fetch("/api/v1/push/public-key");
      if (response.ok) server = await response.json();
    } catch {
      // A server that cannot answer is treated as not configured.
    }
    let subscribed = false;
    if (supported) {
      try {
        const registration = await navigator.serviceWorker.getRegistration();
        subscribed = Boolean(await registration?.pushManager.getSubscription());
      } catch {
        // No registration means no subscription; the state below says so.
      }
    }
    const serverEnabled = Boolean(server?.enabled);
    const state = notificationState({ supported, ios, installed, serverEnabled, permission: supported ? Notification.permission : "default", subscribed });
    setView({ state, iosNotInstalled: ios && !installed, serverDisabled: supported && (!ios || installed) && !serverEnabled, publicKey: server?.publicKey || server?.public_key || "" });
  }, []);

  useEffect(() => {
    refresh();
    const onVisible = () => { if (!document.hidden) refresh(); };
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [refresh]);

  async function enable() {
    if (working) return;
    setWorking(true); setStatus("");
    try {
      if (!("PushManager" in window)) { setStatus("Peramban ini belum mendukung notifikasi."); return; }
      if (Notification.permission === "denied") { setStatus("Notifikasi diblokir untuk situs ini dan hanya dapat diizinkan dari pengaturan peramban."); return; }
      if (Notification.permission === "default" && await Notification.requestPermission() !== "granted") {
        setStatus(Notification.permission === "denied" ? "Izin notifikasi ditolak dan hanya dapat diubah dari pengaturan peramban." : "Izin notifikasi belum diberikan.");
        return;
      }
      if (!view.publicKey) { setStatus("Server ini belum mengaktifkan notifikasi push."); return; }
      const registration = await navigator.serviceWorker.getRegistration() || await navigator.serviceWorker.register("/sw.js", { scope: "/" });
      let subscription = await registration.pushManager.getSubscription();
      if (!subscription) subscription = await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: base64Key(view.publicKey) });
      const response = await fetch("/api/v1/push/subscriptions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ endpoint: subscription.endpoint, keys: subscription.toJSON().keys }),
      });
      if (!response.ok) {
        await subscription.unsubscribe().catch(() => {});
        setStatus("Perangkat ini belum dapat didaftarkan. Coba lagi.");
        return;
      }
      setStatus("Notifikasi aktif di perangkat ini.");
    } catch {
      setStatus("Notifikasi belum dapat diaktifkan. Coba lagi.");
    } finally {
      setWorking(false);
      await refresh();
    }
  }

  async function disable() {
    if (working) return;
    setWorking(true); setStatus("");
    try {
      const result = await disableNotifications(navigator.serviceWorker);
      setStatus(result === "missing" ? "Perangkat ini memang belum terdaftar." : result === "off" ? "Notifikasi dimatikan di perangkat ini." : "Notifikasi dimatikan di perangkat ini, tetapi server belum mengonfirmasi. Coba lagi nanti.");
    } catch {
      setStatus("Notifikasi belum dapat dimatikan. Coba lagi.");
    } finally {
      setWorking(false);
      await refresh();
    }
  }

  return <SettingsSection title="Notifikasi" description="Notifikasi dikirim ke perangkat ini saat ada transaksi atau tindakan integrasi yang menunggu tinjauan.">
    <NotificationsView state={view.state} iosNotInstalled={view.iosNotInstalled} serverDisabled={view.serverDisabled} working={working} status={status} onEnable={enable} onDisable={disable} />
  </SettingsSection>;
}

function SettingsSection({ title, description, children }) {
  const id = title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
  return <section id={id} aria-labelledby={`${id}-title`} className="surface settings-section"><div className="settings-heading"><div><h2 id={`${id}-title`}>{title}</h2><p>{description}</p></div></div>{children}</section>;
}

function BoundedList({ items, limit = 8, renderItem, emptyMessage, emptyClassName = "empty compact" }) {
  const [page, setPage] = useState(0);
  const pageCount = Math.ceil(items.length / limit);
  const currentPage = Math.min(page, Math.max(0, pageCount - 1));
  const visible = items.slice(currentPage * limit, (currentPage + 1) * limit);
  return <div className="settings-list">
    {items.length > limit && <p className="list-count">{items.length} item</p>}
    {visible.map(renderItem)}
    {!items.length && emptyMessage && <p className={emptyClassName}>{emptyMessage}</p>}
    {pageCount > 1 && <div className="list-pagination"><button type="button" className="secondary" disabled={currentPage === 0} onClick={() => setPage(value => value - 1)}>Sebelumnya</button><span>Halaman {currentPage + 1} dari {pageCount}</span><button type="button" className="secondary" disabled={currentPage === pageCount - 1} onClick={() => setPage(value => value + 1)}>Berikutnya</button></div>}
  </div>;
}
