"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { capabilityLabel, financialSourceStatusLabel, label, labels, relationshipLabel, sideLabel, usageRoleLabel, wealthTypeLabel } from "../lib/labels";
import useDialogs from "./useDialogs";
import EmailDeliveries from "./EmailDeliveries";
import { publishInboxCount } from "./InboxCountProvider";
import { dateTime } from "../lib/format";
import { uniqueCategorySlug } from "../lib/slug";
import { filterItems } from "../lib/settingsTabs";
import { LoadError, responseError, useBusyMap, useGuardedLoad } from "../lib/useLoadState";
import { ErrorNotice, Skeleton, Toast } from "./Feedback";

const capabilityCheckStyle = { display: "flex", minHeight: 42, alignItems: "center", gap: 8, color: "var(--ink-soft)", whiteSpace: "nowrap", cursor: "pointer" };
const capabilityInputStyle = { width: 24, height: 24, minHeight: 24, padding: 0, margin: 0, flex: "0 0 auto", accentColor: "var(--accent)", boxShadow: "none", cursor: "pointer" };

// Server messages that deserve plain-language copy. Unknown messages pass through.
const SERVER_ERROR_TEXT = {
  "active sender already exists": "Alamat pengirim ini sudah dipakai oleh sumber aktif lain.",
  "unable to create category": "Kategori belum dapat dibuat. Nama ini mungkin sudah dipakai.",
  "parent category not found": "Kategori induk tidak ditemukan. Pilih ulang.",
};
const ACTIVATE_ERROR_TEXT = {
  ADDRESS_REQUIRED: "Buat alamat email terlebih dahulu.",
  SENDER_REQUIRED: "Tambahkan minimal satu pengirim bank atau provider yang aktif sebelum mengaktifkan email.",
};
const ROTATE_CONFIRM = "Alamat lama akan dinonaktifkan dan alamat baru dibuat. Aturan penerusan di Gmail atau penyedia email Anda harus dibuat ulang dengan alamat baru, lalu email perlu diaktifkan lagi. Lanjutkan?";
// Every fixed key a <RowError> renders. Row errors use the item id, and the provider edit
// form uses `edit-<id>`. A key outside this set is shown at the top instead of vanishing.
const SECTION_ERROR_KEYS = new Set(["account-new", "wealth-new", "known-new", "listener-new", "financial-source-new", "category-new", "salary-source", "email-ingress", "forwarding-resolve"]);

