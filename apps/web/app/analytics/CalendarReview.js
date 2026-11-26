import { useEffect, useRef, useState } from "react";
import { MonthlyCashflowChart } from "../components/Charts";
import CountUp from "../components/CountUp";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import { addMonths, calendarErrorMessage, calendarKpiCells, calendarMembers, calendarPeriodLabel, calendarTransactionHref, currentMonthKey, defaultRange, firstActiveMonth, hasActivity, hasRefunds, monthChoices, monthRowLabel, nextMonthKey, rangeSummary, validateCustomRange } from "../lib/calendarReview";
import { transactionHref } from "../lib/cycleReview";
import { money, monthLabel } from "../lib/format";
import { CalendarCategoryTable } from "./CategoryTable";
import { HouseholdSection, MerchantCard } from "./CycleSections";
import { SectionTitle } from "./shared";

export function CalendarRangeControls({ selection, navigate }) {
  const now = new Date();
  const fallback = defaultRange(selection, now);
  const [from, setFrom] = useState(fallback.from);
  const [to, setTo] = useState(fallback.to);
  const [rangeError, setRangeError] = useState("");
  const [open, setOpen] = useState(false);
  const anchorRef = useRef(null);
  const triggerRef = useRef(null);
  const fromRef = useRef(null);

  const selectedFrom = selection.from;
  const selectedTo = selection.to;
  const selectedRange = selection.range;
  useEffect(() => {
    const shown = defaultRange({ from: selectedFrom, to: selectedTo, range: selectedRange });
    setFrom(shown.from);
    setTo(shown.to);
    setRangeError("");
    setOpen(false);
  }, [selectedFrom, selectedTo, selectedRange]);

  const popoverOpen = open || Boolean(rangeError);
  useEffect(() => {
    if (popoverOpen) fromRef.current?.focus();
  }, [popoverOpen]);
  useEffect(() => {
    if (!popoverOpen) return undefined;
    const onPointerDown = event => {
      if (anchorRef.current && !anchorRef.current.contains(event.target)) {
        setOpen(false);
        setRangeError("");
      }
    };
    const onKeyDown = event => {
      if (event.key === "Escape") {
        setOpen(false);
        setRangeError("");
        triggerRef.current?.focus();
      }
    };
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [popoverOpen]);

  const custom = Boolean(selection.from && selection.to);
  const draft = from !== fallback.from || to !== fallback.to;
  const latest = nextMonthKey(now);
  const choices = monthChoices(latest, from, to);
  const monthOptions = choices.map(choice => <option key={choice.value} value={choice.value}>{choice.label}</option>);
  const rangeProps = value => ({ required: true, "aria-invalid": rangeError ? true : undefined, "aria-describedby": rangeError ? "range-error" : undefined, value });

  function submitRange(event) {
    event.preventDefault();
    if (!draft) return;
    const problem = validateCustomRange(from, to, new Date());
    setRangeError(problem);
    if (problem) {
      setOpen(true);
      return;
    }
    setOpen(false);
    navigate({ from, to });
    triggerRef.current?.focus();
  }

  const periodLabel = calendarPeriodLabel(fallback.from, fallback.to);
  const statusText = `${custom ? `Rentang bulan ${rangeSummary(selection.from, selection.to)}` : `${selection.range} bulan terakhir`} · Asia/Jakarta`;

  return <>
    <div className="range-control-group" data-slide data-slide-panel=".calendar-body" role="group" aria-label="Rentang bulan">
      {["3", "6", "12"].map(range => <button type="button" key={range} className={selection.range === range && !custom ? "active" : "secondary"} aria-pressed={selection.range === range && !custom} onClick={() => navigate({ range, from: "", to: "" })}>{range} Bulan</button>)}
    </div>
    <div ref={anchorRef} className="calendar-range-anchor">
      <button ref={triggerRef} type="button" className={custom ? "active calendar-period-button" : "secondary calendar-period-button"} aria-expanded={popoverOpen} aria-haspopup="dialog" aria-controls="calendar-custom-popover" onClick={() => { if (popoverOpen) { setOpen(false); setRangeError(""); triggerRef.current?.focus(); } else setOpen(true); }}>
        <span>{periodLabel}</span>
        <span className="calendar-period-caret" aria-hidden="true">▾</span>
      </button>
      {popoverOpen && <div id="calendar-custom-popover" className="calendar-range-popover" role="dialog" aria-label="Rentang bulan kustom">
        <form className="custom-range" aria-label="Rentang bulan kustom" onSubmit={submitRange} noValidate>
          <label>Bulan mulai<select ref={fromRef} name="from" {...rangeProps(from)} onChange={event => setFrom(event.target.value)}>{monthOptions}</select></label>
          <label>Bulan selesai<select name="to" {...rangeProps(to)} onChange={event => setTo(event.target.value)}>{monthOptions}</select></label>
          <button type="submit" disabled={!draft}>Terapkan</button>
        </form>
        {rangeError && <div id="range-error" className="notice error feedback" role="alert"><span>{rangeError}</span></div>}
      </div>}
    </div>
    <div className="cycle-status" aria-live="polite">
      <strong>{statusText}</strong>
    </div>
  </>;
}

export function CalendarReview({ selection, palette = null }) {
  const now = new Date();
  const running = currentMonthKey(now);
  const fallback = defaultRange(selection, now);
  const query = new URLSearchParams({ period: selection.from && selection.to ? "custom" : "calendar", range: selection.range });
  if (selection.from && selection.to) { query.set("from", selection.from); query.set("to", selection.to); }
  const queryString = query.toString();

  const summary = useCalendarSection("range-summary", queryString);
  const cashflow = useCalendarSection("cashflow", queryString);
  const categories = useCalendarSection("categories", queryString);
  const merchants = useCalendarSection("merchants", queryString);
  const members = useCalendarSection("members", queryString);
  const sections = [summary, cashflow, categories, merchants, members];
  const allFailed = sections.every(section => section.error);
  const refreshing = sections.some(section => section.loading && section.data);
  const periodText = calendarPeriodLabel(fallback.from, fallback.to);
  const rangeHref = calendarTransactionHref({ from: fallback.from, to: fallback.to, cycle: selection.cycle });

  const memberCountText = members.error
    ? "belum dapat dimuat"
    : members.data
      ? (members.data.length ? `catatan ${members.data.length} pencatat` : "belum ada pencatat")
      : "memuat…";

  return <>
    {allFailed && <ErrorNotice message={summary.error || cashflow.error} retry={() => sections.forEach(section => section.retry())}/>}
    {refreshing && <p className="ledger-loading" role="status">Memuat rentang…</p>}
    {!allFailed && <div className="calendar-body">
      <CalendarKpiSection state={summary} periodText={periodText}/>
      <section className="surface dash-card calendar-section" data-stale={cashflow.loading && cashflow.data ? "true" : undefined} aria-busy={cashflow.loading || undefined} aria-labelledby="cashflow-title">
        <div className="dash-card-head">
          <div>
            <SectionTitle id="cashflow-title" title="Pemasukan vs pengeluaran"/>
          </div>
        </div>
        {cashflow.error
          ? <ErrorNotice message={cashflow.error} retry={cashflow.retry}/>
          : cashflow.data
            ? <>
              <MonthlyCashflowChart items={cashflow.data} height={260} partialPeriod={running}/>
              <p className="dash-caption">Semua nilai berasal dari transaksi terkonfirmasi; pengeluaran bersih setelah refund.{cashflow.data.some(item => item.period === running) ? ` Bulan berjalan (${monthLabel(running)}) belum penuh.` : ""}</p>
              <details className="review-daily">
                <summary>Tabel per bulan › <small>pemasukan, pengeluaran bersih, arus kas</small></summary>
                <MonthlyTable items={cashflow.data} now={now} cycle={selection.cycle}/>
              </details>
            </>
            : <Skeleton cards={1} rows={4} label="Memuat pemasukan vs pengeluaran"/>}
      </section>
      {categories.error
        ? <section id="where" className="surface dash-card calendar-section" aria-labelledby="where-title"><SectionTitle id="where-title" title="Ke mana uangnya"/><ErrorNotice message={categories.error} retry={categories.retry}/></section>
        : categories.data
          ? <CalendarCategoryTable items={categories.data} total={summary.data?.expense ?? null} rangeHref={rangeHref} palette={palette} stale={categories.loading && Boolean(categories.data)}/>
          : <section id="where" className="surface dash-card calendar-section" aria-labelledby="where-title"><SectionTitle id="where-title" title="Ke mana uangnya"/><Skeleton cards={1} rows={3} label="Memuat kategori"/></section>}
      {merchants.error
        ? <section id="merchants" className="surface dash-card calendar-section" aria-labelledby="merchants-title"><SectionTitle id="merchants-title" title="Merchant teratas"/><ErrorNotice message={merchants.error} retry={merchants.retry}/></section>
        : merchants.data
          ? <MerchantCard items={merchants.data} hrefFor={item => calendarTransactionHref({ from: fallback.from, to: fallback.to, cycle: selection.cycle }, { merchantId: item.id })} caption="Pengeluaran bersih setelah refund pada rentang kalender, diurutkan dari server." emptyMessage="Belum ada data merchant pada rentang ini." stale={merchants.loading && Boolean(merchants.data)}/>
          : <section id="merchants" className="surface dash-card calendar-section" aria-labelledby="merchants-title"><SectionTitle id="merchants-title" title="Merchant teratas"/><Skeleton cards={1} rows={2} label="Memuat merchant"/></section>}
      <div className="review-stack surface card">
        <section id="household" className="review-section" aria-labelledby="household-title">
          <details className="report-disclosure">
            <summary>
              <h2 id="household-title" tabIndex={-1}>Catatan rumah tangga</h2>
              <span>{memberCountText}</span>
            </summary>
            <div className="report-disclosure-body">
              {members.error
                ? <ErrorNotice message={members.error} retry={members.retry}/>
                : members.data
                  ? <HouseholdSection members={calendarMembers(members.data)} showHeading={false}/>
                  : <Skeleton cards={1} rows={2} label="Memuat catatan rumah tangga"/>}
            </div>
          </details>
        </section>
      </div>
      <p className="dash-caption">Pembahasan dan tinjauan rumah tangga tersedia pada mode Siklus Gaji.</p>
    </div>}
  </>;
}

function CalendarKpiSection({ state, periodText }) {
  if (state.error) {
    return <section id="position" className="surface dash-card calendar-section" aria-labelledby="calendar-position-title">
      <h2 id="calendar-position-title" className="visually-hidden">Ringkasan rentang kalender</h2>
      <ErrorNotice message={state.error} retry={state.retry}/>
    </section>;
  }
  if (!state.data) {
    return <section id="position" className="kpi-strip" aria-labelledby="calendar-position-title">
      <h2 id="calendar-position-title" className="visually-hidden">Ringkasan rentang kalender</h2>
      <Skeleton cards={1} rows={2} label="Memuat ringkasan rentang kalender"/>
    </section>;
  }
  const cells = calendarKpiCells(state.data, periodText);
  return <section id="position" className="kpi-strip calendar-section" data-stale={state.loading && state.data ? "true" : undefined} aria-busy={state.loading || undefined} aria-labelledby="calendar-position-title">
    <h2 id="calendar-position-title" className="visually-hidden">Ringkasan rentang kalender</h2>
    <dl className="surface">
      {cells.map(cell => <div key={cell.key} className="kpi-cell" data-lead={cell.lead || undefined}>
        <dt>{cell.label}</dt>
        <dd><CountUp text={cell.value}/></dd>
        {cell.verdict && <p className="kpi-verdict">{cell.verdict}</p>}
        {cell.emptyComparison && <p className="kpi-sub">{cell.emptyComparison}</p>}
        {cell.sub && <p className="kpi-sub">{cell.sub}</p>}
      </div>)}
    </dl>
  </section>;
}

function useCalendarSection(name, queryString) {
  const [state, setState] = useState({ data: null, error: "", loading: true });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setState(current => ({ ...current, error: "", loading: true }));
    fetch(`/api/v1/analytics/${name}?${queryString}`, { signal: controller.signal })
      .then(async response => {
        if (!response.ok) {
          const body = await response.json().catch(() => ({}));
          throw new Error(calendarErrorMessage(response.status, body?.error));
        }
        return response.json();
      })
      .then(data => { if (!controller.signal.aborted) setState({ data, error: "", loading: false }); })
      .catch(err => { if (err.name !== "AbortError" && !controller.signal.aborted) setState({ data: null, error: err.message, loading: false }); });
    return () => controller.abort();
  }, [name, queryString, attempt]);
  return { ...state, retry: () => setAttempt(value => value + 1) };
}

