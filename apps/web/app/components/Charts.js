"use client";

import { Bar, BarChart, CartesianGrid, Cell, LabelList, Legend, Line, LineChart, Pie, PieChart, Rectangle, ReferenceDot, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { compactCategories, dayLabel, mapDailySpending, mapMonthlyCashflow, netWorthHistoryRows, netWorthHistorySummary } from "../lib/chartData";
import { compactMillions, hasPace, mapPace, millionTickFormatter, paceAxis, paceCeiling, paceCurves, paceDateLabel, paceMarkerNote, paceTicks } from "../lib/cycleLedger";
import { money } from "../lib/format";
import { transactionHref } from "../lib/cycleReview";

const colors = ["var(--chart-category-1)", "var(--chart-category-2)", "var(--chart-category-3)", "var(--chart-category-4)", "var(--chart-category-5)", "var(--chart-category-6)"];
const defaultTooltipStyle = { border: "2px solid var(--ink)", borderRadius: 12, background: "var(--surface-strong)", boxShadow: "var(--shadow)", fontSize: 12 };
// Retro Ledger chart style: ink outlines, 2px strokes, sparse grid, direct values.
const axisTick = { fontFamily: "var(--font-body)", fontSize: 12 };
const bar = { stroke: "var(--ink)", strokeWidth: 2, radius: [4, 4, 0, 0], maxBarSize: 24 };

function DailyTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null;
  const item = payload[0].payload;
  return <div className="chart-tooltip"><b className="chart-tooltip-title">{dayLabel(item?.period) || label}</b><div className="chart-tooltip-row"><span>Pengeluaran bersih</span><strong>{money(item.expense)}</strong></div>{item.refund != null && <div className="chart-tooltip-row"><span>Refund</span><strong>{money(item.refund)}</strong></div>}</div>;
}

function MonthlyTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null;
  const item = payload[0]?.payload;
  // Served strings, printed as they arrive: the browser never rounds money.
  return <div className="chart-tooltip"><b className="chart-tooltip-title">{label}{item?.partial && " · berjalan"}</b>
    <div className="chart-tooltip-row"><span>Pemasukan</span><strong>{money(item?.income)}</strong></div>
    <div className="chart-tooltip-row"><span>Pengeluaran</span><strong>{money(item?.expense)}</strong></div>
    <div className="chart-tooltip-row chart-tooltip-net"><span>Arus bersih</span><strong>{money(item?.netCashflow)}</strong></div></div>;
}

export function DashboardDailySpendingChart({ items, height = 280 }) {
  const data = mapDailySpending(items);
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran pada periode ini.</p>;
  const max = Math.max(...data.map(item => item.expenseValue), 0);
  return <div className="chart-wrap chart-fill" role="img" aria-label="Grafik pengeluaran harian, dalam jutaan rupiah" style={{ minHeight: height }}><ResponsiveContainer width="100%" height="100%"><LineChart accessibilityLayer={false} data={data} margin={{ top: 10, right: 12, left: 0, bottom: 0 }}><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} padding={{ left: 14, right: 14 }} tick={{ ...axisTick, fill: "var(--chart-axis)" }} interval={data.length > 14 ? 2 : 0}/><YAxis width={52} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={millionTickFormatter(max)} label={{ value: "Rp juta", angle: -90, position: "insideLeft", offset: 14, fill: "var(--chart-axis)", fontFamily: "var(--font-body)", fontSize: 12 }}/><Tooltip content={<DailyTooltip/>}/><Line type="linear" dataKey="expenseValue" stroke="var(--chart-expense)" strokeWidth={3} dot={false} activeDot={{ r: 5, fill: "var(--butter)", stroke: "var(--ink)", strokeWidth: 2 }} isAnimationActive={false}/></LineChart></ResponsiveContainer></div>;
}

// Day tick: the running day of an active cycle says it is not complete yet.
function DayTick({ x, y, payload, partialLabel, last }) {
  const partial = payload.value === partialLabel;
  return <text x={x} y={y + 12} textAnchor={partial && last ? "end" : "middle"} fill="var(--chart-axis)" fontFamily="var(--font-body)" fontSize={12}>{payload.value}{partial && <tspan x={x} dy={14}>belum penuh</tspan>}</text>;
}

