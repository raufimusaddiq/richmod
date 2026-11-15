"use client";

import { useEffect, useRef, useState } from "react";
import DateInput from "../components/DateInput";
import AppShell from "../components/AppShell";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import TransactionList from "../components/TransactionList";
import useAuth from "../components/useAuth";
import useDialogs from "../components/useDialogs";
import useDrawerA11y from "../components/useDrawerA11y";
import { LoadError, useDrawerRecord, useGuardedLoad } from "../lib/useLoadState";
import { dateTime, money, statusLabel, typeLabel } from "../lib/format";
import { selectionHref } from "../lib/cycleReview";
import { cycleEntryWindow, withinCycleWindow } from "../lib/missingTransaction";
import { actorText, auditActionText, evidenceTypeText } from "../lib/labels";
import { VOID_CONFIRM_MESSAGE, VOID_SUCCESS_MESSAGE, canEditTransaction, canVoidTransaction, createErrorMessage, createFormErrors, editErrorMessage, editFormErrors, editPayload, editableFields, dateRangeError, hasActiveFilters, isScopedToTransaction, listSearch, normalizeAmount, transactionsErrorMessage, trapTarget, voidFailure } from "../lib/transactionForm";

const PAGE_SIZE = 50;
const focusable = "a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";


export default function TransactionsPage() {
  const user = useAuth();
  const [items, setItems] = useState([]);
  const [categories, setCategories] = useState([]);
  const [accounts, setAccounts] = useState([]);
  const [wealthAccounts, setWealthAccounts] = useState([]);
  const [members, setMembers] = useState([]);
  // The drawer is one state machine {closed | loading | error | ready, id, data} whose
  // request is aborted on close or on opening another row.
  const drawer = useDrawerRecord(async (id, signal) => {
    const [transaction, evidenceResponse, auditResponse] = await Promise.all([fetch(`/api/v1/transactions/${id}`, { signal }), fetch(`/api/v1/transactions/${id}/evidence`, { signal }), fetch(`/api/v1/transactions/${id}/audit`, { signal })]);
    if (!transaction.ok || !evidenceResponse.ok || !auditResponse.ok) throw new LoadError("Detail transaksi belum dapat dimuat. Coba lagi.");
    return { transaction: await transaction.json(), evidence: await evidenceResponse.json(), audit: await auditResponse.json() };
  }, "Detail transaksi belum dapat dimuat. Coba lagi.");
  const closeDetail = drawer.close;
  const drawerRef = useDrawerA11y(drawer.status !== "closed", closeDetail);
  const selected = drawer.data?.transaction || (drawer.id ? { id: drawer.id } : null);
  const evidence = drawer.data?.evidence || [], audit = drawer.data?.audit || [];
  const [createError, setCreateError] = useState("");
  const [creating, setCreating] = useState(false);
  const [nextCursor, setNextCursor] = useState("");
  const [cursorHistory, setCursorHistory] = useState([]);
  const [transactionType, setTransactionType] = useState("EXPENSE");
  const [transferPurpose, setTransferPurpose] = useState("INTERNAL_TRANSFER");
  // Only the row a create returned, and only a filter, may animate; a first load keeps
  // both empty.
  const [createdId, setCreatedId] = useState(""), [refreshToken, setRefreshToken] = useState(0);
  // The token marks the filter re-run on screen; the ref is the last one painted. A
  // render whose values differ is the one fade.
  const consumedRefresh = useRef(0);
  const refreshPending = refreshToken !== consumedRefresh.current;
  const createDialog = useRef(null), createOpener = useRef(null);
  const [fieldErrors, setFieldErrors] = useState({}), [filterError, setFilterError] = useState(""), [filterKey, setFilterKey] = useState(0);
  // The add form is a native modal <dialog> (top layer, background inert) with the
  // drawer pattern: focus moves in on open, Tab cycles inside, Escape closes, focus
  // returns to the opener.
  function openCreate() {
    setCreateError(""); setFieldErrors({}); createOpener.current = document.activeElement;
    const dialog = createDialog.current; if (!dialog || dialog.open) return;
    dialog.showModal(); (dialog.querySelector("select, input") || dialog).focus();
  }
  function closeCreate() { createDialog.current?.close(); }
  function onCreateClosed() {
    setCreateError(""); setFieldErrors({});
    const opener = createOpener.current; createOpener.current = null;
    if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
  }
  function trapCreateTab(event) {
    if (event.key !== "Tab") return;
    const dialog = createDialog.current, items = [...dialog.querySelectorAll(focusable)];
    const target = trapTarget(items, document.activeElement, event.shiftKey, dialog);
    if (target) { event.preventDefault(); target.focus(); }
  }
  // Edit form: same native modal <dialog> pattern as the add form. The drawer's
  // own Escape/Tab handler is on document, so the dialog stops key events here.
  const editDialog = useRef(null), editOpener = useRef(null), editStale = useRef(false);
  const [editError, setEditError] = useState(""), [editing, setEditing] = useState(false), [editFieldErrors, setEditFieldErrors] = useState({});
  function openEdit() {
    setEditError(""); setEditFieldErrors({}); editStale.current = false; editOpener.current = document.activeElement;
    const dialog = editDialog.current; if (!dialog || dialog.open) return;
    dialog.showModal(); (dialog.querySelector("select, input") || dialog).focus();
  }
  function closeEdit() { editDialog.current?.close(); }
  function onEditClosed() {
    setEditError(""); setEditFieldErrors({});
    const opener = editOpener.current; editOpener.current = null;
    if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    if (editStale.current && drawer.id) { editStale.current = false; drawer.open(drawer.id); }
  }
  function onEditKeyDown(event) {
    event.stopPropagation();
    if (event.key !== "Tab") return;
    const dialog = editDialog.current, items = [...dialog.querySelectorAll(focusable)];
    const target = trapTarget(items, document.activeElement, event.shiftKey, dialog);
    if (target) { event.preventDefault(); target.focus(); }
  }
  async function saveEdit(event) {
    event.preventDefault(); if (editing || !selected?.id) return;
    const formEl = event.currentTarget, values = Object.fromEntries(new FormData(formEl));
    const errors = editFormErrors(values, selected); setEditFieldErrors(errors);
    if (Object.keys(errors).length) { setEditError("Lengkapi isian yang ditandai."); formEl.querySelector("[aria-invalid=true]")?.focus(); return; }
    setEditing(true); setEditError("");
    try {
      const payload = editPayload(selected, values);
      if (!Object.keys(payload).length) { closeEdit(); return; }
      const response = await fetch(`/api/v1/transactions/${selected.id}`, { method: "PATCH", headers: { "Content-Type": "application/json", "If-Match": selected.updatedAt }, body: JSON.stringify(payload) });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) { if (body?.code === "STALE_EDIT") editStale.current = true; throw new Error(editErrorMessage(response.status, body)); }
      closeEdit(); drawer.open(selected.id); load();
    } catch (err) { setEditError(err.name === "TypeError" ? "Koneksi terputus. Coba lagi." : err.message || "Koneksi terputus. Coba lagi."); }
    finally { setEditing(false); }
  }
  // Set when the residual review handed off a missing transaction: the form opens with
  // the date limited to that salary cycle.
  const [missingWindow, setMissingWindow] = useState(null);

  // Void: the confirm dialog, then POST. The ref refuses a second start in the same tick;
  // `voiding` only disables the button. The notice belongs to one record.
  const { confirm, dialogs } = useDialogs();
  const voidPending = useRef(false), openId = useRef(null);
  const [voiding, setVoiding] = useState(false), [voidNotice, setVoidNotice] = useState(null);
  openId.current = drawer.id;
  useEffect(() => { setVoidNotice(null); }, [drawer.id]);
  async function voidTransaction() {
    const id = selected?.id;
    if (voidPending.current || !canVoidTransaction(selected)) return;
    voidPending.current = true;
    try {
      if (!(await confirm(VOID_CONFIRM_MESSAGE, { confirmLabel: "Ya, batalkan", danger: true }))) return;
      setVoiding(true); setVoidNotice(null);
      let outcome;
      try {
        const response = await fetch(`/api/v1/transactions/${id}/void`, { method: "POST" });
        outcome = response.ok ? { kind: "success", message: VOID_SUCCESS_MESSAGE } : voidFailure(response.status, await response.json().catch(() => ({})));
      } catch { outcome = { kind: "error", message: "Koneksi terputus. Periksa status transaksi sebelum mencoba lagi.", reload: true }; }
      // A drawer closed or pointed at another row while the request ran is left alone.
      if (openId.current === id) { setVoidNotice(outcome); if (outcome.kind === "success" || outcome.reload) drawer.open(id); }
      if (outcome.kind === "success" || outcome.reload) load();
    } finally { voidPending.current = false; setVoiding(false); }
  }

  // Superseded loads are aborted and ignored. A failure keeps the rows on screen under
  // the error banner; the first-load skeleton shows only until one load succeeds.
  const loader = useGuardedLoad(async ({ signal, current }, search = window.location.search) => {
    const requestSearch = listSearch(search);
    const [transactions, categoryResponse, accountResponse, wealthAccountResponse, memberResponse] = await Promise.all([fetch(`/api/v1/transactions${requestSearch}`, { signal }), fetch("/api/v1/categories", { signal }), fetch("/api/v1/accounts", { signal }), fetch("/api/v1/wealth/accounts", { signal }), fetch("/api/v1/household/members", { signal })]);
    if (!transactions.ok) { const body = await transactions.json().catch(() => ({})); throw new LoadError(transactionsErrorMessage(transactions.status, body)); }
    const list = await transactions.json(), cursor = transactions.headers.get("X-Next-Cursor") || "";
    const optional = async response => (response.ok ? response.json().catch(() => null) : null);
    const [categoryList, accountList, wealthList, memberList] = [await optional(categoryResponse), await optional(accountResponse), await optional(wealthAccountResponse), await optional(memberResponse)];
    if (!current()) return;
    setItems(Array.isArray(list) ? list : []); setNextCursor(cursor);
    if (Array.isArray(categoryList)) setCategories(categoryList);
    if (Array.isArray(accountList)) setAccounts(accountList);
    if (Array.isArray(wealthList)) setWealthAccounts(wealthList);
    if (Array.isArray(memberList)) setMembers(memberList);
  // Keep the visible rows on failure but drop the cursor, which belongs to the previous
  // query.
  }, { fallback: "Koneksi terputus saat memuat riwayat transaksi. Coba lagi.", onFailure: () => setNextCursor("") });
  const load = loader.run;
  const loading = loader.loading || loader.refreshing;
  const error = loader.error;

  useEffect(() => {
    if (!user) return;
    const state = { ...window.history.state, cursorHistory: window.history.state?.cursorHistory || [] };
    setCursorHistory(state.cursorHistory); window.history.replaceState(state, "", window.location.href); load();
  }, [user, load]);
  useEffect(() => {
    const onPopState = event => { setCursorHistory(event.state?.cursorHistory || []); load(); };
    window.addEventListener("popstate", onPopState); return () => window.removeEventListener("popstate", onPopState);
  }, [load]);
  // Consume the marker once the commit that painted data-refreshed is done. Mutating the
  // ref, not state, keeps the attribute on screen for the whole fade; later renders
  // compare equal and stay still. Skip while loading: the first commit is the skeleton.
  useEffect(() => {
    if (!loading && loader.hasLoaded && refreshPending) consumedRefresh.current = refreshToken;
  }, [loading, loader.hasLoaded, refreshPending, refreshToken]);
  useEffect(() => { if (user) { const id = new URLSearchParams(window.location.search).get("id"); if (id) drawer.open(id); } }, [user, drawer.open]);
  useEffect(() => { if (!user) return; const window_ = cycleEntryWindow(window.location.search); if (window_) { setMissingWindow(window_); setCreateError(""); openCreate(); } }, [user]);

  async function filter(event) {
    event.preventDefault(); const form = new FormData(event.currentTarget); const query = new URLSearchParams();
    for (const [key, value] of form.entries()) if (value) query.set(key, value);
    const rangeError = dateRangeError(form.get("from"), form.get("to"));
    if (rangeError) { setFilterError(rangeError); return; }
    setFilterError("");
    const previous = new URLSearchParams(window.location.search);
    if (previous.has("cycle")) query.set("cycle", previous.get("cycle"));
    if (previous.has("review")) query.set("review", previous.get("review"));
    if (previous.has("merchantId")) query.set("merchantId", previous.get("merchantId"));
    query.set("limit", String(PAGE_SIZE)); setCursorHistory([]); setCreatedId(""); setRefreshToken(token => token + 1);
    const search = query.toString() ? `?${query}` : ""; window.history.replaceState({ ...window.history.state, cursorHistory: [] }, "", `/transactions${search}`); await load(search);
  }

  function resetFilters() {
    setFilterError(""); setMissingWindow(null); setFilterKey(key => key + 1); setCursorHistory([]); setCreatedId(""); setRefreshToken(token => token + 1);
    window.history.replaceState({ ...window.history.state, cursorHistory: [] }, "", "/transactions"); load("");
  }

  async function nextPage() {
    if (!nextCursor) return;
    const query = new URLSearchParams(window.location.search); const history = [...cursorHistory, query.get("cursor") || ""]; setCursorHistory(history); query.set("cursor", nextCursor); query.set("limit", String(PAGE_SIZE));
    const search = `?${query}`; window.history.pushState({ ...window.history.state, cursorHistory: history }, "", `/transactions${search}`); await load(search);
  }

  function previousPage() {
    if (!cursorHistory.length) return;
    window.history.back();
  }

  const openDetail = item => drawer.open(item.id);

  async function createTransaction(event) {
    event.preventDefault(); const formEl = event.currentTarget; setCreating(true); setCreateError("");
    const values = Object.fromEntries(new FormData(event.currentTarget));
    const errors = createFormErrors(values, { cycle: Boolean(missingWindow) });
    setFieldErrors(errors);
    values.amount = normalizeAmount(values.amount);
    if (Object.keys(errors).length) { setCreating(false); setCreateError("Lengkapi isian yang ditandai."); formEl.querySelector("[aria-invalid=true]")?.focus(); return; }
    if (missingWindow && !withinCycleWindow(values.transactionAt, missingWindow)) { setCreateError(`Pilih waktu di dalam siklus ${missingWindow.from} sampai ${missingWindow.to}.`); setCreating(false); return; }
    if (values.transactionAt) values.transactionAt = new Date(values.transactionAt).toISOString(); else delete values.transactionAt;
    if (!values.accountId) delete values.accountId; if (!values.note) delete values.note;
    if (values.type === "TRANSFER") delete values.categoryId; else { delete values.purpose; delete values.relatedWealthAccountId; }
    if (values.purpose === "INTERNAL_TRANSFER" || !values.relatedWealthAccountId) delete values.relatedWealthAccountId;
    try {
      const response = await fetch("/api/v1/transactions", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(values) });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(createErrorMessage(response.status, body));
      closeCreate(); formEl.reset(); if (missingWindow) { setMissingWindow(null); const next = new URLSearchParams(window.location.search); next.delete("record"); window.history.replaceState(window.history.state, "", `/transactions?${next}`); } setTransactionType("EXPENSE"); setTransferPurpose("INTERNAL_TRANSFER"); setCreatedId(body?.id || ""); await load();
    } catch (err) { setCreateError(err.name === "TypeError" ? "Koneksi terputus. Coba lagi." : err.message || "Koneksi terputus. Coba lagi."); }
    finally { setCreating(false); }
  }

  if (user === null) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  if (!user) return null;
  const scoped = isScopedToTransaction(typeof window === "undefined" ? "" : window.location.search);
  const query = new URLSearchParams(typeof window === "undefined" ? "" : window.location.search);
  return <AppShell user={user} eyebrow="TRANSAKSI" title="Riwayat Transaksi" actions={<><button onClick={openCreate}>Tambah transaksi</button><span className="header-meta">{PAGE_SIZE} per halaman · Asia/Jakarta</span></>}>
    {query.get("cycle") && <p className="notice"><a href={selectionHref({ view: "cycle", cycle: query.get("cycle"), category: query.get("categoryId") || "", step: query.get("review") || "" })}>Kembali ke tinjauan siklus</a> · {query.get("from")} sampai {query.get("to")}</p>}
    {query.get("merchantId") && <div className="notice feedback"><span>Filter merchant terpilih dari tinjauan siklus.</span><button className="secondary" type="button" onClick={() => { const next = new URLSearchParams(window.location.search); next.delete("merchantId"); next.delete("cursor"); window.history.replaceState({ ...window.history.state, cursorHistory: [] }, "", `/transactions?${next}`); setCursorHistory([]); load(`?${next}`); }}>Hapus filter merchant</button></div>}
    <dialog ref={createDialog} className="create-dialog" role="dialog" aria-modal="true" aria-labelledby="create-transaction-title" tabIndex={-1} onClose={onCreateClosed} onKeyDown={trapCreateTab}><button type="button" className="dialog-close" aria-label="Tutup" onClick={closeCreate}>×</button><form className="transaction-form" onSubmit={createTransaction} noValidate><div className="section-title"><h2 id="create-transaction-title">Tambah transaksi</h2></div><div className="dialog-fields"><label>Jenis<select name="type" required value={transactionType} onChange={event => setTransactionType(event.target.value)}><option value="EXPENSE">Pengeluaran</option><option value="INCOME">Pemasukan</option><option value="TRANSFER">Transfer</option></select></label>{transactionType === "TRANSFER" && <><label>Tujuan transfer<select name="purpose" value={transferPurpose} onChange={event => setTransferPurpose(event.target.value)}><option value="INTERNAL_TRANSFER">Transfer internal</option><option value="SAVINGS_TRANSFER">Pindah ke tabungan</option><option value="INVESTMENT_CONTRIBUTION">Kontribusi investasi</option><option value="ASSET_PURCHASE">Pembelian aset</option><option value="DEBT_PRINCIPAL_PAYMENT">Pembayaran pokok utang</option></select></label>{transferPurpose !== "INTERNAL_TRANSFER" && <label>Akun kekayaan terkait<select name="relatedWealthAccountId" required aria-invalid={fieldErrors.relatedWealthAccountId ? "true" : undefined}><option value="">Pilih akun kekayaan</option>{wealthAccounts.filter(item => item.active !== false).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}</>}<label><span>Jumlah (IDR) <span className="required-mark" aria-hidden="true">*</span></span><input name="amount" inputMode="numeric" required aria-required="true" aria-invalid={fieldErrors.amount ? "true" : undefined} aria-describedby={fieldErrors.amount ? "create-amount-error" : undefined}/>{fieldErrors.amount && <small id="create-amount-error" className="field-error">{fieldErrors.amount}</small>}</label><label><span>Deskripsi <span className="required-mark" aria-hidden="true">*</span></span><input name="description" maxLength="500" required aria-required="true" aria-invalid={fieldErrors.description ? "true" : undefined} aria-describedby={fieldErrors.description ? "create-description-error" : undefined}/>{fieldErrors.description && <small id="create-description-error" className="field-error">{fieldErrors.description}</small>}</label>{missingWindow && <p className="notice">Catat transaksi yang terlewat di siklus {missingWindow.from} sampai {missingWindow.to}. Waktunya dibatasi ke siklus itu agar sisa siklusnya dihitung ulang.</p>}<label>Waktu<DateInput key={missingWindow ? `cycle-${missingWindow.from}` : "free"} name="transactionAt" type="datetime-local" aria-invalid={fieldErrors.transactionAt ? "true" : undefined} required={Boolean(missingWindow)} min={missingWindow?.min} max={missingWindow?.max} defaultValue={missingWindow?.value}/></label><label>Akun<select name="accountId" required={transactionType === "TRANSFER"} aria-invalid={fieldErrors.accountId ? "true" : undefined}><option value="">{transactionType === "TRANSFER" ? "Pilih akun sumber" : "Tanpa akun"}</option>{accounts.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>{transactionType !== "TRANSFER" && <label>Kategori<select name="categoryId"><option value="">Tanpa kategori</option>{categories.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}<label>Catatan<textarea name="note"/></label>{createError && <p role="alert" className="error-notice">{createError}</p>}</div><div className="dialog-actions"><button type="button" onClick={closeCreate}>Batal</button><button disabled={creating}>{creating ? "Menyimpan…" : "Simpan"}</button></div></form></dialog>
    <form key={filterKey} className="filter-bar surface" onSubmit={filter} inert={loading}><input aria-label="Cari transaksi" name="q" placeholder="Cari tempat transaksi atau catatan" defaultValue={query.get("q") || ""}/><label className="filter-date"><span>Dari tanggal</span><DateInput name="from" type="date" defaultValue={query.get("from") || ""}/></label><label className="filter-date"><span>Sampai tanggal</span><DateInput name="to" type="date" defaultValue={query.get("to") || ""}/></label><select aria-label="Tipe transaksi" name="type" defaultValue={query.get("type") || ""}><option value="">Semua tipe</option><option value="SPENDING">Pengeluaran & refund</option>{Object.entries(typeLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select><select aria-label="Kategori" name="categoryId" defaultValue={query.get("categoryId") || ""}><option value="">Semua kategori</option><option value="uncategorized">Belum dikategorikan</option>{categories.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><select aria-label="Anggota" name="memberId" defaultValue={query.get("memberId") || ""}><option value="">Semua anggota</option>{members.map(item => <option key={item.id} value={item.id}>{item.displayName}</option>)}</select><select aria-label="Status" name="status" defaultValue={query.get("status") || ""}><option value="">Semua status</option>{Object.entries(statusLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select><select aria-label="Akun" name="accountId" defaultValue={query.get("accountId") || ""}><option value="">Semua akun</option>{accounts.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><select aria-label="Sumber" name="source" defaultValue={query.get("source") || ""}><option value="">Semua sumber</option><option value="BANK_EMAIL">Email bank</option><option value="TELEGRAM_TEXT">Pesan Telegram</option><option value="TELEGRAM_IMAGE">Gambar Telegram</option><option value="WEB_MANUAL">Input web</option><option value="WEB_IMAGE">Dokumen web</option></select><input name="merchantId" type="hidden" defaultValue={query.get("merchantId") || ""}/><button>Terapkan filter</button>{hasActiveFilters(`?${query}`) && <button type="button" className="secondary" onClick={resetFilters}>Hapus filter</button>}</form>{filterError && <p role="alert" className="error-notice">{filterError}</p>}
    <ErrorNotice message={error} retry={() => load()}/>
    {!loader.hasLoaded && !loader.failed ? <Skeleton cards={1} rows={6}/> : !loader.hasLoaded ? null : <><section className="surface ledger-panel" aria-busy={loading} data-refreshed={(refreshPending && !loading) || undefined}><div className="section-title"><div><span className="eyebrow">HASIL</span><h2>{items.length} transaksi</h2>{scoped && <p className="notice">Menampilkan hanya transaksi dari tautan bukti. <a href="/transactions" onClick={event => { event.preventDefault(); resetFilters(); }}>Lihat semua transaksi</a></p>}</div></div><TransactionList items={items} onSelect={openDetail} newId={createdId}/></section>{!scoped && <div className="pagination-actions"><button className="secondary" disabled={loading || !cursorHistory.length} onClick={previousPage}>Sebelumnya</button><button className="secondary" disabled={loading || !nextCursor} onClick={nextPage}>Berikutnya</button></div>}</>}
    {drawer.status === "ready" && canEditTransaction(selected) && (() => { const fields = editableFields(selected); return <dialog ref={editDialog} className="create-dialog" role="dialog" aria-modal="true" aria-labelledby="edit-transaction-title" tabIndex={-1} onClose={onEditClosed} onKeyDown={onEditKeyDown}><button type="button" className="dialog-close" aria-label="Tutup" onClick={closeEdit}>×</button><form key={`${selected.id}-${selected.updatedAt}`} className="transaction-form" onSubmit={saveEdit} noValidate><div className="section-title"><h2 id="edit-transaction-title">Ubah transaksi</h2></div><p className="notice">Jumlah, waktu, dan jenis tidak bisa diubah. Batalkan lalu catat ulang jika salah.</p>{fields.category && <label>Kategori<select name="categoryId" defaultValue={selected.categoryId || ""}><option value="">Tanpa kategori</option>{categories.filter(item => item.active || item.id === selected.categoryId).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}{fields.account && <label>Akun<select name="accountId" defaultValue={selected.accountId || ""}><option value="">Tanpa akun</option>{accounts.filter(item => item.active || item.id === selected.accountId).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}<label>Deskripsi{selected.description && <span className="required-mark" aria-hidden="true"> *</span>}<input name="description" maxLength="500" required={Boolean(selected.description)} aria-required={selected.description ? "true" : undefined} defaultValue={selected.description || ""} aria-invalid={editFieldErrors.description ? "true" : undefined} aria-describedby={editFieldErrors.description ? "edit-description-error" : undefined}/>{editFieldErrors.description && <small id="edit-description-error" className="field-error">{editFieldErrors.description}</small>}</label><label>Catatan<textarea name="note" maxLength="2000" defaultValue={selected.note || ""} aria-invalid={editFieldErrors.note ? "true" : undefined} aria-describedby={editFieldErrors.note ? "edit-note-error" : undefined}/>{editFieldErrors.note && <small id="edit-note-error" className="field-error">{editFieldErrors.note}</small>}</label>{editError && <p role="alert" className="error-notice">{editError}</p>}<div className="dialog-actions"><button type="button" onClick={closeEdit}>Batal</button><button disabled={editing}>{editing ? "Menyimpan…" : "Simpan"}</button></div></form></dialog>; })()}
    {dialogs}
    {drawer.status !== "closed" && <div className="drawer-backdrop" onClick={closeDetail}><aside ref={drawerRef} tabIndex={-1} className="detail-drawer" role="dialog" aria-modal="true" aria-label="Detail transaksi" aria-busy={drawer.status === "loading"} onClick={event => event.stopPropagation()}><button className="drawer-close" aria-label="Tutup detail" onClick={closeDetail}>×</button>{drawer.status === "loading" ? <p>Memuat detail…</p> : drawer.status === "error" ? <div><p role="alert">{drawer.error}</p><button onClick={drawer.retry}>Coba lagi</button></div> : <><span className="eyebrow">DETAIL TRANSAKSI</span><h2>{selected.merchantName || selected.description || typeLabel[selected.type]}</h2><strong className="detail-amount">{money(selected.amount)}</strong><dl style={{ border: 0, borderRadius: 0, overflow: "visible" }}><div><dt>Tanggal</dt><dd>{dateTime(selected.transactionAt)}</dd></div><div><dt>Kategori</dt><dd>{selected.categoryName || "Belum dikategorikan"}</dd></div><div><dt>Akun</dt><dd>{selected.accountName || "—"}</dd></div><div><dt>Sumber pencatatan</dt><dd>{sourceLabel(selected.sourceType)}</dd></div><div><dt>Status</dt><dd>{statusLabel[selected.status] || selected.status}</dd></div>{selected.type === "TRANSFER" && <><div><dt>Tujuan transfer</dt><dd>{purposeLabel(selected.purpose)}</dd></div><div><dt>Akun kekayaan</dt><dd>{selected.relatedWealthAccountName || "—"}</dd></div></>}</dl>{voidNotice && <div className={`notice${voidNotice.kind === "success" ? "" : " error"}`} role={voidNotice.kind === "success" ? "status" : "alert"}><span>{voidNotice.message}</span>{voidNotice.kind === "review" && <a className="button secondary" href="/inbox?view=transactions">Buka Tinjauan</a>}</div>}{(canEditTransaction(selected) || canVoidTransaction(selected)) && <div className="drawer-actions">{canEditTransaction(selected) && <button type="button" className="secondary" onClick={openEdit}>Ubah</button>}{canVoidTransaction(selected) && <button type="button" className="danger" disabled={voiding} aria-busy={voiding || undefined} onClick={voidTransaction}>{voiding ? "Membatalkan…" : "Batalkan transaksi"}</button>}</div>}<h3>Bukti pencatatan</h3><div className="timeline">{evidence.map(item => <article key={item.id}><i>✓</i><div><b>{evidenceLabel(item.evidenceType)}</b><small>{sourceLabel(item.sourceType)} · {dateTime(item.receivedAt)}</small></div></article>)}{!evidence.length && <p className="empty compact">Belum ada sumber pencatatan terhubung.</p>}</div><h3>Riwayat Perubahan</h3><div className="timeline">{audit.map(item => <article key={item.id}><i>•</i><div><b>{auditActionText(item.action)}</b><small>{actorText(item.actorType)} · {dateTime(item.createdAt)}</small></div></article>)}{!audit.length && <p className="empty compact">Belum ada perubahan tercatat.</p>}</div></>}</aside></div>}
  </AppShell>;
}

function purposeLabel(value) { return ({ INTERNAL_TRANSFER: "Transfer internal", SAVINGS_TRANSFER: "Pindah ke tabungan", INVESTMENT_CONTRIBUTION: "Kontribusi investasi", ASSET_PURCHASE: "Pembelian aset", DEBT_PRINCIPAL_PAYMENT: "Pembayaran pokok utang" }[value] || value || "—"); }
function sourceLabel(value) { return ({ BANK_EMAIL: "Email bank", TELEGRAM_TEXT: "Pesan Telegram", TELEGRAM_IMAGE: "Gambar Telegram", WEB_MANUAL: "Input web", WEB_IMAGE: "Dokumen web" }[value] || (value ? evidenceTypeText(value) : "—")); }
function evidenceLabel(value) { return evidenceTypeText(value); }