function MonthlyTable({ items, now, cycle }) {
  const start = firstActiveMonth(items);
  const showRefund = hasRefunds(items);
  const columns = showRefund ? 4 : 3;
  const labels = ["Pemasukan", "Pengeluaran bersih", ...(showRefund ? ["Refund"] : []), "Arus kas bersih"];
  const rows = start < 0 ? [] : items.slice(start);
  return <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Arus kas per bulan, geser untuk semua kolom">
    <table className="monthly-table">
      <caption className="visually-hidden">Arus kas per bulan pada rentang kalender</caption>
      <thead><tr><th scope="col">Bulan</th>{labels.map(label => <th key={label} scope="col">{label}</th>)}</tr></thead>
      <tbody>
        {start > 0 && <tr className="month-start"><td colSpan={columns + 1}>Data mulai {monthLabel(items[start].period)}</td></tr>}
        {start < 0 && <tr className="month-start"><td colSpan={columns + 1}>Belum ada transaksi terkonfirmasi pada rentang ini.</td></tr>}
        {rows.map(item => { const active = hasActivity(item); return <tr key={item.period}>
          <th scope="row">{active ? <a href={transactionHref({ start: cycle, measuredUntil: `${addMonths(item.period, 1)}-01` }, { from: `${item.period}-01` })} aria-label={`Lihat pengeluaran ${monthRowLabel(item.period, now)}`}>{monthRowLabel(item.period, now)}</a> : <>{monthRowLabel(item.period, now)}<small>belum ada transaksi</small></>}</th>
          {active ? <>
            <td data-label={labels[0]}>{money(item.income)}</td>
            <td data-label={labels[1]}>{money(item.expense)}</td>
            {showRefund && <td data-label={labels[2]}>{money(item.refund)}</td>}
            <td data-label={labels[showRefund ? 3 : 2]}>{money(item.netCashflow)}</td>
          </> : <td colSpan={columns}>—</td>}
        </tr>; })}
      </tbody>
    </table>
  </div>;
}
