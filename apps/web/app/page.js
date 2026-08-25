"use client";

import { useEffect, useState } from "react";

const rupiah = new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 });
const reasonLabels = {
  AMBIGUOUS_CATEGORY: "Kategori belum pasti",
  POSSIBLE_DUPLICATE: "Kemungkinan transaksi ganda",
  UNKNOWN_MERCHANT: "Merchant belum dikenal",
  UNKNOWN_PURPOSE: "Tujuan transaksi belum jelas",
};

export default function Home() {
  const [user, setUser] = useState(null);
  const [overview, setOverview] = useState(null);
  const [transactions, setTransactions] = useState([]);
  const [reviews, setReviews] = useState([]);
  const [categories, setCategories] = useState([]);
  const [error, setError] = useState("");
  const [working, setWorking] = useState("");

  async function load() {
    const me = await fetch("/api/v1/auth/me");
    if (!me.ok) {
      setUser(false);
      return;
    }
    setUser(await me.json());
    const [summary, ledger, reviewResponse, categoryResponse] = await Promise.all([
      fetch("/api/v1/analytics/overview"),
      fetch("/api/v1/transactions"),
      fetch("/api/v1/reviews"),
      fetch("/api/v1/categories"),
    ]);
    if (summary.ok) setOverview(await summary.json());
    if (ledger.ok) setTransactions(await ledger.json());
    if (reviewResponse.ok) setReviews(await reviewResponse.json());
    if (categoryResponse.ok) setCategories(await categoryResponse.json());
  }

  useEffect(() => { load(); }, []);

  async function login(event) {
    event.preventDefault();
    setError("");
    const form = new FormData(event.currentTarget);
    const response = await fetch("/api/v1/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: form.get("email"), password: form.get("password") }),
    });
    if (!response.ok) {
      setError("Email atau kata sandi tidak cocok.");
      return;
    }
    await load();
  }

  async function reviewAction(id, action, body) {
    setWorking(id);
    setError("");
    const response = await fetch(`/api/v1/reviews/${id}/${action}`, {
      method: "POST",
      headers: body ? { "Content-Type": "application/json" } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    });
    if (!response.ok) {
      const result = await response.json().catch(() => ({}));
      setError(result.error || "Review belum dapat diperbarui.");
      setWorking("");
      return;
    }
    await load();
    setWorking("");
  }

  if (user === null) return <main className="center">Memuat…</main>;
  if (user === false) return <main className="login"><section><span className="eyebrow">FAMILY FINANCE</span><h1>Keuangan rumah tangga, tanpa tebakan.</h1><p>Masuk untuk melihat arus kas dan transaksi keluarga.</p><form onSubmit={login}><label>Email<input name="email" type="email" required /></label><label>Kata sandi<input name="password" type="password" required /></label>{error && <p className="error">{error}</p>}<button>Masuk</button></form></section></main>;

  const cards = overview ? [["Pemasukan", overview.income], ["Pengeluaran", overview.expense], ["Arus kas bersih", overview.netCashflow]] : [];
  return <main className="shell">
    <header><div><span className="eyebrow">FAMILY FINANCE</span><h1>Ringkasan {overview?.period || ""}</h1></div><div className="identity">{user.displayName}<small>GMT+7 · IDR</small></div></header>
    <section className="cards">{cards.map(([label, value]) => <article key={label}><span>{label}</span><strong>{rupiah.format(Number(value))}</strong></article>)}<article><span>Perlu ditinjau</span><strong>{reviews.length}</strong></article></section>
    {error && <p className="notice error">{error}</p>}
    {reviews.length > 0 && <section className="panel review-panel"><div className="panel-title"><div><span className="eyebrow">REVIEW INBOX</span><h2>Butuh keputusan Anda</h2></div><span>{reviews.length} item</span></div><div className="review-grid">{reviews.map(item => <ReviewCard key={item.id} item={item} categories={categories} disabled={working === item.id} action={reviewAction} />)}</div></section>}
    <section className="panel"><div className="panel-title"><h2>Transaksi terbaru</h2><span>{transactions.length} tercatat</span></div><div className="transactions">{transactions.slice(0, 12).map(item => <div className="row" key={item.id}><div><b>{item.description || (item.type === "INCOME" ? "Pemasukan" : "Pengeluaran")}</b><small>{new Date(item.transactionAt).toLocaleString("id-ID", { timeZone: "Asia/Jakarta" })}</small></div><div className={item.type === "INCOME" ? "positive" : "negative"}>{item.type === "INCOME" ? "+" : "−"}{rupiah.format(Number(item.amount))}<small>{item.status}</small></div></div>)}{transactions.length === 0 && <p className="empty">Belum ada transaksi.</p>}</div></section>
  </main>;
}

function ReviewCard({ item, categories, disabled, action }) {
  function confirm(event) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    action(item.id, "confirm", { categoryId: form.get("categoryId") || null, note: form.get("note") || null });
  }
  return <article className="review-card">
    <div className="review-heading"><span className="review-reason">{reasonLabels[item.reason] || item.reason}</span><strong>{rupiah.format(Number(item.amount))}</strong></div>
    <h3>{item.merchantName || item.counterparty || item.description || "Transaksi tanpa keterangan"}</h3>
    <p>{new Date(item.transactionAt).toLocaleString("id-ID", { timeZone: "Asia/Jakarta" })} · {item.sourceType || "INPUT"}</p>
    {item.candidates?.length > 0 && <div className="candidates"><b>Kemungkinan sama dengan:</b>{item.candidates.map(candidate => <button className="candidate" type="button" disabled={disabled} key={candidate.id} onClick={() => action(item.id, "merge", { targetTransactionId: candidate.id })}><span>{candidate.description || "Transaksi sebelumnya"}</span><span>{Math.round(candidate.score * 100)}% cocok</span></button>)}</div>}
    <form className="review-form" onSubmit={confirm}>
      {item.type === "EXPENSE" && <label>Kategori<select name="categoryId" defaultValue={item.categoryId || ""} required><option value="" disabled>Pilih kategori</option>{categories.map(category => <option key={category.id} value={category.id}>{category.name}</option>)}</select></label>}
      <label>Catatan opsional<input name="note" defaultValue={item.note || ""} maxLength="1000" /></label>
      <div className="review-actions"><button disabled={disabled}>{disabled ? "Memproses…" : "Konfirmasi"}</button><button className="danger" type="button" disabled={disabled} onClick={() => { if (window.confirm("Abaikan transaksi ini? Bukti tetap disimpan.")) action(item.id, "reject"); }}>Abaikan</button></div>
    </form>
  </article>;
}