export function CycleSpendingPatternChart({ items, period, average: suppliedAverage, partialDay = "", height = 190 }) {
  const data = mapDailySpending(items);
  const average = suppliedAverage == null ? Number.NaN : Number(suppliedAverage); // served figure, plotted as a line only; absent means no line
  if (!data.length || data.every(item => item.expense === "0")) return <p className="empty compact">Belum ada pengeluaran bersih pada periode ini. Nilai harian tetap dapat dilihat di bawah.</p>;
  const max = Math.max(...data.map(item => item.expenseValue), Number.isFinite(average) ? average : 0, 0);
  const lastIndex = data.length - 1;
  // Every third day, plus the running day; the tick before it is dropped so "belum penuh" never collides.
  const partialLabel = partialDay ? partialDay.slice(8) : "";
  const ticks = data.filter((_, index) => (index % 3 === 0 && !(partialLabel && lastIndex - index < 3 && index !== lastIndex)) || (partialLabel && index === lastIndex)).map(item => item.label);
  const dayBar = ({ x, y, width, height, payload, fill, radius }) => {
    const partial = partialDay && payload.period === partialDay;
    const rectangle = <Rectangle {...{ x, y, width, height, radius }} fill={partial ? "var(--chart-expense-soft)" : fill}/>;
    if (!period || payload.expenseValue === 0) return rectangle;
    // The link covers the whole day band and the full plot height, so a thin bar is
    // still a large target; the drawn bar keeps its own width.
    const band = width / 0.9; // barCategoryGap="10%"
    return <a href={transactionHref(period, { from: payload.period, to: payload.period })} tabIndex={0} aria-label={`Lihat pengeluaran ${dayLabel(payload.period)}`}>
      <rect x={x - (band - width) / 2} y={0} width={band} height={y + height} fill="transparent"/>
      {rectangle}
    </a>;
  };
  return <div className="chart-wrap cycle-spending-chart" role="group" aria-label="Pola pengeluaran harian siklus gaji, dalam jutaan rupiah" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart accessibilityLayer={false} data={data} margin={{ top: 8, right: 10, left: 0, bottom: partialLabel ? 16 : 0 }} barCategoryGap="10%"><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} ticks={ticks} interval={0} tick={partialLabel ? <DayTick partialLabel={partialLabel} last={partialLabel === data[lastIndex].label}/> : { ...axisTick, fill: "var(--chart-axis)" }} height={partialLabel ? 36 : 22}/><YAxis width={40} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={millionTickFormatter(max)}/><Tooltip content={<DailyTooltip/>}/>{Number.isFinite(average) && <ReferenceLine y={average} stroke="var(--chart-reference)" strokeDasharray="6 5" strokeWidth={1.5} ifOverflow="extendDomain"/>}<Bar isAnimationActive={false} dataKey="expenseValue" fill="var(--chart-expense)" radius={[2, 2, 0, 0]} maxBarSize={44} shape={dayBar}/></BarChart></ResponsiveContainer></div>;
}

function PaceTooltip({ active, payload }) {
  if (!active || !payload?.length) return null;
  const item = payload[0].payload;
  return <div className="chart-tooltip"><b className="chart-tooltip-title">{item.period ? `${dayLabel(item.period)} · ` : ""}hari ke-{item.day}</b>
    {item.exact != null && <div className="chart-tooltip-row"><span>Total sampai hari ini</span><strong>{money(item.exact)}</strong></div>}
    {item.previousExact != null && <div className="chart-tooltip-row"><span>Siklus sebelumnya</span><strong>{money(item.previousExact)}</strong></div>}
    {item.medianExact != null && <div className="chart-tooltip-row"><span>Median 3 siklus</span><strong>{money(item.medianExact)}</strong></div>}
  </div>;
}

