"use client";

import { Bar, BarChart, CartesianGrid, Cell, Legend, Line, LineChart, Pie, PieChart, ReferenceDot, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { compactCategories, dayLabel, mapDailySpending, mapMonthlyCashflow, rankCategories } from "../lib/chartData";
import { compactMillions, hasPace, mapPace, monthMarkers, paceCurves } from "../lib/cycleLedger";
import { money } from "../lib/format";

const colors = ["var(--chart-category-1)", "var(--chart-category-2)", "var(--chart-category-3)", "var(--chart-category-4)", "var(--chart-category-5)", "var(--chart-category-6)"];
const defaultTooltipStyle = { border: "2px solid var(--ink)", borderRadius: 12, background: "var(--surface-strong)", boxShadow: "var(--shadow)", fontSize: 12 };
// Retro Ledger chart style: ink outlines, 2px strokes, sparse grid, direct values.
const axisTick = { fontFamily: "var(--font-body)", fontSize: 11 };
const bar = { stroke: "var(--ink)", strokeWidth: 2, radius: [4, 4, 0, 0], maxBarSize: 24 };

function DailyTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null;
  const item = payload[0].payload;
  return <div className="chart-tooltip"><b className="chart-tooltip-title">{dayLabel(item?.period) || label}</b><div className="chart-tooltip-row"><span>Pengeluaran bersih</span><strong>{money(item.expense)}</strong></div>{item.refund != null && <div className="chart-tooltip-row"><span>Refund</span><strong>{money(item.refund)}</strong></div>}</div>;
}

function MonthlyTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null;
  const net = payload[0]?.payload?.netValue || 0;
  return <div className="chart-tooltip"><b className="chart-tooltip-title">{label}{payload[0]?.payload?.partial && " · berjalan"}</b>{payload.map(item => <div className="chart-tooltip-row" key={item.dataKey}><span>{item.dataKey === "incomeValue" ? "Pemasukan" : "Pengeluaran"}</span><strong>{money(String(Math.round(item.value || 0)))}</strong></div>)}<div className="chart-tooltip-row chart-tooltip-net"><span>Arus bersih</span><strong>{money(String(Math.round(net)))}</strong></div></div>;
}

export function DashboardDailySpendingChart({ items, height = 280 }) {
  const data = mapDailySpending(items);
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran pada periode ini.</p>;
  return <div className="chart-wrap" role="img" aria-label="Grafik pengeluaran harian" style={{ height }}><ResponsiveContainer width="100%" height="100%"><LineChart data={data} margin={{ top: 10, right: 12, left: 0, bottom: 0 }}><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} interval={data.length > 14 ? 2 : 0}/><YAxis hide/><Tooltip content={<DailyTooltip/>}/><Line type="linear" dataKey="expenseValue" stroke="var(--chart-expense)" strokeWidth={3} dot={false} activeDot={{ r: 5, fill: "var(--butter)", stroke: "var(--ink)", strokeWidth: 2 }} isAnimationActive={false}/></LineChart></ResponsiveContainer></div>;
}

