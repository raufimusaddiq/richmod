"use client";

import { useEffect, useRef, useState } from "react";
import AppShell from "../components/AppShell";
import { ErrorNotice } from "../components/Feedback";
import useDialogs from "../components/useDialogs";
import { LoadError, responseError, useBusyMap, useGuardedLoad } from "../lib/useLoadState";

const roleLabel = (role) => ({ OWNER: "Administrator", MEMBER: "Anggota" })[role] || role;
const untilLabel = iso => new Date(iso).toLocaleString("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }) + " WIB";
const TICK_MS = 15000;
const INVITE_PATH = { telegram: "telegram-invite", dashboard: "dashboard-invite" };

export default function HouseholdPage() {
  const { confirm, dialogs } = useDialogs();
  const [household, setHousehold] = useState(null);
  // null means "not loaded", never "no members": a failed request must not read as 0 akun.
  const [membersState, setMembers] = useState(null);
  const members = membersState || [];
  const [me, setMe] = useState(null);
  const [invite, setInvite] = useState(null);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const { busy, run: runBusy } = useBusyMap();
  const linkInput = useRef(null);
  // Expiry is time, not state: tick so an invite that lapses while the page is
  // open stops looking valid and offers "create" instead of "cancel".
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), TICK_MS); return () => window.clearInterval(timer); }, []);
  const stillValid = iso => Boolean(iso) && new Date(iso).getTime() > now;

  const loader = useGuardedLoad(async ({ signal, current }) => {
    const [meResponse, householdResponse, membersResponse] = await Promise.all(["/api/v1/auth/me", "/api/v1/household", "/api/v1/household/members"].map(url => fetch(url, { signal })));
    // Only a 401 means signed out; a 5xx or dropped request keeps the session.
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
  const owner = me?.household?.role === "OWNER";

  // Every handler clears the previous error, holds a per-key busy flag, and
  // releases it in finally, so a dropped request can neither stick a button nor
  // leave an old message on screen.
  function addMember(event) {
    event.preventDefault(); const formEl = event.currentTarget; const form = new FormData(formEl);
    return runBusy("add-member", async () => {
      setError("");
      try {
        const response = await fetch("/api/v1/household/members", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ displayName: form.get("displayName"), email: form.get("email") }) });
        if (!response.ok) { setError(await responseError(response, "Anggota belum dapat ditambahkan.")); return; }
        formEl.reset(); await load();
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
      setInvite(build(body)); await load(); return true;
    } catch { setError("Koneksi terputus. Undangan belum dibuat."); return false; }
  }

  const createInvite = memberId => runBusy(`member:${memberId}`, () => issueInvite(memberId, "telegram", "Undangan belum dapat dibuat.", body => ({ kind: "telegram", link: body.link, expiresAt: body.expiresAt, memberId })));
  const createDashboardInvite = memberId => runBusy(`member:${memberId}`, () => issueInvite(memberId, "dashboard", "Undangan login belum dapat dibuat.", body => ({ kind: "dashboard", reset: body.purpose === "RESET", link: window.location.origin + body.link, expiresAt: body.expiresAt, memberId })));

  // 404 means there was nothing pending to cancel, which is the state we want.
  async function cancelInvite(kind, memberId) {
    setError("");
    try {
      const response = await fetch(`/api/v1/household/members/${memberId}/${INVITE_PATH[kind]}`, { method: "DELETE" });
      if (!response.ok && response.status !== 404) { setError("Undangan belum dapat dibatalkan."); return false; }
      setInvite(current => (current && current.memberId === memberId && current.kind === kind ? null : current)); await load(); return true;
    } catch { setError("Koneksi terputus. Undangan belum dibatalkan."); return false; }
  }
  const revokeInvite = (kind, memberId) => runBusy(`member:${memberId}`, () => cancelInvite(kind, memberId));

  // The link is only returned when it is created, so a new link means cancelling the pending one first.
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

  // One link both activates a new account and resets a forgotten password.
  function loginInviteControls(member) {
    const pending = member.dashboardInviteStatus === "PENDING" && stillValid(member.dashboardInviteExpiresAt);
    const resetting = member.passwordInitialized;
    return pending
      ? <><button className="secondary" disabled={Boolean(busy[`member:${member.id}`])} onClick={() => revokeInvite("dashboard", member.id)}>{resetting ? "Batalkan tautan atur ulang" : "Batalkan undangan masuk"}</button><button className="secondary" disabled={Boolean(busy[`member:${member.id}`])} onClick={() => renewInvite("dashboard", member.id)}>{resetting ? "Buat tautan atur ulang baru" : "Buat tautan masuk baru"}</button></>
      : <button className="secondary" disabled={Boolean(busy[`member:${member.id}`])} onClick={() => createDashboardInvite(member.id)}>{resetting ? "Atur ulang kata sandi" : "Undang masuk ke Richmod"}</button>;
  }

  function telegramInviteControls(member) {
    return stillValid(member.telegramInviteExpiresAt)
      ? <><button className="secondary" disabled={Boolean(busy[`member:${member.id}`])} onClick={() => revokeInvite("telegram", member.id)}>Batalkan undangan Telegram</button><button className="secondary" disabled={Boolean(busy[`member:${member.id}`])} onClick={() => renewInvite("telegram", member.id)}>Buat tautan Telegram baru</button></>
      : <button className="secondary" disabled={Boolean(busy[`member:${member.id}`])} onClick={() => createInvite(member.id)}>{member.role === "OWNER" ? "Hubungkan Telegram" : "Undang Telegram"}</button>;
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
    return runBusy(`member:${memberId}`, async () => {
      setError("");
      try {
        const response = await fetch(`/api/v1/household/members/${memberId}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ active: false }) });
        if (!response.ok) { setError(await responseError(response, "Anggota belum dapat dinonaktifkan.")); return; }
        // The deactivated member's one-time link is dead: take its panel down.
        setInvite(current => (current?.memberId === memberId ? null : current));
        await load();
      } catch { setError("Koneksi terputus. Anggota belum dinonaktifkan."); }
    });
  }

  if (!me) return loader.failed
    ? <main className="loading" role="alert">{loader.error} <button className="secondary" onClick={() => load()}>Coba lagi</button></main>
    : <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  return <AppShell user={me} eyebrow="KELUARGA" title={household?.name || "Rumah Tangga"} actions={<span className="header-meta">{membersState ? `${members.filter(member => member.active).length} anggota aktif` : "Anggota belum termuat"}</span>}>
    {dialogs}
    <p className="page-intro">Kelola anggota dan hubungkan Telegram tanpa memasukkan ID secara manual.</p>
    <ErrorNotice message={error || loader.error} retry={error ? undefined : () => load()}/>
    <div className="admin-stack">
      {owner && <section className="panel"><div className="panel-title"><h2>Tambah anggota</h2><span>Peran anggota</span></div><form className="member-form" onSubmit={addMember}><input aria-label="Nama anggota" name="displayName" placeholder="Nama anggota" maxLength="120" required /><input aria-label="Email anggota" name="email" type="email" placeholder="Email anggota" required /><button disabled={Boolean(busy["add-member"])}>Tambahkan</button></form></section>}
      {invite && <section className="panel" aria-live="polite"><div><span className="eyebrow">{invite.kind === "telegram" ? "UNDANGAN TELEGRAM" : invite.reset ? "ATUR ULANG KATA SANDI" : "UNDANGAN MASUK RICHMOD"}</span><h2>{stillValid(invite.expiresAt) ? `Berlaku sampai ${untilLabel(invite.expiresAt)} · sekali pakai` : "Tautan ini sudah kedaluwarsa · buat tautan baru"}</h2><p>{invite.kind === "dashboard" ? invite.reset ? "Kirim tautan ini langsung kepada anggota. Setelah dipakai, kata sandi lama berhenti berlaku dan semua sesi login anggota keluar. Sebelum itu, kata sandi lama masih bisa dipakai." : "Kirim tautan ini langsung kepada anggota yang dituju. Dia membuat kata sandinya sendiri." : members.find(item => item.id === invite.memberId)?.role === "OWNER" ? "Buka tautan ini dari akun Telegram kamu sendiri." : "Kirim tautan ini langsung kepada anggota yang dituju."}</p></div><input ref={linkInput} className="invite-link" readOnly aria-label="Tautan undangan" value={invite.link} onFocus={event => event.currentTarget.select()} /><div className="invite-actions"><button onClick={copyLink}>{copied ? "Tersalin ✓" : "Salin tautan"}</button><a href={invite.link} target="_blank" rel="noreferrer">Buka tautan</a><button className="danger" disabled={Boolean(busy[`member:${invite.memberId}`])} onClick={() => revokeInvite(invite.kind, invite.memberId)}>Batalkan undangan</button></div><small>Tautan hanya tampil sekarang. Kalau hilang, batalkan lalu buat tautan baru.</small></section>}
      <section className="panel"><div className="panel-title"><h2>Anggota</h2><span>{membersState ? `${members.length} akun` : "—"}</span></div>{!membersState && <p className="muted">Daftar anggota belum termuat.</p>}<div className="settings-list">{members.map(member => <article key={member.id}><div><b>{member.displayName}</b><small>{member.email}</small></div><span>{roleLabel(member.role)} · {member.telegramConnected ? "Telegram terhubung" : "Telegram belum terhubung"} · {member.active ? "Aktif" : "Nonaktif"}{member.active && inviteState(member) && <small className="invite-state">{inviteState(member)}</small>}</span>{owner && member.role === "MEMBER" && member.active && <div className="row-actions">{loginInviteControls(member)}{!member.telegramConnected && telegramInviteControls(member)}<button className="danger" disabled={Boolean(busy[`member:${member.id}`])} onClick={() => deactivate(member.id)}>Nonaktifkan</button></div>}{owner && member.role === "OWNER" && member.active && !member.telegramConnected && <div className="row-actions">{telegramInviteControls(member)}</div>}</article>)}</div></section>
    </div>
  </AppShell>;
}
