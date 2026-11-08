"use client";

import { useState } from "react";

export default function InvitePage() {
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  // Editing a field retires the message that was about the previous attempt.
  const edit = setter => event => { setter(event.target.value); setError(""); };

  async function submit(event) {
    event.preventDefault();
    if (busy) return;
    setError("");
    const token = window.location.hash.slice(1);
    if (!token || password !== confirmation) { setError("Tautan atau kata sandi tidak valid."); return; }
    setBusy(true);
    let leaving = false;
    try {
      const response = await fetch("/api/v1/auth/dashboard-invites/accept", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ token, password }) });
      if (!response.ok) {
        setError(response.status >= 500 ? "Server sedang bermasalah. Coba lagi sebentar lagi." : "Undangan kedaluwarsa atau kata sandi terlalu lemah.");
        return;
      }
      leaving = true; // stay busy while the browser navigates away
      window.location.hash = "";
      window.location.href = "/";
    } catch {
      setError("Koneksi terputus. Periksa jaringan lalu coba lagi.");
    } finally {
      if (!leaving) setBusy(false);
    }
  }

  return <main className="auth-page"><section className="auth-card"><span className="eyebrow">UNDANGAN RICHMOD</span><h1>Atur kata sandi</h1><p>Buat kata sandi baru, minimal 12 karakter.</p><form onSubmit={submit}><input type="password" aria-label="Kata sandi baru" autoComplete="new-password" minLength="12" value={password} onChange={edit(setPassword)} required/><input type="password" aria-label="Ulangi kata sandi" autoComplete="new-password" minLength="12" value={confirmation} onChange={edit(setConfirmation)} required/><button disabled={busy}>{busy ? "Mengaktifkan…" : "Aktifkan dan masuk"}</button></form>{error && <p className="notice error" role="alert">{error}</p>}</section></main>;
}
