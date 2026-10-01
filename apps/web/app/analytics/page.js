"use client";

import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import AppShell from "../components/AppShell";
import { CategoryRankingChart, CyclePaceChart, CycleSpendingPatternChart, MonthlyCashflowChart } from "../components/Charts";
import CycleLedger from "../components/CycleLedger";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import InsightCard from "../components/InsightCard";
import CycleDecisions from "../components/CycleDecisions";
import useAuth from "../components/useAuth";
import { dayLabel } from "../lib/chartData";
import { adjacentCycles, paceReferences, verdictPairs } from "../lib/cycleLedger";
import { amountLabel, changeWidth, cycleLabel, measuredLabel, qualityCopy, ratioLabel, readReviewSelection, reviewSteps, selectionHref, signedMoney, transactionHref } from "../lib/cycleReview";
import { dateTime, money, typeLabel } from "../lib/format";
import { pollInsight, selectCycleInsight } from "../lib/insightData";

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
  useEffect(() => {
    if (!selection.category || step || loading) return;
    document.getElementById("drivers-title")?.focus();
  }, [selection.category, step, loading]);

  if (!user) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  const selectedCategory = selection.category ? facts?.categoryChanges.find(item => (item.id || "uncategorized") === selection.category) : null;
  const adjacent = adjacentCycles(facts?.cycles, facts?.period?.start);
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
            {!meeting && <button type="button" className="secondary" onClick={event => event.currentTarget.closest(".cycle-review").querySelectorAll("details").forEach(details => { details.open = true; })}>Buka semua detail</button>}
          </div>
          {meeting && <MeetingNav step={step} selection={selection} navigate={navigate}/>}
          {facts.history?.length > 0 && <CycleLedger history={facts.history} selected={facts.period.start} median={facts.period.state === "CLOSED" ? facts.comparison.expense.median3 : null} verdict={verdictPairs(facts)} onSelect={start => navigate({ cycle: start, category: "", step: "" })}/>}
          <CyclePosition facts={facts}/>
          <section id="spending-shape" className="review-section analytics-chart" aria-labelledby="shape-title">
            <SectionTitle id="shape-title" title={facts.period.state === "ACTIVE" ? "Pola pengeluaran siklus ini" : "Pola pengeluaran siklus terpilih"} description="Kapan pengeluaran terjadi? Nilai harian sudah dikurangi refund; transfer tidak termasuk."/>
            <CycleSpendingPatternChart items={facts.daily} average={facts.spendingShape.averageDailyExpense} height={260}/>
            <CyclePaceChart items={facts.daily} references={paceReferences(facts)} height={220}/>
            <dl className="review-context">
              <Metric label="Rata-rata per hari" value={money(String(Math.round(Number(facts.spendingShape.averageDailyExpense))))}/>
              <Metric label="Hari tertinggi" value={facts.spendingShape.peakDay ? `${dayLabel(facts.spendingShape.peakDay)} · ${money(facts.spendingShape.peakExpense)}` : "Belum ada"}/>
              <Metric label="Porsi hari tertinggi" value={ratioLabel(facts.spendingShape.peakShareOfExpense)}/>
              <Metric label="Hari tanpa pengeluaran bersih" value={`${facts.spendingShape.zeroSpendDays} hari`}/>
            </dl>
            <details className="review-daily"><summary>Lihat nilai harian</summary><div className="review-table-wrap"><table><caption className="visually-hidden">Pengeluaran bersih harian</caption><thead><tr><th scope="col">Tanggal</th><th scope="col">Pengeluaran</th><th scope="col">Refund</th></tr></thead><tbody>{facts.daily.map(item => <tr key={item.period}><th scope="row">{dayLabel(item.period)}</th><td>{money(item.expense)}</td><td>{money(item.refund)}</td></tr>)}</tbody></table></div></details>
          </section>
          <section id="changes" className="review-section" aria-labelledby="changes-title">
            <SectionTitle id="changes-title" title="Apa yang berubah?" description="Perubahan kategori diurutkan berdasarkan selisih absolut oleh server. Besar perubahan bukan penilaian baik atau buruk."/>
            <ComparisonContext comparison={facts.comparison} period={facts.period}/>
            <ChangesTable items={facts.categoryChanges} comparison={facts.comparison} selected={selection.category} select={category => navigate({ category, ...(meeting ? { step: "drivers" } : {}) })}/>
          </section>
          <section id="drivers" className="review-section" aria-labelledby="drivers-title">
            <details className="report-disclosure" open={Boolean(selectedCategory) || step === "drivers"}>
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
            <details className="report-disclosure"><summary><h2 id="destinations-title">Distribusi & merchant</h2><span>Detail pengeluaran</span></summary>
            <p className="review-description">Distribusi pengeluaran bersih per kategori. Merchant diurutkan berdasarkan perubahan absolut dibanding siklus sebelumnya.</p>
            <div className="review-split"><div><CategoryRankingChart items={facts.categoryChanges} serverOwned height={260}/><a href={transactionHref(facts.period)}>Lihat seluruh pengeluaran periode ini</a></div><MerchantTable items={facts.merchantDrivers} period={facts.period}/></div>
            </details>
          </section>
          <section id="household" className="review-section" aria-labelledby="household-title">
            <details className="report-disclosure"><summary><h2 id="household-title">Catatan rumah tangga</h2><span>Atribusi pencatatan</span></summary>
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