// The owner-facing settings sections of the Keluarga hub. The hub owns the page frame and the
// tab bar; this renders the sections of the active tab and loads its own data.
export default function SettingsPanels({ user, tab, anchor = "", onAnchorHandled }) {
  const { confirm, prompt, dialogs } = useDialogs();
  const [data, setData] = useState({ accounts: [], wealthAccounts: [], categories: [], aliases: [], known: [], members: [], operations: null, salarySources: [], listeners: [], financialSources: [], emailIngress: null });
  // A mutation error belongs to the form or row that failed: { key, field, message }.
  // The top notice is reserved for page-load failures.
  const [error, setError] = useState(null), [loadNotice, setLoadNotice] = useState(""); const { busy, run: runBusy, isBusy } = useBusyMap(); const [financialPreview, setFinancialPreview] = useState(null); const [editingFinancialSource, setEditingFinancialSource] = useState(null);
  const [toast, setToast] = useState(""), [highlight, setHighlight] = useState("");
  const closeToast = useCallback(() => setToast(""), []);
  useEffect(() => { if (!highlight) return undefined; const timer = window.setTimeout(() => setHighlight(""), 2000); return () => window.clearTimeout(timer); }, [highlight]);
  // The field the server rejects is known per form; anything else lands on the form itself.
  const fieldError = (key, field) => (error?.key === key && error.field === field ? { "aria-invalid": "true", "aria-describedby": `${key}-error` } : {});
  const owner = user?.household?.role === "OWNER";
  // A key no <RowError> renders still reaches the user, at the top, instead of vanishing.
  const rowIds = new Set([...data.accounts, ...data.wealthAccounts, ...data.known, ...data.listeners, ...data.financialSources, ...data.categories, ...data.aliases].map(item => item.id));
  const unrendered = error && !SECTION_ERROR_KEYS.has(error.key) && !rowIds.has(error.key) && !String(error.key).startsWith("edit-") ? error.message : "";
  // Each endpoint stands alone. A 403/404 means "not available to this role" (an empty
  // section); a 5xx, 401 or dropped connection means "unknown" and is reported.
  const loader = useGuardedLoad(async ({ signal, current }) => {
    const endpoints = ["accounts", "wealth/accounts", "categories", "merchant-aliases", "known-accounts", "household/members", "operations/status", "salary/sources", "bank-email-listeners", "financial-email-sources", "integrations/email-ingress"];
    const settled = await Promise.all(endpoints.map(async value => {
      try {
        const response = await fetch(`/api/v1/${value}`, { signal });
        if (response.ok) return { ok: true, value: await response.json() };
        return response.status === 403 || response.status === 404 ? { ok: true, value: null } : { ok: false };
      } catch (failure) {
        if (failure?.name === "AbortError") throw failure;
        return { ok: false };
      }
    }));
    if (!current()) return;
    const failed = endpoints.filter((_, index) => !settled[index].ok);
    if (failed.length === endpoints.length) throw new LoadError("Pengaturan belum dapat dimuat. Periksa koneksi lalu coba lagi.");
    const pick = (index, previous, fallback) => (settled[index].ok ? settled[index].value ?? fallback : previous);
    setData(previous => ({ accounts: pick(0, previous.accounts, []), wealthAccounts: pick(1, previous.wealthAccounts, []), categories: pick(2, previous.categories, []), aliases: pick(3, previous.aliases, []), known: pick(4, previous.known, []), members: pick(5, previous.members, []), operations: pick(6, previous.operations, null), salarySources: pick(7, previous.salarySources, []), listeners: pick(8, previous.listeners, []), financialSources: pick(9, previous.financialSources, []), emailIngress: pick(10, previous.emailIngress, null) }));
    setLoadNotice(failed.length ? `Sebagian pengaturan belum termuat (${failed.join(", ")}). Coba lagi.` : "");
  }, { fallback: "Pengaturan belum dapat dimuat. Periksa koneksi lalu coba lagi." });
  const load = loader.run;
  // Only the owner manages settings; a member sees notifications alone, so nothing is loaded for them.
  useEffect(() => { if (user && owner) load(); }, [user, owner, load]);
  // Scroll to an old #anchor once its tab has rendered, and keep the active tab
  // in view when the bar is wider than the screen.
  useEffect(() => {
    if (!anchor) return;
    const target = document.getElementById(anchor);
    if (target) { target.scrollIntoView({ block: "start" }); onAnchorHandled?.(); }
  }, [anchor, tab, loader.hasLoaded, onAnchorHandled]);

  // One busy flag per key; a failed or dropped request releases it in finally and says so.
  // `success` is the toast for a completed change, `highlight` the row id to flash, and
  // `field` the input the server rejected (aria-invalid + aria-describedby). The parsed
  // body is returned so a create can flash the row it just made.
  function request(key, url, options, { field, success, highlight: id, errorKey = key } = {}) {
    return runBusy(key, async () => {
      setError(null);
      try {
        const response = await fetch(url, options);
        if (!response.ok) { const message = await responseError(response, "Perubahan belum dapat disimpan."); setError({ key: errorKey, field, message: SERVER_ERROR_TEXT[message] || message }); return false; }
        const body = await response.json().catch(() => ({}));
        await load();
        if (success) setToast(success);
        if (id) setHighlight(id);
        return body || true;
      } catch {
        setError({ key: errorKey, field, message: "Koneksi terputus. Perubahan belum tersimpan." });
        return false;
      }
    });
  }
  // Deactivating is consequential, so it asks first and names the consequence; activating
  // never asks. Focus returns to the button that was used once it is enabled again.
  async function toggle(key, path, body, { active, message, success, highlight: id, button }) {
    if (active && !(await confirm(message, { confirmLabel: "Nonaktifkan", danger: true }))) return false;
    const done = await request(key, `/api/v1/${path}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }, { success, highlight: id });
    if (done && button?.isConnected) button.focus();
    return done;
  }
  async function createAccount(event) { event.preventDefault(); const formEl = event.currentTarget; const form = new FormData(formEl); const name = String(form.get("name") || ""); const created = await request("account-new", "/api/v1/accounts", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name, accountType: form.get("accountType"), trackingPolicy: form.get("trackingPolicy") }) }, { field: "name", success: `Rekening ${name} ditambahkan.` }); if (created) { formEl.reset(); formEl.querySelector("input")?.focus(); setHighlight(created.id); } }
  async function createWealthAccount(event) { event.preventDefault(); const formEl = event.currentTarget; const form = new FormData(formEl); const body = Object.fromEntries(form); body.linkedAccountId = body.linkedAccountId || null; body.ownerUserId = body.ownerUserId || null; const created = await request("wealth-new", "/api/v1/wealth/accounts", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }, { field: "name", success: `Akun kekayaan ${body.name} ditambahkan.` }); if (created) { formEl.reset(); formEl.querySelector("input")?.focus(); setHighlight(created.id); } }
  async function createCategory(event) { event.preventDefault(); const formEl = event.currentTarget; const form = new FormData(formEl); const name = String(form.get("name") || ""); const parentId = form.get("parentId") || null; const slug = uniqueCategorySlug(name, data.categories.filter(item => (item.parentId || null) === parentId).map(item => item.slug)); const created = await request("category-new", "/api/v1/categories", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name, slug, parentId }) }, { field: "name", success: `Kategori ${name} ditambahkan.` }); if (created) { formEl.reset(); formEl.querySelector("input")?.focus(); setHighlight(created.id); } }
  async function createKnown(event) { event.preventDefault(); const formEl = event.currentTarget; const form = new FormData(formEl); const displayName = String(form.get("displayName") || ""); const created = await request("known-new", "/api/v1/known-accounts", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ institution: form.get("institution"), displayName, matchHint: form.get("matchHint"), relationship: form.get("relationship"), userId: form.get("userId") || null, wealthAccountId: form.get("wealthAccountId") || null }) }, { field: "institution", success: `Tujuan transfer ${displayName} ditambahkan.` }); if (created) { formEl.reset(); formEl.querySelector("input")?.focus(); setHighlight(created.id); } }
  function selectSalary(event) { request("salary-source", "/api/v1/salary/sources", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ sourceId: event.target.value }) }, { success: "Sumber slip gaji utama disimpan." }); }
  async function createListener(event) { event.preventDefault(); const formEl = event.currentTarget; const form = new FormData(formEl); const bankName = String(form.get("bankName") || ""); const created = await request("listener-new", "/api/v1/bank-email-listeners", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ bankName, senderAddress: form.get("senderAddress") }) }, { field: "senderAddress", success: `Email dari ${bankName} akan dicatat.` }); if (created) { formEl.reset(); formEl.querySelector("input")?.focus(); setHighlight(created.id); } }
  async function createFinancialSource(event) { event.preventDefault(); const formEl = event.currentTarget; const form = new FormData(formEl); const capabilities = [form.get("cashMovement") && "CASH_MOVEMENT", form.get("wealthValue") && "WEALTH_VALUE"].filter(Boolean); if (!capabilities.length) { setError({ key: "financial-source-new", field: "cashMovement", message: "Pilih minimal satu jenis deteksi." }); return; } const providerName = String(form.get("providerName") || ""); const created = await request("financial-source-new", "/api/v1/financial-email-sources", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ providerName, senderAddress: form.get("senderAddress"), capabilities, defaultWealthAccountId: form.get("defaultWealthAccountId") || null, status: "DRAFT" }) }, { field: "providerName", success: `Provider ${providerName} ditambahkan dan belum aktif.` }); if (created) { formEl.reset(); formEl.querySelector("input")?.focus(); setHighlight(created.id); } }
  function editFinancialSource(item) { setEditingFinancialSource(item); }
  async function saveFinancialSource(event) { event.preventDefault(); const form = new FormData(event.currentTarget); const capabilities = [form.get("cashMovement") && "CASH_MOVEMENT", form.get("wealthValue") && "WEALTH_VALUE"].filter(Boolean); if (!capabilities.length) { setError({ key: `edit-${editingFinancialSource.id}`, field: "cashMovement", message: "Pilih minimal satu jenis deteksi." }); return; } if (await patch(`edit-${editingFinancialSource.id}`, `financial-email-sources/${editingFinancialSource.id}`, { providerName: form.get("providerName"), senderAddress: form.get("senderAddress"), capabilities, defaultWealthAccountId: form.get("defaultWealthAccountId") || null }, { field: "providerName", success: `Perubahan ${editingFinancialSource.providerName} disimpan.`, highlight: editingFinancialSource.id })) setEditingFinancialSource(null); }
  async function testFinancialSource(item) {
    const sample = await prompt("Uji email contoh", [{ name: "subject", label: "Subjek email contoh", defaultValue: "Konfirmasi transaksi", required: true }, { name: "body", label: "Isi email contoh", multiline: true, required: true }], { confirmLabel: "Jalankan uji" });
    if (!sample || !sample.body.trim()) return;
    const { subject, body } = sample;
    await runBusy(`test-${item.id}`, async () => {
      setError(null); setFinancialPreview(null);
      try {
        const response = await fetch(`/api/v1/financial-email-sources/${item.id}/test`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ subject, body }) });
        const created = await response.json().catch(() => ({}));
        if (!response.ok) { setError({ key: item.id, message: created.error || "Preview gagal dimulai." }); return; }
        for (let attempt = 0; attempt < 30; attempt += 1) {
          await new Promise(resolve => setTimeout(resolve, 1000));
          const poll = await fetch(`/api/v1/financial-email-previews/${created.id}`);
          const current = poll.ok ? await poll.json().catch(() => null) : null;
          if (current && (current.status === "SUCCEEDED" || current.status === "FAILED")) { setFinancialPreview(current); return; }
        }
        setError({ key: item.id, message: "Preview belum selesai dalam 30 detik. Coba jalankan uji lagi nanti." });
      } catch {
        setError({ key: item.id, message: "Koneksi terputus saat menjalankan uji. Coba lagi." });
      }
    });
  }
  const emailIngressAction = (action = "") => request(`email-ingress-${action || "create"}`, `/api/v1/integrations/email-ingress${action ? `/${action}` : ""}`, { method: "POST" }, { errorKey: "email-ingress", success: action === "rotate" ? "Alamat email baru dibuat. Aturan penerusan perlu dibuat ulang." : "Alamat email dibuat." });
  // Activation has prerequisites, so its 409s carry a code that maps to a next step.
  const activateEmailIngress = () => runBusy("email-ingress-activate", async () => {
    setError(null);
    try {
      const response = await fetch("/api/v1/integrations/email-ingress/activate", { method: "POST" });
      if (!response.ok) {
        const body = await response.json().catch(() => ({}));
        setError({ key: "email-ingress", message: ACTIVATE_ERROR_TEXT[body?.code] || body?.error || "Email belum dapat diaktifkan." });
        await load();
        return false;
      }
      await load();
      setToast(`Email ${data.emailIngress?.address || "bank"} diaktifkan.`);
      return true;
    } catch {
      setError({ key: "email-ingress", message: "Koneksi terputus. Email belum diaktifkan." });
      return false;
    }
  }).then(Boolean);
  async function copyEmailIngress() { try { await navigator.clipboard.writeText(data.emailIngress.address); } catch { setError({ key: "email-ingress", message: "Alamat email tidak dapat disalin. Salin manual dari teks di atas." }); } }
  const patch = (key, path, body, options) => request(key, `/api/v1/${path}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }, options);
  async function rename(item, kind) { const result = await prompt(`Ganti nama ${kind}`, [{ name: "name", label: `Nama ${kind} baru`, defaultValue: item.name, required: true }]); const name = result?.name?.trim(); if (name && name !== item.name) patch(item.id, `${kind === "rekening" ? "accounts" : "categories"}/${item.id}`, { name }, { field: "name", success: `Nama diubah menjadi ${name}.`, highlight: item.id }); }
  if (!user) return null;
  // A member has no settings to manage: only their own device notifications.
  if (!owner) return <NotificationsSection tab="notifikasi" active={tab} />;
  // Until one load has succeeded the lists are unknown, not empty.
  if (!loader.hasLoaded) return <>{dialogs}<ErrorNotice message={loader.error} retry={() => load()}/>{!loader.failed && <Skeleton cards={2} rows={4}/>}</>;
  return <>
    {dialogs}
    <Toast message={toast} onClose={closeToast}/>
    <ErrorNotice message={unrendered}/>
    <ErrorNotice message={loadNotice || loader.error} retry={() => load()}/>
    <p className="form-legend"><span className="req" aria-hidden="true">*</span> wajib diisi</p>
    <div className="settings-sections">
      <SettingsSection tab="akun" active={tab} title="Rekening manual" description="Rekening yang tidak terhubung ke email bank. Tambahkan di sini agar transaksi tunai atau rekening lama tetap tercatat.">
        {owner && <form className="inline-form" onSubmit={createAccount}><label><span>Nama rekening <span className="req" aria-hidden="true">*</span></span><input aria-label="Nama rekening" name="name" required {...fieldError("account-new", "name")}/></label><label><span>Jenis rekening</span><select aria-label="Jenis rekening" name="accountType" defaultValue="BANK"><option value="BANK">Bank</option><option value="CASH">Tunai</option><option value="EWALLET">Dompet Digital</option><option value="OTHER">Lainnya</option></select></label><label><span>Cara mencatat</span><select aria-label="Cara mencatat" name="trackingPolicy" defaultValue="SPENDING_ONLY" aria-describedby="account-tracking-hint"><option value="SPENDING_ONLY">Pengeluaran Saja</option><option value="FULL_LEDGER">Buku Besar Lengkap</option><option value="REFERENCE_ONLY">Referensi Saja</option></select><FieldHint id="account-tracking-hint">Pengeluaran Saja hanya mencatat uang keluar; Buku Besar Lengkap mencatat masuk dan keluar.</FieldHint></label><button disabled={isBusy("account-new")}>Tambah</button></form>}
        <RowError k="account-new" error={error}/>
        <BoundedList items={data.accounts} renderItem={item => (<article key={item.id} className={highlight === item.id ? "is-new" : undefined}><div><b>{item.name}</b><small>{label(wealthTypeLabel, item.accountType)}{item.systemManaged ? " · Dikelola email" : " · Manual"}</small></div><span>{({SPENDING_ONLY: "Pengeluaran Saja", FULL_LEDGER: "Buku Besar Lengkap", REFERENCE_ONLY: "Referensi Saja"})[item.trackingPolicy] || item.trackingPolicy} · Tidak dapat diubah</span>{owner && !item.systemManaged && <div className="row-actions"><button className="secondary" aria-label={`Ubah nama ${item.name}`} disabled={isBusy(item.id)} onClick={() => rename(item, "rekening")}>Ubah nama</button><button className={item.active ? "danger-quiet" : "secondary"} aria-label={`${item.active ? "Nonaktifkan" : "Aktifkan"} ${item.name}`} disabled={isBusy(item.id)} onClick={event => toggle(item.id, `accounts/${item.id}`, { active: !item.active }, { active: item.active, message: `Nonaktifkan ${item.name}? Rekening ini tidak lagi dipakai untuk transaksi baru sampai diaktifkan kembali.`, success: `${item.name} ${item.active ? "dinonaktifkan" : "diaktifkan"}.`, highlight: item.id, button: event.currentTarget })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button></div>}<RowError k={item.id} error={error}/></article>)} emptyMessage="Belum ada rekening. Tambahkan rekening manual atau hubungkan melalui Email Bank." emptyClassName="empty compact" />
      </SettingsSection>
      <SettingsSection tab="akun" active={tab} title="Akun kekayaan" description="Aset dan utang yang ingin Anda pantau nilainya. Isi sekali, lalu perbarui saldonya saat membuat snapshot kekayaan."><div id="wealth-accounts"/>{owner && <form className="inline-form known-form wealth-form" onSubmit={createWealthAccount}><label><span>Nama akun <span className="req" aria-hidden="true">*</span></span><input aria-label="Nama akun" name="name" required {...fieldError("wealth-new", "name")}/></label><label><span>Institusi (opsional)</span><input aria-label="Institusi (opsional)" name="institution"/></label><label><span>Aset atau kewajiban</span><select aria-label="Aset atau kewajiban" name="side" defaultValue="ASSET"><option value="ASSET">Aset</option><option value="LIABILITY">Kewajiban</option></select></label><label><span>Jenis akun kekayaan</span><select aria-label="Jenis akun kekayaan" name="wealthType" defaultValue="BANK"><option value="BANK">Bank</option><option value="CASH">Tunai</option><option value="EWALLET">Dompet digital</option><option value="MUTUAL_FUND">Reksa dana</option><option value="GOLD">Emas</option><option value="BROKERAGE">Brokerage</option><option value="DEPOSIT">Deposito</option><option value="CRYPTO">Kripto</option><option value="LOAN">Pinjaman</option><option value="OTHER">Lainnya</option></select></label><label><span>Peran akun</span><select aria-label="Peran akun" name="usageRole" defaultValue="SAVINGS" aria-describedby="wealth-role-hint"><option value="TRANSACTIONAL">Transaksional</option><option value="SAVINGS">Tabungan</option><option value="INVESTMENT">Investasi</option><option value="OTHER">Lainnya</option></select><FieldHint id="wealth-role-hint">Menandai akun ini untuk transaksi harian, tabungan, atau investasi.</FieldHint></label><label><span>Pemilik</span><select aria-label="Pemilik" name="ownerUserId" defaultValue=""><option value="">Bersama</option>{data.members.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.displayName}</option>)}</select></label><label><span>Rekening sumber</span><select aria-label="Rekening sumber" name="linkedAccountId" defaultValue="" aria-describedby="wealth-source-hint"><option value="">Tidak ditautkan</option>{data.accounts.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><FieldHint id="wealth-source-hint">Rekening harian tempat dana akun ini disimpan.</FieldHint></label><button disabled={isBusy("wealth-new")}>Tambah</button></form>}<RowError k="wealth-new" error={error}/><BoundedList items={data.wealthAccounts} renderItem={item => (<article key={item.id} className={highlight === item.id ? "is-new" : undefined}><div><b>{item.name}</b><small>{item.institution || "Tanpa institusi"} · {label(wealthTypeLabel, item.wealthType)} · {item.ownerUserId ? "Pemilik khusus" : "Bersama"}</small></div><span>{label(sideLabel, item.side)} · {label(usageRoleLabel, item.usageRole)}</span>{owner && <div className="row-actions"><select aria-label={`Pemilik ${item.name}`} value={item.ownerUserId || ""} disabled={isBusy(item.id)} onChange={event => patch(item.id, `wealth/accounts/${item.id}`, { ownerUserId: event.target.value || null })}><option value="">Bersama</option>{data.members.filter(member => member.active).map(member => <option key={member.id} value={member.id}>{member.displayName}</option>)}</select><select aria-label={`Rekening sumber ${item.name}`} value={item.linkedAccountId || ""} disabled={isBusy(item.id)} onChange={event => patch(item.id, `wealth/accounts/${item.id}`, { linkedAccountId: event.target.value || null })}><option value="">Tidak ditautkan</option>{data.accounts.filter(account => account.active).map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select><button className={item.active ? "danger-quiet" : "secondary"} aria-label={`${item.active ? "Nonaktifkan" : "Aktifkan"} ${item.name}`} disabled={isBusy(item.id)} onClick={event => toggle(item.id, `wealth/accounts/${item.id}`, { active: !item.active }, { active: item.active, message: `Nonaktifkan ${item.name}? Akun ini tidak lagi ikut dihitung di snapshot kekayaan sampai diaktifkan kembali.`, success: `${item.name} ${item.active ? "dinonaktifkan" : "diaktifkan"}.`, highlight: item.id, button: event.currentTarget })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button></div>}<RowError k={item.id} error={error}/></article>)} emptyMessage="Belum ada akun kekayaan." emptyClassName="empty compact" /></SettingsSection>
      <SettingsSection tab="akun" active={tab} title="Sumber Slip Gaji Utama" description="Pilih sumber pemasukan utama rumah tangga. Pilihan ini menentukan awal dan akhir Siklus Gaji."><select aria-label="Sumber Slip Gaji Utama" value={data.salarySources.find(item => item.isPrimary)?.id || ""} onChange={selectSalary} disabled={!owner || isBusy("salary-source")}><option value="" disabled>Belum Dipilih</option>{data.salarySources.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.employer}</option>)}</select><RowError k="salary-source" error={error}/>{data.salarySources.length > 0 && <BoundedList items={data.salarySources} renderItem={item => (<article key={item.id}><div><b>{item.employer}</b><small>{item.active ? "Aktif" : "Nonaktif"}</small></div><span>{item.isPrimary ? "Utama" : "Cadangan"}</span></article>)} />}{!data.salarySources.length && <p className="muted">Belum ada sumber gaji. Konfirmasi slip gaji terlebih dahulu.</p>}</SettingsSection>
      <SettingsSection tab="akun" active={tab} title="Tujuan transfer yang dikenal" description="Rekening tujuan yang sering Anda transfer. Richmod memakai 4+ digit terakhir nomor rekening untuk mengenali transfer sendiri, keluarga, atau investasi.">
        {owner && <form className="inline-form known-form" onSubmit={createKnown}><label><span>Institusi, mis. BCA <span className="req" aria-hidden="true">*</span></span><input aria-label="Institusi, mis. BCA" name="institution" required {...fieldError("known-new", "institution")}/></label><label><span>Nama tampilan <span className="req" aria-hidden="true">*</span></span><input aria-label="Nama tampilan" name="displayName" required/></label><label><span>4+ digit terakhir <span className="req" aria-hidden="true">*</span></span><input aria-label="4+ digit terakhir" name="matchHint" minLength="4" required aria-describedby="known-hint"/><FieldHint id="known-hint">Empat digit atau lebih dari nomor rekening tujuan, untuk mengenali transfer.</FieldHint></label><label><span>Hubungan</span><select aria-label="Hubungan" name="relationship" defaultValue="OWN_ACCOUNT"><option value="OWN_ACCOUNT">Akun sendiri</option><option value="HOUSEHOLD_ACCOUNT">Keluarga</option><option value="INVESTMENT_ACCOUNT">Investasi</option><option value="OTHER">Lainnya</option></select></label><label><span>Akun kekayaan</span><select aria-label="Akun kekayaan" name="wealthAccountId" defaultValue=""><option value="">Tanpa akun</option>{data.wealthAccounts.filter(item => item.active).map(item => <option value={item.id} key={item.id}>{item.name}</option>)}</select></label><label><span>Anggota</span><select aria-label="Anggota" name="userId" defaultValue=""><option value="">Tidak ada</option>{data.members.filter(item => item.active).map(item => <option value={item.id} key={item.id}>{item.displayName}</option>)}</select></label><button disabled={isBusy("known-new")}>Tambah</button></form>}
        <RowError k="known-new" error={error}/>
        <BoundedList items={data.known} renderItem={item => (<article key={item.id} className={highlight === item.id ? "is-new" : undefined}><div><b>{item.displayName}</b><small>{item.institution} · ••••{item.matchHint}</small></div><span>{label(relationshipLabel, item.relationship)}</span>{owner && <div className="row-actions"><select aria-label={`Akun kekayaan ${item.displayName}`} value={item.wealthAccountId || ""} disabled={isBusy(item.id)} onChange={event => patch(item.id, `known-accounts/${item.id}`, { wealthAccountId: event.target.value || null })}><option value="">Tanpa akun</option>{data.wealthAccounts.filter(account => account.active).map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select><button className={item.active ? "danger-quiet" : "secondary"} aria-label={`${item.active ? "Nonaktifkan" : "Aktifkan"} ${item.displayName}`} disabled={isBusy(item.id)} onClick={event => toggle(item.id, `known-accounts/${item.id}`, { active: !item.active }, { active: item.active, message: `Nonaktifkan ${item.displayName}? Transfer ke tujuan ini tidak lagi dikenali sebagai transfer sendiri.`, success: `${item.displayName} ${item.active ? "dinonaktifkan" : "diaktifkan"}.`, highlight: item.id, button: event.currentTarget })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button></div>}<RowError k={item.id} error={error}/></article>)} emptyMessage="Belum ada tujuan transfer yang dikenal." />
      </SettingsSection>
      <SettingsSection tab="sumber" active={tab} title="Notifikasi bank" description="Alamat pengirim email notifikasi bank yang tepercaya. Tambahkan agar transaksi dari bank ini tercatat otomatis sebagai pengeluaran.">
        {owner && <form className="inline-form" onSubmit={createListener}><label><span>Nama bank <span className="req" aria-hidden="true">*</span></span><input aria-label="Nama bank" name="bankName" required/></label><label><span>Alamat pengirim <span className="req" aria-hidden="true">*</span></span><input aria-label="Alamat pengirim" name="senderAddress" type="email" required {...fieldError("listener-new", "senderAddress")}/></label><button disabled={isBusy("listener-new")}>Tambah</button></form>}
        <RowError k="listener-new" error={error}/>
        <BoundedList items={data.listeners} renderItem={item => (<article key={item.id} className={highlight === item.id ? "is-new" : undefined}><div><b>{item.bankName}</b><small>{item.senderAddress}</small></div><span>{item.active ? "Aktif" : "Nonaktif"} · Pengeluaran Saja</span>{owner && <button className={item.active ? "danger-quiet" : "secondary"} aria-label={`${item.active ? "Nonaktifkan" : "Aktifkan"} ${item.bankName}`} disabled={isBusy(item.id)} onClick={event => toggle(item.id, `bank-email-listeners/${item.id}`, { active: !item.active }, { active: item.active, message: `Nonaktifkan ${item.bankName}? Email dari ${item.bankName} tidak akan dicatat lagi sampai diaktifkan kembali.`, success: `Email dari ${item.bankName} ${item.active ? "tidak lagi dicatat" : "dicatat kembali"}.`, highlight: item.id, button: event.currentTarget })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button>}<RowError k={item.id} error={error}/></article>)} emptyMessage="Belum ada notifikasi bank yang dipercaya." />
      </SettingsSection>
      <SettingsSection tab="sumber" active={tab} title="Provider finansial" description="Email otomatis dari aplikasi keuangan seperti dompet digital atau investasi. Sumber baru belum aktif sampai Anda mengaktifkannya.">
        {owner && <form className="inline-form" onSubmit={createFinancialSource}><label><span>Nama provider <span className="req" aria-hidden="true">*</span></span><input aria-label="Nama provider" name="providerName" required {...fieldError("financial-source-new", "providerName")}/></label><label><span>Alamat pengirim <span className="req" aria-hidden="true">*</span></span><input aria-label="Alamat pengirim" name="senderAddress" type="email" required/></label><label className="check" style={capabilityCheckStyle}><input name="cashMovement" type="checkbox" defaultChecked style={capabilityInputStyle} {...fieldError("financial-source-new", "cashMovement")}/> Pergerakan dana</label><label className="check" style={capabilityCheckStyle}><input name="wealthValue" type="checkbox" defaultChecked style={capabilityInputStyle}/> Nilai aset / portfolio</label><label><span>Akun kekayaan bawaan</span><select aria-label="Akun kekayaan bawaan" name="defaultWealthAccountId" defaultValue="" aria-describedby="provider-wealth-hint"><option value="">Tanpa akun</option>{data.wealthAccounts.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><FieldHint id="provider-wealth-hint">Akun kekayaan tempat saldo dari email ini dicatat.</FieldHint></label><button disabled={isBusy("financial-source-new")}>Tambah (belum aktif)</button></form>}
        <RowError k="financial-source-new" error={error}/>
        {owner && editingFinancialSource && <form key={editingFinancialSource.id} className="inline-form" onSubmit={saveFinancialSource}><label><span>Nama provider <span className="req" aria-hidden="true">*</span></span><input aria-label="Nama provider" name="providerName" defaultValue={editingFinancialSource.providerName} required {...fieldError(`edit-${editingFinancialSource.id}`, "providerName")}/></label><label><span>Alamat pengirim <span className="req" aria-hidden="true">*</span></span><input aria-label="Alamat pengirim" name="senderAddress" type="email" defaultValue={editingFinancialSource.senderAddress} required/></label><label className="check" style={capabilityCheckStyle}><input name="cashMovement" type="checkbox" defaultChecked={editingFinancialSource.capabilities.includes("CASH_MOVEMENT")} style={capabilityInputStyle} {...fieldError(`edit-${editingFinancialSource.id}`, "cashMovement")}/> Pergerakan dana</label><label className="check" style={capabilityCheckStyle}><input name="wealthValue" type="checkbox" defaultChecked={editingFinancialSource.capabilities.includes("WEALTH_VALUE")} style={capabilityInputStyle}/> Nilai aset / portfolio</label><label><span>Akun kekayaan bawaan</span><select aria-label="Akun kekayaan bawaan" name="defaultWealthAccountId" defaultValue={editingFinancialSource.defaultWealthAccountId || ""} aria-describedby="provider-edit-wealth-hint"><option value="">Tanpa akun</option>{data.wealthAccounts.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><FieldHint id="provider-edit-wealth-hint">Akun kekayaan tempat saldo dari email ini dicatat.</FieldHint></label><button disabled={isBusy(`edit-${editingFinancialSource.id}`)}>Simpan</button><button type="button" className="secondary" onClick={() => setEditingFinancialSource(null)}>Batal</button><RowError k={`edit-${editingFinancialSource.id}`} error={error}/></form>}
        <BoundedList items={data.financialSources} renderItem={item => (<article key={item.id} className={highlight === item.id ? "is-new" : undefined}><div><b>{item.providerName}</b><small>{item.senderAddress}{item.lastReceivedAt ? ` · diterima ${dateTime(item.lastReceivedAt)}` : ""}</small></div><span>{label(financialSourceStatusLabel, item.status)} · {labels(capabilityLabel, item.capabilities)}{item.defaultWealthAccountId ? " · akun kekayaan bawaan terpasang" : " · ⚠ Perlu akun kekayaan bawaan"}</span>{owner && <div className="row-actions">{item.status !== "ACTIVE" && <button className="secondary" aria-label={`Ubah ${item.providerName}`} disabled={isBusy(item.id)} onClick={() => editFinancialSource(item)}>Ubah</button>}<button className="secondary" aria-label={`Uji ${item.providerName}`} disabled={isBusy(`test-${item.id}`)} onClick={() => testFinancialSource(item)}>Uji</button>{item.status !== "ACTIVE" && <button aria-label={`Aktifkan ${item.providerName}`} disabled={isBusy(item.id)} onClick={event => toggle(item.id, `financial-email-sources/${item.id}`, { status: "ACTIVE" }, { success: `${item.providerName} diaktifkan.`, highlight: item.id, button: event.currentTarget })}>Aktifkan</button>}{item.status === "ACTIVE" && <button className="danger-quiet" aria-label={`Nonaktifkan ${item.providerName}`} disabled={isBusy(item.id)} onClick={event => toggle(item.id, `financial-email-sources/${item.id}`, { status: "DISABLED" }, { active: true, message: `Nonaktifkan ${item.providerName}? Email dari provider ini tidak akan diproses lagi sampai diaktifkan kembali.`, success: `${item.providerName} dinonaktifkan.`, highlight: item.id, button: event.currentTarget })}>Nonaktifkan</button>}</div>}<RowError k={item.id} error={error}/></article>)} emptyMessage="Belum ada sumber email finansial." emptyClassName="empty compact" />
        {financialPreview && <pre className="notice">Preview tanpa mutasi canonical: {JSON.stringify(financialPreview.result || financialPreview.error, null, 2)}</pre>}
      </SettingsSection>
      <SettingsSection tab="kategori" active={tab} title="Kategori keluarga" description="Kategori untuk merapikan transaksi. Nonaktifkan yang tidak dipakai; riwayatnya tetap tersimpan.">
        {owner && <form className="inline-form" onSubmit={createCategory}><label><span>Nama kategori <span className="req" aria-hidden="true">*</span></span><input aria-label="Nama kategori" name="name" required {...fieldError("category-new", "name")}/></label><label><span>Kategori induk</span><select aria-label="Kategori induk" name="parentId" defaultValue=""><option value="">Tanpa induk</option>{data.categories.filter(item => !item.parentId && item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><button disabled={isBusy("category-new")}>Tambah</button></form>}
        <RowError k="category-new" error={error}/>
        <BoundedList items={data.categories} renderItem={item => (<article key={item.id} className={highlight === item.id ? "is-new" : undefined}><div><b>{item.parentId ? "↳ " : ""}{item.name}</b></div><span>{item.active ? "Aktif" : "Nonaktif"}</span>{owner && <div className="row-actions"><button className="secondary" aria-label={`Ubah nama ${item.name}`} disabled={isBusy(item.id)} onClick={() => rename(item, "kategori")}>Ubah nama</button><button className={item.active ? "danger-quiet" : "secondary"} aria-label={`${item.active ? "Nonaktifkan" : "Aktifkan"} ${item.name}`} disabled={isBusy(item.id)} onClick={event => toggle(item.id, `categories/${item.id}`, { active: !item.active }, { active: item.active, message: `Nonaktifkan ${item.name}? Kategori ini tidak lagi bisa dipilih untuk transaksi baru; riwayatnya tetap tersimpan.`, success: `Kategori ${item.name} ${item.active ? "dinonaktifkan" : "diaktifkan"}.`, highlight: item.id, button: event.currentTarget })}>{item.active ? "Nonaktifkan" : "Aktifkan"}</button></div>}<RowError k={item.id} error={error}/></article>)} emptyMessage="Belum ada kategori keluarga." />
      </SettingsSection>
      <SettingsSection tab="kategori" active={tab} title="Aturan kategori otomatis" description="Kategori yang terisi otomatis untuk merchant tertentu. Hanya aturan yang Anda aktifkan yang berjalan.">
        <BoundedList items={data.aliases} renderItem={item => (<article key={item.id} className={highlight === item.id ? "is-new" : undefined}><div><b>{item.normalizedName}</b><small>Alias: {item.rawName}</small></div><span>{item.defaultCategoryName || "Tanpa kategori"}</span>{owner && <button className={item.autoApply ? "danger-quiet" : "secondary"} aria-label={`${item.autoApply ? "Nonaktifkan" : "Aktifkan"} aturan ${item.normalizedName}`} disabled={isBusy(item.id)} onClick={event => toggle(item.id, `merchant-aliases/${item.id}`, { autoApply: !item.autoApply }, { active: item.autoApply, message: `Nonaktifkan aturan ${item.normalizedName}? Kategori tidak lagi terisi otomatis untuk merchant ini.`, success: `Aturan ${item.normalizedName} ${item.autoApply ? "dinonaktifkan" : "diaktifkan"}.`, highlight: item.id, button: event.currentTarget })}>{item.autoApply ? "Nonaktifkan" : "Aktifkan"}</button>}<RowError k={item.id} error={error}/></article>)} emptyMessage="Belum ada aturan merchant." emptyClassName="empty compact" />
      </SettingsSection>
      <SettingsSection tab="sumber" active={tab} title="Email, Telegram, dan pemrosesan otomatis" description="Hubungkan email bank dan Telegram. Ikuti langkahnya; email baru diproses setelah pemilik mengaktifkannya.">
        <EmailOnboardingChecklist ingress={data.emailIngress} owner={owner} isBusy={isBusy} onCreate={() => emailIngressAction()} onActivate={activateEmailIngress} onCopy={copyEmailIngress} onResolveForwarding={id => request("forwarding-resolve", `/api/v1/integration-actions/${id}/resolve`, { method: "POST" }, { success: "Penerusan ditandai selesai." })}/>
        <RowError k="email-ingress" error={error}/>
        <RowError k="forwarding-resolve" error={error}/>
        <div className="integration-grid"><article><span className={`health-dot ${data.emailIngress?.status === "ACTIVE" ? "ok" : ""}`}/><div><b>Email notifikasi bank</b><small>{data.emailIngress?.address || "Alamat belum dibuat"}{data.emailIngress?.status ? ` · ${label(financialSourceStatusLabel, data.emailIngress.status)}` : ""}{data.emailIngress?.lastReceivedAt ? ` · diterima ${dateTime(data.emailIngress.lastReceivedAt)}` : ""}</small></div><div className="integration-actions">{data.emailIngress?.address && <button className="secondary" onClick={copyEmailIngress}>Salin</button>}{owner && data.emailIngress?.address && <button className="secondary" disabled={isBusy("email-ingress-rotate")} onClick={async () => { if (await confirm(ROTATE_CONFIRM, { confirmLabel: "Rotasi alamat", danger: true })) emailIngressAction("rotate"); }}>Rotasi</button>}</div></article><article><span className={`health-dot ${data.members.some(item => item.telegramConnected) ? "ok" : ""}`}/><div><b>Telegram</b><small>{data.members.filter(item => item.telegramConnected).length} anggota terhubung</small></div><a className="button secondary" href="/household?tab=anggota">Kelola</a></article><article id="status-pemrosesan"><span className={`health-dot ${data.operations?.worker?.healthy && data.operations?.llmGateway?.configured ? "ok" : ""}`}/><div><b>Status pemrosesan otomatis</b><small>Pemrosesan email dan dokumen {data.operations?.worker?.healthy ? "berjalan" : "tertunda"} · asisten AI {data.operations?.llmGateway?.configured ? "siap" : "belum diatur"}</small></div></article></div>
      </SettingsSection>
      <IntegrationActionsSection tab="sumber" active={tab} onToast={setToast} onError={message => setError({ key: "forwarding-resolve", message })}/>
      <DeliveryLogSection tab="sumber" active={tab}/>
      <NotificationsSection tab="notifikasi" active={tab} />
    </div>
  </>;
}

const CHECKLIST_STATE_TEXT = { done: "Selesai", current: "Langkah berikutnya", blocked: "Menunggu langkah sebelumnya" };

// Guided onboarding for email ingestion. A pure view of the server's step
// states: the server decides done/current/blocked, this only presents them.
export function EmailOnboardingChecklist({ ingress, owner = false, isBusy = () => false, onCreate, onActivate, onCopy, onResolveForwarding }) {
  const steps = ingress?.steps;
  if (!steps) return null;
  const action = owner ? ingress.forwardingAction : null;
  const items = [
    { key: "address", title: "Buat alamat", state: steps.address,
      detail: steps.address === "done" ? ingress.address : "Richmod membuat alamat email khusus untuk keluarga Anda.",
      actions: steps.address === "current" && owner && <button type="button" disabled={isBusy("email-ingress-create")} onClick={onCreate}>Buat alamat</button> },
    { key: "sender", title: "Tambah pengirim", state: steps.sender,
      detail: steps.sender === "done" ? "Ada pengirim bank atau provider yang aktif." : steps.sender === "blocked" ? "Buat alamat email terlebih dahulu." : "Daftarkan alamat pengirim bank atau provider yang boleh masuk.",
      actions: steps.sender === "current" && owner && <><a className="button" href="#notifikasi-bank">Tambah pengirim bank</a><a className="button secondary" href="#provider-finansial">Tambah provider</a></> },
    { key: "forwarding", title: "Verifikasi penerusan", state: steps.forwarding,
      detail: steps.forwarding === "done" ? "Email dari penerusan sudah diterima." : steps.forwarding === "blocked" ? "Buat alamat email terlebih dahulu." : "Tambahkan alamat ini sebagai alamat penerusan di Gmail atau penyedia email Anda. Langkah ini selesai otomatis setelah email pertama masuk.",
      actions: steps.forwarding !== "blocked" && ingress.address && <button type="button" className="secondary" onClick={onCopy}>Salin alamat</button> },
    { key: "activated", title: "Aktifkan", state: steps.activated,
      detail: steps.activated === "done" ? "Email yang cocok dengan pengirim aktif akan diproses." : steps.activated === "blocked" ? (steps.address === "done" ? "Tambahkan pengirim terlebih dahulu." : "Buat alamat email terlebih dahulu.") : "Aktifkan setelah pengirim terdaftar. Disarankan memverifikasi penerusan lebih dulu.",
      actions: steps.activated === "current" && owner && <button type="button" disabled={isBusy("email-ingress-activate")} onClick={onActivate}>Aktifkan</button> },
  ];
  return <div className="email-checklist" data-testid="email-checklist">
    <ol aria-label="Langkah menghubungkan email">
      {items.map((item, index) => <li key={item.key} data-step={item.key} data-state={item.state} aria-current={item.state === "current" ? "step" : undefined}>
        <span className="step-index" aria-hidden="true">{item.state === "done" ? "✓" : index + 1}</span>
        <div><b>{item.title}</b><small className="step-state">{CHECKLIST_STATE_TEXT[item.state]}</small><small>{item.detail}</small>
          {item.key === "forwarding" && action && <div data-testid="forwarding-verification"><small><b>{action.title}</b> Google meminta konfirmasi sebelum email diteruskan.</small>{action.actionCode && <small className="action-code">Kode konfirmasi: <code>{action.actionCode}</code></small>}</div>}
        </div>
        <div className="integration-actions">{item.actions}{item.key === "forwarding" && action && <>{action.actionUrl && <a className="button" href={action.actionUrl} target="_blank" rel="noopener noreferrer">Verifikasi penerusan</a>}<button type="button" className="secondary" disabled={isBusy("forwarding-resolve")} onClick={() => onResolveForwarding(action.id)}>Tandai selesai</button></>}</div>
      </li>)}
    </ol>
  </div>;
}

// The state model. Three independent facts, each possibly unknown (unknown is not false).
//
//   B — does this browser hold a push endpoint?  true | false
//   S — what does the server say about this endpoint for this session's
//       user+household?  live | retired | absent | unknown
//   E — is push enabled on the server?  enabled | disabled | unknown
//
// Every branch below is one of four rules:
//
//   1. Opt-out is available whenever B is true, even with an unreachable or disabled
//      server, a stale row or a wrong account. This is a safety property.
//   2. The endpoint is discarded only on a definitive answer about it: a subscription
//      created in this call and a definitive 4xx. A 5xx, 429, timeout, thrown request,
//      or any 4xx on a pre-existing endpoint is unknown, so the endpoint is kept.
//   3. "Aktif" is painted only when B is true and S is live.
//   4. E == disabled means new registrations will not work; it never removes the opt-out.
export function notificationState({ supported, ios, installed, permission, browserEndpoint, serverState, serverEnabled }) {
  if (!supported || (ios && !installed)) return "unsupported"; // platform cannot hold an endpoint
  if (permission === "denied") return "denied";
  if (browserEndpoint && serverState === "live") return "subscribed"; // rule 3
  if (serverEnabled === "disabled" && !browserEndpoint) return "unsupported"; // rule 4, and nothing to opt out of
  if (permission === "granted") return "granted";
  return "not-asked";
}

function base64Key(value) {
  const padded = value.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(value.length / 4) * 4, "=");
  return Uint8Array.from(atob(padded), character => character.charCodeAt(0));
}

// S for this endpoint. An endpoint the server does not know belongs to another account
// (or was pruned); a retired row is not live. With no list S is unknown. `canDisable`
// is exactly B (rule 1).
export function reconcileSubscription({ browserEndpoint = null, serverSubscriptions = null }) {
  if (!browserEndpoint) return { serverState: "absent", canDisable: false, issue: "" };
  if (!Array.isArray(serverSubscriptions)) return { serverState: "unknown", canDisable: true, issue: "unknown" };
  const row = serverSubscriptions.find(item => item?.endpoint === browserEndpoint);
  if (!row) return { serverState: "absent", canDisable: true, issue: "unregistered" };
  return row.active
    ? { serverState: "live", canDisable: true, issue: "" }
    : { serverState: "retired", canDisable: true, issue: "retired" };
}

// Three outcomes, never collapsed: `ok`, `definitive` (the server answered no) and
// `unavailable` (5xx/429, thrown, unreadable). Only a definitive answer may change
// what the user is offered.
async function requestOutcome(request, url) {
  let response;
  try {
    response = await request(url);
  } catch {
    return { status: "unavailable" };
  }
  if (!response.ok) return { status: response.status >= 500 || response.status === 429 ? "unavailable" : "definitive" };
  try {
    return { status: "ok", value: await response.json() };
  } catch {
    return { status: "unavailable" };
  }
}

export async function loadPushView({ supported, ios, installed, permission, serviceWorker, request = fetch }) {
  let browserEndpoint = null;
  if (supported) {
    try {
      const registration = await serviceWorker.getRegistration();
      browserEndpoint = (await registration?.pushManager.getSubscription())?.endpoint || null;
    } catch {
      // No registration means no browser subscription; the server list decides.
    }
  }
  // Ask for this endpoint, not a page of rows: a truncated list must never make a live
  // endpoint look absent.
  const endpointQuery = browserEndpoint ? `/api/v1/push/subscriptions?endpoint=${encodeURIComponent(browserEndpoint)}` : null;
  const [key, listed] = await Promise.all([
    requestOutcome(request, "/api/v1/push/public-key"),
    endpointQuery ? requestOutcome(request, endpointQuery) : Promise.resolve({ status: "ok", value: { subscriptions: [] } }),
  ]);
  const keyUnavailable = key.status === "unavailable" || (key.status === "ok" && typeof key.value?.enabled !== "boolean");
  const listUnavailable = listed.status !== "ok" || !Array.isArray(listed.value?.subscriptions);
  const serverEnabled = key.status === "ok" && typeof key.value?.enabled === "boolean" ? (key.value.enabled ? "enabled" : "disabled") : key.status === "definitive" ? "disabled" : "unknown";
  const serverUnavailable = serverEnabled === "unknown" || (Boolean(browserEndpoint) && listUnavailable);
  const { serverState, canDisable, issue } = reconcileSubscription({ browserEndpoint, serverSubscriptions: listUnavailable ? null : listed.value.subscriptions });
  const state = notificationState({ supported, ios, installed, permission, browserEndpoint, serverState, serverEnabled });
  return {
    state, issue, canDisable, serverState, serverEnabled, serverUnavailable, keyUnavailable,
    iosNotInstalled: ios && !installed,
    serverDisabled: supported && (!ios || installed) && serverEnabled === "disabled",
    publicKey: key.status === "ok" ? key.value?.publicKey || key.value?.public_key || "" : "",
  };
}

// Rule 2: only a subscription created in this call may be discarded, and only on a
// definitive 4xx. A pre-existing endpoint is kept on rejection because the server may
// hold a live row for it; anything else is unknown and the next refresh decides.
// Re-registering the same endpoint is an upsert.
export async function registerSubscription(serviceWorker, publicKey, request = fetch) {
  const registration = await serviceWorker.getRegistration() || await serviceWorker.register("/sw.js", { scope: "/" });
  let subscription = await registration.pushManager.getSubscription();
  let created = !subscription;
  if (created) subscription = await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: base64Key(publicKey) });
  const post = target => request("/api/v1/push/subscriptions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ endpoint: target.endpoint, keys: target.toJSON().keys }),
  });
  try {
    let response = await post(subscription);
    let body = {};
    if (!response.ok && response.status === 409) {
      try { body = await response.json(); } catch { body = {}; }
    }
    let replacedEndpoint = "";
    if (!response.ok && response.status === 409 && body?.code === "ENDPOINT_OWNED_BY_ANOTHER_USER") {
      // Another account registered this browser endpoint. Never repoint that row; replace
      // the browser subscription with a new endpoint. The old endpoint is returned for the
      // log line, never its keys.
      replacedEndpoint = subscription.endpoint;
      await subscription.unsubscribe();
      subscription = await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: base64Key(publicKey) });
      created = true;
      response = await post(subscription);
      if (response.ok) return { status: "registered", subscription, replacedEndpoint };
    }
    if (!response.ok) {
      const definitive = created && response.status >= 400 && response.status < 500 && response.status !== 408 && response.status !== 429;
      if (!definitive) return { status: "unconfirmed", subscription };
      await subscription.unsubscribe().catch(() => {});
      return { status: "rejected", subscription };
    }
    return { status: "registered", subscription };
  } catch {
    return { status: "unconfirmed", subscription };
  }
}

// Unsubscribing locally and deleting the server row are one action; either alone
// leaves a subscription the sender keeps trying to reach. The endpoint stays until
// the server confirms, so a failed delete can be retried.
export async function disableNotifications(serviceWorker, request = fetch) {
  const registration = await serviceWorker.getRegistration();
  const subscription = await registration?.pushManager?.getSubscription();
  if (!subscription) return "missing";
  let response;
  try {
    response = await request("/api/v1/push/subscriptions", {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ endpoint: subscription.endpoint }),
    });
  } catch {
    return "error";
  }
  if (!response.ok) return "unconfirmed";
  await subscription.unsubscribe();
  return "off";
}

