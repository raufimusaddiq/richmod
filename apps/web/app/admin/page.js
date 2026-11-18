"use client";

import { Suspense, useCallback, useEffect, useState } from "react";
import { get } from "./shared";
import { useRouter, useSearchParams } from "next/navigation";
import { nextTabIndex } from "../lib/focusTrap";
import AppShell from "../components/AppShell";
import Overview from "./Overview";
import Reviews from "./Reviews";
import Jobs from "./Jobs";
import LLM from "./LLM";
import Logs from "./Logs";
import Households from "./Households";
import Users from "./Users";
import Audit from "./Audit";

const tabs = [
  ["overview", "Ringkasan"],
  ["reviews", "Tinjauan"],
  ["jobs", "Tugas"],
  ["llm", "LLM"],
  ["logs", "Log"],
  ["households", "Rumah tangga"],
  ["users", "Pengguna"],
  ["audit", "Audit"],
];

// The static export prerenders this route, and useSearchParams needs a boundary.
export default function AdminPage() {
  return <Suspense fallback={<main className="loading" role="status" aria-live="polite">Memuat…</main>}><AdminConsole/></Suspense>;
}

function AdminConsole() {
  const router = useRouter(),
    params = useSearchParams(),
    tab = tabs.some(([key]) => key === params.get("tab"))
      ? params.get("tab")
      : "overview";
  const [me, setMe] = useState(null),
    [error, setError] = useState("");
  const [meError, setMeError] = useState("");
  const loadMe = useCallback(() => {
    setMeError("");
    get("/api/v1/auth/me")
      .then(setMe)
      .catch((err) => {
        // Only a 401 means signed out; a network blip must not log the admin out.
        if (err?.status === 401) location.href = "/";
        else setMeError("Sesi belum dapat diperiksa. Coba lagi.");
      });
  }, []);
  useEffect(() => { loadMe(); }, [loadMe]);
  useEffect(() => { setError(""); }, [tab]);
  // A banner belongs to the tab that raised it.
  const select = (key) => { setError(""); router.replace(`/admin?tab=${key}`); };
  // Roving tablist: arrows/Home/End move the selection and the focus together.
  const tabKeys = (event, index) => {
    const next = nextTabIndex(event.key, index, tabs.length);
    if (next === null) return;
    event.preventDefault();
    const buttons = event.currentTarget.parentElement?.querySelectorAll('[role="tab"]');
    select(tabs[next][0]);
    buttons?.[next]?.focus();
  };
  if (!me) return meError
    ? <main className="loading" role="alert">{meError} <button className="secondary" onClick={loadMe}>Coba lagi</button></main>
    : <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  return (
    <AppShell
      user={me}
      eyebrow="Administrasi platform"
      title="Konsol platform"
    >
      <p className="page-intro">
        Operasi platform. Data sensitif dan isi finansial tidak ditampilkan.
      </p>
      <nav aria-label="Navigasi admin">
        <div className="admin-tabs" role="tablist" aria-label="Navigasi admin">
          {tabs.map(([key, label], index) => (
            <button
              key={key}
              role="tab"
              id={`admin-tab-${key}`}
              aria-controls="admin-panel"
              aria-selected={tab === key}
              tabIndex={tab === key ? 0 : -1}
              className={tab === key ? "active" : "secondary"}
              onKeyDown={(event) => tabKeys(event, index)}
              onClick={() => select(key)}
            >
              {label}
            </button>
          ))}
        </div>
      </nav>
      {error && <p className="notice error" role="alert">{error}</p>}
      <div role="tabpanel" id="admin-panel" aria-labelledby={`admin-tab-${tab}`}>
        <AdminTab tab={tab} setError={setError} />
      </div>
    </AppShell>
  );
}

function AdminTab({ tab, setError }) {
  if (tab === "overview") return <Overview setError={setError} />;
  if (tab === "reviews") return <Reviews setError={setError} />;
  if (tab === "jobs") return <Jobs setError={setError} />;
  if (tab === "llm") return <LLM setError={setError} />;
  if (tab === "logs") return <Logs setError={setError} />;
  if (tab === "households") return <Households setError={setError} />;
  if (tab === "users") return <Users setError={setError} />;
  return <Audit setError={setError} />;
}
