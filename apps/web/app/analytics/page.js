"use client";

import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import AppShell from "../components/AppShell";
import CycleLedger from "../components/CycleLedger";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import InsightCard from "../components/InsightCard";
import CycleDecisions from "../components/CycleDecisions";
import useAuth from "../components/useAuth";
import { dayLabel } from "../lib/chartData";
import { adjacentCycles } from "../lib/cycleLedger";
import { cycleLabel, inclusiveEnd, readReviewSelection, selectionHref, transactionHref } from "../lib/cycleReview";
import { categoryKey } from "../lib/cycleWhere";
import { money } from "../lib/format";
import { pollInsight, selectCycleInsight } from "../lib/insightData";
import { Metric } from "./shared";
import { MeetingNav, ComparisonContext, ChangesTable, MerchantTable } from "./CycleSections";
import { CategoryDrawer, ChangesCard, DailyCard, HistoryCard, WhereCard } from "./CycleWhere";
import { KpiStrip, NoticeStrip, PaceCard, QualityFooter, SavingsWealthCard } from "./CycleDashboard";
import { CalendarReview } from "./CalendarReview";

export default function AnalyticsPage() {
  return <Suspense fallback={<main className="loading" role="status">Memuat…</main>}><AnalyticsReview/></Suspense>;
}

