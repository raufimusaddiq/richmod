"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import { ErrorNotice, Skeleton, Toast } from "../components/Feedback";
import ReviewCards from "../components/ReviewCards";
import useAuth from "../components/useAuth";

export default function ReviewsPage() {
  const user = useAuth(); const [items, setItems] = useState([]); const [categories, setCategories] = useState([]); const [working, setWorking] = useState(""); const [error, setError] = useState(""); const [loading,setLoading]=useState(true); const [toast,setToast]=useState("");
  const load = useCallback(async () => { setLoading(true); try { const [reviewResponse, categoryResponse] = await Promise.all([fetch("/api/v1/reviews"), fetch("/api/v1/categories")]); if (reviewResponse.ok) { setItems(await reviewResponse.json()); setError(""); } else setError("Daftar Perlu Ditinjau belum dapat dimuat."); if (categoryResponse.ok) setCategories(await categoryResponse.json()); } catch { setError("Koneksi terputus saat memuat daftar tinjauan."); } finally { setLoading(false); } }, []);
  useEffect(() => { if (user) load(); }, [user, load]);
  async function action(id, name, body) { setWorking(id); setError(""); const canonical = name === "resolve"; const response = await fetch(canonical ? `/api/v1/reviews/${id}/resolve` : `/api/v1/reviews/${id}/${name}`, { method: "POST", headers: body ? { "Content-Type": "application/json" } : undefined, body: body ? JSON.stringify(body) : undefined }); if (!response.ok) { const result = await response.json().catch(() => ({})); setError(result.error || "Tinjauan belum dapat diperbarui."); } else { await load(); setToast("Tinjauan diperbarui dan buku keuangan tetap tersinkron."); } setWorking(""); }
  if (!user) return <main className="loading">Memuat…</main>;
  return <AppShell user={user} eyebrow="PERLU DITINJAU" title={`${items.length} keputusan menunggu`} actions={<span className="header-meta">Satu keputusan · satu tinjauan</span>}><ErrorNotice message={error} retry={load}/>{loading ? <Skeleton cards={2}/> : <ReviewCards items={items} categories={categories} working={working} action={action}/>}<Toast message={toast} onClose={() => setToast("")}/></AppShell>;
}
