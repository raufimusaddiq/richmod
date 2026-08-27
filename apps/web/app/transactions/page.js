"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import TransactionList from "../components/TransactionList";
import useAuth from "../components/useAuth";
import { dateTime, money, statusLabel, typeLabel } from "../lib/format";

export const dynamic = "force-dynamic";

export default function TransactionsPage() {
  const user = useAuth();
  const [items, setItems] = useState([]);
  const [categories, setCategories] = useState([]);
  const [accounts, setAccounts] = useState([]);
  const [members, setMembers] = useState([]);
  const [selected, setSelected] = useState(null);
  const [evidence, setEvidence] = useState([]);
  const [audit, setAudit] = useState([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(async (search = window.location.search) => {
    setLoading(true);
    try {
      const [transactions, categoryResponse, accountResponse, memberResponse] = await Promise.all([fetch(`/api/v1/transactions${search}`), fetch("/api/v1/categories"), fetch("/api/v1/accounts"), fetch("/api/v1/household/members")]);
      if (!transactions.ok) { const body = await transactions.json().catch(() => ({})); setError(body.error || "Transaksi belum dapat dimuat."); return; }
      setItems(await transactions.json()); setError("");
      if (categoryResponse.ok) setCategories(await categoryResponse.json());
      if (accountResponse.ok) setAccounts(await accountResponse.json());
      if (memberResponse.ok) setMembers(await memberResponse.json());
    } catch {
      setError("Koneksi terputus saat memuat riwayat transaksi. Coba lagi.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { if (user) load(); }, [user, load]);
  useEffect(() => { if (user) { const id = new URLSearchParams(window.location.search).get("id"); if (id) openDetail({ id }); } }, [user]);

  async function filter(event) {
    event.preventDefault(); const form = new FormData(event.currentTarget); const query = new URLSearchParams();
    for (const [key, value] of form.entries()) if (value) query.set(key, value);
    const search = query.toString() ? `?${query}` : ""; window.history.replaceState({}, "", `/transactions${search}`); await load(search);
  }

  async function openDetail(item) {
    const [transaction, evidenceResponse, auditResponse] = await Promise.all([fetch(`/api/v1/transactions/${item.id}`), fetch(`/api/v1/transactions/${item.id}/evidence`), fetch(`/api/v1/transactions/${item.id}/audit`)]);
    if (transaction.ok) setSelected(await transaction.json());
    if (evidenceResponse.ok) setEvidence(await evidenceResponse.json());
    if (auditResponse.ok) setAudit(await auditResponse.json());
  }

  if (user === null) return <main className="loading">Memuat…</main>;
  if (!user) return null;
  const query = new URLSearchParams(typeof window === "undefined" ? "" : window.location.search);
  return <AppShell user={user} eyebrow="TRANSAKSI" title="Riwayat Transaksi" actions={<span className="header-meta">Maks. 250 hasil · Asia/Jakarta</span>}>
    <form className="filter-bar surface" onSubmit={filter}><input name="q" placeholder="Cari merchant atau catatan" defaultValue={query.get("q") || ""}/><input name="from" type="date" aria-label="Dari tanggal" defaultValue={query.get("from") || ""}/><input name="to" type="date" aria-label="Sampai tanggal" defaultValue={query.get("to") || ""}/><select name="type" defaultValue={query.get("type") || ""}><option value="">Semua tipe</option>{Object.entries(typeLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select><select name="categoryId" defaultValue={query.get("categoryId") || ""}><option value="">Semua kategori</option>{categories.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><select name="memberId" defaultValue={query.get("memberId") || ""}><option value="">Semua anggota</option>{members.map(item => <option key={item.id} value={item.id}>{item.displayName}</option>)}</select><select name="status" defaultValue={query.get("status") || ""}><option value="">Semua status</option>{Object.entries(statusLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select><select name="accountId" defaultValue={query.get("accountId") || ""}><option value="">Semua akun</option>{accounts.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><select name="source" defaultValue={query.get("source") || ""}><option value="">Semua sumber</option><option value="BANK_EMAIL">Bank Jago</option><option value="TELEGRAM_TEXT">Telegram text</option><option value="TELEGRAM_IMAGE">Telegram image</option><option value="WEB_MANUAL">Web manual</option><option value="WEB_IMAGE">Web document</option></select><button>Terapkan filter</button></form>
    <ErrorNotice message={error} retry={load}/>
    {loading ? <Skeleton cards={1} rows={6}/> : <section className="surface ledger-panel"><div className="section-title"><div><span className="eyebrow">HASIL</span><h2>{items.length} transaksi</h2></div></div><TransactionList items={items} onSelect={openDetail}/></section>}
    {selected && <div className="drawer-backdrop" onClick={() => setSelected(null)}><aside className="detail-drawer" onClick={event => event.stopPropagation()}><button className="drawer-close" onClick={() => setSelected(null)}>×</button><span className="eyebrow">DETAIL TRANSAKSI</span><h2>{selected.merchantName || selected.description || typeLabel[selected.type]}</h2><strong className="detail-amount">{money(selected.amount)}</strong><dl><div><dt>Tanggal</dt><dd>{dateTime(selected.transactionAt)}</dd></div><div><dt>Kategori</dt><dd>{selected.categoryName || "Belum dikategorikan"}</dd></div><div><dt>Akun</dt><dd>{selected.accountName || "—"}</dd></div><div><dt>Sumber pencatatan</dt><dd>{sourceLabel(selected.sourceType)}</dd></div><div><dt>Status</dt><dd>{statusLabel[selected.status] || selected.status}</dd></div></dl><h3>Sumber Pencatatan</h3><div className="timeline">{evidence.map(item => <article key={item.id}><i>✓</i><div><b>{evidenceLabel(item.evidenceType)}</b><small>{sourceLabel(item.sourceType)} · {dateTime(item.receivedAt)}</small></div></article>)}{!evidence.length && <p className="empty compact">Belum ada sumber pencatatan terhubung.</p>}</div><h3>Riwayat Perubahan</h3><div className="timeline">{audit.map(item => <article key={item.id}><i>•</i><div><b>{item.action}</b><small>{item.actorType} · {dateTime(item.createdAt)}</small></div></article>)}{!audit.length && <p className="empty compact">Belum ada perubahan tercatat.</p>}</div></aside></div>}
  </AppShell>;
}

function sourceLabel(value) { return ({ BANK_EMAIL: "Email Bank Jago", TELEGRAM_TEXT: "Pesan Telegram", TELEGRAM_IMAGE: "Gambar Telegram", WEB_MANUAL: "Input Web", WEB_IMAGE: "Dokumen Web" }[value] || value || "—"); }
function evidenceLabel(value) { return ({ BANK_EMAIL: "Email Bank", TELEGRAM_REVIEW_REPLY: "Balasan tinjauan Telegram", TELEGRAM_TEXT: "Pesan Telegram", TELEGRAM_IMAGE: "Gambar Telegram", PAYSLIP_IMAGE: "Slip Gaji", TRANSACTION_SCREENSHOT: "Bukti Transaksi" }[value] || value || "Sumber pencatatan"); }
