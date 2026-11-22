"use client";

import Link from "next/link";
import { useState } from "react";
import { ArrowClockwise, ShieldChevron, SignOut } from "@phosphor-icons/react";
import { PublicNav } from "./PublicShell";
import { useAuthContext } from "./AuthProvider";

export default function NoHouseholdIntro({ user }) {
  const { reload } = useAuthContext();
  const [checking, setChecking] = useState(false);
  const [leaving, setLeaving] = useState(false);

  async function check() {
    setChecking(true);
    try { await reload(); } finally { setChecking(false); }
  }

  async function logout() {
    setLeaving(true);
    try { await fetch("/api/v1/auth/logout", { method: "POST" }); } finally { window.location.href = "/"; }
  }

  return <main className="login-shell no-household-shell">
    <PublicNav hideLogin />
    <div className="no-household-layout">
      <section className="no-household-card" aria-labelledby="no-household-title">
        <span className="public-kicker">RUANG KELUARGA BELUM AKTIF</span>
        <h1 id="no-household-title">Akun kamu sudah terdaftar.</h1>
        <dl className="no-household-identity">
          <div><dt>Nama</dt><dd>{user.displayName}</dd></div>
          <div><dt>Email</dt><dd>{user.email}</dd></div>
        </dl>
        <p>Richmod mencatat keuangan per keluarga. Ruang keluarga baru diaktifkan oleh administrator Richmod. Kalau kamu diundang ke keluarga yang sudah ada, buka tautan <code>/invite#…</code> yang dibagikan administrator keluargamu.</p>
        <p className="no-household-flow">Setelah aktif, catatan pengeluaran dan pemasukan, siklus bulanan, dan posisi kekayaan tersimpan dalam satu buku keluarga.</p>
        <div className="no-household-actions">
          <button type="button" className="public-button-primary" onClick={check} disabled={checking} aria-busy={checking}><ArrowClockwise aria-hidden="true"/>{checking ? "Memeriksa…" : "Periksa status"}</button>
          {user.isSuperAdmin && <Link className="button secondary" href="/admin"><ShieldChevron aria-hidden="true"/>Buka Konsol Admin</Link>}
          <button type="button" className="secondary" onClick={logout} disabled={leaving}><SignOut aria-hidden="true"/>Keluar</button>
        </div>
      </section>
    </div>
  </main>;
}