export function CycleSpendingPatternChart({ items, spent, daysElapsed, average: suppliedAverage, height = 340 }) {
  const data = mapDailySpending(items);
  const average = suppliedAverage != null ? Number(suppliedAverage) : Number(spent || 0) / Math.max(Number(daysElapsed || 0), 1);
  if (!data.length || data.every(item => item.expense === "0")) return <p className="empty compact">Belum ada pengeluaran bersih pada periode ini. Nilai harian tetap dapat dilihat di bawah.</p>;
  return <div className="chart-wrap cycle-spending-chart" role="img" aria-label="Pola pengeluaran harian siklus gaji dalam IDR" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart accessibilityLayer data={data} margin={{ top: 16, right: 10, left: 0, bottom: 0 }} barCategoryGap="20%"><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} interval={data.length > 14 ? 2 : 0}/><YAxis hide/><Tooltip content={<DailyTooltip/>}/><ReferenceLine y={average} stroke="var(--chart-reference)" strokeDasharray="6 6" ifOverflow="extendDomain" label={{ value: `Rata-rata ${money(String(Math.round(average)))}`, position: "insideTopRight", fill: "var(--chart-axis)", fontFamily: "var(--font-body)", fontSize: 11 }}/><Bar isAnimationActive={false} dataKey="expenseValue" fill="var(--chart-expense)" {...bar} maxBarSize={28}/></BarChart></ResponsiveContainer></div>;
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
// References are served comparison totals; equal-day values are markers at the latest day.
export function CyclePaceChart({ items, references = [], pace, height = 240 }) {
  if (!items?.length) return null;
  if (!hasPace(items)) return <p className="empty compact">Total pengeluaran sampai hari ini belum tersedia.</p>;
  const data = mapPace(items, pace);
  const curves = paceCurves(pace);
  const months = monthMarkers(items);
  const last = items.length ? data[items.length - 1] : data[data.length - 1];
  // A level that a served curve already draws would only repeat it; equal-day markers sit on the curve.
  const levels = references.filter(reference => reference.shape === "line" && !(reference.tone === "previous" && curves.previous) && !(reference.tone === "median" && curves.median));
  const tone = reference => (reference.tone === "median" ? "var(--chart-reference)" : "var(--ink-soft)");
  return <div className="cycle-pace">
    <h3 className="pace-title">Total pengeluaran sampai hari ini</h3>
    <div className="chart-wrap cycle-pace-chart" role="img" aria-label="Total pengeluaran bersih kumulatif siklus terpilih, dalam jutaan rupiah" style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart accessibilityLayer data={data} margin={{ top: 12, right: 16, left: 0, bottom: 0 }}>
          <CartesianGrid stroke="var(--chart-grid)" vertical={false}/>
          <XAxis dataKey="day" type="number" domain={[1, Math.max(data.length, 2)]} allowDecimals={false} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} interval="preserveStartEnd"/>
          <YAxis width={40} axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }} tickFormatter={value => compactMillions(String(value))}/>
          <Tooltip content={<PaceTooltip/>}/>
          {months.map(marker => <ReferenceLine key={marker.day} x={marker.day} stroke="var(--line-strong)" strokeDasharray="2 3" label={{ value: marker.label, position: "insideTopLeft", fill: "var(--chart-axis)", fontFamily: "var(--font-body)", fontSize: 11 }}/>)}
          {curves.previous && <Line type="linear" dataKey="previousTotal" stroke="var(--ink-soft)" strokeWidth={2} strokeDasharray="7 5" dot={false} activeDot={false} isAnimationActive={false}/>}
          {curves.median && <Line type="linear" dataKey="medianTotal" stroke="var(--chart-reference)" strokeWidth={2} strokeDasharray="2 5" strokeLinecap="round" dot={false} activeDot={false} isAnimationActive={false}/>}
          {levels.map(reference => <ReferenceLine key={reference.key} y={Number(reference.value)} ifOverflow="extendDomain" stroke={tone(reference)} strokeDasharray={reference.tone === "median" ? "2 5" : "7 5"} strokeWidth={2}/>)}
          {references.filter(reference => reference.shape === "marker").map(reference => <ReferenceDot key={reference.key} x={last.day} y={Number(reference.value)} r={5} ifOverflow="extendDomain" fill="var(--surface-strong)" stroke={tone(reference)} strokeWidth={2}/>)}
          <Line type="linear" dataKey="runningTotal" stroke="var(--chart-expense)" strokeWidth={3} dot={false} activeDot={{ r: 5, fill: "var(--butter)", stroke: "var(--ink)", strokeWidth: 2 }} isAnimationActive={false}/>
        </LineChart>
      </ResponsiveContainer>
    </div>
    {references.length > 0 && <ul className="pace-notes" aria-label="Pembanding laju pengeluaran">{references.map(reference => <li key={reference.key} data-tone={reference.tone} data-shape={reference.shape}><i aria-hidden="true"/>{reference.label}: <strong>{money(reference.value)}</strong></li>)}</ul>}
  </div>;
}

