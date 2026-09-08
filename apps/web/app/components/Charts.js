"use client";

import { Bar, BarChart, CartesianGrid, Cell, Legend, Pie, PieChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { compactCategories, dayLabel, mapDailySpending, mapMonthlyCashflow, rankCategories } from "../lib/chartData";
import { money } from "../lib/format";

const colors = ["var(--chart-category-1)", "var(--chart-category-2)", "var(--chart-category-3)", "var(--chart-category-4)", "var(--chart-category-5)", "var(--chart-category-6)"];
const defaultTooltipStyle = { border: "1px solid var(--line)", borderRadius: 8, background: "var(--surface-strong)", boxShadow: "var(--shadow)" };

function DailyTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null;
  return <div className="chart-tooltip"><b className="chart-tooltip-title">{dayLabel(payload[0]?.payload?.period) || label}</b><div className="chart-tooltip-row"><span>Pengeluaran</span><strong>{money(String(Math.round(payload[0].value || 0)))}</strong></div></div>;
}

function MonthlyTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null;
  const net = payload[0]?.payload?.netValue || 0;
  return <div className="chart-tooltip"><b className="chart-tooltip-title">{label}</b>{payload.map(item => <div className="chart-tooltip-row" key={item.dataKey}><span>{item.dataKey === "incomeValue" ? "Pemasukan" : "Pengeluaran"}</span><strong>{money(String(Math.round(item.value || 0)))}</strong></div>)}<div className="chart-tooltip-row chart-tooltip-net"><span>Arus bersih</span><strong>{money(String(Math.round(net)))}</strong></div></div>;
}

export function DashboardDailySpendingChart({ items, height = 280 }) {
  const data = mapDailySpending(items);
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran pada periode ini.</p>;
  return <div className="chart-wrap" role="img" aria-label="Grafik pengeluaran harian" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart data={data} margin={{ top: 10, right: 12, left: 0, bottom: 0 }} barCategoryGap="28%"><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "var(--chart-axis)", fontSize: 11 }} interval={data.length > 14 ? 2 : 0}/><YAxis hide/><Tooltip content={<DailyTooltip/>}/><Bar dataKey="expenseValue" fill="var(--chart-expense)" radius={[4,4,0,0]} maxBarSize={24}/></BarChart></ResponsiveContainer></div>;
}

export function CycleSpendingPatternChart({ items, spent, daysElapsed, height = 340 }) {
  const data = mapDailySpending(items);
  const average = Number(spent || 0) / Math.max(Number(daysElapsed || 0), 1);
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran pada siklus aktif.</p>;
  return <div className="chart-wrap cycle-spending-chart" role="img" aria-label="Pola pengeluaran harian siklus gaji" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart data={data} margin={{ top: 16, right: 10, left: 0, bottom: 0 }} barCategoryGap="20%"><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "var(--chart-axis)", fontSize: 11 }} interval={data.length > 14 ? 2 : 0}/><YAxis hide/><Tooltip content={<DailyTooltip/>}/><ReferenceLine y={average} stroke="var(--chart-reference)" strokeDasharray="5 5" ifOverflow="extendDomain" label={{ value: `Rata-rata ${money(String(Math.round(average)))}`, position: "insideTopRight", fill: "var(--chart-axis)", fontSize: 11 }}/><Bar dataKey="expenseValue" fill="var(--chart-expense)" radius={[4,4,0,0]} maxBarSize={28}/></BarChart></ResponsiveContainer></div>;
}

export function MonthlyCashflowChart({ items, height = 340 }) {
  const data = mapMonthlyCashflow(items);
  if (!data.length) return <p className="empty compact">Belum ada data bulanan pada periode ini.</p>;
  return <div className="chart-wrap" role="img" aria-label="Perbandingan pemasukan dan pengeluaran bulanan" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }} barCategoryGap="24%"><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "var(--chart-axis)", fontSize: 12 }}/><YAxis hide/><Tooltip content={<MonthlyTooltip/>}/><Legend formatter={value => ({ incomeValue: "Pemasukan", expenseValue: "Pengeluaran" }[value])}/><Bar dataKey="incomeValue" fill="var(--chart-income)" radius={[4,4,0,0]} maxBarSize={34}/><Bar dataKey="expenseValue" fill="var(--chart-expense)" radius={[4,4,0,0]} maxBarSize={34}/></BarChart></ResponsiveContainer></div>;
}

export function CategoryDonutChart({ items, height = 260 }) {
  const data = compactCategories(items, 5).map(item => ({ ...item, value: Number(item.amount || 0) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi pada periode ini.</p>;
  return <div className="category-visual"><div className="chart-wrap" role="img" aria-label="Grafik distribusi kategori" style={{ height }}><ResponsiveContainer width="100%" height="100%"><PieChart><Pie data={data} dataKey="value" nameKey="name" innerRadius="58%" outerRadius="82%" paddingAngle={2}>{data.map((item, index) => <Cell key={item.id || item.name} fill={colors[index % colors.length]}/>)}</Pie><Tooltip formatter={value => money(String(Math.round(value)))} contentStyle={defaultTooltipStyle}/></PieChart></ResponsiveContainer></div><div className="legend-list">{data.map((item, index) => <div key={item.id || item.name}><i style={{ background: colors[index % colors.length] }}/><span>{item.name}<small>{Math.round(Number(item.share || 0) * 100)}%</small></span><b>{money(item.amount)}</b></div>)}</div></div>;
}

export function CategoryRankingChart({ items, height = 320 }) {
  const data = rankCategories(items).map(item => ({ ...item, amountValue: Number(item.amount || 0) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi.</p>;
  return <div className="chart-wrap" role="img" aria-label="Peringkat kategori pengeluaran" style={{ height: Math.max(height, data.length * 36) }}><ResponsiveContainer width="100%" height="100%"><BarChart data={data} layout="vertical" margin={{ top: 4, right: 12, left: 12, bottom: 4 }}><CartesianGrid stroke="var(--chart-grid)" horizontal={false}/><XAxis type="number" hide/><YAxis type="category" dataKey="name" axisLine={false} tickLine={false} width={145} tick={{ fill: "var(--chart-axis)", fontSize: 11 }}/><Tooltip formatter={(value, _, item) => [money(String(Math.round(value))), `${Math.round(Number(item.payload.share || 0) * 100)}%`]}/><Bar dataKey="amountValue" fill="var(--chart-category-2)" radius={[0,4,4,0]} maxBarSize={24}/></BarChart></ResponsiveContainer></div>;
}

export function NetWorthHistoryChart({ items, height = 260 }) {
  const data = [...items].reverse().map(item => ({ label: new Date(item.observedAt).toLocaleDateString("id-ID", { day: "2-digit", month: "short" }), value: Number(item.netWorthIdr || 0), raw: item.netWorthIdr || "0" }));
  return <div className="chart-wrap" role="img" aria-label="Riwayat kekayaan bersih" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }}><CartesianGrid stroke="var(--chart-grid)" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "var(--chart-axis)" }}/><YAxis hide/><Tooltip formatter={(_, __, item) => money(item.payload.raw)} contentStyle={tooltipStyle}/><Bar dataKey="value" fill="var(--chart-category-2)" radius={[4,4,4,0]} maxBarSize={42}/></BarChart></ResponsiveContainer></div>;
}
