"use client";

import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import AppShell from "../components/AppShell";
import { CategoryRankingChart, CyclePaceChart, CycleSpendingPatternChart } from "../components/Charts";
import CycleLedger from "../components/CycleLedger";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import InsightCard from "../components/InsightCard";
import CycleDecisions from "../components/CycleDecisions";
import useAuth from "../components/useAuth";
import { dayLabel } from "../lib/chartData";
import { adjacentCycles, paceReferences, verdictPairs } from "../lib/cycleLedger";
import { cycleLabel, ratioLabel, readReviewSelection, selectionHref, transactionHref } from "../lib/cycleReview";
import { money } from "../lib/format";
import { pollInsight, selectCycleInsight } from "../lib/insightData";
import { SectionTitle, Metric } from "./shared";
import { MeetingNav, CyclePosition, ComparisonContext, ChangesTable, MerchantTable, CategoryDrivers, SavingsWealth, QualitySection } from "./CycleSections";
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

  function navigate(change) {
    window.history.pushState(null, "", selectionHref({ ...selection, ...change }));
  }

  useEffect(() => {
    if (!user || selection.view !== "cycle") return;
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
  const [allOpen, setAllOpen] = useState(false);
  useEffect(() => { setAllOpen(false); }, [cycleStart]);
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
  const selectedCategory = selection.category ? facts?.categoryChanges.find(item => (item.id || "uncategorized") === selection.category) : null;
  const adjacent = adjacentCycles(facts?.cycles, facts?.period?.start);
  // Choosing the selected category again clears it; in a meeting, choosing one moves on to its evidence.
  const chooseCategory = category => navigate({ category: category === selection.category ? "" : category, ...(meeting && category !== selection.category ? { step: "drivers" } : {}) });
  // Choosing a category opens its evidence below without moving the reader; this is the one place focus follows an explicit request.
  const showEvidence = event => { event.preventDefault(); const panel = document.querySelector("#drivers > details"); if (panel) panel.open = true; const title = document.getElementById("drivers-title"); title?.focus(); title?.scrollIntoView({ block: "start" }); };
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
        </div></>}
      </div>
      {selection.view === "calendar" ? <CalendarReview selection={selection} navigate={navigate}/> : <>
        <ErrorNotice message={error} retry={() => setReload(value => value + 1)}/>
        {error && <button type="button" className="secondary" onClick={() => navigate({ cycle: "", category: "" })}>Siklus berjalan</button>}
        {loading && !facts && <Skeleton cards={3} rows={4}/>}
        {refreshing && <p className="ledger-loading" role="status">Memuat siklus…</p>}
        {facts && !error && <>
          <div className="cycle-period" aria-live="polite">
            <strong>{cycleLabel(facts.period)}</strong>
            <span>{facts.period.state === "ACTIVE" ? `Berjalan · hari ke-${facts.spendingShape.days}, hari ini belum penuh` : `Ditutup · ${facts.spendingShape.days} hari`} · Asia/Jakarta</span>
            <small>Data sampai {dayLabel(facts.period.measuredUntil)} (batas akhir tidak termasuk).</small>
            {facts.period.kind !== "SALARY_CYCLE" && <p>Menampilkan bulan kalender sementara. Belum ada gaji utama terkonfirmasi untuk menentukan siklus.</p>}
            {facts.period.kind === "SALARY_CYCLE" && facts.period.state === "CLOSED" && !meeting && <button type="button" onClick={() => navigate({ step: "position" })}>Tinjau siklus ini</button>}
            {!meeting && <button type="button" className="secondary" onClick={event => { const next = !allOpen; event.currentTarget.closest(".cycle-review").querySelectorAll("details").forEach(details => { details.open = next; }); setAllOpen(next); }}>{allOpen ? "Tutup semua detail" : "Buka semua detail"}</button>}
          </div>
          {meeting && <MeetingNav step={step} selection={selection} navigate={navigate}/>}
          {facts.history?.length > 0 && <CycleLedger history={facts.history} selected={facts.period.start} median={facts.period.state === "CLOSED" ? facts.comparison.expense.median3 : null} verdict={verdictPairs(facts)} onSelect={start => navigate({ cycle: start, category: "", step: "" })} categoryHistory={facts.categoryHistory} selectedCategory={selection.category} selectable={facts.categoryChanges.map(item => item.id || "uncategorized")} onSelectCategory={chooseCategory}/>}
          {selectedCategory && !meeting && <p className="ledger-selection" role="status"><span>Bukti kategori <strong>{selectedCategory.name}</strong> terbuka di bagian bawah.</span> <a href="#drivers-title" onClick={showEvidence}>Lihat bukti</a><button type="button" className="secondary" onClick={() => navigate({ category: "" })}>Hapus pilihan</button></p>}
          <CyclePosition facts={facts}/>
          <section id="spending-shape" className="review-section analytics-chart" aria-labelledby="shape-title">
            <SectionTitle id="shape-title" about="pola pengeluaran" title={facts.period.state === "ACTIVE" ? "Pola pengeluaran siklus ini" : "Pola pengeluaran siklus terpilih"} description="Kapan pengeluaran terjadi? Nilai harian sudah dikurangi refund; transfer tidak termasuk."/>
            <div className="shape-charts">
              <CycleSpendingPatternChart items={facts.daily} average={facts.spendingShape.averageDailyExpense} height={260}/>
              <CyclePaceChart items={facts.daily} references={paceReferences(facts)} pace={facts.pace} height={260}/>
            </div>
            <dl className="review-context">
              <Metric label="Rata-rata per hari" value={money(String(Math.round(Number(facts.spendingShape.averageDailyExpense))))}/>
              <Metric label="Hari tertinggi" value={facts.spendingShape.peakDay ? `${dayLabel(facts.spendingShape.peakDay)} · ${money(facts.spendingShape.peakExpense)}` : "Belum ada"}/>
              <Metric label="Porsi hari tertinggi" value={ratioLabel(facts.spendingShape.peakShareOfExpense)}/>
              <Metric label="Hari tanpa pengeluaran bersih" value={`${facts.spendingShape.zeroSpendDays} hari`}/>
            </dl>
            <details className="review-daily"><summary>Lihat nilai harian</summary><div className="review-table-wrap"><table><caption className="visually-hidden">Pengeluaran bersih harian</caption><thead><tr><th scope="col">Tanggal</th><th scope="col">Pengeluaran</th><th scope="col">Refund</th><th scope="col">Total sampai hari ini</th></tr></thead><tbody>{facts.daily.map(item => <tr key={item.period}><th scope="row">{dayLabel(item.period)}</th><td>{money(item.expense)}</td><td>{money(item.refund)}</td><td>{item.cumulativeExpense == null ? "—" : money(item.cumulativeExpense)}</td></tr>)}</tbody></table></div></details>
          </section>
          <section id="changes" className="review-section" aria-labelledby="changes-title">
            <details className="report-disclosure" open={step === "changes"}>
            <summary><h2 id="changes-title" tabIndex={-1}>Detail perubahan</h2><span>Pembanding, selisih per kategori, tabel lengkap</span></summary>
            <p className="review-description">Perubahan kategori diurutkan berdasarkan selisih absolut oleh server. Besar perubahan bukan penilaian baik atau buruk.</p>
            <ComparisonContext comparison={facts.comparison} period={facts.period}/>
            <ChangesTable items={facts.categoryChanges} comparison={facts.comparison} selected={selection.category} select={chooseCategory}/>
            </details>
          </section>
          <section id="drivers" className="review-section" aria-labelledby="drivers-title">
            <details key={selection.category || "none"} className="report-disclosure" open={Boolean(selectedCategory) || step === "drivers"}>
            <summary><h2 id="drivers-title" tabIndex={-1}>Bukti kategori</h2><span>{selectedCategory?.name || "Pilih kategori"}</span></summary>
            <label className="driver-selector">Kategori
              <select value={selectedCategory ? selectedCategory.id || "uncategorized" : ""} onChange={event => navigate({ category: event.target.value })}><option value="">Pilih kategori</option>{facts.categoryChanges.map(item => <option key={item.id || "uncategorized"} value={item.id || "uncategorized"}>{item.name}</option>)}</select>
            </label>
            <div id="category-drivers" aria-live="polite">
              {selectedCategory ? <CategoryDrivers item={selectedCategory} period={{ ...facts.period, reviewStep: step }}/> : <p className="empty compact">Pilih kategori di atas atau melalui tabel perubahan.</p>}
            </div>
            </details>
          </section>
          <section id="destinations" className="review-section" aria-labelledby="destinations-title">
            <details className="report-disclosure"><summary><h2 id="destinations-title">Distribusi & merchant</h2><span>{facts.categoryChanges.length} kategori · {facts.merchantDrivers.length} merchant</span></summary>
            <p className="review-description">Distribusi pengeluaran bersih per kategori. Merchant diurutkan berdasarkan perubahan absolut dibanding siklus sebelumnya.</p>
            <div className="review-split"><div><CategoryRankingChart items={facts.categoryChanges} serverOwned height={260}/><a href={transactionHref(facts.period)}>Lihat seluruh pengeluaran periode ini</a></div><MerchantTable items={facts.merchantDrivers} period={facts.period}/></div>
            </details>
          </section>
          <section id="household" className="review-section" aria-labelledby="household-title">
            <details className="report-disclosure"><summary><h2 id="household-title">Catatan rumah tangga</h2><span>{facts.memberAttribution.length ? `${facts.memberAttribution.length} pencatat` : "Belum ada atribusi"}</span></summary>
            <p className="review-description">Siapa yang memulai pencatatan, ketika diketahui. Ini bukan peringkat tanggung jawab atau perbandingan kebiasaan.</p>
            <dl className="review-attribution">{facts.memberAttribution.map(item => <Metric key={item.id || item.name} label={item.name} value={`${money(item.amount)} · ${item.count} transaksi`}/>)}</dl>
            {!facts.memberAttribution.length && <p className="empty compact">Belum ada pengeluaran yang dapat diatribusikan.</p>}
            </details>
          </section>
          <SavingsWealth facts={facts}/>
          <QualitySection facts={{ ...facts, period: { ...facts.period, reviewStep: step } }}/>
          <section id="discussion" className="review-section" aria-labelledby="discussion-title">
            <details className="report-disclosure" open={step === "discussion"}>
            <summary><h2 id="discussion-title" tabIndex={-1}>Bahan pembahasan</h2><span>{insightError ? "Belum tersedia" : insightLoading || insight?.status === "PENDING" ? "Memproses" : insight?.status === "SUCCEEDED" ? "Tersedia · opsional" : "Opsional"}</span></summary>
            <p className="review-description">Bukan saran keuangan atau keputusan rumah tangga.</p>
            <InsightCard insight={insight} cycle={facts.period} loading={insightLoading} error={insightError} canGenerate={Boolean(cycleStart)} onGenerate={generateInsight}/>
            <a href={meeting ? selectionHref({ ...selection, step: "changes" }) : "#changes"}>Lihat data pendukung pembahasan</a>
            </details>
          </section>
          {cycleStart && <CycleDecisions key={cycleStart} expanded={step === "decisions"} cycleStart={cycleStart} closed={facts.period.state === "CLOSED"} body={drafts[cycleStart] || ""} onBodyChange={body => setDrafts(current => ({ ...current, [cycleStart]: body }))}/>}
        </>}
      </>}
    </div>
  </AppShell>;
}