// Single series on its own scale (never mixed with the daily bars): "are we on pace?".
// Served curves and totals only; the line stops at the latest measured day (no projection).
export function CyclePaceChart({ items, references = [], pace, period, height = 200 }) {
  if (!items?.length) return null;
  if (!hasPace(items)) return <p className="empty compact">Total pengeluaran sampai hari ini belum tersedia.</p>;
  const data = mapPace(items, pace);
  const curves = paceCurves(pace);
  const last = data[items.length - 1];
  const tail = curves.previous ? data[curves.previous.length - 1] : null;
  const sameDay = references.find(reference => reference.key === "previous-same-day");
  const tone = reference => (reference.tone === "median" ? "var(--chart-reference)" : "var(--line-strong)");
  // A level that a served curve already draws would only repeat it; equal-day markers sit on the curve.
  const levels = references.filter(reference => reference.shape === "line" && !(reference.tone === "previous" && curves.previous) && !(reference.tone === "median" && curves.median));
  const endLabel = (value, position, fill = "var(--chart-axis)") => ({ value: compactMillions(value), position, fill, fontFamily: "var(--font-body)", fontSize: 12, fontWeight: 600 });
  // A reference level or a comparison curve can push the top of the plot past the series;
  // a round axis keeps the ticks readable. Curve points are served values, read only for
  // the visual ceiling — never written back into a money figure.
  const axis = paceAxis(paceCeiling({ data, levels: levels.map(reference => reference.value), markers: references.filter(reference => reference.shape === "marker").map(reference => reference.value), last: last.exact }));
  // End-value labels sit on the same x at the last day; when their values are close,
  // push the comparison label away from the current one so the two never overlap.
  const span = axis.domain[1] - axis.domain[0];
  const closeToLast = value => Math.abs(Number(value) - Number(last.exact)) < span * 0.12;
  const sameDayLabel = sameDay ? endLabel(sameDay.value, Number(sameDay.value) < Number(last.exact) ? "bottom" : "top") : null;
  if (sameDayLabel) sameDayLabel.dx = -10; // the marker sits on the today line; keep the value off it
  if (sameDayLabel && closeToLast(sameDay.value)) sameDayLabel.dy = Number(sameDay.value) < Number(last.exact) ? 14 : -14;
  // The previous full-cycle tail is the same point as the same-day marker on a full
  // cycle; one label is enough.
  const tailLabel = tail && !(sameDay && tail.day === last.day) ? endLabel(tail.previousExact, "right") : null;
  if (tailLabel && tail.day === last.day && closeToLast(tail.previousTotal)) tailLabel.dy = Number(tail.previousTotal) < Number(last.exact) ? 14 : -14;
  return <div className="cycle-pace">
    <p className="pace-marker-note">{paceMarkerNote(period, last)}</p>
    <div className="chart-wrap cycle-pace-chart" role="img" aria-label="Total pengeluaran bersih kumulatif siklus terpilih dibanding siklus sebelumnya, dalam jutaan rupiah" style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart accessibilityLayer={false} data={data} margin={{ top: 26, right: 44, left: 0, bottom: 0 }}>
          <CartesianGrid stroke="var(--chart-grid)" vertical={false}/>
          <XAxis dataKey="day" type="number" domain={[1, Math.max(data.length, 2)]} ticks={paceTicks(data.length)} tickFormatter={day => paceDateLabel(items, day)} allowDecimals={false} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/>
          <YAxis width={40} type="number" domain={axis.domain} ticks={axis.ticks} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={millionTickFormatter(axis.domain[1])}/>
          <Tooltip content={<PaceTooltip/>}/>
          <ReferenceLine x={last.day} stroke="var(--ink)" strokeWidth={1}/>
          {curves.previous && <Line type="linear" dataKey="previousTotal" stroke="var(--line-strong)" strokeWidth={2} strokeDasharray="7 5" dot={false} activeDot={false} isAnimationActive={false}/>}
          {curves.median && <Line type="linear" dataKey="medianTotal" stroke="var(--chart-reference)" strokeWidth={2} strokeDasharray="2 5" strokeLinecap="round" dot={false} activeDot={false} isAnimationActive={false}/>}
          {levels.map(reference => <ReferenceLine key={reference.key} y={Number(reference.value)} ifOverflow="extendDomain" stroke={tone(reference)} strokeDasharray={reference.tone === "median" ? "2 5" : "7 5"} strokeWidth={2}/>)}
          {references.filter(reference => reference.shape === "marker").map(reference => <ReferenceDot key={reference.key} x={last.day} y={Number(reference.value)} r={5} ifOverflow="extendDomain" fill="var(--surface-strong)" stroke={tone(reference)} strokeWidth={2} label={reference === sameDay ? sameDayLabel : undefined}/>)}
          {tail && <ReferenceDot x={tail.day} y={Number(tail.previousTotal)} r={0} ifOverflow="extendDomain" label={tailLabel}/>}
          <Line type="linear" dataKey="runningTotal" stroke="var(--chart-expense)" strokeWidth={3} dot={false} activeDot={{ r: 5, fill: "var(--butter)", stroke: "var(--ink)", strokeWidth: 2 }} isAnimationActive={false}/>
          <ReferenceDot x={last.day} y={Number(last.exact)} r={4} ifOverflow="extendDomain" fill="var(--chart-expense)" stroke="var(--chart-expense)" label={endLabel(last.exact, "right", "var(--chart-expense)")}/>
        </LineChart>
      </ResponsiveContainer>
    </div>
    {references.length > 0 && <ul className="pace-notes visually-hidden" aria-label="Pembanding laju pengeluaran">{references.map(reference => <li key={reference.key}>{reference.label}: <strong>{money(reference.value)}</strong></li>)}</ul>}
  </div>;
}