const PUSH_COPY = {
  unsupported: ["Tidak didukung di perangkat ini", "Peramban ini belum mendukung notifikasi push."],
  "not-asked": ["Belum diaktifkan", "Aktifkan untuk menerima pemberitahuan saat ada tinjauan atau tindakan yang menunggu."],
  denied: ["Diblokir", "Notifikasi diblokir untuk situs ini. Buka pengaturan situs di peramban, izinkan notifikasi, lalu muat ulang halaman ini. Richmod tidak dapat meminta izin lagi."],
  granted: ["Belum terdaftar", "Izin notifikasi sudah diberikan, tetapi perangkat ini belum terdaftar. Coba daftarkan ulang."],
  subscribed: ["Aktif", "Notifikasi aktif di perangkat ini. Pemberitahuan muncul saat ada tinjauan atau tindakan baru."],
};

// Adornment inside `granted`; these are not additional states.
const PUSH_ISSUE = {
  unregistered: ["Belum terdaftar untuk akun ini", "Perangkat ini masih menyimpan langganan lama yang tidak terdaftar untuk akun yang sedang masuk. Coba lagi untuk mendaftarkannya kembali."],
  retired: ["Dinonaktifkan server", "Server menonaktifkan langganan perangkat ini, mungkin karena rumah tangga berubah. Coba lagi untuk mendaftarkannya kembali."],
  unknown: ["Belum terkonfirmasi", "Server tidak dapat dihubungi. Perangkat ini masih menyimpan langganan, jadi notifikasi mungkin masih aktif. Matikan notifikasi tetap tersedia."],
};

