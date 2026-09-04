"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import { ErrorNotice, Skeleton, Toast } from "../components/Feedback";
import ReviewCards from "../components/ReviewCards";
import useAuth from "../components/useAuth";

const currentView = () => typeof window !== "undefined" && new URLSearchParams(window.location.search).get("view") === "actions" ? "actions" : "transactions";

export default function InboxPage() {
  const user = useAuth();
  const [view, setView] = useState("transactions");
  const [reviews, setReviews] = useState([]), [actions, setActions] = useState([]), [categories, setCategories] = useState([]);
  const [loading, setLoading] = useState(true), [error, setError] = useState(""), [working, setWorking] = useState(""), [toast, setToast] = useState("");
  const owner = user?.memberships?.[0]?.role === "OWNER";
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [reviewResponse, actionResponse, categoryResponse] = await Promise.all([fetch("/api/v1/reviews"), fetch("/api/v1/integration-actions"), fetch("/api/v1/categories")]);
      if (!reviewResponse.ok || !actionResponse.ok) throw new Error();
      const [reviewItems, actionItems] = await Promise.all([reviewResponse.json(), actionResponse.json()]);
      setReviews(Array.isArray(reviewItems) ? reviewItems : []);
      setActions(Array.isArray(actionItems) ? actionItems : []);
      if (categoryResponse.ok) setCategories(await categoryResponse.json());
      setError("");
    } catch { setError("Inbox belum dapat dimuat."); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { setView(currentView()); const sync = () => setView(currentView()); window.addEventListener("popstate", sync); return () => window.removeEventListener("popstate", sync); }, []);
  useEffect(() => { if (user) load(); }, [user, load]);
  function selectView(next) { setView(next); window.history.pushState({}, "", `/inbox?view=${next}`); }
  async function reviewAction(id, name, body) { setWorking(id); setError(""); const canonical = name === "resolve"; const response = await fetch(canonical ? `/api/v1/reviews/${id}/resolve` : `/api/v1/reviews/${id}/${name}`, { method: "POST", headers: body ? { "Content-Type": "application/json" } : undefined, body: body ? JSON.stringify(body) : undefined }); if (!response.ok) { const result = await response.json().catch(() => ({})); setError(result.error || "Tinjauan belum dapat diperbarui."); } else { await load(); setToast("Tinjauan diperbarui dan buku keuangan tetap tersinkron."); } setWorking(""); }
  async function resolveAction(id) { setWorking(id); const response = await fetch(`/api/v1/integration-actions/${id}/resolve`, { method: "POST" }); if (response.ok) { setActions(items => items.filter(item => item.id !== id)); setToast("Tindakan ditandai selesai."); } else setError("Tindakan belum dapat ditandai selesai."); setWorking(""); }
  if (!user) return <main className="loading">Memuat…</main>;
  const count = view === "actions" ? actions.length : reviews.length;
  return <AppShell user={user} eyebrow="INBOX" title={`${count} item menunggu`} actions={<span className="header-meta">Transaksi dan tindakan tetap diproses terpisah</span>}>
    <div className="inbox-tabs" role="tablist" aria-label="Jenis inbox"><button role="tab" aria-selected={view === "transactions"} className={view === "transactions" ? "active" : ""} onClick={() => selectView("transactions")}>Transaksi <b>{reviews.length}</b></button><button role="tab" aria-selected={view === "actions"} className={view === "actions" ? "active" : ""} onClick={() => selectView("actions")}>Tindakan <b>{actions.length}</b></button></div>
    <ErrorNotice message={error} retry={load}/>
    {loading ? <Skeleton cards={2}/> : view === "transactions" ? <ReviewCards items={reviews} categories={categories} working={working} action={reviewAction}/> : <section className="action-list">{actions.map(item => <article key={item.id} className="surface action-card"><div><span className="eyebrow">{item.integrationType === "EMAIL_FORWARDING" ? "EMAIL" : item.integrationType}</span><h2>{item.title}</h2><p>{item.description}</p><small>Diterima {new Date(item.createdAt).toLocaleString("id-ID", { dateStyle: "medium", timeStyle: "short" })}</small>{item.actionCode && <p className="action-code">Kode konfirmasi: <code>{item.actionCode}</code></p>}</div>{owner ? <div className="action-buttons">{item.actionUrl && <a className="button" href={item.actionUrl} target="_blank" rel="noopener noreferrer">Verifikasi penerusan</a>}<button className="secondary" disabled={working === item.id} onClick={() => resolveAction(item.id)}>Tandai selesai</button></div> : <small>Pemilik household perlu menyelesaikan tindakan ini.</small>}</article>)}{!actions.length && <div className="empty-state"><span>✓</span><h2>Tidak ada tindakan tertunda</h2><p>Richmod akan menampilkan kebutuhan setup integrasi di sini.</p></div>}</section>}
    <Toast message={toast} onClose={() => setToast("")}/>
  </AppShell>;
}
