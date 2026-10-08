"use client";

import { useEffect, useState } from "react";
import { get } from "./shared";
import { useRouter, useSearchParams } from "next/navigation";
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

export default function AdminPage() {
  const router = useRouter(),
    params = useSearchParams(),
    tab = tabs.some(([key]) => key === params.get("tab"))
      ? params.get("tab")
      : "overview";
  const [me, setMe] = useState(null),
    [error, setError] = useState("");
  useEffect(() => {
    get("/api/v1/auth/me")
      .then(setMe)
      .catch(() => {
        location.href = "/";
      });
  }, []);
  const select = (key) => router.replace(`/admin?tab=${key}`);
  if (!me) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  return (
    <AppShell
      user={me}
      eyebrow="ADMINISTRASI PLATFORM"
      title="Konsol platform"
    >
      <p className="page-intro">
        Operasi platform. Data sensitif dan isi finansial tidak ditampilkan.
      </p>
      <nav className="admin-tabs" aria-label="Navigasi admin">
        {tabs.map(([key, label]) => (
          <button
            key={key}
            className={tab === key ? "active" : "secondary"}
            onClick={() => select(key)}
          >
            {label}
          </button>
        ))}
      </nav>
      {error && <p className="notice error">{error}</p>}
      <AdminTab tab={tab} setError={setError} />
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
