"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import { ErrorNotice, Skeleton, Toast } from "../components/Feedback";
import ReviewCards from "../components/ReviewCards";
import useAuth from "../components/useAuth";
import { publishInboxCount } from "../components/InboxCountProvider";
import { reviewLabel } from "../components/review/shared";
import { missingTransactionHref } from "../lib/missingTransaction";

// What the household calls each kind of action; an unknown type keeps its own name.
const actionKinds = { EMAIL_FORWARDING: "EMAIL", SOURCE_PROCESSING: "SUMBER" };
const actionKind = type => actionKinds[type] || type;

const currentView = () => typeof window !== "undefined" && new URLSearchParams(window.location.search).get("view") === "actions" ? "actions" : "transactions";

export default function InboxPage() {
  const user = useAuth();
  const [view, setView] = useState("transactions");
  const [reviews, setReviews] = useState([]), [actions, setActions] = useState([]), [categories, setCategories] = useState([]), [wealthAccounts, setWealthAccounts] = useState([]), [accounts, setAccounts] = useState([]);
  const [loading, setLoading] = useState(true), [error, setError] = useState(""), [working, setWorking] = useState(""), [toast, setToast] = useState("");
  // The needs-your-decision count spans every page and excludes notices; the
  // cursor fetches the next page of transaction reviews ("Muat lagi").
  const [decisionCount, setDecisionCount] = useState(null), [nextCursor, setNextCursor] = useState(""), [loadingMore, setLoadingMore] = useState(false);
  const [resolved, setResolved] = useState(null);
  const owner = user?.household?.role === "OWNER";
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [reviewResponse, actionResponse, categoryResponse, wealthResponse, accountResponse] = await Promise.all([fetch("/api/v1/reviews"), fetch("/api/v1/integration-actions"), fetch("/api/v1/categories"), fetch("/api/v1/wealth/accounts"), fetch("/api/v1/accounts")]);
      // Each list stands on its own: one failing request must not hide the other.
      const failed = [];
      const readList = async (response, apply, name) => { try { if (!response.ok) throw new Error(); const items = await response.json(); apply(Array.isArray(items) ? items : []); } catch { failed.push(name); } };
      await readList(reviewResponse, setReviews, "daftar transaksi");
      setNextCursor(reviewResponse.headers?.get?.("X-Next-Cursor") || "");
      const counted = Number.parseInt(reviewResponse.headers?.get?.("X-Decision-Count") || "", 10);
      setDecisionCount(Number.isFinite(counted) ? counted : null);
      setResolved(null);
      await readList(actionResponse, setActions, "daftar tindakan");
      if (categoryResponse.ok) setCategories(await categoryResponse.json());
      if (wealthResponse.ok) setWealthAccounts(await wealthResponse.json());
      if (accountResponse.ok) setAccounts(await accountResponse.json());
      setError(failed.length ? `Belum termuat: ${failed.join(" dan ")}.` : "");
    } catch { setError("Kotak tinjauan belum dapat dimuat."); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { setView(currentView()); const sync = () => setView(currentView()); window.addEventListener("popstate", sync); return () => window.removeEventListener("popstate", sync); }, []);
  useEffect(() => { if (user) load(); }, [user, load]);
  const decisions = decisionCount ?? reviews.filter(item => item.presentation !== "NOTICE").length;
  useEffect(() => { if (!loading && !error) publishInboxCount(decisions + actions.length); }, [loading, error, decisions, actions]);
  async function loadMore() {
    if (!nextCursor) return;
    setLoadingMore(true);
    try {
      const response = await fetch(`/api/v1/reviews?cursor=${encodeURIComponent(nextCursor)}`);
      if (!response.ok) throw new Error();
      const items = await response.json();
      setReviews(current => [...current, ...(Array.isArray(items) ? items.filter(item => !current.some(existing => existing.id === item.id)) : [])]);
      setNextCursor(response.headers.get("X-Next-Cursor") || "");
    } catch { setError("Tinjauan berikutnya belum dapat dimuat."); }
    finally { setLoadingMore(false); }
  }
  async function loadResolved() {
    if (resolved) return;
    const response = await fetch("/api/v1/reviews/resolved").catch(() => null);
    setResolved(response?.ok ? await response.json() : []);
  }
  async function reopen(id) {
    setWorking(id); setError("");
    const response = await fetch(`/api/v1/reviews/${id}/reopen`, { method: "POST" });
    if (!response.ok) { const result = await response.json().catch(() => ({})); setError(result.error || "Keputusan ini belum dapat dibuka kembali."); setWorking(""); return; }
    await load(); setToast("Tinjauan dibuka kembali dengan data terbaru."); setWorking("");
  }
  const closeToast = useCallback(() => setToast(""), []);

  function selectView(next) { setView(next); window.history.pushState({}, "", `/inbox?view=${next}`); }
  function tabKeys(event) {
    const order = ["transactions", "actions"];
    const step = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const next = order[(order.indexOf(view) + step + order.length) % order.length];
    selectView(next);
    event.currentTarget.parentElement?.querySelector(`[data-view="${next}"]`)?.focus();
  }
  async function reviewAction(id, name, body) { setWorking(id); setError(""); const canonical = name === "resolve"; const response = await fetch(canonical ? `/api/v1/reviews/${id}/resolve` : `/api/v1/reviews/${id}/${name}`, { method: "POST", headers: body ? { "Content-Type": "application/json" } : undefined, body: body ? JSON.stringify(body) : undefined }); if (!response.ok) { const result = await response.json().catch(() => ({})); if (response.status === 409 && result.missingFacts) await load(); setError(result.error || "Tinjauan belum dapat diperbarui."); setWorking(""); return result; } else { const result = response.status === 204 ? null : await response.json().catch(() => null); if (result?.route === "CANONICAL_TRANSACTION_FLOW") { window.location.assign(missingTransactionHref(result)); return; } if (result?.route === "WEALTH_SNAPSHOT") { window.location.assign(`/wealth?observationId=${result.wealthObservationId}`); return; } await load(); setToast(result?.route === "WEALTH_PENDING_SNAPSHOT" ? "Akun disimpan. Nilainya menunggu snapshot berikutnya di halaman Kekayaan." : "Tinjauan diperbarui dan buku keuangan tetap tersinkron."); } setWorking(""); }
  async function resolveAction(id) { setWorking(id); const response = await fetch(`/api/v1/integration-actions/${id}/resolve`, { method: "POST" }); if (response.ok) { setActions(items => items.filter(item => item.id !== id)); setToast("Tindakan ditandai selesai."); } else setError("Tindakan belum dapat ditandai selesai."); setWorking(""); }
  if (!user) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  const count = view === "actions" ? actions.length : decisions;
  return <AppShell user={user} eyebrow="TINJAUAN" title={`${count} item menunggu`} actions={<span className="header-meta">Transaksi dan tindakan tetap diproses terpisah</span>}>
    <div className="inbox-tabs" role="tablist" aria-label="Jenis tinjauan"><button role="tab" data-view="transactions" tabIndex={view === "transactions" ? 0 : -1} aria-selected={view === "transactions"} className={view === "transactions" ? "active" : ""} onKeyDown={tabKeys} onClick={() => selectView("transactions")}>Transaksi <b>{decisions}</b></button><button role="tab" data-view="actions" tabIndex={view === "actions" ? 0 : -1} aria-selected={view === "actions"} className={view === "actions" ? "active" : ""} onKeyDown={tabKeys} onClick={() => selectView("actions")}>Tindakan <b>{actions.length}</b></button></div>
    <ErrorNotice message={error} retry={load}/>
    {loading ? <Skeleton cards={2}/> : view === "transactions" ? <><ReviewCards items={reviews} categories={categories} accounts={accounts} wealthAccounts={wealthAccounts} working={working} action={reviewAction}/>{nextCursor && <div className="review-actions"><button className="secondary" disabled={loadingMore} onClick={loadMore}>{loadingMore ? "Memuat…" : "Muat lagi"}</button></div>}<details className="review-history" onToggle={event => { if (event.currentTarget.open) loadResolved(); }}><summary>Baru diselesaikan</summary>{resolved === null ? <p className="muted">Memuat…</p> : resolved.length === 0 ? <p className="muted">Belum ada keputusan dalam 30 hari terakhir.</p> : <ul>{resolved.map(item => <li key={item.id}><span>{reviewLabel(item.reviewType)} · {item.summary}</span>{item.reopenable ? <button className="secondary" disabled={working === item.id} onClick={() => reopen(item.id)}>Buka lagi</button> : <small>Sudah final</small>}</li>)}</ul>}</details></> : <section className="action-list">{actions.map(item => <article key={item.id} className="surface action-card"><div><span className="eyebrow">{actionKind(item.integrationType)}</span><h2>{item.title}</h2><p>{item.description}</p><small>Diterima {new Date(item.createdAt).toLocaleString("id-ID", { dateStyle: "medium", timeStyle: "short" })}</small>{item.actionCode && <p className="action-code">Kode konfirmasi: <code>{item.actionCode}</code></p>}</div>{owner ? <div className="action-buttons">{item.actionUrl && <a className="button" href={item.actionUrl} target="_blank" rel="noopener noreferrer">Verifikasi penerusan</a>}<button className="secondary" disabled={working === item.id} onClick={() => resolveAction(item.id)}>Tandai selesai</button></div> : <small>Pemilik keluarga perlu menyelesaikan tindakan ini.</small>}</article>)}{!actions.length && <div className="empty-state"><span aria-hidden="true">✓</span><h2>Tidak ada tindakan tertunda</h2><p>Richmod akan menampilkan kebutuhan setup integrasi di sini.</p></div>}</section>}
    <Toast message={toast} onClose={closeToast}/>
  </AppShell>;
}