function SectionTitle({ id, title, description }) {
  return <div className="section-title"><h2 id={id} tabIndex={-1}>{title}</h2>{description && <details className="review-explainer"><summary>Tentang data ini</summary><p>{description}</p></details>}</div>;
}

function MeetingNav({ step, selection, navigate }) {
  const index = reviewSteps.findIndex(([id]) => id === step);
  return <div className="meeting-controls">
    <p role="status">{index + 1} / {reviewSteps.length} · {reviewSteps[index][1]} <small>Sesi tinjauan; keputusan disimpan terpisah.</small></p>
    <nav aria-label="Langkah tinjauan siklus"><ol>{reviewSteps.map(([id, label], i) => <li key={id}><button type="button" className={id === step ? "active" : "secondary"} aria-current={id === step ? "step" : undefined} onClick={() => navigate({ step: id })}>{i + 1}. {label}</button></li>)}</ol></nav>
    <div className="meeting-actions">
      <button type="button" className="secondary" disabled={index === 0} onClick={() => navigate({ step: reviewSteps[index - 1][0] })}>Langkah sebelumnya</button>
      {index < reviewSteps.length - 1 && <button type="button" onClick={() => navigate({ step: reviewSteps[index + 1][0] })}>Langkah berikutnya</button>}
      <button type="button" className="secondary" onClick={() => navigate({ step: "" })}>{index === reviewSteps.length - 1 ? "Selesai meninjau" : "Tampilkan seluruh tinjauan"}</button>
    </div>
  </div>;
}

function Metric({ label, value }) {
  return <div><dt>{label}</dt><dd>{value}</dd></div>;
}

function CyclePosition({ facts }) {
  return <section id="position" className="review-section cycle-position" aria-labelledby="position-title">
    <SectionTitle id="position-title" title="Posisi siklus" description="Hanya transaksi terkonfirmasi. Pengeluaran bersih setelah refund; transfer bukan pengeluaran."/>
    <dl className="cycle-outcome">
      <div className="cycle-net"><dt>Arus kas bersih</dt><dd>{money(facts.cashflow.netCashflow)}</dd></div>
      <Metric label="Pemasukan" value={money(facts.cashflow.income)}/>
      <Metric label="Pengeluaran bersih" value={money(facts.cashflow.expense)}/>
      <Metric label="Tabungan dialokasikan" value={money(facts.cashflow.savingsAllocated)}/>
      <Metric label="Surplus belum dialokasikan" value={money(facts.cashflow.unallocatedSurplus)}/>
    </dl>
    <div className="cycle-footnote"><span>Refund <strong>{money(facts.cashflow.refund)}</strong></span><details className="review-explainer"><summary>Tentang surplus</summary><p>Surplus belum dialokasikan adalah arus kas bersih dikurangi transfer tabungan terkonfirmasi, bukan transaksi tambahan.</p></details></div>
  </section>;
}

