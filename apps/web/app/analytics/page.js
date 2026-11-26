import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { usePathname, useSearchParams } from "../router";
import AppShell from "../components/AppShell";
import { useCategoryPalette } from "../components/CategoryPaletteProvider";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import Presence from "../components/Presence";
import InsightCard from "../components/InsightCard";
import CycleDecisions from "../components/CycleDecisions";
import useAuth from "../components/useAuth";
import { dayLabel } from "../lib/chartData";
import { adjacentCycles, provisionalDays } from "../lib/cycleLedger";
import { cycleLabel, inclusiveEnd, readReviewSelection, selectionHref, splitDataQuality } from "../lib/cycleReview";
import { categoryKey } from "../lib/cycleWhere";
import { pollInsight, selectCycleInsight } from "../lib/insightData";
import { MeetingNav, HouseholdSection, MerchantCard } from "./CycleSections";
import { CategoryDrawer, DailyCard, HistoryCard } from "./CycleWhere";
import CategoryTable from "./CategoryTable";
import { KpiStrip, LastCycleCard, NoticeStrip, PaceCard, QualityFooter } from "./CycleDashboard";
import WealthWorkspace from "../wealth/WealthWorkspace";
import { CalendarReview } from "./CalendarReview";

const reportTabs = [["cycle", "Arus kas"], ["wealth", "Kekayaan"]];

function ReportSwitch({ view, choose }) {
  const refs = useRef({});
  const active = view === "wealth" ? "wealth" : "cycle";
  const move = (event, index) => {
    const last = reportTabs.length - 1;
    const next = { ArrowRight: index === last ? 0 : index + 1, ArrowLeft: index === 0 ? last : index - 1, Home: 0, End: last }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    choose(reportTabs[next][0]);
    refs.current[reportTabs[next][0]]?.focus();
  };
  return <div className="report-switch" data-slide data-slide-panel="#laporan-panel" role="tablist" aria-label="Jenis laporan">
    {reportTabs.map(([id, label], index) => <button key={id} ref={node => { refs.current[id] = node; }} type="button" role="tab" id={`laporan-tab-${id}`} aria-selected={active === id} aria-controls="laporan-panel" tabIndex={active === id ? 0 : -1} onClick={() => choose(id)} onKeyDown={event => move(event, index)}>{label}</button>)}
  </div>;
}

export default function AnalyticsPage() {
  return <Suspense fallback={<main className="loading" role="status">Memuat…</main>}><AnalyticsReview/></Suspense>;
}

