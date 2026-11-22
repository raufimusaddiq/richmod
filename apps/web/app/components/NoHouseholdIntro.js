"use client";

import Link from "next/link";
import { useState } from "react";
import { ArrowClockwise, ArrowRight, Check, FileText, Lightning, ShieldChevron, SignOut, TelegramLogo, WarningCircle } from "@phosphor-icons/react";
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

  return <main className="public-shell login-shell no-household-shell">
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
    <div className="public-content">
      <section className="workflow-section" id="cara-kerja" aria-labelledby="intro-workflow-title">
        <div className="section-heading"><span className="public-kicker">CARA KERJA</span><h2 id="intro-workflow-title">Dari bukti menjadi keputusan yang bisa ditinjau.</h2></div>
        <div className="workflow-rail">
          <article className="workflow-step"><span className="workflow-number">01</span><div className="workflow-icon"><TelegramLogo aria-hidden="true"/></div><h3>Bukti masuk</h3><p>Email bank, pesan Telegram, atau dokumen yang kamu kirim.</p></article>
          <ArrowRight className="workflow-arrow" aria-hidden="true"/>
          <article className="workflow-step"><span className="workflow-number">02</span><div className="workflow-icon"><Lightning aria-hidden="true"/></div><h3>Richmod memahami</h3><p>Model bahasa membaca konteks. Richmod memeriksa hasilnya dengan aturan yang konsisten.</p></article>
          <ArrowRight className="workflow-arrow" aria-hidden="true"/>
          <article className="workflow-step"><span className="workflow-number">03</span><div className="workflow-icon"><Check aria-hidden="true"/></div><h3>Ledger tercatat</h3><p>Yang cukup jelas masuk ke riwayat keluarga dengan sumbernya.</p></article>
        </div>
        <div className="workflow-review"><WarningCircle aria-hidden="true"/><div><strong>Belum cukup jelas?</strong><span>Richmod mengirimkannya ke Kotak Tinjauan—keputusan tetap di tangan kamu.</span></div></div>
      </section>

      <section className="trust-section" id="kepercayaan" aria-labelledby="intro-trust-title">
        <div className="trust-copy">
          <span className="public-kicker">AI MEMBANTU. SISTEM MEMUTUSKAN.</span>
          <h2 id="intro-trust-title">Otomasi yang tahu kapan harus berhenti.</h2>
          <p>Model bahasa membantu membaca input yang tidak terstruktur. Ia tidak punya akses untuk mengubah ledger. Richmod memeriksa hasilnya; hal yang ambigu menunggu keputusan manusia.</p>
          <Link className="public-text-link" href="/privacy">Baca tentang privasi <ArrowRight aria-hidden="true"/></Link>
        </div>
        <div className="evidence-stack" aria-label="Hubungan bukti, interpretasi, dan ledger">
          <div className="stack-item"><FileText aria-hidden="true"/><div><span>Bukti asli</span><strong>Bank Jago · email transaksi</strong><small>6 Sep 2026 · 09:20 WIB</small></div></div>
          <div className="stack-connector" aria-hidden="true">↓</div>
          <div className="stack-item"><Lightning aria-hidden="true"/><div><span>Interpretasi tervalidasi</span><strong>Pasar Minggu · Rp185.000</strong><small>Pengeluaran · Belanja rumah</small></div></div>
          <div className="stack-connector" aria-hidden="true">↓</div>
          <div className="stack-item stack-item-final"><Check aria-hidden="true"/><div><span>Ledger keluarga</span><strong>Tercatat dan dapat ditelusuri</strong><small>Sumber dan status tetap terlihat</small></div></div>
        </div>
      </section>
    </div>
    <footer className="login-footer"><span>Richmod · Keuangan keluarga, tanpa menebak.</span><Link href="/privacy">Privasi</Link><Link href="/terms">Ketentuan</Link></footer>
  </main>;
}
