"use client";

import { useState } from "react";
import GoogleMark from "../components/GoogleMark";
import { GOOGLE_INVITE_START_PATH, googleStartHref } from "../lib/authErrors";

export default function InvitePage() {
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const edit = setter => event => { setter(event.target.value); setError(""); };

  async function startGoogle() {
    if (busy) return;
    setError("");
    const token = window.location.hash.slice(1).trim();
    if (!token) {
      setError("Tautan atau kata sandi tidak valid.");
      return;
    }
    setBusy(true);
    let leaving = false;
    try {
      const response = await fetch(GOOGLE_INVITE_START_PATH, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ token }) });
      if (response.status === 503) {
        setError("Masuk dengan Google belum tersedia saat ini. Buat kata sandi di bawah.");
        return;
      }
      if (!response.ok) {
        setError("Tautan atau kata sandi tidak valid.");
        return;
      }
      leaving = true;
      window.location.hash = "";
      window.location.href = googleStartHref();
    } catch {
      setError("Koneksi terputus. Periksa jaringan lalu coba lagi.");
    } finally {
      if (!leaving) setBusy(false);
    }
  }

  async function submit(event) {
    event.preventDefault();
    if (busy) return;
    setError("");
    const current = window.location.hash.slice(1);
    if (!current || password !== confirmation) { setError("Tautan atau kata sandi tidak valid."); return; }
    setBusy(true);
    let leaving = false;
    try {
      const response = await fetch("/api/v1/auth/dashboard-invites/accept", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ token: current, password }) });
      if (!response.ok) {
        setError(response.status >= 500 ? "Server sedang bermasalah. Coba lagi sebentar lagi." : "Undangan kedaluwarsa atau kata sandi terlalu lemah.");
        return;
      }
      leaving = true;
      window.location.hash = "";
      window.location.href = "/";
    } catch {
      setError("Koneksi terputus. Periksa jaringan lalu coba lagi.");
    } finally {
      if (!leaving) setBusy(false);
    }
  }

  return <main className="auth-page"><section className="auth-card"><span className="eyebrow">Undangan Richmod</span><h1>Aktifkan akun</h1><p>Aktifkan akun dengan Google, atau buat kata sandi baru minimal 12 karakter.</p><button type="button" className="button public-button-google" disabled={busy} onClick={startGoogle}><GoogleMark/>Aktifkan dengan Google</button><div className="login-divider" role="separator"><span>atau buat kata sandi</span></div><form onSubmit={submit}><input type="password" aria-label="Kata sandi baru" autoComplete="new-password" enterKeyHint="next" minLength="12" value={password} onChange={edit(setPassword)} required/><input type="password" aria-label="Ulangi kata sandi" autoComplete="new-password" enterKeyHint="go" minLength="12" value={confirmation} onChange={edit(setConfirmation)} required/><button disabled={busy}>{busy ? "Mengaktifkan…" : "Aktifkan dan masuk"}</button></form>{error && <p className="notice error" role="alert">{error}</p>}</section></main>;
}
