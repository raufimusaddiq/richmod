"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { LOAD, responseError, useBusyMap, useGuardedLoad } from "../lib/useLoadState";
import AppShell from "../components/AppShell";
import { ErrorNotice, Skeleton, Toast } from "../components/Feedback";
import ReviewCards from "../components/ReviewCards";
import useAuth from "../components/useAuth";
import { publishInboxCount } from "../components/InboxCountProvider";
import ReviewHistory from "../components/review/ReviewHistory";
import { money } from "../lib/format";
import { inboxView, legacyViewTarget, resolvedLine } from "../lib/reviewInbox";
import { missingTransactionHref } from "../lib/missingTransaction";

const VIEWS = ["transactions", "history"];

export default function InboxPage() {
  const user = useAuth();
  const [view, setView] = useState("transactions");
  const [reviews, setReviews] = useState([]), [categories, setCategories] = useState([]), [wealthAccounts, setWealthAccounts] = useState([]), [accounts, setAccounts] = useState([]);
  const [error, setError] = useState(""), [toast, setToast] = useState("");
  const { busy, run: runBusy } = useBusyMap();
  const [decisionCount, setDecisionCount] = useState(null), [nextCursor, setNextCursor] = useState(""), [loadingMore, setLoadingMore] = useState(false);
  const [complete, setComplete] = useState(false);
  const [resolved, setResolved] = useState([]);
  const [history, setHistory] = useState({ status: "idle", items: [] }), [historyTick, setHistoryTick] = useState(0);
  const loader = useGuardedLoad(async ({ signal, current }) => {
    const [reviewResponse, categoryResponse, wealthResponse, accountResponse] = await Promise.all(["/api/v1/reviews", "/api/v1/categories", "/api/v1/wealth/accounts", "/api/v1/accounts"].map(url => fetch(url, { signal })));
    const readOptional = async response => { try { return response.ok ? await response.json() : null; } catch { return null; } };
    let reviewList = null;
    try { if (!reviewResponse.ok) throw new Error(); const items = await reviewResponse.json(); reviewList = Array.isArray(items) ? items : []; } catch { reviewList = null; }
    const [categoryList, wealthList, accountList] = [await readOptional(categoryResponse), await readOptional(wealthResponse), await readOptional(accountResponse)];
    if (!current()) return;
    if (reviewList === null) throw new Error("Daftar yang perlu dicek belum dapat dimuat.");
    setReviews(reviewList); setNextCursor(reviewResponse.headers?.get?.("X-Next-Cursor") || "");
    const counted = Number.parseInt(reviewResponse.headers?.get?.("X-Decision-Count") || "", 10);
    setDecisionCount(Number.isFinite(counted) ? counted : null);
    if (Array.isArray(categoryList)) setCategories(categoryList);
    if (Array.isArray(wealthList)) setWealthAccounts(wealthList);
    if (Array.isArray(accountList)) setAccounts(accountList);
    setComplete(true);
    setHistoryTick(tick => tick + 1);
    setError("");
  });
  const load = loader.run;
  useEffect(() => {
    const sync = () => {
      const moved = legacyViewTarget(new URLSearchParams(window.location.search).get("view"));
      if (moved) { window.location.replace(moved); return; }
      setView(inboxView(window.location.search));
    };
    sync(); window.addEventListener("popstate", sync); return () => window.removeEventListener("popstate", sync);
  }, []);
  useEffect(() => { if (user) load(); }, [user, load]);
  const decisions = decisionCount ?? reviews.filter(item => item.presentation !== "NOTICE").length;
  useEffect(() => { if (loader.status === LOAD.READY && complete) publishInboxCount({ reviewCount: decisions }); }, [loader.status, complete, decisions]);
  const historyOpen = view === "history";
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
        setResolved(lines => lines.filter(line => line.reopenId !== id));
        await load(); setToast("Tinjauan dibuka kembali dengan data terbaru.");
      } catch { setError("Koneksi terputus. Keputusan ini belum dapat dibuka kembali."); }
    });
  }
  const closeToast = useCallback(() => setToast(""), []);

  function selectView(next) { setView(next); window.history.pushState({}, "", next === "history" ? "/inbox?view=history" : "/inbox"); }
  function tabKeys(event) {
    const step = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
    const edge = event.key === "Home" ? 0 : event.key === "End" ? VIEWS.length - 1 : null;
    if (!step && edge === null) return;
    event.preventDefault();
    const next = edge !== null ? VIEWS[edge] : VIEWS[(Math.max(VIEWS.indexOf(view), 0) + step + VIEWS.length) % VIEWS.length];
    selectView(next);
    event.currentTarget.parentElement?.querySelector(`[data-view="${next}"]`)?.focus();
  }
  function reviewAction(id, name, body) {
    const item = reviews.find(candidate => candidate.id === id);
    return runBusy(id, async () => {
      setError("");
      try {
        const response = await fetch(`/api/v1/reviews/${id}/${name}`, { method: "POST", headers: body ? { "Content-Type": "application/json" } : undefined, body: body ? JSON.stringify(body) : undefined });
        if (!response.ok) {
          const result = await response.json().catch(() => ({}));
          if (response.status === 409 && result.missingFacts) await load();
          setError(result.error || "Tinjauan belum dapat diperbarui.");
          return result;
        }
        const result = response.status === 204 ? null : await response.json().catch(() => null);
        if (result?.route === "CANONICAL_TRANSACTION_FLOW") { window.location.assign(missingTransactionHref(result)); return undefined; }
        if (result?.route === "WEALTH_SNAPSHOT") { window.location.assign(`/wealth?observationId=${result.wealthObservationId}`); return undefined; }
        if (item) setResolved(lines => [resolvedLine(item, name, body, categories), ...lines.filter(line => line.id !== id)]);
        await load(); if (result?.route === "WEALTH_PENDING_SNAPSHOT") setToast("Akun disimpan. Nilainya menunggu snapshot berikutnya di halaman Kekayaan.");
        return undefined;
      } catch {
        setError("Koneksi terputus. Tinjauan belum dapat diperbarui.");
        return undefined;
      }
    });
  }
  if (!user) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  const empty = view === "transactions" && reviews.length === 0;
  return <AppShell user={user} eyebrow="Perlu dicek" title="Perlu dicek" actions={<span className="header-meta">Satu pertanyaan, satu ketukan</span>}>
    <div className="inbox-tabs" data-slide data-slide-panel="#inbox-panel" role="tablist" aria-label="Tampilan Perlu dicek"><button role="tab" id="inbox-tab-transactions" aria-controls="inbox-panel" data-view="transactions" tabIndex={view === "transactions" ? 0 : -1} aria-selected={view === "transactions"} className={view === "transactions" ? "active" : ""} onKeyDown={tabKeys} onClick={() => selectView("transactions")}>Menunggu ({decisions})</button><button role="tab" id="inbox-tab-history" aria-controls="inbox-panel" data-view="history" tabIndex={view === "history" ? 0 : -1} aria-selected={view === "history"} className={view === "history" ? "active" : ""} onKeyDown={tabKeys} onClick={() => selectView("history")}>Riwayat 30 hari</button></div>
    <ErrorNotice message={error || loader.error} retry={() => load()}/>
    <div role="tabpanel" id="inbox-panel" aria-labelledby={`inbox-tab-${view}`}>{!loader.hasLoaded && !loader.failed ? <Skeleton cards={2}/> : !loader.hasLoaded ? null : view === "history" ? <section className="review-history" aria-label="Keputusan 30 hari terakhir">{history.status === "error" ? <p role="alert">Riwayat belum dapat dimuat. <button type="button" className="secondary" onClick={() => setHistoryTick(tick => tick + 1)}>Coba lagi</button></p> : history.status === "idle" || history.status === "loading" ? <p className="muted">Memuat…</p> : history.items.length === 0 ? <p className="muted">Belum ada keputusan dalam 30 hari terakhir.</p> : <ReviewHistory items={history.items} busy={busy} reopen={reopen}/>}</section> : <>
      {resolved.length > 0 && <ul className="resolved-lines" aria-label="Baru saja diputuskan">{resolved.map(line => <li key={line.id} className="resolved-line"><span aria-hidden="true">✓</span><span className="resolved-text">{line.label}{line.amount ? <> · <b className="num">{money(line.amount)}</b></> : null} {line.summary}</span>{line.canUndo ? <button type="button" className="link-button" disabled={Boolean(busy[line.reopenId])} onClick={() => reopen(line.reopenId)}>Urungkan</button> : line.transactionId ? <Link className="link-button" href={`/transactions?id=${line.transactionId}`}>Lihat transaksi</Link> : null}</li>)}</ul>}
      {empty ? <div className="empty-state"><span aria-hidden="true">✓</span><h2>Semua sudah tercatat rapi</h2><p>Tidak ada yang perlu kamu putuskan sekarang. Richmod akan memberi tahu bila ada yang perlu dicek.</p><p><Link href="/">Ke Beranda</Link> · <Link href="/transactions">Lihat Transaksi</Link></p></div> : <ReviewCards items={reviews} categories={categories} accounts={accounts} wealthAccounts={wealthAccounts} busy={busy} action={reviewAction}/>}
      {nextCursor && <div className="review-actions"><button className="secondary" disabled={loadingMore} onClick={loadMore}>{loadingMore ? "Memuat…" : "Muat lagi"}</button></div>}
    </>}</div>
    <Toast message={toast} onClose={closeToast}/>
  </AppShell>;
}
