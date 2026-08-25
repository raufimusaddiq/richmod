"use client";

import { useEffect, useState } from "react";

const rupiah = new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 });

export default function Home() {
  const [user, setUser] = useState(null);
  const [overview, setOverview] = useState(null);
  const [transactions, setTransactions] = useState([]);
  const [error, setError] = useState("");
  async function load() { const me=await fetch("/api/v1/auth/me");if(!me.ok)return setUser(false);setUser(await me.json());const [summary,ledger]=await Promise.all([fetch("/api/v1/analytics/overview"),fetch("/api/v1/transactions")]);if(summary.ok)setOverview(await summary.json());if(ledger.ok)setTransactions(await ledger.json()) }
  useEffect(()=>{load()},[]);
  async function login(event){event.preventDefault();setError("");const form=new FormData(event.currentTarget);const response=await fetch("/api/v1/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({email:form.get("email"),password:form.get("password")})});if(!response.ok)return setError("Email atau kata sandi tidak cocok.");await load()}
  if(user===null)return <main className="center">Memuat…</main>;
  if(user===false)return <main className="login"><section><span className="eyebrow">FAMILY FINANCE</span><h1>Keuangan rumah tangga, tanpa tebakan.</h1><p>Masuk untuk melihat arus kas dan transaksi keluarga.</p><form onSubmit={login}><label>Email<input name="email" type="email" required /></label><label>Kata sandi<input name="password" type="password" required /></label>{error&&<p className="error">{error}</p>}<button>Masuk</button></form></section></main>;
  const cards=overview?[["Pemasukan",overview.income],["Pengeluaran",overview.expense],["Arus kas bersih",overview.netCashflow]]:[];
  return <main className="shell"><header><div><span className="eyebrow">FAMILY FINANCE</span><h1>Ringkasan {overview?.period||""}</h1></div><div className="identity">{user.displayName}<small>GMT+7 · IDR</small></div></header><section className="cards">{cards.map(([label,value])=><article key={label}><span>{label}</span><strong>{rupiah.format(Number(value))}</strong></article>)}<article><span>Perlu ditinjau</span><strong>{overview?.reviewCount??"–"}</strong></article></section><section className="panel"><div className="panel-title"><h2>Transaksi terbaru</h2><span>{transactions.length} tercatat</span></div><div className="transactions">{transactions.slice(0,12).map(item=><div className="row" key={item.id}><div><b>{item.description||(item.type==="INCOME"?"Pemasukan":"Pengeluaran")}</b><small>{new Date(item.transactionAt).toLocaleString("id-ID",{timeZone:"Asia/Jakarta"})}</small></div><div className={item.type==="INCOME"?"positive":"negative"}>{item.type==="INCOME"?"+":"−"}{rupiah.format(Number(item.amount))}<small>{item.status}</small></div></div>)}{transactions.length===0&&<p className="empty">Belum ada transaksi.</p>}</div></section></main>;
}