function ComparisonContext({ comparison, period }) {
  const elapsed = comparison.mode === "ELAPSED_DAYS";
  const rows = [[elapsed ? "Siklus ini · berjalan" : "Siklus ini", comparison.expense.amount], ["Sebelumnya · penuh", comparison.expense.previousFullCycle], ...(elapsed ? [["Sebelumnya · hari setara", comparison.expense.previous]] : []), [elapsed ? "Median 3 · hari setara" : "Median 3", comparison.expense.median3]];
  const maximum = Math.max(0, ...rows.map(([, value]) => Math.abs(Number(value))));
  return <div className="review-baseline">
    <div className="baseline-meta"><span>{measuredLabel(period)}{elapsed && " · berjalan"}</span><span>{comparison.previousFullCycle ? `Sebelumnya · penuh: ${measuredLabel(comparison.previousFullCycle)}` : "Belum ada siklus pembanding"}</span>{elapsed && <span>Hari setara, bukan siklus penuh: {measuredLabel(comparison.previous)}</span>}<span>{comparison.median3Available ? "Median 3 siklus tersedia" : `Median belum tersedia · ${comparison.eligibleCycles}/3 siklus`}</span></div>
    <dl className="comparison-bars" aria-label="Perbandingan pengeluaran bersih">
      {rows.map(([label, value]) => <div key={label}><dt>{label}</dt><dd><span aria-hidden="true" className="comparison-track"><i data-negative={String(value).startsWith("-") || undefined} style={{ width: maximum ? `${Math.abs(Number(value)) / maximum * 100}%` : "0%" }}/></span><strong>{amountLabel(value)}</strong></dd></div>)}
    </dl>
    <dl className="comparison-deltas">
      <Metric label="Δ siklus sebelumnya · penuh" value={`${signedMoney(comparison.expense.deltaVsPreviousFullCycle)} · ${ratioLabel(comparison.expense.relativeDeltaVsPreviousFullCycle)}`}/>
      {elapsed && <Metric label="Δ hari setara" value={signedMoney(comparison.expense.deltaVsPrevious)}/>}
      <Metric label={elapsed ? "Δ median 3 · hari setara" : "Δ median 3"} value={signedMoney(comparison.expense.deltaVsMedian3)}/>
    </dl>
  </div>;
}

function ChangesTable({ items, comparison, selected, select }) {
  if (!items.length) return <p className="empty compact">Belum ada pengeluaran kategori pada periode ini atau pembandingnya.</p>;
  const elapsed = comparison.mode === "ELAPSED_DAYS";
  const deltaLabel = elapsed ? "Δ hari setara" : "Δ sebelumnya";
  return <>
    <div className="change-ranking-head" aria-hidden="true"><span>Kategori</span><span>{elapsed ? "Siklus berjalan" : "Siklus ini"}</span><span>Δ siklus sebelumnya · penuh</span></div>
    <ul className="change-ranking" aria-label="Perubahan kategori">
      {items.map(item => <li key={item.id || "uncategorized"} data-selected={selected === (item.id || "uncategorized")}>
        <button className="change-button" type="button" aria-controls="category-drivers" aria-pressed={selected === (item.id || "uncategorized")} onClick={() => select(item.id || "uncategorized")}>{item.name}</button>
        <span><span className="visually-hidden">Siklus ini: </span>{money(item.amount)}</span>
        <span><span className="visually-hidden">Δ siklus sebelumnya · penuh: </span>{signedMoney(item.deltaVsPreviousFullCycle)}<small> · {ratioLabel(item.relativeDeltaVsPreviousFullCycle)}</small></span>
        <span className="change-track" aria-hidden="true"><i data-negative={String(item.deltaVsPreviousFullCycle).startsWith("-") || undefined} style={{ width: changeWidth(item, items, "deltaVsPreviousFullCycle") }}/></span>
      </li>)}
    </ul>
    <small>Batang menunjukkan besar selisih relatif, bukan persentase kenaikan.</small>
    <details className="review-daily"><summary>Perbandingan lengkap · median, persentase & kontribusi</summary>
    <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Perbandingan kategori lengkap, geser untuk semua kolom"><table className="changes-table">
    <caption>Perubahan kategori. Persentase tanpa pembanding positif ditampilkan sebagai —. Sebelumnya · penuh: {measuredLabel(comparison.previousFullCycle)}.</caption>
    <thead><tr><th scope="col">Kategori / bukti</th><th scope="col">Siklus ini</th><th scope="col">{elapsed ? "Sebelumnya · hari setara" : "Sebelumnya · penuh"}</th><th scope="col">{elapsed ? "Median 3 · hari setara" : "Median 3"}</th><th scope="col">{deltaLabel}</th><th scope="col">{elapsed ? "Selisih vs median · hari setara" : "Selisih vs median"}</th><th scope="col">{elapsed ? "Kontribusi ke selisih hari setara" : "Kontribusi ke selisih total"}</th>{elapsed && <><th scope="col">Sebelumnya · penuh</th><th scope="col">Δ siklus sebelumnya · penuh</th></>}</tr></thead>
    <tbody>{items.map(item => <tr key={item.id || "uncategorized"} data-selected={selected === (item.id || "uncategorized")}>
      <th scope="row">{item.name}</th>
      <td>{money(item.amount)}</td><td>{amountLabel(item.previous)}</td><td>{amountLabel(item.median3)}</td>
      <td>{signedMoney(item.deltaVsPrevious)}<small>{ratioLabel(item.relativeDeltaVsPrevious)}</small></td>
      <td>{signedMoney(item.deltaVsMedian3)}<small>{ratioLabel(item.relativeDeltaVsMedian3)}</small></td>
      <td>{ratioLabel(item.contributionToExpenseChange)}</td>
      {elapsed && <><td>{amountLabel(item.previousFullCycle)}</td><td>{signedMoney(item.deltaVsPreviousFullCycle)}<small>{ratioLabel(item.relativeDeltaVsPreviousFullCycle)}</small></td></>}
    </tr>)}</tbody>
  </table></div></details></>;
}

