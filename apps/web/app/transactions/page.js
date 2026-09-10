"use client";

import { useCallback, useEffect, useRef, useState } from "react";
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
  const [wealthAccounts, setWealthAccounts] = useState([]);
  const [members, setMembers] = useState([]);
  const [selected, setSelected] = useState(null);
  const [evidence, setEvidence] = useState([]);
  const [audit, setAudit] = useState([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [createError, setCreateError] = useState("");
  const [creating, setCreating] = useState(false);
  const [nextCursor, setNextCursor] = useState("");
  const [cursorHistory, setCursorHistory] = useState([]);
  const [transactionType, setTransactionType] = useState("EXPENSE");
  const [transferPurpose, setTransferPurpose] = useState("INTERNAL_TRANSFER");
  const createDialog = useRef(null);

  const load = useCallback(async (search = window.location.search) => {
    const query = new URLSearchParams(search); if (!query.has("limit")) query.set("limit", "50"); const requestSearch = `?${query}`;
    setLoading(true);
    try {
      const [transactions, categoryResponse, accountResponse, wealthAccountResponse, memberResponse] = await Promise.all([fetch(`/api/v1/transactions${requestSearch}`), fetch("/api/v1/categories"), fetch("/api/v1/accounts"), fetch("/api/v1/wealth/accounts"), fetch("/api/v1/household/members")]);
      if (!transactions.ok) { const body = await transactions.json().catch(() => ({})); setError(body.error || "Transaksi belum dapat dimuat."); return; }
      setItems(await transactions.json()); setNextCursor(transactions.headers.get("X-Next-Cursor") || ""); setError("");
      if (categoryResponse.ok) setCategories(await categoryResponse.json());
      if (accountResponse.ok) setAccounts(await accountResponse.json());
      if (wealthAccountResponse.ok) setWealthAccounts(await wealthAccountResponse.json());
      if (memberResponse.ok) setMembers(await memberResponse.json());
    } catch {
      setError("Koneksi terputus saat memuat riwayat transaksi. Coba lagi.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!user) return;
    const state = { ...window.history.state, cursorHistory: window.history.state?.cursorHistory || [] };
    setCursorHistory(state.cursorHistory); window.history.replaceState(state, "", window.location.href); load();
  }, [user, load]);
  useEffect(() => {
    const onPopState = event => { setCursorHistory(event.state?.cursorHistory || []); load(); };
    window.addEventListener("popstate", onPopState); return () => window.removeEventListener("popstate", onPopState);
  }, [load]);
  useEffect(() => { if (user) { const id = new URLSearchParams(window.location.search).get("id"); if (id) openDetail({ id }); } }, [user]);

  async function filter(event) {
    event.preventDefault(); const form = new FormData(event.currentTarget); const query = new URLSearchParams();
    for (const [key, value] of form.entries()) if (value) query.set(key, value);
    query.set("limit", "50"); setCursorHistory([]);
    const search = query.toString() ? `?${query}` : ""; window.history.replaceState({ ...window.history.state, cursorHistory: [] }, "", `/transactions${search}`); await load(search);
  }

  async function nextPage() {
    if (!nextCursor) return;
    const query = new URLSearchParams(window.location.search); const history = [...cursorHistory, query.get("cursor") || ""]; setCursorHistory(history); query.set("cursor", nextCursor); query.set("limit", "50");
    const search = `?${query}`; window.history.pushState({ ...window.history.state, cursorHistory: history }, "", `/transactions${search}`); await load(search);
  }

  function previousPage() {
    if (!cursorHistory.length) return;
    window.history.back();
  }

  async function openDetail(item) {
    setSelected({ id: item.id }); setEvidence([]); setAudit([]); setDetailError(""); setDetailLoading(true);
    try {
      const [transaction, evidenceResponse, auditResponse] = await Promise.all([fetch(`/api/v1/transactions/${item.id}`), fetch(`/api/v1/transactions/${item.id}/evidence`), fetch(`/api/v1/transactions/${item.id}/audit`)]);
      if (!transaction.ok || !evidenceResponse.ok || !auditResponse.ok) throw new Error();
      setSelected(await transaction.json()); setEvidence(await evidenceResponse.json()); setAudit(await auditResponse.json());
    } catch { setDetailError("Detail transaksi belum dapat dimuat. Coba lagi."); }
    finally { setDetailLoading(false); }
  }

  async function createTransaction(event) {
    event.preventDefault(); setCreating(true); setCreateError("");
    const values = Object.fromEntries(new FormData(event.currentTarget));
    if (values.transactionAt) values.transactionAt = new Date(values.transactionAt).toISOString(); else delete values.transactionAt;
    if (!values.accountId) delete values.accountId; if (!values.note) delete values.note;
    if (values.type === "TRANSFER") delete values.categoryId; else { delete values.purpose; delete values.relatedWealthAccountId; }
    if (values.purpose === "INTERNAL_TRANSFER" || !values.relatedWealthAccountId) delete values.relatedWealthAccountId;
    try {
      const response = await fetch("/api/v1/transactions", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(values) });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(response.status < 500 ? body.error : "Transaksi belum dapat disimpan. Coba lagi.");
      createDialog.current?.close(); event.currentTarget.reset(); setTransactionType("EXPENSE"); setTransferPurpose("INTERNAL_TRANSFER"); await load();
    } catch (err) { setCreateError(err.message || "Koneksi terputus. Coba lagi."); }
    finally { setCreating(false); }
  }

  if (user === null) return <main className="loading">Memuat…</main>;
  if (!user) return null;
  const query = new URLSearchParams(typeof window === "undefined" ? "" : window.location.search);
  return <AppShell user={user} eyebrow="TRANSAKSI" title="Riwayat Transaksi" actions={<><button onClick={() => { setCreateError(""); createDialog.current?.showModal(); }}>Tambah transaksi</button><span className="header-meta">Maks. 250 hasil · Asia/Jakarta</span></>}>
    <dialog ref={createDialog} aria-labelledby="create-transaction-title" onClose={() => setCreateError("")}><form className="transaction-form" onSubmit={createTransaction}><div className="section-title"><h2 id="create-transaction-title">Tambah transaksi</h2><button type="button" aria-label="Tutup" onClick={() => createDialog.current?.close()}>×</button></div><label>Jenis<select name="type" required value={transactionType} onChange={event => setTransactionType(event.target.value)}><option value="EXPENSE">Pengeluaran</option><option value="INCOME">Pemasukan</option><option value="TRANSFER">Transfer</option></select></label>{transactionType === "TRANSFER" && <><label>Tujuan transfer<select name="purpose" value={transferPurpose} onChange={event => setTransferPurpose(event.target.value)}><option value="INTERNAL_TRANSFER">Transfer internal</option><option value="SAVINGS_TRANSFER">Pindah ke tabungan</option><option value="INVESTMENT_CONTRIBUTION">Kontribusi investasi</option><option value="ASSET_PURCHASE">Pembelian aset</option><option value="DEBT_PRINCIPAL_PAYMENT">Pembayaran pokok utang</option></select></label>{transferPurpose !== "INTERNAL_TRANSFER" && <label>Wealth Account terkait<select name="relatedWealthAccountId" required><option value="">Pilih Wealth Account</option>{wealthAccounts.filter(item => item.active !== false).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}</>}<label>Jumlah (IDR)<input name="amount" inputMode="numeric" pattern="[1-9][0-9]*" required/></label><label>Deskripsi<input name="description" maxLength="500" required/></label><label>Waktu<input name="transactionAt" type="datetime-local"/></label><label>Akun<select name="accountId" required={transactionType === "TRANSFER"}><option value="">{transactionType === "TRANSFER" ? "Pilih akun sumber" : "Tanpa akun"}</option>{accounts.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>{transactionType !== "TRANSFER" && <label>Kategori<select name="categoryId"><option value="">Tanpa kategori</option>{categories.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}<label>Catatan<textarea name="note"/></label>{createError && <p role="alert" className="error-notice">{createError}</p>}<div className="dialog-actions"><button type="button" onClick={() => createDialog.current?.close()}>Batal</button><button disabled={creating}>{creating ? "Menyimpan…" : "Simpan"}</button></div></form></dialog>
    <form className="filter-bar surface" onSubmit={filter}><input name="q" placeholder="Cari tempat transaksi atau catatan" defaultValue={query.get("q") || ""}/><input name="from" type="date" aria-label="Dari tanggal" defaultValue={query.get("from") || ""}/><input name="to" type="date" aria-label="Sampai tanggal" defaultValue={query.get("to") || ""}/><select name="type" defaultValue={query.get("type") || ""}><option value="">Semua tipe</option>{Object.entries(typeLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select><select name="categoryId" defaultValue={query.get("categoryId") || ""}><option value="">Semua kategori</option>{categories.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><select name="memberId" defaultValue={query.get("memberId") || ""}><option value="">Semua anggota</option>{members.map(item => <option key={item.id} value={item.id}>{item.displayName}</option>)}</select><select name="status" defaultValue={query.get("status") || ""}><option value="">Semua status</option>{Object.entries(statusLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select><select name="accountId" defaultValue={query.get("accountId") || ""}><option value="">Semua akun</option>{accounts.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><select name="source" defaultValue={query.get("source") || ""}><option value="">Semua sumber</option><option value="BANK_EMAIL">Email bank</option><option value="TELEGRAM_TEXT">Pesan Telegram</option><option value="TELEGRAM_IMAGE">Gambar Telegram</option><option value="WEB_MANUAL">Input web</option><option value="WEB_IMAGE">Dokumen web</option></select><button>Terapkan filter</button></form>
    <ErrorNotice message={error} retry={load}/>
    {loading ? <Skeleton cards={1} rows={6}/> : <><section className="surface ledger-panel"><div className="section-title"><div><span className="eyebrow">HASIL</span><h2>{items.length} transaksi</h2></div></div><TransactionList items={items} onSelect={openDetail}/></section><div className="pagination-actions"><button className="secondary" disabled={loading || !cursorHistory.length} onClick={previousPage}>Sebelumnya</button><button className="secondary" disabled={loading || !nextCursor} onClick={nextPage}>Berikutnya</button></div></>}
    {selected && <div className="drawer-backdrop" onClick={() => setSelected(null)}><aside className="detail-drawer" aria-busy={detailLoading} onClick={event => event.stopPropagation()}><button className="drawer-close" aria-label="Tutup detail" onClick={() => setSelected(null)}>×</button>{detailLoading ? <p>Memuat detail…</p> : detailError ? <div><p role="alert">{detailError}</p><button onClick={() => openDetail(selected)}>Coba lagi</button></div> : <><span className="eyebrow">DETAIL TRANSAKSI</span><h2>{selected.merchantName || selected.description || typeLabel[selected.type]}</h2><strong className="detail-amount">{money(selected.amount)}</strong><dl style={{ border: 0, borderRadius: 0, overflow: "visible" }}><div><dt>Tanggal</dt><dd>{dateTime(selected.transactionAt)}</dd></div><div><dt>Kategori</dt><dd>{selected.categoryName || "Belum dikategorikan"}</dd></div><div><dt>Akun</dt><dd>{selected.accountName || "—"}</dd></div><div><dt>Sumber pencatatan</dt><dd>{sourceLabel(selected.sourceType)}</dd></div><div><dt>Status</dt><dd>{statusLabel[selected.status] || selected.status}</dd></div>{selected.type === "TRANSFER" && <><div><dt>Tujuan transfer</dt><dd>{purposeLabel(selected.purpose)}</dd></div><div><dt>Wealth Account</dt><dd>{selected.relatedWealthAccountName || "—"}</dd></div></>}</dl><h3>Sumber Pencatatan</h3><div className="timeline">{evidence.map(item => <article key={item.id}><i>✓</i><div><b>{evidenceLabel(item.evidenceType)}</b><small>{sourceLabel(item.sourceType)} · {dateTime(item.receivedAt)}</small></div></article>)}{!evidence.length && <p className="empty compact">Belum ada sumber pencatatan terhubung.</p>}</div><h3>Riwayat Perubahan</h3><div className="timeline">{audit.map(item => <article key={item.id}><i>•</i><div><b>{item.action}</b><small>{item.actorType} · {dateTime(item.createdAt)}</small></div></article>)}{!audit.length && <p className="empty compact">Belum ada perubahan tercatat.</p>}</div></>}</aside></div>}
  </AppShell>;
}

function purposeLabel(value) { return ({ INTERNAL_TRANSFER: "Transfer internal", SAVINGS_TRANSFER: "Pindah ke tabungan", INVESTMENT_CONTRIBUTION: "Kontribusi investasi", ASSET_PURCHASE: "Pembelian aset", DEBT_PRINCIPAL_PAYMENT: "Pembayaran pokok utang" }[value] || value || "—"); }
function sourceLabel(value) { return ({ BANK_EMAIL: "Email bank", TELEGRAM_TEXT: "Pesan Telegram", TELEGRAM_IMAGE: "Gambar Telegram", WEB_MANUAL: "Input Web", WEB_IMAGE: "Dokumen Web" }[value] || value || "—"); }
function evidenceLabel(value) { return ({ BANK_EMAIL: "Email Bank", TELEGRAM_REVIEW_REPLY: "Balasan tinjauan Telegram", TELEGRAM_TEXT: "Pesan Telegram", TELEGRAM_IMAGE: "Gambar Telegram", PAYSLIP_IMAGE: "Slip Gaji", TRANSACTION_SCREENSHOT: "Bukti Transaksi" }[value] || value || "Sumber pencatatan"); }