export function NotificationsView({ state, issue = "", canDisable = false, serverUnavailable = false, iosNotInstalled = false, serverDisabled = false, working = false, status = "", onEnable, onDisable }) {
  const [fallbackTitle, fallbackBody] = (state === "granted" && PUSH_ISSUE[issue]) || PUSH_COPY[state] || ["Memeriksa status notifikasi", "Richmod memeriksa dukungan notifikasi di peramban ini."];
  const title = state === "unsupported" && iosNotInstalled ? "Perlu dipasang ke Layar Utama" : fallbackTitle;
  const baseBody = state === "unsupported" && iosNotInstalled
    ? "Safari di iPhone dan iPad hanya menerima notifikasi setelah Richmod ditambahkan ke Layar Utama. Buka menu Bagikan lalu pilih Tambahkan ke Layar Utama."
    : state === "unsupported" && serverDisabled
      ? "Server ini belum mengaktifkan notifikasi push, jadi perangkat belum dapat mendaftar."
      : fallbackBody;
  // Rule 4: a disabled server stops new registrations, not this device's opt-out.
  const body = `${baseBody}${serverDisabled && canDisable ? " Server tidak menerima pendaftaran baru, tetapi perangkat ini tetap dapat dimatikan." : ""}`;
  const showDisable = canDisable; // Rule 1: B alone.
  const showEnable = (state === "not-asked" || state === "granted") && !serverDisabled;
  const note = status || (serverUnavailable ? "Server tidak dapat dihubungi. Status langganan belum dapat dikonfirmasi." : "");
  return <>
    <div className="notification-state">
      <div className="notification-copy"><b>{title}</b><p>{body}</p></div>
      {showEnable && <button type="button" disabled={working} onClick={onEnable}>{working ? "Memproses…" : state === "granted" ? "Coba lagi" : "Aktifkan notifikasi"}</button>}
      {showDisable && <button type="button" className="secondary" disabled={working} onClick={onDisable}>{working ? "Mematikan…" : "Matikan notifikasi"}</button>}
    </div>
    <p className="notification-status" role="status" aria-live="polite">{note}</p>
  </>;
}

