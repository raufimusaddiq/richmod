"use client";

import { useEffect, useRef, useState } from "react";
import AppShell from "../components/AppShell";
import useDialogs from "../components/useDialogs";

const roleLabel = (role) => ({ OWNER: "Administrator", MEMBER: "Anggota" })[role] || role;
const untilLabel = iso => new Date(iso).toLocaleString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }) + " WIB";
const stillValid = iso => Boolean(iso) && new Date(iso).getTime() > Date.now();
const INVITE_PATH = { telegram: "telegram-invite", dashboard: "dashboard-invite" };

export default function HouseholdPage() {
  const { confirm, dialogs } = useDialogs();
  const [household, setHousehold] = useState(null);
  const [members, setMembers] = useState([]);
  const [me, setMe] = useState(null);
  const [invite, setInvite] = useState(null);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const linkInput = useRef(null);

  async function load() {
    const [meResponse, householdResponse, membersResponse] = await Promise.all([fetch("/api/v1/auth/me"), fetch("/api/v1/household"), fetch("/api/v1/household/members")]);
    if (!meResponse.ok) { window.location.href = "/"; return; }
    setMe(await meResponse.json());
    if (householdResponse.ok) setHousehold(await householdResponse.json());
    if (membersResponse.ok) setMembers(await membersResponse.json());
  }

  useEffect(() => { load(); }, []);
  const owner = me?.household?.role === "OWNER";

  async function addMember(event) {
    event.preventDefault(); setError(""); const formEl = event.currentTarget; const form = new FormData(formEl);
    const response = await fetch("/api/v1/household/members", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ displayName: form.get("displayName"), email: form.get("email") }) });
    if (!response.ok) { const body = await response.json().catch(() => ({})); setError(body.error || "Anggota belum dapat ditambahkan."); return; }
    formEl.reset(); await load();
  }

  async function createInvite(memberId) {
    setError(""); setCopied(false); const response = await fetch(`/api/v1/household/members/${memberId}/telegram-invite`, { method: "POST" }); const body = await response.json().catch(() => ({}));
    if (!response.ok) { setError(body.error || "Undangan belum dapat dibuat."); return false; }
    if (!body.link) { setError("Bot Telegram belum dikonfigurasi, tautan undangan belum bisa dibuat."); return false; }
    setInvite({ kind: "telegram", link: body.link, expiresAt: body.expiresAt, memberId }); await load(); return true;
  }

  async function createDashboardInvite(memberId) {
    setError(""); setCopied(false); const response = await fetch(`/api/v1/household/members/${memberId}/dashboard-invite`, { method: "POST" }); const body = await response.json().catch(() => ({}));
    if (!response.ok) { setError(body.error || "Undangan login belum dapat dibuat."); return false; }
    setInvite({ kind: "dashboard", reset: body.purpose === "RESET", link: window.location.origin + body.link, expiresAt: body.expiresAt, memberId }); await load(); return true;
  }

  // 404 means there was nothing pending to cancel, which is the state we want.
  async function revokeInvite(kind, memberId) {
    const response = await fetch(`/api/v1/household/members/${memberId}/${INVITE_PATH[kind]}`, { method: "DELETE" });
    if (!response.ok && response.status !== 404) { setError("Undangan belum dapat dibatalkan."); return false; }
    setInvite(current => (current && current.memberId === memberId && current.kind === kind ? null : current)); await load(); return true;
  }

  // The link is only returned when it is created, so a new link means cancelling the pending one first.
  async function renewInvite(kind, memberId) {
    if (!(await revokeInvite(kind, memberId))) return;
    await (kind === "telegram" ? createInvite(memberId) : createDashboardInvite(memberId));
  }

  async function copyLink() {
    try { await navigator.clipboard.writeText(invite.link); setCopied(true); }
    catch { linkInput.current?.select(); setError("Tautan belum bisa disalin otomatis. Tautan sudah dipilih, tekan Ctrl+C."); }
  }

  // One link both activates a new account and resets a forgotten password.
  function loginInviteControls(member) {
    const pending = member.dashboardInviteStatus === "PENDING" && stillValid(member.dashboardInviteExpiresAt);
    const resetting = member.passwordInitialized;
    return pending
      ? <><button className="secondary" onClick={() => revokeInvite("dashboard", member.id)}>{resetting ? "Batalkan tautan atur ulang" : "Batalkan undangan masuk"}</button><button className="secondary" onClick={() => renewInvite("dashboard", member.id)}>{resetting ? "Buat tautan atur ulang baru" : "Buat tautan masuk baru"}</button></>
      : <button className="secondary" onClick={() => createDashboardInvite(member.id)}>{resetting ? "Atur ulang kata sandi" : "Undang masuk ke Richmod"}</button>;
  }

  function telegramInviteControls(member) {
    return stillValid(member.telegramInviteExpiresAt)
      ? <><button className="secondary" onClick={() => revokeInvite("telegram", member.id)}>Batalkan undangan Telegram</button><button className="secondary" onClick={() => renewInvite("telegram", member.id)}>Buat tautan Telegram baru</button></>
      : <button className="secondary" onClick={() => createInvite(member.id)}>{member.role === "OWNER" ? "Hubungkan Telegram" : "Undang Telegram"}</button>;
  }

  // One plain sentence per invite so an owner can see whether a link is still usable.
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
    const response = await fetch(`/api/v1/household/members/${memberId}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ active: false }) });
    if (!response.ok) { setError("Anggota belum dapat dinonaktifkan."); return; } await load();
  }

  if (!me) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  return <AppShell user={me} eyebrow="KELUARGA" title={household?.name || "Rumah Tangga"} actions={<span className="header-meta">{members.filter(member => member.active).length} anggota aktif</span>}>
    {dialogs}
    <p className="page-intro">Kelola anggota dan hubungkan Telegram tanpa memasukkan ID secara manual.</p>
    {error && <p className="notice error">{error}</p>}
    <div className="admin-stack">
      {owner && <section className="panel"><div className="panel-title"><h2>Tambah anggota</h2><span>Peran anggota</span></div><form className="member-form" onSubmit={addMember}><input aria-label="Nama anggota" name="displayName" placeholder="Nama anggota" maxLength="120" required /><input aria-label="Email anggota" name="email" type="email" placeholder="Email anggota" required /><button>Tambahkan</button></form></section>}
      {invite && <section className="panel" aria-live="polite"><div><span className="eyebrow">{invite.kind === "telegram" ? "UNDANGAN TELEGRAM" : invite.reset ? "ATUR ULANG KATA SANDI" : "UNDANGAN MASUK RICHMOD"}</span><h2>Berlaku sampai {untilLabel(invite.expiresAt)} · sekali pakai</h2><p>{invite.kind === "dashboard" ? invite.reset ? "Kirim tautan ini langsung kepada anggota. Setelah dipakai, kata sandi lama berhenti berlaku dan semua sesi login anggota keluar. Sebelum itu, kata sandi lama masih bisa dipakai." : "Kirim tautan ini langsung kepada anggota yang dituju. Dia membuat kata sandinya sendiri." : members.find(item => item.id === invite.memberId)?.role === "OWNER" ? "Buka tautan ini dari akun Telegram kamu sendiri." : "Kirim tautan ini langsung kepada anggota yang dituju."}</p></div><input ref={linkInput} className="invite-link" readOnly aria-label="Tautan undangan" value={invite.link} onFocus={event => event.currentTarget.select()} /><div className="invite-actions"><button onClick={copyLink}>{copied ? "Tersalin ✓" : "Salin tautan"}</button><a href={invite.link} target="_blank" rel="noreferrer">Buka tautan</a><button className="danger" onClick={() => revokeInvite(invite.kind, invite.memberId)}>Batalkan undangan</button></div><small>Tautan hanya tampil sekarang. Kalau hilang, batalkan lalu buat tautan baru.</small></section>}
      <section className="panel"><div className="panel-title"><h2>Anggota</h2><span>{members.length} akun</span></div><div className="settings-list">{members.map(member => <article key={member.id}><div><b>{member.displayName}</b><small>{member.email}</small></div><span>{roleLabel(member.role)} · {member.telegramConnected ? "Telegram terhubung" : "Telegram belum terhubung"} · {member.active ? "Aktif" : "Nonaktif"}{member.active && inviteState(member) && <small className="invite-state">{inviteState(member)}</small>}</span>{owner && member.role === "MEMBER" && member.active && <div className="row-actions">{loginInviteControls(member)}{!member.telegramConnected && telegramInviteControls(member)}<button className="danger" onClick={() => deactivate(member.id)}>Nonaktifkan</button></div>}{owner && member.role === "OWNER" && member.active && !member.telegramConnected && <div className="row-actions">{telegramInviteControls(member)}</div>}</article>)}</div></section>
    </div>
  </AppShell>;
}
