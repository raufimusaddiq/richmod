"use client";

import { useEffect, useState } from "react";

export default function HouseholdPage() {
  const [household, setHousehold] = useState(null);
  const [members, setMembers] = useState([]);
  const [me, setMe] = useState(null);
  const [invite, setInvite] = useState(null);
  const [error, setError] = useState("");

  async function load() {
    const [meResponse, householdResponse, membersResponse] = await Promise.all([fetch("/api/v1/auth/me"), fetch("/api/v1/household"), fetch("/api/v1/household/members")]);
    if (!meResponse.ok) { window.location.href = "/"; return; }
    setMe(await meResponse.json());
    if (householdResponse.ok) setHousehold(await householdResponse.json());
    if (membersResponse.ok) setMembers(await membersResponse.json());
  }

  useEffect(() => { load(); }, []);
  const owner = me?.memberships?.[0]?.role === "OWNER";

  async function addMember(event) {
    event.preventDefault(); setError(""); const form = new FormData(event.currentTarget);
    const response = await fetch("/api/v1/household/members", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ displayName: form.get("displayName"), email: form.get("email") }) });
    if (!response.ok) { const body = await response.json().catch(() => ({})); setError(body.error || "Anggota belum dapat ditambahkan."); return; }
    event.currentTarget.reset(); await load();
  }

  async function createInvite(memberId) {
    setError(""); const response = await fetch(`/api/v1/household/members/${memberId}/telegram-invite`, { method: "POST" }); const body = await response.json().catch(() => ({}));
    if (!response.ok) { setError(body.error || "Undangan belum dapat dibuat."); return; } setInvite({ ...body, memberId });
  }

  async function revokeInvite(memberId) {
    const response = await fetch(`/api/v1/household/members/${memberId}/telegram-invite`, { method: "DELETE" });
    if (!response.ok) { setError("Undangan belum dapat dicabut."); return; } setInvite(null);
  }

  async function deactivate(memberId) {
    if (!window.confirm("Nonaktifkan anggota ini? Riwayat transaksi tetap disimpan.")) return;
    const response = await fetch(`/api/v1/household/members/${memberId}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ active: false }) });
    if (!response.ok) { setError("Anggota belum dapat dinonaktifkan."); return; } await load();
  }

  return <main className="shell household-page">
    <header><div><span className="eyebrow">HOUSEHOLD</span><h1>{household?.name || "Rumah tangga"}</h1><p>Kelola anggota dan hubungkan Telegram tanpa memasukkan ID secara manual.</p></div><a className="back-link" href="/">← Ringkasan</a></header>
    {error && <p className="notice error">{error}</p>}
    {owner && <section className="panel"><div className="panel-title"><h2>Tambah anggota</h2><span>Role MEMBER</span></div><form className="member-form" onSubmit={addMember}><input name="displayName" placeholder="Nama anggota" maxLength="120" required /><input name="email" type="email" placeholder="Email anggota" required /><button>Tambahkan</button></form></section>}
    {invite && <section className="panel invite-panel"><div><span className="eyebrow">UNDANGAN TELEGRAM</span><h2>Berlaku 15 menit dan hanya sekali pakai</h2><p>Kirim tautan ini langsung kepada anggota yang dituju.</p></div><div className="invite-actions"><a href={invite.link} target="_blank" rel="noreferrer">Buka undangan Telegram</a><button className="danger" onClick={() => revokeInvite(invite.memberId)}>Cabut</button></div></section>}
    <section className="panel"><div className="panel-title"><h2>Anggota</h2><span>{members.length} akun</span></div><div className="member-list">{members.map(member => <article key={member.id}><div><b>{member.displayName}</b><small>{member.email}</small></div><span className="role">{member.role}</span><span className={member.telegramConnected ? "connected" : "muted"}>{member.telegramConnected ? "Telegram terhubung" : "Telegram belum terhubung"}</span><span>{member.active ? "Aktif" : "Nonaktif"}</span>{owner && member.role === "MEMBER" && member.active && <div className="member-actions">{!member.telegramConnected && <button onClick={() => createInvite(member.id)}>Undang Telegram</button>}<button className="danger" onClick={() => deactivate(member.id)}>Nonaktifkan</button></div>}</article>)}</div></section>
  </main>;
}
