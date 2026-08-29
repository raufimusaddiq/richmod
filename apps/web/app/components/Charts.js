"use client";

import { Bar, BarChart, CartesianGrid, Cell, Legend, Pie, PieChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { compactCategories, dayLabel, mapDailySpending, mapMonthlyCashflow } from "../lib/chartData";
import { money } from "../lib/format";

const colors = ["#2d6a4f", "#5f8f72", "#9ebc8a", "#d7a95b", "#c86b55", "#7188a8"];
const tooltipStyle = { border: "1px solid #dfe6df", borderRadius: 12, boxShadow: "0 8px 24px rgba(35,56,43,.12)" };

function DailyTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null;
  return <div className="chart-tooltip"><b>{dayLabel(payload[0]?.payload?.period) || label}</b><span>Pengeluaran</span><strong>{money(String(Math.round(payload[0].value || 0)))}</strong></div>;
}

function MonthlyTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null;
  const net = payload[0]?.payload?.netValue || 0;
  return <div className="chart-tooltip"><b>{label}</b>{payload.map(item => <div key={item.dataKey}><span>{item.dataKey === "incomeValue" ? "Pemasukan" : "Pengeluaran"}</span><strong>{money(String(Math.round(item.value || 0)))}</strong></div>)}<div><span>Arus bersih</span><strong>{money(String(Math.round(net)))}</strong></div></div>;
}

export function DashboardDailySpendingChart({ items, height = 280 }) {
  const data = mapDailySpending(items);
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran pada periode ini.</p>;
  return <div className="chart-wrap" role="img" aria-label="Grafik pengeluaran harian" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart data={data} margin={{ top: 10, right: 12, left: 0, bottom: 0 }} barCategoryGap="28%"><CartesianGrid stroke="#e8ece7" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "#6f786f", fontSize: 11 }} interval={data.length > 14 ? 2 : 0}/><YAxis hide/><Tooltip content={<DailyTooltip/>} contentStyle={tooltipStyle} labelFormatter={label => `Tanggal ${label}`}/><Bar dataKey="expenseValue" fill="#d47a66" radius={[4,4,0,0]} maxBarSize={24}/></BarChart></ResponsiveContainer></div>;
}

export function CycleSpendingPatternChart({ items, spent, daysElapsed, height = 340 }) {
  const data = mapDailySpending(items);
  const average = Number(spent || 0) / Math.max(Number(daysElapsed || 0), 1);
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran pada siklus aktif.</p>;
  return <div className="chart-wrap cycle-spending-chart" role="img" aria-label="Pola pengeluaran harian siklus gaji" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart data={data} margin={{ top: 16, right: 10, left: 0, bottom: 0 }} barCategoryGap="20%"><CartesianGrid stroke="#e8ece7" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "#6f786f", fontSize: 11 }} interval={data.length > 14 ? 2 : 0}/><YAxis hide/><Tooltip content={<DailyTooltip/>} contentStyle={tooltipStyle} labelFormatter={label => `Tanggal ${label}`}/><ReferenceLine y={average} stroke="#7188a8" strokeDasharray="5 5" ifOverflow="extendDomain" label={{ value: `Rata-rata ${money(String(Math.round(average)))}`, position: "insideTopRight", fill: "#6f786f", fontSize: 11 }}/><Bar dataKey="expenseValue" fill="#d47a66" radius={[4,4,0,0]} maxBarSize={28}/></BarChart></ResponsiveContainer></div>;
}

export function MonthlyCashflowChart({ items, height = 340 }) {
  const data = mapMonthlyCashflow(items);
  if (!data.length) return <p className="empty compact">Belum ada data bulanan pada periode ini.</p>;
  return <div className="chart-wrap" role="img" aria-label="Perbandingan pemasukan dan pengeluaran bulanan" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }} barCategoryGap="24%"><CartesianGrid stroke="#e8ece7" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "#6f786f", fontSize: 12 }}/><YAxis hide/><Tooltip content={<MonthlyTooltip/>} contentStyle={tooltipStyle}/><Legend formatter={value => ({ incomeValue: "Pemasukan", expenseValue: "Pengeluaran" }[value])}/><Bar dataKey="incomeValue" fill="#2d6a4f" radius={[4,4,0,0]} maxBarSize={34}/><Bar dataKey="expenseValue" fill="#c86b55" radius={[4,4,0,0]} maxBarSize={34}/></BarChart></ResponsiveContainer></div>;
}

export function CategoryDonutChart({ items, height = 260 }) {
  const data = compactCategories(items, 5).map(item => ({ ...item, value: Number(item.amount || 0) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi pada periode ini.</p>;
  return <div className="category-visual"><div className="chart-wrap" role="img" aria-label="Grafik distribusi kategori" style={{ height }}><ResponsiveContainer width="100%" height="100%"><PieChart><Pie data={data} dataKey="value" nameKey="name" innerRadius="58%" outerRadius="82%" paddingAngle={2}>{data.map((item, index) => <Cell key={item.id || item.name} fill={colors[index % colors.length]}/>)}</Pie><Tooltip formatter={value => money(String(Math.round(value)))} contentStyle={tooltipStyle}/></PieChart></ResponsiveContainer></div><div className="legend-list">{data.map((item, index) => <div key={item.id || item.name}><i style={{ background: colors[index % colors.length] }}/><span>{item.name}<small>{Math.round(Number(item.share || 0) * 100)}%</small></span><b>{money(item.amount)}</b></div>)}</div></div>;
}

export function CategoryRankingChart({ items, height = 320 }) {
  const data = compactCategories(items, 8).map(item => ({ ...item, amountValue: Number(item.amount || 0) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi.</p>;
  return <div className="chart-wrap" role="img" aria-label="Peringkat kategori pengeluaran" style={{ height }}><ResponsiveContainer width="100%" height="100%"><BarChart data={data} layout="vertical" margin={{ top: 4, right: 12, left: 12, bottom: 4 }}><CartesianGrid stroke="#e8ece7" horizontal={false}/><XAxis type="number" hide/><YAxis type="category" dataKey="name" axisLine={false} tickLine={false} width={145} tick={{ fill: "#526057", fontSize: 11 }}/><Tooltip formatter={(value, _, item) => [money(String(Math.round(value))), `${Math.round(Number(item.payload.share || 0) * 100)}%`]} contentStyle={tooltipStyle}/><Bar dataKey="amountValue" fill="#5f8f72" radius={[0,4,4,0]} maxBarSize={24}/></BarChart></ResponsiveContainer></div>;
}