function MerchantTable({ items, period, categoryId }) {
  if (!items.length) return <p className="empty compact">Belum ada merchant pendukung.</p>;
  const elapsed = period.state === "ACTIVE";
  return <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Merchant pendukung, geser untuk semua kolom"><table><caption>Merchant pendukung (maks. 10). Nilai bersih setelah refund.</caption><thead><tr><th scope="col">Merchant</th><th scope="col">Siklus ini</th><th scope="col">Sebelumnya · penuh</th><th scope="col">Δ siklus sebelumnya · penuh</th>{elapsed && <><th scope="col">Sebelumnya · hari setara</th><th scope="col">Δ hari setara</th></>}<th scope="col">{elapsed ? "Median 3 · hari setara" : "Median 3"}</th></tr></thead><tbody>{items.map((item, index) => <tr key={`${item.id}:${index}`}><th scope="row">{item.id ? <a href={transactionHref(period, { merchantId: item.id, categoryId })}>{item.name}</a> : item.name}</th><td>{money(item.amount)}</td><td>{amountLabel(item.previousFullCycle)}</td><td>{signedMoney(item.deltaVsPreviousFullCycle)}<small>{ratioLabel(item.relativeDeltaVsPreviousFullCycle)}</small></td>{elapsed && <><td>{amountLabel(item.previous)}</td><td>{signedMoney(item.deltaVsPrevious)}</td></>}<td>{amountLabel(item.median3)}</td></tr>)}</tbody></table></div>;
}

function CategoryDrivers({ item, period }) {
  return <div className="category-evidence">
    <h3>{item.name}</h3>
    <dl className="review-context"><Metric label="Porsi pengeluaran siklus" value={ratioLabel(item.shareOfExpense)}/><Metric label="Jumlah transaksi" value={item.count}/></dl>
    <MerchantTable items={item.merchants} period={period} categoryId={item.id || "uncategorized"}/>
    <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Transaksi pendukung"><table><caption>Transaksi pendukung terbesar (maks. 10). Refund ditandai terpisah.</caption><thead><tr><th scope="col">Merchant / tanggal</th><th scope="col">Jenis</th><th scope="col">Jumlah</th><th scope="col">Bukti</th></tr></thead><tbody>{item.transactions.map(transaction => <tr key={transaction.id}><th scope="row">{transaction.merchant}<small>{dateTime(transaction.transactionAt)}</small></th><td>{typeLabel[transaction.type]}</td><td>{money(transaction.amount)}</td><td><a href={transactionHref(period, { categoryId: item.id || "uncategorized", id: transaction.id })}>Buka transaksi</a></td></tr>)}</tbody></table></div>
    {!item.transactions.length && <p className="empty compact">Tidak ada transaksi terkonfirmasi kategori ini pada siklus terpilih.</p>}
    <a href={transactionHref(period, { categoryId: item.id || "uncategorized" })}>Lihat transaksi {item.name} dalam periode ini</a>
  </div>;
}

