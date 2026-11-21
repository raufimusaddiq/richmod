"use client";

import { useEffect, useRef, useState } from "react";
import { Bar, BarChart, CartesianGrid, Cell, LabelList, Legend, Line, LineChart, Pie, PieChart, Rectangle, ReferenceDot, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { compactCategories, dayLabel, mapDailySpending, mapMonthlyCashflow, netWorthHistoryRows, netWorthHistorySummary } from "../lib/chartData";
import { OTHER_CATEGORY_COLOR, categoryColorFor, categoryColorMap, categoryKey } from "../lib/categoryColors";
import { useCategoryPalette } from "./CategoryPaletteProvider";
import { compactMillions, hasPace, mapPace, millionTickFormatter, paceAxis, paceCeiling, paceCurves, paceDateLabel, paceEndLabels, paceMarkerNote, paceTicks } from "../lib/cycleLedger";
import { money } from "../lib/format";
import { clippedBarLabels, dailyChartModel, dayLedgerHref, formatDailyTick } from "../lib/home";
import { useRouter } from "next/navigation";
import { transactionHref } from "../lib/cycleReview";

const defaultTooltipStyle = { border: "2px solid var(--ink)", borderRadius: 12, background: "var(--surface-strong)", boxShadow: "var(--shadow)", fontSize: 12 };
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
  return <div className="chart-tooltip"><b className="chart-tooltip-title">{label}{item?.partial && " · berjalan"}</b>
    <div className="chart-tooltip-row"><span>Pemasukan</span><strong>{money(item?.income)}</strong></div>
    <div className="chart-tooltip-row"><span>Pengeluaran</span><strong>{money(item?.expense)}</strong></div>
    <div className="chart-tooltip-row chart-tooltip-net"><span>Arus bersih</span><strong>{money(item?.netCashflow)}</strong></div></div>;
}

function DailyBarShape({ x, y, width, height, payload }) {
  if (!(height > 0)) return null;
  const slashY = y + 7;
  return <g style={{ cursor: payload.expenseValue > 0 ? "pointer" : "default" }}>
    <rect x={x} y={y} width={width} height={height} rx={3} fill="var(--ink-soft)"/>
    {payload.capped && <line x1={x - 2} x2={x + width + 2} y1={slashY + 5} y2={slashY - 3} stroke="var(--surface)" strokeWidth={3}/>}
  </g>;
}

