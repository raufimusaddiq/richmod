"use client";

import Link from "next/link";
import { ArrowLeft, LockKey } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import useAuth from "../components/useAuth";
import { PublicNav, PublicTrustNote } from "../components/PublicShell";

export default function LoginPage() {
  const user = useAuth(false);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => { if (user && user !== false) window.location.replace("/"); }, [user]);

  if (user === null || (user && user !== false)) return <main className="login-shell"><div className="login-loading">Memuat Richmod…</div></main>;

  async function login(event) {
    event.preventDefault(); setError(""); setSubmitting(true);
    try {
      const form = new FormData(event.currentTarget);
      const response = await fetch("/api/v1/auth/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ email: form.get("email"), password: form.get("password") }) });
      if (!response.ok) { setError("Email atau kata sandi tidak cocok."); return; }
      window.location.replace("/");
    } catch { setError("Koneksi terputus. Coba lagi."); } finally { setSubmitting(false); }
  }

  return <main className="login-shell"><PublicNav /><div className="login-layout"><section className="login-context"><Link className="login-back" href="/"><ArrowLeft aria-hidden="true"/> Kembali ke beranda</Link><span className="public-kicker">RUANG KEUANGAN KELUARGA</span><h1>Masuk ke Richmod.</h1><p>Riwayat uang keluarga kamu, dalam satu tempat yang bisa dijelaskan.</p><PublicTrustNote /></section><section className="login-card" aria-labelledby="login-title"><div className="login-card-mark"><LockKey aria-hidden="true"/></div><span className="public-kicker">AKUN RICHMOD</span><h2 id="login-title">Lanjutkan ke ruangmu.</h2><p className="login-card-copy">Gunakan email dan kata sandi yang sudah terdaftar.</p><form onSubmit={login} noValidate><label htmlFor="login-email">Email</label><input id="login-email" name="email" type="email" autoComplete="email" required aria-describedby={error ? "login-error" : undefined}/><label htmlFor="login-password">Kata sandi</label><input id="login-password" name="password" type="password" autoComplete="current-password" required aria-describedby={error ? "login-error" : undefined}/>{error && <p className="login-error" id="login-error" role="alert">{error}</p>}<button className="public-button-primary" type="submit" disabled={submitting}>{submitting ? "Memeriksa…" : "Masuk ke Richmod"}</button></form><small className="login-card-foot">IDR · Asia/Jakarta</small></section></div><footer className="login-footer"><span>Richmod · Keuangan keluarga, tanpa menebak.</span><Link href="/privacy">Privasi</Link><Link href="/terms">Ketentuan</Link></footer></main>;
}