function NotificationsSection({ tab, active }) {
  const [view, setView] = useState({ state: "checking", issue: "", canDisable: false, serverUnavailable: false, keyUnavailable: false, iosNotInstalled: false, serverDisabled: false, publicKey: "" });
  const [working, setWorking] = useState(false);
  const [status, setStatus] = useState("");

  const refresh = useCallback(async () => {
    const supported = typeof window !== "undefined" && "PushManager" in window && "serviceWorker" in navigator && "Notification" in window;
    const ios = typeof navigator !== "undefined" && (/iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1));
    const installed = typeof navigator !== "undefined" && (navigator.standalone === true || window.matchMedia?.("(display-mode: standalone)")?.matches === true);
    setView(await loadPushView({
      supported, ios, installed,
      permission: supported ? Notification.permission : "default",
      serviceWorker: navigator.serviceWorker,
    }));
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
      if (view.keyUnavailable) { setStatus("Server tidak dapat dihubungi. Coba lagi nanti."); return; }
      if (!view.publicKey) { setStatus("Server ini belum mengaktifkan notifikasi push."); return; }
      const result = await registerSubscription(navigator.serviceWorker, view.publicKey);
      if (result.replacedEndpoint) console.info(`push subscription replaced: ${result.replacedEndpoint}`);
      setStatus(result.status === "registered" ? result.replacedEndpoint ? "Notifikasi didaftarkan ulang untuk akun ini." : "Notifikasi aktif di perangkat ini." : result.status === "rejected" ? "Perangkat ini belum dapat didaftarkan. Coba lagi." : "Pendaftaran belum terkonfirmasi. Status diperiksa ulang dari server.");
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
      setStatus(result === "missing" ? "Perangkat ini memang belum terdaftar."
        : result === "off" ? "Notifikasi dimatikan di perangkat ini."
          : result === "unconfirmed" ? "Penghapusan belum dikonfirmasi server. Perangkat ini masih terdaftar; coba matikan lagi."
            : "Server tidak dapat dihubungi. Perangkat ini masih terdaftar; coba matikan lagi.");
    } catch {
      setStatus("Notifikasi belum dapat dimatikan. Coba lagi.");
    } finally {
      setWorking(false);
      await refresh();
    }
  }

  return <SettingsSection tab={tab} active={active} title="Notifikasi" description="Notifikasi dikirim ke perangkat ini saat ada transaksi atau tindakan integrasi yang menunggu tinjauan.">
    <NotificationsView state={view.state} issue={view.issue} canDisable={view.canDisable} serverUnavailable={view.serverUnavailable} iosNotInstalled={view.iosNotInstalled} serverDisabled={view.serverDisabled} working={working} status={status} onEnable={enable} onDisable={disable} />
  </SettingsSection>;
}

