"use client";

import { Area, AreaChart, CartesianGrid, Cell, Legend, Pie, PieChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { money, monthLabel } from "../lib/format";

const colors = ["#2d6a4f", "#5f8f72", "#9ebc8a", "#d7a95b", "#c86b55", "#7188a8"];

export function CashflowChart({ items, height = 310 }) {
  const data = items.map(item => ({ ...item, label: item.period?.length === 10 ? item.period.slice(8) : monthLabel(item.period), incomeValue: Number(item.income || 0), expenseValue: Number(item.expense || 0), netValue: Number(item.netCashflow || 0) }));
  return <div className="chart-wrap" style={{ height }}><ResponsiveContainer width="100%" height="100%"><AreaChart data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }}><defs><linearGradient id="incomeFill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="#2d6a4f" stopOpacity=".3"/><stop offset="1" stopColor="#2d6a4f" stopOpacity="0"/></linearGradient><linearGradient id="expenseFill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="#c86b55" stopOpacity=".22"/><stop offset="1" stopColor="#c86b55" stopOpacity="0"/></linearGradient></defs><CartesianGrid stroke="#e8ece7" vertical={false}/><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "#6f786f", fontSize: 12 }}/><YAxis hide/><Tooltip formatter={(value, name) => [money(String(Math.round(value))), { incomeValue: "Pemasukan", expenseValue: "Pengeluaran", netValue: "Arus bersih" }[name]]} contentStyle={{ border: "1px solid #dfe6df", borderRadius: 12 }}/><Legend formatter={value => ({ incomeValue: "Pemasukan", expenseValue: "Pengeluaran", netValue: "Arus bersih" }[value])}/><Area type="monotone" dataKey="incomeValue" stroke="#2d6a4f" strokeWidth={2.5} fill="url(#incomeFill)" animationDuration={700}/><Area type="monotone" dataKey="expenseValue" stroke="#c86b55" strokeWidth={2.5} fill="url(#expenseFill)" animationDuration={850}/><Area type="monotone" dataKey="netValue" stroke="#526b8b" strokeWidth={2} fill="transparent" animationDuration={1000}/></AreaChart></ResponsiveContainer></div>;
}

export function CategoryChart({ items, height = 260 }) {
  const data = items.filter(item => Number(item.amount) > 0).map(item => ({ ...item, value: Number(item.amount) }));
  if (!data.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi pada periode ini.</p>;
  return <div className="category-visual"><div className="chart-wrap" style={{ height }}><ResponsiveContainer width="100%" height="100%"><PieChart><Pie data={data} dataKey="value" nameKey="name" innerRadius="58%" outerRadius="82%" paddingAngle={2} animationDuration={800}>{data.map((item, index) => <Cell key={item.id || item.name} fill={colors[index % colors.length]}/>)}</Pie><Tooltip formatter={value => money(String(Math.round(value)))} contentStyle={{ border: "1px solid #dfe6df", borderRadius: 12 }}/></PieChart></ResponsiveContainer></div><div className="legend-list">{data.slice(0, 6).map((item, index) => <div key={item.id || item.name}><i style={{ background: colors[index % colors.length] }}/><span>{item.name}<small>{Math.round(Number(item.share || 0) * 100)}%</small></span><b>{money(item.amount)}</b></div>)}</div></div>;
}