function AnalyticsReview() {
  const user = useAuth();
  const palette = useCategoryPalette();
  const params = useSearchParams();
  const pathname = usePathname();
  const selection = readReviewSelection(params.toString(), String(pathname || "").replace(/\/$/, "") === "/wealth" ? "wealth" : "cycle");
  const [facts, setFacts] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const [insight, setInsight] = useState(null);
  const [insightLoading, setInsightLoading] = useState(false);
  const [insightError, setInsightError] = useState("");
  const insightAbort = useRef(null);
  const [drafts, setDrafts] = useState({});
  const [decisionMeta, setDecisionMeta] = useState({ status: "loading", count: 0 });
  const [qualityOpenTrigger, setQualityOpenTrigger] = useState(0);
  const trigger = useRef(null);
  const drawerPushed = useRef(false);
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
      if (!response.ok) throw new Error(response.status === 429 ? "insight rate limit" : response.status === 409 ? "insight pending cutoff" : response.status === 422 ? "insight insufficient data" : "generate");
      const requested = await response.json();
      await pollInsight({ insightId: requested.id, signal: controller.signal, onUpdate: setInsight, load: loadInsights, cycle: { start: cycleStart, measuredUntil: cycleEnd, state: cycleState } });
    } catch (err) {
      if (err.name !== "AbortError") setInsightError(err.message === "insight rate limit" ? "Batas satu pembahasan per jam. Coba lagi setelah jeda satu jam dari permintaan terakhir." : err.message === "insight pending cutoff" ? "Pembahasan sebelumnya masih diproses. Tunggu sampai selesai." : err.message === "insight insufficient data" ? "Data belum cukup lengkap untuk pembahasan. Selesaikan review dan kategori yang tertunda dulu." : err.message === "insight cutoff mismatch" ? "Pembahasan lama tidak cocok dengan rentang ini. Coba lagi nanti." : err.message === "insight polling timeout" ? "Pembahasan belum selesai. Periksa lagi nanti." : "Pembahasan belum dapat dibuat. Data dan bukti tetap tersedia.");
    } finally { if (!controller.signal.aborted) setInsightLoading(false); }
  }
  useEffect(() => () => insightAbort.current?.abort(), []);

  const meeting = selection.view === "cycle" && facts?.period.kind === "SALARY_CYCLE" && facts.period.state === "CLOSED" && Boolean(selection.step);
  const step = meeting ? selection.step : "";
  useEffect(() => {
    if (!step) return;
    const targetId = step === "changes" || step === "drivers" ? "where" : step;
    document.getElementById(targetId)?.querySelector("h2, h3")?.focus();
  }, [step, facts]);

  if (!user) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  const selectedCategory = selection.category ? facts?.categoryChanges.find(item => categoryKey(item) === selection.category) : null;
  const adjacent = adjacentCycles(facts?.cycles, facts?.period?.start);
  const chooseCategory = (category, element) => {
    trigger.current = element || null;
    drawerPushed.current = true;
    navigate({ category, ...(meeting ? { step: "drivers" } : {}) });
  };
  const closeCategory = () => {
    if (drawerPushed.current) { drawerPushed.current = false; window.history.back(); }
    else navigate({ category: "" }, { replace: true });
  };
  const refreshing = selection.view === "cycle" && loading && Boolean(facts);
  const chooseReport = id => {
    if (id === "wealth") navigate({ view: "wealth" });
    else if (selection.view === "wealth") navigate({ view: "cycle" });
  };
  const wealthView = selection.view === "wealth";
  const { advisory: wealthNotices } = splitDataQuality(facts?.dataQuality);
  const plannedDays = facts?.period?.state === "ACTIVE" ? provisionalDays(facts.period) : 0;
  const statusLine = facts
    ? facts.period.state === "ACTIVE"
      ? `Berjalan · hari ke-${facts.spendingShape.days}${plannedDays ? ` dari ±${plannedDays}` : ""} · data s.d. ${dayLabel(inclusiveEnd(facts.period.measuredUntil))}`
      : `Ditutup · ${facts.spendingShape.days} hari · data s.d. ${dayLabel(inclusiveEnd(facts.period.measuredUntil))}`
    : "";

  const jumpToQuality = event => {
    if (meeting) {
      event.preventDefault();
      navigate({ step: "quality" });
      return;
    }
    setQualityOpenTrigger(count => count + 1);
  };

  return <AppShell user={user} eyebrow="Analisis" title="Laporan" headerAside={<ReportSwitch view={selection.view} choose={chooseReport}/>}>
    <div className="analytics-flow">
      {wealthView ? <div className="analytics-panel" role="tabpanel" id="laporan-panel" aria-labelledby="laporan-tab-wealth"><WealthWorkspace user={user}/></div> :
      <div id="laporan-panel" role="tabpanel" aria-labelledby="laporan-tab-cycle" className="cycle-review" data-meeting-step={step || undefined} data-stale={refreshing ? "true" : undefined} aria-busy={refreshing || undefined}>
      <div className="range-controls">
        <div className="range-control-group" data-slide data-slide-panel=".cycle-review > :not(.range-controls)" role="group" aria-label="Tampilan analisis">
          <button type="button" aria-pressed={selection.view === "cycle"} className={selection.view === "cycle" ? "active" : "secondary"} onClick={() => navigate({ view: "cycle" })}>Siklus Gaji</button>
          <button type="button" aria-pressed={selection.view === "calendar"} className={selection.view === "calendar" ? "active" : "secondary"} onClick={() => navigate({ view: "calendar" })}>Kalender</button>
        </div>
        {selection.view === "cycle" && <><div className="cycle-picker">
          <button type="button" className="secondary cycle-step-button" disabled={!adjacent.older} aria-label="Siklus sebelumnya" title="Siklus sebelumnya" onClick={() => navigate({ cycle: adjacent.older, category: "", step: "" })}><span aria-hidden="true">‹</span></button>
          <label className="cycle-selector"><span className="visually-hidden">Siklus yang ditinjau</span>
            <select value={selection.cycle} onChange={event => navigate({ cycle: event.target.value, category: "", step: "" })}>
              <option value="">Siklus berjalan</option>
              {selection.cycle && !facts?.cycles?.some(item => item.start === selection.cycle) && <option value={selection.cycle}>{dayLabel(selection.cycle)}</option>}
              {(facts?.cycles || []).map(period => <option key={period.start} value={period.start}>{cycleLabel(period)}</option>)}
            </select>
          </label>
          <button type="button" className="secondary cycle-step-button" disabled={!adjacent.newer} aria-label="Siklus berikutnya" title="Siklus berikutnya" onClick={() => navigate({ cycle: adjacent.newer, category: "", step: "" })}><span aria-hidden="true">›</span></button>
        </div>
        {facts && !error && <div className="cycle-status" aria-live="polite">
          <strong>{statusLine}</strong>
          {wealthNotices.length > 0 && <a className="quality-chip" href={meeting ? selectionHref({ ...selection, step: "quality" }) : "#quality"} onClick={jumpToQuality}>{wealthNotices.length} perlu dilengkapi</a>}
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
          <LastCycleCard facts={facts} meeting={meeting} onReviewPrevious={start => navigate({ cycle: start, category: "", step: "position" })}/>
          <KpiStrip facts={facts} updated={valueUpdated} meeting={meeting} onStartReview={() => navigate({ step: "position" })}/>
          <PaceCard facts={facts} step={step}/>
          <CategoryTable facts={facts} step={step} selected={selection.category} select={chooseCategory} palette={palette}/>
          <div className="dash-row dash-row-split">
            <HistoryCard facts={facts} step={step} selectedCategory={selection.category} onSelect={start => navigate({ cycle: start, category: "", step: "" })} onSelectCategory={chooseCategory} palette={palette}/>
            <MerchantCard facts={facts} step={step}/>
          </div>
          <DailyCard facts={facts} step={step}/>
          <div className="review-stack surface card">
          <section id="family-talk" className="review-section" aria-labelledby="family-talk-title">
            <details className="report-disclosure" open={step === "discussion" || step === "decisions" || Boolean(drafts[cycleStart]) || undefined}>
              <summary>
                <h2 id="family-talk-title" tabIndex={-1}>Obrolan keluarga</h2>
                <span>{[
                  facts.memberAttribution.length ? `catatan ${facts.memberAttribution.length} pencatat` : "belum ada pencatat",
                  insightError ? "bahan pembahasan belum tersedia" : insightLoading || insight?.status === "PENDING" ? "bahan pembahasan diproses" : insight?.status === "SUCCEEDED" ? "bahan pembahasan tersedia" : "bahan pembahasan",
                  drafts[cycleStart] ? "draf keputusan belum disimpan" : !cycleStart ? "keputusan" : decisionMeta.status === "ready" ? `${decisionMeta.count} keputusan` : decisionMeta.status === "error" ? "keputusan belum tersedia" : "keputusan",
                ].join(" · ")}</span>
              </summary>
              <div className="report-disclosure-body family-talk-sections">
                <HouseholdSection members={facts.memberAttribution}/>
                <section id="discussion" className="family-talk-part" aria-labelledby="discussion-title">
                  <h3 id="discussion-title" tabIndex={-1}>Bahan pembahasan</h3>
                  <InsightCard insight={insight} cycle={facts.period} days={facts.spendingShape.days} loading={insightLoading} error={insightError} canGenerate={Boolean(cycleStart)} onGenerate={generateInsight}/>
                </section>
                {cycleStart && <CycleDecisions key={cycleStart} cycleStart={cycleStart} closed={facts.period.state === "CLOSED"} range={cycleLabel(facts.period)} body={drafts[cycleStart] || ""} onBodyChange={body => setDrafts(current => ({ ...current, [cycleStart]: body }))} onSummary={setDecisionMeta}/>}
              </div>
            </details>
          </section>
          <QualityFooter facts={facts} step={step} openTrigger={qualityOpenTrigger}/>
          </div>
          <Presence show={selectedCategory && (!meeting || step === "drivers")}>{() => <CategoryDrawer item={selectedCategory} facts={facts} period={{ ...facts.period, reviewStep: step }} close={closeCategory} triggerRef={trigger} palette={palette}/>}</Presence>
        </>}
      </>}
      </div>}
    </div>
  </AppShell>;
}