// A failed change is announced where it happened: inside the form or the row. An empty
// message renders nothing, so an alert never appears blank.
function RowError({ k, error }) {
  if (!error?.message || error.key !== k) return null;
  return <p className="field-error" id={`${k}-error`} role="alert">{error.message}</p>;
}

// One line under a field that explains a term the family may not know. The field
// points at it with aria-describedby, so a screen reader hears the explanation.
function FieldHint({ id, children }) {
  return <small className="field-hint" id={id}>{children}</small>;
}

function SettingsSection({ title, description, tab, active, children }) {
  // Only the active tab's sections are mounted, so a hidden section never
  // competes for an anchor or a screen reader's attention.
  if (tab && tab !== active) return null;
  const id = title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
  return <section id={id} aria-labelledby={`${id}-title`} className="surface settings-section"><div className="settings-heading"><div><h2 id={`${id}-title`}>{title}</h2><p>{description}</p></div></div>{children}</section>;
}

function BoundedList({ items, limit = 8, renderItem, emptyMessage, emptyClassName = "empty compact" }) {
  const [page, setPage] = useState(0), [query, setQuery] = useState("");
  // A long list gets a search box; a short one does not need one.
  const searchable = items.length > 10;
  const filtered = filterItems(items, query);
  const pageCount = Math.ceil(filtered.length / limit);
  const currentPage = Math.min(page, Math.max(0, pageCount - 1));
  const visible = filtered.slice(currentPage * limit, (currentPage + 1) * limit);
  return <div className="settings-list">
    {searchable && <div className="list-search"><label><span>Cari</span><input type="search" value={query} placeholder="Cari nama" onChange={event => { setQuery(event.target.value); setPage(0); }}/></label><p className="list-count" role="status">{query.trim() ? `${filtered.length} dari ${items.length} item` : `${items.length} item`}</p></div>}
    {!searchable && items.length > limit && <p className="list-count">{items.length} item</p>}
    {visible.map(renderItem)}
    {!filtered.length && (query.trim() ? <p className={emptyClassName}>Tidak ada yang cocok dengan pencarian.</p> : emptyMessage && <p className={emptyClassName}>{emptyMessage}</p>)}
    {pageCount > 1 && <div className="list-pagination"><button type="button" className="secondary" disabled={currentPage === 0} onClick={() => setPage(value => value - 1)}>Sebelumnya</button><span>Halaman {currentPage + 1} dari {pageCount}</span><button type="button" className="secondary" disabled={currentPage === pageCount - 1} onClick={() => setPage(value => value + 1)}>Berikutnya</button></div>}
  </div>;
}

