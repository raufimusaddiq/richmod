"use client";

import Link from "next/link";
import { ArrowLeft, LockKey } from "@phosphor-icons/react";
import { useEffect, useRef, useState } from "react";
import LoadingShell from "../components/LoadingShell";
import useAuth from "../components/useAuth";
import { PublicNav, PublicTrustNote } from "../components/PublicShell";
import GoogleMark from "../components/GoogleMark";
import { googleLoginErrorMessage, googleStartHref } from "../lib/authErrors";

export default function LoginPage() {
  const user = useAuth(false);
  const passwordRef = useRef(null);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [googleError, setGoogleError] = useState("");

  useEffect(() => { setGoogleError(googleLoginErrorMessage(new URLSearchParams(window.location.search).get("error"))); }, []);

  useEffect(() => { if (user && user !== false) window.location.replace("/"); }, [user]);

  if (user === null || (user && user !== false)) return <LoadingShell variant="login" />;

  async function login(event) {
    event.preventDefault(); setError(""); setSubmitting(true);
    try {
      const form = new FormData(event.currentTarget);
      const response = await fetch("/api/v1/auth/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ email: form.get("email"), password: form.get("password") }) });
      if (!response.ok) { setError("Email atau kata sandi tidak cocok."); return; }
      window.location.replace("/");
    } catch { setError("Koneksi terputus. Coba lagi."); } finally { setSubmitting(false); }
  }

  return <main className="login-shell"><PublicNav hideLogin /><div className="login-layout"><section className="login-context"><Link className="login-back" href="/"><ArrowLeft aria-hidden="true"/> Kembali ke beranda</Link><span className="public-kicker">RUANG KEUANGAN KELUARGA</span><h1>Masuk ke Richmod.</h1><p>Riwayat uang keluarga kamu, dalam satu tempat yang bisa dijelaskan.</p><PublicTrustNote /><img className="login-treeline" src="/brand/richmod-login-treeline.svg" alt="" aria-hidden="true" /></section><section className="login-card" aria-labelledby="login-title"><div className="login-card-mark"><LockKey aria-hidden="true"/></div><span className="public-kicker">AKUN RICHMOD</span><h2 id="login-title">Lanjutkan ke ruangmu.</h2><p className="login-card-copy">Masuk dengan Google atau gunakan email dan kata sandi yang sudah terdaftar.</p>{googleError && <p className="login-error" id="google-error" role="alert">{googleError}</p>}<a className="button public-button-google" href={googleStartHref()} aria-describedby={googleError ? "google-error" : undefined}><GoogleMark/>Masuk dengan Google</a><div className="login-divider" role="separator"><span>atau dengan email</span></div><form onSubmit={login} noValidate><label htmlFor="login-email">Email</label><input id="login-email" name="email" type="email" autoComplete="email" enterKeyHint="next" onKeyDown={event => { if (event.key === "Enter" && !passwordRef.current?.value) { event.preventDefault(); passwordRef.current?.focus(); } }} required aria-describedby={error ? "login-error" : undefined}/><label htmlFor="login-password">Kata sandi</label><input ref={passwordRef} id="login-password" name="password" type="password" autoComplete="current-password" enterKeyHint="go" required aria-describedby={error ? "login-error" : undefined}/>{error && <p className="login-error" id="login-error" role="alert">{error}</p>}<button className="public-button-primary" type="submit" disabled={submitting}>{submitting ? "Memeriksa…" : "Masuk ke Richmod"}</button></form><small className="login-card-foot">IDR · Asia/Jakarta</small></section></div><footer className="login-footer"><span>Richmod · Keuangan keluarga, tanpa menebak.</span><Link href="/privacy">Privasi</Link><Link href="/terms">Ketentuan</Link></footer></main>;
}
