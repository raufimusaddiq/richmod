"use client";

import { useCallback, useEffect, useState } from "react";
import { LOAD, responseError, useBusyMap, useGuardedLoad } from "../lib/useLoadState";
import AppShell from "../components/AppShell";
import { ErrorNotice, Skeleton, Toast } from "../components/Feedback";
import ReviewCards from "../components/ReviewCards";
import useAuth from "../components/useAuth";
import { publishInboxCount } from "../components/InboxCountProvider";
import ReviewHistory from "../components/review/ReviewHistory";
import { missingTransactionHref } from "../lib/missingTransaction";

// What the household calls each kind of action; an unknown type keeps its own name.
const actionKinds = { EMAIL_FORWARDING: "EMAIL", SOURCE_PROCESSING: "SUMBER" };
const actionKind = type => actionKinds[type] || type;

const currentView = () => typeof window !== "undefined" && new URLSearchParams(window.location.search).get("view") === "actions" ? "actions" : "transactions";

export default function InboxPage() {
  const user = useAuth();
  const [view, setView] = useState("transactions");
  const [reviews, setReviews] = useState([]), [actions, setActions] = useState([]), [categories, setCategories] = useState([]), [wealthAccounts, setWealthAccounts] = useState([]), [accounts, setAccounts] = useState([]);
  const [error, setError] = useState(""), [toast, setToast] = useState("");
  // One busy flag per card: acting on card B must not release or overwrite card A.
  const { busy, run: runBusy } = useBusyMap();
  // The needs-your-decision count spans every page and excludes notices; the
  // cursor fetches the next page of transaction reviews ("Muat lagi").
  const [decisionCount, setDecisionCount] = useState(null), [nextCursor, setNextCursor] = useState(""), [loadingMore, setLoadingMore] = useState(false);
  // The badge may only be published from a load that fetched both lists.
  const [complete, setComplete] = useState(false);
  // Recently resolved: its own state machine so a failed fetch is shown and retried, never cached as "empty".
  const [history, setHistory] = useState({ status: "idle", items: [] }), [historyOpen, setHistoryOpen] = useState(false), [historyTick, setHistoryTick] = useState(0);
  const owner = user?.household?.role === "OWNER";
  // A reload (after a 409, a reopen, a save) is a refresh: the cards stay mounted
  // so a typed note, the merchant and the category hint survive it.
  const loader = useGuardedLoad(async ({ signal, current }) => {
    const [reviewResponse, actionResponse, categoryResponse, wealthResponse, accountResponse] = await Promise.all(["/api/v1/reviews", "/api/v1/integration-actions", "/api/v1/categories", "/api/v1/wealth/accounts", "/api/v1/accounts"].map(url => fetch(url, { signal })));
    // Each list stands on its own: one failing request must not hide the other.
    const failed = [];
    const readList = async (response, name) => { try { if (!response.ok) throw new Error(); const items = await response.json(); return Array.isArray(items) ? items : []; } catch { failed.push(name); return null; } };
    const readOptional = async response => { try { return response.ok ? await response.json() : null; } catch { return null; } };
    const reviewList = await readList(reviewResponse, "daftar transaksi");
    const actionList = await readList(actionResponse, "daftar tindakan");
    const [categoryList, wealthList, accountList] = [await readOptional(categoryResponse), await readOptional(wealthResponse), await readOptional(accountResponse)];
    if (!current()) return;
    if (reviewList === null && actionList === null) throw new Error("Kotak tinjauan belum dapat dimuat.");
    if (reviewList !== null) {
      setReviews(reviewList); setNextCursor(reviewResponse.headers?.get?.("X-Next-Cursor") || "");
      const counted = Number.parseInt(reviewResponse.headers?.get?.("X-Decision-Count") || "", 10);
      setDecisionCount(Number.isFinite(counted) ? counted : null);
    }
    if (actionList !== null) setActions(actionList);
    if (Array.isArray(categoryList)) setCategories(categoryList);
    if (Array.isArray(wealthList)) setWealthAccounts(wealthList);
    if (Array.isArray(accountList)) setAccounts(accountList);
    setComplete(failed.length === 0);
    setHistoryTick(tick => tick + 1);
    setError(failed.length ? `Belum termuat: ${failed.join(" dan ")}.` : "");
  });
  const load = loader.run;
  useEffect(() => { setView(currentView()); const sync = () => setView(currentView()); window.addEventListener("popstate", sync); return () => window.removeEventListener("popstate", sync); }, []);
  useEffect(() => { if (user) load(); }, [user, load]);
  const decisions = decisionCount ?? reviews.filter(item => item.presentation !== "NOTICE").length;
  // Keyed on the load itself, not on the banner: a stale action error must not freeze the nav badge.
  useEffect(() => { if (loader.status === LOAD.READY && complete) publishInboxCount(decisions + actions.length); }, [loader.status, complete, decisions, actions]);
  useEffect(() => {
    if (!historyOpen) return undefined;
    const controller = new AbortController();
    setHistory(current => ({ ...current, status: current.status === "ready" ? "refreshing" : "loading" }));
    fetch("/api/v1/reviews/resolved", { signal: controller.signal })
      .then(async response => { if (!response.ok) throw new Error(); const items = await response.json(); setHistory({ status: "ready", items: Array.isArray(items) ? items : [] }); })
      .catch(failure => { if (failure?.name !== "AbortError") setHistory(current => ({ ...current, status: "error" })); });
    return () => controller.abort();
  }, [historyOpen, historyTick]);
  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true); setError("");
    try {
      const response = await fetch(`/api/v1/reviews?cursor=${encodeURIComponent(nextCursor)}`);
      if (!response.ok) throw new Error();
      const items = await response.json();
      setReviews(current => [...current, ...(Array.isArray(items) ? items.filter(item => !current.some(existing => existing.id === item.id)) : [])]);
      setNextCursor(response.headers.get("X-Next-Cursor") || "");
    } catch { setError("Tinjauan berikutnya belum dapat dimuat."); }
    finally { setLoadingMore(false); }
  }
  function reopen(id) {
    return runBusy(id, async () => {
      setError("");
      try {
        const response = await fetch(`/api/v1/reviews/${id}/reopen`, { method: "POST" });
        if (!response.ok) { setError(await responseError(response, "Keputusan ini belum dapat dibuka kembali.")); return; }
        await load(); setToast("Tinjauan dibuka kembali dengan data terbaru.");
      } catch { setError("Koneksi terputus. Keputusan ini belum dapat dibuka kembali."); }
    });
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
  function reviewAction(id, name, body) {
    return runBusy(id, async () => {
      setError("");
      try {
        const response = await fetch(`/api/v1/reviews/${id}/${name}`, { method: "POST", headers: body ? { "Content-Type": "application/json" } : undefined, body: body ? JSON.stringify(body) : undefined });
        if (!response.ok) {
          const result = await response.json().catch(() => ({}));
          // Refresh in place: the card that asked stays mounted to show its hint.
          if (response.status === 409 && result.missingFacts) await load();
          setError(result.error || "Tinjauan belum dapat diperbarui.");
          return result;
        }
        const result = response.status === 204 ? null : await response.json().catch(() => null);
        if (result?.route === "CANONICAL_TRANSACTION_FLOW") { window.location.assign(missingTransactionHref(result)); return undefined; }
        if (result?.route === "WEALTH_SNAPSHOT") { window.location.assign(`/wealth?observationId=${result.wealthObservationId}`); return undefined; }
        await load(); setToast(result?.route === "WEALTH_PENDING_SNAPSHOT" ? "Akun disimpan. Nilainya menunggu snapshot berikutnya di halaman Kekayaan." : "Tinjauan diperbarui dan buku keuangan tetap tersinkron.");
        return undefined;
      } catch {
        setError("Koneksi terputus. Tinjauan belum dapat diperbarui.");
        return undefined;
      }
    });
  }
  function resolveAction(id) {
    return runBusy(id, async () => {
      setError("");
      try {
        const response = await fetch(`/api/v1/integration-actions/${id}/resolve`, { method: "POST" });
        if (response.ok) { setActions(items => items.filter(item => item.id !== id)); setToast("Tindakan ditandai selesai."); }
        else setError(await responseError(response, "Tindakan belum dapat ditandai selesai."));
      } catch { setError("Koneksi terputus. Tindakan belum dapat ditandai selesai."); }
    });
  }
  if (!user) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  const count = view === "actions" ? actions.length : decisions;
  return <AppShell user={user} eyebrow="TINJAUAN" title={`${count} item menunggu`} actions={<span className="header-meta">Transaksi dan tindakan tetap diproses terpisah</span>}>
    <div className="inbox-tabs" role="tablist" aria-label="Jenis tinjauan"><button role="tab" data-view="transactions" tabIndex={view === "transactions" ? 0 : -1} aria-selected={view === "transactions"} className={view === "transactions" ? "active" : ""} onKeyDown={tabKeys} onClick={() => selectView("transactions")}>Transaksi <b>{decisions}</b></button><button role="tab" data-view="actions" tabIndex={view === "actions" ? 0 : -1} aria-selected={view === "actions"} className={view === "actions" ? "active" : ""} onKeyDown={tabKeys} onClick={() => selectView("actions")}>Tindakan <b>{actions.length}</b></button></div>
    <ErrorNotice message={error || loader.error} retry={() => load()}/>
    {!loader.hasLoaded && !loader.failed ? <Skeleton cards={2}/> : !loader.hasLoaded ? null : view === "transactions" ? <><ReviewCards items={reviews} categories={categories} accounts={accounts} wealthAccounts={wealthAccounts} busy={busy} action={reviewAction}/>{nextCursor && <div className="review-actions"><button className="secondary" disabled={loadingMore} onClick={loadMore}>{loadingMore ? "Memuat…" : "Muat lagi"}</button></div>}<details className="review-history" onToggle={event => setHistoryOpen(event.currentTarget.open)}><summary>Baru diselesaikan</summary>{history.status === "error" ? <p role="alert">Riwayat belum dapat dimuat. <button type="button" className="secondary" onClick={() => setHistoryTick(tick => tick + 1)}>Coba lagi</button></p> : history.status === "idle" || history.status === "loading" ? <p className="muted">Memuat…</p> : history.items.length === 0 ? <p className="muted">Belum ada keputusan dalam 30 hari terakhir.</p> : <ReviewHistory items={history.items} busy={busy} reopen={reopen}/>}</details></> : <section className="action-list">{actions.map(item => <article key={item.id} className="surface action-card"><div><span className="eyebrow">{actionKind(item.integrationType)}</span><h2>{item.title}</h2><p>{item.description}</p><small>Diterima {new Date(item.createdAt).toLocaleString("id-ID", { dateStyle: "medium", timeStyle: "short" })}</small>{item.actionCode && <p className="action-code">Kode konfirmasi: <code>{item.actionCode}</code></p>}</div>{owner ? <div className="action-buttons">{item.actionUrl && <a className="button" href={item.actionUrl} target="_blank" rel="noopener noreferrer">Verifikasi penerusan</a>}<button className="secondary" disabled={Boolean(busy[item.id])} onClick={() => resolveAction(item.id)}>Tandai selesai</button></div> : <small>Pemilik keluarga perlu menyelesaikan tindakan ini.</small>}</article>)}{!actions.length && <div className="empty-state"><span aria-hidden="true">✓</span><h2>Tidak ada tindakan tertunda</h2><p>Richmod akan menampilkan kebutuhan setup integrasi di sini.</p></div>}</section>}
    <Toast message={toast} onClose={closeToast}/>
  </AppShell>;
}
