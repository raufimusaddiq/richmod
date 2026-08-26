"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import { ErrorNotice, Skeleton, Toast } from "../components/Feedback";
import ReviewCards from "../components/ReviewCards";
import useAuth from "../components/useAuth";

export default function ReviewsPage() {
  const user = useAuth(); const [items, setItems] = useState([]); const [categories, setCategories] = useState([]); const [working, setWorking] = useState(""); const [error, setError] = useState(""); const [loading,setLoading]=useState(true); const [toast,setToast]=useState("");
  const load = useCallback(async () => { setLoading(true); try { const [reviewResponse, categoryResponse] = await Promise.all([fetch("/api/v1/reviews"), fetch("/api/v1/categories")]); if (reviewResponse.ok) { setItems(await reviewResponse.json()); setError(""); } else setError("Review Inbox belum dapat dimuat."); if (categoryResponse.ok) setCategories(await categoryResponse.json()); } catch { setError("Koneksi terputus saat memuat review."); } finally { setLoading(false); } }, []);
  useEffect(() => { if (user) load(); }, [user, load]);
  async function action(id, name, body) { setWorking(id); setError(""); const response = await fetch(`/api/v1/reviews/${id}/${name}`, { method: "POST", headers: body ? { "Content-Type": "application/json" } : undefined, body: body ? JSON.stringify(body) : undefined }); if (!response.ok) { const result = await response.json().catch(() => ({})); setError(result.error || "Review belum dapat diperbarui."); } else { await load(); setToast("Review diperbarui dan ledger tetap tersinkron."); } setWorking(""); }
  if (!user) return <main className="loading">Memuat…</main>;
  return <AppShell user={user} eyebrow="REVIEW INBOX" title={`${items.length} keputusan menunggu`} actions={<span className="header-meta">Satu keputusan · satu review object</span>}><ErrorNotice message={error} retry={load}/>{loading ? <Skeleton cards={2}/> : <ReviewCards items={items} categories={categories} working={working} action={action}/>}<Toast message={toast} onClose={() => setToast("")}/></AppShell>;
}