function SavingsWealth({ facts }) {
  const { wealth, cashflow } = facts;
  return <section id="savings-wealth" className="review-section" aria-labelledby="savings-title">
    <SectionTitle id="savings-title" title="Tabungan & kekayaan" description="Alokasi tabungan adalah transfer terkonfirmasi; kekayaan adalah pengamatan saldo, bukan transaksi."/>
    <div className="review-split">
      <div><h3>Ke mana surplus dialokasikan?</h3><dl className="review-context">
        <Metric label="Tabungan dialokasikan" value={money(cashflow.savingsAllocated)}/><Metric label="Belum dialokasikan" value={money(cashflow.unallocatedSurplus)}/>
      </dl>
      <dl className="review-destinations">{facts.savingsDestinations.map(item => <Metric key={item.id || item.name} label={item.name} value={money(item.amount)}/>)}</dl>
      {!facts.savingsDestinations.length && <p>Belum ada alokasi tabungan terkonfirmasi.</p>}
      </div>
      <div><h3>Pergerakan kekayaan</h3><dl className="review-context">
        <Metric label="Perubahan kekayaan bersih" value={signedMoney(wealth.netWorthChange)}/>
      </dl>
      {wealth.netWorthChange == null && <p>Belum dapat direkonsiliasi.</p>}
      <details className="review-daily"><summary>Rincian saldo & rekonsiliasi</summary><dl className="review-context">
        <Metric label="Kekayaan bersih sebelumnya" value={amountLabel(wealth.previous?.netWorth)}/><Metric label="Kekayaan bersih terbaru" value={amountLabel(wealth.current?.netWorth)}/>
        <Metric label="Kontribusi arus kas terkonfirmasi" value={amountLabel(wealth.confirmedCashflow)}/>
        <Metric label="Valuasi & perubahan lain" value={signedMoney(wealth.valuationAndOtherChange)}/>
      </dl>
      <details className="review-explainer"><summary>Tentang rekonsiliasi</summary><p>Rekonsiliasi mengikuti selang waktu pengamatan, bukan saldo akhir siklus yang diperkirakan. Selisih lainnya bukan laba investasi atau transfer tabungan.</p></details>
      {[["Sebelumnya", wealth.previous], ["Terbaru", wealth.current]].map(([label, snapshot]) => <p className="snapshot-context" key={label}>{label}: {snapshot ? <><a href={`/wealth?snapshotId=${encodeURIComponent(snapshot.id)}`}>{dateTime(snapshot.observedAt)}</a> · usia {snapshot.ageDays} hari pada batas pengukuran</> : "belum tersedia"}</p>)}
      {wealth.netWorthChange == null && <p>Pergerakan belum dapat direkonsiliasi. Periksa catatan dan kelengkapan akun.</p>}
      </details>
      <a href="/wealth">Buka detail Kekayaan</a></div>
    </div>
  </section>;
}

function QualitySection({ facts }) {
  return <section id="quality" className="review-section" aria-labelledby="quality-title">
    <SectionTitle id="quality-title" title="Kelengkapan data & tindak lanjut" description="Hal yang belum lengkap tetap terlihat. Tidak ada skor kepercayaan dari model."/>
    {!facts.dataQuality.length ? <p>Tidak ada kendala yang tercatat untuk periode ini.</p> : <ul className="review-quality">{facts.dataQuality.map(blocker => {
      const [label, action] = qualityCopy[blocker.kind] || ["Data tinjauan perlu dilengkapi", "Buka Inbox"];
      const counted = ["OPEN_REVIEWS", "UNCATEGORIZED_EXPENSE", "PROCESSING_INCOMPLETE"].includes(blocker.kind);
      return <li key={blocker.kind}><div><strong>{counted ? `${blocker.count} ` : ""}{label}</strong>{blocker.amount != null && <p>{money(blocker.amount)} terkonfirmasi.</p>}</div><a href={blocker.kind === "UNCATEGORIZED_EXPENSE" ? transactionHref(facts.period, { type: "EXPENSE", categoryId: "uncategorized" }) : ["/inbox", "/wealth", "/settings"].includes(blocker.action) ? blocker.action : "/inbox"}>{action}</a></li>;
    })}</ul>}
  </section>;
}

