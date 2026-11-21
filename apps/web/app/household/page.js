"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowRight, ShieldChevron } from "@phosphor-icons/react";
import AppShell from "../components/AppShell";
import { ErrorNotice, Toast } from "../components/Feedback";
import useDialogs from "../components/useDialogs";
import SettingsPanels from "../components/SettingsPanels";
import { useInboxCounts } from "../components/InboxCountProvider";
import { nextTabIndex } from "../lib/focusTrap";
import { tabFromLocation, tabsForRole } from "../lib/settingsTabs";
import { LoadError, responseError, useBusyMap, useGuardedLoad } from "../lib/useLoadState";

const roleLabel = (role) => ({ OWNER: "Administrator", MEMBER: "Anggota" })[role] || role;
const untilLabel = iso => new Date(iso).toLocaleString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }) + " WIB";
const TICK_MS = 15000;
const INVITE_PATH = { telegram: "telegram-invite", dashboard: "dashboard-invite" };

export default function HouseholdPage() {
  const { confirm, dialogs } = useDialogs();
  const [household, setHousehold] = useState(null);
  const [membersState, setMembers] = useState(null);
  const members = membersState || [];
  const [me, setMe] = useState(null);
  const [invite, setInvite] = useState(null);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const [toast, setToast] = useState(""), [highlight, setHighlight] = useState("");
  const closeToast = useCallback(() => setToast(""), []);
  useEffect(() => { if (!highlight) return undefined; const timer = window.setTimeout(() => setHighlight(""), 2000); return () => window.clearTimeout(timer); }, [highlight]);
  const { busy, run: runBusy } = useBusyMap();
  const linkInput = useRef(null);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), TICK_MS); return () => window.clearInterval(timer); }, []);
  const stillValid = iso => Boolean(iso) && new Date(iso).getTime() > now;

  const loader = useGuardedLoad(async ({ signal, current }) => {
    const [meResponse, householdResponse, membersResponse] = await Promise.all(["/api/v1/auth/me", "/api/v1/household", "/api/v1/household/members"].map(url => fetch(url, { signal })));
    if (meResponse.status === 401) { window.location.href = "/"; return; }
    if (!meResponse.ok) throw new LoadError("Sesi belum dapat diperiksa. Coba lagi.");
    const nextMe = await meResponse.json();
    const nextHousehold = householdResponse.ok ? await householdResponse.json().catch(() => null) : null;
    const nextMembers = membersResponse.ok ? await membersResponse.json().catch(() => null) : null;
    if (!current()) return;
    setMe(nextMe);
    if (nextHousehold) setHousehold(nextHousehold);
    if (Array.isArray(nextMembers)) setMembers(nextMembers);
    if (!nextHousehold || !Array.isArray(nextMembers)) throw new LoadError("Data keluarga belum dapat dimuat sepenuhnya. Coba lagi.");
  }, { fallback: "Data keluarga belum dapat dimuat. Periksa koneksi lalu coba lagi." });
  const load = loader.run;

  useEffect(() => { load(); }, [load]);
  useEffect(() => { setCopied(false); }, [invite?.link]);
  useEffect(() => { if (invite?.link) linkInput.current?.focus(); }, [invite?.link]);
  const owner = me?.household?.role === "OWNER";
  const { actionCount } = useInboxCounts();
  const tabs = tabsForRole(owner);
  const tabKeyList = tabs.map(([key]) => key).join(",");
  const [tab, setTab] = useState("anggota"), [anchor, setAnchor] = useState("");
  useEffect(() => {
    if (me === null) return undefined;
    const allowed = tabKeyList.split(",");
    const apply = () => { const next = tabFromLocation(window.location.search, window.location.hash, allowed); setTab(next.tab); setAnchor(next.anchor); };
    apply();
    window.addEventListener("popstate", apply);
    window.addEventListener("hashchange", apply);
    return () => { window.removeEventListener("popstate", apply); window.removeEventListener("hashchange", apply); };
  }, [me, tabKeyList]);
  const clearAnchor = useCallback(() => setAnchor(""), []);
  function selectTab(key) { setTab(key); setAnchor(""); window.history.pushState({}, "", `/household?tab=${key}`); }
  function tabKeys(event, index) {
    const next = nextTabIndex(event.key, index, tabs.length);
    if (next === null) return;
    event.preventDefault();
    selectTab(tabs[next][0]);
    event.currentTarget.parentElement?.querySelectorAll("[role=tab]")[next]?.focus();
  }
  useEffect(() => { document.getElementById(`settings-tab-${tab}`)?.scrollIntoView({ block: "nearest", inline: "nearest" }); }, [tab]);
  useEffect(() => {
    if (tab !== "anggota" || !anchor) return;
    const target = document.getElementById(anchor);
    if (target) { target.scrollIntoView({ block: "start" }); setAnchor(""); }
  }, [tab, anchor, me]);

  function addMember(event) {
    event.preventDefault(); const formEl = event.currentTarget; const form = new FormData(formEl);
    const displayName = String(form.get("displayName") || "");
    return runBusy("add-member", async () => {
      setError("");
      try {
        const response = await fetch("/api/v1/household/members", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ displayName, email: form.get("email") }) });
        if (!response.ok) { setError(await responseError(response, "Anggota belum dapat ditambahkan.")); return; }
        const created = await response.json().catch(() => ({}));
        formEl.reset(); formEl.querySelector("input")?.focus();
        setToast(`Anggota ${displayName} ditambahkan.`);
        if (created?.id) setHighlight(created.id);
        await load();
      } catch { setError("Koneksi terputus. Anggota belum ditambahkan."); }
    });
  }

  async function issueInvite(memberId, kind, failure, build) {
    setError("");
    try {
      const response = await fetch(`/api/v1/household/members/${memberId}/${INVITE_PATH[kind]}`, { method: "POST" });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) { setError(body.error || failure); return false; }
      if (kind === "telegram" && !body.link) { setError("Bot Telegram belum dikonfigurasi, tautan undangan belum bisa dibuat."); return false; }
      const name = members.find(item => item.id === memberId)?.displayName || "anggota";
      setInvite(build(body));
      setToast(kind === "telegram" ? `Undangan Telegram untuk ${name} dibuat.` : body.purpose === "RESET" ? `Tautan atur ulang untuk ${name} dibuat.` : `Undangan masuk untuk ${name} dibuat.`);
      setHighlight(memberId);
      await load(); return true;
    } catch { setError("Koneksi terputus. Undangan belum dibuat."); return false; }
  }

  const createInvite = memberId => runBusy(`member:${memberId}`, () => issueInvite(memberId, "telegram", "Undangan belum dapat dibuat.", body => ({ kind: "telegram", link: body.link, expiresAt: body.expiresAt, memberId })));
  const createDashboardInvite = memberId => runBusy(`member:${memberId}`, () => issueInvite(memberId, "dashboard", "Undangan login belum dapat dibuat.", body => ({ kind: "dashboard", reset: body.purpose === "RESET", link: window.location.origin + body.link, expiresAt: body.expiresAt, memberId })));

  async function cancelInvite(kind, memberId) {
    setError("");
    try {
      const response = await fetch(`/api/v1/household/members/${memberId}/${INVITE_PATH[kind]}`, { method: "DELETE" });
      if (!response.ok && response.status !== 404) { setError("Undangan belum dapat dibatalkan."); return false; }
      setInvite(current => (current && current.memberId === memberId && current.kind === kind ? null : current));
      setToast(`Undangan ${members.find(item => item.id === memberId)?.displayName || "anggota"} dibatalkan.`);
      setHighlight(memberId);
      await load(); return true;
    } catch { setError("Koneksi terputus. Undangan belum dibatalkan."); return false; }
  }
  const revokeInvite = (kind, memberId) => runBusy(`member:${memberId}`, () => cancelInvite(kind, memberId));

  const renewInvite = (kind, memberId) => runBusy(`member:${memberId}`, async () => {
    if (!(await cancelInvite(kind, memberId))) return;
    if (kind === "telegram") await issueInvite(memberId, "telegram", "Undangan belum dapat dibuat.", body => ({ kind: "telegram", link: body.link, expiresAt: body.expiresAt, memberId }));
    else await issueInvite(memberId, "dashboard", "Undangan login belum dapat dibuat.", body => ({ kind: "dashboard", reset: body.purpose === "RESET", link: window.location.origin + body.link, expiresAt: body.expiresAt, memberId }));
  });

  async function copyLink() {
    setError("");
    try { await navigator.clipboard.writeText(invite.link); setCopied(true); }
    catch { linkInput.current?.select(); setError("Tautan belum bisa disalin otomatis. Tautan sudah dipilih, tekan Ctrl+C."); }
  }

  function loginInviteControls(member) {
    const pending = member.dashboardInviteStatus === "PENDING" && stillValid(member.dashboardInviteExpiresAt);
    const resetting = member.passwordInitialized;
    const cancel = resetting ? "Batalkan tautan atur ulang" : "Batalkan undangan masuk";
    const renew = resetting ? "Buat tautan atur ulang baru" : "Buat tautan masuk baru";
    const create = resetting ? "Atur ulang kata sandi" : "Undang masuk ke Richmod";
    return pending
      ? <><button className="secondary" aria-label={`${cancel} ${member.displayName}`} disabled={Boolean(busy[`member:${member.id}`])} onClick={() => revokeInvite("dashboard", member.id)}>{cancel}</button><button className="secondary" aria-label={`${renew} ${member.displayName}`} disabled={Boolean(busy[`member:${member.id}`])} onClick={() => renewInvite("dashboard", member.id)}>{renew}</button></>
      : <button className="secondary" aria-label={`${create} ${member.displayName}`} disabled={Boolean(busy[`member:${member.id}`])} onClick={() => createDashboardInvite(member.id)}>{create}</button>;
  }

  function telegramInviteControls(member) {
    const create = member.role === "OWNER" ? "Hubungkan Telegram" : "Undang Telegram";
    return stillValid(member.telegramInviteExpiresAt)
      ? <><button className="secondary" aria-label={`Batalkan undangan Telegram ${member.displayName}`} disabled={Boolean(busy[`member:${member.id}`])} onClick={() => revokeInvite("telegram", member.id)}>Batalkan undangan Telegram</button><button className="secondary" aria-label={`Buat tautan Telegram baru ${member.displayName}`} disabled={Boolean(busy[`member:${member.id}`])} onClick={() => renewInvite("telegram", member.id)}>Buat tautan Telegram baru</button></>
      : <button className="secondary" aria-label={`${create} ${member.displayName}`} disabled={Boolean(busy[`member:${member.id}`])} onClick={() => createInvite(member.id)}>{create}</button>;
  }

  function inviteState(member) {
    const notes = [];
    const loginLabel = member.passwordInitialized ? "Tautan atur ulang kata sandi" : "Undangan masuk";
    if (member.dashboardInviteStatus === "PENDING") notes.push(stillValid(member.dashboardInviteExpiresAt) ? `${loginLabel} berlaku sampai ${untilLabel(member.dashboardInviteExpiresAt)}` : `${loginLabel} sudah kedaluwarsa`);
    else if (!member.passwordInitialized) notes.push("Belum membuat kata sandi");
    if (!member.telegramConnected && member.telegramInviteExpiresAt) notes.push(stillValid(member.telegramInviteExpiresAt) ? `Undangan Telegram berlaku sampai ${untilLabel(member.telegramInviteExpiresAt)}` : "Undangan Telegram sudah kedaluwarsa");
    return notes.join(" · ");
  }

  async function deactivate(memberId) {
    if (!(await confirm("Nonaktifkan anggota ini? Riwayat transaksi tetap disimpan.", { confirmLabel: "Nonaktifkan", danger: true }))) return;
    return runBusy(`member:${memberId}`, async () => {
      setError("");
      try {
        const response = await fetch(`/api/v1/household/members/${memberId}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ active: false }) });
        if (!response.ok) { setError(await responseError(response, "Anggota belum dapat dinonaktifkan.")); return; }
        setInvite(current => (current?.memberId === memberId ? null : current));
        setToast(`Anggota ${members.find(item => item.id === memberId)?.displayName || "anggota"} dinonaktifkan.`);
        setHighlight(memberId);
        await load();
        document.getElementById(`member-${memberId}`)?.focus();
      } catch { setError("Koneksi terputus. Anggota belum dinonaktifkan."); }
    });
  }

  if (!me) return loader.failed
    ? <main className="loading" role="alert">{loader.error} <button className="secondary" onClick={() => load()}>Coba lagi</button></main>
    : <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  return <AppShell user={me} eyebrow="Keluarga" title={household?.name || "Rumah Tangga"} actions={<span className="header-meta">{membersState ? `${members.filter(member => member.active).length} anggota aktif` : "Anggota belum termuat"}</span>}>
    {dialogs}
    <Toast message={toast} onClose={closeToast}/>
    {me?.isSuperAdmin && <Link className="mobile-admin-entry" href="/admin"><ShieldChevron aria-hidden="true"/><div><b>Admin Richmod</b><small>Konsol operasional &amp; rumah tangga</small></div><ArrowRight aria-hidden="true"/></Link>}
    <nav className="settings-tabs-wrap" aria-label="Bagian keluarga">
      <div className="settings-tabs" role="tablist" aria-label="Bagian keluarga">
        {tabs.map(([key, label], index) => <button key={key} role="tab" id={`settings-tab-${key}`} aria-controls="settings-panel" aria-selected={tab === key} aria-current={tab === key ? "true" : undefined} tabIndex={tab === key ? 0 : -1} className={tab === key ? "active" : "secondary"} onKeyDown={event => tabKeys(event, index)} onClick={() => selectTab(key)}>{label}{key === "sumber" && owner && actionCount > 0 && <b className="nav-badge" aria-label={`${actionCount} tindakan menunggu`}>{actionCount}</b>}</button>)}
      </div>
    </nav>
    <div role="tabpanel" id="settings-panel" aria-labelledby={`settings-tab-${tab}`}>
    {tab !== "anggota" && <SettingsPanels user={me} tab={tab} anchor={anchor} onAnchorHandled={clearAnchor}/>}
    {tab === "anggota" && <>
    <p className="page-intro">Kelola anggota dan hubungkan Telegram tanpa memasukkan ID secara manual.</p>
    <ErrorNotice message={error || loader.error} retry={error ? undefined : () => load()}/>
    <div className="admin-stack">
      <section className="panel"><div className="panel-title"><h2>Anggota</h2><span>{membersState ? `${members.length} akun` : "—"}</span></div>{!membersState && <p className="muted">Daftar anggota belum termuat.</p>}<div className="settings-list">{members.map(member => <article key={member.id} id={`member-${member.id}`} tabIndex={-1} className={highlight === member.id ? "is-new" : undefined}><div><b>{member.displayName}</b><small>{member.email}</small></div><span>{roleLabel(member.role)} · {member.telegramConnected ? "Telegram terhubung" : "Telegram belum terhubung"} · {member.active ? "Aktif" : "Nonaktif"}{member.active && inviteState(member) && <small className="invite-state">{inviteState(member)}</small>}</span>{owner && member.role === "MEMBER" && member.active && <div className="row-actions">{loginInviteControls(member)}{!member.telegramConnected && telegramInviteControls(member)}<button className="danger" aria-label={`Nonaktifkan ${member.displayName}`} disabled={Boolean(busy[`member:${member.id}`])} onClick={() => deactivate(member.id)}>Nonaktifkan</button></div>}{owner && member.role === "OWNER" && member.active && !member.telegramConnected && <div className="row-actions">{telegramInviteControls(member)}</div>}</article>)}</div></section>
      {invite && <section className="panel" aria-live="polite"><div><span className="eyebrow">{invite.kind === "telegram" ? "Undangan Telegram" : invite.reset ? "Atur ulang kata sandi" : "Undangan masuk Richmod"}</span><h2>{stillValid(invite.expiresAt) ? `Berlaku sampai ${untilLabel(invite.expiresAt)} · sekali pakai` : "Tautan ini sudah kedaluwarsa · buat tautan baru"}</h2><p>{invite.kind === "dashboard" ? invite.reset ? "Kirim tautan ini langsung kepada anggota. Setelah dipakai, kata sandi lama berhenti berlaku dan semua sesi login anggota keluar. Sebelum itu, kata sandi lama masih bisa dipakai." : "Kirim tautan ini langsung kepada anggota yang dituju. Dia membuat kata sandinya sendiri." : members.find(item => item.id === invite.memberId)?.role === "OWNER" ? "Buka tautan ini dari akun Telegram kamu sendiri." : "Kirim tautan ini langsung kepada anggota yang dituju."}</p></div><input ref={linkInput} className="invite-link" readOnly aria-label="Tautan undangan" value={invite.link} onFocus={event => event.currentTarget.select()} /><div className="invite-actions"><button onClick={copyLink}>{copied ? "Tersalin ✓" : "Salin tautan"}</button><a href={invite.link} target="_blank" rel="noreferrer">Buka tautan</a><button className="danger" disabled={Boolean(busy[`member:${invite.memberId}`])} onClick={() => revokeInvite(invite.kind, invite.memberId)}>Batalkan undangan</button></div><small>Tautan hanya tampil sekarang. Kalau hilang, batalkan lalu buat tautan baru.</small></section>}
      {owner && <section id="tambah-anggota" className="panel"><div className="panel-title"><h2>Tambah anggota</h2></div><p className="form-legend"><span className="req" aria-hidden="true">*</span> wajib diisi</p><form className="member-form" onSubmit={addMember}><label><span>Nama anggota <span className="req" aria-hidden="true">*</span></span><input aria-label="Nama anggota" name="displayName" autoCapitalize="words" enterKeyHint="next" maxLength="120" required /></label><label><span>Email anggota <span className="req" aria-hidden="true">*</span></span><input aria-label="Email anggota" name="email" type="email" enterKeyHint="done" required /></label><button disabled={Boolean(busy["add-member"])}>Tambahkan</button></form></section>}
    </div>
    </>}
    </div>
  </AppShell>;
}