const ACTION_KINDS = { EMAIL_FORWARDING: "Email", SOURCE_PROCESSING: "Sumber" };

// Pending setup actions for the connected sources (e.g. the email forwarding verification
// code). The owner resolves them here; the count is shared with the rest of the shell.
function IntegrationActionsSection({ tab, active, onToast, onError }) {
  const open = tab === active;
  const [state, setState] = useState({ status: "idle", items: [] });
  const [resolving, setResolving] = useState("");
  const loadActions = useCallback(async () => {
    setState(current => ({ ...current, status: current.items.length ? "refreshing" : "loading" }));
    try {
      const response = await fetch("/api/v1/integration-actions");
      if (!response.ok) throw new Error("load failed");
      const items = await response.json();
      const list = Array.isArray(items) ? items : [];
      setState({ status: "ready", items: list });
      publishInboxCount({ actionCount: list.length });
    } catch { setState(current => ({ ...current, status: "error" })); }
  }, []);
  useEffect(() => { if (open) loadActions(); }, [open, loadActions]);
  async function resolve(id) {
    if (resolving) return;
    setResolving(id);
    try {
      const response = await fetch(`/api/v1/integration-actions/${id}/resolve`, { method: "POST" });
      if (!response.ok) { onError("Tindakan belum dapat ditandai selesai. Coba lagi."); return; }
      const remaining = state.items.filter(item => item.id !== id);
      setState({ status: "ready", items: remaining });
      publishInboxCount({ actionCount: remaining.length });
      onToast("Tindakan ditandai selesai.");
    } catch { onError("Koneksi terputus. Tindakan belum ditandai selesai."); }
    finally { setResolving(""); }
  }
  return <SettingsSection tab={tab} active={active} title="Tindakan" description="Langkah setup sumber otomatis yang menunggu Anda, misalnya kode verifikasi penerusan email.">
    {state.status === "error" && <p role="alert">Tindakan belum dapat dimuat. <button type="button" className="secondary" onClick={loadActions}>Coba lagi</button></p>}
    {(state.status === "idle" || state.status === "loading") && <p className="muted">Memuat…</p>}
    {state.status !== "error" && state.status !== "idle" && state.status !== "loading" && !state.items.length && <p className="empty compact">Tidak ada tindakan tertunda. Richmod akan menampilkan kebutuhan setup di sini.</p>}
    <div className="action-list">{state.items.map(item => <article key={item.id} className="action-card"><div><span className="eyebrow">{ACTION_KINDS[item.integrationType] || item.integrationType}</span><h3>{item.title}</h3><p>{item.description}</p><small>Diterima {dateTime(item.createdAt)}</small>{item.actionCode && <p className="action-code">Kode konfirmasi: <code>{item.actionCode}</code></p>}</div><div className="action-buttons">{item.actionUrl && <a className="button" href={item.actionUrl} target="_blank" rel="noopener noreferrer">Verifikasi</a>}<button type="button" className="secondary" disabled={Boolean(resolving)} onClick={() => resolve(item.id)}>Tandai selesai</button></div></article>)}</div>
  </SettingsSection>;
}