function CalendarReview({ selection, navigate }) {
  const [data, setData] = useState(null);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setData(null); setError("");
    const query = new URLSearchParams({ period: selection.from && selection.to ? "custom" : "calendar", range: selection.range });
    if (selection.from && selection.to) { query.set("from", selection.from); query.set("to", selection.to); }
    Promise.all(["cashflow", "spending", "categories", "merchants", "members"].map(async name => {
      const response = await fetch(`/api/v1/analytics/${name}?${query}`, { signal: controller.signal });
      if (!response.ok) throw new Error("Analisis kalender belum dapat dimuat. Periksa rentang lalu coba lagi.");
      return [name, await response.json()];
    })).then(entries => { if (!controller.signal.aborted) setData(Object.fromEntries(entries)); })
      .catch(err => { if (err.name !== "AbortError") setError(err.message); });
    return () => controller.abort();
  }, [selection.range, selection.from, selection.to, reload]);
  return <>
    <div className="range-controls"><div className="range-control-group">{["3", "6", "12"].map(range => <button type="button" key={range} className={selection.range === range && !selection.from ? "active" : "secondary"} aria-pressed={selection.range === range && !selection.from} onClick={() => navigate({ range, from: "", to: "" })}>{range} Bulan</button>)}</div>
      <form className="custom-range" onSubmit={event => { event.preventDefault(); const form = new FormData(event.currentTarget); navigate({ from: form.get("from"), to: form.get("to") }); }}><label>Bulan mulai<input name="from" type="month" defaultValue={selection.from} required/></label><label>Bulan selesai<input name="to" type="month" defaultValue={selection.to} required/></label><button className="secondary">Kustom</button></form>
    </div>
    <p>{selection.from && selection.to ? `Rentang bulan ${selection.from} sampai ${selection.to}` : `${selection.range} bulan terakhir`} · Asia/Jakarta</p>
    <ErrorNotice message={error} retry={() => setReload(value => value + 1)}/>
    {!data && !error && <Skeleton cards={1} rows={4}/>}
    {data && <>
      <section className="review-section analytics-chart"><SectionTitle title="Pemasukan vs pengeluaran" description="Bagaimana arus kas berubah antar bulan? Semua nilai berasal dari transaksi terkonfirmasi."/><MonthlyCashflowChart items={data.cashflow} height={280}/></section>
      <section className="review-section"><SectionTitle title="Arus kas per bulan"/><div className="review-table-wrap"><table><thead><tr><th scope="col">Bulan</th><th scope="col">Pemasukan</th><th scope="col">Pengeluaran</th><th scope="col">Arus kas bersih</th></tr></thead><tbody>{data.cashflow.map(item => <tr key={item.period}><th scope="row">{item.period}</th><td>{money(item.income)}</td><td>{money(item.expense)}</td><td>{money(item.netCashflow)}</td></tr>)}</tbody></table></div></section>
      <section className="review-section"><SectionTitle title="Distribusi kategori" description="Kategori mana yang menyusun pengeluaran rentang ini?"/><CategoryRankingChart items={data.categories}/></section>
      <section className="review-section"><SectionTitle title="Merchant" description="Pengeluaran bersih setelah refund dalam rentang kalender."/><ValueList items={data.merchants}/></section>
      <section className="review-section"><SectionTitle title="Catatan rumah tangga" description="Atribusi pencatatan ketika diketahui, bukan peringkat anggota."/><ValueList items={[...data.members].sort((a, b) => a.name.localeCompare(b.name, "id"))}/></section>
      <section className="review-section"><SectionTitle title="Pengeluaran setelah refund"/><ValueList items={data.spending.map(item => ({ name: item.period, amount: item.netSpending }))}/></section>
      <p>Pembahasan dan tinjauan rumah tangga tersedia pada mode Siklus Gaji.</p>
    </>}
  </>;
}

function ValueList({ items }) {
  return items.length ? <dl className="review-values">{items.map((item, index) => <Metric key={item.id || `${item.name}:${index}`} label={item.name} value={money(item.amount)}/>)}</dl> : <p className="empty compact">Belum ada data pada rentang ini.</p>;
}
