"use client";

import { useEffect, useState } from "react";

export default function SettingsPage() {
  const [me, setMe] = useState(null);
  const [rules, setRules] = useState([]);
  const [working, setWorking] = useState("");
  const [error, setError] = useState("");

  async function load() {
    const [meResponse, rulesResponse] = await Promise.all([
      fetch("/api/v1/auth/me"),
      fetch("/api/v1/merchant-aliases"),
    ]);
    if (!meResponse.ok) {
      window.location.href = "/";
      return;
    }
    setMe(await meResponse.json());
    if (!rulesResponse.ok) {
      const body = await rulesResponse.json().catch(() => ({}));
      setError(body.error || "Aturan merchant belum dapat dimuat.");
      return;
    }
    setRules(await rulesResponse.json());
  }

  useEffect(() => { load(); }, []);
  const owner = me?.memberships?.[0]?.role === "OWNER";

  async function setRule(rule, autoApply) {
    setWorking(rule.id);
    setError("");
    const response = await fetch(`/api/v1/merchant-aliases/${rule.id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ autoApply }),
    });
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      setError(body.error || "Aturan merchant belum dapat diubah.");
    } else {
      await load();
    }
    setWorking("");
  }

  return <main className="shell settings-page">
    <header><div><span className="eyebrow">SETTINGS</span><h1>Aturan merchant</h1><p>Aturan hanya dibuat setelah konfirmasi eksplisit dan berlaku untuk household ini.</p></div><a className="back-link" href="/">← Ringkasan</a></header>
    {error && <p className="notice error">{error}</p>}
    <section className="panel"><div className="panel-title"><h2>Kategori otomatis</h2><span>{rules.filter(rule => rule.autoApply).length} aktif</span></div>
      <div className="merchant-rule-list">{rules.map(rule => <article key={rule.id}><div><b>{rule.normalizedName}</b><small>Alias: {rule.rawName}</small></div><div><span>{rule.defaultCategoryName || "Tanpa kategori"}</span><small>{rule.autoApply ? "Diterapkan otomatis" : "Dinonaktifkan"}</small></div>{owner && <button className={rule.autoApply ? "danger" : ""} disabled={working === rule.id} onClick={() => setRule(rule, !rule.autoApply)}>{rule.autoApply ? "Nonaktifkan" : "Aktifkan"}</button>}</article>)}{rules.length === 0 && <p className="empty">Belum ada aturan merchant. Konfirmasi transaksi lalu pilih “ingat merchant” untuk membuatnya.</p>}</div>
    </section>
  </main>;
}
