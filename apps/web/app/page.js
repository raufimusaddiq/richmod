"use client";

import { useEffect, useState } from "react";

const rupiah = new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 });
const money = value => {
  try { return rupiah.format(BigInt(value || "0")); } catch { return "Rp0"; }
};
const percent = value => `${Math.round(Number(value || 0) * 100)}%`;
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
  const [documents, setDocuments] = useState([]);
  const [budgets, setBudgets] = useState([]);
  const [cashflow, setCashflow] = useState([]);
  const [categorySpending, setCategorySpending] = useState([]);
  const [merchantSpending, setMerchantSpending] = useState([]);
  const [memberSpending, setMemberSpending] = useState([]);
  const [insights, setInsights] = useState([]);
  const [error, setError] = useState("");
  const [working, setWorking] = useState("");

  async function load() {
    const me = await fetch("/api/v1/auth/me");
    if (!me.ok) {
      setUser(false);
      return;
    }
    setUser(await me.json());
    const [summary, ledger, reviewResponse, categoryResponse, documentResponse, budgetResponse, cashflowResponse, categoryAnalytics, merchantAnalytics, memberAnalytics, insightResponse] = await Promise.all([
      fetch("/api/v1/analytics/overview"),
      fetch("/api/v1/transactions"),
      fetch("/api/v1/reviews"),
      fetch("/api/v1/categories"),
      fetch("/api/v1/documents"),
      fetch("/api/v1/budgets"),
      fetch("/api/v1/analytics/cashflow"),
      fetch("/api/v1/analytics/categories"),
      fetch("/api/v1/analytics/merchants"),
      fetch("/api/v1/analytics/members"),
      fetch("/api/v1/insights"),
    ]);
    if (summary.ok) setOverview(await summary.json());
    if (ledger.ok) setTransactions(await ledger.json());
    if (reviewResponse.ok) setReviews(await reviewResponse.json());
    if (categoryResponse.ok) setCategories(await categoryResponse.json());
    if (documentResponse.ok) setDocuments(await documentResponse.json());
    if (budgetResponse.ok) setBudgets(await budgetResponse.json());
    if (cashflowResponse.ok) setCashflow(await cashflowResponse.json());
    if (categoryAnalytics.ok) setCategorySpending(await categoryAnalytics.json());
    if (merchantAnalytics.ok) setMerchantSpending(await merchantAnalytics.json());
    if (memberAnalytics.ok) setMemberSpending(await memberAnalytics.json());
    if (insightResponse.ok) setInsights(await insightResponse.json());
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

  async function uploadDocument(event) {
    event.preventDefault();
    setWorking("upload");
    setError("");
    const response = await fetch("/api/v1/documents", { method: "POST", body: new FormData(event.currentTarget) });
    if (!response.ok) {
      const result = await response.json().catch(() => ({}));
      setError(result.error || "Dokumen belum dapat diunggah.");
      setWorking("");
      return;
    }
    event.currentTarget.reset();
    await load();
    setWorking("");
  }

  async function createBudget(event) {
    event.preventDefault();
    setWorking("budget");
    setError("");
    const form = new FormData(event.currentTarget);
    const response = await fetch("/api/v1/budgets", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ categoryId: form.get("categoryId"), monthlyAmount: form.get("monthlyAmount"), startMonth: overview?.period || "" }) });
    if (!response.ok) {
      const result = await response.json().catch(() => ({}));
      setError(result.error || "Anggaran belum dapat dibuat.");
    } else {
      event.currentTarget.reset();
      await load();
    }
    setWorking("");
  }

  async function closeBudget(id) {
    setWorking(id);
    const response = await fetch(`/api/v1/budgets/${id}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ active: false }) });
    if (!response.ok) setError("Anggaran belum dapat dinonaktifkan.");
    else await load();
    setWorking("");
  }

  async function generateInsight() {
    setWorking("insight");
    setError("");
    const response = await fetch("/api/v1/insights/generate", { method: "POST" });
    if (!response.ok) {
      const result = await response.json().catch(() => ({}));
      setError(result.error || "Insight belum dapat dibuat.");
      setWorking("");
      return;
    }
    await load();
    setTimeout(load, 3500);
    setWorking("");
  }

  if (user === null) return <main className="center">Memuat…</main>;
  if (user === false) return <main className="login"><section><span className="eyebrow">FAMILY FINANCE</span><h1>Keuangan rumah tangga, tanpa tebakan.</h1><p>Masuk untuk melihat arus kas dan transaksi keluarga.</p><form onSubmit={login}><label>Email<input name="email" type="email" required /></label><label>Kata sandi<input name="password" type="password" required /></label>{error && <p className="error">{error}</p>}<button>Masuk</button></form></section></main>;

  const cards = overview ? [["Pemasukan", overview.income], ["Pengeluaran", overview.expense], ["Arus kas bersih", overview.netCashflow]] : [];
  return <main className="shell">
    <header><div><span className="eyebrow">FAMILY FINANCE</span><h1>Ringkasan {overview?.period || ""}</h1></div><div className="identity">{user.displayName}<small>GMT+7 · IDR</small><a href="/household">Kelola household</a> · <a href="/settings">Settings</a></div></header>
    <section className="cards">{cards.map(([label, value]) => <article key={label}><span>{label}</span><strong>{money(value)}</strong></article>)}<article><span>Perlu ditinjau</span><strong>{reviews.length}</strong></article></section>
    {error && <p className="notice error">{error}</p>}
    <section className="panel document-panel"><div className="panel-title"><div><span className="eyebrow">DOKUMEN KEUANGAN</span><h2>Unggah satu gambar, biarkan sistem mengenalinya</h2></div><span>JPEG/PNG · maks. 10 MB</span></div><form className="upload-form" onSubmit={uploadDocument}><input name="file" type="file" accept="image/jpeg,image/png,.jpg,.jpeg,.png" required /><button disabled={working === "upload"}>{working === "upload" ? "Mengunggah…" : "Unggah & klasifikasikan"}</button></form>{documents.length > 0 && <div className="document-list">{documents.slice(0, 6).map(document => <a key={document.id} href={`/api/v1/documents/${document.id}/content`} target="_blank" rel="noreferrer"><b>{document.documentType || "Sedang diklasifikasikan"}</b><span>{document.width}×{document.height} · {document.status}</span></a>)}</div>}</section>
    <section className="analytics-grid">
      <AnalyticsPanel title="Arus kas 6 bulan" items={cashflow.map(item => ({ name: item.period, amount: item.netCashflow }))} />
      <AnalyticsPanel title="Pengeluaran per kategori" items={categorySpending} />
      <AnalyticsPanel title="Merchant teratas" items={merchantSpending} />
      <AnalyticsPanel title="Kontribusi anggota" items={memberSpending} />
    </section>
    <section className="panel budget-panel"><div className="panel-title"><div><span className="eyebrow">ANGGARAN BULANAN</span><h2>Batas pengeluaran per kategori</h2></div><span>{budgets.length} aktif</span></div>{user.memberships?.[0]?.role === "OWNER" && <form className="budget-form" onSubmit={createBudget}><select name="categoryId" required defaultValue=""><option value="" disabled>Pilih kategori</option>{categories.filter(category => category.active).map(category => <option key={category.id} value={category.id}>{category.name}</option>)}</select><input name="monthlyAmount" inputMode="numeric" pattern="[0-9]+" placeholder="Batas IDR, contoh 2000000" required /><button disabled={working === "budget"}>Tambah anggaran</button></form>}<div className="budget-list">{budgets.map(item => <article key={item.id}><div><b>{item.categoryName}</b><small>{money(item.spent)} dari {money(item.monthlyAmount)}</small></div><div className="budget-meter"><i style={{ width: `${Math.min(100, Math.max(0, Number(item.utilization) * 100))}%` }} /></div><div><strong>{percent(item.utilization)}</strong>{user.memberships?.[0]?.role === "OWNER" && <button className="text-button" disabled={working === item.id} onClick={() => closeBudget(item.id)}>Nonaktifkan</button>}</div></article>)}{budgets.length === 0 && <p className="empty">Belum ada anggaran aktif.</p>}</div></section>
    <section className="panel insight-panel"><div className="panel-title"><div><span className="eyebrow">INSIGHT</span><h2>Ringkasan dari metrik terverifikasi</h2></div><button disabled={working === "insight"} onClick={generateInsight}>{working === "insight" ? "Meminta…" : "Buat insight"}</button></div>{insights[0]?.status === "SUCCEEDED" ? <article><p>{insights[0].text}</p><small>Kelengkapan data {percent(insights[0].dataCompleteness)} · keyakinan {percent(insights[0].confidence)}</small></article> : insights[0]?.status === "PENDING" ? <p className="empty">Insight sedang dibuat dari agregat bulan ini…</p> : insights[0]?.status === "FAILED" ? <p className="empty">Insight gagal dibuat. Data keuangan tidak berubah.</p> : <p className="empty">Belum ada insight untuk bulan ini.</p>}</section>
    {reviews.length > 0 && <section className="panel review-panel"><div className="panel-title"><div><span className="eyebrow">REVIEW INBOX</span><h2>Butuh keputusan Anda</h2></div><span>{reviews.length} item</span></div><div className="review-grid">{reviews.map(item => <ReviewCard key={item.id} item={item} categories={categories} disabled={working === item.id} action={reviewAction} />)}</div></section>}
    <section className="panel"><div className="panel-title"><h2>Transaksi terbaru</h2><span>{transactions.length} tercatat</span></div><div className="transactions">{transactions.slice(0, 12).map(item => { const neutral = item.type === "UNCLASSIFIED" || item.type === "TRANSFER"; const prefix = item.type === "INCOME" ? "+" : neutral ? "↔ " : "−"; return <div className="row" key={item.id}><div><b>{item.description || (item.type === "INCOME" ? "Pemasukan" : neutral ? "Transfer" : "Pengeluaran")}</b><small>{new Date(item.transactionAt).toLocaleString("id-ID", { timeZone: "Asia/Jakarta" })}</small></div><div className={item.type === "INCOME" ? "positive" : neutral ? "neutral" : "negative"}>{prefix}{money(item.amount)}<small>{item.status}</small></div></div>; })}{transactions.length === 0 && <p className="empty">Belum ada transaksi.</p>}</div></section>
  </main>;
}

function ReviewCard({ item, categories, disabled, action }) {
  function confirm(event) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    action(item.id, "confirm", { categoryId: form.get("categoryId") || null, note: form.get("note") || null, rememberMerchant: form.get("rememberMerchant") === "on" });
  }
  if (item.type === "UNCLASSIFIED") return <TransferReviewCard item={item} categories={categories} disabled={disabled} action={action} />;
  return <article className="review-card">
    <div className="review-heading"><span className="review-reason">{reasonLabels[item.reason] || item.reason}</span><strong>{money(item.amount)}</strong></div>
    <h3>{item.merchantName || item.counterparty || item.description || "Transaksi tanpa keterangan"}</h3>
    <p>{new Date(item.transactionAt).toLocaleString("id-ID", { timeZone: "Asia/Jakarta" })} · {item.sourceType || "INPUT"}</p>
    {item.candidates?.length > 0 && <div className="candidates"><b>Kemungkinan sama dengan:</b>{item.candidates.map(candidate => <button className="candidate" type="button" disabled={disabled} key={candidate.id} onClick={() => action(item.id, "merge", { targetTransactionId: candidate.id })}><span>{candidate.description || "Transaksi sebelumnya"}</span><span>{Math.round(candidate.score * 100)}% cocok</span></button>)}</div>}
    <form className="review-form" onSubmit={confirm}>
      {item.type === "EXPENSE" && <label>Kategori<select name="categoryId" defaultValue={item.categoryId || ""} required><option value="" disabled>Pilih kategori</option>{categories.map(category => <option key={category.id} value={category.id}>{category.name}</option>)}</select></label>}
      <label>Catatan opsional<input name="note" defaultValue={item.note || ""} maxLength="1000" /></label>
      {item.type === "EXPENSE" && item.merchantName && <label className="remember-rule"><input name="rememberMerchant" type="checkbox" /> Ingat kategori ini untuk merchant tersebut</label>}
      <div className="review-actions"><button disabled={disabled}>{disabled ? "Memproses…" : "Konfirmasi"}</button><button className="danger" type="button" disabled={disabled} onClick={() => { if (window.confirm("Abaikan transaksi ini? Bukti tetap disimpan.")) action(item.id, "reject"); }}>Abaikan</button></div>
    </form>
  </article>;
}

function TransferReviewCard({ item, categories, disabled, action }) {
  function expense(event) { event.preventDefault(); const form = new FormData(event.currentTarget); action(item.id, "classify-transfer", { classification: "EXPENSE", categoryId: form.get("categoryId") }); }
  return <article className="review-card"><div className="review-heading"><span className="review-reason">TRANSFER PERLU KLASIFIKASI</span><strong>{money(item.amount)}</strong></div><h3>{item.counterparty || "Tujuan transfer belum dikenal"}</h3><p>{new Date(item.transactionAt).toLocaleString("id-ID", { timeZone: "Asia/Jakarta" })} · tidak dihitung sebagai pengeluaran</p><div className="review-actions transfer-actions"><button disabled={disabled} onClick={() => action(item.id, "classify-transfer", { classification: "OWN_ACCOUNT" })}>Rekening sendiri</button><button disabled={disabled} onClick={() => action(item.id, "classify-transfer", { classification: "HOUSEHOLD_ACCOUNT" })}>Household</button><button disabled={disabled} onClick={() => action(item.id, "classify-transfer", { classification: "INVESTMENT_ACCOUNT" })}>Investasi / RDN</button><button className="danger" disabled={disabled} onClick={() => action(item.id, "classify-transfer", { classification: "IGNORE" })}>Abaikan</button></div><form className="review-form" onSubmit={expense}><label>Jika ini pengeluaran<select name="categoryId" required defaultValue=""><option value="" disabled>Pilih kategori</option>{categories.map(category => <option key={category.id} value={category.id}>{category.name}</option>)}</select></label><button disabled={disabled}>Catat sebagai pengeluaran</button></form></article>;
}

function AnalyticsPanel({ title, items }) {
  const positive = items.filter(item => BigInt(item.amount || "0") > 0n);
  const max = positive.reduce((current, item) => BigInt(item.amount) > current ? BigInt(item.amount) : current, 0n);
  return <section className="panel mini-panel"><div className="panel-title"><h2>{title}</h2></div><div className="rank-list">{items.slice(0, 6).map((item, index) => { const amount = BigInt(item.amount || "0"); const width = max > 0n && amount > 0n ? Number((amount * 100n) / max) : 0; return <div key={item.id || `${item.name}-${index}`}><span>{item.name}</span><b>{money(item.amount)}</b><i style={{ width: `${width}%` }} /></div>; })}{items.length === 0 && <p className="empty">Belum ada data bulan ini.</p>}</div></section>;
}