function AnalyticsReview() {
  const user = useAuth();
  const params = useSearchParams();
  const selection = readReviewSelection(params.toString());
  const [facts, setFacts] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const [insight, setInsight] = useState(null);
  const [insightLoading, setInsightLoading] = useState(false);
  const [insightError, setInsightError] = useState("");
  const insightAbort = useRef(null);
  const [drafts, setDrafts] = useState({});
  // The drawer returns focus to what opened it; a drawer opened by this page is closed with Back, one opened by a shared URL is replaced.
  const trigger = useRef(null);
  const drawerPushed = useRef(false);
  const comparison = useRef(null);
  // A cycle the reader picked is the only thing allowed to mark an updated
  // figure; the first load and the insight poll are not.
  const valueNav = useRef(false);
  const [valueUpdated, setValueUpdated] = useState(0);
  useEffect(() => {
    if (!facts || !valueNav.current) return;
    valueNav.current = false;
    setValueUpdated(value => value + 1);
  }, [facts]);

  useEffect(() => {
    if (!Object.values(drafts).some(body => body.trim())) return;
    const warn = event => { event.preventDefault(); event.returnValue = ""; };
    const guardLink = event => {
      const link = event.target.closest?.("a[href]");
      if (!link || event.ctrlKey || event.metaKey || event.shiftKey || link.target === "_blank") return;
      const url = new URL(link.href, window.location.href);
      if (url.pathname === "/analytics" && url.search === window.location.search && url.origin === window.location.origin) return;
      if (!window.confirm("Ada draf keputusan yang belum disimpan. Tinggalkan halaman?")) { event.preventDefault(); event.stopPropagation(); }
    };
    window.addEventListener("beforeunload", warn);
    document.addEventListener("click", guardLink, true);
    return () => { window.removeEventListener("beforeunload", warn); document.removeEventListener("click", guardLink, true); };
  }, [drafts]);

  function navigate(change, { replace = false } = {}) {
    if ("cycle" in change) valueNav.current = true;
    window.history[replace ? "replaceState" : "pushState"](null, "", selectionHref({ ...selection, ...change }));
  }
  useEffect(() => { if (!selection.category) drawerPushed.current = false; }, [selection.category]);

  const loadedKey = useRef("");
  useEffect(() => {
    if (!user || selection.view !== "cycle") return;
    // The default load rewrites the URL to the cycle it resolved; that is the same payload, so don't refetch it.
    if (selection.cycle && loadedKey.current === `${selection.cycle}|${reload}`) return;
    const controller = new AbortController();
    setLoading(true); setError("");
    const query = new URLSearchParams();
    if (selection.cycle) query.set("cycle_start", selection.cycle);
    fetch(`/api/v1/analytics/cycle-review?${query}`, { signal: controller.signal, cache: "no-store" })
      .then(async response => {
        if (!response.ok) throw new Error(response.status === 404 ? "Siklus ini tidak ditemukan. Pilih siklus berjalan." : "Tinjauan siklus belum dapat dimuat. Coba lagi.");
        const result = await response.json();
        if (result?.version !== "cycle-review-v1" || !result.period || !result.cashflow || !result.spendingShape || !result.comparison?.expense || !result.wealth || !["daily", "cycles", "categoryChanges", "merchantDrivers", "memberAttribution", "savingsDestinations", "dataQuality"].every(key => Array.isArray(result[key]))) throw new Error("Data tinjauan belum dapat dibaca. Coba lagi.");
        if (controller.signal.aborted) return;
        setFacts(result);
        loadedKey.current = `${result.period.kind === "SALARY_CYCLE" ? result.period.start : selection.cycle}|${reload}`;
        if (!selection.cycle && result.period.kind === "SALARY_CYCLE") {
          window.history.replaceState(null, "", selectionHref({ ...readReviewSelection(window.location.search), cycle: result.period.start }));
        }
      })
      .catch(err => { if (err.name !== "AbortError") { setFacts(null); setError(err.message || "Koneksi terputus saat memuat tinjauan."); } })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [user, selection.view, selection.cycle, reload]);

  const cycleStart = facts?.period?.kind === "SALARY_CYCLE" ? facts.period.start : "";
  const cycleEnd = facts?.period?.measuredUntil || "";
  const cycleState = facts?.period?.state || "";
  const loadInsights = useCallback(async signal => {
    const query = new URLSearchParams({ cycle_start: cycleStart });
    const response = await fetch(`/api/v1/insights?${query}`, { signal, cache: "no-store" });
    if (!response.ok) throw new Error("load");
    return response.json();
  }, [cycleStart]);

  useEffect(() => {
    insightAbort.current?.abort();
    setInsight(null); setInsightError(""); setInsightLoading(false);
    if (!user || selection.view !== "cycle" || !cycleStart) return;
    const controller = new AbortController();
    insightAbort.current = controller;
    loadInsights(controller.signal).then(async items => {
      const selected = selectCycleInsight(items, { start: cycleStart, measuredUntil: cycleEnd, state: cycleState });
      if (controller.signal.aborted) return;
      setInsight(selected);
      if (selected?.status === "PENDING") await pollInsight({ insightId: selected.id, load: loadInsights, onUpdate: setInsight, signal: controller.signal, cycle: { start: cycleStart, measuredUntil: cycleEnd, state: cycleState } });
    }).catch(err => {
      if (err.name !== "AbortError") setInsightError(err.message === "insight cutoff mismatch" ? "Pembahasan lama tidak cocok dengan rentang ini. Coba lagi nanti." : err.message === "insight polling timeout" ? "Pembahasan belum selesai. Periksa lagi nanti." : "Pembahasan belum dapat dimuat.");
    });
    return () => controller.abort();
  }, [user, selection.view, cycleStart, cycleEnd, cycleState, loadInsights]);

  async function generateInsight() {
    if (!cycleStart || insightLoading) return;
    insightAbort.current?.abort();
    const controller = new AbortController();
    insightAbort.current = controller;
    setInsightLoading(true); setInsightError("");
    try {
      const query = new URLSearchParams({ period: "cycle", cycle_start: cycleStart });
      const response = await fetch(`/api/v1/insights/generate?${query}`, { method: "POST", signal: controller.signal });
      if (!response.ok) throw new Error(response.status === 429 ? "insight rate limit" : response.status === 409 ? "insight pending cutoff" : "generate");
      const requested = await response.json();
      await pollInsight({ insightId: requested.id, signal: controller.signal, onUpdate: setInsight, load: loadInsights, cycle: { start: cycleStart, measuredUntil: cycleEnd, state: cycleState } });
    } catch (err) {
      if (err.name !== "AbortError") setInsightError(err.message === "insight rate limit" ? "Batas satu pembahasan per jam. Coba lagi setelah jeda satu jam dari permintaan terakhir." : err.message === "insight pending cutoff" ? "Pembahasan sebelumnya masih diproses. Tunggu sampai selesai." : err.message === "insight cutoff mismatch" ? "Pembahasan lama tidak cocok dengan rentang ini. Coba lagi nanti." : err.message === "insight polling timeout" ? "Pembahasan belum selesai. Periksa lagi nanti." : "Pembahasan belum dapat dibuat. Data dan bukti tetap tersedia.");
    } finally { if (!controller.signal.aborted) setInsightLoading(false); }
  }
  useEffect(() => () => insightAbort.current?.abort(), []);

  const meeting = selection.view === "cycle" && facts?.period.kind === "SALARY_CYCLE" && facts.period.state === "CLOSED" && Boolean(selection.step);
  const step = meeting ? selection.step : "";
  useEffect(() => {
    if (step) document.getElementById(step)?.querySelector("h2")?.focus();
  }, [step, facts]);

  if (!user) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  const selectedCategory = selection.category ? facts?.categoryChanges.find(item => categoryKey(item) === selection.category) : null;
  const adjacent = adjacentCycles(facts?.cycles, facts?.period?.start);
  // Choosing a category opens its drawer (the category stays in the URL); in a meeting it moves on to the evidence step.
  const chooseCategory = (category, element) => {
    trigger.current = element || null;
    drawerPushed.current = true;
    navigate({ category, ...(meeting ? { step: "drivers" } : {}) });
  };
  const closeCategory = () => {
    if (drawerPushed.current) { drawerPushed.current = false; window.history.back(); }
    else navigate({ category: "" }, { replace: true });
  };
  const openComparison = () => { const panel = comparison.current; if (!panel) return; panel.open = true; panel.scrollIntoView({ block: "start" }); panel.querySelector("summary")?.focus(); };
  const refreshing = selection.view === "cycle" && loading && Boolean(facts);
  return <AppShell user={user} eyebrow="Analisis" title={selection.view === "calendar" ? "Analisis kalender" : "Laporan siklus"}>
    <div className="analytics-flow cycle-review" data-meeting-step={step || undefined} data-stale={refreshing ? "true" : undefined} aria-busy={refreshing || undefined}>
      <div className="range-controls">
        <div className="range-control-group" aria-label="Tampilan analisis">
          <button type="button" aria-pressed={selection.view === "cycle"} className={selection.view === "cycle" ? "active" : "secondary"} onClick={() => navigate({ view: "cycle" })}>Siklus Gaji</button>
          <button type="button" aria-pressed={selection.view === "calendar"} className={selection.view === "calendar" ? "active" : "secondary"} onClick={() => navigate({ view: "calendar" })}>Kalender</button>
        </div>
        {selection.view === "cycle" && <><label className="cycle-selector">Siklus yang ditinjau
          <select value={selection.cycle} onChange={event => navigate({ cycle: event.target.value, category: "", step: "" })}>
            <option value="">Siklus berjalan</option>
            {selection.cycle && !facts?.cycles?.some(item => item.start === selection.cycle) && <option value={selection.cycle}>{dayLabel(selection.cycle)}</option>}
            {(facts?.cycles || []).map(period => <option key={period.start} value={period.start}>{cycleLabel(period)}</option>)}
          </select>
        </label>
        <div className="cycle-step" role="group" aria-label="Pindah siklus">
          <button type="button" className="secondary" disabled={!adjacent.older} onClick={() => navigate({ cycle: adjacent.older, category: "", step: "" })}>‹ Siklus sebelumnya</button>
          <button type="button" className="secondary" disabled={!adjacent.newer} onClick={() => navigate({ cycle: adjacent.newer, category: "", step: "" })}>Siklus berikutnya ›</button>
        </div>
        {facts && !error && <div className="cycle-status" aria-live="polite">
          <strong>{facts.period.state === "ACTIVE" ? `Berjalan · hari ke-${facts.spendingShape.days} · data sampai ${dayLabel(inclusiveEnd(facts.period.measuredUntil))}` : `Ditutup · ${facts.spendingShape.days} hari · data sampai ${dayLabel(inclusiveEnd(facts.period.measuredUntil))}`}</strong>
          <span>{facts.period.state === "ACTIVE" && "Hari ini belum penuh · "}Asia/Jakarta</span>
          {facts.period.kind === "SALARY_CYCLE" && facts.period.state === "CLOSED" && !meeting && <button type="button" onClick={() => navigate({ step: "position" })}>Tinjau siklus ini</button>}
        </div>}</>}
      </div>
      {selection.view === "calendar" ? <CalendarReview selection={selection} navigate={navigate}/> : <>
        <ErrorNotice message={error} retry={() => setReload(value => value + 1)}/>
        {error && <button type="button" className="secondary" onClick={() => navigate({ cycle: "", category: "" })}>Siklus berjalan</button>}
        {loading && !facts && <Skeleton cards={3} rows={4}/>}
        {refreshing && <p className="ledger-loading" role="status">Memuat siklus…</p>}
        {facts && !error && <>
          {meeting && <MeetingNav step={step} selection={selection} navigate={navigate}/>}
          <NoticeStrip facts={{ ...facts, period: { ...facts.period, reviewStep: step } }}/>
          <KpiStrip facts={facts} updated={valueUpdated}/>
          <div className="dash-row"><PaceCard facts={facts}/><SavingsWealthCard facts={facts}/></div>
          <div className="dash-row dash-row-half"><WhereCard facts={facts} step={step} select={chooseCategory} openComparison={openComparison}/>
            <ChangesCard facts={facts} step={step} select={chooseCategory} comparisonRef={comparison}><ComparisonContext comparison={facts.comparison} period={facts.period}/><ChangesTable items={facts.categoryChanges} comparison={facts.comparison} selected={selection.category} select={chooseCategory}/></ChangesCard></div>
          <div className="dash-row">
            <DailyCard facts={facts} step={step}><details className="review-daily"><summary>Lihat nilai harian</summary><div className="review-table-wrap" tabIndex={0} role="region" aria-label="Nilai pengeluaran harian, geser untuk semua kolom"><table><caption className="visually-hidden">Pengeluaran bersih harian</caption><thead><tr><th scope="col">Tanggal</th><th scope="col">Pengeluaran</th><th scope="col">Refund</th><th scope="col">Total sampai hari ini</th></tr></thead><tbody>{facts.daily.map(item => <tr key={item.period}><th scope="row">{Number(item.expense) !== 0 ? <a href={transactionHref({ ...facts.period, reviewStep: step }, { from: item.period, to: item.period })} aria-label={`Lihat pengeluaran ${dayLabel(item.period)}`}>{dayLabel(item.period)}</a> : dayLabel(item.period)}</th><td>{money(item.expense)}</td><td>{money(item.refund)}</td><td>{item.cumulativeExpense == null ? "—" : money(item.cumulativeExpense)}</td></tr>)}</tbody></table></div></details></DailyCard>
            <HistoryCard facts={facts} onSelect={start => navigate({ cycle: start, category: "", step: "" })}/>
          </div>
          {facts.history?.length > 0 && <section id="matrix" className="review-section" aria-labelledby="matrix-title">
            <details className="report-disclosure" open={step === "position" || step === "changes"}>
            <summary><h2 id="matrix-title" tabIndex={-1}>Kategori × siklus</h2><span>Matriks {facts.history.length} siklus · siklus ke siklus</span></summary>
            <CycleLedger history={facts.history} selected={facts.period.start} median={facts.period.state === "CLOSED" ? facts.comparison.expense.median3 : null} onSelect={start => navigate({ cycle: start, category: "", step: "" })} categoryHistory={facts.categoryHistory} selectedCategory={selection.category} selectable={facts.categoryChanges.map(item => item.id || "uncategorized")} onSelectCategory={chooseCategory}/>
            </details>
          </section>}
          <section id="merchants" className="review-section" aria-labelledby="merchants-title">
            <details className="report-disclosure"><summary><h2 id="merchants-title">Merchant pendukung</h2><span>{facts.merchantDrivers.length} merchant</span></summary>
            <p className="review-description">Merchant diurutkan berdasarkan perubahan absolut dibanding siklus sebelumnya.</p>
            <MerchantTable items={facts.merchantDrivers} period={facts.period}/>
            <a href={transactionHref(facts.period)}>Lihat seluruh pengeluaran periode ini</a>
            </details>
          </section>
          <section id="household" className="review-section" aria-labelledby="household-title">
            <details className="report-disclosure"><summary><h2 id="household-title">Catatan rumah tangga</h2><span>{facts.memberAttribution.length ? `${facts.memberAttribution.length} pencatat` : "Belum ada atribusi"}</span></summary>
            <p className="review-description">Siapa yang memulai pencatatan, ketika diketahui. Ini bukan peringkat tanggung jawab atau perbandingan kebiasaan.</p>
            <dl className="review-attribution">{facts.memberAttribution.map(item => <Metric key={item.id || item.name} label={item.name} value={`${money(item.amount)} · ${item.count} transaksi`}/>)}</dl>
            {!facts.memberAttribution.length && <p className="empty compact">Belum ada pengeluaran yang dapat diatribusikan.</p>}
            </details>
          </section>
          <QualityFooter facts={facts} step={step}/>
          <section id="discussion" className="review-section" aria-labelledby="discussion-title">
            <details className="report-disclosure" open={step === "discussion"}>
            <summary><h2 id="discussion-title" tabIndex={-1}>Bahan pembahasan</h2><span>{insightError ? "Belum tersedia" : insightLoading || insight?.status === "PENDING" ? "Memproses" : insight?.status === "SUCCEEDED" ? "Tersedia · opsional" : "Opsional"}</span></summary>
            <p className="review-description">Bukan saran keuangan atau keputusan rumah tangga.</p>
            <InsightCard insight={insight} cycle={facts.period} loading={insightLoading} error={insightError} canGenerate={Boolean(cycleStart)} onGenerate={generateInsight}/>
            <a href={meeting ? selectionHref({ ...selection, step: "changes" }) : "#changes"}>Lihat data pendukung pembahasan</a>
            </details>
          </section>
          {selectedCategory && (!meeting || step === "drivers") && <CategoryDrawer item={selectedCategory} facts={facts} period={{ ...facts.period, reviewStep: step }} close={closeCategory} triggerRef={trigger}/>}
          {cycleStart && <CycleDecisions key={cycleStart} expanded={step === "decisions"} cycleStart={cycleStart} closed={facts.period.state === "CLOSED"} body={drafts[cycleStart] || ""} onBodyChange={body => setDrafts(current => ({ ...current, [cycleStart]: body }))}/>}
        </>}
      </>}
    </div>
  </AppShell>;
}