export function MonthlyCashflowChart({ items, height = 340, partialPeriod = "" }) {
  const data = mapMonthlyCashflow(items, partialPeriod);
  if (!data.length) return <p className="empty compact">Belum ada data bulanan pada periode ini.</p>;
  return <div className="chart-wrap" role="img" aria-label="Perbandingan pemasukan dan pengeluaran bulanan" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }} barCategoryGap="24%"><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/><YAxis hide/><Tooltip content={<MonthlyTooltip/>}/><Legend formatter={value => ({ incomeValue: "Pemasukan", expenseValue: "Pengeluaran" }[value])}/><Bar dataKey="incomeValue" fill="var(--chart-income)" {...bar} maxBarSize={34}/><Bar dataKey="expenseValue" fill="var(--chart-expense)" {...bar} maxBarSize={34}/></BarChart></ResponsiveContainer></div>;
}

export function CategoryDonutChart({ items, height = 260 }) {
  const data = compactCategories(items, 5).map(item => ({ ...item, value: Number(item.amount || 0) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi pada periode ini.</p>;
  return <div className="category-visual"><div className="chart-wrap" role="img" aria-label="Grafik distribusi kategori" style={{ height }}><ResponsiveContainer width="100%" height="100%"><PieChart><Pie data={data} dataKey="value" nameKey="name" innerRadius="58%" outerRadius="82%" paddingAngle={2} labelLine={false} label={false} isAnimationActive={false}>{data.map((item, index) => <Cell key={item.id || item.name} fill={colors[index % colors.length]} stroke="var(--ink)" strokeWidth={2}/>)}</Pie><Tooltip formatter={value => money(String(Math.round(value)))} contentStyle={defaultTooltipStyle}/></PieChart></ResponsiveContainer></div><div className="legend-list">{data.map((item, index) => <div key={item.id || item.name}><i style={{ background: colors[index % colors.length] }}/><span>{item.name}<small>{Math.round(Number(item.share || 0) * 100)}%</small></span><b>{money(item.amount)}</b></div>)}</div></div>;
}

export function CategoryRankingChart({ items, serverOwned = false, height = 320 }) {
  const data = (serverOwned ? items : rankCategories(items)).map(item => ({ ...item, amountValue: Number(item.amount || 0) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi.</p>;
  return <div className="chart-wrap" role="img" aria-label="Distribusi kategori pengeluaran dalam IDR" style={{ height: Math.max(height, data.length * 36) }}><ResponsiveContainer width="100%" height="100%"><BarChart accessibilityLayer data={data} layout="vertical" margin={{ top: 4, right: 12, left: 12, bottom: 4 }}><CartesianGrid stroke="var(--chart-grid)" horizontal={false}/><XAxis type="number" hide/><YAxis type="category" dataKey="name" axisLine={false} tickLine={false} width={145} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/><Tooltip formatter={(_, __, item) => [money(item.payload.amount), serverOwned ? "Pengeluaran bersih" : `${Math.round(Number(item.payload.share || 0) * 100)}%`]} contentStyle={defaultTooltipStyle}/><Bar isAnimationActive={false} dataKey="amountValue" fill="var(--chart-category-2)" stroke="var(--ink)" strokeWidth={2} radius={[0, 4, 4, 0]} maxBarSize={24}/></BarChart></ResponsiveContainer></div>;
}

export function NetWorthHistoryChart({ items, height = 260 }) {
  const data = [...items].reverse().map(item => ({ label: new Date(item.observedAt).toLocaleDateString("id-ID", { day: "2-digit", month: "short" }), value: Number(item.netWorthIdr || 0), raw: item.netWorthIdr || "0" }));
  return <div className="chart-wrap" role="img" aria-label="Riwayat kekayaan bersih" style={{ height }}><ResponsiveContainer width="100%" height="100%"><LineChart data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }}><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ ...axisTick, fill: "var(--chart-axis)" }}/><YAxis hide/><Tooltip formatter={(_, __, item) => money(item.payload.raw)} contentStyle={defaultTooltipStyle}/><Line type="linear" dataKey="value" stroke="var(--chart-category-2)" strokeWidth={3} dot={{ r: 4, fill: "var(--butter)", strokeWidth: 2 }} isAnimationActive={false}/></LineChart></ResponsiveContainer></div>;
}