export function MonthlyCashflowChart({ items, height = 340, partialPeriod = "" }) {
  const data = mapMonthlyCashflow(items, partialPeriod);
  if (!data.length) return <p className="empty compact">Belum ada data bulanan pada periode ini.</p>;
  const max = Math.max(...data.flatMap(item => [item.incomeValue, item.expenseValue]), 0);
  // Legend in bar order (income, then expense), not Recharts' reversed payload order.
  const legend = () => <ul className="dash-legend" data-shape="bar" aria-label="Keterangan grafik"><li data-series="income"><i aria-hidden="true"/>Pemasukan</li><li data-series="expense"><i aria-hidden="true"/>Pengeluaran</li></ul>;
  return <div className="chart-wrap" role="img" aria-label="Perbandingan pemasukan dan pengeluaran bulanan, dalam jutaan rupiah" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart accessibilityLayer={false} data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }} barCategoryGap="24%"><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/><YAxis width={52} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={millionTickFormatter(max)} label={{ value: "Rp juta", angle: -90, position: "insideLeft", offset: 14, fill: "var(--chart-axis)", fontFamily: "var(--font-body)", fontSize: 12 }}/><Tooltip content={<MonthlyTooltip/>}/><Legend content={legend}/><Bar isAnimationActive={false} dataKey="incomeValue" fill="var(--chart-income)" {...bar} maxBarSize={34}/><Bar isAnimationActive={false} dataKey="expenseValue" fill="var(--chart-expense)" {...bar} maxBarSize={34}/></BarChart></ResponsiveContainer></div>;
}

export function CategoryDonutChart({ items, height = 260 }) {
  const data = compactCategories(items, 5).map(item => ({ ...item, value: Number(item.amount || 0) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi pada periode ini.</p>;
  return <div className="category-visual"><div className="chart-wrap" role="img" aria-label="Grafik distribusi kategori" style={{ height }}><ResponsiveContainer width="100%" height="100%"><PieChart accessibilityLayer={false}><Pie data={data} dataKey="value" nameKey="name" innerRadius="58%" outerRadius="82%" paddingAngle={2} labelLine={false} label={false} isAnimationActive={false}>{data.map((item, index) => <Cell key={item.id || item.name} fill={colors[index % colors.length]} stroke="var(--ink)" strokeWidth={2}/>)}</Pie><Tooltip formatter={value => money(String(Math.round(value)))} contentStyle={defaultTooltipStyle}/></PieChart></ResponsiveContainer></div><div className="legend-list">{data.map((item, index) => <div key={item.id || item.name}><i style={{ background: colors[index % colors.length] }}/><span>{item.name}<small>{Math.round(Number(item.share || 0) * 100)}%</small></span><b>{money(item.amount)}</b></div>)}</div></div>;
}

export function CategoryRankingChart({ items, height = 320 }) {
  // Server order, always: the API already ranks by amount, and the browser never sorts money.
  const data = items.map(item => ({ ...item, amountValue: Number(item.amount || 0), amountLabel: compactMillions(item.amount) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi.</p>;
  return <div className="category-ranking">
    <p className="chart-unit">Rp juta</p>
    <div className="chart-wrap" role="img" aria-label="Distribusi kategori pengeluaran, dalam jutaan rupiah" style={{ height: Math.max(height, data.length * 40) }}><ResponsiveContainer width="100%" height="100%"><BarChart accessibilityLayer={false} data={data} layout="vertical" margin={{ top: 4, right: 56, left: 12, bottom: 4 }}><CartesianGrid stroke="var(--chart-grid)" horizontal={false}/><XAxis type="number" hide/><YAxis type="category" dataKey="name" axisLine={false} tickLine={false} width={145} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/><Tooltip formatter={(_, __, item) => [money(item.payload.amount), "Pengeluaran bersih"]} contentStyle={defaultTooltipStyle}/><Bar isAnimationActive={false} dataKey="amountValue" fill="var(--chart-category-2)" stroke="var(--ink)" strokeWidth={2} radius={[0, 4, 4, 0]} maxBarSize={24}>{data.map(item => <Cell key={item.id || item.name} fill={item.remainder ? "var(--line-strong)" : "var(--chart-category-2)"}/>)}<LabelList dataKey="amountLabel" position="right" fill="var(--ink-soft)" fontFamily="var(--font-body)" fontSize={12}/></Bar></BarChart></ResponsiveContainer></div>
  </div>;
}

export function NetWorthHistoryChart({ items, height = 260 }) {
  const data = netWorthHistoryRows(items);
  // Defensive: the wealth page only mounts this with two or more snapshots.
  if (!data.length) return null;
  const max = Math.max(...data.map(row => row.value), 0);
  const summary = netWorthHistorySummary(data);
  return <div className="chart-wrap" role="img" aria-label={summary} style={{ height }}><ResponsiveContainer width="100%" height="100%"><LineChart accessibilityLayer={false} data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }}><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/><YAxis width={40} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={millionTickFormatter(max)}/><Tooltip formatter={(_, __, item) => money(item.payload.raw)} contentStyle={defaultTooltipStyle}/><Line type="linear" dataKey="value" stroke="var(--chart-category-2)" strokeWidth={3} dot={{ r: 4, fill: "var(--butter)", strokeWidth: 2 }} isAnimationActive={false}/></LineChart></ResponsiveContainer></div>;
}