export function DashboardDailySpendingChart({ items, height = 240 }) {
  const router = useRouter();
  const wrapRef = useRef(null);
  const [narrow, setNarrow] = useState(false);
  useEffect(() => {
    const node = wrapRef.current;
    if (!node) return undefined;
    const measure = () => setNarrow(node.clientWidth > 0 && node.clientWidth < 420);
    measure();
    if (typeof ResizeObserver === "undefined") return undefined;
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  const data = mapDailySpending(items);
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran pada periode ini.</p>;
  const model = dailyChartModel(data);
  const labels = clippedBarLabels(model.rows, { narrow });
  const hasStagger = [...labels.values()].some(entry => entry.tier > 0);
  const open = row => { if (row?.expenseValue > 0 && row.period) router.push(dayLedgerHref(row.period)); };
  const biggest = model.biggest;
  return <div className="daily-spend">
    <div ref={wrapRef} className="chart-wrap chart-fill" role="img" aria-label="Grafik pengeluaran harian. Pilih sebuah hari untuk melihat transaksinya." style={{ minHeight: height }}><ResponsiveContainer width="100%" height="100%" minWidth={0}><BarChart accessibilityLayer={false} data={model.rows} margin={{ top: hasStagger ? 36 : labels.size ? 22 : 12, right: 12, left: 0, bottom: 0 }}><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} interval={data.length > 14 ? 2 : 0}/><YAxis width={52} domain={model.axis.domain} ticks={model.axis.ticks} allowDataOverflow axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={formatDailyTick}/><Tooltip content={<DailyTooltip/>} cursor={{ fill: "var(--surface-muted)" }}/><ReferenceLine y={Number(model.average)} stroke="var(--chart-reference)" strokeDasharray="5 4" strokeWidth={1.5}/><Bar dataKey="plotValue" shape={<DailyBarShape/>} maxBarSize={22} isAnimationActive={false} onClick={row => open(row?.payload || row)}><LabelList dataKey="expense" content={({ x, y, width, index }) => { const entry = labels.get(index); if (!entry) return null; const anchor = index <= 1 ? "start" : index >= model.rows.length - 2 ? "end" : "middle"; const labelX = anchor === "start" ? x : anchor === "end" ? x + width : x + width / 2; return <text x={labelX} y={y - 6 - entry.tier * 14} textAnchor={anchor} fill="var(--ink-soft)" fontFamily="var(--font-body)" fontSize={12} fontWeight={650}>{entry.text}</text>; }}/></Bar></BarChart></ResponsiveContainer></div>
    <dl className="daily-facts"><div><dt>Rata-rata per hari</dt><dd>{money(model.average)}</dd></div><div><dt>Hari terbesar</dt><dd>{biggest ? <>{money(biggest.expense)}<small> · {dayLabel(biggest.period)}</small></> : "—"}</dd></div></dl>
  </div>;
}

function DayTick({ x, y, payload, partialLabel, last }) {
  const partial = payload.value === partialLabel;
  return <text x={x} y={y + 12} textAnchor={partial && last ? "end" : "middle"} fill="var(--chart-axis)" fontFamily="var(--font-body)" fontSize={12}>{payload.value}{partial && <tspan x={x} dy={14}>belum penuh</tspan>}</text>;
}

export function CycleSpendingPatternChart({ items, period, average: suppliedAverage, partialDay = "", height = 190 }) {
  const data = mapDailySpending(items);
  const average = suppliedAverage == null ? Number.NaN : Number(suppliedAverage);
  if (!data.length || data.every(item => item.expense === "0")) return <p className="empty compact">Belum ada pengeluaran bersih pada periode ini. Nilai harian tetap dapat dilihat di bawah.</p>;
  const max = Math.max(...data.map(item => item.expenseValue), Number.isFinite(average) ? average : 0, 0);
  const lastIndex = data.length - 1;
  const partialLabel = partialDay ? partialDay.slice(8) : "";
  const ticks = data.filter((_, index) => (index % 3 === 0 && !(partialLabel && lastIndex - index < 3 && index !== lastIndex)) || (partialLabel && index === lastIndex)).map(item => item.label);
  const dayBar = ({ x, y, width, height, payload, fill, radius }) => {
    const partial = partialDay && payload.period === partialDay;
    const rectangle = <Rectangle {...{ x, y, width, height, radius }} fill={partial ? "var(--line-strong)" : fill}/>;
    if (!period || payload.expenseValue === 0) return rectangle;
    const band = width / 0.9;
    return <a href={transactionHref(period, { from: payload.period, to: payload.period })} tabIndex={0} aria-label={`Lihat pengeluaran ${dayLabel(payload.period)}`}>
      <rect x={x - (band - width) / 2} y={0} width={band} height={y + height} fill="transparent"/>
      {rectangle}
    </a>;
  };
  return <div className="chart-wrap cycle-spending-chart" role="group" aria-label="Pola pengeluaran harian siklus gaji, dalam jutaan rupiah" style={{ height }}><ResponsiveContainer width="100%" height="100%" minWidth={0}><BarChart accessibilityLayer={false} data={data} margin={{ top: 8, right: 10, left: 0, bottom: partialLabel ? 16 : 0 }} barCategoryGap="10%"><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} ticks={ticks} interval={0} tick={partialLabel ? <DayTick partialLabel={partialLabel} last={partialLabel === data[lastIndex].label}/> : { ...axisTick, fill: "var(--chart-axis)" }} height={partialLabel ? 36 : 22}/><YAxis width={40} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={millionTickFormatter(max)}/><Tooltip content={<DailyTooltip/>}/>{Number.isFinite(average) && <ReferenceLine y={average} stroke="var(--chart-reference)" strokeDasharray="6 5" strokeWidth={1.5} ifOverflow="extendDomain"/>}<Bar isAnimationActive={false} dataKey="expenseValue" fill="var(--ink-soft)" radius={[2, 2, 0, 0]} maxBarSize={44} shape={dayBar}/></BarChart></ResponsiveContainer></div>;
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

export function CyclePaceChart({ items, references = [], pace, period, height = 200 }) {
  if (!items?.length) return null;
  if (!hasPace(items)) return <p className="empty compact">Total pengeluaran sampai hari ini belum tersedia.</p>;
  const data = mapPace(items, pace);
  const curves = paceCurves(pace);
  const last = data[items.length - 1];
  const tone = reference => (reference.tone === "median" ? "var(--chart-reference)" : "var(--line-strong)");
  const levels = references.filter(reference => reference.shape === "line" && !(reference.tone === "previous" && curves.previous) && !(reference.tone === "median" && curves.median));
  const axis = paceAxis(paceCeiling({ data, levels: levels.map(reference => reference.value), markers: references.filter(reference => reference.shape === "marker").map(reference => reference.value), last: last.exact }));
  const endLabels = paceEndLabels({ data, last, curves, references, axis, plotHeight: Math.max(80, height - 50) });
  const renderEndLabel = entry => ({ viewBox }) => {
    if (!viewBox || !Number.isFinite(viewBox.x) || !Number.isFinite(viewBox.y)) return null;
    return <text x={viewBox.x + 8} y={viewBox.y + 4 + entry.dy} fill={entry.fill} fontFamily="var(--font-body)" fontSize={12} fontWeight={600} textAnchor="start" data-pace-end={entry.key}>{entry.text}</text>;
  };
  return <div className="cycle-pace">
    <p className="pace-marker-note">{paceMarkerNote(period, last)}</p>
    <div className="chart-wrap cycle-pace-chart" role="img" aria-label="Total pengeluaran bersih kumulatif siklus terpilih dibanding siklus sebelumnya, dalam jutaan rupiah" style={{ height }}>
      <ResponsiveContainer width="100%" height="100%" minWidth={0}>
        <LineChart accessibilityLayer={false} data={data} margin={{ top: 26, right: 48, left: 0, bottom: 0 }}>
          <CartesianGrid stroke="var(--chart-grid)" vertical={false}/>
          <XAxis dataKey="day" type="number" domain={[1, Math.max(data.length, 2)]} ticks={paceTicks(data.length)} tickFormatter={day => paceDateLabel(items, day)} allowDecimals={false} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/>
          <YAxis width={40} type="number" domain={axis.domain} ticks={axis.ticks} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={millionTickFormatter(axis.domain[1])}/>
          <Tooltip content={<PaceTooltip/>}/>
          <ReferenceLine x={last.day} stroke="var(--ink)" strokeWidth={1}/>
          {curves.previous && <Line type="linear" dataKey="previousTotal" stroke="var(--line-strong)" strokeWidth={2} strokeDasharray="7 5" dot={false} activeDot={false} isAnimationActive={false}/>}
          {curves.median && <Line type="linear" dataKey="medianTotal" stroke="var(--chart-reference)" strokeWidth={2} strokeDasharray="2 5" strokeLinecap="round" dot={false} activeDot={false} isAnimationActive={false}/>}
          {levels.map(reference => <ReferenceLine key={reference.key} y={Number(reference.value)} ifOverflow="extendDomain" stroke={tone(reference)} strokeDasharray={reference.tone === "median" ? "2 5" : "7 5"} strokeWidth={2}/>)}
          {references.filter(reference => reference.shape === "marker").map(reference => <ReferenceDot key={reference.key} x={last.day} y={Number(reference.value)} r={5} ifOverflow="extendDomain" fill="var(--surface-strong)" stroke={tone(reference)} strokeWidth={2}/>)}
          <Line type="linear" dataKey="runningTotal" stroke="var(--ink)" strokeWidth={3} dot={false} activeDot={{ r: 5, fill: "var(--butter)", stroke: "var(--ink)", strokeWidth: 2 }} isAnimationActive={false}/>
          <ReferenceDot x={last.day} y={Number(last.exact)} r={4.5} ifOverflow="extendDomain" fill="var(--butter)" stroke="var(--ink)" strokeWidth={2}/>
          {endLabels.map(entry => <ReferenceDot key={`end-${entry.key}`} x={entry.day} y={entry.value} r={0} ifOverflow="extendDomain" label={{ content: renderEndLabel(entry) }}/>)}
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
  const legend = () => <ul className="dash-legend" data-shape="bar" aria-label="Keterangan grafik"><li data-series="income"><i aria-hidden="true"/>Pemasukan</li><li data-series="expense"><i aria-hidden="true"/>Pengeluaran</li></ul>;
  return <div className="chart-wrap" role="img" aria-label="Perbandingan pemasukan dan pengeluaran bulanan, dalam jutaan rupiah" style={{ height }}><ResponsiveContainer width="100%" height="100%" minWidth={0}><BarChart accessibilityLayer={false} data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }} barCategoryGap="24%"><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/><YAxis width={52} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={millionTickFormatter(max)} label={{ value: "Rp juta", angle: -90, position: "insideLeft", offset: 14, fill: "var(--chart-axis)", fontFamily: "var(--font-body)", fontSize: 12 }}/><Tooltip content={<MonthlyTooltip/>}/><Legend content={legend}/><Bar isAnimationActive={false} dataKey="incomeValue" fill="var(--chart-income)" {...bar} maxBarSize={34}/><Bar isAnimationActive={false} dataKey="expenseValue" fill="var(--chart-expense)" {...bar} maxBarSize={34}/></BarChart></ResponsiveContainer></div>;
}