// The newest forwarded emails, newest first, paged by the server.
function DeliveryLogSection({ tab, active }) {
  const open = tab === active;
  const [state, setState] = useState({ status: "idle", items: [], next: "", more: false });
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!open) return undefined;
    const controller = new AbortController();
    setState(current => ({ ...current, status: current.status === "ready" ? "refreshing" : "loading" }));
    fetch("/api/v1/integrations/email-ingress/deliveries", { signal: controller.signal })
      .then(async response => { if (!response.ok) throw new Error("load failed"); const items = await response.json(); setState({ status: "ready", items: Array.isArray(items) ? items : [], next: response.headers.get("X-Next-Cursor") || "", more: false }); })
      .catch(failure => { if (failure?.name !== "AbortError") setState(current => ({ ...current, status: "error" })); });
    return () => controller.abort();
  }, [open, tick]);
  async function loadMore() {
    if (!state.next || state.more) return;
    setState(current => ({ ...current, more: true, moreError: false }));
    try {
      const response = await fetch(`/api/v1/integrations/email-ingress/deliveries?cursor=${encodeURIComponent(state.next)}`);
      if (!response.ok) throw new Error("load failed");
      const older = await response.json();
      setState(current => ({ ...current, items: [...current.items, ...(Array.isArray(older) ? older : [])], next: response.headers.get("X-Next-Cursor") || "", more: false }));
    } catch { setState(current => ({ ...current, more: false, moreError: true })); }
  }
  return <SettingsSection tab={tab} active={active} title="Email masuk terakhir" description="Email yang diteruskan ke alamat Richmod dan apa yang terjadi dengannya.">
    <EmailDeliveries state={state} retry={() => setTick(value => value + 1)} loadMore={loadMore}/>
  </SettingsSection>;
}