export function CategoryDonutChart({ items, height = 260, palette = null }) {
  const sharedPalette = useCategoryPalette();
  const activePalette = palette || sharedPalette;
  const data = compactCategories(items, 5).map(item => ({ ...item, value: Number(item.amount || 0) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi pada periode ini.</p>;
  const { colors } = categoryColorMap(items, activePalette);
  const fillFor = item => (item.id === "other" || item.other || item.remainder ? OTHER_CATEGORY_COLOR : colors.get(categoryKey(item)) || categoryColorFor(item, activePalette));
  return <div className="category-visual"><div className="chart-wrap" role="img" aria-label="Grafik distribusi kategori" style={{ height }}><ResponsiveContainer width="100%" height="100%" minWidth={0}><PieChart accessibilityLayer={false}><Pie data={data} dataKey="value" nameKey="name" innerRadius="58%" outerRadius="82%" paddingAngle={2} labelLine={false} label={false} isAnimationActive={false}>{data.map(item => <Cell key={item.id || item.name} fill={fillFor(item)} stroke="var(--ink)" strokeWidth={2}/>)}</Pie><Tooltip formatter={value => money(String(Math.round(value)))} contentStyle={defaultTooltipStyle}/></PieChart></ResponsiveContainer></div><div className="legend-list">{data.map(item => <div key={item.id || item.name}><i style={{ background: fillFor(item) }}/><span>{item.name}<small>{Math.round(Number(item.share || 0) * 100)}%</small></span><b>{money(item.amount)}</b></div>)}</div></div>;
}

export function CategoryRankingChart({ items, height = 320, palette = null }) {
  const sharedPalette = useCategoryPalette();
  const activePalette = palette || sharedPalette;
  const data = items.map(item => ({ ...item, amountValue: Number(item.amount || 0), amountLabel: compactMillions(item.amount) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi.</p>;
  const { colors } = categoryColorMap(data, activePalette);
  return <div className="category-ranking">
    <p className="chart-unit">Rp juta</p>
    <div className="chart-wrap" role="img" aria-label="Distribusi kategori pengeluaran, dalam jutaan rupiah" style={{ height: Math.max(height, data.length * 40) }}><ResponsiveContainer width="100%" height="100%" minWidth={0}><BarChart accessibilityLayer={false} data={data} layout="vertical" margin={{ top: 4, right: 56, left: 12, bottom: 4 }}><CartesianGrid stroke="var(--chart-grid)" horizontal={false}/><XAxis type="number" hide/><YAxis type="category" dataKey="name" axisLine={false} tickLine={false} width={145} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/><Tooltip formatter={(_, __, item) => [money(item.payload.amount), "Pengeluaran bersih"]} contentStyle={defaultTooltipStyle}/><Bar isAnimationActive={false} dataKey="amountValue" fill="var(--chart-category-2)" stroke="var(--ink)" strokeWidth={2} radius={[0, 4, 4, 0]} maxBarSize={24}>{data.map(item => <Cell key={item.id || item.name} fill={item.remainder ? OTHER_CATEGORY_COLOR : colors.get(categoryKey(item)) || categoryColorFor(item, activePalette)}/>)}<LabelList dataKey="amountLabel" position="right" fill="var(--ink-soft)" fontFamily="var(--font-body)" fontSize={12}/></Bar></BarChart></ResponsiveContainer></div>
  </div>;
}

export function NetWorthHistoryChart({ items, height = 260 }) {
  const data = netWorthHistoryRows(items);
  if (!data.length) return null;
  const max = Math.max(...data.map(row => row.value), 0);
  const summary = netWorthHistorySummary(data);
  return <div className="chart-wrap" role="img" aria-label={summary} style={{ height }}><ResponsiveContainer width="100%" height="100%" minWidth={0}><LineChart accessibilityLayer={false} data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }}><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/><YAxis width={40} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={millionTickFormatter(max)}/><Tooltip formatter={(_, __, item) => money(item.payload.raw)} contentStyle={defaultTooltipStyle}/><Line type="linear" dataKey="value" stroke="var(--chart-category-2)" strokeWidth={3} dot={{ r: 4, fill: "var(--butter)", strokeWidth: 2 }} isAnimationActive={false}/></LineChart></ResponsiveContainer></div>;
}



